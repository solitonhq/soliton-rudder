package blackbox

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"soliton-rudder/internal/kube"
)

func st(kind, ns, name, status, detail string) kube.ObservedState {
	return kube.ObservedState{Key: kind + "/" + ns + "/" + name, Kind: kind, Namespace: ns, Name: name, Status: status, Detail: detail}
}

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestDiffWritesOnlyChanges(t *testing.T) {
	cur := []kube.ObservedState{st("Pod", "shop", "a", "ok", "Running"), st("Pod", "shop", "b", "ok", "Running")}
	recs, prev := diffStates(nil, cur, nil, 1000)
	if len(recs) != 2 || recs[0].Prev != "" || recs[0].Status != "ok" {
		t.Fatalf("baseline: %+v", recs)
	}
	// nothing changed: nothing written
	if recs, _ = diffStates(prev, cur, nil, 2000); len(recs) != 0 {
		t.Fatalf("quiet cluster must cost nothing: %+v", recs)
	}
	// b crashes, a is deleted, c appears
	cur2 := []kube.ObservedState{st("Pod", "shop", "b", "error", "CrashLoopBackOff · 3 restarts"), st("Pod", "shop", "c", "warn", "Pending")}
	recs, _ = diffStates(prev, cur2, nil, 3000)
	by := map[string]Record{}
	for _, r := range recs {
		by[r.Name] = r
	}
	if r := by["b"]; r.Status != "error" || r.Severity != "error" || r.Prev != "ok: Running" || r.Reason != "CrashLoopBackOff · 3 restarts" {
		t.Errorf("b: %+v", r)
	}
	if r := by["a"]; r.Status != "gone" || r.Prev != "ok: Running" {
		t.Errorf("a: %+v", r)
	}
	if r := by["c"]; r.Severity != "warn" || r.Prev != "" {
		t.Errorf("c: %+v", r)
	}
}

func TestDiffUnreadKindIsUnknownNotDeleted(t *testing.T) {
	prev := map[string]snap{"Node//n1": {"ok", "Ready", "Node", "", "n1"}, "Pod/a/x": {"ok", "Running", "Pod", "a", "x"}}
	recs, next := diffStates(prev, []kube.ObservedState{st("Pod", "a", "x", "ok", "Running")}, map[string]bool{"Node": true}, 5)
	if len(recs) != 0 {
		t.Fatalf("an unreadable node list must not record deletions: %+v", recs)
	}
	if _, ok := next["Node//n1"]; !ok {
		t.Error("the node must stay remembered")
	}
	// when it is readable and really gone, it is recorded
	recs, _ = diffStates(prev, []kube.ObservedState{st("Pod", "a", "x", "ok", "Running")}, nil, 6)
	if len(recs) != 1 || recs[0].Status != "gone" {
		t.Fatalf("%+v", recs)
	}
}

func TestEventRecordsRespectWindowAndSeverity(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	evs := []kube.ObservedEvent{
		{UID: "u1", Count: 2, Namespace: "shop", Kind: "Pod", Name: "a", Type: "Warning", Reason: "BackOff", Message: "m", At: now.Add(-time.Minute)},
		{UID: "u2", Count: 1, Namespace: "shop", Kind: "Pod", Name: "a", Type: "Normal", Reason: "Killing", At: now.Add(-time.Minute)},
		{UID: "u3", Count: 1, Type: "Warning", Reason: "Old", At: now.Add(-48 * time.Hour)},
	}
	recs := eventRecords(evs, now.Add(-24*time.Hour).UnixMilli())
	if len(recs) != 2 || recs[0].Key != "u1#2" || recs[0].Severity != "warn" || recs[1].Severity != "info" {
		t.Fatalf("%+v", recs)
	}
}

