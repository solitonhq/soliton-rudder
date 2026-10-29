package kube

import (
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func topoNodeByID(t Topology, id string) *TopoNode {
	for i := range t.Nodes {
		if t.Nodes[i].ID == id {
			return &t.Nodes[i]
		}
	}
	return nil
}

func hasEdge(t Topology, from, to, kind string) bool {
	for _, e := range t.Edges {
		if e.From == from && e.To == to && e.Kind == kind {
			return true
		}
	}
	return false
}

func topoPod(t *testing.T, name, rsName string, ready bool, extra string) unstructured.Unstructured {
	cond := "False"
	if ready {
		cond = "True"
	}
	return u(t, fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: shop
  labels: {app: web}
  ownerReferences: [{kind: ReplicaSet, name: %s, controller: true}]
spec:
  nodeName: n1
  containers:
  - name: c
    envFrom: [{configMapRef: {name: web-cfg}}]
  volumes:
  - {name: s, secret: {secretName: web-tls}}
status:
  phase: Running
  conditions: [{type: Ready, status: "%s"}]
%s`, name, rsName, cond, extra))
}

func baseTopo(t *testing.T) topoInput {
	return topoInput{
		Ingresses: []unstructured.Unstructured{u(t, `
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata: {name: web, namespace: shop}
spec:
  rules:
  - host: shop.example.com
    http:
      paths:
      - {path: /, backend: {service: {name: web, port: {number: 80}}}}
      - {path: /old, backend: {service: {name: gone, port: {number: 80}}}}`)},
		Services: []unstructured.Unstructured{u(t, `
apiVersion: v1
kind: Service
metadata: {name: web, namespace: shop}
spec: {selector: {app: web}}`)},
		Workloads: []unstructured.Unstructured{u(t, `
apiVersion: apps/v1
kind: Deployment
metadata: {name: web, namespace: shop}
spec: {replicas: 2}
status: {readyReplicas: 1}`)},
		ReplicaSets: []unstructured.Unstructured{u(t, `
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: web-abc
  namespace: shop
  ownerReferences: [{kind: Deployment, name: web, controller: true}]`)},
		Nodes: []unstructured.Unstructured{u(t, `
apiVersion: v1
kind: Node
metadata: {name: n1}
status: {conditions: [{type: Ready, status: "True"}]}`)},
	}
}

func TestTopologyChain(t *testing.T) {
	in := baseTopo(t)
	in.Pods = []unstructured.Unstructured{topoPod(t, "web-abc-1", "web-abc", true, ""), topoPod(t, "web-abc-2", "web-abc", false, "")}
	g := buildTopology(in)

	for _, e := range [][3]string{
		{"Ingress/shop/web", "Service/shop/web", "routes"},
		{"Service/shop/web", "Deployment/shop/web", "selects"},
		{"Deployment/shop/web", "Pod/shop/web-abc-1", "owns"},
		{"Pod/shop/web-abc-1", "Node//n1", "runs-on"},
		{"Deployment/shop/web", "ConfigMap/shop/web-cfg", "mounts"},
		{"Deployment/shop/web", "Secret/shop/web-tls", "mounts"},
	} {
		if !hasEdge(g, e[0], e[1], e[2]) {
			t.Errorf("missing edge %v", e)
		}
	}
	if n := topoNodeByID(g, "Pod/shop/web-abc-2"); n == nil || n.Status != "warn" {
		t.Errorf("unready pod should be warn: %+v", n)
	}
	if n := topoNodeByID(g, "Service/shop/web"); n.Status != "warn" || n.Detail != "1/2 pods ready" {
		t.Errorf("service: %+v", n)
	}
	if n := topoNodeByID(g, "Deployment/shop/web"); n.Status != "warn" || n.Detail != "1/2 ready" {
		t.Errorf("deployment: %+v", n)
	}
	// ReplicaSets are collapsed into their Deployment
	if topoNodeByID(g, "ReplicaSet/shop/web-abc") != nil {
		t.Error("replicaset should not be a node")
	}
	for _, n := range g.Nodes {
		if n.Kind == "Pod" && n.Tier != TierPod || n.Kind == "Ingress" && n.Tier != TierIngress {
			t.Errorf("wrong tier %+v", n)
		}
	}
}

func TestTopologyMissingBackendMarksIngress(t *testing.T) {
	in := baseTopo(t)
	in.Pods = []unstructured.Unstructured{topoPod(t, "web-abc-1", "web-abc", true, "")}
	g := buildTopology(in)
	miss := topoNodeByID(g, "Service/shop/gone")
	if miss == nil || !miss.Missing || miss.Status != "error" || miss.ResourceKey != "" {
		t.Fatalf("missing service: %+v", miss)
	}
	if n := topoNodeByID(g, "Ingress/shop/web"); n.Status != "error" {
		t.Errorf("ingress with a dangling backend must be error, got %s", n.Status)
	}
	if !hasEdge(g, "Ingress/shop/web", "Service/shop/gone", "routes") {
		t.Error("edge to the missing service should exist so the break is visible")
	}
}

func TestTopologyServiceWithoutPods(t *testing.T) {
	in := baseTopo(t)
	g := buildTopology(in)
	if n := topoNodeByID(g, "Service/shop/web"); n.Status != "error" || n.Detail != "selector matches no pods" {
		t.Errorf("%+v", n)
	}
}

func TestTopologyCrashLoopIsError(t *testing.T) {
	in := baseTopo(t)
	in.Pods = []unstructured.Unstructured{topoPod(t, "web-abc-1", "web-abc", false, `  containerStatuses:
  - name: c
    state: {waiting: {reason: CrashLoopBackOff}}`)}
	g := buildTopology(in)
	if n := topoNodeByID(g, "Pod/shop/web-abc-1"); n.Status != "error" || n.Detail != "CrashLoopBackOff" {
		t.Errorf("%+v", n)
	}
}

func TestTopologyCapsPodsButKeepsProblems(t *testing.T) {
	in := baseTopo(t)
	for i := 0; i < 20; i++ {
		in.Pods = append(in.Pods, topoPod(t, fmt.Sprintf("web-abc-%02d", i), "web-abc", true, ""))
	}
	in.Pods = append(in.Pods, topoPod(t, "web-abc-zzz-bad", "web-abc", false, `  containerStatuses:
  - name: c
    state: {waiting: {reason: ImagePullBackOff}}`))
	g := buildTopology(in)
	pods := 0
	for _, n := range g.Nodes {
		if n.Kind == "Pod" {
			pods++
		}
	}
	if pods != topoPodsPerOwner {
		t.Errorf("want %d pods drawn, got %d", topoPodsPerOwner, pods)
	}
	if topoNodeByID(g, "Pod/shop/web-abc-zzz-bad") == nil {
		t.Error("the failing pod must survive the cap")
	}
	if n := topoNodeByID(g, "Deployment/shop/web"); !strings.Contains(n.Detail, "showing 6 of 21 pods") {
		t.Error("deployment detail should mention hidden pods")
	}
}

func TestTopologyBarePodAndHPAAndCordonedNode(t *testing.T) {
	in := baseTopo(t)
	bare := u(t, `
apiVersion: v1
kind: Pod
metadata: {name: debug, namespace: shop}
spec: {nodeName: n2}
status: {phase: Running, conditions: [{type: Ready, status: "True"}]}`)
	in.Pods = []unstructured.Unstructured{bare}
	in.Nodes = append(in.Nodes, u(t, `
apiVersion: v1
kind: Node
metadata: {name: n2}
spec: {unschedulable: true}
status: {conditions: [{type: Ready, status: "True"}]}`))
	in.HPAs = []unstructured.Unstructured{u(t, `
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata: {name: web, namespace: shop}
spec: {maxReplicas: 4, scaleTargetRef: {kind: Deployment, name: web}}
status: {currentReplicas: 4}`)}
	g := buildTopology(in)
	if n := topoNodeByID(g, "Pod/shop/debug"); n == nil || n.Tier != TierPod {
		t.Fatalf("bare pod: %+v", n)
	}
	if n := topoNodeByID(g, "Node//n2"); n.Status != "warn" || n.Detail != "Cordoned" {
		t.Errorf("node: %+v", n)
	}
	if !hasEdge(g, "HorizontalPodAutoscaler/shop/web", "Deployment/shop/web", "scales") {
		t.Error("hpa edge")
	}
	if n := topoNodeByID(g, "HorizontalPodAutoscaler/shop/web"); n.Status != "warn" {
		t.Errorf("hpa at max should warn: %+v", n)
	}
}

func TestTopologyCronJobOwnership(t *testing.T) {
	in := topoInput{
		Workloads: []unstructured.Unstructured{u(t, `
apiVersion: batch/v1
kind: CronJob
metadata: {name: nightly, namespace: ops}
spec: {schedule: "0 2 * * *"}`)},
		Jobs: []unstructured.Unstructured{u(t, `
apiVersion: batch/v1
kind: Job
metadata:
  name: nightly-1
  namespace: ops
  ownerReferences: [{kind: CronJob, name: nightly, controller: true}]`)},
		Pods: []unstructured.Unstructured{u(t, `
apiVersion: v1
kind: Pod
metadata:
  name: nightly-1-x
  namespace: ops
  ownerReferences: [{kind: Job, name: nightly-1, controller: true}]
status: {phase: Succeeded}`)},
	}
	g := buildTopology(in)
	if !hasEdge(g, "CronJob/ops/nightly", "Pod/ops/nightly-1-x", "owns") {
		t.Errorf("pod should hang under its CronJob: %+v", g.Edges)
	}
	if n := topoNodeByID(g, "Pod/ops/nightly-1-x"); n.Status != "ok" {
		t.Errorf("completed pod is ok: %+v", n)
	}
}

func TestTopologyTruncationKeepsErrors(t *testing.T) {
	in := topoInput{}
	for i := 0; i < topoMaxNodes+50; i++ {
		in.Services = append(in.Services, u(t, fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata: {name: s%04d, namespace: big}
spec: {type: ExternalName, externalName: x.example.com}`, i)))
	}
	in.Services = append(in.Services, u(t, `
apiVersion: v1
kind: Service
metadata: {name: zz-broken, namespace: big}
spec: {selector: {app: nothing}}`))
	g := buildTopology(in)
	if !g.Truncated || len(g.Nodes) != topoMaxNodes {
		t.Fatalf("truncated=%v nodes=%d", g.Truncated, len(g.Nodes))
	}
	if topoNodeByID(g, "Service/big/zz-broken") == nil {
		t.Error("error nodes must be kept when truncating")
	}
}
