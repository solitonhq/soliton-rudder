package kube

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type ProbeHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ProbeInfo is one liveness, readiness or startup probe of a container, with Kubernetes' defaults filled in.
type ProbeInfo struct {
	Container        string        `json:"container"`
	Kind             string        `json:"kind"` // liveness | readiness | startup
	Type             string        `json:"type"` // http | tcp | exec | grpc
	Scheme           string        `json:"scheme"`
	Path             string        `json:"path"`
	Port             int           `json:"port"` // resolved; 0 if a named port is not declared
	PortName         string        `json:"portName"`
	Host             string        `json:"host"`
	Headers          []ProbeHeader `json:"headers"`
	Command          []string      `json:"command"`
	GRPCService      string        `json:"grpcService"`
	InitialDelay     int           `json:"initialDelay"`
	Period           int           `json:"period"`
	Timeout          int           `json:"timeout"`
	SuccessThreshold int           `json:"successThreshold"`
	FailureThreshold int           `json:"failureThreshold"`
}

type ContainerState struct {
	Name     string `json:"name"`
	Ready    bool   `json:"ready"`
	Started  bool   `json:"started"`
	Restarts int    `json:"restarts"`
	State    string `json:"state"` // running | waiting: reason | terminated: reason
}

type PodProbes struct {
	PodIP      string           `json:"podIP"`
	Phase      string           `json:"phase"`
	Probes     []ProbeInfo      `json:"probes"`
	Containers []ContainerState `json:"containers"`
}

func intOr(m map[string]any, key string, def int) int {
	switch v := m[key].(type) {
	case int64:
		if v > 0 || key == "initialDelaySeconds" {
			return int(v)
		}
	case float64:
		if v > 0 || key == "initialDelaySeconds" {
			return int(v)
		}
	}
	return def
}

// probesOf reads the probes of every container, resolving named ports against the container's declared ports.
func probesOf(pod *unstructured.Unstructured) PodProbes {
	out := PodProbes{Probes: []ProbeInfo{}, Containers: []ContainerState{}}
	out.PodIP, _, _ = unstructured.NestedString(pod.Object, "status", "podIP")
	out.Phase, _, _ = unstructured.NestedString(pod.Object, "status", "phase")

	statuses := map[string]map[string]any{}
	css, _, _ := unstructured.NestedSlice(pod.Object, "status", "containerStatuses")
	for _, c := range css {
		if cm, ok := c.(map[string]any); ok {
			statuses[str(cm["name"])] = cm
		}
	}
	containers, _, _ := unstructured.NestedSlice(pod.Object, "spec", "containers")
	for _, c := range containers {
		cm, _ := c.(map[string]any)
		name := str(cm["name"])
		named := map[string]int{}
		ports, _ := cm["ports"].([]any)
		for _, p := range ports {
			pm, _ := p.(map[string]any)
			if n, ok := pm["containerPort"].(int64); ok && str(pm["name"]) != "" {
				named[str(pm["name"])] = int(n)
			}
		}
		for _, kind := range []struct{ field, label string }{{"startupProbe", "startup"}, {"livenessProbe", "liveness"}, {"readinessProbe", "readiness"}} {
			pr, ok := cm[kind.field].(map[string]any)
			if !ok {
				continue
			}
			pi := ProbeInfo{
				Container: name, Kind: kind.label, Headers: []ProbeHeader{}, Command: []string{},
				InitialDelay: intOr(pr, "initialDelaySeconds", 0), Period: intOr(pr, "periodSeconds", 10),
				Timeout: intOr(pr, "timeoutSeconds", 1), SuccessThreshold: intOr(pr, "successThreshold", 1),
				FailureThreshold: intOr(pr, "failureThreshold", 3),
			}
			port := func(v any) {
				switch p := v.(type) {
				case int64:
					pi.Port = int(p)
				case float64:
					pi.Port = int(p)
				case string:
					if n, err := strconv.Atoi(p); err == nil {
						pi.Port = n
					} else {
						pi.PortName, pi.Port = p, named[p]
					}
				}
			}
			switch {
			case pr["httpGet"] != nil:
				hg, _ := pr["httpGet"].(map[string]any)
				pi.Type, pi.Scheme, pi.Path, pi.Host = "http", strings.ToUpper(str(hg["scheme"])), str(hg["path"]), str(hg["host"])
				if pi.Scheme == "" {
					pi.Scheme = "HTTP"
				}
				if pi.Path == "" {
					pi.Path = "/"
				}
				port(hg["port"])
				hs, _ := hg["httpHeaders"].([]any)
				for _, h := range hs {
					hm, _ := h.(map[string]any)
					pi.Headers = append(pi.Headers, ProbeHeader{Name: str(hm["name"]), Value: str(hm["value"])})
				}
			case pr["tcpSocket"] != nil:
				ts, _ := pr["tcpSocket"].(map[string]any)
				pi.Type, pi.Host = "tcp", str(ts["host"])
				port(ts["port"])
			case pr["grpc"] != nil:
				g, _ := pr["grpc"].(map[string]any)
				pi.Type, pi.GRPCService = "grpc", str(g["service"])
				port(g["port"])
			case pr["exec"] != nil:
				e, _ := pr["exec"].(map[string]any)
				pi.Type = "exec"
				pi.Command = strSlice(e["command"])
			default:
				continue
			}
			out.Probes = append(out.Probes, pi)
		}
		cs := ContainerState{Name: name, State: "waiting"}
		if st := statuses[name]; st != nil {
			cs.Ready, _ = st["ready"].(bool)
			cs.Started, _ = st["started"].(bool)
			if n, ok := st["restartCount"].(int64); ok {
				cs.Restarts = int(n)
			}
			if state, ok := st["state"].(map[string]any); ok {
				switch {
				case state["running"] != nil:
					cs.State = "running"
				case state["waiting"] != nil:
					w, _ := state["waiting"].(map[string]any)
					cs.State = "waiting: " + str(w["reason"])
				case state["terminated"] != nil:
					t, _ := state["terminated"].(map[string]any)
					cs.State = "terminated: " + str(t["reason"])
				}
			}
		}
		out.Containers = append(out.Containers, cs)
	}
	return out
}

