package blackbox

import (
	"fmt"
	"sort"

	"soliton-rudder/internal/kube"
)

type snap struct {
	Status, Detail, Kind, Namespace, Name string
}

func severityOf(status string) string {
	switch status {
	case "error":
		return "error"
	case "warn":
		return "warn"
	}
	return "info"
}

func describe(s snap) string {
	if s.Detail == "" {
		return s.Status
	}
	return s.Status + ": " + s.Detail
}

// diffStates turns two observations into records: only what changed is written, so a quiet cluster costs nothing.
func diffStates(prev map[string]snap, cur []kube.ObservedState, unread map[string]bool, now int64) ([]Record, map[string]snap) {
	next := make(map[string]snap, len(cur))
	var out []Record
	for _, c := range cur {
		n := snap{c.Status, c.Detail, c.Kind, c.Namespace, c.Name}
		next[c.Key] = n
		old, had := prev[c.Key]
		if had && old.Status == n.Status && old.Detail == n.Detail {
			continue
		}
		r := Record{TS: now, Kind: "state", Key: c.Key, ObjKind: c.Kind, Namespace: c.Namespace, Name: c.Name,
			Severity: severityOf(n.Status), Status: n.Status, Reason: n.Detail}
		if had {
			r.Prev = describe(old)
		}
		out = append(out, r)
	}
	for k, old := range prev {
		if _, still := next[k]; !still {
			if unread[old.Kind] {
				next[k] = old // could not be read this time: unknown, not deleted
				continue
			}
			out = append(out, Record{TS: now, Kind: "state", Key: k, ObjKind: old.Kind, Namespace: old.Namespace, Name: old.Name,
				Severity: "info", Status: "gone", Reason: "deleted", Prev: describe(old)})
		}
	}
	sortRecords(out)
	return out, next
}

func sortRecords(r []Record) {
	sort.SliceStable(r, func(i, j int) bool { return r[i].Key < r[j].Key })
}

// eventRecords converts events newer than since (ms) into records; the store ignores ones it already has.
func eventRecords(evs []kube.ObservedEvent, since int64) []Record {
	var out []Record
	for _, e := range evs {
		ts := e.At.UnixMilli()
		if ts < since {
			continue
		}
		sev := "info"
		if e.Type == "Warning" {
			sev = "warn"
		}
		out = append(out, Record{TS: ts, Kind: "event", Key: fmt.Sprintf("%s#%d", e.UID, e.Count), ObjKind: e.Kind, Namespace: e.Namespace,
			Name: e.Name, Severity: sev, Reason: e.Reason, Message: e.Message})
	}
	return out
}
