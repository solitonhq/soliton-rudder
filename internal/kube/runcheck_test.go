package kube

import (
	"context"
	"strings"
	"testing"
)

func TestPickContainer(t *testing.T) {
	p := u(t, `
apiVersion: v1
kind: Pod
metadata: {name: p, namespace: a}
spec: {containers: [{name: app}, {name: sidecar}]}
status:
  containerStatuses:
  - {name: app, ready: true, restartCount: 0}
  - {name: sidecar, ready: false, restartCount: 4}`)
	if c := pickContainer(&p, true); c != "sidecar" {
		t.Errorf("previous logs belong to the container that restarted: %s", c)
	}
	if c := pickContainer(&p, false); c != "sidecar" {
		t.Errorf("current logs: the container that is not ready: %s", c)
	}
	q := u(t, "apiVersion: v1\nkind: Pod\nmetadata: {name: q}\nspec: {containers: [{name: only}]}")
	if c := pickContainer(&q, true); c != "only" {
		t.Errorf("fallback to spec: %s", c)
	}
}

func TestFormatters(t *testing.T) {
	if got := formatEvents(nil); !strings.Contains(got, "No events") {
		t.Error(got)
	}
	got := formatEvents([]Event{{Type: "Warning", Reason: "BackOff", Message: "restarting", Count: 5, Last: "10:00:00"}})
	if !strings.Contains(got, "BackOff ×5: restarting") {
		t.Error(got)
	}
	h := formatHealth(HealthReport{Status: "failing", Summary: "0/1 ready", Checks: []Check{{Name: "Ready", Status: "fail", Detail: "no"}}, Hints: []string{"check logs"}})
	if !strings.Contains(h, "FAILING: 0/1 ready") || !strings.Contains(h, "[fail] Ready: no") || !strings.Contains(h, "- check logs") {
		t.Error(h)
	}
}

func TestTailAndCap(t *testing.T) {
	long := strings.Repeat("line\n", maxCheckBytes)
	got := tailText(long)
	if len(got) > maxCheckBytes+64 || !strings.HasPrefix(got, "… (earlier lines omitted)\nline") {
		t.Errorf("%d %q", len(got), got[:40])
	}
	if tailText("short") != "short" {
		t.Error("short text untouched")
	}
	if c := capText(long); !strings.HasSuffix(c, "(truncated)") {
		t.Error("cap")
	}
}

func TestRunCheckRefusesSecretsAndMutations(t *testing.T) {
	s := &Session{}
	for _, do := range []string{"restart", "delete", "scale", "exec"} {
		if _, err := s.RunCheck(context.Background(), do, "core/v1/pods", "a", "b"); err == nil || !strings.Contains(err.Error(), "not a read-only check") {
			t.Errorf("%s must be refused: %v", do, err)
		}
	}
	if _, err := s.RunCheck(context.Background(), "yaml", "core/v1/secrets", "a", "b"); err == nil {
		t.Error("secrets must never be shown")
	}
}
