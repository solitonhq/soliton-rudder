package blackbox

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"soliton-rudder/internal/kube"
)

type ClusterCfg struct {
	Enabled     bool     `json:"enabled"`
	Namespaces  []string `json:"namespaces"` // empty = all
	IntervalSec int      `json:"intervalSec"`
}

type Config struct {
	Clusters       map[string]ClusterCfg `json:"clusters"`
	RetentionHours int                   `json:"retentionHours"`
	MaxRecords     int                   `json:"maxRecords"` // per cluster
}

func DefaultConfig() Config {
	return Config{Clusters: map[string]ClusterCfg{}, RetentionHours: 24, MaxRecords: 200_000}
}

func clamp(v, lo, hi, def int) int {
	if v == 0 {
		return def
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Normalize clamps every setting into a range that keeps the recorder cheap and the database bounded.
func (c Config) Normalize() Config {
	c.RetentionHours = clamp(c.RetentionHours, 1, 168, 24)
	c.MaxRecords = clamp(c.MaxRecords, 10_000, 1_000_000, 200_000)
	out := make(map[string]ClusterCfg, len(c.Clusters))
	for id, cc := range c.Clusters {
		cc.IntervalSec = clamp(cc.IntervalSec, 5, 300, 15)
		out[id] = cc
	}
	c.Clusters = out
	return c
}

var nsNameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// ValidateCluster checks and cleans the per-cluster settings coming from the UI.
func ValidateCluster(cc ClusterCfg) (ClusterCfg, error) {
	if len(cc.Namespaces) > 50 {
		return cc, fmt.Errorf("at most 50 namespaces can be recorded")
	}
	clean := []string{}
	for _, n := range cc.Namespaces {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if len(n) > 63 || !nsNameRe.MatchString(n) {
			return cc, fmt.Errorf("%q is not a valid namespace name", n)
		}
		clean = append(clean, n)
	}
	cc.Namespaces = clean
	if cc.IntervalSec != 0 && (cc.IntervalSec < 5 || cc.IntervalSec > 300) {
		return cc, fmt.Errorf("the interval must be between 5 and 300 seconds")
	}
	return cc, nil
}

// ValidateLimits checks the retention (hours) and record cap; 0 means "keep the current value".
func ValidateLimits(retentionHours, maxRecords int) error {
	if retentionHours != 0 && (retentionHours < 1 || retentionHours > 168) {
		return fmt.Errorf("retention must be between 1 hour and 7 days")
	}
	if maxRecords != 0 && (maxRecords < 10_000 || maxRecords > 1_000_000) {
		return fmt.Errorf("the record limit must be between 10,000 and 1,000,000")
	}
	return nil
}

type Status struct {
	Running  bool   `json:"running"`
	LastRun  int64  `json:"lastRun"` // ms
	LastErr  string `json:"lastError"`
	Recorded int    `json:"recorded"` // records written since start
}

// Observer reads a cluster; injected so the recorder does not depend on live sessions.
type Observer func(ctx context.Context, clusterID string, namespaces []string) (kube.Observation, error)

type rec struct {
	cancel context.CancelFunc
	cfg    ClusterCfg
	mu     sync.Mutex
	st     Status
	prev   map[string]snap
	loaded bool
	ticks  int
}

type Manager struct {
	store   *Store
	observe Observer
	now     func() time.Time

	mu        sync.Mutex
	recs      map[string]*rec
	retention time.Duration
	maxRows   int
}

func NewManager(store *Store, observe Observer) *Manager {
	return &Manager{store: store, observe: observe, now: time.Now, recs: map[string]*rec{}, retention: 24 * time.Hour, maxRows: 200_000}
}

// Apply starts, restarts or stops recorders so they match cfg.
func (m *Manager) Apply(cfg Config) {
	cfg = cfg.Normalize()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retention = time.Duration(cfg.RetentionHours) * time.Hour
	m.maxRows = cfg.MaxRecords
	for id, r := range m.recs {
		if cc, ok := cfg.Clusters[id]; !ok || !cc.Enabled || !sameCfg(cc, r.cfg) {
			r.cancel()
			delete(m.recs, id)
		}
	}
	for id, cc := range cfg.Clusters {
		if !cc.Enabled || m.recs[id] != nil {
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		r := &rec{cancel: cancel, cfg: cc, st: Status{Running: true}}
		m.recs[id] = r
		go m.loop(ctx, id, r)
	}
}

func sameCfg(a, b ClusterCfg) bool {
	if a.IntervalSec != b.IntervalSec || len(a.Namespaces) != len(b.Namespaces) {
		return false
	}
	for i := range a.Namespaces {
		if a.Namespaces[i] != b.Namespaces[i] {
			return false
		}
	}
	return true
}

func (m *Manager) Status(id string) Status {
	m.mu.Lock()
	r := m.recs[id]
	m.mu.Unlock()
	if r == nil {
		return Status{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.st
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, r := range m.recs {
		r.cancel()
		delete(m.recs, id)
	}
}

func (m *Manager) loop(ctx context.Context, id string, r *rec) {
	for {
		m.step(ctx, id, r)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(r.cfg.IntervalSec) * time.Second):
		}
	}
}

// step performs one observation. Errors are kept in the status and retried next tick: a flaky API server must not
// stop the recorder, and an unreachable cluster must not be recorded as "everything is gone".
func (m *Manager) step(ctx context.Context, id string, r *rec) {
	m.mu.Lock()
	retention, maxRows := m.retention, m.maxRows
	m.mu.Unlock()
	now := m.now()
	fail := func(err error) {
		r.mu.Lock()
		r.st.LastErr, r.st.LastRun = err.Error(), now.UnixMilli()
		r.mu.Unlock()
	}
	if !r.loaded {
		last, err := m.store.LastStates(id)
		if err != nil {
			fail(err)
			return
		}
		r.prev = make(map[string]snap, len(last))
		for _, l := range last {
			r.prev[l.Key] = snap{Status: l.Status, Detail: l.Reason, Kind: l.ObjKind, Namespace: l.Namespace, Name: l.Name}
		}
		r.loaded = true
	}
	obs, err := m.observe(ctx, id, r.cfg.Namespaces)
	if err != nil {
		fail(err)
		return
	}
	unread := map[string]bool{}
	for _, k := range obs.Unread {
		unread[k] = true
	}
	states, next := diffStates(r.prev, obs.States, unread, now.UnixMilli())
	recs := append(states, eventRecords(obs.Events, now.Add(-retention).UnixMilli())...)
	if err := m.store.Add(id, recs); err != nil {
		fail(err)
		return
	}
	r.prev = next
	r.ticks++
	if r.ticks%20 == 1 {
		_ = m.store.Prune(id, now.Add(-retention).UnixMilli(), maxRows)
	}
	r.mu.Lock()
	r.st.LastErr, r.st.LastRun, r.st.Recorded = "", now.UnixMilli(), r.st.Recorded+len(recs)
	if len(obs.Notes) > 0 {
		r.st.LastErr = "partial: " + obs.Notes[0]
	}
	r.mu.Unlock()
}
