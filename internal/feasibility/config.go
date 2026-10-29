package feasibility

import (
	"strings"
)

// Config tunes the Cluster Doctor: which namespaces count as production, and the unit prices and headroom
// used for cost estimates. Defaults are the OpenCost reference on-demand rates; users set their own.
type Config struct {
	ProdNamespaces []string `json:"prodNamespaces"` // exact names or patterns with a leading/trailing * (added to the built-in detection)
	CPUHourUSD     float64  `json:"cpuHourUsd"`     // price of one vCPU for one hour
	MemGBHourUSD   float64  `json:"memGbHourUsd"`   // price of one GB of memory for one hour
	HeadroomPct    float64  `json:"headroomPct"`    // safety margin kept above observed usage when right-sizing
}

const hoursPerMonth = 730

func DefaultConfig() Config {
	return Config{ProdNamespaces: []string{}, CPUHourUSD: 0.031611, MemGBHourUSD: 0.004237, HeadroomPct: 20}
}

// Normalize replaces unset or invalid values with defaults.
func (c Config) Normalize() Config {
	d := DefaultConfig()
	if c.CPUHourUSD <= 0 {
		c.CPUHourUSD = d.CPUHourUSD
	}
	if c.MemGBHourUSD <= 0 {
		c.MemGBHourUSD = d.MemGBHourUSD
	}
	if c.HeadroomPct < 0 || c.HeadroomPct > 300 {
		c.HeadroomPct = d.HeadroomPct
	}
	if c.ProdNamespaces == nil {
		c.ProdNamespaces = []string{}
	}
	return c
}

var prodWords = map[string]bool{"prod": true, "production": true, "prd": true, "live": true}

// matchPattern supports exact names and a single leading or trailing "*".
func matchPattern(pat, s string) bool {
	pat, s = strings.ToLower(strings.TrimSpace(pat)), strings.ToLower(s)
	switch {
	case pat == "":
		return false
	case strings.HasPrefix(pat, "*") && strings.HasSuffix(pat, "*") && len(pat) > 2:
		return strings.Contains(s, pat[1:len(pat)-1])
	case strings.HasPrefix(pat, "*"):
		return strings.HasSuffix(s, pat[1:])
	case strings.HasSuffix(pat, "*"):
		return strings.HasPrefix(s, pat[:len(pat)-1])
	}
	return pat == s
}

// IsProdNamespace decides whether a namespace holds production workloads: configured names/patterns, a
// production-like environment label, or a name containing a word such as prod/production/prd/live.
func (c Config) IsProdNamespace(name string, labels map[string]string) bool {
	for _, p := range c.ProdNamespaces {
		if matchPattern(p, name) {
			return true
		}
	}
	for _, k := range []string{"environment", "env", "tier", "stage", "app.kubernetes.io/environment"} {
		if prodWords[strings.ToLower(labels[k])] {
			return true
		}
	}
	for _, tok := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return r == '-' || r == '_' || r == '.' }) {
		if prodWords[tok] {
			return true
		}
	}
	return false
}

// isProd looks a namespace up in the snapshot (labels come from the namespace list).
func (s *Snapshot) isProd(ns string) bool {
	if s.prodCache == nil {
		s.prodCache = map[string]bool{}
		labels := map[string]map[string]string{}
		for _, n := range s.list("namespaces") {
			labels[n.GetName()] = n.GetLabels()
		}
		s.nsLabels = labels
	}
	if v, ok := s.prodCache[ns]; ok {
		return v
	}
	v := s.Cfg.IsProdNamespace(ns, s.nsLabels[ns])
	s.prodCache[ns] = v
	return v
}
