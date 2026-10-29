package kube

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Application grouping. Kubernetes has no "application" object, so one is recognised from what tools leave behind. In
// this order: a Helm release, the recommended labels (part-of, instance), the legacy app label, and optionally the
// namespace. Every application says how it was grouped, so a wrong guess can be understood and corrected.

const (
	GroupHelm      = "helm"
	GroupPartOf    = "part-of"
	GroupInstance  = "instance"
	GroupApp       = "app"
	GroupNamespace = "namespace"

	appHistoryBloat     = 50
	appMaxMembers       = 200
	appMaxProblemPods   = 20
	appMaxUnassigned    = 200
	argoLinkAnnotPrefix = "link.argocd.argoproj.io/"
)

type AppMember struct {
	Kind        string `json:"kind"`
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	ResourceKey string `json:"resourceKey"`
	Status      string `json:"status"` // ok | warn | error | unknown
	Detail      string `json:"detail"`
}

type AppHelm struct {
	Release   string `json:"release"`
	Namespace string `json:"namespace"`
	Revision  int    `json:"revision"`
	Status    string `json:"status"`
	Updated   string `json:"updated"`
	Revisions int    `json:"revisions"` // revisions kept in the cluster
}

type AppIssue struct {
	Severity string `json:"severity"` // error | warn | info
	Message  string `json:"message"`
}

type AppLink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type AppCounts struct {
	Workloads        int `json:"workloads"`
	WorkloadsHealthy int `json:"workloadsHealthy"`
	Pods             int `json:"pods"`
	PodsReady        int `json:"podsReady"`
	Restarts         int `json:"restarts"`
}

type Application struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	GroupedBy    string      `json:"groupedBy"`
	Namespaces   []string    `json:"namespaces"`
	Status       string      `json:"status"`
	Counts       AppCounts   `json:"counts"`
	Version      string      `json:"version"`
	Chart        string      `json:"chart"`
	ChartVersion string      `json:"chartVersion"`
	AppVersion   string      `json:"appVersion"`
	Images       []string    `json:"images"`
	Helm         *AppHelm    `json:"helm"`
	URLs         []string    `json:"urls"`
	Links        []AppLink   `json:"links"` // from annotations on its objects
	Members      []AppMember `json:"members"`
	LastChange   string      `json:"lastChange"`
	Issues       []AppIssue  `json:"issues"`
}

type Applications struct {
	Apps            []Application `json:"apps"`
	Unassigned      []AppMember   `json:"unassigned"`
	UnassignedTotal int           `json:"unassignedTotal"`
	Notes           []string      `json:"notes"`
}

type appsInput struct {
	Workloads, Pods, Services, Ingresses, HPAs []unstructured.Unstructured
	Helm                                       []helmMeta // every revision of every release
	GroupByNamespace                           bool
}

type appKey struct{ src, ns, name string }

func (k appKey) id() string { return k.src + "|" + k.ns + "|" + k.name }

// keyOf says which application an object belongs to by its own labels and annotations.
func keyOf(o *unstructured.Unstructured, byNamespace bool) (appKey, bool) {
	a, l := o.GetAnnotations(), o.GetLabels()
	if rel := a["meta.helm.sh/release-name"]; rel != "" {
		ns := a["meta.helm.sh/release-namespace"]
		if ns == "" {
			ns = o.GetNamespace()
		}
		return appKey{GroupHelm, ns, rel}, true
	}
	if l["app.kubernetes.io/managed-by"] == "Helm" && l["app.kubernetes.io/instance"] != "" {
		return appKey{GroupHelm, o.GetNamespace(), l["app.kubernetes.io/instance"]}, true
	}
	if v := l["app.kubernetes.io/part-of"]; v != "" {
		return appKey{GroupPartOf, o.GetNamespace(), v}, true
	}
	if v := l["app.kubernetes.io/instance"]; v != "" {
		return appKey{GroupInstance, o.GetNamespace(), v}, true
	}
	for _, k := range []string{"app", "k8s-app"} {
		if v := l[k]; v != "" {
			return appKey{GroupApp, o.GetNamespace(), v}, true
		}
	}
	if byNamespace && o.GetNamespace() != "" {
		return appKey{GroupNamespace, o.GetNamespace(), o.GetNamespace()}, true
	}
	return appKey{}, false
}

func selectorOf(o *unstructured.Unstructured) map[string]string {
	m, _, _ := unstructured.NestedStringMap(o.Object, "spec", "selector", "matchLabels")
	return m
}

func subsetOf(sel, labels map[string]string) bool {
	if len(sel) == 0 {
		return false
	}
	for k, v := range sel {
		if labels[k] != v {
			return false
		}
	}
	return true
}

