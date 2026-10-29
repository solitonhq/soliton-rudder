// Package integrations persists per-cluster integration settings (e.g. the Prometheus endpoint).
package integrations

import (
	"database/sql"
	"encoding/json"
	"errors"

	_ "modernc.org/sqlite"

	"soliton-rudder/internal/kube"
)

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS integration_settings (
  cluster_id TEXT NOT NULL,
  key TEXT NOT NULL,
  value TEXT NOT NULL,
  PRIMARY KEY (cluster_id, key)
);`

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
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
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) get(cluster, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM integration_settings WHERE cluster_id=? AND key=?`, cluster, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

func (s *Store) set(cluster, key, value string) error {
	_, err := s.db.Exec(`INSERT INTO integration_settings (cluster_id,key,value) VALUES (?,?,?) ON CONFLICT(cluster_id,key) DO UPDATE SET value=excluded.value`, cluster, key, value)
	return err
}

func (s *Store) PromConfig(cluster string) (kube.PromConfig, bool, error) {
	v, ok, err := s.get(cluster, "prometheus")
	if err != nil || !ok {
		return kube.PromConfig{}, false, err
	}
	var c kube.PromConfig
	if err := json.Unmarshal([]byte(v), &c); err != nil {
		return kube.PromConfig{}, false, nil // corrupt entry: treat as unconfigured
	}
	return c, true, nil
}

func (s *Store) SetPromConfig(cluster string, c kube.PromConfig) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return s.set(cluster, "prometheus", string(b))
}

func (s *Store) ClearPromConfig(cluster string) error {
	_, err := s.db.Exec(`DELETE FROM integration_settings WHERE cluster_id=? AND key='prometheus'`, cluster)
	return err
}

func (s *Store) DeleteCluster(cluster string) error {
	_, err := s.db.Exec(`DELETE FROM integration_settings WHERE cluster_id=?`, cluster)
	return err
}

// GetRaw / SetRaw keep small JSON documents under an arbitrary scope (a cluster id, or "_global").
func (s *Store) GetRaw(scope, key string) (string, bool, error) { return s.get(scope, key) }

func (s *Store) SetRaw(scope, key, value string) error { return s.set(scope, key, value) }
