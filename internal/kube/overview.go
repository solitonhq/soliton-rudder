package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type OverviewNodes struct {
	Total    int `json:"total"`
	Ready    int `json:"ready"`
	NotReady int `json:"notReady"`
	Cordoned int `json:"cordoned"`
	Pressure int `json:"pressure"` // Ready but reporting memory/disk/PID pressure
}

type OverviewPods struct {
	Total     int `json:"total"`
	Ready     int `json:"ready"`
	NotReady  int `json:"notReady"` // running but not (all) ready
	Pending   int `json:"pending"`
	Failed    int `json:"failed"`
	Succeeded int `json:"succeeded"`
}

type OverviewWorkloads struct {
	Total    int `json:"total"`
	Healthy  int `json:"healthy"`
	Degraded int `json:"degraded"`
	Failing  int `json:"failing"`
}

type OverviewCapacity struct {
	CPUAllocM     int64 `json:"cpuAllocM"`
	MemAlloc      int64 `json:"memAlloc"`
	CPURequestedM int64 `json:"cpuRequestedM"`
	MemRequested  int64 `json:"memRequested"`
	CPUUsedM      int64 `json:"cpuUsedM"`
	MemUsed       int64 `json:"memUsed"`
	HasUsage      bool  `json:"hasUsage"`
}

type Attention struct {
	Severity    string `json:"severity"` // error | warn
	Kind        string `json:"kind"`
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	Reason      string `json:"reason"`
	ResourceKey string `json:"resourceKey"`
}

