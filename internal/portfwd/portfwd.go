// Package portfwd manages saved, auto-reconnecting port forwards.
package portfwd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"soliton-rudder/internal/kube"
)

const EventStatus = "pfstatus"

type Forward struct {
	ID         string `json:"id"`
	ClusterID  string `json:"clusterId"`
	Namespace  string `json:"namespace"`
	Kind       string `json:"kind"` // pod | service | deployment | statefulset | daemonset | replicaset
	Name       string `json:"name"`
	RemotePort int    `json:"remotePort"`
	LocalPort  int    `json:"localPort"` // 0 = pick a free port
	Auto       bool   `json:"auto"`      // start when the cluster connects
	CreatedAt  int64  `json:"createdAt"`
}

// Runtime is a forward plus its live state.
type Runtime struct {
	Forward
	Status     string `json:"status"` // stopped | connecting | active | healing | degraded | error
	ActivePort int    `json:"activePort"`
	Pod        string `json:"pod"`
	Message    string `json:"message"`
	// Self-healing metrics for the current run (reset when the forward is stopped and started again).
	Reconnects       int    `json:"reconnects"`
	LastHealedReason string `json:"lastHealedReason"`
	LastHealedAt     int64  `json:"lastHealedAt"`
}

type SessionFunc func(clusterID string) (*kube.Session, error)

type Manager struct {
	db      *sql.DB
	session SessionFunc
	emit    func(string, any)

	mu      sync.Mutex
	state   map[string]*Runtime
	cancels map[string]context.CancelFunc
}

const schema = `
CREATE TABLE IF NOT EXISTS port_forwards (
  id TEXT PRIMARY KEY,
  cluster_id TEXT NOT NULL,
  namespace TEXT NOT NULL,
  kind TEXT NOT NULL,
  name TEXT NOT NULL,
  remote_port INTEGER NOT NULL,
  local_port INTEGER NOT NULL DEFAULT 0,
  auto INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);`

func NewManager(dbPath string, session SessionFunc, emit func(string, any)) (*Manager, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout=5000;`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Manager{db: db, session: session, emit: emit, state: map[string]*Runtime{}, cancels: map[string]context.CancelFunc{}}, nil
}

func (m *Manager) Close() error {
	m.StopAll()
	return m.db.Close()
}

func (m *Manager) load(where string, args ...any) ([]Forward, error) {
	rows, err := m.db.Query(`SELECT id,cluster_id,namespace,kind,name,remote_port,local_port,auto,created_at FROM port_forwards `+where+` ORDER BY created_at, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Forward{}
	for rows.Next() {
		var f Forward
		var auto int
		if err := rows.Scan(&f.ID, &f.ClusterID, &f.Namespace, &f.Kind, &f.Name, &f.RemotePort, &f.LocalPort, &auto, &f.CreatedAt); err != nil {
			return nil, err
		}
		f.Auto = auto == 1
		out = append(out, f)
	}
	return out, rows.Err()
}

// List returns saved forwards for a cluster with live state.
func (m *Manager) List(clusterID string) ([]Runtime, error) {
	fs, err := m.load(`WHERE cluster_id=?`, clusterID)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Runtime, 0, len(fs))
	for _, f := range fs {
		if rt, ok := m.state[f.ID]; ok {
			c := *rt
			c.Forward = f
			out = append(out, c)
		} else {
			out = append(out, Runtime{Forward: f, Status: "stopped"})
		}
	}
	return out, nil
}

var validKinds = map[string]bool{"pod": true, "service": true, "deployment": true, "statefulset": true, "daemonset": true, "replicaset": true}

func validate(f Forward) error {
	switch {
	case f.ClusterID == "" || f.Namespace == "" || f.Name == "":
		return errors.New("cluster, namespace and name are required")
	case !validKinds[f.Kind]:
		return fmt.Errorf("unsupported kind %q", f.Kind)
	case f.RemotePort < 1 || f.RemotePort > 65535:
		return errors.New("remote port must be 1-65535")
	case f.LocalPort < 0 || f.LocalPort > 65535 || (f.LocalPort > 0 && f.LocalPort < 1024):
		return errors.New("local port must be 0 (auto) or 1024-65535")
	}
	return nil
}

