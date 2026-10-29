package kube

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

func u(t *testing.T, doc string) unstructured.Unstructured {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatal(err)
	}
	return unstructured.Unstructured{Object: normalizeNumbers(m).(map[string]any)}
}

// normalizeNumbers turns whole float64s into int64, as the real API client decodes them.
func normalizeNumbers(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = normalizeNumbers(e)
		}
	case []any:
		for i, e := range x {
			x[i] = normalizeNumbers(e)
		}
	case float64:
		if x == float64(int64(x)) {
			return int64(x)
		}
	}
	return v
}

func certSecret(t *testing.T, ns, name string, dns []string, notAfter time.Time) *unstructured.Unstructured {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: dns[0]}, DNSNames: dns,
		NotBefore: notAfter.Add(-90 * 24 * time.Hour), NotAfter: notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	crt := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Secret",
		"metadata": map[string]any{"name": name, "namespace": ns},
		"data":     map[string]any{"tls.crt": base64.StdEncoding.EncodeToString(crt)},
	}}
}

const ingA = `
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata: {name: web, namespace: shop}
spec:
  ingressClassName: nginx
  tls: [{hosts: [shop.example.com], secretName: shop-tls}]
  rules:
  - host: shop.example.com
    http:
      paths:
      - {path: /, pathType: Prefix, backend: {service: {name: frontend, port: {number: 80}}}}
      - {path: /api, pathType: Prefix, backend: {service: {name: api, port: {name: http}}}}
      - {path: /ghost, pathType: Prefix, backend: {service: {name: ghost, port: {number: 80}}}}
status: {loadBalancer: {ingress: [{hostname: lb.example.net}]}}
`

func TestBuildIngressViewRoutingAndHealth(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ings := []unstructured.Unstructured{u(t, ingA)}
	svcs := []unstructured.Unstructured{
		u(t, "{apiVersion: v1, kind: Service, metadata: {name: frontend, namespace: shop}, spec: {ports: [{port: 80}]}}"),
		u(t, "{apiVersion: v1, kind: Service, metadata: {name: api, namespace: shop}, spec: {ports: [{name: http, port: 8080}]}}"),
	}
	slices := []unstructured.Unstructured{
		u(t, "{metadata: {name: f1, namespace: shop, labels: {kubernetes.io/service-name: frontend}}, endpoints: [{conditions: {ready: true}, targetRef: {kind: Pod, name: fe-1}}, {conditions: {ready: false}, targetRef: {kind: Pod, name: fe-2}}]}"),
		u(t, "{metadata: {name: a1, namespace: shop, labels: {kubernetes.io/service-name: api}}, endpoints: [{conditions: {ready: false}, targetRef: {kind: Pod, name: api-1}}]}"),
	}
	secrets := map[string]*unstructured.Unstructured{
		"shop/shop-tls": certSecret(t, "shop", "shop-tls", []string{"shop.example.com"}, now.Add(10*24*time.Hour)),
	}
	v := buildIngressView(ings, svcs, slices, secrets, now)
	if len(v.Ingresses) != 1 {
		t.Fatalf("%+v", v)
	}
	in := v.Ingresses[0]
	if in.Class != "nginx" || len(in.Addresses) != 1 || in.Addresses[0] != "lb.example.net" {
		t.Fatalf("meta: %+v", in)
	}
	if len(in.TLS) != 1 || in.TLS[0].DaysLeft != 10 || in.TLS[0].Error != "" {
		t.Fatalf("tls: %+v", in.TLS)
	}
	rule := in.Rules[0]
	if !rule.Secure || len(rule.Paths) != 3 {
		t.Fatalf("rule: %+v", rule)
	}
	fe, api, ghost := rule.Paths[0], rule.Paths[1], rule.Paths[2]
	if !fe.Found || fe.Ready != 1 || fe.Total != 2 || fe.Issue != "" {
		t.Fatalf("frontend: %+v", fe)
	}
	if !api.Found || api.Ready != 0 || api.Issue == "" {
		t.Fatalf("api should report no ready pods: %+v", api)
	}
	if ghost.Found || ghost.Issue == "" {
		t.Fatalf("ghost should be missing: %+v", ghost)
	}
	var sev = map[string]int{}
	for _, i := range in.Issues {
		sev[i.Severity]++
	}
	// expiring cert (<14d) + no-ready api + missing ghost service = 3 errors
	if sev["error"] != 3 {
		t.Fatalf("issues: %+v", in.Issues)
	}
}

func TestIngressPathExposesServiceAndPodAddresses(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ing := u(t, `{apiVersion: networking.k8s.io/v1, kind: Ingress, metadata: {name: web, namespace: shop}, spec: {rules: [{host: shop.example.com, http: {paths: [{path: /, backend: {service: {name: frontend, port: {number: 80}}}}]}}]}, status: {loadBalancer: {ingress: [{ip: 203.0.113.7}, {hostname: lb.example.net}]}}}`)
	svcs := []unstructured.Unstructured{u(t, "{apiVersion: v1, kind: Service, metadata: {name: frontend, namespace: shop}, spec: {type: ClusterIP, clusterIP: 10.96.0.12, ports: [{port: 80}]}}")}
	slices := []unstructured.Unstructured{u(t, "{metadata: {name: f1, namespace: shop, labels: {kubernetes.io/service-name: frontend}}, endpoints: [{conditions: {ready: true}, addresses: [10.244.1.5], targetRef: {kind: Pod, name: fe-1}}, {conditions: {ready: false}, addresses: [10.244.2.9]}]}")}
	in := buildIngressView([]unstructured.Unstructured{ing}, svcs, slices, nil, now).Ingresses[0]
	if len(in.Addresses) != 2 || in.Addresses[0] != "203.0.113.7" || in.Addresses[1] != "lb.example.net" {
		t.Fatalf("load balancer addresses: %v", in.Addresses)
	}
	p := in.Rules[0].Paths[0]
	if p.ClusterIP != "10.96.0.12" || p.ServiceType != "ClusterIP" {
		t.Errorf("service: %+v", p)
	}
	if len(p.Pods) != 2 || p.Pods[0] != (PodRef{Name: "fe-1", Ready: true, IP: "10.244.1.5"}) {
		t.Errorf("pod: %+v", p.Pods)
	}
	if p.Pods[1].Name != "10.244.2.9" || p.Pods[1].IP != "10.244.2.9" || p.Pods[1].Ready {
		t.Errorf("an endpoint without a pod reference is named by its address: %+v", p.Pods[1])
	}
}

