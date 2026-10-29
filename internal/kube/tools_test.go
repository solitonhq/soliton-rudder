package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/client-go/rest"
)

func TestSplitTimestamp(t *testing.T) {
	l := splitTimestamp("2026-01-01T10:00:00.123456789Z hello world")
	if l.TS != "2026-01-01T10:00:00.123456789Z" || l.Text != "hello world" {
		t.Fatalf("%+v", l)
	}
	if l := splitTimestamp("no timestamp here"); l.TS != "" || l.Text != "no timestamp here" {
		t.Fatalf("%+v", l)
	}
}

func TestParseManifest(t *testing.T) {
	docs, err := ParseManifest("---\napiVersion: v1\nkind: ConfigMap\nmetadata: {name: a}\n---\n\n---\napiVersion: v1\nkind: List\nitems:\n- {apiVersion: v1, kind: Secret, metadata: {name: b}}\n- {apiVersion: v1, kind: Secret, metadata: {name: c}}\n")
	if err != nil || len(docs) != 3 {
		t.Fatalf("%v %d", err, len(docs))
	}
	if _, err := ParseManifest("foo: bar"); err == nil {
		t.Fatal("missing kind must fail")
	}
	if _, err := ParseManifest("  \n"); err == nil {
		t.Fatal("empty must fail")
	}
	if _, err := ParseManifest("a: [unterminated"); err == nil {
		t.Fatal("bad yaml must fail")
	}
}

func newToolsServer(t *testing.T) (*Session, *collector) {
	js := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}
	var mu sync.Mutex
	stored := map[string]map[string]any{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"kind": "APIVersions", "versions": []string{"v1"}})
	})
	mux.HandleFunc("/apis", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"kind": "APIGroupList", "groups": []any{}})
	})
	mux.HandleFunc("/api/v1", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"kind": "APIResourceList", "groupVersion": "v1", "resources": []map[string]any{
			{"name": "configmaps", "kind": "ConfigMap", "namespaced": true, "verbs": []string{"get", "list", "watch", "patch"}},
		}})
	})
	mux.HandleFunc("/api/v1/namespaces/default/pods/web/log", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "2026-01-01T10:00:00.000000000Z first\n2026-01-01T10:00:01.000000000Z second\npartial-no-newline")
	})
	mux.HandleFunc("/api/v1/namespaces/", func(w http.ResponseWriter, r *http.Request) {
		const prefix = "/api/v1/namespaces/default/configmaps/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, prefix)
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			if obj, ok := stored[name]; ok {
				js(w, obj)
				return
			}
			w.WriteHeader(404)
			js(w, map[string]any{"kind": "Status", "apiVersion": "v1", "status": "Failure", "reason": "NotFound", "code": 404})
		case http.MethodPatch:
			var obj map[string]any
			json.NewDecoder(r.Body).Decode(&obj)
			obj["metadata"].(map[string]any)["resourceVersion"] = "9"
			if r.URL.Query().Get("dryRun") == "" {
				stored[name] = obj
			}
			js(w, obj)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	col := &collector{}
	s, err := NewSession("c1", &rest.Config{Host: srv.URL}, func(ev string, d any) {
		col.emit(ev, d)
		col.mu.Lock()
		defer col.mu.Unlock()
		switch v := d.(type) {
		case LogBatch:
			col.logs = append(col.logs, v)
		case LogEnd:
			col.ends = append(col.ends, v)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, col
}

func TestPreviewThenApplyManifest(t *testing.T) {
	s, _ := newToolsServer(t)
	ctx := context.Background()
	doc := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cfg\ndata:\n  a: \"1\"\n"

	prev, err := s.PreviewManifest(ctx, doc, "default", false)
	if err != nil || len(prev) != 1 {
		t.Fatalf("%v %+v", err, prev)
	}
	if prev[0].Action != "create" || prev[0].Before != "" || !strings.Contains(prev[0].After, "a: \"1\"") {
		t.Fatalf("preview: %+v", prev[0])
	}
	// dry-run must not persist
	again, _ := s.PreviewManifest(ctx, doc, "default", false)
	if again[0].Action != "create" {
		t.Fatal("dry-run persisted")
	}

	res, _ := s.ApplyManifest(ctx, doc, "default", false)
	if res[0].Action != "create" || res[0].Error != "" {
		t.Fatalf("apply: %+v", res[0])
	}
	res, _ = s.PreviewManifest(ctx, doc, "default", false)
	if res[0].Action != "unchanged" {
		t.Fatalf("expected unchanged, got %+v", res[0])
	}
	changed := strings.Replace(doc, `"1"`, `"2"`, 1)
	res, _ = s.PreviewManifest(ctx, changed, "default", false)
	if res[0].Action != "configure" || res[0].Before == res[0].After {
		t.Fatalf("expected configure: %+v", res[0])
	}
}

func TestApplyUnknownKindIsPerDocumentError(t *testing.T) {
	s, _ := newToolsServer(t)
	doc := "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: ok}\n---\napiVersion: nope.io/v1\nkind: Widget\nmetadata: {name: w}\n"
	res, err := s.PreviewManifest(context.Background(), doc, "default", false)
	if err != nil || len(res) != 2 {
		t.Fatalf("%v %+v", err, res)
	}
	if res[0].Action != "create" || res[1].Action != "error" || res[1].Error == "" {
		t.Fatalf("%+v", res)
	}
}

func TestLogStreamBatchesAndEnds(t *testing.T) {
	s, col := newToolsServer(t)
	id, err := s.StreamLogs(LogOptions{Namespace: "default", Pod: "web", Container: "app", TailLines: 100})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		col.mu.Lock()
		done := len(col.ends) > 0
		col.mu.Unlock()
		if done {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	col.mu.Lock()
	defer col.mu.Unlock()
	if len(col.ends) != 1 || col.ends[0].Sub != id || col.ends[0].Error != "" {
		t.Fatalf("ends: %+v", col.ends)
	}
	var lines []LogLine
	for _, b := range col.logs {
		if b.Pod != "web" || b.Container != "app" {
			t.Fatalf("batch tagging: %+v", b)
		}
		lines = append(lines, b.Lines...)
	}
	if len(lines) != 3 || lines[0].Text != "first" || lines[1].TS == "" || lines[2].Text != "partial-no-newline" {
		t.Fatalf("lines: %+v", lines)
	}
	if _, err := s.StreamLogs(LogOptions{}); err == nil {
		t.Fatal("missing pod must fail")
	}
}
