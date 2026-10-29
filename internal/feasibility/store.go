package feasibility

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"errors"
	"io"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("report not found")

const keepPerCluster = 100

// Store keeps assessment history in SQLite (reports are gzip-compressed JSON).
type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS feasibility_runs (
  id TEXT PRIMARY KEY,
  cluster_id TEXT NOT NULL,
  started_at INTEGER NOT NULL,
  duration_ms INTEGER NOT NULL,
  score INTEGER NOT NULL,
  server_version TEXT NOT NULL DEFAULT '',
  counts TEXT NOT NULL,
  categories TEXT NOT NULL,
  report BLOB NOT NULL
);
CREATE INDEX IF NOT EXISTS feasibility_cluster ON feasibility_runs(cluster_id, started_at);`

func OpenStore(path string) (*Store, error) {
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

func gz(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(b); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gunzip(b []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// Save stores a report and trims the cluster's history to the newest keepPerCluster runs.
func (s *Store) Save(r Report) error {
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	blob, err := gz(raw)
	if err != nil {
		return err
	}
	counts, _ := json.Marshal(r.Counts)
	cats, _ := json.Marshal(r.Categories)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO feasibility_runs (id,cluster_id,started_at,duration_ms,score,server_version,counts,categories,report) VALUES (?,?,?,?,?,?,?,?,?)`,
		r.ID, r.ClusterID, r.StartedAt, r.DurationMs, r.Score, r.ServerVersion, string(counts), string(cats), blob); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM feasibility_runs WHERE cluster_id=? AND id NOT IN (SELECT id FROM feasibility_runs WHERE cluster_id=? ORDER BY started_at DESC, id DESC LIMIT ?)`,
		r.ClusterID, r.ClusterID, keepPerCluster); err != nil {
		return err
	}
	return tx.Commit()
}

// List returns history summaries, oldest first (chart order).
func (s *Store) List(clusterID string) ([]Summary, error) {
	rows, err := s.db.Query(`SELECT id,cluster_id,started_at,duration_ms,score,server_version,counts,categories FROM feasibility_runs WHERE cluster_id=? ORDER BY started_at ASC, id ASC`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Summary{}
	for rows.Next() {
		var sm Summary
		var counts, cats string
		if err := rows.Scan(&sm.ID, &sm.ClusterID, &sm.StartedAt, &sm.DurationMs, &sm.Score, &sm.ServerVersion, &counts, &cats); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(counts), &sm.Counts)
		_ = json.Unmarshal([]byte(cats), &sm.Categories)
		out = append(out, sm)
	}
	return out, rows.Err()
}

func (s *Store) Get(id string) (Report, error) {
	var blob []byte
	err := s.db.QueryRow(`SELECT report FROM feasibility_runs WHERE id=?`, id).Scan(&blob)
	if errors.Is(err, sql.ErrNoRows) {
		return Report{}, ErrNotFound
	}
	if err != nil {
		return Report{}, err
	}
	raw, err := gunzip(blob)
	if err != nil {
		return Report{}, err
	}
	var r Report
	return r, json.Unmarshal(raw, &r)
}

func (s *Store) Delete(id string) error {
	res, err := s.db.Exec(`DELETE FROM feasibility_runs WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteCluster(clusterID string) error {
	_, err := s.db.Exec(`DELETE FROM feasibility_runs WHERE cluster_id=?`, clusterID)
	return err
}

// DeleteAll removes the whole assessment history (all clusters).
func (s *Store) DeleteAll() error {
	_, err := s.db.Exec(`DELETE FROM feasibility_runs`)
	return err
}
