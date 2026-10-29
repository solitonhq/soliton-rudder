package kube

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
)

type DrainPod struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Owner     string `json:"owner"`
	Action    string `json:"action"`   // evict | skip
	Severity  string `json:"severity"` // ok | warn | error
	Reason    string `json:"reason"`
	MovesTo   string `json:"movesTo"` // node the scheduler is expected to pick ("" if none fits)
	CPUm      int64  `json:"cpuM"`
	Mem       int64  `json:"mem"`
}

type PDBImpact struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Rule      string `json:"rule"` // "minAvailable 2" / "maxUnavailable 25%"
	Matching  int    `json:"matching"`
	Healthy   int    `json:"healthy"`
	Allowed   int    `json:"allowed"` // disruptions allowed right now
	Evicting  int    `json:"evicting"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
}

type CapacitySummary struct {
	FreeCPUm int64 `json:"freeCpuM"` // across the remaining schedulable nodes
	FreeMem  int64 `json:"freeMem"`
	NeedCPUm int64 `json:"needCpuM"`
	NeedMem  int64 `json:"needMem"`
	Nodes    int   `json:"nodes"`
}

type HPADrill struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Target    string `json:"target"`
	Current   int    `json:"current"`
	Max       int    `json:"max"`
	Fits      int    `json:"fits"` // extra replicas that still fit
	Severity  string `json:"severity"`
	Message   string `json:"message"`
}

type DrainResult struct {
	Node     string          `json:"node"`
	Verdict  string          `json:"verdict"` // safe | disruptive | blocked
	Summary  []string        `json:"summary"`
	Pods     []DrainPod      `json:"pods"`
	PDBs     []PDBImpact     `json:"pdbs"`
	Capacity CapacitySummary `json:"capacity"`
	HPAs     []HPADrill      `json:"hpas"`
	Notes    []string        `json:"notes"`
}

type drainInput struct {
	Nodes, Pods, PDBs, HPAs, ReplicaSets, Jobs []unstructured.Unstructured
}

// SimulateDrain predicts what `kubectl drain <node>` would do, without touching anything.
func (s *Session) SimulateDrain(ctx context.Context, node string) (DrainResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*opTimeout)
	defer cancel()
	var in drainInput
	var notes []string
	for _, c := range []struct {
		what string
		r    Resource
		dst  *[]unstructured.Unstructured
		req  bool
	}{{"nodes", nodeRes, &in.Nodes, true}, {"pods", podRes, &in.Pods, true}, {"PodDisruptionBudgets", pdbRes, &in.PDBs, false},
		{"HorizontalPodAutoscalers", hpaRes, &in.HPAs, false}, {"ReplicaSets", rsRes, &in.ReplicaSets, false}, {"Jobs", jobRes, &in.Jobs, false}} {
		items, err := s.ListScoped(ctx, c.r, "", metav1.ListOptions{})
		if err != nil {
			if c.req {
				return DrainResult{}, fmt.Errorf("reading %s: %w", c.what, err)
			}
			notes = append(notes, fmt.Sprintf("%s unavailable (%s): that part of the analysis is skipped", c.what, shortErr(err)))
			continue
		}
		*c.dst = items
	}
	r, err := simulateDrain(in, node)
	r.Notes = append(r.Notes, notes...)
	return r, err
}

type capNode struct {
	name        string
	cpu, mem    int64 // free
	taints      []map[string]any
	schedulable bool
}

func qty(v any) *resource.Quantity {
	q, _ := resource.ParseQuantity(fmt.Sprint(v))
	return &q
}

// podRequests sums the scheduler-relevant requests of a pod (max of init containers vs sum of containers).
func podRequests(p *unstructured.Unstructured) (cpuM, mem int64) {
	sum := func(key string) (c, m int64) {
		cs, _, _ := unstructured.NestedSlice(p.Object, "spec", key)
		for _, x := range cs {
			cm, _ := x.(map[string]any)
			req, _, _ := unstructured.NestedMap(cm, "resources", "requests")
			if v, ok := req["cpu"]; ok {
				c += qty(v).MilliValue()
			}
			if v, ok := req["memory"]; ok {
				m += qty(v).Value()
			}
		}
		return
	}
	cpuM, mem = sum("containers")
	if ic, im := sum("initContainers"); ic > cpuM {
		cpuM = ic
	} else if im > mem {
		mem = im
	}
	return
}

func isTerminal(p *unstructured.Unstructured) bool {
	ph, _, _ := unstructured.NestedString(p.Object, "status", "phase")
	return ph == "Succeeded" || ph == "Failed"
}

func ownerLabel(p *unstructured.Unstructured, rs, jobs map[string]*unstructured.Unstructured) (kind, name string, ok bool) {
	return controllerOwner(p, rs, jobs)
}

func tolerates(p *unstructured.Unstructured, taint map[string]any) bool {
	tols, _, _ := unstructured.NestedSlice(p.Object, "spec", "tolerations")
	for _, t := range tols {
		tm, _ := t.(map[string]any)
		if e := str(tm["effect"]); e != "" && e != str(taint["effect"]) {
			continue
		}
		op := str(tm["operator"])
		if op == "Exists" && (str(tm["key"]) == "" || str(tm["key"]) == str(taint["key"])) {
			return true
		}
		if (op == "" || op == "Equal") && str(tm["key"]) == str(taint["key"]) && str(tm["value"]) == str(taint["value"]) {
			return true
		}
	}
	return false
}

func canHost(n *capNode, p *unstructured.Unstructured) bool {
	for _, t := range n.taints {
		eff := str(t["effect"])
		if (eff == "NoSchedule" || eff == "NoExecute") && !tolerates(p, t) {
			return false
		}
	}
	return true
}

func nodeReady(n *unstructured.Unstructured) bool {
	conds, _, _ := unstructured.NestedSlice(n.Object, "status", "conditions")
	for _, c := range conds {
		cm, _ := c.(map[string]any)
		if str(cm["type"]) == "Ready" {
			return str(cm["status"]) == "True"
		}
	}
	return false
}

func simulateDrain(in drainInput, node string) (DrainResult, error) {
	res := DrainResult{Node: node, Pods: []DrainPod{}, PDBs: []PDBImpact{}, HPAs: []HPADrill{}, Summary: []string{}, Notes: []string{
		"Scheduling is estimated from CPU/memory requests, taints and nodeSelector. Pod affinity, topology spread and volume zones are not modelled.",
	}}
	var target *unstructured.Unstructured
	nodeObj := map[string]*unstructured.Unstructured{}
	for i := range in.Nodes {
		nodeObj[in.Nodes[i].GetName()] = &in.Nodes[i]
		if in.Nodes[i].GetName() == node {
			target = &in.Nodes[i]
		}
	}
	if target == nil {
		return res, fmt.Errorf("node %q not found", node)
	}
	rs := map[string]*unstructured.Unstructured{}
	for i := range in.ReplicaSets {
		rs[in.ReplicaSets[i].GetNamespace()+"/"+in.ReplicaSets[i].GetName()] = &in.ReplicaSets[i]
	}
	jobs := map[string]*unstructured.Unstructured{}
	for i := range in.Jobs {
		jobs[in.Jobs[i].GetNamespace()+"/"+in.Jobs[i].GetName()] = &in.Jobs[i]
	}

	// free capacity of every node that could receive pods
	used := map[string][2]int64{}
	for i := range in.Pods {
		p := &in.Pods[i]
		nn, _, _ := unstructured.NestedString(p.Object, "spec", "nodeName")
		if nn == "" || isTerminal(p) {
			continue
		}
		c, m := podRequests(p)
		u := used[nn]
		used[nn] = [2]int64{u[0] + c, u[1] + m}
	}
	var caps []*capNode
	for _, n := range in.Nodes {
		if n.GetName() == node {
			continue
		}
		alloc, _, _ := unstructured.NestedMap(n.Object, "status", "allocatable")
		cn := &capNode{name: n.GetName()}
		cn.cpu = qty(alloc["cpu"]).MilliValue() - used[n.GetName()][0]
		cn.mem = qty(alloc["memory"]).Value() - used[n.GetName()][1]
		cn.taints, _ = nestedMapSlice(n.Object, "spec", "taints")
		unsched, _, _ := unstructured.NestedBool(n.Object, "spec", "unschedulable")
		cn.schedulable = !unsched && nodeReady(&n)
		if cn.schedulable {
			caps = append(caps, cn)
			res.Capacity.FreeCPUm += max64(cn.cpu, 0)
			res.Capacity.FreeMem += max64(cn.mem, 0)
		}
	}
	res.Capacity.Nodes = len(caps)

	// readiness per controller, to spot single-replica workloads
	readyOf := map[string]int{}
	totalOf := map[string]int{}
	ctrlKey := func(p *unstructured.Unstructured) string {
		if k, n, ok := ownerLabel(p, rs, jobs); ok {
			return p.GetNamespace() + "/" + k + "/" + n
		}
		return ""
	}
	for i := range in.Pods {
		p := &in.Pods[i]
		if isTerminal(p) {
			continue
		}
		if k := ctrlKey(p); k != "" {
			totalOf[k]++
			if podReady(p) {
				readyOf[k]++
			}
		}
	}

	type pending struct {
		idx int
		p   *unstructured.Unstructured
	}
	var toPlace []pending
	evictedByPDB := map[string]int{}
	var pdbs []*unstructured.Unstructured
	for i := range in.PDBs {
		pdbs = append(pdbs, &in.PDBs[i])
	}

	unschedulable, _, _ := unstructured.NestedBool(target.Object, "spec", "unschedulable")
	if !unschedulable {
		res.Notes = append(res.Notes, "The node is not cordoned yet: drain cordons it first, so nothing new will be scheduled there.")
	}

	for i := range in.Pods {
		p := &in.Pods[i]
		if nn, _, _ := unstructured.NestedString(p.Object, "spec", "nodeName"); nn != node {
			continue
		}
		dp := DrainPod{Namespace: p.GetNamespace(), Name: p.GetName(), Action: "evict", Severity: "ok", Reason: "will be evicted and rescheduled"}
		dp.CPUm, dp.Mem = podRequests(p)
		kind, oname, owned := ownerLabel(p, rs, jobs)
		if owned {
			dp.Owner = kind + "/" + oname
		}
		switch {
		case isTerminal(p):
			dp.Action, dp.Reason = "skip", "already finished"
		case p.GetAnnotations()["kubernetes.io/config.mirror"] != "":
			dp.Action, dp.Reason = "skip", "static (mirror) pod: ignored by drain"
		case owned && kind == "DaemonSet":
			dp.Action, dp.Reason = "skip", "DaemonSet pod: ignored by drain, stays on the node"
		case !owned:
			dp.Severity, dp.Reason = "error", "not managed by a controller: drain refuses (needs --force) and the pod will not come back"
		default:
			if hasEmptyDir(p) {
				dp.Severity, dp.Reason = "warn", "uses emptyDir: drain needs --delete-emptydir-data and the data is lost"
			}
			if totalOf[ctrlKey(p)] <= 1 {
				if dp.Severity == "ok" {
					dp.Severity = "warn"
				}
				dp.Reason += "; the only replica of " + dp.Owner + ": expect downtime while it restarts"
			}
		}
		res.Pods = append(res.Pods, dp)
		if dp.Action == "evict" && dp.Severity != "error" {
			toPlace = append(toPlace, pending{len(res.Pods) - 1, p})
		}
		if dp.Action == "evict" && podReady(p) {
			for _, b := range pdbs {
				if b.GetNamespace() == p.GetNamespace() && pdbSelects(b, p) {
					evictedByPDB[b.GetNamespace()+"/"+b.GetName()]++
				}
			}
		}
	}

	// PDB impact
	for _, b := range pdbs {
		key := b.GetNamespace() + "/" + b.GetName()
		k := evictedByPDB[key]
		if k == 0 {
			continue
		}
		imp := pdbImpact(b, in.Pods, k)
		res.PDBs = append(res.PDBs, imp)
	}

	// placement: biggest pods first, first node that fits
	sort.SliceStable(toPlace, func(i, j int) bool {
		a, b := res.Pods[toPlace[i].idx], res.Pods[toPlace[j].idx]
		return a.CPUm+a.Mem/(1<<20) > b.CPUm+b.Mem/(1<<20)
	})
	for _, pe := range toPlace {
		dp := &res.Pods[pe.idx]
		res.Capacity.NeedCPUm += dp.CPUm
		res.Capacity.NeedMem += dp.Mem
		var best *capNode
		for _, c := range caps {
			if !canHost(c, pe.p) || !selectorFits(pe.p, nodeObj[c.name]) || c.cpu < dp.CPUm || c.mem < dp.Mem {
				continue
			}
			if best == nil || c.cpu+c.mem/(1<<20) > best.cpu+best.mem/(1<<20) {
				best = c
			}
		}
		if best == nil {
			dp.Severity = "error"
			dp.Reason += "; no other node has room for it (it would stay Pending)"
			continue
		}
		best.cpu -= dp.CPUm
		best.mem -= dp.Mem
		dp.MovesTo = best.name
	}

	// HPA scale-out drill on what is left after the drain
	for _, h := range in.HPAs {
		d := hpaDrill(&h, in.Pods, rs, jobs, caps)
		if d != nil {
			res.HPAs = append(res.HPAs, *d)
		}
	}

	// verdict
	blocked, disruptive := false, false
	for _, p := range res.Pods {
		if p.Severity == "error" {
			blocked = true
			res.Summary = append(res.Summary, fmt.Sprintf("%s/%s: %s", p.Namespace, p.Name, p.Reason))
		} else if p.Severity == "warn" {
			disruptive = true
		}
	}
	for _, b := range res.PDBs {
		if b.Severity == "error" {
			blocked = true
			res.Summary = append(res.Summary, fmt.Sprintf("PDB %s/%s: %s", b.Namespace, b.Name, b.Message))
		} else if b.Severity == "warn" {
			disruptive = true
		}
	}
	for _, h := range res.HPAs {
		if h.Severity != "ok" {
			disruptive = true
		}
	}
	switch {
	case blocked:
		res.Verdict = "blocked"
	case disruptive:
		res.Verdict = "disruptive"
		res.Summary = append(res.Summary, "The drain will complete, but some workloads see downtime, slow evictions or reduced scale-out headroom.")
	default:
		res.Verdict = "safe"
		res.Summary = append(res.Summary, "All pods can be evicted without violating a PodDisruptionBudget, and there is room to reschedule them.")
	}
	return res, nil
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func nestedMapSlice(o map[string]any, f ...string) ([]map[string]any, bool) {
	s, found, _ := unstructured.NestedSlice(o, f...)
	var out []map[string]any
	for _, x := range s {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, found
}

func hasEmptyDir(p *unstructured.Unstructured) bool {
	vols, _, _ := unstructured.NestedSlice(p.Object, "spec", "volumes")
	for _, v := range vols {
		if vm, ok := v.(map[string]any); ok {
			if _, ok := vm["emptyDir"]; ok {
				return true
			}
		}
	}
	return false
}

func selectorFits(p, node *unstructured.Unstructured) bool {
	sel, _, _ := unstructured.NestedStringMap(p.Object, "spec", "nodeSelector")
	if node == nil {
		return len(sel) == 0
	}
	return labels.SelectorFromSet(sel).Matches(labels.Set(node.GetLabels()))
}

func pdbSelects(b, p *unstructured.Unstructured) bool {
	sel, _, _ := unstructured.NestedMap(b.Object, "spec", "selector")
	if sel == nil {
		return false
	}
	return selectorMatches(sel, p.GetLabels())
}

// intOrPercent resolves "2" or "25%" against total; percentages round up like the disruption controller.
func intOrPercent(v any, total int) (int, bool) {
	switch x := v.(type) {
	case nil:
		return 0, false
	case string:
		if strings.HasSuffix(x, "%") {
			pct, err := strconv.Atoi(strings.TrimSuffix(x, "%"))
			if err != nil {
				return 0, false
			}
			return int(math.Ceil(float64(pct) * float64(total) / 100)), true
		}
		n, err := strconv.Atoi(x)
		return n, err == nil
	default:
		n, err := strconv.Atoi(fmt.Sprint(x))
		return n, err == nil
	}
}

func pdbImpact(b *unstructured.Unstructured, pods []unstructured.Unstructured, evicting int) PDBImpact {
	imp := PDBImpact{Namespace: b.GetNamespace(), Name: b.GetName(), Evicting: evicting, Severity: "ok"}
	for i := range pods {
		p := &pods[i]
		if p.GetNamespace() != b.GetNamespace() || isTerminal(p) || !pdbSelects(b, p) {
			continue
		}
		imp.Matching++
		if podReady(p) {
			imp.Healthy++
		}
	}
	minV, hasMin, _ := unstructured.NestedFieldNoCopy(b.Object, "spec", "minAvailable")
	maxV, hasMax, _ := unstructured.NestedFieldNoCopy(b.Object, "spec", "maxUnavailable")
	desired := 0
	switch {
	case hasMin && minV != nil:
		desired, _ = intOrPercent(minV, imp.Matching)
		imp.Rule = fmt.Sprintf("minAvailable %v", minV)
	case hasMax && maxV != nil:
		mu, _ := intOrPercent(maxV, imp.Matching)
		desired = imp.Matching - mu
		imp.Rule = fmt.Sprintf("maxUnavailable %v", maxV)
	}
	imp.Allowed = imp.Healthy - desired
	if imp.Allowed < 0 {
		imp.Allowed = 0
	}
	switch {
	case imp.Allowed == 0:
		imp.Severity = "error"
		imp.Message = fmt.Sprintf("allows no disruption right now (%d of %d healthy, %s): drain would wait forever on %d pod(s)", imp.Healthy, imp.Matching, imp.Rule, evicting)
	case evicting > imp.Allowed:
		imp.Severity = "warn"
		imp.Message = fmt.Sprintf("allows %d disruption(s) but %d pods are on this node: evictions will proceed one by one as replacements become ready", imp.Allowed, evicting)
	default:
		imp.Message = fmt.Sprintf("allows %d disruption(s); %d pod(s) evicted: fine", imp.Allowed, evicting)
	}
	return imp
}

// hpaDrill checks that an autoscaler can still reach maxReplicas on the capacity left after the drain.
func hpaDrill(h *unstructured.Unstructured, pods []unstructured.Unstructured, rs, jobs map[string]*unstructured.Unstructured, caps []*capNode) *HPADrill {
	kind, _, _ := unstructured.NestedString(h.Object, "spec", "scaleTargetRef", "kind")
	name, _, _ := unstructured.NestedString(h.Object, "spec", "scaleTargetRef", "name")
	mx, _, _ := unstructured.NestedInt64(h.Object, "spec", "maxReplicas")
	d := &HPADrill{Namespace: h.GetNamespace(), Name: h.GetName(), Target: kind + "/" + name, Max: int(mx), Severity: "ok"}
	var perCPU, perMem int64
	for i := range pods {
		p := &pods[i]
		if p.GetNamespace() != h.GetNamespace() || isTerminal(p) {
			continue
		}
		if k, n, ok := controllerOwner(p, rs, jobs); ok && k == kind && n == name {
			d.Current++
			c, m := podRequests(p)
			perCPU, perMem = max64(perCPU, c), max64(perMem, m)
		}
	}
	if d.Current == 0 {
		return nil
	}
	extra := d.Max - d.Current
	if extra <= 0 {
		d.Message = "already at its maximum replica count"
		return d
	}
	for _, c := range caps {
		n := math.MaxInt32
		if perCPU > 0 {
			n = int(max64(c.cpu, 0) / perCPU)
		}
		if perMem > 0 {
			if m := int(max64(c.mem, 0) / perMem); m < n {
				n = m
			}
		}
		if n == math.MaxInt32 {
			n = extra // no requests: nothing limits it by the scheduler's arithmetic
		}
		d.Fits += n
	}
	if d.Fits >= extra {
		d.Message = fmt.Sprintf("can still scale from %d to %d (room for %d more)", d.Current, d.Max, d.Fits)
		return d
	}
	d.Severity = "warn"
	if d.Fits == 0 {
		d.Severity = "error"
	}
	d.Message = fmt.Sprintf("could not reach %d replicas: room for only %d of the %d extra", d.Max, d.Fits, extra)
	return d
}