func TestIngressConflictsAndCertCoverage(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(name string) unstructured.Unstructured {
		return u(t, `
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata: {name: `+name+`, namespace: d}
spec:
  ingressClassName: nginx
  tls: [{hosts: [app.example.com], secretName: c}]
  rules:
  - host: app.example.com
    http: {paths: [{path: /, pathType: Prefix, backend: {service: {name: s, port: {number: 80}}}}]}
status: {loadBalancer: {ingress: [{ip: 1.2.3.4}]}}
`)
	}
	svcs := []unstructured.Unstructured{u(t, "{metadata: {name: s, namespace: d}, spec: {ports: [{port: 80}]}}")}
	secrets := map[string]*unstructured.Unstructured{"d/c": certSecret(t, "d", "c", []string{"other.example.com"}, now.Add(300*24*time.Hour))}
	v := buildIngressView([]unstructured.Unstructured{mk("a"), mk("b")}, svcs, nil, secrets, now)
	for _, in := range v.Ingresses {
		var conflict, cover bool
		for _, i := range in.Issues {
			if i.Severity == "error" && len(i.Message) > 17 && i.Message[:17] == "Conflicting route" {
				conflict = true
			}
			if i.Severity == "warn" && i.Message != "" && contains2(i.Message, "does not cover") {
				cover = true
			}
		}
		if !conflict || !cover {
			t.Fatalf("%s: conflict=%v cover=%v issues=%+v", in.Name, conflict, cover, in.Issues)
		}
	}
}

func contains2(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestHostMatchesWildcard(t *testing.T) {
	cases := []struct {
		p, h string
		ok   bool
	}{{"*.example.com", "a.example.com", true}, {"*.example.com", "a.b.example.com", false}, {"*.example.com", "example.com", false}, {"a.com", "a.com", true}}
	for _, c := range cases {
		if hostMatches(c.p, c.h) != c.ok {
			t.Fatalf("%v", c)
		}
	}
}

func TestIngressMissingSecretAndNoAddress(t *testing.T) {
	ing := u(t, `
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata: {name: x, namespace: d}
spec:
  tls: [{hosts: [x.io], secretName: gone}]
  rules: [{host: x.io, http: {paths: [{path: /, pathType: Prefix, backend: {resource: {kind: StorageBucket, name: b}}}]}}]
`)
	v := buildIngressView([]unstructured.Unstructured{ing}, nil, nil, map[string]*unstructured.Unstructured{"d/gone": nil}, time.Now())
	in := v.Ingresses[0]
	if in.Rules[0].Paths[0].Resource != "StorageBucket/b" {
		t.Fatalf("%+v", in.Rules[0].Paths[0])
	}
	var errs, warns int
	for _, i := range in.Issues {
		switch i.Severity {
		case "error":
			errs++
		case "warn":
			warns++
		}
	}
	if errs != 1 || warns != 1 {
		t.Fatalf("issues: %+v", in.Issues)
	}
}

// The UI maps over every array; a JSON null (nil slice) there blanks the whole app.
func TestIngressViewNeverEmitsNullArrays(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ings := []unstructured.Unstructured{
		u(t, ingA),
		u(t, `{apiVersion: networking.k8s.io/v1, kind: Ingress, metadata: {name: bare, namespace: shop}, spec: {}}`),
		u(t, `{apiVersion: networking.k8s.io/v1, kind: Ingress, metadata: {name: res, namespace: shop}, spec: {rules: [{http: {paths: [{path: /, backend: {resource: {kind: Bucket, name: b}}}]}}]}}`),
	}
	svcs := []unstructured.Unstructured{
		u(t, "{apiVersion: v1, kind: Service, metadata: {name: frontend, namespace: shop}, spec: {ports: [{port: 80}]}}"),
		u(t, "{apiVersion: v1, kind: Service, metadata: {name: api, namespace: shop}, spec: {ports: [{name: http, port: 8080}]}}"),
	}
	// an EndpointSlice that exists but lists no endpoints (all pods gone / scaled to zero)
	slices := []unstructured.Unstructured{
		u(t, "{metadata: {name: f1, namespace: shop, labels: {kubernetes.io/service-name: frontend}}, endpoints: []}"),
	}
	secrets := map[string]*unstructured.Unstructured{"shop/shop-tls": nil}
	raw, err := json.Marshal(buildIngressView(ings, svcs, slices, secrets, now))
	if err != nil {
		t.Fatal(err)
	}
	var generic any
	_ = json.Unmarshal(raw, &generic)
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				if e == nil && k != "names" { // names is typed as nullable on the TypeScript side
					t.Errorf("%s.%s is null", path, k)
				}
				walk(path+"."+k, e)
			}
		case []any:
			for i, e := range x {
				walk(fmt.Sprintf("%s[%d]", path, i), e)
			}
		}
	}
	walk("$", generic)
}