type WarningEvent struct {
	At          int64  `json:"at"` // unix ms of the latest occurrence
	Namespace   string `json:"namespace"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Reason      string `json:"reason"`
	Message     string `json:"message"`
	Count       int    `json:"count"`
	ResourceKey string `json:"resourceKey"`
}

type Overview struct {
	ServerVersion  string            `json:"serverVersion"`
	Namespaces     int               `json:"namespaces"`
	Nodes          OverviewNodes     `json:"nodes"`
	Pods           OverviewPods      `json:"pods"`
	Workloads      OverviewWorkloads `json:"workloads"`
	Capacity       OverviewCapacity  `json:"capacity"`
	Attention      []Attention       `json:"attention"`
	AttentionTotal int               `json:"attentionTotal"`
	Warnings       []WarningEvent    `json:"warnings"`
	Notes          []string          `json:"notes"`
	Access         Access            `json:"access"`
}

type overviewInput struct {
	Nodes, Pods, Workloads, Events []unstructured.Unstructured
	Namespaces                     int
	NodeUsage                      map[string]Usage // nil when metrics-server is unavailable
	Now                            time.Time
}

const (
	overviewMaxAttention = 60
	overviewMaxWarnings  = 20
	overviewEventWindow  = time.Hour
	attentionPodGrace    = 5 * time.Minute
)

// ClusterOverview gathers what the Overview page shows. Optional parts that cannot be read become notes.
func (s *Session) ClusterOverview(ctx context.Context) (Overview, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*opTimeout)
	defer cancel()
	in := overviewInput{Now: time.Now()}
	var notes []string
	access := s.Access(ctx)
	list := func(r Resource, kind string, required bool, opts metav1.ListOptions) []unstructured.Unstructured {
		items, err := s.ListScoped(ctx, r, "", opts)
		if err != nil {
			if !required && (access.ClusterWide || r.Namespaced) { // a limited user cannot read cluster-scoped kinds: not worth a note
				notes = append(notes, fmt.Sprintf("%s unavailable: %s", r.Name, shortErr(err)))
			}
			return nil
		}
		for i := range items {
			if kind != "" {
				items[i].SetKind(kind)
			}
		}
		return items
	}
	pods, err := s.ListScoped(ctx, podRes, "", metav1.ListOptions{})
	if err != nil {
		return Overview{Access: access}, fmt.Errorf("reading pods: %w", err)
	}
	in.Pods = pods
	in.Nodes = list(nodeRes, "Node", false, metav1.ListOptions{})
	for _, w := range topoWorkloadKinds {
		if w.Kind == "CronJob" {
			continue
		}
		in.Workloads = append(in.Workloads, list(w.Res, w.Kind, false, metav1.ListOptions{})...)
	}
	if access.ClusterWide {
		in.Namespaces = len(list(nsRes, "", false, metav1.ListOptions{}))
	} else {
		in.Namespaces = len(access.Namespaces) // the ones this user can see
	}
	in.Events = list(eventRes, "", false, metav1.ListOptions{FieldSelector: "type=Warning"})
	if u, err := s.UsageMap(ctx, "nodes", ""); err == nil {
		in.NodeUsage = u
	}
	o := buildOverview(in)
	o.Access = access
	if !access.ClusterWide {
		o.Notes = append(o.Notes, "Limited access: figures cover only the namespaces you can read.")
	}
	o.Notes = append(o.Notes, notes...)
	if v, err := s.ServerVersion(ctx); err == nil {
		o.ServerVersion = v
	}
	return o, nil
}

func buildOverview(in overviewInput) Overview {
	o := Overview{Attention: []Attention{}, Warnings: []WarningEvent{}, Notes: []string{}, Namespaces: in.Namespaces, Access: Access{Namespaces: []string{}}}
	var attn []Attention
	add := func(sev, kind, ns, name, reason string) {
		attn = append(attn, Attention{Severity: sev, Kind: kind, Namespace: ns, Name: name, Reason: reason, ResourceKey: topoResKey[kind]})
	}

	// nodes
	for i := range in.Nodes {
		n := &in.Nodes[i]
		o.Nodes.Total++
		st, why := nodeStatus(n)
		cordoned, _, _ := unstructured.NestedBool(n.Object, "spec", "unschedulable")
		if cordoned {
			o.Nodes.Cordoned++
		}
		switch {
		case st == "error":
			o.Nodes.NotReady++
			add("error", "Node", "", n.GetName(), why)
		default:
			o.Nodes.Ready++
			if st == "warn" {
				if cordoned && why == "Cordoned" {
					add("warn", "Node", "", n.GetName(), why)
				} else {
					o.Nodes.Pressure++
					add("warn", "Node", "", n.GetName(), why)
				}
			}
		}
		if st != "error" {
			alloc, _, _ := unstructured.NestedMap(n.Object, "status", "allocatable")
			o.Capacity.CPUAllocM += qty(alloc["cpu"]).MilliValue()
			o.Capacity.MemAlloc += qty(alloc["memory"]).Value()
		}
		if u, ok := in.NodeUsage[n.GetName()]; ok {
			o.Capacity.HasUsage = true
			o.Capacity.CPUUsedM += u.CPU
			o.Capacity.MemUsed += u.Mem
		}
	}

	// pods
	for i := range in.Pods {
		p := &in.Pods[i]
		o.Pods.Total++
		phase, _, _ := unstructured.NestedString(p.Object, "status", "phase")
		st, why := podStatus(p)
		switch phase {
		case "Succeeded":
			o.Pods.Succeeded++
		case "Failed":
			o.Pods.Failed++
		case "Pending":
			o.Pods.Pending++
		default:
			if podReady(p) {
				o.Pods.Ready++
			} else {
				o.Pods.NotReady++
			}
		}
		if phase != "Succeeded" && phase != "Failed" {
			if node, _, _ := unstructured.NestedString(p.Object, "spec", "nodeName"); node != "" {
				c, m := podRequests(p)
				o.Capacity.CPURequestedM += c
				o.Capacity.MemRequested += m
			}
		}
		switch {
		case st == "error":
			add("error", "Pod", p.GetNamespace(), p.GetName(), why)
		case st == "warn" && why != "Terminating" && in.Now.Sub(p.GetCreationTimestamp().Time) > attentionPodGrace:
			add("warn", "Pod", p.GetNamespace(), p.GetName(), why)
		}
	}

	// workloads
	for i := range in.Workloads {
		w := &in.Workloads[i]
		o.Workloads.Total++
		st, why := workloadState(w)
		switch st {
		case "error":
			o.Workloads.Failing++
			add("error", w.GetKind(), w.GetNamespace(), w.GetName(), why)
		case "warn":
			if strings.HasPrefix(why, "scaled to 0") {
				o.Workloads.Healthy++ // deliberately off, not a problem
			} else {
				o.Workloads.Degraded++
				add("warn", w.GetKind(), w.GetNamespace(), w.GetName(), why)
			}
		default:
			o.Workloads.Healthy++
		}
	}

	sort.SliceStable(attn, func(i, j int) bool {
		a, b := attn[i], attn[j]
		if a.Severity != b.Severity {
			return a.Severity == "error"
		}
		rank := map[string]int{"Node": 0, "Deployment": 1, "StatefulSet": 1, "DaemonSet": 1, "Pod": 2}
		if rank[a.Kind] != rank[b.Kind] {
			return rank[a.Kind] < rank[b.Kind]
		}
		return a.Namespace+"/"+a.Name < b.Namespace+"/"+b.Name
	})
	o.AttentionTotal = len(attn)
	if len(attn) > overviewMaxAttention {
		attn = attn[:overviewMaxAttention]
	}
	if attn != nil {
		o.Attention = attn
	}

	// warning events of the last hour, merged per object and reason
	type key struct{ ns, kind, name, reason string }
	merged := map[key]*WarningEvent{}
	for i := range in.Events {
		e := &in.Events[i]
		at := eventTime(e)
		if at.IsZero() || in.Now.Sub(at) > overviewEventWindow {
			continue
		}
		io, _, _ := unstructured.NestedMap(e.Object, "involvedObject")
		ns := str(io["namespace"])
		if ns == "" {
			ns = e.GetNamespace()
		}
		k := key{ns, str(io["kind"]), str(io["name"]), str(nested(e.Object, "reason"))}
		count, _ := nested(e.Object, "count").(int64)
		if count == 0 {
			count = 1
		}
		msg := strings.TrimSpace(str(nested(e.Object, "message")))
		if len(msg) > 300 {
			msg = msg[:300] + "…"
		}
		cur := merged[k]
		if cur == nil {
			merged[k] = &WarningEvent{At: at.UnixMilli(), Namespace: k.ns, Kind: k.kind, Name: k.name, Reason: k.reason, Message: msg, Count: int(count), ResourceKey: topoResKey[k.kind]}
			continue
		}
		cur.Count += int(count)
		if at.UnixMilli() > cur.At {
			cur.At, cur.Message = at.UnixMilli(), msg
		}
	}
	ws := make([]WarningEvent, 0, len(merged))
	for _, w := range merged {
		ws = append(ws, *w)
	}
	sort.SliceStable(ws, func(i, j int) bool {
		if ws[i].At != ws[j].At {
			return ws[i].At > ws[j].At
		}
		return ws[i].Name < ws[j].Name
	})
	if len(ws) > overviewMaxWarnings {
		ws = ws[:overviewMaxWarnings]
	}
	o.Warnings = ws
	return o
}
