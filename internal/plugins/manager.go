package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// PluginInfo is the UI-facing description of an installed plugin.
type PluginInfo struct {
	Manifest    Manifest     `json:"manifest"`
	Enabled     bool         `json:"enabled"`
	Granted     []string     `json:"granted"`
	State       string       `json:"state"`
	Error       string       `json:"error"`
	InstalledAt int64        `json:"installedAt"`
	Permissions []Permission `json:"permissions"` // requested, with descriptions
}

// Inspection is shown to the user before installation so they can review and grant permissions.
type Inspection struct {
	Manifest        Manifest     `json:"manifest"`
	Permissions     []Permission `json:"permissions"`
	Update          bool         `json:"update"`
	ExistingVersion string       `json:"existingVersion"`
	HasBackend      bool         `json:"hasBackend"`
	Warnings        []string     `json:"warnings"`
}

// Finding / RuleOutcome carry assessment results contributed by plugins.
type Finding struct {
	Severity    string `json:"severity"`
	Kind        string `json:"kind"`
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	ResourceKey string `json:"resourceKey"`
	Message     string `json:"message"`
}

type RuleOutcome struct {
	PluginID   string         `json:"pluginId"`
	PluginName string         `json:"pluginName"`
	Rule       AssessmentRule `json:"rule"`
	Checked    int            `json:"checked"`
	Findings   []Finding      `json:"findings"`
	Error      string         `json:"error"`
}

type record struct {
	Enabled     bool     `json:"enabled"`
	Granted     []string `json:"granted"`
	InstalledAt int64    `json:"installedAt"`
}

type installed struct {
	man *Manifest
	dir string
	rec record
}

type Manager struct {
	root     string // <config>/plugins
	dataRoot string
	version  string
	broker   *Broker
	onChange func()
	ctx      context.Context
	cancel   context.CancelFunc

	mu      sync.RWMutex
	plugins map[string]*installed
	procs   map[string]*proc
}

// NewManager loads installed plugins from root. Call Start to launch enabled backends.
func NewManager(root, hostVersion string, host Host, onChange func()) (*Manager, error) {
	m := &Manager{
		root: root, dataRoot: filepath.Join(root, "data"), version: hostVersion, onChange: onChange,
		plugins: map[string]*installed{}, procs: map[string]*proc{},
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.broker = NewBroker(host, NewStorage(m.dataRoot))
	for _, d := range []string{m.installedDir(), m.dataRoot} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
	}
	state := m.loadState()
	entries, _ := os.ReadDir(m.installedDir())
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(m.installedDir(), e.Name())
		man, err := ReadManifest(dir)
		if err != nil || man.ID != e.Name() {
			continue // a broken plugin directory must not stop the app
		}
		rec, ok := state[man.ID]
		if !ok {
			rec = record{Enabled: false, InstalledAt: time.Now().Unix()}
		}
		m.plugins[man.ID] = &installed{man: man, dir: dir, rec: rec}
	}
	return m, nil
}

func (m *Manager) installedDir() string { return filepath.Join(m.root, "installed") }
func (m *Manager) stateFile() string    { return filepath.Join(m.root, "state.json") }
func (m *Manager) changed() {
	if m.onChange != nil {
		m.onChange()
	}
}

func (m *Manager) loadState() map[string]record {
	out := map[string]record{}
	if b, err := os.ReadFile(m.stateFile()); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

// saveLocked persists enabled/granted state; callers hold m.mu.
func (m *Manager) saveLocked() error {
	state := map[string]record{}
	for id, p := range m.plugins {
		state[id] = p.rec
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.stateFile() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.stateFile())
}

// Start launches backends of enabled plugins.
func (m *Manager) Start() {
	m.mu.RLock()
	var ids []string
	for id, p := range m.plugins {
		if p.rec.Enabled && p.man.Backend != nil {
			ids = append(ids, id)
		}
	}
	m.mu.RUnlock()
	for _, id := range ids {
		m.startProc(id)
	}
}

func (m *Manager) startProc(id string) {
	m.mu.Lock()
	p := m.plugins[id]
	if p == nil || p.man.Backend == nil || m.procs[id] != nil {
		m.mu.Unlock()
		return
	}
	pr := newProc(m, id, p.dir, p.man)
	m.procs[id] = pr
	m.mu.Unlock()
	pr.start(m.ctx)
}

func (m *Manager) stopProc(id string) {
	m.mu.Lock()
	pr := m.procs[id]
	delete(m.procs, id)
	m.mu.Unlock()
	if pr != nil {
		pr.stop()
	}
}

// Shutdown stops every backend.
func (m *Manager) Shutdown() {
	m.mu.RLock()
	ids := make([]string, 0, len(m.procs))
	for id := range m.procs {
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) { defer wg.Done(); m.stopProc(id) }(id)
	}
	wg.Wait()
	m.cancel()
}

