package kube

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/rest"
)

const probePod = `
apiVersion: v1
kind: Pod
metadata: {name: web-1, namespace: shop}
spec:
  containers:
  - name: app
    ports: [{name: http, containerPort: 8080}, {name: metrics, containerPort: 9090}]
    startupProbe: {httpGet: {path: /started, port: http}, failureThreshold: 30, periodSeconds: 2}
    livenessProbe: {httpGet: {path: /healthz, port: 8080, scheme: HTTPS, httpHeaders: [{name: X-Token, value: abc}]}, initialDelaySeconds: 15, periodSeconds: 20, timeoutSeconds: 3}
    readinessProbe: {httpGet: {port: metrics}}
  - name: db
    livenessProbe: {tcpSocket: {port: 5432}}
    readinessProbe: {exec: {command: [pg_isready, -U, app]}, periodSeconds: 5}
  - name: grpc
    livenessProbe: {grpc: {port: 50051, service: health}}
  - name: plain
    image: x
status:
  podIP: 10.244.1.5
  phase: Running
  containerStatuses:
  - {name: app, ready: true, started: true, restartCount: 0, state: {running: {}}}
  - {name: db, ready: false, started: true, restartCount: 4, state: {waiting: {reason: CrashLoopBackOff}}}
  - {name: grpc, ready: true, started: true, restartCount: 0, state: {running: {}}}`

func findProbe(p PodProbes, container, kind string) ProbeInfo {
	for _, x := range p.Probes {
		if x.Container == container && x.Kind == kind {
			return x
		}
	}
	return ProbeInfo{}
}

func TestProbesOfResolvesPortsAndFillsKubernetesDefaults(t *testing.T) {
	pod := u(t, probePod)
	p := probesOf(&pod)
	if p.PodIP != "10.244.1.5" || p.Phase != "Running" {
		t.Errorf("%+v", p)
	}
	live := findProbe(p, "app", "liveness")
	if live.Type != "http" || live.Scheme != "HTTPS" || live.Path != "/healthz" || live.Port != 8080 || live.InitialDelay != 15 || live.Period != 20 || live.Timeout != 3 || live.FailureThreshold != 3 || live.SuccessThreshold != 1 {
		t.Errorf("liveness: %+v", live)
	}
	if len(live.Headers) != 1 || live.Headers[0] != (ProbeHeader{"X-Token", "abc"}) {
		t.Errorf("headers: %+v", live.Headers)
	}
	start := findProbe(p, "app", "startup")
	if start.Port != 8080 || start.PortName != "http" || start.FailureThreshold != 30 || start.Period != 2 {
		t.Errorf("a named port is resolved to its number and keeps its name: %+v", start)
	}
	ready := findProbe(p, "app", "readiness")
	if ready.Port != 9090 || ready.Path != "/" || ready.Scheme != "HTTP" || ready.Period != 10 || ready.Timeout != 1 || ready.InitialDelay != 0 {
		t.Errorf("defaults (path /, HTTP, period 10s, timeout 1s): %+v", ready)
	}
	if tcp := findProbe(p, "db", "liveness"); tcp.Type != "tcp" || tcp.Port != 5432 {
		t.Errorf("tcp: %+v", tcp)
	}
	if ex := findProbe(p, "db", "readiness"); ex.Type != "exec" || strings.Join(ex.Command, " ") != "pg_isready -U app" || ex.Period != 5 {
		t.Errorf("exec: %+v", ex)
	}
	if g := findProbe(p, "grpc", "liveness"); g.Type != "grpc" || g.Port != 50051 || g.GRPCService != "health" {
		t.Errorf("grpc: %+v", g)
	}
	if len(p.Probes) != 6 {
		t.Errorf("a container without probes contributes none: %d", len(p.Probes))
	}
}

