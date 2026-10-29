package feasibility

import "sort"

type DiffItem struct {
	RuleID  string  `json:"ruleId"`
	Title   string  `json:"title"`
	Finding Finding `json:"finding"`
}

type RuleDelta struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Before int    `json:"before"`
	After  int    `json:"after"`
}

// Diff compares two reports of the same cluster (From = older, To = newer).
type Diff struct {
	From        Summary     `json:"from"`
	To          Summary     `json:"to"`
	ScoreDelta  int         `json:"scoreDelta"`
	New         []DiffItem  `json:"new"`
	Resolved    []DiffItem  `json:"resolved"`
	Unchanged   int         `json:"unchanged"`
	RuleDeltas  []RuleDelta `json:"ruleDeltas"`  // rules whose affected count changed
	Approximate bool        `json:"approximate"` // some rule's findings were truncated, so item lists may be incomplete
}

func summaryOf(r Report) Summary {
	return Summary{ID: r.ID, ClusterID: r.ClusterID, StartedAt: r.StartedAt, DurationMs: r.DurationMs, Score: r.Score,
		ServerVersion: r.ServerVersion, Counts: r.Counts, Categories: r.Categories}
}

func Compare(from, to Report) Diff {
	d := Diff{From: summaryOf(from), To: summaryOf(to), ScoreDelta: to.Score - from.Score, New: []DiffItem{}, Resolved: []DiffItem{}, RuleDeltas: []RuleDelta{}}
	before := map[string]DiffItem{}
	beforeCount := map[string]int{}
	titles := map[string]string{}
	for _, r := range from.Rules {
		beforeCount[r.ID] = r.Affected
		titles[r.ID] = r.Title
		if r.Truncated {
			d.Approximate = true
		}
		for _, f := range r.Findings {
			before[f.Key(r.ID)] = DiffItem{RuleID: r.ID, Title: r.Title, Finding: f}
		}
	}
	seen := map[string]bool{}
	for _, r := range to.Rules {
		titles[r.ID] = r.Title
		if r.Truncated {
			d.Approximate = true
		}
		if a, b := beforeCount[r.ID], r.Affected; a != b {
			d.RuleDeltas = append(d.RuleDeltas, RuleDelta{ID: r.ID, Title: r.Title, Before: a, After: b})
		}
		delete(beforeCount, r.ID)
		for _, f := range r.Findings {
			k := f.Key(r.ID)
			seen[k] = true
			if _, ok := before[k]; ok {
				d.Unchanged++
			} else {
				d.New = append(d.New, DiffItem{RuleID: r.ID, Title: r.Title, Finding: f})
			}
		}
	}
	for id, n := range beforeCount { // rules that disappeared entirely
		if n != 0 {
			d.RuleDeltas = append(d.RuleDeltas, RuleDelta{ID: id, Title: titles[id], Before: n, After: 0})
		}
	}
	for k, it := range before {
		if !seen[k] {
			d.Resolved = append(d.Resolved, it)
		}
	}
	less := func(a, b DiffItem) bool {
		if sevRank[a.Finding.Severity] != sevRank[b.Finding.Severity] {
			return sevRank[a.Finding.Severity] > sevRank[b.Finding.Severity]
		}
		return a.Finding.Key(a.RuleID) < b.Finding.Key(b.RuleID)
	}
	sort.Slice(d.New, func(i, j int) bool { return less(d.New[i], d.New[j]) })
	sort.Slice(d.Resolved, func(i, j int) bool { return less(d.Resolved[i], d.Resolved[j]) })
	sort.Slice(d.RuleDeltas, func(i, j int) bool { return d.RuleDeltas[i].ID < d.RuleDeltas[j].ID })
	return d
}
