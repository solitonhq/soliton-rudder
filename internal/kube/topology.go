package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Topology tiers: the column a node is drawn in.
const (
	TierIngress  = 0
	TierService  = 1
	TierWorkload = 2
	TierPod      = 3
	TierBackend  = 4 // nodes, config, storage
)

const (
	topoMaxNodes     = 800
	topoPodsPerOwner = 6
)

type TopoNode struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Tier        int    `json:"tier"`
	Status      string `json:"status"` // ok | warn | error | unknown
	Detail      string `json:"detail"`
	ResourceKey string `json:"resourceKey"` // empty when the object cannot be opened (missing)
	Missing     bool   `json:"missing"`
}

type TopoEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"` // routes | selects | owns | runs-on | mounts | scales
}

type Topology struct {
	Nodes     []TopoNode `json:"nodes"`
	Edges     []TopoEdge `json:"edges"`
	Truncated bool       `json:"truncated"`
	Notes     []string   `json:"notes"`
}

type topoInput struct {
	Ingresses, Services, Pods, Workloads, ReplicaSets, Jobs, HPAs, Nodes []unstructured.Unstructured
}

var (
	topoWorkloadKinds = []struct {
		Res  Resource
		Kind string
	}{
		{Resource{Group: "apps", Version: "v1", Name: "deployments", Namespaced: true}, "Deployment"},
		{Resource{Group: "apps", Version: "v1", Name: "statefulsets", Namespaced: true}, "StatefulSet"},
		{Resource{Group: "apps", Version: "v1", Name: "daemonsets", Namespaced: true}, "DaemonSet"},
		{Resource{Group: "batch", Version: "v1", Name: "cronjobs", Namespaced: true}, "CronJob"},
	}
	topoResKey = map[string]string{
		"Ingress": "networking.k8s.io/v1/ingresses", "Service": "core/v1/services", "Pod": "core/v1/pods",
		"Deployment": "apps/v1/deployments", "StatefulSet": "apps/v1/statefulsets", "DaemonSet": "apps/v1/daemonsets",
		"CronJob": "batch/v1/cronjobs", "Job": "batch/v1/jobs", "Node": "core/v1/nodes",
		"ConfigMap": "core/v1/configmaps", "Secret": "core/v1/secrets", "PersistentVolumeClaim": "core/v1/persistentvolumeclaims",
		"HorizontalPodAutoscaler": "autoscaling/v2/horizontalpodautoscalers",
	}
	nodeRes = Resource{Group: "core", Version: "v1", Name: "nodes"}
	rsRes   = Resource{Group: "apps", Version: "v1", Name: "replicasets", Namespaced: true}
	jobRes  = Resource{Group: "batch", Version: "v1", Name: "jobs", Namespaced: true}
	hpaRes  = Resource{Group: "autoscaling", Version: "v2", Name: "horizontalpodautoscalers", Namespaced: true}
)

// Topology collects the objects that make up the traffic and ownership graph of ns ("" = all).
func (s *Session) Topology(ctx context.Context, ns string) (Topology, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*opTimeout)
	defer cancel()
	var in topoInput
	var notes []string
	required := func(r Resource, dst *[]unstructured.Unstructured) error {
		items, err := s.ListScoped(ctx, r, ns, metav1.ListOptions{})
		if err != nil {
			return err
		}
		*dst = append(*dst, items...)
		return nil
	}
	optional := func(what string, r Resource, ns string, dst *[]unstructured.Unstructured) {
		items, err := s.ListScoped(ctx, r, ns, metav1.ListOptions{})
		if err != nil {
			notes = append(notes, fmt.Sprintf("%s unavailable: %v", what, shortErr(err)))
			return
		}
		*dst = append(*dst, items...)
	}
	if err := required(svcRes, &in.Services); err != nil {
		return Topology{}, err
	}
	if err := required(podRes, &in.Pods); err != nil {
		return Topology{}, err
	}
	optional("ingresses", ingressRes, ns, &in.Ingresses)
	for _, w := range topoWorkloadKinds {
		var items []unstructured.Unstructured
		optional(strings.ToLower(w.Kind)+"s", w.Res, ns, &items)
		for i := range items {
			items[i].SetKind(w.Kind)
		}
		in.Workloads = append(in.Workloads, items...)
	}
	optional("replicasets", rsRes, ns, &in.ReplicaSets)
	optional("jobs", jobRes, ns, &in.Jobs)
	optional("autoscalers", hpaRes, ns, &in.HPAs)
	optional("nodes", nodeRes, "", &in.Nodes)
	t := buildTopology(in)
	t.Notes = append(t.Notes, notes...)
	return t, nil
}

