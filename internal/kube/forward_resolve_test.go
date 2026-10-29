package kube

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"k8s.io/client-go/rest"
)

type resolveAPI struct {
	mu   sync.Mutex
	pods map[string]map[string]any // name -> object
}

func podObj(name string, ready bool, terminating bool, created string, labels map[string]string, owner map[string]any) map[string]any {
	status := "False"
	if ready {
		status = "True"
	}
	meta := map[string]any{"name": name, "namespace": "default", "creationTimestamp": created, "labels": labels}
	if terminating {
		meta["deletionTimestamp"] = "2026-01-02T00:00:00Z"
	}
	if owner != nil {
		meta["ownerReferences"] = []any{owner}
	}
	return map[string]any{
		"kind": "Pod", "apiVersion": "v1", "metadata": meta,
		"spec":   map[string]any{"containers": []any{map[string]any{"name": "app", "ports": []any{map[string]any{"name": "http", "containerPort": 8080}}}}},
		"status": map[string]any{"phase": "Running", "conditions": []any{map[string]any{"type": "Ready", "status": status}}},
	}
}

func newResolveSession(t *testing.T, api *resolveAPI) *Session {
	mux := http.NewServeMux()
	js := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}
	notFound := func(w http.ResponseWriter) {
		w.WriteHeader(404)
		js(w, map[string]any{"kind": "Status", "apiVersion": "v1", "status": "Failure", "reason": "NotFound", "code": 404})
	}
	mux.HandleFunc("/api/v1/namespaces/default/pods", func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		defer api.mu.Unlock()
		sel := r.URL.Query().Get("labelSelector") // "app=web" or "app=web,tier=x"
		var items []any
		for _, p := range api.pods {
			labels, _ := p["metadata"].(map[string]any)["labels"].(map[string]string)
			ok := true
			for _, kv := range strings.Split(sel, ",") {
				if kv == "" {
					continue
				}
				k, v, _ := strings.Cut(kv, "=")
				if labels[k] != v {
					ok = false
				}
			}
			if ok {
				items = append(items, p)
			}
		}
		js(w, map[string]any{"kind": "PodList", "apiVersion": "v1", "items": items})
	})
	mux.HandleFunc("/api/v1/namespaces/default/pods/", func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		defer api.mu.Unlock()
		if p, ok := api.pods[strings.TrimPrefix(r.URL.Path, "/api/v1/namespaces/default/pods/")]; ok {
			js(w, p)
			return
		}
		notFound(w)
	})
	sel := map[string]any{"matchLabels": map[string]string{"app": "web"}}
	mux.HandleFunc("/apis/apps/v1/namespaces/default/replicasets/web-rs1", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"kind": "ReplicaSet", "apiVersion": "apps/v1", "metadata": map[string]any{"name": "web-rs1", "ownerReferences": []any{map[string]any{"kind": "Deployment", "name": "web"}}},
			"spec": map[string]any{"selector": map[string]any{"matchLabels": map[string]string{"app": "web", "pod-template-hash": "rs1"}}}})
	})
	mux.HandleFunc("/apis/apps/v1/namespaces/default/deployments/web", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"kind": "Deployment", "apiVersion": "apps/v1", "metadata": map[string]any{"name": "web"}, "spec": map[string]any{"selector": sel}})
	})
	mux.HandleFunc("/apis/apps/v1/namespaces/default/statefulsets/db", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"kind": "StatefulSet", "apiVersion": "apps/v1", "metadata": map[string]any{"name": "db"}, "spec": map[string]any{"selector": map[string]any{"matchLabels": map[string]string{"app": "db"}}}})
	})
	mux.HandleFunc("/api/v1/namespaces/default/services/web", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"kind": "Service", "apiVersion": "v1", "metadata": map[string]any{"name": "web"},
			"spec": map[string]any{"selector": map[string]string{"app": "web"}, "ports": []any{map[string]any{"port": 80, "targetPort": "http"}}}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s, err := NewSession("c1", &rest.Config{Host: srv.URL}, func(string, any) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

var webLabels = map[string]string{"app": "web"}
var rsOwner = map[string]any{"kind": "ReplicaSet", "name": "web-rs1"}

func TestServiceResolvesNewestReadyPodAndNamedPort(t *testing.T) {
	api := &resolveAPI{pods: map[string]map[string]any{
		"web-old":  podObj("web-old", true, false, "2026-01-01T00:00:00Z", webLabels, rsOwner),
		"web-new":  podObj("web-new", true, false, "2026-01-01T01:00:00Z", webLabels, rsOwner),
		"web-bad":  podObj("web-bad", false, false, "2026-01-01T02:00:00Z", webLabels, rsOwner),
		"web-term": podObj("web-term", true, true, "2026-01-01T03:00:00Z", webLabels, rsOwner),
	}}
	s := newResolveSession(t, api)
	pod, port, err := s.forwardResolver(ForwardTarget{Namespace: "default", Kind: "service", Name: "web"}, 80)(context.Background())
	if err != nil || pod != "web-new" || port != 8080 {
		t.Fatalf("%q %d %v (terminating and unready pods must be skipped; targetPort name http → 8080)", pod, port, err)
	}
	if _, _, err := s.forwardResolver(ForwardTarget{Namespace: "default", Kind: "service", Name: "web"}, 9999)(context.Background()); err == nil || !strings.Contains(err.Error(), "does not expose") {
		t.Fatalf("%v", err)
	}
}

func (a *resolveAPI) set(name string, obj map[string]any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if obj == nil {
		delete(a.pods, name)
	} else {
		a.pods[name] = obj
	}
}

func TestPodTargetFollowsItsDeploymentWhenReplaced(t *testing.T) {
	api := &resolveAPI{pods: map[string]map[string]any{
		"web-1": podObj("web-1", true, false, "2026-01-01T00:00:00Z", webLabels, rsOwner),
		"web-2": podObj("web-2", true, false, "2026-01-01T01:00:00Z", webLabels, rsOwner),
	}}
	s := newResolveSession(t, api)
	resolve := s.forwardResolver(ForwardTarget{Namespace: "default", Kind: "pod", Name: "web-1"}, 8080)
	ctx := context.Background()

	if pod, port, err := resolve(ctx); err != nil || pod != "web-1" || port != 8080 {
		t.Fatalf("a healthy pod forwards to itself: %q %d %v", pod, port, err)
	}
	api.set("web-1", podObj("web-1", true, true, "2026-01-01T00:00:00Z", webLabels, rsOwner)) // terminating
	if pod, _, err := resolve(ctx); err != nil || pod != "web-2" {
		t.Fatalf("a terminating pod is replaced by a sibling of its Deployment: %q %v", pod, err)
	}
	api.set("web-1", nil) // gone entirely
	api.set("web-3", podObj("web-3", true, false, "2026-01-01T05:00:00Z", webLabels, rsOwner))
	if pod, _, err := resolve(ctx); err != nil || pod != "web-3" {
		t.Fatalf("after the pod is deleted the newest ready replica is used: %q %v", pod, err)
	}
	api.set("web-2", podObj("web-2", false, false, "2026-01-01T01:00:00Z", webLabels, rsOwner))
	api.set("web-3", nil)
	if _, _, err := resolve(ctx); err == nil || !strings.Contains(err.Error(), "no ready replacement") {
		t.Fatalf("%v", err)
	}
}

func TestStatefulSetAndBarePodsWaitForTheSamePod(t *testing.T) {
	dbOwner := map[string]any{"kind": "StatefulSet", "name": "db"}
	api := &resolveAPI{pods: map[string]map[string]any{
		"db-0": podObj("db-0", false, false, "2026-01-01T00:00:00Z", map[string]string{"app": "db"}, dbOwner),
		"db-1": podObj("db-1", true, false, "2026-01-01T00:00:00Z", map[string]string{"app": "db"}, dbOwner),
		"solo": podObj("solo", true, false, "2026-01-01T00:00:00Z", map[string]string{"app": "solo"}, nil),
	}}
	s := newResolveSession(t, api)
	ctx := context.Background()
	db0 := s.forwardResolver(ForwardTarget{Namespace: "default", Kind: "pod", Name: "db-0"}, 5432)
	if _, _, err := db0(ctx); err == nil || !strings.Contains(err.Error(), "waiting for it to come back") {
		t.Fatalf("db-0 is a specific replica: it must not be swapped for db-1: %v", err)
	}
	api.set("db-0", podObj("db-0", true, false, "2026-01-01T00:00:00Z", map[string]string{"app": "db"}, dbOwner))
	if pod, _, err := db0(ctx); err != nil || pod != "db-0" {
		t.Fatalf("%q %v", pod, err)
	}
	solo := s.forwardResolver(ForwardTarget{Namespace: "default", Kind: "pod", Name: "solo"}, 80)
	if pod, _, err := solo(ctx); err != nil || pod != "solo" {
		t.Fatalf("%q %v", pod, err)
	}
	if _, _, err := s.forwardResolver(ForwardTarget{Namespace: "default", Kind: "pod", Name: "ghost"}, 80)(ctx); err == nil {
		t.Fatal("a pod that never existed is an error")
	}
}

func TestWorkloadTargetUsesItsSelector(t *testing.T) {
	api := &resolveAPI{pods: map[string]map[string]any{
		"web-1": podObj("web-1", true, false, "2026-01-01T00:00:00Z", webLabels, rsOwner),
		"web-2": podObj("web-2", true, false, "2026-01-01T02:00:00Z", webLabels, rsOwner),
		"other": podObj("other", true, false, "2026-01-01T09:00:00Z", map[string]string{"app": "other"}, nil),
	}}
	s := newResolveSession(t, api)
	pod, port, err := s.forwardResolver(ForwardTarget{Namespace: "default", Kind: "deployment", Name: "web"}, 3000)(context.Background())
	if err != nil || pod != "web-2" || port != 3000 {
		t.Fatalf("%q %d %v", pod, port, err)
	}
	if _, _, err := s.forwardResolver(ForwardTarget{Namespace: "default", Kind: "cronjob", Name: "x"}, 1)(context.Background()); err == nil {
		t.Fatal("unsupported kinds are rejected")
	}
}
