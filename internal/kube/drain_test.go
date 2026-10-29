package kube

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func dNode(t *testing.T, name, cpu, mem, extra string) unstructured.Unstructured {
	return u(t, fmt.Sprintf(`
apiVersion: v1
kind: Node
metadata: {name: %s, labels: {kubernetes.io/hostname: %s}}
spec: {%s}
status:
  allocatable: {cpu: "%s", memory: %s}
  conditions: [{type: Ready, status: "True"}]`, name, name, extra, cpu, mem))
}

// dPod: a ready pod owned by ReplicaSet rs (→ Deployment dep) on node, with requests.
func dPod(t *testing.T, name, node, dep, cpu, mem, extra string) unstructured.Unstructured {
	return u(t, fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: shop
  labels: {app: %s}
  ownerReferences: [{kind: ReplicaSet, name: %s-rs, controller: true}]
spec:
  nodeName: %s
  containers:
  - name: c
    resources: {requests: {cpu: "%s", memory: %s}}
%s
status:
  phase: Running
  conditions: [{type: Ready, status: "True"}]`, name, dep, dep, node, cpu, mem, extra))
}

func dRS(t *testing.T, dep string) unstructured.Unstructured {
	return u(t, fmt.Sprintf(`
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: %s-rs
  namespace: shop
  ownerReferences: [{kind: Deployment, name: %s, controller: true}]`, dep, dep))
}

func dPDB(t *testing.T, name, app, rule string) unstructured.Unstructured {
	return u(t, fmt.Sprintf(`
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata: {name: %s, namespace: shop}
spec:
  %s
  selector: {matchLabels: {app: %s}}`, name, rule, app))
}

func podByName(r DrainResult, n string) DrainPod {
	for _, p := range r.Pods {
		if p.Name == n {
			return p
		}
	}
	return DrainPod{}
}

func TestDrainSafeWhenRoomAndPDBAllow(t *testing.T) {
	in := drainInput{
		Nodes:       []unstructured.Unstructured{dNode(t, "n1", "4", "8Gi", ""), dNode(t, "n2", "4", "8Gi", "")},
		Pods:        []unstructured.Unstructured{dPod(t, "api-1", "n1", "api", "500m", "512Mi", ""), dPod(t, "api-2", "n2", "api", "500m", "512Mi", "")},
		ReplicaSets: []unstructured.Unstructured{dRS(t, "api")},
		PDBs:        []unstructured.Unstructured{dPDB(t, "api", "api", "minAvailable: 1")},
	}
	r, err := simulateDrain(in, "n1")
	if err != nil || r.Verdict != "safe" {
		t.Fatalf("%v %+v", err, r)
	}
	if p := podByName(r, "api-1"); p.MovesTo != "n2" || p.Action != "evict" {
		t.Errorf("%+v", p)
	}
	if len(r.PDBs) != 1 || r.PDBs[0].Allowed != 1 || r.PDBs[0].Severity != "ok" {
		t.Errorf("%+v", r.PDBs)
	}
}

func TestDrainBlockedByZeroDisruptionPDB(t *testing.T) {
	in := drainInput{
		Nodes:       []unstructured.Unstructured{dNode(t, "n1", "4", "8Gi", ""), dNode(t, "n2", "4", "8Gi", "")},
		Pods:        []unstructured.Unstructured{dPod(t, "solo", "n1", "solo", "100m", "64Mi", "")},
		ReplicaSets: []unstructured.Unstructured{dRS(t, "solo")},
		PDBs:        []unstructured.Unstructured{dPDB(t, "solo", "solo", "minAvailable: 1")},
	}
	r, _ := simulateDrain(in, "n1")
	if r.Verdict != "blocked" || len(r.PDBs) != 1 || r.PDBs[0].Severity != "error" || r.PDBs[0].Allowed != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestDrainPDBPercentRoundsUp(t *testing.T) {
	var pods []unstructured.Unstructured
	for i := 0; i < 4; i++ {
		pods = append(pods, dPod(t, fmt.Sprintf("w-%d", i), "n2", "w", "10m", "8Mi", ""))
	}
	pods[0] = dPod(t, "w-0", "n1", "w", "10m", "8Mi", "")
	in := drainInput{
		Nodes:       []unstructured.Unstructured{dNode(t, "n1", "4", "8Gi", ""), dNode(t, "n2", "4", "8Gi", "")},
		Pods:        pods,
		ReplicaSets: []unstructured.Unstructured{dRS(t, "w")},
		PDBs:        []unstructured.Unstructured{dPDB(t, "w", "w", `minAvailable: "70%"`)}, // ceil(0.7*4)=3 → 1 allowed
	}
	r, _ := simulateDrain(in, "n1")
	if r.PDBs[0].Allowed != 1 || r.Verdict != "safe" {
		t.Fatalf("%+v", r.PDBs)
	}
}

func TestDrainSlowWhenMorePodsThanAllowed(t *testing.T) {
	in := drainInput{
		Nodes: []unstructured.Unstructured{dNode(t, "n1", "4", "8Gi", ""), dNode(t, "n2", "4", "8Gi", "")},
		Pods: []unstructured.Unstructured{
			dPod(t, "a-1", "n1", "a", "10m", "8Mi", ""), dPod(t, "a-2", "n1", "a", "10m", "8Mi", ""), dPod(t, "a-3", "n2", "a", "10m", "8Mi", ""),
		},
		ReplicaSets: []unstructured.Unstructured{dRS(t, "a")},
		PDBs:        []unstructured.Unstructured{dPDB(t, "a", "a", "maxUnavailable: 1")},
	}
	r, _ := simulateDrain(in, "n1")
	if r.Verdict != "disruptive" || r.PDBs[0].Severity != "warn" {
		t.Fatalf("%+v", r)
	}
}

func TestDrainBarePodDaemonSetAndEmptyDir(t *testing.T) {
	bare := dPod(t, "bare", "n1", "x", "10m", "8Mi", "")
	bare.SetOwnerReferences(nil)
	ds := dPod(t, "agent", "n1", "agent", "10m", "8Mi", "")
	ds.Object["metadata"].(map[string]any)["ownerReferences"] = []any{map[string]any{"kind": "DaemonSet", "name": "agent", "controller": true}}
	ed := dPod(t, "cache-1", "n1", "cache", "10m", "8Mi", "  volumes: [{name: v, emptyDir: {}}]")
	in := drainInput{
		Nodes:       []unstructured.Unstructured{dNode(t, "n1", "4", "8Gi", ""), dNode(t, "n2", "4", "8Gi", "")},
		Pods:        []unstructured.Unstructured{bare, ds, ed, dPod(t, "cache-2", "n2", "cache", "10m", "8Mi", "")},
		ReplicaSets: []unstructured.Unstructured{dRS(t, "cache")},
	}
	r, _ := simulateDrain(in, "n1")
	if r.Verdict != "blocked" {
		t.Fatalf("a bare pod blocks the drain: %+v", r)
	}
	if p := podByName(r, "bare"); p.Severity != "error" {
		t.Errorf("%+v", p)
	}
	if p := podByName(r, "agent"); p.Action != "skip" {
		t.Errorf("%+v", p)
	}
	if p := podByName(r, "cache-1"); p.Severity != "warn" {
		t.Errorf("emptyDir: %+v", p)
	}
}

func TestDrainNoRoomLeavesPodPending(t *testing.T) {
	in := drainInput{
		Nodes: []unstructured.Unstructured{dNode(t, "n1", "4", "8Gi", ""), dNode(t, "n2", "1", "1Gi", "")},
		Pods: []unstructured.Unstructured{
			dPod(t, "big-1", "n1", "big", "2", "512Mi", ""), dPod(t, "big-2", "n2", "big", "500m", "256Mi", ""),
		},
		ReplicaSets: []unstructured.Unstructured{dRS(t, "big")},
	}
	r, _ := simulateDrain(in, "n1")
	if r.Verdict != "blocked" || podByName(r, "big-1").MovesTo != "" || podByName(r, "big-1").Severity != "error" {
		t.Fatalf("%+v", r)
	}
	if r.Capacity.NeedCPUm != 2000 || r.Capacity.FreeCPUm != 500 {
		t.Errorf("capacity %+v", r.Capacity)
	}
}

func TestDrainSkipsCordonedAndTaintedTargets(t *testing.T) {
	in := drainInput{
		Nodes: []unstructured.Unstructured{
			dNode(t, "n1", "4", "8Gi", ""), dNode(t, "cordoned", "8", "16Gi", "unschedulable: true"),
			dNode(t, "tainted", "8", "16Gi", `taints: [{key: gpu, effect: NoSchedule}]`),
		},
		Pods:        []unstructured.Unstructured{dPod(t, "p-1", "n1", "p", "100m", "64Mi", ""), dPod(t, "p-2", "cordoned", "p", "100m", "64Mi", "")},
		ReplicaSets: []unstructured.Unstructured{dRS(t, "p")},
	}
	r, _ := simulateDrain(in, "n1")
	if podByName(r, "p-1").MovesTo != "" || r.Verdict != "blocked" {
		t.Fatalf("neither cordoned nor tainted nodes may receive it: %+v", r)
	}
	// tolerating the taint makes the tainted node a valid target
	in.Pods[0] = dPod(t, "p-1", "n1", "p", "100m", "64Mi", "  tolerations: [{key: gpu, operator: Exists}]")
	if r, _ := simulateDrain(in, "n1"); podByName(r, "p-1").MovesTo != "tainted" {
		t.Fatalf("%+v", podByName(r, "p-1"))
	}
}

func TestDrainSingleReplicaIsDisruptive(t *testing.T) {
	in := drainInput{
		Nodes:       []unstructured.Unstructured{dNode(t, "n1", "4", "8Gi", ""), dNode(t, "n2", "4", "8Gi", "")},
		Pods:        []unstructured.Unstructured{dPod(t, "only", "n1", "only", "100m", "64Mi", "")},
		ReplicaSets: []unstructured.Unstructured{dRS(t, "only")},
	}
	r, _ := simulateDrain(in, "n1")
	if r.Verdict != "disruptive" {
		t.Fatalf("%+v", r)
	}
}

func TestDrainHPAHeadroomAfterDrain(t *testing.T) {
	hpa := u(t, `
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata: {name: web, namespace: shop}
spec: {maxReplicas: 10, scaleTargetRef: {kind: Deployment, name: web}}`)
	in := drainInput{
		Nodes: []unstructured.Unstructured{dNode(t, "n1", "4", "8Gi", ""), dNode(t, "n2", "2", "8Gi", "")},
		Pods: []unstructured.Unstructured{
			dPod(t, "web-1", "n1", "web", "500m", "128Mi", ""), dPod(t, "web-2", "n2", "web", "500m", "128Mi", ""),
		},
		ReplicaSets: []unstructured.Unstructured{dRS(t, "web")},
		HPAs:        []unstructured.Unstructured{hpa},
	}
	r, _ := simulateDrain(in, "n1")
	if len(r.HPAs) != 1 {
		t.Fatalf("%+v", r.HPAs)
	}
	h := r.HPAs[0]
	// n2 has 2000-500=1500m, web-1 moves there (500m) → 1000m left → 2 more pods fit; needs 8
	if h.Current != 2 || h.Fits != 2 || h.Severity != "warn" {
		t.Fatalf("%+v", h)
	}
}

func TestDrainUnknownNode(t *testing.T) {
	if _, err := simulateDrain(drainInput{}, "ghost"); err == nil {
		t.Fatal("expected error")
	}
}
