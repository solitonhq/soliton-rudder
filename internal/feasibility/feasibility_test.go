package feasibility

import (
	"context"
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
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"

	"soliton-rudder/internal/kube"
)

func normalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = normalize(e)
		}
	case []any:
		for i, e := range x {
			x[i] = normalize(e)
		}
	case float64:
		if x == float64(int64(x)) {
			return int64(x)
		}
	}
	return v
}

func obj(t *testing.T, doc string) unstructured.Unstructured {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatal(err)
	}
	return unstructured.Unstructured{Object: normalize(m).(map[string]any)}
}

func snap(t *testing.T, server string, lists map[string][]string) *Snapshot {
	s := &Snapshot{ServerVersion: server, Lists: map[string][]unstructured.Unstructured{}, Missing: map[string]string{}}
	for name, docs := range lists {
		for _, d := range docs {
			s.Lists[name] = append(s.Lists[name], obj(t, d))
		}
	}
	return s
}

func result(rep Report, id string) RuleResult {
	for _, r := range rep.Rules {
		if r.ID == id {
			return r
		}
	}
	panic("no rule " + id)
}

func names(r RuleResult) string {
	var n []string
	for _, f := range r.Findings {
		n = append(n, f.Name)
	}
	return strings.Join(n, ",")
}

const goodDeploy = `
kind: Deployment
metadata: {name: good, namespace: app, labels: {a: b}}
spec:
  replicas: 3
  template:
    metadata: {labels: {app: good}}
    spec:
      securityContext: {runAsUser: 1000}
      containers:
      - name: c
        image: registry.io/app:1.2.3
        readinessProbe: {httpGet: {path: /, port: 80}}
        resources: {requests: {cpu: 100m, memory: 128Mi}, limits: {memory: 256Mi}}
        securityContext: {allowPrivilegeEscalation: false}
status: {readyReplicas: 3}
`

const badDeploy = `
kind: Deployment
metadata: {name: bad, namespace: app}
spec:
  replicas: 1
  template:
    metadata: {labels: {app: bad}}
    spec:
      hostNetwork: true
      volumes: [{name: s, hostPath: {path: /var/run/docker.sock}}]
      containers:
      - name: c
        image: nginx
        securityContext: {privileged: true, runAsUser: 0, capabilities: {add: [SYS_ADMIN, NET_RAW]}}
status: {readyReplicas: 0}
`

func TestHygieneAndSecurityRules(t *testing.T) {
	s := snap(t, "v1.34.1", map[string][]string{
		"deployments":          {goodDeploy, badDeploy},
		"poddisruptionbudgets": {},
		"nodes":                {"metadata: {name: n1}\nstatus: {allocatable: {cpu: '4', memory: 16Gi}, conditions: [{type: Ready, status: 'True'}]}"},
	})
	rep := Evaluate(s)
	for id, want := range map[string]string{
		"no-requests": "bad", "no-memory-limit": "bad", "no-readiness-probe": "bad", "single-replica": "bad",
		"privileged": "bad", "host-namespaces": "bad", "host-path": "bad", "run-as-root": "bad",
		"dangerous-capabilities": "bad", "latest-tag": "bad", "privilege-escalation": "bad",
		"unhealthy-workloads": "bad", "no-pdb": "good", // good has 3 replicas and no PDB
	} {
		r := result(rep, id)
		if !strings.Contains(names(r), want) {
			t.Errorf("%s: findings = %q, want to include %q (skipped=%q)", id, names(r), want, r.Skipped)
		}
	}
	// the well-configured deployment must not trip hygiene/security rules
	for _, id := range []string{"no-requests", "no-memory-limit", "no-readiness-probe", "privileged", "run-as-root", "latest-tag", "privilege-escalation", "single-replica"} {
		if strings.Contains(names(result(rep, id)), "good") {
			t.Errorf("%s flagged the good deployment", id)
		}
	}
	if sev := result(rep, "privileged").Severity; sev != Critical {
		t.Errorf("privileged severity = %s", sev)
	}
	if sev := result(rep, "host-path").Severity; sev != High {
		t.Errorf("docker.sock hostPath should be high, got %s", sev)
	}
	if sev := result(rep, "dangerous-capabilities").Severity; sev != High {
		t.Errorf("SYS_ADMIN should be high, got %s", sev)
	}
}

