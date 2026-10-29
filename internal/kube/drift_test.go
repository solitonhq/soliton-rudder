package kube

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

const depl = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: %NS%
  uid: abc
  resourceVersion: "12"
  generation: 3
  labels: {app: web, app.kubernetes.io/version: "%VER%"}
  annotations: {deployment.kubernetes.io/revision: "4", kubectl.kubernetes.io/last-applied-configuration: "{}", team: shop}
spec:
  replicas: %REP%
  template:
    spec:
      containers:
      - name: web
        image: shop/web:%VER%
        ports: [{containerPort: 8080}]
        env:
        - {name: LOG, value: info}
        - {name: MODE, value: %MODE%}
        resources: {requests: {cpu: 100m}}
      - name: sidecar
        image: envoy:1
status: {readyReplicas: 3}`

func dep(t *testing.T, ns, ver, rep, mode string) *unstructured.Unstructured {
	d := strings.NewReplacer("%NS%", ns, "%VER%", ver, "%REP%", rep, "%MODE%", mode).Replace(depl)
	o := u(t, d)
	return &o
}

func pathsOf(f []FieldDiff) map[string]FieldDiff {
	m := map[string]FieldDiff{}
	for _, d := range f {
		m[d.Path] = d
	}
	return m
}

func TestNormalizeIgnoresServerAndClusterLocalFields(t *testing.T) {
	a := normalizeForDrift(dep(t, "staging", "1.0", "2", "x"))
	b := normalizeForDrift(dep(t, "prod", "1.0", "2", "x"))
	if d := diffObjects("Deployment", a, b); len(d) != 0 {
		t.Fatalf("namespace, uid, status and revision noise must not count: %+v", d)
	}
	meta := a["metadata"].(map[string]any)
	if _, has := meta["uid"]; has {
		t.Error("uid kept")
	}
	if an := meta["annotations"].(map[string]any); len(an) != 1 || an["team"] != "shop" {
		t.Errorf("annotations %+v", an)
	}
}

func TestDiffFindsImageReplicasAndNamedListElements(t *testing.T) {
	src := normalizeForDrift(dep(t, "staging", "2.0", "2", "x"))
	dst := normalizeForDrift(dep(t, "prod", "1.0", "5", "x"))
	d := pathsOf(diffObjects("Deployment", src, dst))
	img := d["spec.template.spec.containers[name=web].image"]
	if img.Op != "changed" || img.Src != "shop/web:2.0" || img.Dst != "shop/web:1.0" || !img.Promotable || img.EnvSpecific {
		t.Errorf("image: %+v", img)
	}
	rep := d["spec.replicas"]
	if !rep.EnvSpecific || rep.Note == "" {
		t.Errorf("replicas should be flagged environment-specific: %+v", rep)
	}
	if _, ok := d[`metadata.labels["app.kubernetes.io/version"]`]; !ok {
		t.Errorf("dotted label keys use bracket paths: %v", d)
	}
	if _, ok := d["spec.template.spec.containers[name=sidecar].image"]; ok {
		t.Error("untouched container must not appear")
	}
}

func TestDiffAddedRemovedAndEnvByName(t *testing.T) {
	src := normalizeForDrift(dep(t, "a", "1.0", "2", "fast"))
	dst := normalizeForDrift(dep(t, "b", "1.0", "2", "slow"))
	// the target lost the LOG variable and has an extra one
	dstC := dst["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any)[0].(map[string]any)
	dstC["env"] = []any{map[string]any{"name": "MODE", "value": "slow"}, map[string]any{"name": "EXTRA", "value": "1"}}
	d := pathsOf(diffObjects("Deployment", src, dst))
	if d["spec.template.spec.containers[name=web].env[name=MODE].value"].Op != "changed" {
		t.Errorf("%v", d)
	}
	if x := d["spec.template.spec.containers[name=web].env[name=LOG].value"]; x.Op != "added" || !x.Promotable {
		t.Errorf("LOG: %+v", x)
	}
	if x := d["spec.template.spec.containers[name=web].env[name=EXTRA].value"]; x.Op != "removed" || x.Promotable || x.Note == "" {
		t.Errorf("EXTRA must be reported but not promotable: %+v", x)
	}
}

func TestPromotionManifestIsMinimalAndKeepsListIdentity(t *testing.T) {
	src := dep(t, "staging", "2.0", "2", "fast")
	m, n, err := promotionManifest(src, "prod", []string{
		"spec.template.spec.containers[name=web].image",
		"spec.template.spec.containers[name=web].env[name=MODE].value",
	})
	if err != nil || n != 2 {
		t.Fatal(err, n)
	}
	var got map[string]any
	if err := yaml.Unmarshal([]byte(m), &got); err != nil {
		t.Fatal(err)
	}
	meta := got["metadata"].(map[string]any)
	if meta["name"] != "web" || meta["namespace"] != "prod" || len(meta) != 2 {
		t.Errorf("metadata %+v", meta)
	}
	spec := got["spec"].(map[string]any)
	if _, has := spec["replicas"]; has {
		t.Error("unselected fields must not be in the manifest")
	}
	cs := spec["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any)
	if len(cs) != 1 {
		t.Fatalf("only the touched container: %+v", cs)
	}
	c := cs[0].(map[string]any)
	if c["name"] != "web" || c["image"] != "shop/web:2.0" {
		t.Errorf("%+v", c)
	}
	env := c["env"].([]any)
	if len(env) != 1 || env[0].(map[string]any)["name"] != "MODE" || env[0].(map[string]any)["value"] != "fast" {
		t.Errorf("env %+v", env)
	}
	if strings.Contains(m, "status") || strings.Contains(m, "uid") {
		t.Error("server fields leaked")
	}
}

func TestPromotionPortKeepsNumericKeyAndRejectsBadPaths(t *testing.T) {
	src := dep(t, "s", "1", "1", "x")
	m, _, err := promotionManifest(src, "p", []string{"spec.template.spec.containers[name=web].ports[containerPort=8080]"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m, "containerPort: 8080") || strings.Contains(m, `"8080"`) {
		t.Errorf("containerPort must stay a number:\n%s", m)
	}
	if _, _, err := promotionManifest(src, "p", []string{"spec.nope"}); err == nil {
		t.Error("unknown path should fail")
	}
	if _, _, err := promotionManifest(src, "p", nil); err == nil {
		t.Error("empty selection should fail")
	}
}

func TestPromotionOfAtomicListReplacesWholeList(t *testing.T) {
	o := u(t, `
