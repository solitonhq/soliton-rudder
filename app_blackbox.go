package main

import (
	"encoding/json"
	"soliton-rudder/internal/blackbox"
	"soliton-rudder/internal/kube"
	"soliton-rudder/internal/runbook"
)

const (
	bbScope     = "_global"
	bbKey       = "blackbox"
	runbooksKey = "runbooks"
)

func (a *App) blackboxConfig() blackbox.Config {
	cfg := blackbox.DefaultConfig()
	if raw, ok, err := a.integr.GetRaw(bbScope, bbKey); err == nil && ok {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	if cfg.Clusters == nil {
		cfg.Clusters = map[string]blackbox.ClusterCfg{}
	}
	return cfg.Normalize()
}

func (a *App) saveBlackboxConfig(cfg blackbox.Config) error {
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := a.integr.SetRaw(bbScope, bbKey, string(b)); err != nil {
		return err
	}
	a.bbm.Apply(cfg)
	return nil
}

// BlackboxInfo is what the UI shows about the recorder of one cluster.
type BlackboxInfo struct {
	Config         blackbox.ClusterCfg `json:"config"`
	RetentionHours int                 `json:"retentionHours"`
	MaxRecords     int                 `json:"maxRecords"`
	Status         blackbox.Status     `json:"status"`
	Span           blackbox.Span       `json:"span"`
}

func (a *App) blackboxInfo(id string) (BlackboxInfo, error) {
	cfg := a.blackboxConfig()
	cc, ok := cfg.Clusters[id]
	if !ok {
		cc = blackbox.ClusterCfg{Namespaces: []string{}, IntervalSec: 15}
	}
	if cc.Namespaces == nil {
		cc.Namespaces = []string{}
	}
	sp, err := a.bb.Span(id)
	return BlackboxInfo{Config: cc, RetentionHours: cfg.RetentionHours, MaxRecords: cfg.MaxRecords, Status: a.bbm.Status(id), Span: sp}, err
}

func (a *App) GetBlackbox(clusterID string) (BlackboxInfo, error) { return a.blackboxInfo(clusterID) }

// SetBlackbox turns recording on or off for a cluster and sets the limits that keep it cheap.
func (a *App) SetBlackbox(clusterID string, cc blackbox.ClusterCfg, retentionHours, maxRecords int) (BlackboxInfo, error) {
	if _, err := a.clusters.Get(clusterID); err != nil {
		return BlackboxInfo{}, err
	}
	cc, err := blackbox.ValidateCluster(cc)
	if err != nil {
		return BlackboxInfo{}, err
	}
	if err := blackbox.ValidateLimits(retentionHours, maxRecords); err != nil {
		return BlackboxInfo{}, err
	}
	a.bbMu.Lock()
	cfg := a.blackboxConfig()
	cfg.Clusters[clusterID] = cc
	if retentionHours != 0 {
		cfg.RetentionHours = retentionHours
	}
	if maxRecords != 0 {
		cfg.MaxRecords = maxRecords
	}
	err = a.saveBlackboxConfig(cfg.Normalize())
	a.bbMu.Unlock()
	if err != nil {
		return BlackboxInfo{}, err
	}
	return a.blackboxInfo(clusterID)
}

func (a *App) BlackboxQuery(clusterID string, q blackbox.Query) ([]blackbox.Record, error) {
	return a.bb.Query(clusterID, q)
}

func (a *App) BlackboxStateAt(clusterID string, ts int64) ([]blackbox.Record, error) {
	return a.bb.StateAt(clusterID, ts)
}

func (a *App) BlackboxBuckets(clusterID string, from, to int64, n int) ([]blackbox.Bucket, error) {
	if n < 1 || n > 500 {
		n = 120
	}
	return a.bb.Buckets(clusterID, from, to, n)
}

// ClearBlackbox deletes everything recorded for a cluster.
func (a *App) ClearBlackbox(clusterID string) error { return a.bb.Clear(clusterID) }

func (a *App) forgetBlackbox(id string) {
	a.bbMu.Lock()
	defer a.bbMu.Unlock()
	cfg := a.blackboxConfig()
	if _, ok := cfg.Clusters[id]; ok {
		delete(cfg.Clusters, id)
		_ = a.saveBlackboxConfig(cfg)
	}
	_ = a.bb.Clear(id)
}

// ---- Runbooks ----

type RunbooksInfo struct {
	Builtin []runbook.Runbook `json:"builtin"`
	Custom  string            `json:"custom"` // the user's YAML
	Error   string            `json:"error"`  // why the saved YAML is not in effect, if so
}

func (a *App) customRunbooks() ([]runbook.Runbook, string, string) {
	raw, _, _ := a.integr.GetRaw(bbScope, runbooksKey)
	rbs, err := runbook.Parse(raw)
	if err != nil {
		return nil, raw, err.Error()
	}
	return rbs, raw, ""
}

func (a *App) GetRunbooks() RunbooksInfo {
	_, raw, errText := a.customRunbooks()
	return RunbooksInfo{Builtin: runbook.Builtins(), Custom: raw, Error: errText}
}

// SaveRunbooks validates and stores the user's runbooks (a YAML list).
func (a *App) SaveRunbooks(doc string) error {
	if _, err := runbook.Parse(doc); err != nil {
		return err
	}
	return a.integr.SetRaw(bbScope, runbooksKey, doc)
}

// MatchRunbooks returns the runbooks that fit an incident, best first, with variables filled in.
func (a *App) MatchRunbooks(inc runbook.Incident) []runbook.Resolved {
	custom, _, _ := a.customRunbooks()
	return runbook.Select(runbook.All(custom), inc)
}

// AllRunbooksFor lists every runbook for an incident, so the user can pick one that was not auto-matched.
func (a *App) AllRunbooksFor(inc runbook.Incident) []runbook.Resolved {
	custom, _, _ := a.customRunbooks()
	out := []runbook.Resolved{}
	for _, r := range runbook.All(custom) {
		out = append(out, runbook.Select([]runbook.Runbook{withAnyMatch(r)}, inc)...)
	}
	return out
}

func withAnyMatch(r runbook.Runbook) runbook.Runbook {
	r.Match = runbook.Match{}
	return r
}

// RunRunbookCheck runs one read-only runbook step. Steps that change the cluster are not run here.
func (a *App) RunRunbookCheck(clusterID, do, key, ns, name string) (kube.CheckOutput, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return kube.CheckOutput{}, err
	}
	return s.RunCheck(a.ctx, do, key, ns, name)
}

