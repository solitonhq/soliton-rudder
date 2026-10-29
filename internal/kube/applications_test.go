package kube

import (
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func appWL(t *testing.T, kind, ns, name, meta, replicas, ready, image string) unstructured.Unstructured {
	return u(t, fmt.Sprintf(`
apiVersion: apps/v1
kind: %s
metadata: {name: %s, namespace: %s, %s}
spec:
  replicas: %s
  selector: {matchLabels: {pod-app: %s}}
  template:
    spec: {containers: [{name: c, image: "%s"}]}
status: {readyReplicas: %s}`, kind, name, ns, meta, replicas, name, image, ready))
}

func appPod(t *testing.T, ns, name, app string, ready bool, restarts int) unstructured.Unstructured {
	r := "False"
	if ready {
		r = "True"
	}
	return u(t, fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata: {name: %s, namespace: %s, uid: u-%s, labels: {pod-app: %s}}
status:
  phase: Running
  conditions: [{type: Ready, status: "%s"}]
  containerStatuses: [{name: c, restartCount: %d}]`, name, ns, name, app, r, restarts))
}

func byID(a Applications, id string) *Application {
	for i := range a.Apps {
		if a.Apps[i].ID == id {
			return &a.Apps[i]
		}
	}
	return nil
}

func TestApplicationsGroupByHelmReleaseAndAggregateHealth(t *testing.T) {
	in := appsInput{
		Workloads: []unstructured.Unstructured{
			appWL(t, "Deployment", "shop", "web", `labels: {app.kubernetes.io/managed-by: Helm, app.kubernetes.io/version: "2.4.1", helm.sh/chart: shop-1.7.0}, annotations: {meta.helm.sh/release-name: shop, meta.helm.sh/release-namespace: shop}`, "3", "3", "reg.io/shop/web:2.4.1"),
			appWL(t, "Deployment", "shop", "api", `labels: {app.kubernetes.io/managed-by: Helm, app.kubernetes.io/version: "2.4.1", helm.sh/chart: shop-1.7.0}, annotations: {meta.helm.sh/release-name: shop, meta.helm.sh/release-namespace: shop}`, "2", "1", "reg.io/shop/api:2.4.1"),
		},
		Pods: []unstructured.Unstructured{
			appPod(t, "shop", "web-1", "web", true, 0), appPod(t, "shop", "web-2", "web", true, 1), appPod(t, "shop", "web-3", "web", true, 0),
			appPod(t, "shop", "api-1", "api", true, 0), appPod(t, "shop", "api-2", "api", false, 7),
		},
		Services:  []unstructured.Unstructured{u(t, `{apiVersion: v1, kind: Service, metadata: {name: web, namespace: shop, annotations: {meta.helm.sh/release-name: shop, meta.helm.sh/release-namespace: shop, "link.argocd.argoproj.io/Grafana": "https://grafana.example.com/d/shop", "link.argocd.argoproj.io/Evil": "javascript:alert(1)"}}, spec: {type: ClusterIP, selector: {pod-app: web}}}`)},
		Ingresses: []unstructured.Unstructured{u(t, `{apiVersion: networking.k8s.io/v1, kind: Ingress, metadata: {name: web, namespace: shop, annotations: {meta.helm.sh/release-name: shop, meta.helm.sh/release-namespace: shop}}, spec: {tls: [{hosts: [shop.example.com]}], rules: [{host: shop.example.com}, {host: plain.example.com}, {host: "*.wild.example.com"}]}}`)},
		HPAs:      []unstructured.Unstructured{u(t, `{apiVersion: autoscaling/v2, kind: HorizontalPodAutoscaler, metadata: {name: web, namespace: shop}, spec: {maxReplicas: 3, scaleTargetRef: {kind: Deployment, name: web}}, status: {currentReplicas: 3}}`)},
		Helm: []helmMeta{
			{name: "shop", ns: "shop", status: "superseded", revision: 11, updated: "2026-10-20T10:00:00Z"},
			{name: "shop", ns: "shop", status: "deployed", revision: 12, updated: "2026-10-28T09:30:00Z"},
		},
	}
	got := buildApplications(in)
	app := byID(got, "helm|shop|shop")
	if app == nil || len(got.Apps) != 1 {
		t.Fatalf("one application per Helm release: %+v", got.Apps)
	}
	if app.GroupedBy != GroupHelm || app.Status != "warn" {
		t.Errorf("api has 1 of 2 ready, so the application is degraded: %s %s", app.GroupedBy, app.Status)
	}
	if c := app.Counts; c != (AppCounts{Workloads: 2, WorkloadsHealthy: 1, Pods: 5, PodsReady: 4, Restarts: 8}) {
		t.Errorf("counts %+v", c)
	}
	if app.Chart != "shop" || app.ChartVersion != "1.7.0" || app.AppVersion != "2.4.1" || app.Version != "2.4.1" {
		t.Errorf("versions: %q %q %q %q", app.Chart, app.ChartVersion, app.AppVersion, app.Version)
	}
	if app.Helm == nil || app.Helm.Revision != 12 || app.Helm.Revisions != 2 || app.Helm.Status != "deployed" {
		t.Errorf("helm %+v", app.Helm)
	}
	if app.LastChange != "2026-10-28T09:30:00Z" {
		t.Errorf("a Helm release is dated by its last deploy: %s", app.LastChange)
	}
	if strings.Join(app.URLs, " ") != "http://plain.example.com https://shop.example.com" {
		t.Errorf("urls (https only where TLS covers the host, wildcards skipped): %v", app.URLs)
	}
	if len(app.Links) != 1 || app.Links[0] != (AppLink{"Grafana", "https://grafana.example.com/d/shop"}) {
		t.Errorf("links from annotations, never a javascript: URL: %+v", app.Links)
	}
	kinds := map[string]int{}
	for _, m := range app.Members {
		kinds[m.Kind]++
	}
	if kinds["Deployment"] != 2 || kinds["Service"] != 1 || kinds["Ingress"] != 1 || kinds["HorizontalPodAutoscaler"] != 1 || kinds["Pod"] != 1 {
		t.Errorf("members (only the unhealthy pod is listed): %v", kinds)
	}
	if len(app.Images) != 2 || app.Images[0] != "reg.io/shop/api:2.4.1" {
		t.Errorf("images %v", app.Images)
	}
	assertNoNilSlices(t, "Applications", got)
}

func TestApplicationsGroupingPriorityAndFallbacks(t *testing.T) {
	wl := func(name, ns, labels string) unstructured.Unstructured {
		return appWL(t, "Deployment", ns, name, "labels: {"+labels+"}", "1", "1", "x:1")
	}
	in := appsInput{Workloads: []unstructured.Unstructured{
		wl("a", "n1", "app.kubernetes.io/part-of: platform, app.kubernetes.io/instance: a-inst, app: a"),
		wl("b", "n1", "app.kubernetes.io/part-of: platform"),
		wl("c", "n1", "app.kubernetes.io/instance: c-inst, app: c"),
		wl("d", "n1", "app: legacy"),
		wl("e", "n2", "k8s-app: oldstyle"),
		wl("f", "n1", "app.kubernetes.io/managed-by: Helm, app.kubernetes.io/instance: from-label"),
		wl("g", "n1", "tier: backend"),
	}}
	got := buildApplications(in)
	for _, want := range []string{"part-of|n1|platform", "instance|n1|c-inst", "app|n1|legacy", "app|n2|oldstyle", "helm|n1|from-label"} {
		if byID(got, want) == nil {
			t.Errorf("missing %s in %v", want, ids(got))
		}
	}
	if p := byID(got, "part-of|n1|platform"); p == nil || p.Counts.Workloads != 2 {
		t.Errorf("part-of wins over instance and app, and joins a and b: %+v", p)
	}
	if got.UnassignedTotal != 1 || got.Unassigned[0].Name != "g" {
		t.Errorf("an unlabelled workload is unassigned, not guessed: %+v", got.Unassigned)
	}
	in.GroupByNamespace = true
	got = buildApplications(in)
	if byID(got, "namespace|n1|n1") == nil || got.UnassignedTotal != 0 {
		t.Errorf("with the namespace fallback on, the rest is grouped by namespace: %v / %d", ids(got), got.UnassignedTotal)
	}
	if ns := byID(got, "namespace|n1|n1"); ns == nil || ns.GroupedBy != GroupNamespace || ns.Counts.Workloads != 1 {
		t.Errorf("%+v", ns)
	}
}

func ids(a Applications) []string {
	var out []string
	for _, x := range a.Apps {
		out = append(out, x.ID)
	}
	return out
}

func TestApplicationsSameNameInTwoNamespacesStaysSeparate(t *testing.T) {
	l := "labels: {app.kubernetes.io/part-of: shop}"
	got := buildApplications(appsInput{Workloads: []unstructured.Unstructured{
		appWL(t, "Deployment", "staging", "web", l, "1", "1", "x:1"), appWL(t, "Deployment", "prod", "web", l, "1", "1", "x:1"),
	}})
	if len(got.Apps) != 2 {
		t.Fatalf("environments must not merge: %v", ids(got))
	}
}

func TestApplicationsHelmReleaseSpanningNamespacesIsOneApplication(t *testing.T) {
	m := func(ns string) string {
		return fmt.Sprintf("annotations: {meta.helm.sh/release-name: platform, meta.helm.sh/release-namespace: infra}, labels: {x: %s}", ns)
	}
	got := buildApplications(appsInput{Workloads: []unstructured.Unstructured{
		appWL(t, "Deployment", "infra", "a", m("1"), "1", "1", "x:1"), appWL(t, "Deployment", "monitoring", "b", m("2"), "1", "1", "x:1"),
	}})
	app := byID(got, "helm|infra|platform")
	if len(got.Apps) != 1 || app == nil || strings.Join(app.Namespaces, ",") != "infra,monitoring" {
		t.Fatalf("%v %+v", ids(got), app)
	}
}

func TestApplicationsReleaseProblemsAndReleasesWithoutWorkloads(t *testing.T) {
	var metas []helmMeta
	for i := 1; i <= 60; i++ {
		metas = append(metas, helmMeta{name: "heavy", ns: "ops", status: "superseded", revision: i, updated: "2026-01-01T00:00:00Z"})
	}
	metas = append(metas,
		helmMeta{name: "broken", ns: "kong", status: "failed", revision: 3, updated: "2026-10-01T00:00:00Z"},
		helmMeta{name: "stuck", ns: "kong", status: "pending-upgrade", revision: 2, updated: "2026-10-01T00:00:00Z"},
	)
	got := buildApplications(appsInput{Helm: metas})
	br := byID(got, "helm|kong|broken")
	if br == nil || br.Status != "error" || len(br.Issues) < 2 {
		t.Fatalf("a failed release is an error, and says it has no workloads: %+v", br)
	}
	if st := byID(got, "helm|kong|stuck"); st == nil || st.Status != "warn" || !strings.Contains(st.Issues[len(st.Issues)-1].Message, "pending-upgrade") {
		t.Errorf("%+v", st)
	}
	hv := byID(got, "helm|ops|heavy")
	found := false
	for _, is := range hv.Issues {
		if strings.Contains(is.Message, "60 revisions") && strings.Contains(is.Message, "--history-max") {
			found = true
		}
	}
	if !found {
		t.Errorf("history bloat is flagged with the fix: %+v", hv.Issues)
	}
	if got.Apps[0].Status != "error" {
		t.Errorf("problems sort first: %v", ids(got))
	}
}

func TestApplicationsServiceAndIngressJoinThroughWhatTheyPointAt(t *testing.T) {
	// nothing carries a Helm annotation: the service joins by its selector, the ingress by its backend
	got := buildApplications(appsInput{
		Workloads: []unstructured.Unstructured{appWL(t, "Deployment", "d", "web", "labels: {app: web}", "1", "1", "x:1")},
		Pods:      []unstructured.Unstructured{appPod(t, "d", "web-1", "web", true, 0)},
		Services:  []unstructured.Unstructured{u(t, `{apiVersion: v1, kind: Service, metadata: {name: front, namespace: d}, spec: {selector: {pod-app: web}}}`), u(t, `{apiVersion: v1, kind: Service, metadata: {name: other, namespace: d}, spec: {selector: {pod-app: nobody}}}`)},
		Ingresses: []unstructured.Unstructured{u(t, `{apiVersion: networking.k8s.io/v1, kind: Ingress, metadata: {name: i, namespace: d}, spec: {rules: [{host: web.example.com, http: {paths: [{path: /, backend: {service: {name: front, port: {number: 80}}}}]}}]}}`)},
	})
	app := byID(got, "app|d|web")
	n := map[string]bool{}
	for _, m := range app.Members {
		n[m.Kind+"/"+m.Name] = true
	}
	if !n["Service/front"] || !n["Ingress/i"] || n["Service/other"] {
		t.Errorf("members %v", n)
	}
	if len(app.URLs) != 1 || app.URLs[0] != "http://web.example.com" {
		t.Errorf("%v", app.URLs)
	}
}

func TestApplicationsVersionFallsBackToImageTagThenChart(t *testing.T) {
	one := buildApplications(appsInput{Workloads: []unstructured.Unstructured{appWL(t, "Deployment", "d", "x", "labels: {app: x}", "1", "1", "reg.io/x:v9")}})
	if v := one.Apps[0].Version; v != "v9" {
		t.Errorf("a single image tag is the version: %q", v)
	}
	many := buildApplications(appsInput{Workloads: []unstructured.Unstructured{
		appWL(t, "Deployment", "d", "x", "labels: {app: x, helm.sh/chart: x-3.1.0}", "1", "1", "reg.io/x:v9"),
		appWL(t, "Deployment", "d", "y", "labels: {app: x, helm.sh/chart: x-3.1.0}", "1", "1", "reg.io/y:v1"),
	}})
	if v := many.Apps[0].Version; v != "3.1.0" {
		t.Errorf("with several different tags the chart version is used: %q", v)
	}
	digest := buildApplications(appsInput{Workloads: []unstructured.Unstructured{appWL(t, "Deployment", "d", "x", "labels: {app: x}", "1", "1", "reg.io/x@sha256:abc")}})
	if v := digest.Apps[0].Version; v != "" {
		t.Errorf("a digest is not a version: %q", v)
	}
}

func TestSplitChartAndImageTag(t *testing.T) {
	for in, want := range map[string][2]string{
		"shop-1.7.0": {"shop", "1.7.0"}, "my-cool-chart-0.3.1-rc.1": {"my-cool-chart", "0.3.1-rc.1"}, "k10-7.0.5": {"k10", "7.0.5"}, "nover": {"nover", ""},
	} {
		if n, v := splitChart(in); n != want[0] || v != want[1] {
			t.Errorf("%s → %s %s", in, n, v)
		}
	}
	for in, want := range map[string]string{"nginx": "", "nginx:1.25": "1.25", "reg.io:5000/x": "", "reg.io:5000/x:2": "2", "x@sha256:1": ""} {
		if got := imageTag(in); got != want {
			t.Errorf("%s → %q", in, got)
		}
	}
}

func TestApplicationsScaledToZeroIsNotAProblem(t *testing.T) {
	got := buildApplications(appsInput{Workloads: []unstructured.Unstructured{appWL(t, "Deployment", "d", "idle", "labels: {app: idle}", "0", "0", "x:1")}})
	if a := got.Apps[0]; a.Status != "ok" || a.Counts.WorkloadsHealthy != 1 {
		t.Errorf("%+v", a)
	}
}

func TestApplicationsEmptyAndCaps(t *testing.T) {
	assertNoNilSlices(t, "empty", buildApplications(appsInput{}))
	var ws []unstructured.Unstructured
	for i := 0; i < 300; i++ {
		ws = append(ws, appWL(t, "Deployment", "d", fmt.Sprintf("w%03d", i), "labels: {tier: x}", "1", "1", "x:1"))
	}
	got := buildApplications(appsInput{Workloads: ws})
	if got.UnassignedTotal != 300 || len(got.Unassigned) != appMaxUnassigned {
		t.Errorf("the unassigned list is capped but counted: %d / %d", len(got.Unassigned), got.UnassignedTotal)
	}
}

func TestApplicationsAtScaleIsFast(t *testing.T) {
	var ws, ps []unstructured.Unstructured
	for i := 0; i < 500; i++ {
		ns := fmt.Sprintf("ns%03d", i%100)
		name := fmt.Sprintf("w%03d", i)
		ws = append(ws, appWL(t, "Deployment", ns, name, "labels: {app.kubernetes.io/part-of: "+name+"}", "3", "3", "x:1"))
		for j := 0; j < 5; j++ {
			ps = append(ps, appPod(t, ns, fmt.Sprintf("%s-%d", name, j), name, true, 0))
		}
	}
	got := buildApplications(appsInput{Workloads: ws, Pods: ps})
	if len(got.Apps) != 500 {
		t.Fatalf("%d", len(got.Apps))
	}
	if got.Apps[0].Counts.Pods != 5 {
		t.Errorf("%+v", got.Apps[0].Counts)
	}
}