func (m *Manager) grantsFor(id string) Grants {
	return func(perm string) bool {
		m.mu.RLock()
		defer m.mu.RUnlock()
		p := m.plugins[id]
		if p == nil || !p.rec.Enabled {
			return false
		}
		for _, g := range p.rec.Granted {
			if g == perm {
				return true
			}
		}
		return false
	}
}

func (m *Manager) info(p *installed) PluginInfo {
	pi := PluginInfo{Manifest: *p.man, Enabled: p.rec.Enabled, Granted: append([]string{}, p.rec.Granted...), InstalledAt: p.rec.InstalledAt,
		Permissions: PermissionInfo(p.man.Permissions), State: StateStopped}
	if pi.Granted == nil {
		pi.Granted = []string{}
	}
	switch {
	case p.man.Backend == nil:
		pi.State = StateNoBack
	case !p.rec.Enabled:
		pi.State = StateStopped
	default:
		if pr := m.procs[p.man.ID]; pr != nil {
			pi.State, pi.Error = pr.snapshot()
		}
	}
	return pi
}

func (m *Manager) List() []PluginInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]PluginInfo, 0, len(m.plugins))
	for _, p := range m.plugins {
		out = append(out, m.info(p))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.Name < out[j].Manifest.Name })
	return out
}

// Inspect validates a plugin source and reports what it would be allowed to do.
func (m *Manager) Inspect(src string) (Inspection, error) {
	_, man, cleanup, err := stage(src, filepath.Join(m.root, "staging"))
	if err != nil {
		return Inspection{}, err
	}
	defer cleanup()
	ins := Inspection{Manifest: *man, Permissions: PermissionInfo(man.Permissions), HasBackend: man.Backend != nil, Warnings: []string{}}
	m.mu.RLock()
	if ex := m.plugins[man.ID]; ex != nil {
		ins.Update, ins.ExistingVersion = true, ex.man.Version
	}
	m.mu.RUnlock()
	if man.Backend != nil {
		ins.Warnings = append(ins.Warnings, "This plugin runs a program on your Mac with your user privileges. Permissions only limit what it can do through Soliton Rudder; install plugins you trust.")
	}
	for _, p := range ins.Permissions {
		if p.Risk == "high" {
			ins.Warnings = append(ins.Warnings, fmt.Sprintf("“%s” is a high-risk permission.", p.Title))
		}
	}
	return ins, nil
}

// Install copies a plugin into place with the granted permissions (a subset of those requested).
func (m *Manager) Install(src string, granted []string) (PluginInfo, error) {
	dir, man, cleanup, err := stage(src, filepath.Join(m.root, "staging"))
	if err != nil {
		return PluginInfo{}, err
	}
	defer cleanup()
	req := map[string]bool{}
	for _, p := range man.Permissions {
		req[p] = true
	}
	var grants []string
	for _, g := range granted {
		if !req[g] {
			return PluginInfo{}, fmt.Errorf("permission %q was not requested by the plugin", g)
		}
		grants = append(grants, g)
	}

	m.stopProc(man.ID)
	target := filepath.Join(m.installedDir(), man.ID)
	trash := filepath.Join(m.root, "staging", fmt.Sprintf("old-%s-%d", man.ID, time.Now().UnixNano()))
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, trash); err != nil {
			return PluginInfo{}, err
		}
		defer os.RemoveAll(trash)
	}
	if err := os.Rename(dir, target); err != nil {
		if err2 := copyTree(dir, target); err2 != nil { // cross-device fallback
			return PluginInfo{}, err2
		}
	}
	rec := record{Enabled: true, Granted: grants, InstalledAt: time.Now().Unix()}
	if old := m.plugins[man.ID]; old != nil {
		rec.InstalledAt = old.rec.InstalledAt
	}
	m.plugins[man.ID] = &installed{man: man, dir: target, rec: rec}
	if err := m.saveLocked(); err != nil {
		return PluginInfo{}, err
	}
	pi := m.info(m.plugins[man.ID])
	go func() {
		m.startProc(man.ID)
		m.changed()
	}()
	return pi, nil
}

func (m *Manager) Uninstall(id string) error {
	m.stopProc(id)
	m.mu.Lock()
	p := m.plugins[id]
	if p == nil {
		m.mu.Unlock()
		return errors.New("plugin not installed")
	}
	delete(m.plugins, id)
	err := m.saveLocked()
	m.mu.Unlock()
	if err != nil {
		return err
	}
	_ = os.RemoveAll(p.dir)
	_ = m.broker.storage.Purge(id)
	m.changed()
	return nil
}