// ---- Access (users who can only read some namespaces) ----

const accessKey = "access"

func (a *App) accessNamespaces(clusterID string) []string {
	raw, ok, err := a.integr.GetRaw(clusterID, accessKey)
	if err != nil || !ok {
		return nil
	}
	var v struct {
		Namespaces []string `json:"namespaces"`
	}
	if json.Unmarshal([]byte(raw), &v) != nil {
		return nil
	}
	return v.Namespaces
}

// AccessInfo is what the UI needs to adapt to limited permissions.
type AccessInfo struct {
	kube.Access
	Configured []string `json:"configured"` // what the user typed in the access settings
}

// GetAccess reports how much of the cluster the current credentials can read.
func (a *App) GetAccess(clusterID string) (AccessInfo, error) {
	s, err := a.kube.Ensure(clusterID)
	if err != nil {
		return AccessInfo{}, err
	}
	cfg := a.accessNamespaces(clusterID)
	if cfg == nil {
		cfg = []string{}
	}
	return AccessInfo{Access: s.Access(a.ctx), Configured: cfg}, nil
}

// SetAccessNamespaces stores the namespaces to work with for a cluster where the user cannot read them all
// (an empty list goes back to discovering them), and re-checks the access.
func (a *App) SetAccessNamespaces(clusterID string, namespaces []string) (AccessInfo, error) {
	if _, err := a.clusters.Get(clusterID); err != nil {
		return AccessInfo{}, err
	}
	clean, err := blackbox.ValidateCluster(blackbox.ClusterCfg{Namespaces: namespaces}) // same naming rules and limits
	if err != nil {
		return AccessInfo{}, err
	}
	b, _ := json.Marshal(map[string]any{"namespaces": clean.Namespaces})
	if err := a.integr.SetRaw(clusterID, accessKey, string(b)); err != nil {
		return AccessInfo{}, err
	}
	if s, err := a.kube.Session(clusterID); err == nil {
		s.RefreshAccess()
	}
	return a.GetAccess(clusterID)
}
