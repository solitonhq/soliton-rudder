package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

const (
	maxKeyLen    = 128
	maxValueSize = 256 << 10
	maxTotalSize = 2 << 20
	maxKeys      = 500
)

// Storage is a small per-plugin key/value store (JSON values) kept in the plugin's data directory.
type Storage struct {
	root string
	mu   sync.Mutex
}

func NewStorage(root string) *Storage { return &Storage{root: root} }

func (s *Storage) path(id string) string { return filepath.Join(s.root, id, "kv.json") }

func (s *Storage) load(id string) (map[string]json.RawMessage, error) {
	b, err := os.ReadFile(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]json.RawMessage{}
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]json.RawMessage{}, nil // corrupt: start fresh rather than wedge the plugin
	}
	return m, nil
}

func (s *Storage) save(id string, m map[string]json.RawMessage) error {
	if err := os.MkdirAll(filepath.Dir(s.path(id)), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp := s.path(id) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(id))
}

func (s *Storage) Get(id, key string) (json.RawMessage, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load(id)
	if err != nil {
		return nil, false, err
	}
	v, ok := m[key]
	return v, ok, nil
}

func (s *Storage) Set(id, key string, value json.RawMessage) error {
	if key == "" || len(key) > maxKeyLen {
		return fmt.Errorf("key must be 1–%d characters", maxKeyLen)
	}
	if len(value) > maxValueSize {
		return fmt.Errorf("value exceeds %d KB", maxValueSize>>10)
	}
	if !json.Valid(value) {
		return errors.New("value must be valid JSON")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load(id)
	if err != nil {
		return err
	}
	if _, exists := m[key]; !exists && len(m) >= maxKeys {
		return fmt.Errorf("storage is limited to %d keys", maxKeys)
	}
	m[key] = value
	total := 0
	for _, v := range m {
		total += len(v)
	}
	if total > maxTotalSize {
		return fmt.Errorf("plugin storage is limited to %d MB", maxTotalSize>>20)
	}
	return s.save(id, m)
}

func (s *Storage) Delete(id, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load(id)
	if err != nil {
		return err
	}
	delete(m, key)
	return s.save(id, m)
}

func (s *Storage) Keys(id string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load(id)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// Purge removes all of a plugin's data.
func (s *Storage) Purge(id string) error { return os.RemoveAll(filepath.Join(s.root, id)) }
