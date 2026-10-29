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

// A cluster that was never opened in the explorer (no session, no catalog) must still be usable by the comparison.
func TestEnsureReadyConnectsAndDiscoversOnDemandAndListsNamespaces(t *testing.T) {
	var discoveries atomic.Int32
	js := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"kind": "APIVersions", "versions": []string{"v1"}})
	})
	mux.HandleFunc("/apis", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"kind": "APIGroupList", "groups": []any{}})
	})
	mux.HandleFunc("/api/v1", func(w http.ResponseWriter, r *http.Request) {
		discoveries.Add(1)
		js(w, map[string]any{"kind": "APIResourceList", "groupVersion": "v1", "resources": []map[string]any{
			{"name": "namespaces", "kind": "Namespace", "namespaced": false, "verbs": []string{"list", "watch", "get"}},
			{"name": "configmaps", "kind": "ConfigMap", "namespaced": true, "verbs": []string{"list", "watch", "get"}},
		}})
	})
	mux.HandleFunc("/api/v1/namespaces", func(w http.ResponseWriter, r *http.Request) {
		item := func(n string) map[string]any {
			return map[string]any{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": n}}
		}
		js(w, map[string]any{"kind": "NamespaceList", "apiVersion": "v1", "items": []any{item("prod"), item("default"), item("kube-system")}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	m := NewManager(func(string) (*rest.Config, error) { return &rest.Config{Host: srv.URL}, nil }, func(string, any) {})

	if _, err := m.Session("never-opened"); err == nil {
		t.Fatal("precondition: a cluster that was not opened has no session")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := m.EnsureReady(ctx, "never-opened")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.resource("core/v1/configmaps"); err != nil {
		t.Fatalf("the catalog must be loaded so kinds can be resolved: %v", err)
	}
	ns, err := s.Namespaces(ctx)
	if err != nil || len(ns) != 3 || ns[0] != "default" || ns[2] != "prod" {
		t.Fatalf("%v %v", ns, err)
	}
	if _, err := m.EnsureReady(ctx, "never-opened"); err != nil {
		t.Fatal(err)
	}
	if n := discoveries.Load(); n != 1 {
		t.Errorf("discovery must run once, not on every call: %d", n)
	}
}