func TestPDBMatchingAndSystemNamespaceExclusion(t *testing.T) {
	s := snap(t, "v1.34.0", map[string][]string{
		"deployments":          {goodDeploy, strings.Replace(strings.Replace(goodDeploy, "name: good, namespace: app", "name: sys, namespace: kube-system", 1), "app: good", "app: sys", 1)},
		"poddisruptionbudgets": {"metadata: {name: p, namespace: app}\nspec: {selector: {matchLabels: {app: good}}}"},
	})
	rep := Evaluate(s)
	if r := result(rep, "no-pdb"); r.Affected != 0 {
		t.Fatalf("PDB should cover the deployment: %+v", r.Findings)
	}
	if r := result(rep, "single-replica"); r.Checked != 1 {
		t.Fatalf("kube-system must be excluded, checked=%d", r.Checked)
	}
}

func TestNodeCapacityRules(t *testing.T) {
	node := func(n string) string {
		return fmt.Sprintf("metadata: {name: %s}\nstatus: {allocatable: {cpu: '2', memory: 8Gi}, conditions: [{type: Ready, status: 'True'}]}", n)
	}
	pod := func(name, node, cpu string) string {
		return fmt.Sprintf("metadata: {name: %s, namespace: app}\nspec: {nodeName: %s, containers: [{name: c, resources: {requests: {cpu: %s, memory: 1Gi}}}]}\nstatus: {phase: Running}", name, node, cpu)
	}
	s := snap(t, "v1.34.0", map[string][]string{
		"nodes": {node("n1"), node("n2")},
		"pods":  {pod("a", "n1", "1900m"), pod("b", "n2", "500m")},
	})
	rep := Evaluate(s)
	sat := result(rep, "node-request-saturation")
	if len(sat.Findings) != 1 || sat.Findings[0].Name != "n1" || sat.Findings[0].Severity != Medium {
		t.Fatalf("saturation: %+v", sat.Findings)
	}
	// 2.4 cores requested vs 2.0 left without the biggest node
	if r := result(rep, "cluster-headroom"); r.Affected != 1 || r.Severity != High {
		t.Fatalf("headroom: %+v", r)
	}
	one := snap(t, "v1.34.0", map[string][]string{"nodes": {node("n1")}, "pods": {}})
	if r := result(Evaluate(one), "cluster-headroom"); r.Severity != Medium {
		t.Fatalf("single node: %+v", r)
	}
}

func TestRuntimeReliabilityRules(t *testing.T) {
	old := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	s := snap(t, "v1.34.0", map[string][]string{
		"nodes": {"metadata: {name: n1}\nstatus: {conditions: [{type: Ready, status: 'False'}]}", "metadata: {name: n2}\nspec: {unschedulable: true}\nstatus: {conditions: [{type: Ready, status: 'True'}, {type: DiskPressure, status: 'True'}]}"},
		"pods": {
			"metadata: {name: crash, namespace: app}\nstatus: {phase: Running, containerStatuses: [{name: c, restartCount: 9, state: {waiting: {reason: CrashLoopBackOff}}}]}",
			"metadata: {name: pend, namespace: app, creationTimestamp: '" + old + "'}\nstatus: {phase: Pending}",
			"metadata: {name: dead, namespace: app}\nstatus: {phase: Failed, reason: Evicted}",
			"metadata: {name: fine, namespace: app}\nstatus: {phase: Running, containerStatuses: [{name: c, restartCount: 0, state: {running: {}}}]}",
		},
		"persistentvolumeclaims":   {"metadata: {name: data, namespace: app, creationTimestamp: '" + old + "'}\nstatus: {phase: Pending}"},
		"persistentvolumes":        {"metadata: {name: pv1}\nstatus: {phase: Released}"},
		"horizontalpodautoscalers": {"metadata: {name: h, namespace: app}\nspec: {maxReplicas: 5}\nstatus: {currentReplicas: 5, desiredReplicas: 5}"},
	})
	rep := Evaluate(s)
	check := map[string]string{"crashing-pods": "crash", "stuck-pending": "pend", "failed-pods": "dead", "pending-pvc": "data", "released-volumes": "pv1", "hpa-maxed": "h"}
	for id, want := range check {
		if got := names(result(rep, id)); got != want {
			t.Errorf("%s: %q, want %q", id, got, want)
		}
	}
	nh := result(rep, "node-health")
	if nh.Affected != 3 || nh.Severity != Critical { // n1 NotReady, n2 pressure + cordoned
		t.Fatalf("node-health: %+v", nh.Findings)
	}
}