func TestProbesOfReportsContainerStateAndUnknownNamedPorts(t *testing.T) {
	pod := u(t, probePod)
	p := probesOf(&pod)
	got := map[string]ContainerState{}
	for _, c := range p.Containers {
		got[c.Name] = c
	}
	if c := got["db"]; c.Ready || c.Restarts != 4 || c.State != "waiting: CrashLoopBackOff" {
		t.Errorf("%+v", c)
	}
	if c := got["app"]; !c.Ready || c.State != "running" {
		t.Errorf("%+v", c)
	}
	if c := got["plain"]; c.State != "waiting" || c.Ready {
		t.Errorf("a container with no status yet: %+v", c)
	}
	bad := u(t, `{apiVersion: v1, kind: Pod, metadata: {name: x}, spec: {containers: [{name: c, livenessProbe: {httpGet: {path: /h, port: nope}}}]}}`)
	if pr := probesOf(&bad).Probes[0]; pr.Port != 0 || pr.PortName != "nope" {
		t.Errorf("an undeclared named port stays unresolved: %+v", pr)
	}
	empty := u(t, `{apiVersion: v1, kind: Pod, metadata: {name: x}, spec: {containers: [{name: c}]}}`)
	assertNoNilSlices(t, "PodProbes", probesOf(&empty))
}

func TestCheckProbeThroughARealTunnel(t *testing.T) {
	api := fakeKubelet(t, containerHTTP(t))
	s, err := NewSession("c1", &rest.Config{Host: api.URL}, func(string, any) {})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	ok, err := s.CheckProbe(ctx, "shop", "web-1", ProbeRequest{Type: "http", Scheme: "HTTP", Port: 8080, Path: "/healthz"})
	if err != nil || !ok.OK || ok.Status != 200 || !strings.Contains(ok.Body, "hello from the pod: /healthz") {
		t.Fatalf("%v %+v", err, ok)
	}
	fail, err := s.CheckProbe(ctx, "shop", "web-1", ProbeRequest{Type: "http", Port: 8080, Path: "/fail"})
	if err != nil || fail.OK || fail.Status != 503 || fail.Body != "not ready" || !strings.Contains(fail.Detail, "only 200") {
		t.Fatalf("a 503 is a failed probe, with its body: %v %+v", err, fail)
	}
	hdr, err := s.CheckProbe(ctx, "shop", "web-1", ProbeRequest{Type: "http", Port: 8080, Path: "/hdr", Headers: []ProbeHeader{{"X-Token", "abc"}, {"Host", "probe.example"}}})
	if err != nil || !strings.Contains(hdr.Body, "token=abc") || !strings.Contains(hdr.Body, "host=probe.example") || !strings.Contains(hdr.Body, "ua=kube-probe") {
		t.Fatalf("probe headers (including Host) are sent like the kubelet does: %v %+v", err, hdr)
	}
	tcp, err := s.CheckProbe(ctx, "shop", "web-1", ProbeRequest{Type: "tcp", Port: 8080})
	if err != nil || !tcp.OK {
		t.Fatalf("%v %+v", err, tcp)
	}
	if _, err := s.CheckProbe(ctx, "shop", "web-1", ProbeRequest{Type: "exec", Port: 1}); err == nil || !strings.Contains(err.Error(), "Shell") {
		t.Errorf("exec probes cannot run from here: %v", err)
	}
	if _, err := s.CheckProbe(ctx, "shop", "web-1", ProbeRequest{Type: "http", Port: 0}); err == nil {
		t.Error("an unresolved named port must be reported, not probed")
	}
}

func TestCheckTCPProbeOnAClosedPortFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := ln.Addr().String()
	ln.Close() // nothing listens there any more
	api := fakeKubelet(t, closed)
	s, err := NewSession("c1", &rest.Config{Host: api.URL}, func(string, any) {})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	r, err := s.CheckProbe(ctx, "shop", "web-1", ProbeRequest{Type: "tcp", Port: 5432})
	if err != nil || r.OK {
		t.Fatalf("a port nobody listens on must fail: %v %+v", err, r)
	}
}
