package kube

import (
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func ovPod(t *testing.T, name, phase, extra string) unstructured.Unstructured {
	return u(t, fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata: {name: %s, namespace: shop, creationTimestamp: "2026-10-28T10:00:00Z"}
spec:
  nodeName: n1
  containers: [{name: c, resources: {requests: {cpu: 250m, memory: 128Mi}}}]
status:
  phase: %s
%s`, name, phase, extra))
}

func TestBuildOverviewCountsAndAttention(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	in := overviewInput{
		Now: now, Namespaces: 7,
		Nodes: []unstructured.Unstructured{
			u(t, `{apiVersion: v1, kind: Node, metadata: {name: n1}, status: {allocatable: {cpu: "4", memory: 8Gi}, conditions: [{type: Ready, status: "True"}]}}`),
			u(t, `{apiVersion: v1, kind: Node, metadata: {name: n2}, spec: {unschedulable: true}, status: {allocatable: {cpu: "4", memory: 8Gi}, conditions: [{type: Ready, status: "True"}]}}`),
			u(t, `{apiVersion: v1, kind: Node, metadata: {name: n3}, status: {allocatable: {cpu: "4", memory: 8Gi}, conditions: [{type: Ready, status: "False"}]}}`),
		},
		Pods: []unstructured.Unstructured{
			ovPod(t, "ok", "Running", "  conditions: [{type: Ready, status: \"True\"}]"),
			ovPod(t, "crash", "Running", "  containerStatuses: [{name: c, restartCount: 3, state: {waiting: {reason: CrashLoopBackOff}}}]"),
			ovPod(t, "pending-old", "Pending", ""),
			ovPod(t, "done", "Succeeded", ""),
			ovPod(t, "dead", "Failed", ""),
			u(t, `{apiVersion: v1, kind: Pod, metadata: {name: pending-new, namespace: shop, creationTimestamp: "2026-10-28T11:58:00Z"}, status: {phase: Pending}}`),
		},
		Workloads: []unstructured.Unstructured{
			u(t, `{apiVersion: apps/v1, kind: Deployment, metadata: {name: web, namespace: shop}, spec: {replicas: 3}, status: {readyReplicas: 3}}`),
			u(t, `{apiVersion: apps/v1, kind: Deployment, metadata: {name: api, namespace: shop}, spec: {replicas: 3}, status: {readyReplicas: 1}}`),
			u(t, `{apiVersion: apps/v1, kind: StatefulSet, metadata: {name: db, namespace: shop}, spec: {replicas: 2}, status: {readyReplicas: 0}}`),
			u(t, `{apiVersion: apps/v1, kind: Deployment, metadata: {name: off, namespace: shop}, spec: {replicas: 0}}`),
		},
		NodeUsage: map[string]Usage{"n1": {CPU: 1500, Mem: 3 << 30}, "n3": {CPU: 100, Mem: 1 << 30}},
	}
	o := buildOverview(in)
	if o.Namespaces != 7 || o.Nodes != (OverviewNodes{Total: 3, Ready: 2, NotReady: 1, Cordoned: 1}) {
		t.Errorf("nodes %+v ns %d", o.Nodes, o.Namespaces)
	}
	if o.Pods != (OverviewPods{Total: 6, Ready: 1, NotReady: 1, Pending: 2, Failed: 1, Succeeded: 1}) {
		t.Errorf("pods %+v", o.Pods)
	}
	if o.Workloads != (OverviewWorkloads{Total: 4, Healthy: 2, Degraded: 1, Failing: 1}) {
		t.Errorf("a workload scaled to 0 is deliberate, not degraded: %+v", o.Workloads)
	}
	c := o.Capacity
	if c.CPUAllocM != 8000 || c.MemAlloc != 16<<30 {
		t.Errorf("a NotReady node contributes no allocatable capacity: %+v", c)
	}
	if !c.HasUsage || c.CPUUsedM != 1600 || c.MemUsed != 4<<30 {
		t.Errorf("usage: %+v", c)
	}
	// a failed pod is listed as needing attention; finished pods hold no requests: ok, crash, pending-old are scheduled (3 × 250m)
	if c.CPURequestedM != 750 || c.MemRequested != 3*128<<20 {
		t.Errorf("requests: %+v", c)
	}
	var names []string
	for _, a := range o.Attention {
		names = append(names, a.Severity+":"+a.Kind+"/"+a.Name)
	}
	want := []string{"error:Node/n3", "error:StatefulSet/db", "error:Pod/crash", "error:Pod/dead", "warn:Node/n2", "warn:Deployment/api", "warn:Pod/pending-old"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Errorf("attention order (errors first; nodes, workloads, pods) and the grace period for new pods:\n got %v\nwant %v", names, want)
	}
	if o.AttentionTotal != 7 {
		t.Errorf("total %d", o.AttentionTotal)
	}
	if o.Attention[2].ResourceKey != "core/v1/pods" || o.Attention[0].ResourceKey != "core/v1/nodes" {
		t.Errorf("rows can be opened: %+v", o.Attention)
	}
	assertNoNilSlices(t, "Overview", o)
}

func TestBuildOverviewMergesRecentWarningEvents(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	ev := func(name, reason, at string, count int) unstructured.Unstructured {
		return u(t, fmt.Sprintf(`
apiVersion: v1
kind: Event
metadata: {name: e-%s-%s-%s, namespace: shop}
type: Warning
reason: %s
message: "msg %s"
count: %d
lastTimestamp: "%s"
involvedObject: {kind: Pod, name: %s, namespace: shop}`, name, reason, at, reason, at, count, at, name))
	}
	o := buildOverview(overviewInput{Now: now, Events: []unstructured.Unstructured{
		ev("web-1", "BackOff", "2026-10-28T11:50:00Z", 5),
		ev("web-1", "BackOff", "2026-10-28T11:58:00Z", 3), // same object and reason: merged, latest message wins
		ev("db-0", "FailedMount", "2026-10-28T11:30:00Z", 1),
		ev("old", "Unhealthy", "2026-10-28T09:00:00Z", 9), // older than an hour
	}})
	if len(o.Warnings) != 2 {
		t.Fatalf("%+v", o.Warnings)
	}
	w := o.Warnings[0]
	if w.Name != "web-1" || w.Reason != "BackOff" || w.Count != 8 || w.Message != "msg 2026-10-28T11:58:00Z" || w.ResourceKey != "core/v1/pods" {
		t.Errorf("%+v", w)
	}
	if o.Warnings[1].Name != "db-0" {
		t.Errorf("newest first: %+v", o.Warnings)
	}
}

func TestBuildOverviewCapsAttention(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	var pods []unstructured.Unstructured
	for i := 0; i < 100; i++ {
		pods = append(pods, ovPod(t, fmt.Sprintf("p%03d", i), "Running", "  containerStatuses: [{name: c, state: {waiting: {reason: ImagePullBackOff}}}]"))
	}
	o := buildOverview(overviewInput{Now: now, Pods: pods})
	if len(o.Attention) != overviewMaxAttention || o.AttentionTotal != 100 {
		t.Errorf("%d of %d", len(o.Attention), o.AttentionTotal)
	}
}

func TestBuildOverviewEmptyCluster(t *testing.T) {
	assertNoNilSlices(t, "Overview", buildOverview(overviewInput{Now: time.Now()}))
}
