package feasibility

import (
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"soliton-rudder/internal/kube"
)

func parseManifest(t *testing.T, y string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal([]byte(y), &m); err != nil {
		t.Fatalf("manifest is not valid YAML: %v\n%s", err, y)
	}
	return m
}

func dig(m map[string]any, path ...string) any {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

func containersOf(t *testing.T, m map[string]any, path ...string) []map[string]any {
	t.Helper()
	raw, _ := dig(m, append(path, "containers")...).([]any)
	var out []map[string]any
	for _, c := range raw {
		out = append(out, c.(map[string]any))
	}
	return out
}

const fixDeploy = `
apiVersion: apps/v1
kind: Deployment
metadata: {name: web, namespace: shop}
spec:
  replicas: 1
  selector: {matchLabels: {app: web}}
  template:
    metadata: {labels: {app: web}}
    spec:
      containers:
      - name: app
        image: x:1
        ports: [{containerPort: 8080}]
        resources: {requests: {cpu: 250m}}
      - name: sidecar
        image: y:1
        securityContext: {allowPrivilegeEscalation: false}
`

func TestAddRequestsFromUsageOnlyFillsWhatIsMissing(t *testing.T) {
	o := obj(t, fixDeploy)
	fixes := ProposeFixes("no-requests", &o, FixContext{
		Usage:       map[string]kube.Usage{"app": {CPU: 80, Mem: 200 << 20}, "sidecar": {CPU: 10, Mem: 20 << 20}},
		UsageSource: "Prometheus p95", Cfg: DefaultConfig(),
	})
	if len(fixes) != 1 || fixes[0].Action != "apply" || fixes[0].Risk != "medium" || fixes[0].Target.Kind != "Deployment" {
		t.Fatalf("%+v", fixes)
	}
	m := parseManifest(t, fixes[0].Manifest)
	if m["apiVersion"] != "apps/v1" || m["kind"] != "Deployment" || dig(m, "metadata", "name") != "web" || dig(m, "metadata", "namespace") != "shop" {
		t.Fatalf("identity: %v", m)
	}
	cs := containersOf(t, m, "spec", "template", "spec")
	byName := map[string]map[string]any{}
	for _, c := range cs {
		byName[c["name"].(string)] = c
	}
	app := dig(byName["app"], "resources", "requests").(map[string]any)
	if _, has := app["cpu"]; has {
		t.Fatal("an existing request must not be overwritten")
	}
	if app["memory"] != "240Mi" { // 200Mi × 1.2
		t.Fatalf("memory: %v", app["memory"])
	}
	side := dig(byName["sidecar"], "resources", "requests").(map[string]any)
	if side["cpu"] != "12m" || side["memory"] != "32Mi" { // floors: 10m×1.2=12m, memory floor 32Mi
		t.Fatalf("sidecar: %v", side)
	}
	if !strings.Contains(strings.Join(fixes[0].Notes, " "), "Prometheus p95") {
		t.Fatalf("notes: %v", fixes[0].Notes)
	}

	// no usage ⇒ generic defaults and an honest note
	fixes = ProposeFixes("no-requests", &o, FixContext{})
	if !strings.Contains(strings.Join(fixes[0].Notes, " "), "generic starting values") || !strings.Contains(fixes[0].Manifest, "128Mi") {
		t.Fatalf("%+v", fixes[0])
	}
}

func TestSecurityFixesTouchOnlyContainersThatNeedThem(t *testing.T) {
	o := obj(t, fixDeploy)
	pe := ProposeFixes("privilege-escalation", &o, FixContext{})
	cs := containersOf(t, parseManifest(t, pe[0].Manifest), "spec", "template", "spec")
	if len(cs) != 1 || cs[0]["name"] != "app" || dig(cs[0], "securityContext", "allowPrivilegeEscalation") != false {
		t.Fatalf("sidecar already has it set: %v", cs)
	}
	compliant := obj(t, strings.Replace(fixDeploy, "        resources: {requests: {cpu: 250m}}", "        securityContext: {allowPrivilegeEscalation: false}", 1))
	if ProposeFixes("privilege-escalation", &compliant, FixContext{}) != nil {
		t.Fatal("nothing to fix when every container is already compliant")
	}
	sc := ProposeFixes("no-seccomp", &o, FixContext{})
	if dig(parseManifest(t, sc[0].Manifest), "spec", "template", "spec", "securityContext", "seccompProfile", "type") != "RuntimeDefault" || sc[0].Risk != "low" {
		t.Fatalf("%+v", sc)
	}
	root := ProposeFixes("no-run-as-nonroot", &o, FixContext{})
	ro := ProposeFixes("no-readonly-rootfs", &o, FixContext{})
	if root[0].Risk != "high" || ro[0].Risk != "high" || len(root[0].Notes) == 0 || !strings.Contains(strings.Join(ro[0].Notes, " "), "emptyDir") {
		t.Fatalf("risky fixes must carry warnings: %+v %+v", root, ro)
	}
	priv := ProposeFixes("privileged", &o, FixContext{})
	if len(priv) != 1 || priv[0].Risk != "high" || !strings.Contains(priv[0].Manifest, "privileged: false") {
		t.Fatalf("%+v", priv)
	}
}

func TestProbeFixesNeedADeclaredPort(t *testing.T) {
	o := obj(t, fixDeploy)
	r := ProposeFixes("no-readiness-probe", &o, FixContext{})
	cs := containersOf(t, parseManifest(t, r[0].Manifest), "spec", "template", "spec")
	if len(cs) != 1 || cs[0]["name"] != "app" || dig(cs[0], "readinessProbe", "tcpSocket", "port") != float64(8080) {
		t.Fatalf("only the container with a port gets a probe: %v", cs)
	}
	if !strings.Contains(strings.Join(r[0].Notes, " "), "sidecar") {
		t.Fatalf("the skipped container must be explained: %v", r[0].Notes)
	}
	l := ProposeFixes("no-liveness-probe", &o, FixContext{})
	if l[0].Risk != "medium" || !strings.Contains(l[0].Manifest, "initialDelaySeconds: 30") {
		t.Fatalf("%+v", l)
	}
}

func TestWorkloadLevelFixes(t *testing.T) {
	o := obj(t, fixDeploy)
	sr := ProposeFixes("single-replica", &o, FixContext{})
	if dig(parseManifest(t, sr[0].Manifest), "spec", "replicas") != float64(2) {
		t.Fatalf("%+v", sr)
	}
	ds := obj(t, "apiVersion: apps/v1\nkind: DaemonSet\nmetadata: {name: d, namespace: x}\nspec: {template: {spec: {containers: [{name: c}]}}}")
	if ProposeFixes("single-replica", &ds, FixContext{}) != nil {
		t.Fatal("DaemonSets have no replica count")
	}
	pdb := ProposeFixes("no-pdb", &o, FixContext{})
	pm := parseManifest(t, pdb[0].Manifest)
	if pm["kind"] != "PodDisruptionBudget" || pm["apiVersion"] != "policy/v1" || dig(pm, "metadata", "name") != "web-pdb" ||
		dig(pm, "spec", "maxUnavailable") != float64(1) || dig(pm, "spec", "selector", "matchLabels", "app") != "web" || pdb[0].Target.Kind != "PodDisruptionBudget" {
		t.Fatalf("%v", pm)
	}
}

func TestRightSizeOnlyShrinksClearlyOversizedRequests(t *testing.T) {
	o := obj(t, `
apiVersion: apps/v1
kind: Deployment
metadata: {name: big, namespace: app}
spec:
  template:
    spec:
      containers:
      - {name: huge, resources: {requests: {cpu: 2000m, memory: 4Gi}}}
      - {name: snug, resources: {requests: {cpu: 130m, memory: 300Mi}}}
`)
	fc := FixContext{Usage: map[string]kube.Usage{"huge": {CPU: 100, Mem: 500 << 20}, "snug": {CPU: 100, Mem: 250 << 20}}, UsageSource: "test", Cfg: DefaultConfig()}
	fx := ProposeFixes(RightSizeRuleID, &o, fc)
	cs := containersOf(t, parseManifest(t, fx[0].Manifest), "spec", "template", "spec")
	if len(cs) != 1 || cs[0]["name"] != "huge" {
		t.Fatalf("a container within 30%% of right-size is left alone: %v", cs)
	}
	req := dig(cs[0], "resources", "requests").(map[string]any)
	if req["cpu"] != "120m" || req["memory"] != "600Mi" {
		t.Fatalf("%v", req)
	}
	if ProposeFixes(RightSizeRuleID, &o, FixContext{}) != nil {
		t.Fatal("right-sizing needs usage data")
	}
}

func TestCronJobPathAndManagedByWarnings(t *testing.T) {
	cj := obj(t, "apiVersion: batch/v1\nkind: CronJob\nmetadata: {name: nightly, namespace: ops}\nspec: {jobTemplate: {spec: {template: {spec: {containers: [{name: c, image: x}]}}}}}")
	f := ProposeFixes("no-seccomp", &cj, FixContext{})
	if dig(parseManifest(t, f[0].Manifest), "spec", "jobTemplate", "spec", "template", "spec", "securityContext", "seccompProfile", "type") != "RuntimeDefault" {
		t.Fatalf("CronJob pod spec lives under jobTemplate: %s", f[0].Manifest)
	}
	for tool, labels := range map[string]string{
		"Helm":    "{app.kubernetes.io/managed-by: Helm}",
		"Argo CD": "{argocd.argoproj.io/instance: shop}",
		"Flux":    "{kustomize.toolkit.fluxcd.io/name: apps}",
	} {
		o := obj(t, strings.Replace(fixDeploy, "metadata: {name: web, namespace: shop}", "metadata: {name: web, namespace: shop, labels: "+labels+"}", 1))
		fx := ProposeFixes("no-seccomp", &o, FixContext{})
		if fx[0].ManagedBy != tool || !strings.Contains(strings.Join(fx[0].Notes, " "), "revert") {
			t.Errorf("%s: %+v", tool, fx[0])
		}
	}
	plain := obj(t, fixDeploy)
	if ProposeFixes("no-seccomp", &plain, FixContext{})[0].ManagedBy != "" {
		t.Fatal("unmanaged workloads carry no warning")
	}
}

func TestPodsAndNamespacesFixes(t *testing.T) {
	pod := obj(t, "apiVersion: v1\nkind: Pod\nmetadata: {name: dead, namespace: app}\nstatus: {phase: Failed}")
	d := ProposeFixes("failed-pods", &pod, FixContext{})
	if len(d) != 1 || d[0].Action != "delete" || d[0].Manifest != "" || d[0].Target.Name != "dead" || d[0].Risk != "low" {
		t.Fatalf("%+v", d)
	}
	other := ProposeFixes("no-seccomp", &pod, FixContext{})
	if len(other) != 1 || other[0].Action != "none" {
		t.Fatalf("a bare pod cannot be patched in place: %+v", other)
	}
	ns := obj(t, "apiVersion: v1\nkind: Namespace\nmetadata: {name: shop}")
	np := ProposeFixes("no-network-policy", &ns, FixContext{})
	m := parseManifest(t, np[0].Manifest)
	if len(np) != 1 || m["kind"] != "NetworkPolicy" || dig(m, "metadata", "namespace") != "shop" || np[0].Risk != "high" || !strings.Contains(strings.Join(np[0].Notes, " "), "ingress controller") {
		t.Fatalf("%+v", np)
	}
	if ProposeFixes("latest-tag", &pod, FixContext{}) != nil {
		t.Fatal("rules without an automatic fix return nothing")
	}
	for _, id := range FixableRules() {
		if id == "" {
			t.Fatal("empty id")
		}
	}
}

func TestMemoryLimitUsesRequestOrUsage(t *testing.T) {
	o := obj(t, "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: m, namespace: a}\nspec: {template: {spec: {containers: [{name: c, resources: {requests: {memory: 256Mi}}}]}}}")
	f := ProposeFixes("no-memory-limit", &o, FixContext{})
	if dig(parseManifest(t, f[0].Manifest), "spec", "template", "spec", "containers").([]any)[0].(map[string]any)["resources"].(map[string]any)["limits"].(map[string]any)["memory"] != "256Mi" {
		t.Fatalf("%s", f[0].Manifest)
	}
	f = ProposeFixes("no-memory-limit", &o, FixContext{Usage: map[string]kube.Usage{"c": {Mem: 400 << 20}}})
	if !strings.Contains(f[0].Manifest, "600Mi") {
		t.Fatalf("1.5× observed usage when it exceeds the request: %s", f[0].Manifest)
	}
}