func (m *Manager) SetEnabled(id string, enabled bool) error {
	m.mu.Lock()
	p := m.plugins[id]
	if p == nil {
		m.mu.Unlock()
		return errors.New("plugin not installed")
	}
	p.rec.Enabled = enabled
	err := m.saveLocked()
	m.mu.Unlock()
	if err != nil {
		return err
	}
	if enabled {
		m.startProc(id)
	} else {
		m.stopProc(id)
	}
	m.changed()
	return nil
}

func (m *Manager) SetGranted(id string, granted []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.plugins[id]
	if p == nil {
		return errors.New("plugin not installed")
	}
	allowed := map[string]bool{}
	for _, perm := range p.man.Permissions {
		allowed[perm] = true
	}
	var out []string
	for _, g := range granted {
		if !allowed[g] {
			return fmt.Errorf("permission %q was not requested by the plugin", g)
		}
		out = append(out, g)
	}
	p.rec.Granted = out
	defer m.changed()
	return m.saveLocked()
}

func (m *Manager) Restart(id string) error {
	m.mu.RLock()
	p := m.plugins[id]
	m.mu.RUnlock()
	if p == nil {
		return errors.New("plugin not installed")
	}
	m.stopProc(id)
	if p.rec.Enabled {
		m.startProc(id)
	}
	m.changed()
	return nil
}

func (m *Manager) Logs(id string) []string {
	m.mu.RLock()
	pr := m.procs[id]
	m.mu.RUnlock()
	if pr == nil {
		return []string{}
	}
	return pr.Logs()
}

func (m *Manager) enabled(id string) (*installed, *proc, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p := m.plugins[id]
	if p == nil {
		return nil, nil, errors.New("plugin not installed")
	}
	if !p.rec.Enabled {
		return nil, nil, errors.New("plugin is disabled")
	}
	return p, m.procs[id], nil
}

// UICall handles a request from a plugin's UI panel: host API calls, or forwarding to its backend.
func (m *Manager) UICall(ctx context.Context, id, method string, params json.RawMessage) (any, error) {
	_, pr, err := m.enabled(id)
	if err != nil {
		return nil, err
	}
	if method == "plugin.call" {
		if pr == nil {
			return nil, errors.New("this plugin has no backend")
		}
		var out json.RawMessage
		if err := pr.Call(ctx, "ui/call", json.RawMessage(params), &out); err != nil {
			return nil, err
		}
		if len(out) == 0 {
			return nil, nil
		}
		return out, nil
	}
	return m.broker.Call(ctx, id, m.grantsFor(id), method, params)
}

// RunCommand executes a plugin command on its backend and returns an optional message for the user.
func (m *Manager) RunCommand(ctx context.Context, id, command string, cmdCtx json.RawMessage) (string, error) {
	p, pr, err := m.enabled(id)
	if err != nil {
		return "", err
	}
	found := false
	for _, c := range p.man.Commands {
		if c.ID == command {
			found = true
		}
	}
	if !found {
		return "", fmt.Errorf("unknown command %q", command)
	}
	if pr == nil {
		return "", errors.New("this plugin has no backend")
	}
	var res struct {
		Message string `json:"message"`
	}
	if err := pr.Call(ctx, "command/run", map[string]any{"command": command, "context": cmdCtx}, &res); err != nil {
		return "", err
	}
	return res.Message, nil
}

// AssessRules runs every assessment rule contributed by enabled plugins for a cluster.
func (m *Manager) AssessRules(ctx context.Context, clusterID string) []RuleOutcome {
	m.mu.RLock()
	type job struct {
		p  *installed
		pr *proc
	}
	var jobs []job
	for id, p := range m.plugins {
		if p.rec.Enabled && len(p.man.AssessmentRules) > 0 {
			jobs = append(jobs, job{p, m.procs[id]})
		}
	}
	m.mu.RUnlock()
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].p.man.ID < jobs[j].p.man.ID })
	var out []RuleOutcome
	for _, j := range jobs {
		for _, rule := range j.p.man.AssessmentRules {
			ro := RuleOutcome{PluginID: j.p.man.ID, PluginName: j.p.man.Name, Rule: rule, Findings: []Finding{}}
			if j.pr == nil {
				ro.Error = "plugin backend is not running"
			} else {
				var res struct {
					Checked  int       `json:"checked"`
					Findings []Finding `json:"findings"`
				}
				if err := j.pr.Call(ctx, "assess/run", map[string]any{"ruleId": rule.ID, "clusterId": clusterID}, &res); err != nil {
					ro.Error = err.Error()
				} else {
					ro.Checked, ro.Findings = res.Checked, res.Findings
					if ro.Findings == nil {
						ro.Findings = []Finding{}
					}
				}
			}
			out = append(out, ro)
		}
	}
	return out
}
