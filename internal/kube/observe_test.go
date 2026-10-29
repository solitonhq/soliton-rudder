package kube

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestObserveStatesAndRestarts(t *testing.T) {
	in := observeInput{
		Pods: []unstructured.Unstructured{
			u(t, `
apiVersion: v1
kind: Pod
metadata: {name: ok, namespace: shop}
status:
  phase: Running
  conditions: [{type: Ready, status: "True"}]`),
			u(t, `
apiVersion: v1
kind: Pod
metadata: {name: crash, namespace: shop}
status:
  phase: Running
  containerStatuses:
  - {name: c, restartCount: 7, state: {waiting: {reason: CrashLoopBackOff}}}`),
			u(t, `
apiVersion: v1
kind: Pod
metadata: {name: weird, namespace: shop}
status: {phase: Unknown}`),
		},
		Nodes: []unstructured.Unstructured{u(t, `
apiVersion: v1
kind: Node
metadata: {name: n1}
status: {conditions: [{type: Ready, status: "False"}]}`)},
		Workloads: []unstructured.Unstructured{u(t, `
apiVersion: apps/v1
kind: Deployment
metadata: {name: web, namespace: shop}
spec: {replicas: 3}
status: {readyReplicas: 1}`)},
	}
	o := observe(in)
	got := map[string]ObservedState{}
	for _, s := range o.States {
		got[s.Key] = s
	}
	if s := got["Pod/shop/ok"]; s.Status != "ok" {
		t.Errorf("%+v", s)
	}
	if s := got["Pod/shop/crash"]; s.Status != "error" || s.Detail != "CrashLoopBackOff · 7 restarts" {
		t.Errorf("%+v", s)
	}
	if s := got["Pod/shop/weird"]; s.Status != "warn" {
		t.Errorf("unknown phase is a warning, never silently ok: %+v", s)
	}
	if s := got["Node//n1"]; s.Status != "error" || s.Detail != "NotReady" {
		t.Errorf("%+v", s)
	}
	if s := got["Deployment/shop/web"]; s.Status != "warn" || s.Detail != "1/3 ready" {
		t.Errorf("%+v", s)
	}
}

func TestObserveEventsKeepWarningsAndExplanatoryNormals(t *testing.T) {
	ev := func(typ, reason, ts string, count int) unstructured.Unstructured {
		return u(t, `
apiVersion: v1
kind: Event
metadata: {name: e-`+reason+`, namespace: shop, uid: uid-`+reason+`}
type: `+typ+`
reason: `+reason+`
message: msg
count: `+string(rune('0'+count))+`
lastTimestamp: "`+ts+`"
involvedObject: {kind: Pod, name: web-1, namespace: shop}`)
	}
	in := observeInput{Events: []unstructured.Unstructured{
		ev("Warning", "BackOff", "2026-10-28T10:00:00Z", 3),
		ev("Normal", "Pulled", "2026-10-28T09:59:00Z", 1),
		ev("Normal", "Killing", "2026-10-28T09:58:00Z", 1),
		u(t, "apiVersion: v1\nkind: Event\nmetadata: {name: x, namespace: a, uid: z}\ntype: Warning\nreason: NoTime\nmessage: m"),
	}}
	o := observe(in)
	if len(o.Events) != 2 || o.Events[0].Reason != "Killing" || o.Events[1].Reason != "BackOff" {
		t.Fatalf("noise (Pulled) and undated events are dropped, rest ordered by time: %+v", o.Events)
	}
	if e := o.Events[1]; e.Count != 3 || e.Kind != "Pod" || e.Name != "web-1" || e.Namespace != "shop" || e.UID != "uid-BackOff" || e.At.IsZero() {
		t.Errorf("%+v", e)
	}
}
