package kube

import (
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func npPod(t *testing.T, ns, name, ip, lbls, ports string) unstructured.Unstructured {
	return u(t, fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata: {name: %s, namespace: %s, labels: %s}
spec:
  containers:
  - name: c
    ports: %s
status: {podIP: %s}`, name, ns, lbls, ports, ip))
}

func npNS(t *testing.T, name, lbls string) unstructured.Unstructured {
	return u(t, fmt.Sprintf("apiVersion: v1\nkind: Namespace\nmetadata: {name: %s, labels: %s}", name, lbls))
}

func npPolicy(t *testing.T, ns, name, spec string) unstructured.Unstructured {
	return u(t, fmt.Sprintf("apiVersion: networking.k8s.io/v1\nkind: NetworkPolicy\nmetadata: {name: %s, namespace: %s}\nspec:\n%s", name, ns, spec))
}

func world(t *testing.T, pols ...unstructured.Unstructured) netInput {
	return netInput{
		Namespaces: []unstructured.Unstructured{npNS(t, "shop", "{team: shop}"), npNS(t, "mon", "{team: ops}"), npNS(t, "other", "{team: x}")},
		Pods: []unstructured.Unstructured{
			npPod(t, "shop", "web", "10.0.0.1", "{app: web}", "[{name: http, containerPort: 8080}]"),
			npPod(t, "shop", "db", "10.0.0.2", "{app: db}", "[{containerPort: 5432}]"),
			npPod(t, "mon", "prom", "10.0.1.1", "{app: prom}", "[]"),
			npPod(t, "other", "rogue", "10.0.2.1", "{app: rogue}", "[]"),
		},
		Services: []unstructured.Unstructured{u(t, `
apiVersion: v1
kind: Service
metadata: {name: web, namespace: shop}
spec:
  selector: {app: web}
  ports: [{port: 80, targetPort: http}]`)},
		Policies: pols,
	}
}

func pod(ns, name string) NetEndpoint { return NetEndpoint{Kind: "Pod", Namespace: ns, Name: name} }

func TestNetpolNoPoliciesAllowsEverything(t *testing.T) {
	r := simulateNetwork(world(t), NetQuery{From: pod("other", "rogue"), To: pod("shop", "db"), Port: "5432"})
	if r.Verdict != "allowed" || r.Paths[0].Steps[1].Isolated {
		t.Fatalf("%+v", r)
	}
}

func TestNetpolDefaultDenyBlocksAndNamesThePolicy(t *testing.T) {
	deny := npPolicy(t, "shop", "default-deny", "  podSelector: {}\n  policyTypes: [Ingress]")
	r := simulateNetwork(world(t, deny), NetQuery{From: pod("other", "rogue"), To: pod("shop", "db"), Port: "5432"})
	if r.Verdict != "denied" || !strings.Contains(r.Summary, "default-deny") || !strings.Contains(r.Summary, "ingress of shop/db") {
		t.Fatalf("%+v", r)
	}
}

func TestNetpolPodAndNamespaceSelectors(t *testing.T) {
	allow := npPolicy(t, "shop", "db-from-web", `  podSelector: {matchLabels: {app: db}}
  policyTypes: [Ingress]
  ingress:
  - from: [{podSelector: {matchLabels: {app: web}}}]
    ports: [{protocol: TCP, port: 5432}]
  - from: [{namespaceSelector: {matchLabels: {team: ops}}}]`)
	w := world(t, allow)
	cases := []struct {
		from NetEndpoint
		port string
		want string
	}{
		{pod("shop", "web"), "5432", "allowed"},
		{pod("shop", "web"), "9999", "denied"},  // port not in rule
		{pod("mon", "prom"), "9999", "allowed"}, // any port from ops namespace
		{pod("other", "rogue"), "5432", "denied"},
	}
	for _, c := range cases {
		if r := simulateNetwork(w, NetQuery{From: c.from, To: pod("shop", "db"), Port: c.port}); r.Verdict != c.want {
			t.Errorf("%s→db:%s want %s got %s (%s)", c.from.Name, c.port, c.want, r.Verdict, r.Summary)
		}
	}
}

func TestNetpolPodSelectorOnlyIsScopedToThePolicyNamespace(t *testing.T) {
	// a pod with the same labels in ANOTHER namespace must not match a bare podSelector peer
	allow := npPolicy(t, "shop", "p", `  podSelector: {matchLabels: {app: db}}
  ingress: [{from: [{podSelector: {matchLabels: {app: rogue}}}]}]`)
	if r := simulateNetwork(world(t, allow), NetQuery{From: pod("other", "rogue"), To: pod("shop", "db"), Port: "5432"}); r.Verdict != "denied" {
		t.Fatalf("%+v", r)
	}
}

func TestNetpolEgressDefaultDenyBlocksAtTheSource(t *testing.T) {
	deny := npPolicy(t, "shop", "no-egress", "  podSelector: {matchLabels: {app: web}}\n  policyTypes: [Egress]")
	r := simulateNetwork(world(t, deny), NetQuery{From: pod("shop", "web"), To: pod("shop", "db"), Port: "5432"})
	if r.Verdict != "denied" || r.Paths[0].Steps[0].Side != "egress" || r.Paths[0].Steps[0].Allowed {
		t.Fatalf("%+v", r)
	}
	// DNS-style allowance: egress allowed to everything on 53 only
	dns := npPolicy(t, "shop", "dns", "  podSelector: {matchLabels: {app: web}}\n  policyTypes: [Egress]\n  egress: [{ports: [{protocol: UDP, port: 53}]}]")
	r = simulateNetwork(world(t, dns), NetQuery{From: pod("shop", "web"), To: pod("shop", "db"), Port: "5432"})
	if r.Verdict != "denied" {
		t.Fatalf("only port 53 is allowed out: %+v", r)
	}
	r = simulateNetwork(world(t, dns), NetQuery{From: pod("shop", "web"), To: pod("shop", "db"), Port: "5432", Protocol: "UDP"})
	if r.Verdict != "denied" {
		t.Fatalf("UDP 5432 is not UDP 53: %+v", r)
	}
}

func TestNetpolIPBlockAndExcept(t *testing.T) {
	allow := npPolicy(t, "shop", "ext", `  podSelector: {matchLabels: {app: web}}
  policyTypes: [Ingress]
  ingress:
  - from: [{ipBlock: {cidr: 203.0.113.0/24, except: [203.0.113.128/25]}}]`)
	w := world(t, allow)
	for ip, want := range map[string]string{"203.0.113.10": "allowed", "203.0.113.200": "denied", "198.51.100.1": "denied"} {
		r := simulateNetwork(w, NetQuery{From: NetEndpoint{Kind: "External", IP: ip}, To: pod("shop", "web"), Port: "8080"})
		if r.Verdict != want {
			t.Errorf("%s want %s got %s", ip, want, r.Verdict)
		}
	}
	if r := simulateNetwork(w, NetQuery{From: NetEndpoint{Kind: "External", IP: "nope"}, To: pod("shop", "web")}); r.Verdict != "unknown" {
		t.Error("invalid IP")
	}
}

func TestNetpolNamedPortsAndServiceTranslation(t *testing.T) {
	allow := npPolicy(t, "shop", "web-http", `  podSelector: {matchLabels: {app: web}}
  policyTypes: [Ingress]
  ingress: [{ports: [{port: http}]}]`)
	w := world(t, allow)
	// via the Service: port 80 → targetPort "http" → 8080, which the named-port rule resolves to
	r := simulateNetwork(w, NetQuery{From: pod("other", "rogue"), To: NetEndpoint{Kind: "Service", Namespace: "shop", Name: "web"}, Port: "80"})
	if r.Verdict != "allowed" || r.Paths[0].Port != 8080 {
		t.Fatalf("%+v", r)
	}
	// directly on a port the rule does not cover
	r = simulateNetwork(w, NetQuery{From: pod("other", "rogue"), To: pod("shop", "web"), Port: "9090"})
	if r.Verdict != "denied" {
		t.Fatalf("%+v", r)
	}
	if r := simulateNetwork(w, NetQuery{From: pod("other", "rogue"), To: NetEndpoint{Kind: "Service", Namespace: "shop", Name: "web"}, Port: "81"}); r.Verdict != "unknown" {
		t.Errorf("unknown service port: %+v", r)
	}
}

func TestNetpolEndPortRange(t *testing.T) {
	allow := npPolicy(t, "shop", "range", `  podSelector: {matchLabels: {app: db}}
  policyTypes: [Ingress]
  ingress: [{ports: [{port: 5000, endPort: 5500}]}]`)
	w := world(t, allow)
	for port, want := range map[string]string{"5432": "allowed", "4999": "denied", "5501": "denied"} {
		if r := simulateNetwork(w, NetQuery{From: pod("other", "rogue"), To: pod("shop", "db"), Port: port}); r.Verdict != want {
			t.Errorf("%s: %s", port, r.Verdict)
		}
	}
}

func TestNetpolMatchExpressionsAndEmptyRule(t *testing.T) {
	allow := npPolicy(t, "shop", "expr", `  podSelector: {matchExpressions: [{key: app, operator: In, values: [db]}]}
  policyTypes: [Ingress]
  ingress:
  - from: [{namespaceSelector: {matchExpressions: [{key: team, operator: NotIn, values: [x]}]}}]`)
	w := world(t, allow)
	if r := simulateNetwork(w, NetQuery{From: pod("mon", "prom"), To: pod("shop", "db"), Port: "5432"}); r.Verdict != "allowed" {
		t.Errorf("NotIn should admit ops: %+v", r)
	}
	if r := simulateNetwork(w, NetQuery{From: pod("other", "rogue"), To: pod("shop", "db"), Port: "5432"}); r.Verdict != "denied" {
		t.Errorf("NotIn must exclude team x: %+v", r)
	}
	open := npPolicy(t, "shop", "open", "  podSelector: {matchLabels: {app: db}}\n  policyTypes: [Ingress]\n  ingress: [{}]")
	if r := simulateNetwork(world(t, open), NetQuery{From: pod("other", "rogue"), To: pod("shop", "db"), Port: "1"}); r.Verdict != "allowed" {
		t.Errorf("an empty rule allows all: %+v", r)
	}
}

func TestNetpolServiceFanOutPartial(t *testing.T) {
	w := world(t, npPolicy(t, "shop", "only-w1", `  podSelector: {matchLabels: {app: web, v: "2"}}
  policyTypes: [Ingress]`))
	w.Pods[0] = npPod(t, "shop", "web", "10.0.0.1", "{app: web, v: \"1\"}", "[{name: http, containerPort: 8080}]")
	w.Pods = append(w.Pods, npPod(t, "shop", "web2", "10.0.0.9", "{app: web, v: \"2\"}", "[{name: http, containerPort: 8080}]"))
	r := simulateNetwork(w, NetQuery{From: pod("other", "rogue"), To: NetEndpoint{Kind: "Service", Namespace: "shop", Name: "web"}, Port: "80"})
	if r.Verdict != "partial" || len(r.Paths) != 2 {
		t.Fatalf("%+v", r)
	}
}

func TestNetpolMissingEndpoints(t *testing.T) {
	r := simulateNetwork(world(t), NetQuery{From: pod("x", "nope"), To: pod("shop", "db")})
	if r.Verdict != "unknown" {
		t.Error(r.Verdict)
	}
	r = simulateNetwork(world(t), NetQuery{From: pod("shop", "web"), To: pod("shop", "nope")})
	if r.Verdict != "unknown" {
		t.Error(r.Verdict)
	}
}
