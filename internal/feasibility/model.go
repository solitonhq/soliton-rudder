// Package feasibility runs cluster health/readiness assessments, scores them and keeps history.
package feasibility

type Severity string

const (
	Critical Severity = "critical"
	High     Severity = "high"
	Medium   Severity = "medium"
	Low      Severity = "low"
	Info     Severity = "info"
)

var sevRank = map[Severity]int{Info: 0, Low: 1, Medium: 2, High: 3, Critical: 4}

var sevWeight = map[Severity]float64{Critical: 20, High: 10, Medium: 5, Low: 2, Info: 0}

type Category string

const (
	CatResources   Category = "resources"
	CatReliability Category = "reliability"
	CatSecurity    Category = "security"
	CatMaintenance Category = "maintenance"
)

var Categories = []Category{CatResources, CatReliability, CatSecurity, CatMaintenance}

// Finding is one affected object (or a cluster-level observation when Name is empty).
type Finding struct {
	Severity    Severity `json:"severity"`
	Kind        string   `json:"kind"`
	Namespace   string   `json:"namespace"`
	Name        string   `json:"name"`
	ResourceKey string   `json:"resourceKey"` // for navigation in the explorer
	Message     string   `json:"message"`
}

// Key identifies a finding across runs (same rule + object).
func (f Finding) Key(rule string) string {
	return rule + "|" + f.Kind + "|" + f.Namespace + "|" + f.Name
}

// RuleResult is one rule's outcome in a report.
type RuleResult struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Category    Category  `json:"category"`
	Severity    Severity  `json:"severity"` // highest severity among findings
	Description string    `json:"description"`
	Remediation string    `json:"remediation"`
	Checked     int       `json:"checked"`  // candidate objects examined
	Affected    int       `json:"affected"` // findings count (before truncation)
	Findings    []Finding `json:"findings"`
	Truncated   bool      `json:"truncated"`
	Skipped     string    `json:"skipped"` // reason the rule could not run
}

type CategoryScore struct {
	Category Category `json:"category"`
	Score    int      `json:"score"`
	Findings int      `json:"findings"`
}

type Counts struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Info     int `json:"info"`
}

func (c *Counts) add(s Severity, n int) {
	switch s {
	case Critical:
		c.Critical += n
	case High:
		c.High += n
	case Medium:
		c.Medium += n
	case Low:
		c.Low += n
	case Info:
		c.Info += n
	}
}

type Stats struct {
	Nodes       int `json:"nodes"`
	Namespaces  int `json:"namespaces"`
	Pods        int `json:"pods"`
	Workloads   int `json:"workloads"`
	Services    int `json:"services"`
	Ingresses   int `json:"ingresses"`
	RulesRun    int `json:"rulesRun"`
	RulesFailed int `json:"rulesSkipped"`
}

// Report is a complete assessment of one cluster at one point in time.
type Report struct {
	ID            string          `json:"id"`
	ClusterID     string          `json:"clusterId"`
	ClusterName   string          `json:"clusterName"`
	StartedAt     int64           `json:"startedAt"` // unix seconds
	DurationMs    int64           `json:"durationMs"`
	ServerVersion string          `json:"serverVersion"`
	Score         int             `json:"score"`
	Categories    []CategoryScore `json:"categories"`
	Counts        Counts          `json:"counts"`
	Stats         Stats           `json:"stats"`
	Notes         []string        `json:"notes"`
	FinOps        *FinOps         `json:"finops,omitempty"`
	Rules         []RuleResult    `json:"rules"`
}

// Summary is the lightweight history row.
type Summary struct {
	ID            string          `json:"id"`
	ClusterID     string          `json:"clusterId"`
	StartedAt     int64           `json:"startedAt"`
	DurationMs    int64           `json:"durationMs"`
	Score         int             `json:"score"`
	ServerVersion string          `json:"serverVersion"`
	Counts        Counts          `json:"counts"`
	Categories    []CategoryScore `json:"categories"`
}

// Progress is streamed to the UI while a run is in flight.
type Progress struct {
	Run   string `json:"run"`
	Phase string `json:"phase"`
	Pct   int    `json:"pct"`
}

// Done is emitted when a run finishes (successfully or not).
type Done struct {
	Run    string `json:"run"`
	Report string `json:"report"` // report id on success
	Error  string `json:"error"`
}

const (
	EventProgress = "fzprogress"
	EventDone     = "fzdone"
	maxFindings   = 100 // per rule stored in a report
)