func TestOneNodeRuleSpreadDetection(t *testing.T) {
	pod := func(n, node string) string {
		return fmt.Sprintf("metadata: {name: %s, namespace: app, ownerReferences: [{kind: ReplicaSet, name: web-abc}]}\nspec: {nodeName: %s}\nstatus: {phase: Running}", n, node)
	}
	nodes := []string{"metadata: {name: n1}", "metadata: {name: n2}"}
	s := snap(t, "v1.34.0", map[string][]string{
		"nodes":       nodes,
		"replicasets": {"metadata: {name: web-abc, namespace: app, ownerReferences: [{kind: Deployment, name: web}]}"},
		"pods":        {pod("p1", "n1"), pod("p2", "n1")},
	})
	if r := result(Evaluate(s), "replicas-one-node"); names(r) != "web" {
		t.Fatalf("%+v", r)
	}
	s.Lists["pods"] = []unstructured.Unstructured{obj(t, pod("p1", "n1")), obj(t, pod("p2", "n2"))}
	if r := result(Evaluate(s), "replicas-one-node"); r.Affected != 0 {
		t.Fatalf("spread pods flagged: %+v", r)
	}
	s.Lists["nodes"] = []unstructured.Unstructured{obj(t, nodes[0])}
	if r := result(Evaluate(s), "replicas-one-node"); r.Skipped == "" {
		t.Fatal("single schedulable node should skip the rule")
	}
}

func TestNetworkAndIngressRules(t *testing.T) {
	s := snap(t, "v1.34.0", map[string][]string{
		"namespaces":      {"metadata: {name: a}", "metadata: {name: b}"},
		"pods":            {"metadata: {name: p, namespace: a}", "metadata: {name: q, namespace: b}"},
		"networkpolicies": {"metadata: {name: deny, namespace: a}"},
		"ingresses": {
			"metadata: {name: i1, namespace: a}\nspec: {tls: [{hosts: ['*.ex.com']}], rules: [{host: www.ex.com}, {host: other.io}]}",
		},
	})
	rep := Evaluate(s)
	if got := names(result(rep, "no-network-policy")); got != "b" {
		t.Fatalf("netpol: %q", got)
	}
	r := result(rep, "ingress-no-tls")
	if len(r.Findings) != 1 || !strings.Contains(r.Findings[0].Message, "other.io") || strings.Contains(r.Findings[0].Message, "www.ex.com") {
		t.Fatalf("ingress tls: %+v", r.Findings)
	}
}

func TestMaintenanceRules(t *testing.T) {
	la := func(api string) string {
		return fmt.Sprintf(`metadata: {name: x, namespace: a, annotations: {"kubectl.kubernetes.io/last-applied-configuration": '{"apiVersion":"%s","kind":"Ingress"}'}}`, api)
	}
	s := snap(t, "v1.34.2-eks-1", map[string][]string{
		"ingresses": {la("extensions/v1beta1"), la("networking.k8s.io/v1")},
		"nodes": {
			"metadata: {name: old}\nstatus: {nodeInfo: {kubeletVersion: v1.30.1}}",
			"metadata: {name: lag}\nstatus: {nodeInfo: {kubeletVersion: v1.33.0}}",
			"metadata: {name: ok}\nstatus: {nodeInfo: {kubeletVersion: v1.34.2}}",
		},
	})
	rep := Evaluate(s)
	d := result(rep, "deprecated-apis")
	if len(d.Findings) != 1 || d.Findings[0].Severity != High {
		t.Fatalf("deprecated: %+v", d.Findings)
	}
	sk := result(rep, "version-skew")
	if len(sk.Findings) != 2 || sk.Findings[0].Name != "old" || sk.Findings[0].Severity != High || sk.Findings[1].Severity != Low {
		t.Fatalf("skew: %+v", sk.Findings)
	}
	if r := result(Evaluate(snap(t, "v1.35.0", nil)), "k8s-support"); r.Affected != 0 {
		t.Fatalf("v1.35 should be fully supported: %+v", r)
	}
	if r := result(Evaluate(snap(t, "v1.28.9", nil)), "k8s-support"); r.Severity != High {
		t.Fatalf("v1.28 should be unsupported: %+v", r)
	}
	if r := result(Evaluate(snap(t, "v1.34.0", nil)), "k8s-support"); r.Severity != Low {
		t.Fatalf("v1.34 is the oldest supported: %+v", r)
	}
}

func TestMemoryRulesNeedMetrics(t *testing.T) {
	pod := "metadata: {name: p, namespace: a}\nspec: {containers: [{name: c, resources: {limits: {memory: 100Mi}, requests: {cpu: 500m, memory: 512Mi}}}]}\nstatus: {phase: Running}"
	s := snap(t, "v1.34.0", map[string][]string{"pods": {pod}})
	rep := Evaluate(s)
	if result(rep, "memory-near-limit").Skipped == "" || result(rep, "overprovisioned").Skipped == "" {
		t.Fatal("rules should skip without metrics")
	}
	s.Usage = map[string]kube.Usage{"a/p": {CPU: 10, Mem: 95 << 20}}
	rep = Evaluate(s)
	if names(result(rep, "memory-near-limit")) != "p" {
		t.Fatalf("%+v", result(rep, "memory-near-limit"))
	}
	if !strings.Contains(result(rep, "overprovisioned").Findings[0].Message, "CPU 10m") {
		t.Fatalf("%+v", result(rep, "overprovisioned"))
	}
}

