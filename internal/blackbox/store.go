// Package blackbox is a flight recorder for clusters: while Soliton Rudder runs it keeps the changes in health
// and the notable events of a cluster, so the state at an earlier moment can be reconstructed afterwards.
package blackbox

import (
	"database/sql"
	"strings"

	_ "modernc.org/sqlite"
)

// Record is one stored fact. Kind "state" is a change of an object's health; kind "event" is a Kubernetes Event.
type Record struct {
	ID        int64  `json:"id"`
	TS        int64  `json:"ts"` // unix milliseconds
	Kind      string `json:"kind"`
	Key       string `json:"key"` // state: Kind/ns/name; event: uid#count
	ObjKind   string `json:"objKind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Severity  string `json:"severity"` // info | warn | error
	Status    string `json:"status"`   // state only: ok | warn | error | gone
	Reason    string `json:"reason"`
	Message   string `json:"message"`
	Prev      string `json:"prev"` // state only: what it was before
}

type Query struct {
	From        int64    `json:"from"`
	To          int64    `json:"to"`
	Kinds       []string `json:"kinds"`
	Namespace   string   `json:"namespace"`
	MinSeverity string   `json:"minSeverity"` // "" | warn | error
	Text        string   `json:"text"`
	Limit       int      `json:"limit"`
	Newest      bool     `json:"newest"`
}

type Bucket struct {
	TS    int64 `json:"ts"`
	Info  int   `json:"info"`
	Warn  int   `json:"warn"`
	Error int   `json:"error"`
}

type Span struct {
	Min   int64 `json:"min"`
	Max   int64 `json:"max"`
	Count int   `json:"count"`
}

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS bb_records (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  cluster_id TEXT NOT NULL,
  ts INTEGER NOT NULL,
  kind TEXT NOT NULL,
  key TEXT NOT NULL DEFAULT '',
  obj_kind TEXT NOT NULL DEFAULT '',
  namespace TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL DEFAULT '',
  severity TEXT NOT NULL DEFAULT 'info',
  status TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  message TEXT NOT NULL DEFAULT '',
  prev TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS bb_ts ON bb_records(cluster_id, ts);
CREATE INDEX IF NOT EXISTS bb_key ON bb_records(cluster_id, kind, key, id);
CREATE UNIQUE INDEX IF NOT EXISTS bb_event_uniq ON bb_records(cluster_id, key) WHERE kind = 'event';`

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

// Add stores records; an event already stored (same uid and count) is ignored.
func (s *Store) Add(cluster string, recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	st, err := tx.Prepare(`INSERT OR IGNORE INTO bb_records(cluster_id,ts,kind,key,obj_kind,namespace,name,severity,status,reason,message,prev) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer st.Close()
	for _, r := range recs {
		if _, err := st.Exec(cluster, r.TS, r.Kind, r.Key, r.ObjKind, r.Namespace, r.Name, r.Severity, r.Status, r.Reason, r.Message, r.Prev); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

const cols = `id,ts,kind,key,obj_kind,namespace,name,severity,status,reason,message,prev`

func scan(rows *sql.Rows) ([]Record, error) {
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.ID, &r.TS, &r.Kind, &r.Key, &r.ObjKind, &r.Namespace, &r.Name, &r.Severity, &r.Status, &r.Reason, &r.Message, &r.Prev); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// StateAt returns the last known state of every object at time ts (objects that were gone by then are omitted).
func (s *Store) StateAt(cluster string, ts int64) ([]Record, error) {
	rows, err := s.db.Query(`SELECT `+cols+` FROM bb_records WHERE id IN (
		SELECT MAX(id) FROM bb_records WHERE cluster_id=? AND kind='state' AND ts<=? GROUP BY key)
		AND status<>'gone' ORDER BY obj_kind, namespace, name`, cluster, ts)
	if err != nil {
		return nil, err
	}
	return scan(rows)
}

// LastStates returns the most recent state per object, used to avoid re-recording unchanged objects after a restart.
func (s *Store) LastStates(cluster string) ([]Record, error) {
	return s.StateAt(cluster, 1<<62)
}

func (s *Store) Query(cluster string, q Query) ([]Record, error) {
	var where []string
	args := []any{cluster}
	where = append(where, "cluster_id=?")
	if q.From > 0 {
		where, args = append(where, "ts>=?"), append(args, q.From)
	}
	if q.To > 0 {
		where, args = append(where, "ts<=?"), append(args, q.To)
	}
	if len(q.Kinds) > 0 {
		where = append(where, "kind IN ("+strings.TrimSuffix(strings.Repeat("?,", len(q.Kinds)), ",")+")")
		for _, k := range q.Kinds {
			args = append(args, k)
		}
	}
	if q.Namespace != "" {
		where, args = append(where, "namespace=?"), append(args, q.Namespace)
	}
	switch q.MinSeverity {
	case "warn":
		where = append(where, "severity IN ('warn','error')")
	case "error":
		where = append(where, "severity='error'")
	}
	if t := strings.TrimSpace(q.Text); t != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(t) + "%"
		where = append(where, `(name LIKE ? ESCAPE '\' OR reason LIKE ? ESCAPE '\' OR message LIKE ? ESCAPE '\' OR obj_kind LIKE ? ESCAPE '\')`)
		args = append(args, like, like, like, like)
	}
	order := "ts ASC, id ASC"
	if q.Newest {
		order = "ts DESC, id DESC"
	}
	limit := q.Limit
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rows, err := s.db.Query(`SELECT `+cols+` FROM bb_records WHERE `+strings.Join(where, " AND ")+` ORDER BY `+order+` LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	return scan(rows)
}

// Buckets counts records per severity in n equal slices of [from,to].
func (s *Store) Buckets(cluster string, from, to int64, n int) ([]Bucket, error) {
	if n < 1 {
		n = 1
	}
	if to <= from {
		to = from + 1
	}
	width := (to - from + int64(n) - 1) / int64(n)
	out := make([]Bucket, n)
	for i := range out {
		out[i].TS = from + int64(i)*width
	}
	rows, err := s.db.Query(`SELECT (ts-?)/? AS b, severity, COUNT(*) FROM bb_records WHERE cluster_id=? AND ts>=? AND ts<=? AND NOT (kind='state' AND status='ok' AND prev='') GROUP BY b, severity`, from, width, cluster, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var b int
		var sev string
		var c int
		if err := rows.Scan(&b, &sev, &c); err != nil {
			return nil, err
		}
		if b >= n {
			b = n - 1
		}
		switch sev {
		case "error":
			out[b].Error += c
		case "warn":
			out[b].Warn += c
		default:
			out[b].Info += c
		}
	}
	return out, rows.Err()
}

func (s *Store) Span(cluster string) (Span, error) {
	var sp Span
	var min, max sql.NullInt64
	err := s.db.QueryRow(`SELECT MIN(ts), MAX(ts), COUNT(*) FROM bb_records WHERE cluster_id=?`, cluster).Scan(&min, &max, &sp.Count)
	sp.Min, sp.Max = min.Int64, max.Int64
	return sp, err
}

// Prune drops records older than cutoff and the oldest ones beyond maxRows. The latest state of every live object is
// kept (moved up to the cutoff) so the state at any retained moment can still be reconstructed.
func (s *Store) Prune(cluster string, cutoff int64, maxRows int) error {
	if maxRows > 0 {
		var ts sql.NullInt64
		err := s.db.QueryRow(`SELECT ts FROM bb_records WHERE cluster_id=? ORDER BY id DESC LIMIT 1 OFFSET ?`, cluster, maxRows).Scan(&ts)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if ts.Valid && ts.Int64 > cutoff {
			cutoff = ts.Int64
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	const latest = `SELECT MAX(id) FROM bb_records WHERE cluster_id=? AND kind='state' GROUP BY key`
	if _, err := tx.Exec(`DELETE FROM bb_records WHERE cluster_id=? AND ts<? AND (kind<>'state' OR id NOT IN (`+latest+`) OR status='gone')`, cluster, cutoff, cluster); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE bb_records SET ts=? WHERE cluster_id=? AND kind='state' AND ts<?`, cutoff, cluster, cutoff); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Clear(cluster string) error {
	_, err := s.db.Exec(`DELETE FROM bb_records WHERE cluster_id=?`, cluster)
	return err
}
