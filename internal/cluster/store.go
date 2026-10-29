package cluster

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("cluster not found")

// Store persists cluster records in SQLite.
type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS clusters (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  color TEXT NOT NULL,
  icon TEXT NOT NULL DEFAULT '',
  pinned INTEGER NOT NULL DEFAULT 0,
  position INTEGER NOT NULL DEFAULT 0,
  source TEXT NOT NULL,
  kubeconfig_path TEXT NOT NULL DEFAULT '',
  context TEXT NOT NULL,
  created_at INTEGER NOT NULL
);`

func OpenStore(path string) (*Store, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // single writer; avoids SQLITE_BUSY and keeps :memory: coherent
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

const cols = `id,name,color,icon,pinned,position,source,kubeconfig_path,context,created_at`

func scan(r interface{ Scan(...any) error }) (Cluster, error) {
	var c Cluster
	var pinned int
	err := r.Scan(&c.ID, &c.Name, &c.Color, &c.Icon, &pinned, &c.Position, &c.Source, &c.KubeconfigPath, &c.Context, &c.CreatedAt)
	c.Pinned = pinned == 1
	return c, err
}

func (s *Store) List() ([]Cluster, error) {
	rows, err := s.db.Query(`SELECT ` + cols + ` FROM clusters ORDER BY pinned DESC, position ASC, created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Cluster{}
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) Get(id string) (Cluster, error) {
	c, err := scan(s.db.QueryRow(`SELECT `+cols+` FROM clusters WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Cluster{}, ErrNotFound
	}
	return c, err
}

func (s *Store) Insert(c Cluster) (Cluster, error) {
	if c.CreatedAt == 0 {
		c.CreatedAt = time.Now().Unix()
	}
	var max sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(position) FROM clusters`).Scan(&max); err != nil {
		return c, err
	}
	c.Position = int(max.Int64) + 1
	_, err := s.db.Exec(`INSERT INTO clusters (`+cols+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.Name, c.Color, c.Icon, b2i(c.Pinned), c.Position, c.Source, c.KubeconfigPath, c.Context, c.CreatedAt)
	return c, err
}

func (s *Store) Update(id string, p Patch) (Cluster, error) {
	c, err := s.Get(id)
	if err != nil {
		return c, err
	}
	if p.Name != nil {
		c.Name = *p.Name
	}
	if p.Color != nil {
		c.Color = *p.Color
	}
	if p.Icon != nil {
		c.Icon = *p.Icon
	}
	if p.Pinned != nil {
		c.Pinned = *p.Pinned
	}
	_, err = s.db.Exec(`UPDATE clusters SET name=?,color=?,icon=?,pinned=? WHERE id=?`,
		c.Name, c.Color, c.Icon, b2i(c.Pinned), id)
	return c, err
}

func (s *Store) Delete(id string) error {
	res, err := s.db.Exec(`DELETE FROM clusters WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Reorder assigns positions following the given id order, atomically.
func (s *Store) Reorder(ids []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE clusters SET position=? WHERE id=?`, i, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