func TestStoreStateAtReconstructsThePast(t *testing.T) {
	s := openTemp(t)
	c := "c1"
	mk := func(ts int64, name, status, reason string) Record {
		return Record{TS: ts, Kind: "state", Key: "Pod/shop/" + name, ObjKind: "Pod", Namespace: "shop", Name: name, Status: status, Severity: severityOf(status), Reason: reason}
	}
	if err := s.Add(c, []Record{
		mk(100, "a", "ok", "Running"), mk(100, "b", "ok", "Running"),
		mk(200, "b", "error", "CrashLoopBackOff"), mk(300, "a", "gone", "deleted"), mk(400, "b", "ok", "Running"),
	}); err != nil {
		t.Fatal(err)
	}
	at := func(ts int64) map[string]string {
		rs, err := s.StateAt(c, ts)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]string{}
		for _, r := range rs {
			m[r.Name] = r.Status
		}
		return m
	}
	if m := at(150); m["a"] != "ok" || m["b"] != "ok" || len(m) != 2 {
		t.Errorf("t=150 %v", m)
	}
	if m := at(250); m["b"] != "error" || m["a"] != "ok" {
		t.Errorf("t=250 %v", m)
	}
	if m := at(350); len(m) != 1 || m["b"] != "error" {
		t.Errorf("t=350 a was deleted: %v", m)
	}
	if m := at(500); m["b"] != "ok" {
		t.Errorf("t=500 %v", m)
	}
	if m := at(50); len(m) != 0 {
		t.Errorf("before the recording started nothing is known: %v", m)
	}
	last, _ := s.LastStates(c)
	if len(last) != 1 || last[0].Name != "b" {
		t.Errorf("LastStates %+v", last)
	}
}

func TestStoreEventsAreDeduplicatedAndQueryable(t *testing.T) {
	s := openTemp(t)
	ev := Record{TS: 10, Kind: "event", Key: "u1#1", ObjKind: "Pod", Namespace: "shop", Name: "a", Severity: "warn", Reason: "BackOff", Message: "restarting 100%_x"}
	for i := 0; i < 3; i++ {
		if err := s.Add("c", []Record{ev}); err != nil {
			t.Fatal(err)
		}
	}
	ev2 := ev
	ev2.Key, ev2.TS, ev2.Severity = "u1#2", 20, "info"
	_ = s.Add("c", []Record{ev2, {TS: 30, Kind: "state", Key: "Pod/shop/a", Status: "ok", Severity: "info", Namespace: "other", Name: "a"}})
	all, _ := s.Query("c", Query{})
	if len(all) != 3 {
		t.Fatalf("same uid+count must be stored once: %d", len(all))
	}
	if r, _ := s.Query("c", Query{Kinds: []string{"event"}, MinSeverity: "warn"}); len(r) != 1 {
		t.Errorf("severity filter %+v", r)
	}
	if r, _ := s.Query("c", Query{Text: "100%_x"}); len(r) != 2 {
		t.Errorf("LIKE wildcards are escaped, so a literal percent sign matches: %d", len(r))
	}
	if r, _ := s.Query("c", Query{Text: "10_x"}); len(r) != 0 {
		t.Errorf("underscore must not act as a wildcard: %d", len(r))
	}
	if r, _ := s.Query("c", Query{Namespace: "other"}); len(r) != 1 {
		t.Errorf("namespace filter %+v", r)
	}
	if r, _ := s.Query("c", Query{From: 15, To: 25, Newest: true}); len(r) != 1 || r[0].TS != 20 {
		t.Errorf("window %+v", r)
	}
	if r, _ := s.Query("other-cluster", Query{}); len(r) != 0 {
		t.Error("clusters are isolated")
	}
}

