// Package runbook holds step-by-step troubleshooting guides that are matched to an incident (a failing object).
// Read-only steps can run inside the app; steps that change the cluster are always confirmed by the user.
package runbook

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

// Step actions. Checks only read; actions change things or open a tool and need confirmation in the UI.
const (
	DoLogs         = "logs"
	DoPreviousLogs = "previous-logs"
	DoEvents       = "events"
	DoHealth       = "health"
	DoYAML         = "yaml"
	DoRestart      = "restart"
	DoScale        = "scale"
	DoDelete       = "delete"
	DoDebug        = "debug"
	DoOpen         = "open"
	DoManual       = "manual"
)

var doKinds = map[string][]string{ // which object kinds an action applies to; nil = any
	DoLogs: {"Pod"}, DoPreviousLogs: {"Pod"}, DoDebug: {"Pod"}, DoDelete: {"Pod"},
	DoRestart: {"Deployment", "StatefulSet", "DaemonSet"}, DoScale: {"Deployment", "StatefulSet"},
	DoEvents: nil, DoHealth: nil, DoYAML: nil, DoOpen: nil, DoManual: nil,
}

// ReadOnly reports whether the step only reads and may therefore run without asking.
func ReadOnly(do string) bool {
	switch do {
	case DoLogs, DoPreviousLogs, DoEvents, DoHealth, DoYAML:
		return true
	}
	return false
}

// Applies reports whether action do makes sense for an object of the given kind.
func Applies(do, kind string) bool {
	ks, known := doKinds[do]
	if !known {
		return false
	}
	if ks == nil {
		return true
	}
	for _, k := range ks {
		if k == kind {
			return true
		}
	}
	return false
}

type Match struct {
	Kinds   []string `json:"kinds,omitempty"`   // empty = any kind
	Reasons []string `json:"reasons,omitempty"` // case-insensitive substrings of the failure reason
	Status  string   `json:"status,omitempty"`  // warn | error
}

type Step struct {
	Title    string `json:"title"`
	Do       string `json:"do"`
	Text     string `json:"text,omitempty"`
	Replicas *int   `json:"replicas,omitempty"` // scale only
}

type Runbook struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Match       Match  `json:"match"`
	Steps       []Step `json:"steps"`
	Builtin     bool   `json:"builtin"`
}

// Incident describes what failed: the object kind, and the reason/status the recorder or the user saw.
type Incident struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Reason    string `json:"reason"`
	Status    string `json:"status"`
}

// ResolvedStep is a step prepared for an incident: variables substituted and applicability decided.
type ResolvedStep struct {
	Step
	Applicable bool `json:"applicable"`
	ReadOnly   bool `json:"readOnly"`
}

type Resolved struct {
	Runbook
	Steps []ResolvedStep `json:"steps"`
	Score int            `json:"score"`
}

const (
	maxRunbooks = 50
	maxSteps    = 20
	maxText     = 4000
)

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// Parse reads user-defined runbooks (a YAML list) and validates them.
func Parse(doc string) ([]Runbook, error) {
	if strings.TrimSpace(doc) == "" {
		return nil, nil
	}
	var rbs []Runbook
	if err := yaml.UnmarshalStrict([]byte(doc), &rbs); err != nil {
		return nil, fmt.Errorf("invalid runbook YAML: %w", err)
	}
	if err := Validate(rbs); err != nil {
		return nil, err
	}
	return rbs, nil
}

