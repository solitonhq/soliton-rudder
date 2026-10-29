package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

// rbacAPI is a cluster as seen by a user who can only read some namespaces: cluster-wide lists are forbidden,
// namespaced ones work for allowed namespaces, and access reviews answer accordingly.
type rbacAPI struct {
	srv          *httptest.Server
	allowed      map[string]bool
	allNS        []string
	canListNS    bool
	ssar         bool            // access reviews available
	clusterWide  bool            // the user may do everything cluster-wide
	podsIn       map[string]int  // pods per namespace
	failNS       map[string]bool // namespaces whose lists fail with a 500
	ssarCalls    atomic.Int32
	nsListCalls  atomic.Int32
	watchStarted atomic.Int32
}

var (
	reNSPath    = regexp.MustCompile(`^/api/v1/namespaces/([^/]+)/([^/]+)$`)
	reNSGroup   = regexp.MustCompile(`^/apis/([^/]+)/([^/]+)/namespaces/([^/]+)/([^/]+)$`)
	reWide      = regexp.MustCompile(`^/api/v1/([^/]+)$`)
	reWideGroup = regexp.MustCompile(`^/apis/([^/]+)/([^/]+)/([^/]+)$`)
)

func newRBACAPI(t *testing.T, allowed []string, allNS []string, canListNS bool) *rbacAPI {
	f := &rbacAPI{allowed: map[string]bool{}, allNS: allNS, canListNS: canListNS, ssar: true, podsIn: map[string]int{}, failNS: map[string]bool{}}
	for _, n := range allowed {
		f.allowed[n] = true
		f.podsIn[n] = 2
	}
	js := func(w http.ResponseWriter, code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(v)
	}
	forbidden := func(w http.ResponseWriter, what string) {
		js(w, 403, map[string]any{"kind": "Status", "apiVersion": "v1", "status": "Failure", "reason": "Forbidden", "code": 403, "message": what + " is forbidden"})
	}
	pod := func(ns string, i int) map[string]any {
		return map[string]any{"apiVersion": "v1", "kind": "Pod",
			"metadata": map[string]any{"name": fmt.Sprintf("p%d", i), "namespace": ns, "uid": fmt.Sprintf("%s-%d", ns, i), "creationTimestamp": "2026-01-01T00:00:00Z"},
			"spec":     map[string]any{"nodeName": "n1", "containers": []any{map[string]any{"name": "c"}}},
			"status":   map[string]any{"phase": "Running", "conditions": []any{map[string]any{"type": "Ready", "status": "True"}}}}
	}
	table := func(ns string) map[string]any {
		rows := []any{}
		for i := 0; i < f.podsIn[ns]; i++ {
			p := pod(ns, i)
			rows = append(rows, map[string]any{"cells": []any{p["metadata"].(map[string]any)["name"], "Running"},
				"object": map[string]any{"kind": "PartialObjectMetadata", "metadata": p["metadata"]}})
		}
		return map[string]any{"kind": "Table", "apiVersion": "meta.k8s.io/v1", "metadata": map[string]any{"resourceVersion": "10"},
			"columnDefinitions": []any{map[string]any{"name": "Name", "type": "string"}, map[string]any{"name": "Status", "type": "string"}}, "rows": rows}
	}
	serve := func(w http.ResponseWriter, r *http.Request, ns, res string) {
		if f.failNS[ns] {
			js(w, 500, map[string]any{"kind": "Status", "apiVersion": "v1", "status": "Failure", "code": 500, "message": "boom"})
			return
		}
		if !f.clusterWide && !f.allowed[ns] {
			forbidden(w, res+" in "+ns)
			return
		}
		if r.URL.Query().Get("watch") == "true" {
			f.watchStarted.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		if strings.Contains(r.Header.Get("Accept"), "as=Table") {
			js(w, 200, table(ns))
			return
		}
		items := []any{}
		if res == "pods" {
			for i := 0; i < f.podsIn[ns]; i++ {
				items = append(items, pod(ns, i))
			}
		}
		js(w, 200, map[string]any{"kind": "List", "apiVersion": "v1", "metadata": map[string]any{}, "items": items})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/version":
			js(w, 200, map[string]string{"gitVersion": "v1.30.1"})
		case p == "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews" && r.Method == http.MethodPost:
			f.ssarCalls.Add(1)
			if !f.ssar {
				js(w, 404, map[string]any{"kind": "Status", "apiVersion": "v1", "status": "Failure", "reason": "NotFound", "code": 404})
				return
			}
			var body struct {
				Spec struct {
					ResourceAttributes struct{ Namespace, Verb, Resource string } `json:"resourceAttributes"`
				} `json:"spec"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			ra := body.Spec.ResourceAttributes
			ok := f.clusterWide
			switch {
			case ra.Resource == "namespaces" && ra.Namespace == "":
				ok = ok || f.canListNS
			case ra.Namespace != "":
				ok = ok || f.allowed[ra.Namespace]
			}
			js(w, 201, map[string]any{"kind": "SelfSubjectAccessReview", "apiVersion": "authorization.k8s.io/v1", "status": map[string]any{"allowed": ok}})
		case p == "/api/v1/namespaces":
			f.nsListCalls.Add(1)
			if !f.clusterWide && !f.canListNS {
				forbidden(w, "namespaces")
				return
			}
			items := []any{}
			for _, n := range f.allNS {
				items = append(items, map[string]any{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": n}})
			}
			js(w, 200, map[string]any{"kind": "NamespaceList", "apiVersion": "v1", "items": items})
		case reNSPath.MatchString(p):
			m := reNSPath.FindStringSubmatch(p)
			serve(w, r, m[1], m[2])
		case reNSGroup.MatchString(p):
			m := reNSGroup.FindStringSubmatch(p)
			serve(w, r, m[3], m[4])
		case reWide.MatchString(p) || reWideGroup.MatchString(p):
			res := p[strings.LastIndex(p, "/")+1:]
			if res == "nodes" || strings.HasPrefix(p, "/apis/metrics.k8s.io") {
				js(w, 404, map[string]any{"kind": "Status", "apiVersion": "v1", "status": "Failure", "reason": "NotFound", "code": 404})
				return
			}
			if f.clusterWide {
				items := []any{}
				if res == "pods" {
					for ns, n := range f.podsIn {
						for i := 0; i < n; i++ {
							items = append(items, pod(ns, i))
						}
					}
				}
				js(w, 200, map[string]any{"kind": "List", "apiVersion": "v1", "items": items})
				return
			}
			forbidden(w, res+" at the cluster scope")
		default:
			http.NotFound(w, r)
		}
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *rbacAPI) session(t *testing.T, hooks ScopeHooks, emit Emitter) *Session {
	t.Helper()
	if emit == nil {
		emit = func(string, any) {}
	}
	s, err := NewSession("c1", &rest.Config{Host: f.srv.URL}, emit)
	if err != nil {
		t.Fatal(err)
	}
	s.SetScopeHooks(hooks)
	t.Cleanup(s.Close)
	return s
}

func ctx5(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestAccessFullClusterAccess(t *testing.T) {
	f := newRBACAPI(t, nil, []string{"a", "b"}, true)
	f.clusterWide = true
	a := f.session(t, ScopeHooks{}, nil).Access(ctx5(t))
	if !a.ClusterWide || a.Source != "cluster" || !a.CanListNamespaces {
		t.Errorf("%+v", a)
	}
}

func TestAccessDiscoversTheNamespacesAUserCanRead(t *testing.T) {
	f := newRBACAPI(t, []string{"team-a", "team-b"}, []string{"kube-system", "other", "team-a", "team-b"}, true)
	s := f.session(t, ScopeHooks{}, nil)
	a := s.Access(ctx5(t))
	if a.ClusterWide || a.Source != "discovered" || fmt.Sprint(a.Namespaces) != "[team-a team-b]" || !a.CanListNamespaces {
		t.Fatalf("%+v", a)
	}
	// the answer is cached: no new reviews on the next call
	n := f.ssarCalls.Load()
	s.Access(ctx5(t))
	if f.ssarCalls.Load() != n {
		t.Error("access must be resolved once, not on every call")
	}
	s.RefreshAccess()
	s.Access(ctx5(t))
	if f.ssarCalls.Load() == n {
		t.Error("RefreshAccess must re-check")
	}
}

func TestAccessConfiguredNamespacesWinThenKubeconfigThenNone(t *testing.T) {
	f := newRBACAPI(t, []string{"team-a"}, []string{"team-a", "team-b"}, true)
	a := f.session(t, ScopeHooks{Configured: func() []string { return []string{" team-b ", "team-b", "team-c"} }}, nil).Access(ctx5(t))
	if a.Source != "configured" || fmt.Sprint(a.Namespaces) != "[team-b team-c]" {
		t.Errorf("what the user chose is used as given: %+v", a)
	}
	g := newRBACAPI(t, []string{"mine"}, nil, false) // cannot list namespaces
	a = g.session(t, ScopeHooks{DefaultNS: func() string { return "mine" }}, nil).Access(ctx5(t))
	if a.Source != "kubeconfig" || fmt.Sprint(a.Namespaces) != "[mine]" || a.CanListNamespaces {
		t.Errorf("%+v", a)
	}
	a = g.session(t, ScopeHooks{}, nil).Access(ctx5(t))
	if a.Source != "none" || len(a.Namespaces) != 0 || !strings.Contains(a.Note, "access settings") {
		t.Errorf("nothing to go on: say what to do: %+v", a)
	}
}

func TestAccessFallsBackToARealRequestWhenReviewsAreUnavailable(t *testing.T) {
	f := newRBACAPI(t, []string{"team-a"}, []string{"team-a"}, false)
	f.ssar = false
	a := f.session(t, ScopeHooks{DefaultNS: func() string { return "team-a" }}, nil).Access(ctx5(t))
	if a.ClusterWide || a.Source != "kubeconfig" {
		t.Errorf("a forbidden cluster-wide list means restricted: %+v", a)
	}
	g := newRBACAPI(t, nil, nil, false)
	g.ssar, g.clusterWide = false, true
	if a := g.session(t, ScopeHooks{}, nil).Access(ctx5(t)); !a.ClusterWide {
		t.Errorf("%+v", a)
	}
}

func TestAccessAssumesFullAccessWhenPermissionsCannotBeChecked(t *testing.T) {
	f := newRBACAPI(t, nil, nil, false)
	s := f.session(t, ScopeHooks{}, nil)
	f.srv.Close() // unreachable
	a := s.Access(ctx5(t))
	if !a.ClusterWide || !strings.Contains(a.Note, "could not be checked") {
		t.Errorf("trouble reaching the API must not be mistaken for restricted access: %+v", a)
	}
	s.accMu.Lock()
	cached := s.acc != nil
	s.accMu.Unlock()
	if cached {
		t.Error("an uncertain answer must not be cached")
	}
}

func TestListScopedMergesNamespacesAndSkipsForbiddenOnes(t *testing.T) {
	f := newRBACAPI(t, []string{"team-a", "team-b"}, []string{"team-a", "team-b"}, true)
	s := f.session(t, ScopeHooks{Configured: func() []string { return []string{"team-a", "team-b", "secret-ns"} }}, nil)
	items, err := s.ListScoped(ctx5(t), podRes, "", metav1ListOptions())
	if err != nil || len(items) != 4 {
		t.Fatalf("a forbidden namespace must not hide the others: %v %d", err, len(items))
	}
	// an explicit namespace is queried alone
	items, _ = s.ListScoped(ctx5(t), podRes, "team-a", metav1ListOptions())
	if len(items) != 2 {
		t.Errorf("%d", len(items))
	}
	// everything forbidden is an error, not an empty list
	h := newRBACAPI(t, nil, nil, false)
	if _, err := h.session(t, ScopeHooks{Configured: func() []string { return []string{"x", "y"} }}, nil).ListScoped(ctx5(t), podRes, "", metav1ListOptions()); err == nil {
		t.Error("expected the forbidden error")
	}
	// strict: a namespace that fails for another reason fails the whole list
	f.failNS["team-b"] = true
	if _, err := s.ListScopedStrict(ctx5(t), podRes, "", metav1ListOptions()); err == nil {
		t.Error("the recorder must not see a half-read list as a complete one")
	}
	if items, err := s.ListScoped(ctx5(t), podRes, "", metav1ListOptions()); err != nil || len(items) != 2 {
		t.Errorf("the lenient list keeps what it could read: %v %d", err, len(items))
	}
}

func TestListRawIsScopedToo(t *testing.T) {
	f := newRBACAPI(t, []string{"team-a", "team-b"}, []string{"team-a", "team-b"}, true)
	s := f.session(t, ScopeHooks{}, nil)
	items, err := s.ListRaw(ctx5(t), "core", "v1", "pods", true)
	if err != nil || len(items) != 4 {
		t.Fatalf("features built on ListRaw (assessments, Argo, Flux…) get the same treatment: %v %d", err, len(items))
	}
}

func TestOverviewWorksForAUserWithLimitedAccess(t *testing.T) {
	f := newRBACAPI(t, []string{"team-a", "team-b"}, []string{"kube-system", "team-a", "team-b"}, true)
	s := f.session(t, ScopeHooks{}, nil)
	o, err := s.ClusterOverview(ctx5(t))
	if err != nil {
		t.Fatal(err)
	}
	if o.Pods.Total != 4 || o.Pods.Ready != 4 {
		t.Errorf("pods %+v", o.Pods)
	}
	if o.Namespaces != 2 || o.Access.ClusterWide || fmt.Sprint(o.Access.Namespaces) != "[team-a team-b]" {
		t.Errorf("only the namespaces this user can see are counted: %d %+v", o.Namespaces, o.Access)
	}
	if !containsStr(o.Notes, "Limited access: figures cover only the namespaces you can read.") {
		t.Errorf("notes: %v", o.Notes)
	}
	for _, n := range o.Notes {
		if strings.Contains(n, "nodes unavailable") {
			t.Errorf("nodes are cluster-scoped: not being able to read them is expected, not a problem to report: %v", o.Notes)
		}
	}
}

func TestTopologyAndIngressWorkForALimitedUser(t *testing.T) {
	f := newRBACAPI(t, []string{"team-a"}, []string{"team-a"}, true)
	s := f.session(t, ScopeHooks{}, nil)
	topo, err := s.Topology(ctx5(t), "")
	if err != nil {
		t.Fatal(err)
	}
	pods := 0
	for _, n := range topo.Nodes {
		if n.Kind == "Pod" {
			pods++
		}
	}
	if pods != 2 {
		t.Errorf("pods in the topology: %d", pods)
	}
	if _, err := s.IngressMap(ctx5(t), ""); err != nil {
		t.Errorf("ingress map: %v", err)
	}
}

func TestNamespacesFallBackToTheUsersOwnWhenTheyCannotBeListed(t *testing.T) {
	f := newRBACAPI(t, []string{"mine"}, nil, false)
	s := f.session(t, ScopeHooks{DefaultNS: func() string { return "mine" }}, nil)
	ns, err := s.Namespaces(ctx5(t))
	if err != nil || fmt.Sprint(ns) != "[mine]" {
		t.Errorf("%v %v", ns, err)
	}
}

func TestTableStreamMergesNamespacesWithoutThemWipingEachOther(t *testing.T) {
	f := newRBACAPI(t, []string{"team-a", "team-b"}, []string{"team-a", "team-b", "secret"}, true)
	col := &collector{}
	s := f.session(t, ScopeHooks{Configured: func() []string { return []string{"team-a", "team-b", "secret"} }}, col.emit)
	s.AddCatalogForTest("core/v1/pods", podRes)
	if _, err := s.Subscribe("core/v1/pods"); err != nil {
		t.Fatal(err)
	}
	col.waitRows(t, func(ev []RowsEvent) bool {
		seen := map[string]bool{}
		for _, e := range ev {
			if !e.Reset || e.Scope == "" {
				continue
			}
			seen[e.Scope] = len(e.Upsert) == 2
		}
		return seen["team-a"] && seen["team-b"]
	})
	col.mu.Lock()
	defer col.mu.Unlock()
	for _, e := range col.rows {
		if e.Reset && e.Scope == "" {
			t.Fatalf("a multi-namespace snapshot must say which namespace it replaces: %+v", e)
		}
		if e.Scope == "secret" {
			t.Errorf("a forbidden namespace produces no rows")
		}
	}
	for _, st := range col.st {
		if st.State == "error" {
			t.Errorf("one forbidden namespace must not turn the whole table into an error: %+v", st)
		}
	}
}

func TestTableStreamReportsAnErrorWhenNothingIsReadable(t *testing.T) {
	f := newRBACAPI(t, nil, nil, false)
	col := &collector{}
	s := f.session(t, ScopeHooks{Configured: func() []string { return []string{"x", "y"} }}, col.emit)
	s.AddCatalogForTest("core/v1/pods", podRes)
	if _, err := s.Subscribe("core/v1/pods"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		col.mu.Lock()
		for _, st := range col.st {
			if st.State == "error" && strings.Contains(st.Message, "do not have permission to list pods") {
				col.mu.Unlock()
				return
			}
		}
		col.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	col.mu.Lock()
	t.Fatalf("expected a forbidden error status; got %+v", col.st)
}

func TestFullAccessStillStreamsOneClusterWideTable(t *testing.T) {
	f := newRBACAPI(t, []string{"a", "b"}, []string{"a", "b"}, true)
	f.clusterWide = true
	col := &collector{}
	s := f.session(t, ScopeHooks{}, col.emit)
	s.AddCatalogForTest("core/v1/pods", podRes)
	if _, err := s.Subscribe("core/v1/pods"); err != nil {
		t.Fatal(err)
	}
	col.waitRows(t, func(ev []RowsEvent) bool { return len(ev) > 0 })
	col.mu.Lock()
	defer col.mu.Unlock()
	if e := col.rows[0]; !e.Reset || e.Scope != "" {
		t.Errorf("with full access nothing changes: %+v", e)
	}
}

func containsStr(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func metav1ListOptions() metav1.ListOptions { return metav1.ListOptions{} }

func TestPluginListObjectsHonoursNamespaceLimitsAndTheLimit(t *testing.T) {
	f := newRBACAPI(t, []string{"team-a", "team-b"}, []string{"team-a", "team-b"}, true)
	s := f.session(t, ScopeHooks{}, nil)
	objs, more, err := s.ListObjects(ctx5(t), podRes, "", 3)
	if err != nil || len(objs) != 3 || !more {
		t.Fatalf("4 pods in two namespaces, limit 3: %v %d %v", err, len(objs), more)
	}
	objs, more, _ = s.ListObjects(ctx5(t), podRes, "", 10)
	if len(objs) != 4 || more {
		t.Errorf("%d %v", len(objs), more)
	}
}