func shortErr(err error) string {
	m := err.Error()
	if len(m) > 120 {
		m = m[:120] + "…"
	}
	return m
}

type topoBuilder struct {
	nodes map[string]*TopoNode
	edges map[string]TopoEdge
	order []string
}

func tid(kind, ns, name string) string { return kind + "/" + ns + "/" + name }

func (b *topoBuilder) node(n TopoNode) *TopoNode {
	n.ID = tid(n.Kind, n.Namespace, n.Name)
	if ex := b.nodes[n.ID]; ex != nil {
		return ex
	}
	if n.ResourceKey == "" && !n.Missing {
		n.ResourceKey = topoResKey[n.Kind]
	}
	if n.Status == "" {
		n.Status = "unknown"
	}
	b.nodes[n.ID] = &n
	b.order = append(b.order, n.ID)
	return &n
}

func (b *topoBuilder) edge(from, to, kind string) {
	k := from + ">" + to + ">" + kind
	if _, ok := b.edges[k]; !ok {
		b.edges[k] = TopoEdge{From: from, To: to, Kind: kind}
	}
}

func worse(a, b string) string {
	rank := map[string]int{"unknown": 0, "ok": 1, "warn": 2, "error": 3}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// podStatus classifies a pod as ok / warn / error with a short reason.
func podStatus(p *unstructured.Unstructured) (string, string) {
	phase, _, _ := unstructured.NestedString(p.Object, "status", "phase")
	if p.GetDeletionTimestamp() != nil {
		return "warn", "Terminating"
	}
	css, _, _ := unstructured.NestedSlice(p.Object, "status", "containerStatuses")
	for _, c := range css {
		cm, _ := c.(map[string]any)
		state, _ := cm["state"].(map[string]any)
		if w, ok := state["waiting"].(map[string]any); ok {
			r := str(w["reason"])
			switch r {
			case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError", "InvalidImageName", "CreateContainerError":
				return "error", r
			}
			if r != "" {
				return "warn", r
			}
		}
		if t, ok := state["terminated"].(map[string]any); ok && str(t["reason"]) == "OOMKilled" {
			return "error", "OOMKilled"
		}
	}
	switch phase {
	case "Failed":
		reason, _, _ := unstructured.NestedString(p.Object, "status", "reason")
		if reason == "" {
			reason = "Failed"
		}
		return "error", reason
	case "Succeeded":
		return "ok", "Completed"
	case "Pending":
		return "warn", "Pending"
	case "Running":
		if podReady(p) {
			return "ok", "Running"
		}
		return "warn", "Running, not ready"
	}
	return "unknown", phase
}

// controllerOwner follows ownerReferences up to the workload shown in the graph
// (ReplicaSet → Deployment, Job → CronJob); ok is false for bare pods.
func controllerOwner(o *unstructured.Unstructured, rs, jobs map[string]*unstructured.Unstructured) (kind, name string, ok bool) {
	for _, r := range o.GetOwnerReferences() {
		if r.Controller == nil || !*r.Controller {
			continue
		}
		switch r.Kind {
		case "ReplicaSet":
			if x := rs[o.GetNamespace()+"/"+r.Name]; x != nil {
				for _, rr := range x.GetOwnerReferences() {
					if rr.Kind == "Deployment" {
						return "Deployment", rr.Name, true
					}
				}
			}
			return "ReplicaSet", r.Name, true
		case "Job":
			if x := jobs[o.GetNamespace()+"/"+r.Name]; x != nil {
				for _, rr := range x.GetOwnerReferences() {
					if rr.Kind == "CronJob" {
						return "CronJob", rr.Name, true
					}
				}
			}
			return "Job", r.Name, true
		default:
			return r.Kind, r.Name, true
		}
	}
	return "", "", false
}

func labelsMatch(sel, lbl map[string]string) bool {
	if len(sel) == 0 {
		return false
	}
	for k, v := range sel {
		if lbl[k] != v {
			return false
		}
	}
	return true
}

func buildTopology(in topoInput) Topology {
	b := &topoBuilder{nodes: map[string]*TopoNode{}, edges: map[string]TopoEdge{}}
	rs := map[string]*unstructured.Unstructured{}
	for i := range in.ReplicaSets {
		rs[in.ReplicaSets[i].GetNamespace()+"/"+in.ReplicaSets[i].GetName()] = &in.ReplicaSets[i]
	}
	jobs := map[string]*unstructured.Unstructured{}
	for i := range in.Jobs {
		jobs[in.Jobs[i].GetNamespace()+"/"+in.Jobs[i].GetName()] = &in.Jobs[i]
	}
	nodeObj := map[string]*unstructured.Unstructured{}
	for i := range in.Nodes {
		nodeObj[in.Nodes[i].GetName()] = &in.Nodes[i]
	}

	// Workloads
	for i := range in.Workloads {
		w := &in.Workloads[i]
		n := TopoNode{Kind: w.GetKind(), Name: w.GetName(), Namespace: w.GetNamespace(), Tier: TierWorkload}
		n.Status, n.Detail = workloadState(w)
		b.node(n)
	}

	// Pods, grouped under their workload
	type podEntry struct {
		p      *unstructured.Unstructured
		status string
		why    string
	}
	byOwner := map[string][]podEntry{}
	for i := range in.Pods {
		p := &in.Pods[i]
		st, why := podStatus(p)
		k, nm, ok := controllerOwner(p, rs, jobs)
		oid := tid("Pod", p.GetNamespace(), p.GetName())
		if ok {
			oid = tid(k, p.GetNamespace(), nm)
			if b.nodes[oid] == nil { // owner kind we did not list (e.g. a CRD controller): still show it
				b.node(TopoNode{Kind: k, Name: nm, Namespace: p.GetNamespace(), Tier: TierWorkload, Status: "unknown", ResourceKey: topoResKey[k]})
			}
		}
		byOwner[oid] = append(byOwner[oid], podEntry{p, st, why})
	}
	podOwner := map[string]string{} // pod id → owner id (or itself)
	shown := map[string]bool{}
	ownerIDs := make([]string, 0, len(byOwner))
	for id := range byOwner {
		ownerIDs = append(ownerIDs, id)
	}
	sort.Strings(ownerIDs)
	for _, oid := range ownerIDs {
		pods := byOwner[oid]
		sort.SliceStable(pods, func(i, j int) bool { // problems first so they are never hidden by the cap
			if pods[i].status != pods[j].status {
				return worse(pods[i].status, pods[j].status) == pods[i].status
			}
			return pods[i].p.GetName() < pods[j].p.GetName()
		})
		for _, pe := range pods {
			pid := tid("Pod", pe.p.GetNamespace(), pe.p.GetName())
			podOwner[pid] = oid
		}
		isBare := strings.HasPrefix(oid, "Pod/")
		owner := b.nodes[oid]
		var agg string
		for _, pe := range pods {
			agg = worse(agg, pe.status)
		}
		if owner != nil && owner.Status == "unknown" {
			owner.Status = agg
			owner.Detail = fmt.Sprintf("%d pod(s)", len(pods))
		}
		limit := topoPodsPerOwner
		if isBare {
			limit = 1
		}
		for i, pe := range pods {
			if i >= limit {
				break
			}
			pn := b.node(TopoNode{Kind: "Pod", Name: pe.p.GetName(), Namespace: pe.p.GetNamespace(), Tier: TierPod, Status: pe.status, Detail: pe.why})
			shown[pn.ID] = true
			if !isBare {
				b.edge(oid, pn.ID, "owns")
			}
			if nn, _, _ := unstructured.NestedString(pe.p.Object, "spec", "nodeName"); nn != "" {
				st, d := "ok", ""
				if o := nodeObj[nn]; o != nil {
					st, d = nodeStatus(o)
				}
				nd := b.node(TopoNode{Kind: "Node", Name: nn, Tier: TierBackend, Status: st, Detail: d})
				b.edge(pn.ID, nd.ID, "runs-on")
			}
		}
		if len(pods) > limit && owner != nil {
			owner.Detail += fmt.Sprintf(" · showing %d of %d pods", limit, len(pods))
		}
		// config and storage references are drawn per owner, never per pod
		for _, ref := range podRefs(pods[0].p) {
			c := b.node(TopoNode{Kind: ref.kind, Name: ref.name, Namespace: pods[0].p.GetNamespace(), Tier: TierBackend, Status: "unknown"})
			b.edge(oid, c.ID, "mounts")
			if isBare {
				b.edge(tid("Pod", pods[0].p.GetNamespace(), pods[0].p.GetName()), c.ID, "mounts")
			}
		}
	}

	// Services select pods → edges go to the pods' owners (and to the shown pods)
	for i := range in.Services {
		sv := &in.Services[i]
		sel, _, _ := unstructured.NestedStringMap(sv.Object, "spec", "selector")
		typ, _, _ := unstructured.NestedString(sv.Object, "spec", "type")
		n := TopoNode{Kind: "Service", Name: sv.GetName(), Namespace: sv.GetNamespace(), Tier: TierService}
		var total, ready int
		owners := map[string]bool{}
		for j := range in.Pods {
			p := &in.Pods[j]
			if p.GetNamespace() != sv.GetNamespace() || !labelsMatch(sel, p.GetLabels()) {
				continue
			}
			total++
			if podReady(p) {
				ready++
			}
			owners[podOwner[tid("Pod", p.GetNamespace(), p.GetName())]] = true
		}
		switch {
		case typ == "ExternalName":
			n.Status, n.Detail = "ok", "ExternalName → "+str(nested(sv.Object, "spec", "externalName"))
		case len(sel) == 0:
			n.Status, n.Detail = "unknown", "no selector (manual endpoints)"
		case total == 0:
			n.Status, n.Detail = "error", "selector matches no pods"
		case ready == 0:
			n.Status, n.Detail = "error", fmt.Sprintf("0/%d pods ready", total)
		case ready < total:
			n.Status, n.Detail = "warn", fmt.Sprintf("%d/%d pods ready", ready, total)
		default:
			n.Status, n.Detail = "ok", fmt.Sprintf("%d/%d pods ready", ready, total)
		}
		if typ != "" && typ != "ClusterIP" {
			n.Detail += " · " + typ
		}
		sn := b.node(n)
		oids := make([]string, 0, len(owners))
		for o := range owners {
			oids = append(oids, o)
		}
		sort.Strings(oids)
		for _, o := range oids {
			if b.nodes[o] != nil {
				b.edge(sn.ID, o, "selects")
			}
		}
	}

	// Ingresses
	for i := range in.Ingresses {
		ing := &in.Ingresses[i]
		n := b.node(TopoNode{Kind: "Ingress", Name: ing.GetName(), Namespace: ing.GetNamespace(), Tier: TierIngress, Status: "ok", Detail: topoIngressHosts(ing)})
		for _, svc := range ingressBackends(ing) {
			sid := tid("Service", ing.GetNamespace(), svc)
			if b.nodes[sid] == nil {
				b.node(TopoNode{Kind: "Service", Name: svc, Namespace: ing.GetNamespace(), Tier: TierService, Status: "error", Detail: "not found", Missing: true})
				n.Status = worse(n.Status, "error")
			} else {
				n.Status = worse(n.Status, b.nodes[sid].Status)
			}
			b.edge(n.ID, sid, "routes")
		}
	}

	// HPAs scale workloads
	for i := range in.HPAs {
		h := &in.HPAs[i]
		kind, _, _ := unstructured.NestedString(h.Object, "spec", "scaleTargetRef", "kind")
		name, _, _ := unstructured.NestedString(h.Object, "spec", "scaleTargetRef", "name")
		target := tid(kind, h.GetNamespace(), name)
		if b.nodes[target] == nil {
			continue
		}
		cur, _, _ := unstructured.NestedInt64(h.Object, "status", "currentReplicas")
		mx, _, _ := unstructured.NestedInt64(h.Object, "spec", "maxReplicas")
		st := "ok"
		if cur >= mx && mx > 0 {
			st = "warn"
		}
		d := fmt.Sprintf("%d/%d max replicas", cur, mx)
		if st == "warn" {
			d += " (at limit)"
		}
		hn := b.node(TopoNode{Kind: "HorizontalPodAutoscaler", Name: h.GetName(), Namespace: h.GetNamespace(), Tier: TierService, Status: st, Detail: d})
		b.edge(hn.ID, target, "scales")
	}

	return b.finish()
}

func (b *topoBuilder) finish() Topology {
	t := Topology{Nodes: []TopoNode{}, Edges: []TopoEdge{}, Notes: []string{}}
	// Nodes that are only referenced (config, nodes) with no inbound edge are impossible; drop disconnected leftovers.
	ids := append([]string(nil), b.order...)
	sort.SliceStable(ids, func(i, j int) bool {
		a, c := b.nodes[ids[i]], b.nodes[ids[j]]
		if a.Tier != c.Tier {
			return a.Tier < c.Tier
		}
		return ids[i] < ids[j]
	})
	if len(ids) > topoMaxNodes {
		// keep the problem nodes first, then the rest in tier order
		sort.SliceStable(ids, func(i, j int) bool {
			return b.nodes[ids[i]].Status == "error" && b.nodes[ids[j]].Status != "error"
		})
		ids = ids[:topoMaxNodes]
		t.Truncated = true
		t.Notes = append(t.Notes, fmt.Sprintf("Showing %d of %d objects: pick a namespace to see the rest.", topoMaxNodes, len(b.order)))
	}
	keep := map[string]bool{}
	for _, id := range ids {
		keep[id] = true
	}
	for _, id := range ids {
		t.Nodes = append(t.Nodes, *b.nodes[id])
	}
	keys := make([]string, 0, len(b.edges))
	for k := range b.edges {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		e := b.edges[k]
		if keep[e.From] && keep[e.To] {
			t.Edges = append(t.Edges, e)
		}
	}
	sort.SliceStable(t.Nodes, func(i, j int) bool {
		if t.Nodes[i].Tier != t.Nodes[j].Tier {
			return t.Nodes[i].Tier < t.Nodes[j].Tier
		}
		return t.Nodes[i].ID < t.Nodes[j].ID
	})
	return t
}

func nested(o map[string]any, f ...string) any {
	v, _, _ := unstructured.NestedFieldNoCopy(o, f...)
	return v
}

func workloadCounts(w *unstructured.Unstructured) (desired, ready int64) {
	switch w.GetKind() {
	case "DaemonSet":
		desired, _, _ = unstructured.NestedInt64(w.Object, "status", "desiredNumberScheduled")
		ready, _, _ = unstructured.NestedInt64(w.Object, "status", "numberReady")
	default:
		if v, found, _ := unstructured.NestedInt64(w.Object, "spec", "replicas"); found {
			desired = v
		} else {
			desired = 1
		}
		ready, _, _ = unstructured.NestedInt64(w.Object, "status", "readyReplicas")
	}
	return
}

func nodeStatus(n *unstructured.Unstructured) (string, string) {
	conds, _, _ := unstructured.NestedSlice(n.Object, "status", "conditions")
	st, d := "ok", "Ready"
	for _, c := range conds {
		cm, _ := c.(map[string]any)
		t, s := str(cm["type"]), str(cm["status"])
		switch {
		case t == "Ready" && s != "True":
			return "error", "NotReady"
		case t != "Ready" && s == "True":
			st, d = "warn", t
		}
	}
	if u, _, _ := unstructured.NestedBool(n.Object, "spec", "unschedulable"); u && st == "ok" {
		st, d = "warn", "Cordoned"
	}
	return st, d
}

type podRef struct{ kind, name string }

// podRefs lists the ConfigMaps, Secrets and PVCs a pod spec refers to, by name only.
func podRefs(p *unstructured.Unstructured) []podRef {
	seen := map[podRef]bool{}
	var out []podRef
	add := func(k, n string) {
		r := podRef{k, n}
		if n != "" && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	vols, _, _ := unstructured.NestedSlice(p.Object, "spec", "volumes")
	for _, v := range vols {
		vm, _ := v.(map[string]any)
		if m, ok := vm["configMap"].(map[string]any); ok {
			add("ConfigMap", str(m["name"]))
		}
		if m, ok := vm["secret"].(map[string]any); ok {
			add("Secret", str(m["secretName"]))
		}
		if m, ok := vm["persistentVolumeClaim"].(map[string]any); ok {
			add("PersistentVolumeClaim", str(m["claimName"]))
		}
		if m, ok := vm["projected"].(map[string]any); ok {
			srcs, _ := m["sources"].([]any)
			for _, s := range srcs {
				sm, _ := s.(map[string]any)
				if c, ok := sm["configMap"].(map[string]any); ok {
					add("ConfigMap", str(c["name"]))
				}
				if c, ok := sm["secret"].(map[string]any); ok {
					add("Secret", str(c["name"]))
				}
			}
		}
	}
	for _, key := range []string{"containers", "initContainers"} {
		cs, _, _ := unstructured.NestedSlice(p.Object, "spec", key)
		for _, c := range cs {
			cm, _ := c.(map[string]any)
			from, _ := cm["envFrom"].([]any)
			for _, f := range from {
				fm, _ := f.(map[string]any)
				if r, ok := fm["configMapRef"].(map[string]any); ok {
					add("ConfigMap", str(r["name"]))
				}
				if r, ok := fm["secretRef"].(map[string]any); ok {
					add("Secret", str(r["name"]))
				}
			}
			env, _ := cm["env"].([]any)
			for _, e := range env {
				em, _ := e.(map[string]any)
				vf, _ := em["valueFrom"].(map[string]any)
				if r, ok := vf["configMapKeyRef"].(map[string]any); ok {
					add("ConfigMap", str(r["name"]))
				}
				if r, ok := vf["secretKeyRef"].(map[string]any); ok {
					add("Secret", str(r["name"]))
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].kind+out[i].name < out[j].kind+out[j].name })
	return out
}

func ingressBackends(ing *unstructured.Unstructured) []string {
	var out []string
	add := func(b any) {
		bm, _ := b.(map[string]any)
		if sm, ok := bm["service"].(map[string]any); ok {
			if n := str(sm["name"]); n != "" && !contains(out, n) {
				out = append(out, n)
			}
		}
	}
	rules, _, _ := unstructured.NestedSlice(ing.Object, "spec", "rules")
	for _, r := range rules {
		rm, _ := r.(map[string]any)
		paths, _, _ := unstructured.NestedSlice(rm, "http", "paths")
		for _, p := range paths {
			pm, _ := p.(map[string]any)
			add(pm["backend"])
		}
	}
	if db, ok, _ := unstructured.NestedMap(ing.Object, "spec", "defaultBackend"); ok {
		add(db)
	}
	return out
}

func topoIngressHosts(ing *unstructured.Unstructured) string {
	var hosts []string
	rules, _, _ := unstructured.NestedSlice(ing.Object, "spec", "rules")
	for _, r := range rules {
		rm, _ := r.(map[string]any)
		if h := str(rm["host"]); h != "" && !contains(hosts, h) {
			hosts = append(hosts, h)
		}
	}
	if len(hosts) > 3 {
		return strings.Join(hosts[:3], ", ") + fmt.Sprintf(" +%d", len(hosts)-3)
	}
	return strings.Join(hosts, ", ")
}

// workloadState classifies a workload from its spec and status.
func workloadState(w *unstructured.Unstructured) (status, detail string) {
	if w.GetKind() == "CronJob" {
		if susp, _, _ := unstructured.NestedBool(w.Object, "spec", "suspend"); susp {
			return "warn", "suspended"
		}
		return "ok", "schedule " + str(nested(w.Object, "spec", "schedule"))
	}
	desired, ready := workloadCounts(w)
	switch {
	case desired == 0:
		return "warn", "scaled to 0"
	case ready == 0:
		return "error", fmt.Sprintf("0/%d ready", desired)
	case ready < desired:
		return "warn", fmt.Sprintf("%d/%d ready", ready, desired)
	}
	return "ok", fmt.Sprintf("%d/%d ready", ready, desired)
}