func TestMissingListSkipsRuleInsteadOfFalseClean(t *testing.T) {
	s := snap(t, "v1.34.0", map[string][]string{"deployments": {goodDeploy}})
	s.Missing["poddisruptionbudgets"] = "forbidden"
	rep := Evaluate(s)
	r := result(rep, "no-pdb")
	if r.Skipped == "" || r.Affected != 0 {
		t.Fatalf("%+v", r)
	}
	if rep.Stats.RulesFailed == 0 || len(rep.Notes) < 2 {
		t.Fatalf("notes/stats: %+v %v", rep.Stats, rep.Notes)
	}
}

func TestScoring(t *testing.T) {
	clean := Evaluate(snap(t, "v1.36.0", map[string][]string{
		"nodes": {"metadata: {name: a}\nstatus: {allocatable: {cpu: '4', memory: 16Gi}, conditions: [{type: Ready, status: 'True'}], nodeInfo: {kubeletVersion: v1.36.0}}", "metadata: {name: b}\nstatus: {allocatable: {cpu: '4', memory: 16Gi}, conditions: [{type: Ready, status: 'True'}], nodeInfo: {kubeletVersion: v1.36.0}}"},
	}))
	if clean.Score != 100 {
		t.Fatalf("clean score = %d; notes=%v", clean.Score, clean.Rules)
	}
	for _, c := range clean.Categories {
		if c.Score != 100 {
			t.Fatalf("%+v", c)
		}
	}
	bad := Evaluate(snap(t, "v1.36.0", map[string][]string{"deployments": {goodDeploy, badDeploy}}))
	if bad.Score >= clean.Score || bad.Score <= 0 {
		t.Fatalf("bad score = %d", bad.Score)
	}
	if bad.Counts.Critical == 0 || bad.Counts.High == 0 {
		t.Fatalf("%+v", bad.Counts)
	}
	// more affected objects ⇒ lower (or equal) score, never higher
	one := RuleResult{Severity: High, Checked: 10, Affected: 1}
	all := RuleResult{Severity: High, Checked: 10, Affected: 10}
	if rulePenalty(all) <= rulePenalty(one) || rulePenalty(one) < 4 || rulePenalty(all) > 10 {
		t.Fatalf("penalties: %v %v", rulePenalty(one), rulePenalty(all))
	}
	score, _ := Score([]RuleResult{{Category: CatSecurity, Severity: Critical, Checked: 1, Affected: 1}, {Category: CatSecurity, Severity: Critical, Checked: 1, Affected: 1}, {Category: CatSecurity, Severity: Critical, Checked: 1, Affected: 1}, {Category: CatSecurity, Severity: Critical, Checked: 1, Affected: 1}, {Category: CatSecurity, Severity: Critical, Checked: 1, Affected: 1}, {Category: CatSecurity, Severity: Critical, Checked: 1, Affected: 1}})
	if score != 0 {
		t.Fatalf("score must clamp at 0, got %d", score)
	}
}

