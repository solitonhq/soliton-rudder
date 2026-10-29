package kube

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/client-go/rest"
)

func newMetricsSession(t *testing.T, withMetrics bool) (*Session, *atomic.Int32) {
	var calls atomic.Int32
	js := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/apis/metrics.k8s.io/v1beta1/namespaces/default/pods", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !withMetrics {
			w.WriteHeader(404)
			js(w, map[string]any{"kind": "Status", "apiVersion": "v1", "status": "Failure", "reason": "NotFound", "code": 404})
			return
		}
		js(w, map[string]any{"items": []map[string]any{
			{"metadata": map[string]any{"name": "web-1", "namespace": "default"}, "containers": []map[string]any{
				{"usage": map[string]string{"cpu": "250m", "memory": "100Mi"}}, {"usage": map[string]string{"cpu": "50m", "memory": "28Mi"}}}},
			{"metadata": map[string]any{"name": "web-2", "namespace": "default"}, "containers": []map[string]any{
				{"usage": map[string]string{"cpu": "100m", "memory": "64Mi"}}}},
			{"metadata": map[string]any{"name": "other", "namespace": "default"}, "containers": []map[string]any{
				{"usage": map[string]string{"cpu": "999m", "memory": "1Gi"}}}},
		}})
	})
	mux.HandleFunc("/apis/metrics.k8s.io/v1beta1/nodes", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"items": []map[string]any{{"metadata": map[string]any{"name": "n1"}, "usage": map[string]string{"cpu": "1500m", "memory": "2Gi"}}}})
	})
	mux.HandleFunc("/api/v1/nodes/n1", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"kind": "Node", "apiVersion": "v1", "metadata": map[string]any{"name": "n1"}, "status": map[string]any{"allocatable": map[string]string{"cpu": "4", "memory": "8Gi"}}})
	})
	mux.HandleFunc("/apis/apps/v1/namespaces/default/deployments/web", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"kind": "Deployment", "apiVersion": "apps/v1", "metadata": map[string]any{"name": "web"}, "spec": map[string]any{"selector": map[string]any{"matchLabels": map[string]string{"app": "web"}}}})
	})
	mux.HandleFunc("/api/v1/namespaces/default/pods", func(w http.ResponseWriter, r *http.Request) {
		res := map[string]any{"limits": map[string]string{"memory": "256Mi"}, "requests": map[string]string{"cpu": "200m", "memory": "128Mi"}}
		pod := func(n string) map[string]any {
			return map[string]any{"kind": "Pod", "apiVersion": "v1", "metadata": map[string]any{"name": n, "namespace": "default"}, "spec": map[string]any{"containers": []map[string]any{{"name": "c", "resources": res}}}}
		}
		js(w, map[string]any{"kind": "PodList", "apiVersion": "v1", "items": []any{pod("web-1"), pod("web-2")}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s, err := NewSession("c1", &rest.Config{Host: srv.URL}, func(string, any) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	s.catalog["core/v1/nodes"] = Resource{Key: "core/v1/nodes", Group: "core", Version: "v1", Name: "nodes", Kind: "Node"}
	s.catalog["core/v1/pods"] = Resource{Key: "core/v1/pods", Group: "core", Version: "v1", Name: "pods", Kind: "Pod", Namespaced: true}
	s.catalog["apps/v1/deployments"] = Resource{Key: "apps/v1/deployments", Group: "apps", Version: "v1", Name: "deployments", Kind: "Deployment", Namespaced: true}
	return s, &calls
}

func TestUsageMapParsesAndSumsContainers(t *testing.T) {
	s, _ := newMetricsSession(t, true)
	m, err := s.UsageMap(context.Background(), "pods", "default")
	if err != nil {
		t.Fatal(err)
	}
	if u := m["default/web-1"]; u.CPU != 300 || u.Mem != 128*1024*1024 {
		t.Fatalf("%+v", u)
	}
	nodes, _ := s.UsageMap(context.Background(), "nodes", "")
	if nodes["n1"].CPU != 1500 {
		t.Fatalf("%+v", nodes)
	}
	if _, err := s.UsageMap(context.Background(), "services", ""); err == nil {
		t.Fatal("unsupported kind must fail")
	}
}

func TestWorkloadSeriesAggregatesItsPodsOnly(t *testing.T) {
	s, _ := newMetricsSession(t, true)
	ser, err := s.MetricsSeries(context.Background(), "apps/v1/deployments", "default", "web")
	if err != nil {
		t.Fatal(err)
	}
	if !ser.Available || ser.Members != 2 || len(ser.Points) != 1 {
		t.Fatalf("%+v", ser)
	}
	if p := ser.Points[0]; p.CPU != 400 || p.Mem != (128+64)*1024*1024 {
		t.Fatalf("point: %+v", p)
	}
	if ser.CPURequest != 400 || ser.MemLimit != 512*1024*1024 {
		t.Fatalf("sizing: %+v", ser)
	}
}

func TestNodeSeriesAndSamplerKeepsHistory(t *testing.T) {
	s, _ := newMetricsSession(t, true)
	s.metricsEvery = 30 * time.Millisecond
	ser, err := s.MetricsSeries(context.Background(), "core/v1/nodes", "", "n1")
	if err != nil || !ser.Available || ser.CPUCapacity != 4000 || ser.MemCapacity != 8*1024*1024*1024 {
		t.Fatalf("%v %+v", err, ser)
	}
	time.Sleep(150 * time.Millisecond)
	ser, _ = s.MetricsSeries(context.Background(), "core/v1/nodes", "", "n1")
	if len(ser.Points) < 3 {
		t.Fatalf("expected history to accumulate, got %d", len(ser.Points))
	}
}

func TestSeriesWithoutMetricsServerIsFriendly(t *testing.T) {
	s, _ := newMetricsSession(t, false)
	ser, err := s.MetricsSeries(context.Background(), "apps/v1/deployments", "default", "web")
	if err != nil {
		t.Fatal(err)
	}
	if ser.Available || ser.Error != noMetrics {
		t.Fatalf("%+v", ser)
	}
}