func Validate(rbs []Runbook) error {
	if len(rbs) > maxRunbooks {
		return fmt.Errorf("at most %d runbooks are allowed", maxRunbooks)
	}
	seen := map[string]bool{}
	for i, r := range rbs {
		where := fmt.Sprintf("runbook #%d", i+1)
		if r.ID != "" {
			where = fmt.Sprintf("runbook %q", r.ID)
		}
		switch {
		case !idRe.MatchString(r.ID):
			return fmt.Errorf("%s: id must be lowercase letters, digits and dashes", where)
		case seen[r.ID]:
			return fmt.Errorf("%s: duplicate id", where)
		case strings.TrimSpace(r.Title) == "":
			return fmt.Errorf("%s: title is required", where)
		case len(r.Match.Reasons) == 0 && r.Match.Status == "":
			return fmt.Errorf("%s: match needs reasons or a status, otherwise it would match every incident", where)
		case r.Match.Status != "" && r.Match.Status != "warn" && r.Match.Status != "error":
			return fmt.Errorf("%s: match.status must be warn or error", where)
		case len(r.Steps) == 0:
			return fmt.Errorf("%s: at least one step is required", where)
		case len(r.Steps) > maxSteps:
			return fmt.Errorf("%s: at most %d steps", where, maxSteps)
		}
		seen[r.ID] = true
		for j, s := range r.Steps {
			sw := fmt.Sprintf("%s step %d", where, j+1)
			if strings.TrimSpace(s.Title) == "" {
				return fmt.Errorf("%s: title is required", sw)
			}
			if _, ok := doKinds[s.Do]; !ok {
				return fmt.Errorf("%s: unknown action %q (use logs, previous-logs, events, health, yaml, restart, scale, delete, debug, open or manual)", sw, s.Do)
			}
			if s.Do == DoScale && (s.Replicas == nil || *s.Replicas < 0 || *s.Replicas > 1000) {
				return fmt.Errorf("%s: scale needs replicas between 0 and 1000", sw)
			}
			if len(s.Text) > maxText {
				return fmt.Errorf("%s: text is too long", sw)
			}
		}
	}
	return nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func score(r Runbook, inc Incident) (int, bool) {
	if len(r.Match.Kinds) > 0 && !contains(r.Match.Kinds, inc.Kind) {
		return 0, false
	}
	sc := 0
	reason := strings.ToLower(inc.Reason)
	if len(r.Match.Reasons) > 0 {
		hit := false
		for _, m := range r.Match.Reasons {
			if m != "" && strings.Contains(reason, strings.ToLower(m)) {
				hit = true
			}
		}
		if !hit {
			return 0, false
		}
		sc += 2
	}
	if r.Match.Status != "" {
		if r.Match.Status != inc.Status {
			return 0, false
		}
		sc++
	}
	if len(r.Match.Kinds) > 0 {
		sc++
	}
	return sc, true
}

func subst(s string, inc Incident) string {
	return strings.NewReplacer("{{name}}", inc.Name, "{{namespace}}", inc.Namespace, "{{kind}}", inc.Kind).Replace(s)
}

// Select returns the runbooks that fit the incident, best match first (user-defined win ties over built-in ones).
func Select(all []Runbook, inc Incident) []Resolved {
	out := []Resolved{} // never nil: the UI tells "no match" from "still loading" by this
	for _, r := range all {
		sc, ok := score(r, inc)
		if !ok {
			continue
		}
		res := Resolved{Runbook: r, Score: sc}
		res.Runbook.Description = subst(r.Description, inc)
		res.Steps = make([]ResolvedStep, 0, len(r.Steps))
		for _, s := range r.Steps {
			s.Title, s.Text = subst(s.Title, inc), subst(s.Text, inc)
			res.Steps = append(res.Steps, ResolvedStep{Step: s, Applicable: Applies(s.Do, inc.Kind), ReadOnly: ReadOnly(s.Do)})
		}
		out = append(out, res)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return !out[i].Builtin && out[j].Builtin
	})
	return out
}

// All returns the built-in runbooks followed by the user's.
func All(custom []Runbook) []Runbook {
	out := append([]Runbook(nil), Builtins()...)
	for _, c := range custom {
		c.Builtin = false
		// a user runbook with a built-in's id replaces it
		replaced := false
		for i := range out {
			if out[i].ID == c.ID {
				out[i], replaced = c, true
			}
		}
		if !replaced {
			out = append(out, c)
		}
	}
	return out
}
