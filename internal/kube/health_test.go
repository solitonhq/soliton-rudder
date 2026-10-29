package kube

import (
	"strings"
	"testing"
)

func find(rep HealthReport, name string) *Check {
	for i := range rep.Checks {
		if strings.Contains(rep.Checks[i].Name, name) {
			return &rep.Checks[i]
		}
	}
	return nil
}

func TestPodCrashLoopAndOOM(t *testing.T) {
	p := u(t, `
status:
  phase: Running
  conditions: [{type: Ready, status: "False"}]
  containerStatuses:
  - name: app
    ready: false
    restartCount: 7
    state: {waiting: {reason: CrashLoopBackOff, message: back-off 5m restarting failed container}}
    lastState: {terminated: {reason: OOMKilled, exitCode: 137}}
spec:
  containers: [{name: app, resources: {limits: {memory: 256Mi}}}]
`)
	rep := buildHealth("pods", &p)
	if rep.Status != "failing" {
		t.Fatalf("%+v", rep)
	}
	if c := find(rep, "Container app"); c == nil || c.Status != "fail" || !strings.Contains(c.Detail, "CrashLoopBackOff") {
		t.Fatalf("%+v", rep.Checks)
	}
	if c := find(rep, "restarts"); c == nil || !strings.Contains(c.Detail, "OOMKilled") {
		t.Fatalf("%+v", rep.Checks)
	}
	hints := strings.Join(rep.Hints, "\n")
	if !strings.Contains(hints, "256Mi") || !strings.Contains(hints, "Logs") {
		t.Fatalf("hints: %v", rep.Hints)
	}
}

func TestPodHealthy(t *testing.T) {
	p := u(t, `
status:
  phase: Running
  conditions: [{type: Ready, status: "True"}]
  containerStatuses: [{name: app, ready: true, restartCount: 0, state: {running: {startedAt: "2026-01-01T00:00:00Z"}}}]
`)
	rep := buildHealth("pods", &p)
	if rep.Status != "healthy" || len(rep.Hints) != 0 {
		t.Fatalf("%+v", rep)
	}
}

func TestPodUnschedulable(t *testing.T) {
	p := u(t, `
status:
  phase: Pending
  conditions: [{type: PodScheduled, status: "False", reason: Unschedulable, message: "0/3 nodes are available: insufficient cpu"}]
`)
	rep := buildHealth("pods", &p)
	if rep.Status != "failing" || find(rep, "Scheduling") == nil || len(rep.Hints) == 0 {
		t.Fatalf("%+v", rep)
	}
}

func TestDeploymentStates(t *testing.T) {
	d := u(t, "{spec: {replicas: 3}, status: {readyReplicas: 1, updatedReplicas: 3, availableReplicas: 1}}")
	if rep := buildHealth("deployments", &d); rep.Status != "degraded" {
		t.Fatalf("%+v", rep)
	}
	z := u(t, "{spec: {replicas: 3}, status: {}}")
	if rep := buildHealth("deployments", &z); rep.Status != "failing" {
		t.Fatalf("%+v", rep)
	}
	stuck := u(t, "{spec: {replicas: 2}, status: {readyReplicas: 2, updatedReplicas: 2, availableReplicas: 2, conditions: [{type: Progressing, status: \"False\", reason: ProgressDeadlineExceeded}]}}")
	rep := buildHealth("deployments", &stuck)
	if rep.Status != "failing" || !strings.Contains(strings.Join(rep.Hints, " "), "deadline") {
		t.Fatalf("%+v", rep)
	}
	off := u(t, "{spec: {replicas: 0}, status: {}}")
	if rep := buildHealth("deployments", &off); rep.Status != "degraded" {
		t.Fatalf("scaled to zero should warn: %+v", rep)
	}
	ok := u(t, "{spec: {replicas: 2}, status: {readyReplicas: 2, updatedReplicas: 2, availableReplicas: 2, conditions: [{type: Available, status: \"True\"}]}}")
	if rep := buildHealth("deployments", &ok); rep.Status != "healthy" {
		t.Fatalf("%+v", rep)
	}
}

func TestNodePressureAndCordon(t *testing.T) {
	n := u(t, "{spec: {unschedulable: true}, status: {conditions: [{type: Ready, status: \"True\"}, {type: MemoryPressure, status: \"True\", message: low memory}, {type: DiskPressure, status: \"False\"}]}}")
	rep := buildHealth("nodes", &n)
	if rep.Status != "failing" || find(rep, "MemoryPressure").Status != "fail" || find(rep, "Schedulable") == nil {
		t.Fatalf("%+v", rep)
	}
	notReady := u(t, "{status: {conditions: [{type: Ready, status: \"Unknown\", message: kubelet stopped posting status}]}}")
	if rep := buildHealth("nodes", &notReady); rep.Status != "failing" {
		t.Fatalf("%+v", rep)
	}
}

func TestJobAndPVC(t *testing.T) {
	f := u(t, "{status: {failed: 3, conditions: [{type: Failed, status: \"True\", reason: BackoffLimitExceeded}]}}")
	if rep := buildHealth("jobs", &f); rep.Status != "failing" {
		t.Fatalf("%+v", rep)
	}
	c := u(t, "{status: {succeeded: 1, conditions: [{type: Complete, status: \"True\"}]}}")
	if rep := buildHealth("jobs", &c); rep.Status != "healthy" {
		t.Fatalf("%+v", rep)
	}
	pvc := u(t, "{status: {phase: Pending}}")
	if rep := buildHealth("persistentvolumeclaims", &pvc); rep.Status != "degraded" || len(rep.Hints) == 0 {
		t.Fatalf("%+v", rep)
	}
	unknown := u(t, "{status: {}}")
	if rep := buildHealth("widgets", &unknown); rep.Status != "unknown" {
		t.Fatalf("%+v", rep)
	}
}
