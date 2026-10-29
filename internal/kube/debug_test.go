package kube

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/client-go/rest"
)

type debugAPI struct {
	mu        sync.Mutex
	patch     map[string]any
	patchPath string
	reads     int
	phase     string
	waiting   string // reason reported once patched ("" = becomes running on 2nd read)
	patchCode int
	version   string
}

func newDebugSession(t *testing.T, api *debugAPI) *Session {
	pod := func() map[string]any {
		api.mu.Lock()
		defer api.mu.Unlock()
		o := map[string]any{
			"kind": "Pod", "apiVersion": "v1", "metadata": map[string]any{"name": "web", "namespace": "default"},
			"spec":   map[string]any{"containers": []any{map[string]any{"name": "app"}, map[string]any{"name": "sidecar"}}},
			"status": map[string]any{"phase": api.phase},
		}
		if api.patch != nil {
			specs := api.patch["spec"].(map[string]any)["ephemeralContainers"].([]any)
			o["spec"].(map[string]any)["ephemeralContainers"] = specs
			api.reads++
			name := specs[0].(map[string]any)["name"]
			var state map[string]any
			switch {
			case api.waiting != "":
				state = map[string]any{"waiting": map[string]any{"reason": api.waiting, "message": "pull access denied"}}
			case api.reads >= 2:
				state = map[string]any{"running": map[string]any{}}
			default:
				state = map[string]any{"waiting": map[string]any{"reason": "ContainerCreating"}}
			}
			o["status"].(map[string]any)["ephemeralContainerStatuses"] = []any{map[string]any{"name": name, "state": state}}
		}
		return o
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"gitVersion": api.version})
	})
	mux.HandleFunc("/api/v1/namespaces/default/pods/web/ephemeralcontainers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPatch {
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		if api.patchCode != 0 {
			w.WriteHeader(api.patchCode)
			json.NewEncoder(w).Encode(map[string]any{"kind": "Status", "apiVersion": "v1", "status": "Failure", "code": api.patchCode, "reason": map[int]string{403: "Forbidden", 404: "NotFound"}[api.patchCode], "message": "denied"})
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/strategic-merge-patch+json" {
			t.Errorf("content type %q", ct)
		}
		b, _ := io.ReadAll(r.Body)
		api.mu.Lock()
		api.patchPath = r.URL.Path
		json.Unmarshal(b, &api.patch)
		api.mu.Unlock()
		json.NewEncoder(w).Encode(pod())
	})
	mux.HandleFunc("/api/v1/namespaces/default/pods/web", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pod())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s, err := NewSession("c1", &rest.Config{Host: srv.URL}, func(string, any) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	debugPollEvery, debugWaitMax = 10*time.Millisecond, 600*time.Millisecond
	t.Cleanup(func() { debugPollEvery, debugWaitMax = 700*time.Millisecond, 90*time.Second })
	return s
}

func TestStartDebugInjectsEphemeralContainerAndWaitsUntilRunning(t *testing.T) {
	api := &debugAPI{phase: "Running", version: "v1.30.2"}
	s := newDebugSession(t, api)
	dc, err := s.StartDebug(context.Background(), "default", "web", DebugOptions{Image: "nicolaka/netshoot", Target: "app", Caps: true})
	if err != nil {
		t.Fatal(err)
	}
	if dc.State != "running" || !strings.HasPrefix(dc.Name, "debugger-") || dc.Image != "nicolaka/netshoot" || dc.Target != "app" {
		t.Fatalf("%+v", dc)
	}
	if api.patchPath != "/api/v1/namespaces/default/pods/web/ephemeralcontainers" {
		t.Fatalf("must use the ephemeralcontainers subresource, got %s", api.patchPath)
	}
	spec := api.patch["spec"].(map[string]any)["ephemeralContainers"].([]any)[0].(map[string]any)
	if spec["stdin"] != true || spec["tty"] != true || spec["targetContainerName"] != "app" || spec["name"] != dc.Name {
		t.Fatalf("%+v", spec)
	}
	caps := spec["securityContext"].(map[string]any)["capabilities"].(map[string]any)["add"].([]any)
	if len(caps) != 2 || caps[0] != "NET_ADMIN" || caps[1] != "SYS_PTRACE" {
		t.Fatalf("caps: %v", caps)
	}
	// existing debug containers can be listed again for re-attach
	list, _ := s.DebugContainers(context.Background(), "default", "web")
	if len(list) != 1 || list[0].Name != dc.Name || list[0].State != "running" {
		t.Fatalf("%+v", list)
	}
}