func TestStoreRoundTripPruneAndCompare(t *testing.T) {
	st, err := OpenStore(filepath.Join(t.TempDir(), "f.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	mk := func(id string, at int64, score int, rules ...RuleResult) Report {
		return Report{ID: id, ClusterID: "c1", StartedAt: at, Score: score, Rules: rules, Categories: []CategoryScore{{Category: CatSecurity, Score: score}}}
	}
	f := func(name string) Finding {
		return Finding{Severity: High, Kind: "Pod", Namespace: "a", Name: name, Message: "m"}
	}
	r1 := mk("r1", 100, 60, RuleResult{ID: "x", Title: "X", Affected: 2, Findings: []Finding{f("a"), f("b")}})
	r2 := mk("r2", 200, 75, RuleResult{ID: "x", Title: "X", Affected: 2, Findings: []Finding{f("b"), f("c")}})
	for _, r := range []Report{r1, r2} {
		if err := st.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.Get("r2")
	if err != nil || got.Score != 75 || len(got.Rules[0].Findings) != 2 {
		t.Fatalf("%v %+v", err, got)
	}
	l, _ := st.List("c1")
	if len(l) != 2 || l[0].ID != "r1" || l[1].Score != 75 {
		t.Fatalf("%+v", l)
	}
	if o, _ := st.List("other"); len(o) != 0 {
		t.Fatal("cross-cluster leak")
	}
	d := Compare(r1, r2)
	if d.ScoreDelta != 15 || len(d.New) != 1 || d.New[0].Finding.Name != "c" || len(d.Resolved) != 1 || d.Resolved[0].Finding.Name != "a" || d.Unchanged != 1 {
		t.Fatalf("%+v", d)
	}
	if err := st.Delete("r1"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get("r1"); err != ErrNotFound {
		t.Fatalf("%v", err)
	}
	for i := 0; i < keepPerCluster+5; i++ {
		_ = st.Save(mk(fmt.Sprintf("n%03d", i), int64(1000+i), 50))
	}
	l, _ = st.List("c1")
	if len(l) != keepPerCluster {
		t.Fatalf("expected prune to %d, got %d", keepPerCluster, len(l))
	}
}

func TestExportHTMLEscapes(t *testing.T) {
	r := Report{ClusterName: "prod", Score: 70, StartedAt: 1700000000, Categories: []CategoryScore{{Category: CatSecurity, Score: 70}},
		Rules: []RuleResult{{ID: "x", Title: "Bad", Severity: High, Affected: 1, Checked: 1, Findings: []Finding{{Severity: High, Kind: "Pod", Name: "<script>alert(1)</script>", Message: "m"}}}}}
	out, err := ExportHTML(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "<script>alert") || !strings.Contains(out, "&lt;script&gt;") || !strings.Contains(out, "prod") {
		t.Fatalf("not escaped:\n%s", out)
	}
}

func TestCollectAgainstFakeAPI(t *testing.T) {
	js := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) { js(w, map[string]string{"gitVersion": "v1.34.1"}) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/poddisruptionbudgets"):
			w.WriteHeader(403)
			js(w, map[string]any{"kind": "Status", "apiVersion": "v1", "status": "Failure", "reason": "Forbidden", "code": 403, "message": "nope"})
		case strings.HasSuffix(p, "/cronjobs"):
			w.WriteHeader(404)
			js(w, map[string]any{"kind": "Status", "apiVersion": "v1", "status": "Failure", "reason": "NotFound", "code": 404})
		case strings.HasPrefix(p, "/apis/metrics.k8s.io"):
			w.WriteHeader(404)
			js(w, map[string]any{"kind": "Status", "apiVersion": "v1", "status": "Failure", "reason": "NotFound", "code": 404})
		default:
			items := []any{}
			if strings.HasSuffix(p, "/nodes") {
				items = append(items, map[string]any{"apiVersion": "v1", "kind": "Node", "metadata": map[string]any{"name": "n1"}})
			}
			js(w, map[string]any{"kind": "List", "apiVersion": "v1", "items": items})
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	sess, err := kube.NewSession("c1", &rest.Config{Host: srv.URL}, func(string, any) {})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	var phases []string
	snapshot, err := Collect(context.Background(), sess, func(p string, _ int) { phases = append(phases, p) })
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.list("nodes")) != 1 {
		t.Fatalf("nodes: %+v", snapshot.Lists["nodes"])
	}
	if _, bad := snapshot.Missing["poddisruptionbudgets"]; !bad {
		t.Fatalf("forbidden list must be recorded as missing: %v", snapshot.Missing)
	}
	if _, bad := snapshot.Missing["cronjobs"]; bad {
		t.Fatal("a missing API (404) is just empty, not an error")
	}
	if snapshot.Usage != nil || snapshot.UsageErr == "" {
		t.Fatal("metrics should be reported unavailable")
	}
	if len(phases) < len(listSpecs) {
		t.Fatalf("progress phases: %v", phases)
	}
	rep := Evaluate(snapshot)
	if rep.ServerVersion != "v1.34.1" || result(rep, "no-pdb").Skipped == "" {
		t.Fatalf("%+v", rep.ServerVersion)
	}
}

func TestAppendRulesRescores(t *testing.T) {
	rep := Evaluate(snap(t, "v1.36.0", map[string][]string{
		"nodes": {"metadata: {name: a}\nstatus: {allocatable: {cpu: '4', memory: 16Gi}, conditions: [{type: Ready, status: 'True'}], nodeInfo: {kubeletVersion: v1.36.0}}", "metadata: {name: b}\nstatus: {allocatable: {cpu: '4', memory: 16Gi}, conditions: [{type: Ready, status: 'True'}], nodeInfo: {kubeletVersion: v1.36.0}}"},
	}))
	if rep.Score != 100 {
		t.Fatalf("baseline %d", rep.Score)
	}
	runs, failed := rep.Stats.RulesRun, rep.Stats.RulesFailed
	AppendRules(&rep, []RuleResult{
		{ID: "plugin:x:r", Title: "Plugin rule", Category: CatSecurity, Severity: High, Checked: 4, Affected: 2, Findings: []Finding{{Severity: High, Kind: "Pod", Name: "a"}, {Severity: High, Kind: "Pod", Name: "b"}}},
		{ID: "plugin:x:broken", Title: "Broken", Category: CatSecurity, Skipped: "plugin backend is not running", Findings: []Finding{}},
	})
	if rep.Score >= 100 || rep.Counts.High != 2 || rep.Stats.RulesRun != runs+1 || rep.Stats.RulesFailed != failed+1 || len(rep.Rules) != len(Rules)+2 {
		t.Fatalf("%+v %+v", rep.Counts, rep.Stats)
	}
	var sec int
	for _, c := range rep.Categories {
		if c.Category == CatSecurity {
			sec = c.Score
		}
	}
	if sec >= 100 {
		t.Fatalf("security category must drop: %d", sec)
	}
}

func TestRulesDocumentation(t *testing.T) {
	ids := map[string]bool{}
	for _, r := range Rules {
		if ids[r.ID] {
			t.Errorf("duplicate rule id %s", r.ID)
		}
		ids[r.ID] = true
		if ruleSeverities[r.ID] == "" {
			t.Errorf("rule %s has no entry in ruleSeverities (docs.go)", r.ID)
		}
		if r.Title == "" || r.Description == "" || r.Remediation == "" || categoryTitles[r.Category] == "" {
			t.Errorf("rule %s is missing title/description/remediation/category", r.ID)
		}
	}
	for id := range ruleSeverities {
		if !ids[id] {
			t.Errorf("ruleSeverities documents unknown rule %s", id)
		}
	}
	md := RulesMarkdown()
	for _, r := range Rules {
		if !strings.Contains(md, r.Title) {
			t.Errorf("generated docs omit %q", r.Title)
		}
	}
}

func TestCertificateExpiryRule(t *testing.T) {
	mk := func(name string, cn string, notAfter time.Time) string {
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn}, DNSNames: []string{cn}, NotBefore: notAfter.Add(-90 * 24 * time.Hour), NotAfter: notAfter}
		der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		crt := base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
		return fmt.Sprintf("metadata: {name: %s, namespace: web}\ndata: {tls.crt: %s}", name, crt)
	}
	now := time.Now()
	s := snap(t, "v1.36.0", map[string][]string{"tlssecrets": {
		mk("ok", "ok.io", now.Add(90*24*time.Hour)), mk("soon", "soon.io", now.Add(20*24*time.Hour)),
		mk("urgent", "urgent.io", now.Add(5*24*time.Hour)), mk("dead", "dead.io", now.Add(-3*24*time.Hour)),
		"metadata: {name: broken, namespace: web}\ndata: {}",
	}})
	r := result(Evaluate(s), "certificate-expiry")
	if r.Checked != 5 || r.Affected != 4 || r.Severity != High {
		t.Fatalf("%+v", r)
	}
	sev := map[string]Severity{}
	for _, f := range r.Findings {
		sev[f.Name] = f.Severity
	}
	if sev["dead"] != High || sev["urgent"] != High || sev["soon"] != Medium || sev["broken"] != Medium || sev["ok"] != "" {
		t.Fatalf("%v", sev)
	}
	// no permission to list secrets ⇒ skipped, never a false "all clear"
	denied := snap(t, "v1.36.0", nil)
	denied.Missing["tlssecrets"] = "forbidden"
	if r := result(Evaluate(denied), "certificate-expiry"); r.Skipped == "" {
		t.Fatal("must be skipped")
	}
}