// Add saves a forward (and starts it when start is true).
func (m *Manager) Add(f Forward, start bool) (Runtime, error) {
	if err := validate(f); err != nil {
		return Runtime{}, err
	}
	f.ID, f.CreatedAt = uuid.NewString(), time.Now().Unix()
	if _, err := m.db.Exec(`INSERT INTO port_forwards (id,cluster_id,namespace,kind,name,remote_port,local_port,auto,created_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		f.ID, f.ClusterID, f.Namespace, f.Kind, f.Name, f.RemotePort, f.LocalPort, b2i(f.Auto), f.CreatedAt); err != nil {
		return Runtime{}, err
	}
	rt := Runtime{Forward: f, Status: "stopped"}
	if start {
		if err := m.Start(f.ID); err != nil {
			return rt, err
		}
		rt.Status = "connecting"
	}
	return rt, nil
}

// Ensure returns the saved forward for the same target and ports (creating it if needed) and starts it.
// It makes one-click forwarding idempotent: clicking twice reuses the existing forward instead of piling up copies.
func (m *Manager) Ensure(f Forward) (Runtime, error) {
	if err := validate(f); err != nil {
		return Runtime{}, err
	}
	existing, err := m.load(`WHERE cluster_id=? AND namespace=? AND kind=? AND name=? AND remote_port=?`, f.ClusterID, f.Namespace, f.Kind, f.Name, f.RemotePort)
	if err != nil {
		return Runtime{}, err
	}
	if len(existing) > 0 {
		if err := m.Start(existing[0].ID); err != nil {
			return Runtime{}, err
		}
		list, _ := m.List(f.ClusterID)
		for _, r := range list {
			if r.ID == existing[0].ID {
				return r, nil
			}
		}
	}
	return m.Add(f, true)
}

func (m *Manager) SetAuto(id string, auto bool) error {
	res, err := m.db.Exec(`UPDATE port_forwards SET auto=? WHERE id=?`, b2i(auto), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("forward not found")
	}
	return nil
}

func (m *Manager) Remove(id string) error {
	m.Stop(id)
	m.mu.Lock()
	delete(m.state, id)
	m.mu.Unlock()
	_, err := m.db.Exec(`DELETE FROM port_forwards WHERE id=?`, id)
	return err
}

// RemoveCluster drops every forward belonging to a deleted cluster.
func (m *Manager) RemoveCluster(clusterID string) {
	fs, _ := m.load(`WHERE cluster_id=?`, clusterID)
	for _, f := range fs {
		_ = m.Remove(f.ID)
	}
}

func (m *Manager) Start(id string) error {
	fs, err := m.load(`WHERE id=?`, id)
	if err != nil {
		return err
	}
	if len(fs) == 0 {
		return errors.New("forward not found")
	}
	f := fs[0]
	m.mu.Lock()
	if _, running := m.cancels[id]; running {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	sess, err := m.session(f.ClusterID)
	if err != nil {
		return err
	}
	if sess == nil {
		return errors.New("the cluster is not connected")
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancels[id] = cancel
	m.mu.Unlock()
	m.set(f, kube.ForwardState{Status: "connecting", LocalPort: f.LocalPort})

	target := kube.ForwardTarget{Namespace: f.Namespace, Kind: f.Kind, Name: f.Name}
	go func() {
		sess.RunForward(ctx, target, f.RemotePort, f.LocalPort, func(st kube.ForwardState) { m.set(f, st) })
	}()
	return nil
}

func (m *Manager) Stop(id string) {
	m.mu.Lock()
	cancel := m.cancels[id]
	delete(m.cancels, id)
	rt := m.state[id]
	m.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	if rt != nil {
		m.mu.Lock()
		rt.Status, rt.ActivePort, rt.Pod, rt.Message = "stopped", 0, "", ""
		rt.Reconnects, rt.LastHealedReason, rt.LastHealedAt = 0, "", 0
		c := *rt
		m.mu.Unlock()
		m.emit(EventStatus, c)
	}
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.cancels))
	for id := range m.cancels {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Stop(id)
	}
}

// StopCluster stops (without deleting) all forwards of a cluster.
func (m *Manager) StopCluster(clusterID string) {
	fs, _ := m.load(`WHERE cluster_id=?`, clusterID)
	for _, f := range fs {
		m.Stop(f.ID)
	}
}

// StartAuto starts the cluster's auto-start forwards.
func (m *Manager) StartAuto(clusterID string) {
	fs, _ := m.load(`WHERE cluster_id=? AND auto=1`, clusterID)
	for _, f := range fs {
		_ = m.Start(f.ID)
	}
}

func (m *Manager) set(f Forward, st kube.ForwardState) {
	m.mu.Lock()
	if _, running := m.cancels[f.ID]; !running {
		m.mu.Unlock()
		return // stopped meanwhile; ignore late reports
	}
	rt := m.state[f.ID]
	if rt == nil {
		rt = &Runtime{Forward: f}
		m.state[f.ID] = rt
	}
	rt.Status, rt.ActivePort, rt.Pod, rt.Message = st.Status, st.LocalPort, st.Pod, st.Message
	rt.Reconnects, rt.LastHealedReason, rt.LastHealedAt = st.Reconnects, st.LastHealedReason, st.LastHealedAt
	c := *rt
	m.mu.Unlock()
	m.emit(EventStatus, c)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