// PodProbes lists the probes of a pod and the state of its containers.
func (s *Session) PodProbes(ctx context.Context, ns, pod string) (PodProbes, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	p, err := s.client(podRes, ns).Get(ctx, pod, metav1.GetOptions{})
	if err != nil {
		return PodProbes{}, err
	}
	return probesOf(p), nil
}

type ProbeRequest struct {
	Type    string        `json:"type"` // http | tcp
	Scheme  string        `json:"scheme"`
	Port    int           `json:"port"`
	Path    string        `json:"path"`
	Headers []ProbeHeader `json:"headers"`
}

type ProbeResult struct {
	OK         bool   `json:"ok"`
	Status     int    `json:"status"` // HTTP status, 0 for TCP or when no response arrived
	DurationMs int64  `json:"durationMs"`
	Detail     string `json:"detail"`
	Body       string `json:"body"` // the first part of the response body
}

const probeBodyMax = 2048

// CheckProbe runs a probe from this machine through a temporary port-forward to the pod, and reports what Kubernetes
// would conclude: an HTTP status from 200 to 399 is success. Exec and gRPC probes cannot be run this way.
func (s *Session) CheckProbe(ctx context.Context, ns, pod string, req ProbeRequest) (ProbeResult, error) {
	if req.Port < 1 || req.Port > 65535 {
		return ProbeResult{}, fmt.Errorf("the probe's port is not known (a named port that the container does not declare?)")
	}
	switch req.Type {
	case "http", "tcp":
	case "exec":
		return ProbeResult{}, fmt.Errorf("exec probes run a command inside the container: use Shell to run it")
	default:
		return ProbeResult{}, fmt.Errorf("%s probes cannot be run from here", req.Type)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tun, err := s.startTunnel(ctx, ns, pod, req.Port)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("could not reach the pod: %w", err)
	}
	defer tun.stop()
	return runProbe(ctx, tun.addr, req), nil
}

// runProbe performs the request against addr (a local tunnel endpoint).
func runProbe(ctx context.Context, addr string, req ProbeRequest) ProbeResult {
	start := time.Now()
	done := func(r ProbeResult) ProbeResult {
		r.DurationMs = time.Since(start).Milliseconds()
		return r
	}
	if req.Type == "tcp" {
		c, err := net.DialTimeout("tcp", addr, 3*time.Second)
		if err != nil {
			return done(ProbeResult{Detail: err.Error()})
		}
		defer c.Close()
		// the local end accepts at once; a refused port shows up as the stream closing right after
		_ = c.SetReadDeadline(time.Now().Add(800 * time.Millisecond))
		buf := make([]byte, 1)
		if _, err := c.Read(buf); err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return done(ProbeResult{OK: true, Detail: "the port accepted the connection"})
			}
			return done(ProbeResult{Detail: "the connection was closed straight away: nothing is listening on that port"})
		}
		return done(ProbeResult{OK: true, Detail: "the port accepted the connection"})
	}

	scheme := "http"
	if strings.EqualFold(req.Scheme, "HTTPS") {
		scheme = "https"
	}
	path := req.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, scheme+"://"+addr+path, nil)
	if err != nil {
		return done(ProbeResult{Detail: err.Error()})
	}
	hreq.Header.Set("User-Agent", "kube-probe/soliton-rudder")
	hreq.Header.Set("Accept", "*/*")
	for _, h := range req.Headers {
		if strings.EqualFold(h.Name, "Host") {
			hreq.Host = h.Value
			continue
		}
		hreq.Header.Set(h.Name, h.Value)
	}
	client := &http.Client{
		Timeout:       8 * time.Second,
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DisableKeepAlives: true}, // like the kubelet
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(hreq)
	if err != nil {
		return done(ProbeResult{Detail: err.Error()})
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, probeBodyMax))
	ok := resp.StatusCode >= 200 && resp.StatusCode < 400
	detail := resp.Status
	if !ok {
		detail += ": Kubernetes counts this as a failure (only 200–399 succeed)"
	}
	return done(ProbeResult{OK: ok, Status: resp.StatusCode, Detail: detail, Body: string(body)})
}
