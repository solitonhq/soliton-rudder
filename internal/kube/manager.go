package kube

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"k8s.io/client-go/rest"
)

// ConfigFunc resolves a rest.Config for a registered cluster id.
type ConfigFunc func(clusterID string) (*rest.Config, error)

// Manager owns one Session per connected cluster.
type Manager struct {
	cfg  ConfigFunc
	emit Emitter

	mu       sync.Mutex
	sessions map[string]*Session
	subOwner map[string]string // sub id -> cluster id
	closed   map[string]bool   // disconnected on purpose: background work must not quietly reopen these
	scope    func(clusterID string) ScopeHooks
}

// EventConnections is emitted with the sorted ids of the connected clusters whenever that set changes.
const EventConnections = "connections"

// ErrDisconnected is returned to background work for a cluster the user disconnected.
var ErrDisconnected = errors.New("this cluster is disconnected: connect it again to resume")

func NewManager(cfg ConfigFunc, emit Emitter) *Manager {
	return &Manager{cfg: cfg, emit: emit, sessions: map[string]*Session{}, subOwner: map[string]string{}, closed: map[string]bool{}}
}

// Connect (re)uses the cluster's session and returns a fresh resource catalog.
func (m *Manager) Connect(ctx context.Context, clusterID string) ([]Resource, error) {
	m.reopen(clusterID)
	s, err := m.session(clusterID, true)
	if err != nil {
		return nil, err
	}
	return s.Discover(ctx)
}

func (m *Manager) session(clusterID string, create bool) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[clusterID]; ok {
		return s, nil
	}
	if !create {
		return nil, fmt.Errorf("cluster not connected")
	}
	cfg, err := m.cfg(clusterID)
	if err != nil {
		return nil, err
	}
	s, err := NewSession(clusterID, cfg, m.emit)
	if err != nil {
		return nil, err
	}
	if m.scope != nil {
		s.SetScopeHooks(m.scope(clusterID))
	}
	m.sessions[clusterID] = s
	m.emitConnectionsLocked()
	return s, nil
}

// SetScopeProvider tells sessions where to learn the user's namespaces from (settings and the kubeconfig).
func (m *Manager) SetScopeProvider(f func(clusterID string) ScopeHooks) {
	m.mu.Lock()
	m.scope = f
	m.mu.Unlock()
}

// reopen clears a deliberate disconnect: the user is using the cluster again.
func (m *Manager) reopen(clusterID string) {
	m.mu.Lock()
	delete(m.closed, clusterID)
	m.mu.Unlock()
}

// Connected returns the sorted ids of the clusters that have a live session.
func (m *Manager) Connected() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connectedLocked()
}

func (m *Manager) connectedLocked() []string {
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (m *Manager) emitConnectionsLocked() {
	if m.emit != nil {
		m.emit(EventConnections, m.connectedLocked())
	}
}

// EnsureReady connects to the cluster if needed and makes sure its resource catalog is loaded, so features that
// work on a cluster other than the one open in the explorer (comparison) do not depend on it having been opened.
func (m *Manager) EnsureReady(ctx context.Context, clusterID string) (*Session, error) {
	m.reopen(clusterID)
	s, err := m.session(clusterID, true)
	if err != nil {
		return nil, err
	}
	if !s.hasCatalog() {
		if _, err := s.Discover(ctx); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Session returns an existing connected session.
func (m *Manager) Session(clusterID string) (*Session, error) { return m.session(clusterID, false) }

func (m *Manager) Disconnect(clusterID string) {
	m.mu.Lock()
	s := m.sessions[clusterID]
	delete(m.sessions, clusterID)
	m.closed[clusterID] = true
	m.emitConnectionsLocked()
	m.mu.Unlock()
	if s != nil {
		s.Close()
	}
}

func (m *Manager) Subscribe(clusterID, key string) (string, error) {
	s, err := m.session(clusterID, false)
	if err != nil {
		return "", err
	}
	id, err := s.Subscribe(key)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	m.subOwner[id] = clusterID
	m.mu.Unlock()
	return id, nil
}

func (m *Manager) Unsubscribe(subID string) {
	m.mu.Lock()
	owner := m.subOwner[subID]
	delete(m.subOwner, subID)
	s := m.sessions[owner]
	m.mu.Unlock()
	if s != nil {
		s.Unsubscribe(subID)
	}
}

// Ensure returns the cluster's session, creating it (without discovery) if needed.
// An explicit use (a comparison, an assessment, a forward the user starts) counts as connecting again.
func (m *Manager) Ensure(clusterID string) (*Session, error) {
	m.reopen(clusterID)
	return m.session(clusterID, true)
}

// EnsureBackground is for work nobody is waiting for (the blackbox recorder): it connects on first use, but never
// reopens a cluster the user disconnected.
func (m *Manager) EnsureBackground(clusterID string) (*Session, error) {
	m.mu.Lock()
	closed := m.closed[clusterID]
	m.mu.Unlock()
	if closed {
		return nil, ErrDisconnected
	}
	return m.session(clusterID, true)
}

// StreamLogs starts a log stream on a connected cluster; stop it with Unsubscribe.
func (m *Manager) StreamLogs(clusterID string, opts LogOptions) (string, error) {
	s, err := m.session(clusterID, false)
	if err != nil {
		return "", err
	}
	id, err := s.StreamLogs(opts)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	m.subOwner[id] = clusterID
	m.mu.Unlock()
	return id, nil
}
