package feasibility

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/client-go/rest"

	"soliton-rudder/internal/kube"
)

func fakeCluster(t *testing.T) *kube.Session {
	t.Helper()
	js := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) { js(w, map[string]string{"gitVersion": "v1.34.1"}) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/apis/metrics.k8s.io") {
			w.WriteHeader(404)
			js(w, map[string]any{"kind": "Status", "apiVersion": "v1", "status": "Failure", "reason": "NotFound", "code": 404})
			return
		}
		js(w, map[string]any{"kind": "List", "apiVersion": "v1", "items": []any{}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s, err := kube.NewSession("c1", &rest.Config{Host: srv.URL}, func(string, any) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

type doneCollector struct {
	mu    sync.Mutex
	dones []Done
}

func (c *doneCollector) emit(name string, data any) {
	if name != EventDone {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dones = append(c.dones, data.(Done))
}

func (c *doneCollector) wait(t *testing.T) Done {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		if len(c.dones) > 0 {
			d := c.dones[0]
			c.mu.Unlock()
			return d
		}
		c.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the run never finished")
	return Done{}
}

func TestSuccessfulRunReportsTheSavedReportNotCancelled(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "f.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sess := fakeCluster(t)
	col := &doneCollector{}
	svc := NewService(store, func(string) (*kube.Session, error) { return sess, nil }, func(string) string { return "c1" }, col.emit)

	runID, err := svc.Start("c1")
	if err != nil {
		t.Fatal(err)
	}
	d := col.wait(t)
	if d.Error != "" {
		t.Fatalf("a successful run must not be reported as failed or cancelled: %q", d.Error)
	}
	if d.Report != runID || d.Run != runID {
		t.Fatalf("done must name the saved report: %+v (run %s)", d, runID)
	}
	if list, _ := store.List("c1"); len(list) != 1 || list[0].ID != runID {
		t.Fatalf("the report must be in the history: %+v", list)
	}
	if svc.Running("c1") != "" {
		t.Error("the run must be cleared")
	}
}

func TestCancelledRunIsReportedAsCancelled(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "f.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// a cluster that hangs on every request, so the run can only end by cancellation
	hang := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-hang:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(hang); srv.Close() })
	sess, err := kube.NewSession("c1", &rest.Config{Host: srv.URL}, func(string, any) {})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	col := &doneCollector{}
	svc := NewService(store, func(string) (*kube.Session, error) { return sess, nil }, func(string) string { return "c1" }, col.emit)
	if _, err := svc.Start("c1"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	svc.Cancel("c1")
	if d := col.wait(t); d.Error != "cancelled" || d.Report != "" {
		t.Fatalf("%+v", d)
	}
	if list, _ := store.List("c1"); len(list) != 0 {
		t.Fatalf("a cancelled run saves nothing: %+v", list)
	}
}
