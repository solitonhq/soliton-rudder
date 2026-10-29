package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	maxSamples   = 60              // 15 minutes at the default 15s interval
	scopeIdleTTL = 5 * time.Minute // samplers stop when nobody asks for a while
)

// Usage is resource consumption: CPU in millicores, memory in bytes.
type Usage struct {
	CPU int64 `json:"cpu"`
	Mem int64 `json:"mem"`
}

type Point struct {
	T   int64 `json:"t"` // unix ms
	CPU int64 `json:"cpu"`
	Mem int64 `json:"mem"`
}

// Series is a usage history for one object plus its sizing, for charts.
type Series struct {
	Available    bool    `json:"available"`
	Error        string  `json:"error"`
	Source       string  `json:"source"` // metrics-server | prometheus
	IntervalSecs int     `json:"intervalSecs"`
	Points       []Point `json:"points"`
	Members      int     `json:"members"` // pods aggregated
	CPURequest   int64   `json:"cpuRequest"`
	CPULimit     int64   `json:"cpuLimit"`
	MemRequest   int64   `json:"memRequest"`
	MemLimit     int64   `json:"memLimit"`
	CPUCapacity  int64   `json:"cpuCapacity"` // nodes: allocatable
	MemCapacity  int64   `json:"memCapacity"`
}

const noMetrics = "metrics-server is not available on this cluster"

type sample struct {
	t int64
	u map[string]Usage
}

type metricScope struct {
	kind, ns string

	mu      sync.Mutex
	samples []sample
	err     error
	lastReq time.Time
	running bool
	first   chan struct{} // closed after the first sample attempt
	once    sync.Once
}