apiVersion: v1
kind: ConfigMap
metadata: {name: c, namespace: a}
data: {k: v}`)
	ing := u(t, `
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata: {name: i, namespace: a}
spec:
  rules:
  - host: a.example.com
    http: {paths: [{path: /, pathType: Prefix, backend: {service: {name: s, port: {number: 80}}}}]}`)
	_ = o
	m, _, err := promotionManifest(&ing, "b", []string{"spec.rules"})
	if err != nil || !strings.Contains(m, "a.example.com") {
		t.Fatalf("%v\n%s", err, m)
	}
}

func TestFullManifestForMissingObject(t *testing.T) {
	m, err := fullManifest(dep(t, "staging", "2.0", "2", "x"), "prod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m, "namespace: prod") || strings.Contains(m, "staging") || strings.Contains(m, "status") || strings.Contains(m, "resourceVersion") {
		t.Errorf("%s", m)
	}
}

func TestServiceNormalisationAndPortKey(t *testing.T) {
	s1 := u(t, `
apiVersion: v1
kind: Service
metadata: {name: s, namespace: a}
spec:
  clusterIP: 10.0.0.1
  clusterIPs: [10.0.0.1]
  ports: [{name: http, port: 80, targetPort: 8080, nodePort: 31000}]`)
	s2 := u(t, `
apiVersion: v1
kind: Service
metadata: {name: s, namespace: b}
spec:
  clusterIP: 10.9.9.9
  clusterIPs: [10.9.9.9]
  ports: [{name: http, port: 80, targetPort: 9090, nodePort: 32000}]`)
	d := diffObjects("Service", normalizeForDrift(&s1), normalizeForDrift(&s2))
	if len(d) != 1 || d[0].Path != "spec.ports[port=80].targetPort" {
		t.Fatalf("clusterIP and nodePort are per-cluster; only targetPort differs: %+v", d)
	}
}

func TestCompareSets(t *testing.T) {
	cm := func(ns, name, v string, owner bool) unstructured.Unstructured {
		doc := "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: " + name + ", namespace: " + ns + "}\ndata: {k: " + v + "}"
		if owner {
			doc = "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: " + name + ", namespace: " + ns + ", ownerReferences: [{kind: X, name: y, apiVersion: v1, uid: u}]}\ndata: {k: " + v + "}"
		}
		return u(t, doc)
	}
	src := []unstructured.Unstructured{cm("a", "same", "1", false), cm("a", "diff", "1", false), cm("a", "new", "1", false), cm("a", "gen", "1", true), cm("a", "kube-root-ca.crt", "1", false)}
	dst := []unstructured.Unstructured{cm("b", "same", "1", false), cm("b", "diff", "2", false), cm("b", "old", "1", false), cm("b", "kube-root-ca.crt", "9", false)}
	g := compareSets("ConfigMaps", "core/v1/configmaps", src, dst)
	got := map[string]DriftItem{}
	for _, i := range g.Items {
		got[i.Name] = i
	}
	if got["same"].Status != "same" || got["diff"].Status != "differs" || got["diff"].Changes != 1 ||
		got["new"].Status != "onlySource" || got["old"].Status != "onlyTarget" {
		t.Errorf("%+v", got)
	}
	if _, ok := got["gen"]; ok {
		t.Error("controller-owned objects are skipped")
	}
	if _, ok := got["kube-root-ca.crt"]; ok {
		t.Error("kube-root-ca.crt is skipped")
	}
	if g.Items[0].Status != "differs" {
		t.Errorf("differences sort first: %+v", g.Items)
	}
}

func TestSecretsCannotBeCompared(t *testing.T) {
	if driftKindAllowed("core/v1/secrets") {
		t.Fatal("secrets must never be offered")
	}
	for _, k := range DriftKinds() {
		if strings.Contains(k.Key, "secret") {
			t.Fatal(k)
		}
	}
}