func TestProductionNamespaceDetection(t *testing.T) {
	c := DefaultConfig()
	c.ProdNamespaces = []string{"billing", "core-*", "*-eu"}
	yes := []struct {
		ns     string
		labels map[string]string
	}{
		{"production", nil}, {"shop-prod", nil}, {"prod-eu-1", nil}, {"api.live", nil}, {"billing", nil}, {"core-payments", nil}, {"checkout-eu", nil},
		{"shop", map[string]string{"environment": "Production"}}, {"x", map[string]string{"tier": "prod"}},
	}
	for _, y := range yes {
		if !c.IsProdNamespace(y.ns, y.labels) {
			t.Errorf("%s %v should be production", y.ns, y.labels)
		}
	}
	for _, n := range []string{"staging", "dev", "product-catalog", "reproduce", "qa"} {
		if c.IsProdNamespace(n, nil) {
			t.Errorf("%s must not be production", n)
		}
	}
	if DefaultConfig().IsProdNamespace("billing", nil) {
		t.Error("custom names only apply when configured")
	}
}

func singleReplicaDeploy(ns string) string {
	return fmt.Sprintf("kind: Deployment\nmetadata: {name: api, namespace: %s}\nspec: {replicas: 1, template: {metadata: {labels: {app: api}}, spec: {containers: [{name: c, image: x:1}]}}}", ns)
}

