// Package secrets abstracts credential storage (macOS Keychain / libsecret).
package secrets

import (
	"errors"
	"sync"

	"github.com/zalando/go-keyring"
)

var ErrNotFound = errors.New("secret not found")

type Store interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
}

const serviceName = "soliton-rudder"

type keychain struct{}

// Keychain returns a Store backed by the OS keychain.
func Keychain() Store { return keychain{} }

func (keychain) Get(key string) (string, error) {
	v, err := keyring.Get(serviceName, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return v, err
}

func (keychain) Set(key, value string) error { return keyring.Set(serviceName, key, value) }

func (keychain) Delete(key string) error {
	err := keyring.Delete(serviceName, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// Memory is an in-memory Store for tests.
type Memory struct {
	mu sync.Mutex
	m  map[string]string
}

func NewMemory() *Memory { return &Memory{m: map[string]string{}} }

func (s *Memory) Get(k string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[k]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (s *Memory) Set(k, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = v
	return nil
}

func (s *Memory) Delete(k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, k)
	return nil
}
