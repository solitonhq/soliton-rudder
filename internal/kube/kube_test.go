package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"k8s.io/client-go/rest"
)

func tableJSON(rv string, withCols bool, rows ...[3]string) map[string]any {
	t := map[string]any{
		"kind": "Table", "apiVersion": "meta.k8s.io/v1",
		"metadata": map[string]any{"resourceVersion": rv},
	}
	if withCols {
		t["columnDefinitions"] = []map[string]any{
			{"name": "Name", "type": "string"}, {"name": "Status", "type": "string"},
		}
	}
	rs := []map[string]any{}
	for _, r := range rows { // uid, name, status
		rs = append(rs, map[string]any{
			"cells": []any{r[1], r[2]},
			"object": map[string]any{"kind": "PartialObjectMetadata", "metadata": map[string]any{
				"name": r[1], "namespace": "default", "uid": r[0], "creationTimestamp": "2026-01-01T00:00:00Z"}},
		})
	}
	t["rows"] = rs
	return t
}

type fakeAPI struct {
	srv     *httptest.Server
	watchCh chan map[string]any
	mu      sync.Mutex
	lists   int
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{watchCh: make(chan map[string]any, 16)}
	mux := http.NewServeMux()
	js := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"kind": "APIVersions", "versions": []string{"v1"}})
	})
	mux.HandleFunc("/apis", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"kind": "APIGroupList", "groups": []any{}})
	})
	mux.HandleFunc("/api/v1", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"kind": "APIResourceList", "groupVersion": "v1", "resources": []map[string]any{
			{"name": "pods", "kind": "Pod", "namespaced": true, "verbs": []string{"list", "watch", "get"}},
			{"name": "pods/log", "kind": "Pod", "namespaced": true, "verbs": []string{"get"}},
			{"name": "bindings", "kind": "Binding", "namespaced": true, "verbs": []string{"create"}},
		}})
	})
	mux.HandleFunc("/api/v1/pods", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("watch") != "true" {
			f.mu.Lock()
			f.lists++
			f.mu.Unlock()
			js(w, tableJSON("10", true, [3]string{"u1", "web", "Running"}, [3]string{"u2", "db", "Pending"}))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fl := w.(http.Flusher)
		fl.Flush()
		enc := json.NewEncoder(w)
		for {
			select {
			case ev, ok := <-f.watchCh:
				if !ok {
					return
				}
				enc.Encode(ev)
				fl.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

type collector struct {
	mu   sync.Mutex
	rows []RowsEvent
	st   []StatusEvent
	logs []LogBatch
	ends []LogEnd
}

func (c *collector) emit(name string, data any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch v := data.(type) {
	case RowsEvent:
		c.rows = append(c.rows, v)
	case StatusEvent:
		c.st = append(c.st, v)
	}
}

func (c *collector) waitRows(t *testing.T, pred func([]RowsEvent) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		ok := pred(append([]RowsEvent(nil), c.rows...))
		c.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("condition not met; events: %+v", c.rows)
}

func setup(t *testing.T) (*Manager, *fakeAPI, *collector) {
	f := newFakeAPI(t)
	col := &collector{}
	m := NewManager(func(string) (*rest.Config, error) { return &rest.Config{Host: f.srv.URL}, nil }, col.emit)
	return m, f, col
}

func TestCatalogFiltersSubresourcesAndUnlistable(t *testing.T) {
	m, _, _ := setup(t)
	rs, err := m.Connect(context.Background(), "c1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].Key != "core/v1/pods" || !rs[0].Namespaced {
		t.Fatalf("catalog: %+v", rs)
	}
}

func TestStreamSnapshotThenBatchedChanges(t *testing.T) {
	m, f, col := setup(t)
	ctx := context.Background()
	if _, err := m.Connect(ctx, "c1"); err != nil {
		t.Fatal(err)
	}
	sub, err := m.Subscribe("c1", "core/v1/pods")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Unsubscribe(sub)

	col.waitRows(t, func(e []RowsEvent) bool {
		return len(e) > 0 && e[0].Reset && len(e[0].Upsert) == 2 && len(e[0].Columns) == 2
	})

	f.watchCh <- map[string]any{"type": "MODIFIED", "object": tableJSON("11", false, [3]string{"u2", "db", "Running"})}
	f.watchCh <- map[string]any{"type": "ADDED", "object": tableJSON("12", false, [3]string{"u3", "cache", "Running"})}
	f.watchCh <- map[string]any{"type": "DELETED", "object": tableJSON("13", false, [3]string{"u1", "web", "Running"})}

	col.waitRows(t, func(e []RowsEvent) bool {
		up, del := map[string]string{}, map[string]bool{}
		for _, ev := range e[1:] {
			for _, r := range ev.Upsert {
				up[r.UID] = r.Cells[1].(string)
			}
			for _, d := range ev.Delete {
				del[d] = true
			}
		}
		return up["u2"] == "Running" && up["u3"] == "Running" && del["u1"]
	})
}

func TestUnsubscribeStopsStream(t *testing.T) {
	m, f, col := setup(t)
	m.Connect(context.Background(), "c1")
	sub, _ := m.Subscribe("c1", "core/v1/pods")
	col.waitRows(t, func(e []RowsEvent) bool { return len(e) > 0 })
	m.Unsubscribe(sub)
	time.Sleep(150 * time.Millisecond)
	col.mu.Lock()
	n := len(col.rows)
	col.mu.Unlock()
	f.watchCh <- map[string]any{"type": "ADDED", "object": tableJSON("20", false, [3]string{"u9", "late", "Running"})}
	time.Sleep(300 * time.Millisecond)
	col.mu.Lock()
	defer col.mu.Unlock()
	if len(col.rows) != n {
		t.Fatal("events delivered after unsubscribe")
	}
}

func TestGoneTriggersRelist(t *testing.T) {
	m, f, col := setup(t)
	m.Connect(context.Background(), "c1")
	sub, _ := m.Subscribe("c1", "core/v1/pods")
	defer m.Unsubscribe(sub)
	col.waitRows(t, func(e []RowsEvent) bool { return len(e) > 0 })
	f.watchCh <- map[string]any{"type": "ERROR", "object": map[string]any{"kind": "Status", "code": 410, "message": "too old"}}
	col.waitRows(t, func(e []RowsEvent) bool {
		resets := 0
		for _, ev := range e {
			if ev.Reset {
				resets++
			}
		}
		return resets >= 2
	})
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lists < 2 {
		t.Fatalf("expected relist, lists=%d", f.lists)
	}
}

func TestUnknownResource(t *testing.T) {
	m, _, _ := setup(t)
	m.Connect(context.Background(), "c1")
	if _, err := m.Subscribe("c1", "apps/v1/nothing"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := m.Subscribe("nope", "core/v1/pods"); err == nil {
		t.Fatal("expected not-connected error")
	}
	_ = fmt.Sprint
}

// A large cluster: 10k pods must list, decode and reach the UI as one snapshot quickly, and a burst of
// watch updates must be coalesced into few events instead of one event per change.
func TestStreamHandlesTenThousandPods(t *testing.T) {
	const n = 10_000
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"cells": []any{fmt.Sprintf("web-%05d", i), "1/1", "Running", 0, "3d", "10.0.0.1", "node-a", "<none>", "<none>"},
			"object": map[string]any{"kind": "PartialObjectMetadata", "metadata": map[string]any{
				"name": fmt.Sprintf("web-%05d", i), "namespace": "default", "uid": fmt.Sprintf("uid-%d", i), "creationTimestamp": "2026-01-01T00:00:00Z"}},
		})
	}
	snapshot := map[string]any{"kind": "Table", "apiVersion": "meta.k8s.io/v1", "metadata": map[string]any{"resourceVersion": "1"},
		"columnDefinitions": []map[string]any{{"name": "Name", "type": "string"}, {"name": "Ready", "type": "string"}, {"name": "Status", "type": "string"}, {"name": "Restarts", "type": "integer"}, {"name": "Age", "type": "string"}, {"name": "IP", "type": "string"}, {"name": "Node", "type": "string"}, {"name": "A", "type": "string"}, {"name": "B", "type": "string"}},
		"rows":              rows}
	body, _ := json.Marshal(snapshot)
	watch := make(chan map[string]any, 2000)
	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"kind": "APIVersions", "versions": []string{"v1"}})
	})
	mux.HandleFunc("/apis", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"kind": "APIGroupList", "groups": []any{}})
	})
	mux.HandleFunc("/api/v1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"kind": "APIResourceList", "groupVersion": "v1", "resources": []map[string]any{{"name": "pods", "kind": "Pod", "namespaced": true, "verbs": []string{"list", "watch"}}}})
	})
	mux.HandleFunc("/api/v1/pods", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("watch") != "true" {
			w.Header().Set("Content-Type", "application/json")
			w.Write(body)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fl := w.(http.Flusher)
		fl.Flush()
		enc := json.NewEncoder(w)
		for {
			select {
			case ev := <-watch:
				enc.Encode(ev)
				fl.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	col := &collector{}
	m := NewManager(func(string) (*rest.Config, error) { return &rest.Config{Host: srv.URL}, nil }, col.emit)
	if _, err := m.Connect(context.Background(), "c1"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	sub, err := m.Subscribe("c1", "core/v1/pods")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Unsubscribe(sub)
	col.waitRows(t, func(e []RowsEvent) bool { return len(e) > 0 && e[0].Reset && len(e[0].Upsert) == n })
	snap := time.Since(start)

	// 1,000 pod status changes within a few milliseconds
	before := func() int { col.mu.Lock(); defer col.mu.Unlock(); return len(col.rows) }()
	for i := 0; i < 1000; i++ {
		watch <- map[string]any{"type": "MODIFIED", "object": tableJSON(fmt.Sprint(100+i), false, [3]string{fmt.Sprintf("uid-%d", i), fmt.Sprintf("web-%05d", i), "Terminating"})}
	}
	col.waitRows(t, func(e []RowsEvent) bool {
		seen := map[string]bool{}
		for _, ev := range e[1:] {
			for _, r := range ev.Upsert {
				seen[r.UID] = true
			}
		}
		return len(seen) >= 1000
	})
	events := func() int { col.mu.Lock(); defer col.mu.Unlock(); return len(col.rows) }() - before
	t.Logf("10k pods: snapshot to UI in %s (%d KB payload); 1000 updates coalesced into %d events", snap.Round(time.Millisecond), len(body)/1024, events)
	if snap > 5*time.Second {
		t.Fatalf("snapshot took %s", snap)
	}
	if events > 100 {
		t.Fatalf("1000 updates produced %d events; batching is not working", events)
	}
}