// fetchUsage reads metrics.k8s.io; keys are "ns/name" for pods and "name" for nodes.
func (s *Session) fetchUsage(ctx context.Context, kind, ns string) (map[string]Usage, error) {
	path := "/apis/metrics.k8s.io/v1beta1/nodes"
	if kind == "pods" {
		path = "/apis/metrics.k8s.io/v1beta1/pods"
		if ns != "" {
			path = "/apis/metrics.k8s.io/v1beta1/namespaces/" + ns + "/pods"
		}
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	raw, err := s.rc.Get().AbsPath(path).Do(ctx).Raw()
	if err != nil {
		if apierrors.IsNotFound(err) || apierrors.IsServiceUnavailable(err) {
			return nil, errors.New(noMetrics)
		}
		return nil, err
	}
	var list struct {
		Items []struct {
			Metadata struct{ Name, Namespace string } `json:"metadata"`
			Usage    map[string]string                `json:"usage"`
			Contains []struct {
				Usage map[string]string `json:"usage"`
			} `json:"containers"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	out := make(map[string]Usage, len(list.Items))
	for _, it := range list.Items {
		var u Usage
		add := func(m map[string]string) {
			if q, err := resource.ParseQuantity(m["cpu"]); err == nil {
				u.CPU += q.MilliValue()
			}
			if q, err := resource.ParseQuantity(m["memory"]); err == nil {
				u.Mem += q.Value()
			}
		}
		if kind == "nodes" {
			add(it.Usage)
			out[it.Metadata.Name] = u
			continue
		}
		for _, c := range it.Contains {
			add(c.Usage)
		}
		out[it.Metadata.Namespace+"/"+it.Metadata.Name] = u
	}
	return out, nil
}

// UsageMap returns current usage for all pods (or one namespace) or all nodes.
func (s *Session) UsageMap(ctx context.Context, kind, ns string) (map[string]Usage, error) {
	if kind != "pods" && kind != "nodes" {
		return nil, fmt.Errorf("usage is only available for pods and nodes")
	}
	return s.fetchUsage(ctx, kind, ns)
}

func (s *Session) scope(kind, ns string) *metricScope {
	id := kind + ":" + ns
	s.mu.Lock()
	defer s.mu.Unlock()
	sc := s.scopes[id]
	if sc == nil {
		sc = &metricScope{kind: kind, ns: ns, first: make(chan struct{})}
		s.scopes[id] = sc
	}
	sc.mu.Lock()
	sc.lastReq = time.Now()
	start := !sc.running
	sc.running = true
	sc.mu.Unlock()
	if start {
		go s.sampleLoop(sc)
	}
	return sc
}

func (s *Session) sampleLoop(sc *metricScope) {
	t := time.NewTicker(s.metricsEvery)
	defer t.Stop()
	for {
		u, err := s.fetchUsage(s.ctx, sc.kind, sc.ns)
		sc.mu.Lock()
		sc.err = err
		if err == nil {
			sc.samples = append(sc.samples, sample{t: time.Now().UnixMilli(), u: u})
			if len(sc.samples) > maxSamples {
				sc.samples = sc.samples[len(sc.samples)-maxSamples:]
			}
		}
		sc.mu.Unlock()
		sc.once.Do(func() { close(sc.first) })

		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
		}
		sc.mu.Lock()
		if time.Since(sc.lastReq) > scopeIdleTTL {
			sc.running = false
			sc.mu.Unlock()
			return
		}
		sc.mu.Unlock()
	}
}

// MetricsSeries returns the recent usage history of a pod, node or workload (summed over its pods).
func (s *Session) MetricsSeries(ctx context.Context, key, ns, name string) (Series, error) {
	res, err := s.resource(key)
	if err != nil {
		return Series{}, err
	}
	out := Series{Source: "metrics-server", IntervalSecs: int(s.metricsEvery / time.Second), Points: []Point{}}
	gctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	var members map[string]bool
	kind, scopeNS := "pods", ns
	switch res.Name {
	case "nodes":
		kind, scopeNS = "nodes", ""
		node, err := s.client(res, "").Get(gctx, name, metav1.GetOptions{})
		if err != nil {
			return out, err
		}
		members = map[string]bool{name: true}
		if q, ok := nestedQuantity(node.Object, "status", "allocatable", "cpu"); ok {
			out.CPUCapacity = q.MilliValue()
		}
		if q, ok := nestedQuantity(node.Object, "status", "allocatable", "memory"); ok {
			out.MemCapacity = q.Value()
		}
	case "pods", "deployments", "statefulsets", "daemonsets", "replicasets", "jobs", "replicationcontrollers", "services":
		pods, err := s.podsFor(gctx, res, ns, name)
		if err != nil {
			return out, err
		}
		members = map[string]bool{}
		for i := range pods {
			members[ns+"/"+pods[i].GetName()] = true
			addPodSizing(&out, &pods[i])
		}
	default:
		return out, fmt.Errorf("metrics are not available for %s", res.Kind)
	}
	out.Members = len(members)

	sc := s.scope(kind, scopeNS)
	select {
	case <-sc.first:
	case <-gctx.Done():
		return out, gctx.Err()
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if len(sc.samples) == 0 {
		if sc.err != nil {
			out.Error = sc.err.Error()
		}
		return out, nil
	}
	out.Available = true
	for _, sm := range sc.samples {
		var p Point
		found := false
		for m := range members {
			if u, ok := sm.u[m]; ok {
				p.CPU += u.CPU
				p.Mem += u.Mem
				found = true
			}
		}
		if found {
			p.T = sm.t
			out.Points = append(out.Points, p)
		}
	}
	return out, nil
}

func addPodSizing(out *Series, pod *unstructured.Unstructured) {
	cs, _, _ := unstructured.NestedSlice(pod.Object, "spec", "containers")
	for _, c := range cs {
		cm, _ := c.(map[string]any)
		res, _ := cm["resources"].(map[string]any)
		for _, f := range []struct {
			group, name string
			dst         *int64
			milli       bool
		}{
			{"requests", "cpu", &out.CPURequest, true}, {"limits", "cpu", &out.CPULimit, true},
			{"requests", "memory", &out.MemRequest, false}, {"limits", "memory", &out.MemLimit, false},
		} {
			g, _ := res[f.group].(map[string]any)
			if v, ok := g[f.name].(string); ok {
				if q, err := resource.ParseQuantity(v); err == nil {
					if f.milli {
						*f.dst += q.MilliValue()
					} else {
						*f.dst += q.Value()
					}
				}
			}
		}
	}
}

func nestedQuantity(obj map[string]any, fields ...string) (resource.Quantity, bool) {
	v, ok, _ := unstructured.NestedString(obj, fields...)
	if !ok {
		return resource.Quantity{}, false
	}
	q, err := resource.ParseQuantity(v)
	return q, err == nil
}