func TestStartDebugWithoutCapsOrTargetOmitsThem(t *testing.T) {
	api := &debugAPI{phase: "Running", version: "v1.30.0"}
	s := newDebugSession(t, api)
	if _, err := s.StartDebug(context.Background(), "default", "web", DebugOptions{Image: "busybox:stable"}); err != nil {
		t.Fatal(err)
	}
	spec := api.patch["spec"].(map[string]any)["ephemeralContainers"].([]any)[0].(map[string]any)
	if _, ok := spec["securityContext"]; ok {
		t.Fatal("no capabilities were requested")
	}
	if _, ok := spec["targetContainerName"]; ok {
		t.Fatal("no target was requested")
	}
}

func TestStartDebugValidation(t *testing.T) {
	api := &debugAPI{phase: "Running", version: "v1.30.0"}
	s := newDebugSession(t, api)
	ctx := context.Background()
	for _, img := range []string{"", "bad image", "-flag", "img;rm -rf /", "x$(id)"} {
		if _, err := s.StartDebug(ctx, "default", "web", DebugOptions{Image: img}); err == nil {
			t.Errorf("image %q must be rejected", img)
		}
	}
	if _, err := s.StartDebug(ctx, "default", "web", DebugOptions{Image: "busybox", Target: "ghost"}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unknown target container: %v", err)
	}
	api.phase = "Pending"
	if _, err := s.StartDebug(ctx, "default", "web", DebugOptions{Image: "busybox"}); err == nil || !strings.Contains(err.Error(), "running") {
		t.Fatalf("pending pod: %v", err)
	}
	if api.patch != nil {
		t.Fatal("nothing may be patched when validation fails")
	}
}

func TestStartDebugOldClusterAndPermissionErrors(t *testing.T) {
	old := &debugAPI{phase: "Running", version: "v1.21.5"}
	if _, err := newDebugSession(t, old).StartDebug(context.Background(), "default", "web", DebugOptions{Image: "busybox"}); err == nil || !strings.Contains(err.Error(), "1.23") {
		t.Fatalf("%v", err)
	}
	forbidden := &debugAPI{phase: "Running", version: "v1.30.0", patchCode: 403}
	if _, err := newDebugSession(t, forbidden).StartDebug(context.Background(), "default", "web", DebugOptions{Image: "busybox"}); err == nil || !strings.Contains(err.Error(), "pods/ephemeralcontainers") {
		t.Fatalf("%v", err)
	}
	missing := &debugAPI{phase: "Running", version: "v1.30.0", patchCode: 404}
	if _, err := newDebugSession(t, missing).StartDebug(context.Background(), "default", "web", DebugOptions{Image: "busybox"}); err == nil || !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("%v", err)
	}
}

func TestStartDebugReportsImagePullFailureAndTimeout(t *testing.T) {
	bad := &debugAPI{phase: "Running", version: "v1.30.0", waiting: "ImagePullBackOff"}
	_, err := newDebugSession(t, bad).StartDebug(context.Background(), "default", "web", DebugOptions{Image: "nope/nope"})
	if err == nil || !strings.Contains(err.Error(), "ImagePullBackOff") || !strings.Contains(err.Error(), "access to the image") {
		t.Fatalf("%v", err)
	}
	slow := &debugAPI{phase: "Running", version: "v1.30.0", waiting: "ContainerCreating"}
	_, err = newDebugSession(t, slow).StartDebug(context.Background(), "default", "web", DebugOptions{Image: "busybox"})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("%v", err)
	}
}

func TestDebugContainerNamesAreUnique(t *testing.T) {
	p := u(t, "spec: {containers: [{name: app}], initContainers: [{name: init}], ephemeralContainers: [{name: debugger-aaaaa}]}")
	seen := map[string]bool{"app": true, "init": true, "debugger-aaaaa": true}
	for i := 0; i < 200; i++ {
		n := debugContainerName(&p)
		if seen[n] && n != "" {
			if n == "app" || n == "init" || n == "debugger-aaaaa" {
				t.Fatalf("collided with an existing container: %s", n)
			}
		}
		if !strings.HasPrefix(n, "debugger-") || len(n) != len("debugger-")+5 {
			t.Fatalf("bad name %q", n)
		}
	}
}