var chartLabel = regexp.MustCompile(`^(.+?)-(v?\d+\.\d+.*)$`)

func splitChart(label string) (name, version string) {
	if m := chartLabel.FindStringSubmatch(label); m != nil {
		return m[1], m[2]
	}
	return label, ""
}

func podTemplate(w *unstructured.Unstructured) []string {
	path := []string{"spec", "template", "spec"}
	if w.GetKind() == "CronJob" {
		path = []string{"spec", "jobTemplate", "spec", "template", "spec"}
	}
	var images []string
	for _, f := range []string{"containers", "initContainers"} {
		cs, _, _ := unstructured.NestedSlice(w.Object, append(append([]string{}, path...), f)...)
		for _, c := range cs {
			if cm, ok := c.(map[string]any); ok {
				if img := str(cm["image"]); img != "" {
					images = append(images, img)
				}
			}
		}
	}
	return images
}

// imageTag returns the tag of an image reference ("" for digests and untagged images).
func imageTag(img string) string {
	if i := strings.Index(img, "@"); i >= 0 {
		return ""
	}
	if i := strings.LastIndex(img, ":"); i > strings.LastIndex(img, "/") {
		return img[i+1:]
	}
	return ""
}

func worstStatus(a, b string) string {
	rank := map[string]int{"": 0, "unknown": 1, "ok": 2, "warn": 3, "error": 4}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func parseRFC(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func ingressHostsWithScheme(ing *unstructured.Unstructured) []string {
	secure := map[string]bool{}
	tls, _, _ := unstructured.NestedSlice(ing.Object, "spec", "tls")
	for _, t := range tls {
		tm, _ := t.(map[string]any)
		for _, h := range strSlice(tm["hosts"]) {
			secure[h] = true
		}
	}
	var out []string
	rules, _, _ := unstructured.NestedSlice(ing.Object, "spec", "rules")
	for _, r := range rules {
		rm, _ := r.(map[string]any)
		h := str(rm["host"])
		if h == "" || strings.Contains(h, "*") {
			continue
		}
		scheme := "http"
		if secure[h] {
			scheme = "https"
		}
		out = append(out, scheme+"://"+h)
	}
	return out
}

func ingressBackendServices(ing *unstructured.Unstructured) []string { return ingressBackends(ing) }

func buildApplications(in appsInput) Applications {
	type acc struct {
		app      *Application
		wl       []*unstructured.Unstructured
		podSeen  map[string]bool
		nsSet    map[string]bool
		imageSet map[string]bool
		urlSet   map[string]bool
		linkSet  map[string]bool
		versions map[string]int
		charts   map[string]int
		latest   time.Time
		svcNames map[string]bool // services of the app, by ns/name
	}
	apps := map[appKey]*acc{}
	order := []appKey{}
	get := func(k appKey) *acc {
		if a := apps[k]; a != nil {
			return a
		}
		a := &acc{
			app:     &Application{ID: k.id(), Name: k.name, GroupedBy: k.src, Namespaces: []string{}, Images: []string{}, URLs: []string{}, Links: []AppLink{}, Members: []AppMember{}, Issues: []AppIssue{}, Status: "unknown"},
			podSeen: map[string]bool{}, nsSet: map[string]bool{}, imageSet: map[string]bool{}, urlSet: map[string]bool{}, linkSet: map[string]bool{},
			versions: map[string]int{}, charts: map[string]int{}, svcNames: map[string]bool{},
		}
		apps[k] = a
		order = append(order, k)
		return a
	}
	touchNS := func(a *acc, ns string) {
		if ns != "" && !a.nsSet[ns] {
			a.nsSet[ns] = true
			a.app.Namespaces = append(a.app.Namespaces, ns)
		}
	}
	harvestLinks := func(a *acc, o *unstructured.Unstructured) {
		for k, v := range o.GetAnnotations() {
			if !strings.HasPrefix(k, argoLinkAnnotPrefix) {
				continue
			}
			lv := strings.ToLower(strings.TrimSpace(v))
			if !strings.HasPrefix(lv, "http://") && !strings.HasPrefix(lv, "https://") {
				continue // never carry javascript: or other schemes into a link
			}
			title := strings.TrimPrefix(k, argoLinkAnnotPrefix)
			if id := title + "\x00" + v; !a.linkSet[id] {
				a.linkSet[id] = true
				a.app.Links = append(a.app.Links, AppLink{Title: title, URL: strings.TrimSpace(v)})
			}
		}
	}
	unassigned := Applications{Unassigned: []AppMember{}}

	// workloads
	for i := range in.Workloads {
		w := &in.Workloads[i]
		k, ok := keyOf(w, in.GroupByNamespace)
		st, why := workloadState(w)
		m := AppMember{Kind: w.GetKind(), Namespace: w.GetNamespace(), Name: w.GetName(), ResourceKey: topoResKey[w.GetKind()], Status: st, Detail: why}
		if !ok {
			unassigned.UnassignedTotal++
			if len(unassigned.Unassigned) < appMaxUnassigned {
				unassigned.Unassigned = append(unassigned.Unassigned, m)
			}
			continue
		}
		a := get(k)
		a.wl = append(a.wl, w)
		touchNS(a, w.GetNamespace())
		a.app.Members = append(a.app.Members, m)
		a.app.Counts.Workloads++
		if st == "ok" || strings.HasPrefix(why, "scaled to 0") {
			a.app.Counts.WorkloadsHealthy++
		}
		if !strings.HasPrefix(why, "scaled to 0") {
			a.app.Status = worstStatus(a.app.Status, st)
		} else if a.app.Status == "unknown" {
			a.app.Status = "ok"
		}
		for _, img := range podTemplate(w) {
			if !a.imageSet[img] {
				a.imageSet[img] = true
				a.app.Images = append(a.app.Images, img)
			}
		}
		l := w.GetLabels()
		if v := l["app.kubernetes.io/version"]; v != "" {
			a.versions[v]++
		}
		if c := l["helm.sh/chart"]; c != "" {
			a.charts[c]++
		}
		if t := w.GetCreationTimestamp().Time; t.After(a.latest) {
			a.latest = t
		}
		conds, _, _ := unstructured.NestedSlice(w.Object, "status", "conditions")
		for _, c := range conds {
			cm, _ := c.(map[string]any)
			if str(cm["type"]) == "Progressing" {
				if t := parseRFC(str(cm["lastUpdateTime"])); t.After(a.latest) {
					a.latest = t
				}
			}
		}
		harvestLinks(a, w)
	}

	// pods: matched to their workloads by selector
	podsByNS := map[string][]*unstructured.Unstructured{}
	for i := range in.Pods {
		p := &in.Pods[i]
		podsByNS[p.GetNamespace()] = append(podsByNS[p.GetNamespace()], p)
	}
	type claimed struct {
		labels map[string]string
		a      *acc
	}
	claimedByNS := map[string][]claimed{}
	for _, k := range order {
		a := apps[k]
		for _, w := range a.wl {
			sel := selectorOf(w)
			if len(sel) == 0 {
				continue
			}
			for _, p := range podsByNS[w.GetNamespace()] {
				if !subsetOf(sel, p.GetLabels()) || a.podSeen[string(p.GetUID())+p.GetName()] {
					continue
				}
				a.podSeen[string(p.GetUID())+p.GetName()] = true
				claimedByNS[p.GetNamespace()] = append(claimedByNS[p.GetNamespace()], claimed{p.GetLabels(), a})
				phase, _, _ := unstructured.NestedString(p.Object, "status", "phase")
				if phase == "Succeeded" {
					continue
				}
				a.app.Counts.Pods++
				if podReady(p) {
					a.app.Counts.PodsReady++
				}
				a.app.Counts.Restarts += int(podRestarts(p))
				st, why := podStatus(p)
				if st == "error" || (st == "warn" && why != "Terminating") {
					if n := countKind(a.app.Members, "Pod"); n < appMaxProblemPods {
						a.app.Members = append(a.app.Members, AppMember{Kind: "Pod", Namespace: p.GetNamespace(), Name: p.GetName(), ResourceKey: topoResKey["Pod"], Status: st, Detail: why})
					}
				}
			}
		}
	}

	// services, ingresses and autoscalers join an application by their own keys, or by what they point at
	svcApp := map[string]*acc{} // ns/name -> app
	for i := range in.Services {
		sv := &in.Services[i]
		var a *acc
		if k, ok := keyOf(sv, in.GroupByNamespace); ok && apps[k] != nil {
			a = apps[k]
		} else if sel, _, _ := unstructured.NestedStringMap(sv.Object, "spec", "selector"); len(sel) > 0 {
			for _, c := range claimedByNS[sv.GetNamespace()] {
				if subsetOf(sel, c.labels) {
					a = c.a
					break
				}
			}
		}
		if a == nil {
			continue
		}
		svcApp[sv.GetNamespace()+"/"+sv.GetName()] = a
		touchNS(a, sv.GetNamespace())
		typ, _, _ := unstructured.NestedString(sv.Object, "spec", "type")
		if typ == "" {
			typ = "ClusterIP"
		}
		a.app.Members = append(a.app.Members, AppMember{Kind: "Service", Namespace: sv.GetNamespace(), Name: sv.GetName(), ResourceKey: topoResKey["Service"], Status: "ok", Detail: typ})
		harvestLinks(a, sv)
	}
	for i := range in.Ingresses {
		ing := &in.Ingresses[i]
		var a *acc
		if k, ok := keyOf(ing, in.GroupByNamespace); ok && apps[k] != nil {
			a = apps[k]
		} else {
			for _, svc := range ingressBackendServices(ing) {
				if x := svcApp[ing.GetNamespace()+"/"+svc]; x != nil {
					a = x
					break
				}
			}
		}
		if a == nil {
			continue
		}
		touchNS(a, ing.GetNamespace())
		urls := ingressHostsWithScheme(ing)
		for _, u := range urls {
			if !a.urlSet[u] {
				a.urlSet[u] = true
				a.app.URLs = append(a.app.URLs, u)
			}
		}
		detail := strings.Join(urls, ", ")
		a.app.Members = append(a.app.Members, AppMember{Kind: "Ingress", Namespace: ing.GetNamespace(), Name: ing.GetName(), ResourceKey: topoResKey["Ingress"], Status: "ok", Detail: detail})
		harvestLinks(a, ing)
	}
	for i := range in.HPAs {
		h := &in.HPAs[i]
		kind, _, _ := unstructured.NestedString(h.Object, "spec", "scaleTargetRef", "kind")
		name, _, _ := unstructured.NestedString(h.Object, "spec", "scaleTargetRef", "name")
		for _, k := range order {
			a := apps[k]
			for _, w := range a.wl {
				if w.GetKind() == kind && w.GetName() == name && w.GetNamespace() == h.GetNamespace() {
					cur, _, _ := unstructured.NestedInt64(h.Object, "status", "currentReplicas")
					mx, _, _ := unstructured.NestedInt64(h.Object, "spec", "maxReplicas")
					st, d := "ok", fmt.Sprintf("%d/%d replicas", cur, mx)
					if mx > 0 && cur >= mx {
						st, d = "warn", d+" (at its maximum)"
					}
					a.app.Members = append(a.app.Members, AppMember{Kind: "HorizontalPodAutoscaler", Namespace: h.GetNamespace(), Name: h.GetName(), ResourceKey: topoResKey["HorizontalPodAutoscaler"], Status: st, Detail: d})
				}
			}
		}
	}

	// Helm releases: attach to applications, and make applications of releases that have no live workloads
	type rel struct {
		latest    helmMeta
		revisions int
	}
	releases := map[appKey]*rel{}
	for _, m := range in.Helm {
		k := appKey{GroupHelm, m.ns, m.name}
		r := releases[k]
		if r == nil {
			r = &rel{}
			releases[k] = r
		}
		r.revisions++
		if m.revision > r.latest.revision {
			r.latest = m
		}
	}
	for k, r := range releases {
		a := apps[k]
		if a == nil {
			a = get(k)
			touchNS(a, k.ns)
			a.app.Issues = append(a.app.Issues, AppIssue{"info", "This Helm release has no workloads in the cluster (or you cannot read them)."})
		}
		a.app.Helm = &AppHelm{Release: k.name, Namespace: k.ns, Revision: r.latest.revision, Status: r.latest.status, Updated: r.latest.updated, Revisions: r.revisions}
		switch {
		case r.latest.status == "failed":
			a.app.Issues = append(a.app.Issues, AppIssue{"error", fmt.Sprintf("The latest Helm revision (%d) failed to deploy.", r.latest.revision)})
			a.app.Status = worstStatus(a.app.Status, "error")
		case strings.HasPrefix(r.latest.status, "pending"):
			a.app.Issues = append(a.app.Issues, AppIssue{"warn", fmt.Sprintf("The Helm release is stuck in %q: another upgrade may be running or has been interrupted.", r.latest.status)})
			a.app.Status = worstStatus(a.app.Status, "warn")
		}
		if r.revisions > appHistoryBloat {
			a.app.Issues = append(a.app.Issues, AppIssue{"warn", fmt.Sprintf("%d revisions are kept in the cluster. Limit them with `helm upgrade --history-max` to keep it lean.", r.revisions)})
		}
	}

	// finish each application
	out := Applications{Apps: make([]Application, 0, len(order)), Unassigned: unassigned.Unassigned, UnassignedTotal: unassigned.UnassignedTotal, Notes: []string{}}
	for _, k := range order {
		a := apps[k]
		app := a.app
		sort.Strings(app.Namespaces)
		sort.Strings(app.Images)
		sort.Strings(app.URLs)
		sort.Slice(app.Links, func(i, j int) bool { return app.Links[i].Title < app.Links[j].Title })
		app.Chart, app.ChartVersion = bestChart(a.charts)
		app.AppVersion = mostCommon(a.versions)
		app.Version = app.AppVersion
		if app.Version == "" && len(app.Images) > 0 {
			tags := map[string]bool{}
			for _, img := range app.Images {
				if t := imageTag(img); t != "" {
					tags[t] = true
				}
			}
			if len(tags) == 1 {
				for t := range tags {
					app.Version = t
				}
			}
		}
		if app.Version == "" {
			app.Version = app.ChartVersion
		}
		switch {
		case app.Helm != nil && app.Helm.Updated != "":
			app.LastChange = app.Helm.Updated
		case !a.latest.IsZero():
			app.LastChange = a.latest.UTC().Format(time.RFC3339)
		}
		sort.SliceStable(app.Members, func(i, j int) bool {
			rank := map[string]int{"Deployment": 0, "StatefulSet": 0, "DaemonSet": 0, "CronJob": 0, "Pod": 1, "Service": 2, "Ingress": 3, "HorizontalPodAutoscaler": 4}
			x, y := app.Members[i], app.Members[j]
			if rank[x.Kind] != rank[y.Kind] {
				return rank[x.Kind] < rank[y.Kind]
			}
			return x.Namespace+"/"+x.Name < y.Namespace+"/"+y.Name
		})
		if len(app.Members) > appMaxMembers {
			app.Members = app.Members[:appMaxMembers]
		}
		out.Apps = append(out.Apps, *app)
	}
	sort.SliceStable(out.Apps, func(i, j int) bool {
		rank := map[string]int{"error": 0, "warn": 1, "unknown": 2, "ok": 3}
		a, b := out.Apps[i], out.Apps[j]
		if rank[a.Status] != rank[b.Status] {
			return rank[a.Status] < rank[b.Status]
		}
		return a.Name+strings.Join(a.Namespaces, ",") < b.Name+strings.Join(b.Namespaces, ",")
	})
	return out
}

func countKind(ms []AppMember, kind string) int {
	n := 0
	for _, m := range ms {
		if m.Kind == kind {
			n++
		}
	}
	return n
}

func mostCommon(m map[string]int) string {
	best, bn := "", 0
	for k, n := range m {
		if n > bn || (n == bn && k < best) {
			best, bn = k, n
		}
	}
	return best
}

func bestChart(m map[string]int) (name, version string) {
	return splitChart(mostCommon(m))
}

// Applications finds the applications of the cluster (or of the namespaces the user can read).
func (s *Session) Applications(ctx context.Context, groupByNamespace bool) (Applications, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*opTimeout)
	defer cancel()
	in := appsInput{GroupByNamespace: groupByNamespace}
	var notes []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	list := func(what string, r Resource, kind string, required bool, dst *[]unstructured.Unstructured, errOut *error) {
		defer wg.Done()
		items, err := s.ListScoped(ctx, r, "", metav1.ListOptions{})
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			if required {
				*errOut = fmt.Errorf("reading %s: %w", what, err)
			} else {
				notes = append(notes, fmt.Sprintf("%s unavailable: %s", what, shortErr(err)))
			}
			return
		}
		for i := range items {
			if kind != "" {
				items[i].SetKind(kind)
			}
		}
		*dst = append(*dst, items...)
	}
	var podErr error
	wg.Add(5)
	go list("pods", podRes, "", true, &in.Pods, &podErr)
	go list("services", svcRes, "", false, &in.Services, new(error))
	go list("ingresses", ingressRes, "", false, &in.Ingresses, new(error))
	go list("autoscalers", hpaRes, "", false, &in.HPAs, new(error))
	var helmErr error
	go func() {
		defer wg.Done()
		metas, err := s.listHelmMeta(ctx)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			helmErr = err
			return
		}
		in.Helm = metas
	}()
	for _, w := range topoWorkloadKinds {
		wg.Add(1)
		go list(strings.ToLower(w.Kind)+"s", w.Res, w.Kind, false, &in.Workloads, new(error))
	}
	wg.Wait()
	if podErr != nil {
		return Applications{}, podErr
	}
	if helmErr != nil {
		notes = append(notes, "Helm releases could not be read: "+shortErr(helmErr))
	}
	out := buildApplications(in)
	out.Notes = append(out.Notes, notes...)
	return out, nil
}
