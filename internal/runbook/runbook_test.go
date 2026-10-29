package runbook

import "testing"

func ids(r []Resolved) []string {
	var out []string
	for _, x := range r {
		out = append(out, x.ID)
	}
	return out
}

func TestBuiltinsAreValid(t *testing.T) {
	b := Builtins()
	if err := Validate(b); err != nil {
		t.Fatal(err)
	}
	for _, r := range b {
		if !r.Builtin {
			t.Errorf("%s not marked builtin", r.ID)
		}
		for _, s := range r.Steps {
			if s.Do == DoScale {
				t.Errorf("built-in runbooks must not scale anything on their own: %s", r.ID)
			}
		}
	}
}

func TestSelectMatchesTheRightRunbookForCommonFailures(t *testing.T) {
	all := Builtins()
	cases := []struct {
		inc  Incident
		want string
	}{
		{Incident{Kind: "Pod", Name: "a", Reason: "CrashLoopBackOff · 7 restarts", Status: "error"}, "crashloop"},
		{Incident{Kind: "Pod", Name: "a", Reason: "ImagePullBackOff", Status: "error"}, "image-pull"},
		{Incident{Kind: "Pod", Name: "a", Reason: "ErrImagePull", Status: "error"}, "image-pull"},
		{Incident{Kind: "Pod", Name: "a", Reason: "OOMKilled", Status: "error"}, "oom-killed"},
		{Incident{Kind: "Pod", Name: "a", Reason: "Pending", Status: "warn"}, "pod-pending"},
		{Incident{Kind: "Node", Name: "n1", Reason: "NotReady", Status: "error"}, "node-not-ready"},
		{Incident{Kind: "Deployment", Name: "web", Reason: "0/3 ready", Status: "error"}, "workload-not-ready"},
		{Incident{Kind: "Deployment", Name: "web", Reason: "1/3 ready", Status: "warn"}, "workload-degraded"},
		{Incident{Kind: "Pod", Name: "a", Reason: "FailedMount", Status: "warn"}, "volume-mount"},
	}
	for _, c := range cases {
		got := Select(all, c.inc)
		if len(got) == 0 || got[0].ID != c.want {
			t.Errorf("%+v: want %s first, got %v", c.inc, c.want, ids(got))
		}
	}
	if got := Select(all, Incident{Kind: "Pod", Reason: "Running", Status: "ok"}); len(got) != 0 {
		t.Errorf("a healthy pod matches nothing: %v", ids(got))
	}
	if got := Select(all, Incident{Kind: "Pod", Reason: "ImagePullBackOff", Status: "error"}); contains(ids(got), "crashloop") {
		t.Errorf("ImagePullBackOff is not a crash loop: %v", ids(got))
	}
}

func TestSelectSubstitutesVariablesAndMarksApplicability(t *testing.T) {
	got := Select(Builtins(), Incident{Kind: "Node", Name: "n1", Reason: "NotReady", Status: "error"})
	if got[0].Description != "Node n1 stopped reporting as Ready." {
		t.Errorf("%q", got[0].Description)
	}
	got = Select(Builtins(), Incident{Kind: "Pod", Namespace: "shop", Name: "web-1", Reason: "CrashLoopBackOff", Status: "error"})
	for _, s := range got[0].Steps {
		if s.Do == DoPreviousLogs && (!s.Applicable || !s.ReadOnly) {
			t.Errorf("logs apply to pods and are read-only: %+v", s)
		}
		if s.Do == DoDelete && s.ReadOnly {
			t.Error("delete is never read-only")
		}
	}
	// a Node cannot be asked for logs
	custom := []Runbook{{ID: "x", Title: "t", Match: Match{Reasons: []string{"NotReady"}}, Steps: []Step{{Title: "l", Do: DoLogs}}}}
	if r := Select(custom, Incident{Kind: "Node", Reason: "NotReady"}); r[0].Steps[0].Applicable {
		t.Error("logs on a node must be marked not applicable")
	}
}

func TestParseValidatesUserRunbooks(t *testing.T) {
	ok := `
- id: my-db
  title: Database is down
  match: {kinds: [StatefulSet], reasons: ["0/"]}
  steps:
  - {title: Look, do: health}
  - {title: Scale up, do: scale, replicas: 2}
  - {title: Ask the DBA, do: manual, text: "Call {{name}} owner"}`
	rbs, err := Parse(ok)
	if err != nil || len(rbs) != 1 || *rbs[0].Steps[1].Replicas != 2 {
		t.Fatal(err, rbs)
	}
	bad := map[string]string{
		"missing id":     "- {title: t, match: {reasons: [x]}, steps: [{title: s, do: health}]}",
		"bad id":         "- {id: Bad_ID, title: t, match: {reasons: [x]}, steps: [{title: s, do: health}]}",
		"no match":       "- {id: a, title: t, steps: [{title: s, do: health}]}",
		"no steps":       "- {id: a, title: t, match: {reasons: [x]}}",
		"unknown action": "- {id: a, title: t, match: {reasons: [x]}, steps: [{title: s, do: shell}]}",
		"scale no count": "- {id: a, title: t, match: {reasons: [x]}, steps: [{title: s, do: scale}]}",
		"scale huge":     "- {id: a, title: t, match: {reasons: [x]}, steps: [{title: s, do: scale, replicas: 99999}]}",
		"bad status":     "- {id: a, title: t, match: {status: panic}, steps: [{title: s, do: health}]}",
		"unknown field":  "- {id: a, title: t, match: {reasons: [x]}, steps: [{title: s, do: health, command: rm}]}",
		"not yaml":       "{{{",
	}
	for name, doc := range bad {
		if _, err := Parse(doc); err == nil {
			t.Errorf("%s should be rejected", name)
		}
	}
	dup := "- {id: a, title: t, match: {reasons: [x]}, steps: [{title: s, do: health}]}\n- {id: a, title: t, match: {reasons: [x]}, steps: [{title: s, do: health}]}"
	if _, err := Parse(dup); err == nil {
		t.Error("duplicate ids")
	}
	if r, err := Parse("  \n"); err != nil || r != nil {
		t.Error("empty is fine")
	}
}

func TestCustomRunbooksOverrideBuiltinsAndWinTies(t *testing.T) {
	custom := []Runbook{{ID: "crashloop", Title: "Our crashloop process", Match: Match{Reasons: []string{"CrashLoopBackOff"}}, Steps: []Step{{Title: "Page the team", Do: DoManual}}}}
	all := All(custom)
	n := 0
	for _, r := range all {
		if r.ID == "crashloop" {
			n++
			if r.Title != "Our crashloop process" || r.Builtin {
				t.Errorf("%+v", r)
			}
		}
	}
	if n != 1 {
		t.Errorf("replaced, not duplicated: %d", n)
	}
	mine := Runbook{ID: "mine", Title: "mine", Match: Match{Reasons: []string{"OOMKilled"}, Kinds: []string{"Pod"}}, Steps: []Step{{Title: "x", Do: DoManual}}}
	got := Select(All([]Runbook{mine}), Incident{Kind: "Pod", Reason: "OOMKilled"})
	if got[0].ID != "mine" {
		t.Errorf("more specific user runbook ranks first: %v", ids(got))
	}
}

func TestSelectNeverReturnsNil(t *testing.T) {
	if got := Select(Builtins(), Incident{Kind: "Pod", Reason: "Running", Status: "ok"}); got == nil {
		t.Fatal("nil encodes as JSON null and the UI would wait forever instead of saying 'no match'")
	}
}