func TestPruneKeepsTheLatestStateSoThePastStaysReconstructable(t *testing.T) {
	s := openTemp(t)
	c := "c"
	_ = s.Add(c, []Record{
		{TS: 10, Kind: "state", Key: "Pod/x/steady", ObjKind: "Pod", Name: "steady", Status: "ok", Severity: "info"},
		{TS: 10, Kind: "state", Key: "Pod/x/flaky", ObjKind: "Pod", Name: "flaky", Status: "ok", Severity: "info"},
		{TS: 20, Kind: "state", Key: "Pod/x/flaky", ObjKind: "Pod", Name: "flaky", Status: "error", Severity: "error"},
		{TS: 20, Kind: "state", Key: "Pod/x/dead", ObjKind: "Pod", Name: "dead", Status: "ok", Severity: "info"},
		{TS: 25, Kind: "state", Key: "Pod/x/dead", ObjKind: "Pod", Name: "dead", Status: "gone", Severity: "info"},
		{TS: 22, Kind: "event", Key: "e#1", Severity: "warn", Reason: "Old"},
		{TS: 100, Kind: "event", Key: "e#2", Severity: "warn", Reason: "New"},
	})
	if err := s.Prune(c, 50, 0); err != nil {
		t.Fatal(err)
	}
	ev, _ := s.Query(c, Query{Kinds: []string{"event"}})
	if len(ev) != 1 || ev[0].Reason != "New" {
		t.Errorf("old events are dropped: %+v", ev)
	}
	rs, _ := s.StateAt(c, 60)
	m := map[string]string{}
	for _, r := range rs {
		m[r.Name] = r.Status
	}
	if len(m) != 2 || m["steady"] != "ok" || m["flaky"] != "error" {
		t.Errorf("the last known state of live objects survives pruning, deleted ones do not: %v", m)
	}
	sp, _ := s.Span(c)
	if sp.Min != 50 {
		t.Errorf("retained baselines are moved to the cutoff so the range does not lie about its start: %+v", sp)
	}
}

func TestPruneMaxRows(t *testing.T) {
	s := openTemp(t)
	var rs []Record
	for i := 1; i <= 100; i++ {
		rs = append(rs, Record{TS: int64(i), Kind: "event", Key: string(rune('a'+i%26)) + string(rune('A'+i/26)) + "#1", Severity: "warn"})
	}
	_ = s.Add("c", rs)
	if err := s.Prune("c", 0, 40); err != nil {
		t.Fatal(err)
	}
	sp, _ := s.Span("c")
	if sp.Count > 41 || sp.Count < 39 {
		t.Errorf("row cap not enforced: %+v", sp)
	}
}

func TestBuckets(t *testing.T) {
	s := openTemp(t)
	_ = s.Add("c", []Record{
		{TS: 0, Kind: "state", Key: "k1", Status: "ok", Severity: "info"}, // plain baseline: not counted
		{TS: 5, Kind: "event", Key: "e1", Severity: "warn"},
		{TS: 6, Kind: "event", Key: "e2", Severity: "error"},
		{TS: 95, Kind: "event", Key: "e3", Severity: "warn"},
	})
	b, err := s.Buckets("c", 0, 100, 10)
	if err != nil || len(b) != 10 {
		t.Fatal(err, b)
	}
	if b[0].Warn != 1 || b[0].Error != 1 || b[9].Warn != 1 || b[0].Info != 0 {
		t.Errorf("%+v", b)
	}
}

