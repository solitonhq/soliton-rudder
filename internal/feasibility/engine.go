package feasibility

import (
	"fmt"
	"math"
	"sort"
)

// Evaluate runs every rule against the snapshot with the default configuration.
func Evaluate(snap *Snapshot) Report { return EvaluateWith(snap, DefaultConfig()) }

// EvaluateWith runs every rule against the snapshot and scores the result. It is pure.
func EvaluateWith(snap *Snapshot, cfg Config) Report {
	snap.Cfg = cfg.Normalize()
	rep := Report{ServerVersion: snap.ServerVersion, Notes: []string{}, Rules: make([]RuleResult, 0, len(Rules))}
	for _, r := range Rules {
		rr := runRule(r, snap)
		if rr.Skipped != "" {
			rep.Stats.RulesFailed++
			rep.Notes = append(rep.Notes, fmt.Sprintf("Skipped “%s”: %s", r.Title, rr.Skipped))
		} else {
			rep.Stats.RulesRun++
		}
		rep.Rules = append(rep.Rules, rr)
	}
	recount(&rep)
	rep.Stats.Nodes = len(snap.list("nodes"))
	rep.Stats.Namespaces = len(snap.list("namespaces"))
	rep.Stats.Pods = len(snap.list("pods"))
	rep.Stats.Workloads = len(snap.workloads())
	rep.Stats.Services = len(snap.list("services"))
	rep.Stats.Ingresses = len(snap.list("ingresses"))
	rep.FinOps = safeFinOps(snap, &rep)
	rep.Notes = append(rep.Notes, "System namespaces (kube-*) are excluded from workload hygiene and security rules.")
	return rep
}

// recount derives severity counts, the score and category scores from rep.Rules.
func recount(rep *Report) {
	rep.Counts = Counts{}
	for _, rr := range rep.Rules {
		for _, f := range rr.Findings {
			rep.Counts.add(f.Severity, 1)
		}
		if rr.Truncated {
			// stored findings are counted above; add the remainder at the rule's severity
			rep.Counts.add(rr.Severity, rr.Affected-len(rr.Findings))
		}
	}
	rep.Score, rep.Categories = Score(rep.Rules)
}

// AppendRules adds externally produced rule results (e.g. from plugins) and rescores the report.
func AppendRules(rep *Report, extra []RuleResult) {
	for _, rr := range extra {
		if rr.Findings == nil {
			rr.Findings = []Finding{} // plugins may omit it; the UI maps over it
		}
		if rr.Skipped != "" {
			rep.Stats.RulesFailed++
			rep.Notes = append(rep.Notes, fmt.Sprintf("Skipped “%s”: %s", rr.Title, rr.Skipped))
		} else {
			rep.Stats.RulesRun++
		}
		rep.Rules = append(rep.Rules, rr)
	}
	recount(rep)
}

func runRule(r Rule, snap *Snapshot) (rr RuleResult) {
	rr = RuleResult{ID: r.ID, Title: r.Title, Category: r.Category, Description: r.Description, Remediation: r.Remediation, Findings: []Finding{}}
	for _, need := range r.Needs {
		if msg, bad := snap.Missing[need]; bad {
			rr.Skipped = fmt.Sprintf("could not read %s (%s)", need, shorten(msg, 120))
			return rr
		}
	}
	defer func() {
		if p := recover(); p != nil {
			rr.Skipped = fmt.Sprintf("internal error: %v", p)
			rr.Findings, rr.Affected = []Finding{}, 0
		}
	}()
	out := r.Run(snap)
	if out.Skipped != "" {
		rr.Skipped = out.Skipped
		return rr
	}
	rr.Checked = out.Checked
	fs := out.Findings
	sort.SliceStable(fs, func(i, j int) bool {
		if sevRank[fs[i].Severity] != sevRank[fs[j].Severity] {
			return sevRank[fs[i].Severity] > sevRank[fs[j].Severity]
		}
		if fs[i].Namespace != fs[j].Namespace {
			return fs[i].Namespace < fs[j].Namespace
		}
		return fs[i].Name < fs[j].Name
	})
	rr.Affected = len(fs)
	for _, f := range fs {
		if sevRank[f.Severity] >= sevRank[rr.Severity] {
			rr.Severity = f.Severity
		}
	}
	if len(fs) > maxFindings {
		fs, rr.Truncated = fs[:maxFindings], true
	}
	if fs != nil {
		rr.Findings = fs
	}
	return rr
}

func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// rulePenalty is how many points a rule costs: its severity weight, scaled between 40% (a few objects)
// and 100% (every examined object affected).
func rulePenalty(r RuleResult) float64 {
	if r.Affected == 0 || r.Skipped != "" {
		return 0
	}
	total := r.Checked
	if r.Affected > total {
		total = r.Affected
	}
	ratio := float64(r.Affected) / float64(total)
	return sevWeight[r.Severity] * (0.4 + 0.6*ratio)
}

// Score returns the overall score (0–100) and per-category scores.
func Score(rules []RuleResult) (int, []CategoryScore) {
	clamp := func(v float64) int { return int(math.Round(math.Max(0, math.Min(100, v)))) }
	var overall float64
	perCat := map[Category]*CategoryScore{}
	pen := map[Category]float64{}
	for _, c := range Categories {
		perCat[c] = &CategoryScore{Category: c}
	}
	for _, r := range rules {
		p := rulePenalty(r)
		overall += p
		pen[r.Category] += p
		if cs := perCat[r.Category]; cs != nil {
			cs.Findings += r.Affected
		}
	}
	out := make([]CategoryScore, 0, len(Categories))
	for _, c := range Categories {
		cs := perCat[c]
		cs.Score = clamp(100 - pen[c])
		out = append(out, *cs)
	}
	return clamp(100 - overall), out
}

// safeFinOps never lets the cost estimate take the assessment down: if it fails, the report simply has no cost tab.
func safeFinOps(snap *Snapshot, rep *Report) (f *FinOps) {
	defer func() {
		if p := recover(); p != nil {
			f = nil
			rep.Notes = append(rep.Notes, fmt.Sprintf("Cost estimate unavailable: internal error: %v", p))
		}
	}()
	return computeFinOps(snap)
}
