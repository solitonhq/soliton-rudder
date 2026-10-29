package feasibility

import (
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Cost estimates are what the *requested* resources would cost at the configured unit prices, and how much of
// that exceeds observed usage plus headroom. They are planning figures, not a cloud bill.

type NSCost struct {
	Namespace        string  `json:"namespace"`
	Pods             int     `json:"pods"`
	MonthlyCostUSD   float64 `json:"monthlyCostUsd"`
	PotentialSaveUSD float64 `json:"potentialSaveUsd"`
	RequestedCPUm    int64   `json:"requestedCpuM"`
	RequestedMem     int64   `json:"requestedMem"`
	UsedCPUm         int64   `json:"usedCpuM"`
	UsedMem          int64   `json:"usedMem"`
	UnrequestedPods  int     `json:"unrequestedPods"` // pods without any requests (cost taken from usage when known)
}

type WorkloadCost struct {
	Kind             string  `json:"kind"`
	Namespace        string  `json:"namespace"`
	Name             string  `json:"name"`
	ResourceKey      string  `json:"resourceKey"`
	Pods             int     `json:"pods"`
	MonthlyCostUSD   float64 `json:"monthlyCostUsd"`
	PotentialSaveUSD float64 `json:"potentialSaveUsd"`
	RequestedCPUm    int64   `json:"requestedCpuM"`
	RequestedMem     int64   `json:"requestedMem"`
	UsedCPUm         int64   `json:"usedCpuM"`
	UsedMem          int64   `json:"usedMem"`
}

type FinOps struct {
	Confidence       string         `json:"confidence"` // high (Prometheus p95) | low (single sample) | none (no usage data)
	Source           string         `json:"source"`
	CPUHourUSD       float64        `json:"cpuHourUsd"`
	MemGBHourUSD     float64        `json:"memGbHourUsd"`
	HeadroomPct      float64        `json:"headroomPct"`
	MonthlyCostUSD   float64        `json:"monthlyCostUsd"`
	PotentialSaveUSD float64        `json:"potentialSaveUsd"`
	Namespaces       []NSCost       `json:"namespaces"`
	Top              []WorkloadCost `json:"top"` // by potential savings
	Notes            []string       `json:"notes"`
}

const (
	minCPUm = 10
	minMem  = 32 << 20
	gb      = float64(1 << 30)
)

func monthly(cpuM, mem int64, c Config) float64 {
	return (float64(cpuM)/1000*c.CPUHourUSD + float64(mem)/gb*c.MemGBHourUSD) * hoursPerMonth
}

// RightSize is the request that fits observed usage with the configured headroom.
func RightSize(usedCPUm, usedMem int64, headroomPct float64) (cpuM, mem int64) {
	f := 1 + headroomPct/100
	cpuM = int64(float64(usedCPUm)*f + 0.5)
	mem = int64(float64(usedMem) * f)
	if cpuM < minCPUm {
		cpuM = minCPUm
	}
	if mem < minMem {
		mem = minMem
	}
	return
}

// workloadOf resolves a pod's top-level owner (Deployment via ReplicaSet, CronJob via Job, …).
func (s *Snapshot) workloadOf(p *unstructured.Unstructured, rsOwner, jobOwner map[string]string) (kind, key, name string) {
	for _, ref := range p.GetOwnerReferences() {
		switch ref.Kind {
		case "ReplicaSet":
			if d := rsOwner[p.GetNamespace()+"/"+ref.Name]; d != "" {
				return "Deployment", "apps/v1/deployments", d
			}
			return "ReplicaSet", "apps/v1/replicasets", ref.Name
		case "StatefulSet":
			return "StatefulSet", "apps/v1/statefulsets", ref.Name
		case "DaemonSet":
			return "DaemonSet", "apps/v1/daemonsets", ref.Name
		case "Job":
			if c := jobOwner[p.GetNamespace()+"/"+ref.Name]; c != "" {
				return "CronJob", "batch/v1/cronjobs", c
			}
			return "Job", "batch/v1/jobs", ref.Name
		default:
			return ref.Kind, "", ref.Name
		}
	}
	return "Pod", "core/v1/pods", p.GetName()
}

func computeFinOps(s *Snapshot) *FinOps {
	cfg := s.Cfg
	f := &FinOps{CPUHourUSD: cfg.CPUHourUSD, MemGBHourUSD: cfg.MemGBHourUSD, HeadroomPct: cfg.HeadroomPct, Namespaces: []NSCost{}, Top: []WorkloadCost{}, Notes: []string{}}
	usage := s.PromUsage
	switch {
	case usage != nil:
		f.Confidence, f.Source = "high", "Prometheus 95th percentile over 24 hours"
	case s.Usage != nil:
		usage = s.Usage
		f.Confidence, f.Source = "low", "metrics-server (a single current sample)"
		f.Notes = append(f.Notes, "Savings are based on one usage sample and can over- or under-state waste. Connect Prometheus for a 24-hour 95th-percentile basis.")
	default:
		f.Confidence = "none"
		f.Notes = append(f.Notes, "No usage data (metrics-server / Prometheus unavailable): costs are computed from requests only and potential savings are not estimated.")
	}
	if s.PromNote != "" {
		f.Notes = append(f.Notes, s.PromNote)
	}

	rsOwner := map[string]string{}
	for _, rs := range s.list("replicasets") {
		for _, ref := range rs.GetOwnerReferences() {
			if ref.Kind == "Deployment" {
				rsOwner[rs.GetNamespace()+"/"+rs.GetName()] = ref.Name
			}
		}
	}
	jobOwner := map[string]string{}
	for _, j := range s.list("jobs") {
		for _, ref := range j.GetOwnerReferences() {
			if ref.Kind == "CronJob" {
				jobOwner[j.GetNamespace()+"/"+j.GetName()] = ref.Name
			}
		}
	}

	byNS := map[string]*NSCost{}
	byWL := map[string]*WorkloadCost{}
	for i := range s.list("pods") {
		p := &s.list("pods")[i]
		if ph, _, _ := unstructured.NestedString(p.Object, "status", "phase"); ph != "Running" {
			continue
		}
		spec, _, _ := unstructured.NestedMap(p.Object, "spec")
		req := podRequests(spec)
		u, haveUsage := usage[p.GetNamespace()+"/"+p.GetName()]

		cpuM, mem := req.cpu, req.mem
		unrequested := req.cpu == 0 && req.mem == 0
		if unrequested && haveUsage {
			cpuM, mem = u.CPU, u.Mem // allocate what it actually uses
		}
		cost := monthly(cpuM, mem, cfg)
		var save float64
		if haveUsage && !unrequested && !isSystemNS(p.GetNamespace()) {
			rc, rm := RightSize(u.CPU, u.Mem, cfg.HeadroomPct)
			over := func(a, b int64) int64 {
				if a > b {
					return a - b
				}
				return 0
			}
			save = monthly(over(req.cpu, rc), over(req.mem, rm), cfg)
		}

		ns := byNS[p.GetNamespace()]
		if ns == nil {
			ns = &NSCost{Namespace: p.GetNamespace()}
			byNS[p.GetNamespace()] = ns
		}
		ns.Pods++
		ns.MonthlyCostUSD += cost
		ns.PotentialSaveUSD += save
		ns.RequestedCPUm += req.cpu
		ns.RequestedMem += req.mem
		if haveUsage {
			ns.UsedCPUm += u.CPU
			ns.UsedMem += u.Mem
		}
		if unrequested {
			ns.UnrequestedPods++
		}

		kind, key, name := s.workloadOf(p, rsOwner, jobOwner)
		wk := p.GetNamespace() + "|" + kind + "|" + name
		w := byWL[wk]
		if w == nil {
			w = &WorkloadCost{Kind: kind, ResourceKey: key, Namespace: p.GetNamespace(), Name: name}
			byWL[wk] = w
		}
		w.Pods++
		w.MonthlyCostUSD += cost
		w.PotentialSaveUSD += save
		w.RequestedCPUm += req.cpu
		w.RequestedMem += req.mem
		if haveUsage {
			w.UsedCPUm += u.CPU
			w.UsedMem += u.Mem
		}
		f.MonthlyCostUSD += cost
		f.PotentialSaveUSD += save
	}
	for _, n := range byNS {
		f.Namespaces = append(f.Namespaces, *n)
	}
	sort.Slice(f.Namespaces, func(i, j int) bool {
		a, b := f.Namespaces[i], f.Namespaces[j]
		if a.PotentialSaveUSD != b.PotentialSaveUSD {
			return a.PotentialSaveUSD > b.PotentialSaveUSD
		}
		if a.MonthlyCostUSD != b.MonthlyCostUSD {
			return a.MonthlyCostUSD > b.MonthlyCostUSD
		}
		return a.Namespace < b.Namespace
	})
	var wls []WorkloadCost
	for _, w := range byWL {
		wls = append(wls, *w)
	}
	sort.Slice(wls, func(i, j int) bool {
		if wls[i].PotentialSaveUSD != wls[j].PotentialSaveUSD {
			return wls[i].PotentialSaveUSD > wls[j].PotentialSaveUSD
		}
		return wls[i].MonthlyCostUSD > wls[j].MonthlyCostUSD
	})
	if f.Confidence != "none" {
		for _, w := range wls {
			if w.PotentialSaveUSD >= 0.5 && len(f.Top) < 15 {
				f.Top = append(f.Top, w)
			}
		}
	}
	for i := range f.Namespaces {
		f.Namespaces[i].MonthlyCostUSD = round2(f.Namespaces[i].MonthlyCostUSD)
		f.Namespaces[i].PotentialSaveUSD = round2(f.Namespaces[i].PotentialSaveUSD)
	}
	for i := range f.Top {
		f.Top[i].MonthlyCostUSD = round2(f.Top[i].MonthlyCostUSD)
		f.Top[i].PotentialSaveUSD = round2(f.Top[i].PotentialSaveUSD)
	}
	f.MonthlyCostUSD, f.PotentialSaveUSD = round2(f.MonthlyCostUSD), round2(f.PotentialSaveUSD)
	f.Notes = append(f.Notes, fmt.Sprintf("Estimate = requested resources × configured rates (%.4f $/vCPU-hour, %.4f $/GB-hour) over %d hours; not your cloud bill.", cfg.CPUHourUSD, cfg.MemGBHourUSD, hoursPerMonth))
	return f
}

func round2(v float64) float64 { return float64(int64(v*100+0.5)) / 100 }
