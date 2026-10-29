// Package settings persists user preferences as a JSON file in the OS config dir.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Theme string

const (
	ThemeDarkBlack Theme = "dark-black"
	ThemeDarkBlue  Theme = "dark-blue"
	ThemeLight     Theme = "light"
)

func (t Theme) valid() bool {
	switch t {
	case ThemeDarkBlack, ThemeDarkBlue, ThemeLight:
		return true
	}
	return false
}

// Settings is the persisted preference set. Fields are added per phase.
type Settings struct {
	Theme Theme `json:"theme"`
	// CheckUpdates enables a once-a-day look at GitHub Releases for a newer version.
	CheckUpdates    bool  `json:"checkUpdates"`
	LastUpdateCheck int64 `json:"lastUpdateCheck"`
}

func defaults() Settings { return Settings{Theme: ThemeDarkBlack, CheckUpdates: true} }

// Store is a concurrency-safe settings store backed by a JSON file.
type Store struct {
	mu   sync.RWMutex
	path string
	cur  Settings
}

// DefaultPath returns <user config dir>/soliton-rudder/settings.json.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "soliton-rudder", "settings.json"), nil
}

// Open loads settings from path. A missing or corrupt file yields defaults.
func Open(path string) (*Store, error) {
	s := &Store{path: path, cur: defaults()}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read settings: %w", err)
	}
	loaded := defaults() // fields missing from older files keep their defaults
	if json.Unmarshal(data, &loaded) == nil && loaded.Theme.valid() {
		s.cur = loaded
	}
	return s, nil
}

func (s *Store) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

func (s *Store) SetTheme(t Theme) error {
	if !t.valid() {
		return fmt.Errorf("unknown theme %q", t)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cur
	next.Theme = t
	if err := s.save(next); err != nil {
		return err
	}
	s.cur = next
	return nil
}

func (s *Store) update(fn func(*Settings)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cur
	fn(&next)
	if err := s.save(next); err != nil {
		return err
	}
	s.cur = next
	return nil
}

func (s *Store) SetCheckUpdates(on bool) error {
	return s.update(func(v *Settings) { v.CheckUpdates = on })
}

func (s *Store) SetLastUpdateCheck(ts int64) error {
	return s.update(func(v *Settings) { v.LastUpdateCheck = ts })
}

// Reset restores every preference to its default.
func (s *Store) Reset() error { return s.update(func(v *Settings) { *v = defaults() }) }

// save writes atomically (temp file + rename) so a crash never corrupts settings.
func (s *Store) save(v Settings) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