func TestProdAwareSeverity(t *testing.T) {
	s := snap(t, "v1.36.0", map[string][]string{
		"deployments":          {singleReplicaDeploy("shop-prod"), singleReplicaDeploy("shop-dev")},
		"poddisruptionbudgets": {},
	})
	r := result(Evaluate(s), "single-replica")
	sev := map[string]Severity{}
	for _, f := range r.Findings {
		sev[f.Namespace] = f.Severity
	}
	if sev["shop-prod"] != High || sev["shop-dev"] != Medium {
		t.Fatalf("%v", sev)
	}
	// namespace labels count too, and the configured list is honoured
	s2 := snap(t, "v1.36.0", map[string][]string{
		"deployments": {singleReplicaDeploy("alpha"), singleReplicaDeploy("beta")},
		"namespaces":  {"metadata: {name: alpha, labels: {env: prod}}", "metadata: {name: beta}"},
	})
	cfg := DefaultConfig()
	cfg.ProdNamespaces = []string{"beta"}
	r = result(EvaluateWith(s2, cfg), "single-replica")
	for _, f := range r.Findings {
		if f.Severity != High {
			t.Fatalf("both namespaces are production: %+v", f)
		}
	}
}

func TestHardeningRules(t *testing.T) {
	deploy := func(name, containers string, podSpec string) string {
		return fmt.Sprintf("kind: Deployment\nmetadata: {name: %s, namespace: app}\nspec: {replicas: 2, template: {metadata: {labels: {app: %s}}, spec: {%s containers: [%s]}}}", name, name, podSpec, containers)
	}
	s := snap(t, "v1.36.0", map[string][]string{"deployments": {
		deploy("bare", "{name: c, image: x:1}", ""),
		deploy("hardened", "{name: c, image: x:1, securityContext: {readOnlyRootFilesystem: true, runAsNonRoot: true}}", "securityContext: {seccompProfile: {type: RuntimeDefault}},"),
		deploy("uid", "{name: c, image: x:1, securityContext: {runAsUser: 1000}}", ""),
		deploy("root", "{name: c, image: x:1, securityContext: {runAsUser: 0}}", ""),
		deploy("podlevel", "{name: c, image: x:1, securityContext: {seccompProfile: {type: Localhost}}}", "securityContext: {runAsNonRoot: true},"),
	}, "poddisruptionbudgets": {}})
	rep := Evaluate(s)
	has := func(rule, wl string) bool { return strings.Contains(","+names(result(rep, rule))+",", ","+wl+",") }
	checks := []struct {
		rule, wl string
		want     bool
	}{
		{"no-seccomp", "bare", true}, {"no-seccomp", "hardened", false}, {"no-seccomp", "podlevel", false},
		{"no-readonly-rootfs", "bare", true}, {"no-readonly-rootfs", "hardened", false},
		{"no-run-as-nonroot", "bare", true}, {"no-run-as-nonroot", "hardened", false}, {"no-run-as-nonroot", "uid", false},
		{"no-run-as-nonroot", "root", false}, // explicit uid 0 belongs to run-as-root, no duplicate report
		{"run-as-root", "root", true}, {"no-run-as-nonroot", "podlevel", false},
	}
	for _, c := range checks {
		if has(c.rule, c.wl) != c.want {
			t.Errorf("%s on %s: got %v, want %v (findings %q)", c.rule, c.wl, !c.want, c.want, names(result(rep, c.rule)))
		}
	}
}

func TestProbeRules(t *testing.T) {
	dep := func(name, container string) string {
		return fmt.Sprintf("kind: Deployment\nmetadata: {name: %s, namespace: app}\nspec: {replicas: 2, template: {metadata: {labels: {app: %s}}, spec: {containers: [%s]}}}", name, name, container)
	}
	s := snap(t, "v1.36.0", map[string][]string{"deployments": {
		dep("none", "{name: c, image: x:1}"),
		dep("ok", "{name: c, image: x:1, ports: [{name: http, containerPort: 8080}], livenessProbe: {httpGet: {path: /h, port: http}}, readinessProbe: {tcpSocket: {port: 8080}}}"),
		dep("startup-only", "{name: c, image: x:1, startupProbe: {tcpSocket: {port: 8080}}}"),
		dep("wrong-num", "{name: c, image: x:1, ports: [{containerPort: 8080}], livenessProbe: {httpGet: {path: /h, port: 9999}}}"),
		dep("wrong-name", "{name: c, image: x:1, ports: [{containerPort: 8080}], readinessProbe: {httpGet: {path: /h, port: web}}}"),
		dep("no-ports-declared", "{name: c, image: x:1, livenessProbe: {httpGet: {path: /h, port: 9999}}}"),
	}, "poddisruptionbudgets": {}})
	rep := Evaluate(s)
	live := names(result(rep, "no-liveness-probe"))
	if !strings.Contains(live, "none") || strings.Contains(live, "ok") || strings.Contains(live, "startup-only") {
		t.Fatalf("liveness: %q", live)
	}
	pc := result(rep, "probe-misconfigured")
	if names(pc) != "wrong-name,wrong-num" && names(pc) != "wrong-num,wrong-name" {
		t.Fatalf("probe config: %q", names(pc))
	}
	for _, f := range pc.Findings {
		if f.Name == "wrong-num" && !strings.Contains(f.Message, "9999") {
			t.Fatalf("%+v", f)
		}
	}
}