func TestRecorderStepRecordsChangesRetriesErrorsAndResumesAfterRestart(t *testing.T) {
	s := openTemp(t)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	obs := kube.Observation{States: []kube.ObservedState{st("Pod", "shop", "a", "ok", "Running")}}
	var failNext error
	m := NewManager(s, func(ctx context.Context, id string, ns []string) (kube.Observation, error) {
		if failNext != nil {
			return kube.Observation{}, failNext
		}
		return obs, nil
	})
	m.now = func() time.Time { return now }
	newRec := func() *rec { return &rec{cfg: ClusterCfg{Enabled: true, IntervalSec: 15}} }

	r := newRec()
	m.step(context.Background(), "c", r)
	if sp, _ := s.Span("c"); sp.Count != 1 {
		t.Fatalf("baseline: %+v", sp)
	}
	m.step(context.Background(), "c", r)
	if sp, _ := s.Span("c"); sp.Count != 1 {
		t.Fatalf("unchanged must not add records: %+v", sp)
	}
	// an unreachable API server is an error status, never "everything was deleted"
	failNext = errors.New("connection refused")
	m.step(context.Background(), "c", r)
	if r.st.LastErr == "" {
		t.Error("error not surfaced")
	}
	if sp, _ := s.Span("c"); sp.Count != 1 {
		t.Fatalf("a failed read must not write: %+v", sp)
	}
	failNext = nil
	obs.States = []kube.ObservedState{st("Pod", "shop", "a", "error", "CrashLoopBackOff")}
	now = now.Add(time.Minute)
	m.step(context.Background(), "c", r)
	if sp, _ := s.Span("c"); sp.Count != 2 || r.st.LastErr != "" {
		t.Fatalf("%+v %q", sp, r.st.LastErr)
	}
	// "restart the app": a fresh recorder loads the last states, so the unchanged pod is not recorded again
	r2 := newRec()
	m.step(context.Background(), "c", r2)
	if sp, _ := s.Span("c"); sp.Count != 2 {
		t.Fatalf("restart must not duplicate baselines: %+v", sp)
	}
}

func TestConfigNormalizeAndApply(t *testing.T) {
	c := Config{Clusters: map[string]ClusterCfg{"a": {Enabled: true, IntervalSec: 1}, "b": {Enabled: true, IntervalSec: 9999}, "c": {}}, RetentionHours: 9999, MaxRecords: 5}.Normalize()
	if c.RetentionHours != 168 || c.MaxRecords != 10_000 || c.Clusters["a"].IntervalSec != 5 || c.Clusters["b"].IntervalSec != 300 || c.Clusters["c"].IntervalSec != 15 {
		t.Errorf("%+v", c)
	}
	if d := (Config{}).Normalize(); d.RetentionHours != 24 || d.MaxRecords != 200_000 {
		t.Errorf("%+v", d)
	}

	s := openTemp(t)
	calls := make(chan string, 10)
	m := NewManager(s, func(ctx context.Context, id string, ns []string) (kube.Observation, error) {
		calls <- id
		return kube.Observation{}, nil
	})
	m.Apply(Config{Clusters: map[string]ClusterCfg{"a": {Enabled: true}, "off": {Enabled: false}}})
	select {
	case id := <-calls:
		if id != "a" {
			t.Fatal(id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("recorder did not start")
	}
	if !m.Status("a").Running || m.Status("off").Running {
		t.Error("status")
	}
	m.Apply(Config{Clusters: map[string]ClusterCfg{}})
	if m.Status("a").Running {
		t.Error("recorder should have stopped")
	}
	m.Stop()
}

func TestValidateCluster(t *testing.T) {
	cc, err := ValidateCluster(ClusterCfg{Enabled: true, Namespaces: []string{" shop ", "", "kube-system"}, IntervalSec: 30})
	if err != nil || len(cc.Namespaces) != 2 || cc.Namespaces[0] != "shop" {
		t.Fatal(err, cc)
	}
	for _, bad := range []ClusterCfg{
		{Namespaces: []string{"Bad_Name"}}, {Namespaces: []string{"a/b"}}, {Namespaces: []string{"-x"}},
		{IntervalSec: 2}, {IntervalSec: 301}, {Namespaces: make([]string, 51)},
	} {
		if _, err := ValidateCluster(bad); err == nil {
			t.Errorf("should be rejected: %+v", bad)
		}
	}
	if err := ValidateLimits(0, 0); err != nil {
		t.Error("zero keeps the current value")
	}
	for _, c := range [][2]int{{200, 0}, {-1, 0}, {0, 5}, {0, 2_000_000}} {
		if ValidateLimits(c[0], c[1]) == nil {
			t.Errorf("limits %v should be rejected", c)
		}
	}
}
