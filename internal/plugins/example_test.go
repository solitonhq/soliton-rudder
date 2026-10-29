package plugins

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/client-go/rest"

	"soliton-rudder/internal/kube"
)

// exampleHost serves a tiny fake API server so the shipped example plugin can run end to end.
type exampleHost struct {
	fakeHost
	sess *kube.Session
}

func (h *exampleHost) Session(string) (*kube.Session, error) { return h.sess, nil }

func TestShippedExamplePluginEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	js := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/namespaces", func(w http.ResponseWriter, r *http.Request) {
		ns := func(name string, labels map[string]string) map[string]any {
			return map[string]any{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": name, "labels": labels}}
		}
		js(w, map[string]any{"kind": "NamespaceList", "apiVersion": "v1", "items": []any{ns("shop", map[string]string{"owner": "team-a"}), ns("legacy", nil), ns("kube-system", nil)}})
	})
	mux.HandleFunc("/api/v1/pods", func(w http.ResponseWriter, r *http.Request) {
		pod := func(ns string) map[string]any {
			return map[string]any{"apiVersion": "v1", "kind": "Pod", "metadata": map[string]any{"name": "p", "namespace": ns}}
		}
		js(w, map[string]any{"kind": "PodList", "apiVersion": "v1", "items": []any{pod("shop"), pod("shop"), pod("legacy")}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	sess, err := kube.NewSession("c1", &rest.Config{Host: srv.URL}, func(string, any) {})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	h := &exampleHost{sess: sess}
	m, err := NewManager(filepath.Join(t.TempDir(), "plugins"), "test", h, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Shutdown()
	src := filepath.Join("..", "..", "examples", "plugins", "namespace-report")
	ins, err := m.Inspect(src)
	if err != nil || ins.Manifest.ID != "io.soliton.namespace-report" || len(ins.Permissions) != 3 {
		t.Fatalf("%v %+v", err, ins)
	}
	if _, err := m.Install(src, []string{"clusters.read", "kube.read", "ui.notify"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, "io.soliton.namespace-report", StateRunning)
	ctx := context.Background()
	id := "io.soliton.namespace-report"

	msg, err := m.RunCommand(ctx, id, "scan", json.RawMessage(`{"clusterId":"c1"}`))
	if err != nil || msg != "3 namespaces, 1 without an owner label" {
		t.Fatalf("%v %q", err, msg)
	}
	out, err := m.UICall(ctx, id, "plugin.call", json.RawMessage(`{"method":"summary","params":{"clusterId":"c1"}}`))
	if err != nil || !strings.Contains(string(out.(json.RawMessage)), `"name":"shop","pods":2,"owner":"team-a"`) {
		t.Fatalf("%v %s", err, out)
	}
	rules := m.AssessRules(ctx, "c1")
	if len(rules) != 1 || rules[0].Error != "" || rules[0].Checked != 2 || len(rules[0].Findings) != 1 || rules[0].Findings[0].Name != "legacy" {
		t.Fatalf("%+v", rules)
	}
	// without kube.read the same plugin is refused by the host
	if err := m.SetGranted(id, []string{"ui.notify"}); err != nil {
		t.Fatal(err)
	}
	rules = m.AssessRules(ctx, "c1")
	if rules[0].Error == "" || !strings.Contains(rules[0].Error, "permission denied") {
		t.Fatalf("expected permission error: %+v", rules[0])
	}
}