func TestServiceFixesRedirectsPodFindingsToTheirDeployment(t *testing.T) {
	js := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/namespaces/shop/pods/web-abc", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"kind": "Pod", "apiVersion": "v1", "metadata": map[string]any{"name": "web-abc", "namespace": "shop", "ownerReferences": []any{map[string]any{"kind": "ReplicaSet", "name": "web-rs"}}}})
	})
	mux.HandleFunc("/apis/apps/v1/namespaces/shop/replicasets/web-rs", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"kind": "ReplicaSet", "apiVersion": "apps/v1", "metadata": map[string]any{"name": "web-rs", "namespace": "shop", "ownerReferences": []any{map[string]any{"kind": "Deployment", "name": "web"}}}})
	})
	mux.HandleFunc("/apis/apps/v1/namespaces/shop/deployments/web", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"kind": "Deployment", "apiVersion": "apps/v1", "metadata": map[string]any{"name": "web", "namespace": "shop"},
			"spec": map[string]any{"selector": map[string]any{"matchLabels": map[string]string{"app": "web"}}, "template": map[string]any{"spec": map[string]any{"containers": []any{
				map[string]any{"name": "app", "resources": map[string]any{"requests": map[string]string{"cpu": "2", "memory": "4Gi"}}}}}}}})
	})
	mux.HandleFunc("/api/v1/namespaces/shop/pods", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"kind": "PodList", "apiVersion": "v1", "items": []any{map[string]any{"kind": "Pod", "apiVersion": "v1", "metadata": map[string]any{"name": "web-abc", "namespace": "shop"}}}})
	})
	mux.HandleFunc("/apis/metrics.k8s.io/v1beta1/namespaces/shop/pods", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"items": []any{map[string]any{"metadata": map[string]any{"name": "web-abc"}, "containers": []any{map[string]any{"name": "app", "usage": map[string]string{"cpu": "100m", "memory": "500Mi"}}}}}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	sess, err := kube.NewSession("c1", &rest.Config{Host: srv.URL}, func(string, any) {})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	sess.AddCatalogForTest("apps/v1/deployments", kube.Resource{Key: "apps/v1/deployments", Group: "apps", Version: "v1", Name: "deployments", Kind: "Deployment", Namespaced: true})

	store, err := OpenStore(filepath.Join(t.TempDir(), "f.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := NewService(store, func(string) (*kube.Session, error) { return sess, nil }, func(string) string { return "c" }, func(string, any) {})
	svc.SetConfigProvider(func() Config { c := DefaultConfig(); c.HeadroomPct = 50; return c })

	fx, err := svc.Fixes(context.Background(), "c1", "overprovisioned", "core/v1/pods", "shop", "web-abc")
	if err != nil || len(fx) != 1 {
		t.Fatalf("%v %+v", err, fx)
	}
	if fx[0].Target.Kind != "Deployment" || fx[0].Target.Name != "web" || fx[0].Target.ResourceKey != "apps/v1/deployments" {
		t.Fatalf("a pod finding is fixed on its Deployment: %+v", fx[0].Target)
	}
	if !strings.Contains(fx[0].Manifest, "150m") || !strings.Contains(fx[0].Manifest, "750Mi") { // usage × (1+50%) from the configured headroom
		t.Fatalf("%s", fx[0].Manifest)
	}
	if !strings.Contains(strings.Join(fx[0].Notes, " "), "metrics-server") {
		t.Fatalf("%v", fx[0].Notes)
	}
	if _, err := svc.Fixes(context.Background(), "c1", "latest-tag", "core/v1/pods", "shop", "web-abc"); err == nil {
		t.Fatal("unfixable rules are rejected")
	}
	if _, err := svc.Fixes(context.Background(), "c1", "no-seccomp", "bogus", "shop", "x"); err == nil {
		t.Fatal("invalid resource keys are rejected")
	}
}
