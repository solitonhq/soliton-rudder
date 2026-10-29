package kube

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	tableAccept   = "application/json;as=Table;v=v1;g=meta.k8s.io,application/json"
	listPageSize  = 500
	flushInterval = 100 * time.Millisecond
	watchTimeout  = 5 * 60 // seconds; server closes, we resume from the last resourceVersion
	maxBackoff    = 10 * time.Second
)

type rowMeta struct {
	Metadata struct {
		Name              string `json:"name"`
		Namespace         string `json:"namespace"`
		UID               string `json:"uid"`
		CreationTimestamp string `json:"creationTimestamp"`
	} `json:"metadata"`
}

func toColumns(defs []metav1.TableColumnDefinition) []Column {
	out := make([]Column, len(defs))
	for i, d := range defs {
		out[i] = Column{Name: d.Name, Type: d.Type, Priority: d.Priority, Description: d.Description}
	}
	return out
}

func toRows(rows []metav1.TableRow) []Row {
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		var m rowMeta
		if len(r.Object.Raw) > 0 {
			_ = json.Unmarshal(r.Object.Raw, &m)
		}
		if m.Metadata.UID == "" {
			continue
		}
		out = append(out, Row{
			UID: m.Metadata.UID, Namespace: m.Metadata.Namespace, Name: m.Metadata.Name,
			Created: m.Metadata.CreationTimestamp, Cells: r.Cells,
		})
	}
	return out
}

type watchEvent struct {
	Type   string          `json:"type"`
	Object json.RawMessage `json:"object"`
}

// statusSink receives the state of one stream; for a multi-namespace table the states are merged.
type statusSink func(state, message string)

// runStream keeps a resource's table in sync until ctx is cancelled. With full access that is one cluster-wide
// stream; for a user who can only read some namespaces it is one stream per namespace feeding the same table.
func (s *Session) runStream(ctx context.Context, sub string, res Resource) {
	targets := s.listTargets(ctx, res, "")
	if len(targets) == 1 {
		s.streamOne(ctx, sub, res, targets[0], "", false, func(state, msg string) {
			s.emit(EventStatus, StatusEvent{Sub: sub, State: state, Message: msg})
		})
		return
	}
	var (
		mu     sync.Mutex
		live   = map[string]bool{}
		failed = map[string]string{}
	)
	report := func(ns string) statusSink {
		return func(state, msg string) {
			mu.Lock()
			defer mu.Unlock()
			switch state {
			case "live":
				live[ns] = true
				delete(failed, ns)
				s.emit(EventStatus, StatusEvent{Sub: sub, State: "live"})
			case "error":
				failed[ns] = msg
				// one namespace failing must not blank a table that other namespaces keep filling
				if len(live) == 0 && len(failed) == len(targets) {
					s.emit(EventStatus, StatusEvent{Sub: sub, State: "error", Message: msg})
				}
			case "forbidden":
				failed[ns] = msg
				if len(live) == 0 && len(failed) == len(targets) {
					s.emit(EventStatus, StatusEvent{Sub: sub, State: "error", Message: fmt.Sprintf(
						"You do not have permission to list %s in the namespaces being used (%s). Choose other namespaces in the cluster's access settings.",
						res.Name, strings.Join(targets, ", "))})
				}
			}
		}
	}
	s.emit(EventStatus, StatusEvent{Sub: sub, State: "connecting"})
	var wg sync.WaitGroup
	for _, ns := range targets {
		wg.Add(1)
		go func(ns string) {
			defer wg.Done()
			s.streamOne(ctx, sub, res, ns, ns, true, report(ns))
		}(ns)
	}
	wg.Wait()
}

// streamOne is the list → snapshot → watch loop for one namespace (or the whole cluster for ns == ""). scope is
// attached to its snapshots so that a relist replaces only this namespace's rows. With skipForbidden, a namespace the
// user may not read is given up on instead of retried.
func (s *Session) streamOne(ctx context.Context, sub string, res Resource, ns, scope string, skipForbidden bool, sink statusSink) {
	backoff := time.Second
	for ctx.Err() == nil {
		sink("connecting", "")
		cols, rows, rv, err := s.list(ctx, res, ns)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if skipForbidden && apierrors.IsForbidden(err) {
				sink("forbidden", err.Error())
				return
			}
			sink("error", err.Error())
			if !sleep(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		backoff = time.Second
		s.emit(EventRows, RowsEvent{Sub: sub, Reset: true, Scope: scope, Columns: cols, Upsert: rows, Delete: []string{}})
		sink("live", "")

		err = s.watch(ctx, sub, res, ns, rv)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			sink("error", err.Error())
			if !sleep(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, maxBackoff)
		}
	}
}

func (s *Session) list(ctx context.Context, res Resource, ns string) ([]Column, []Row, string, error) {
	var (
		cols []Column
		rows []Row
		rv   string
		cont string
	)
	for {
		req := s.rc.Get().AbsPath(apiPathNS(res, ns)).SetHeader("Accept", tableAccept).Param("limit", strconv.Itoa(listPageSize))
		if cont != "" {
			req = req.Param("continue", cont)
		}
		raw, err := req.Do(ctx).Raw()
		if err != nil {
			return nil, nil, "", err
		}
		var t metav1.Table
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, nil, "", fmt.Errorf("decode table: %w", err)
		}
		if cols == nil {
			cols = toColumns(t.ColumnDefinitions)
		}
		rows = append(rows, toRows(t.Rows)...)
		rv, cont = t.ResourceVersion, t.Continue
		if cont == "" {
			return cols, rows, rv, nil
		}
	}
}

// watch streams changes from rv, resuming across server-side timeouts. It returns
// nil when the stream should simply be re-established from a fresh list.
func (s *Session) watch(ctx context.Context, sub string, res Resource, ns, rv string) error {
	pending := map[string]Row{}
	deleted := map[string]struct{}{}
	flush := func() {
		if len(pending) == 0 && len(deleted) == 0 {
			return
		}
		ev := RowsEvent{Sub: sub, Upsert: make([]Row, 0, len(pending)), Delete: make([]string, 0, len(deleted))}
		for _, r := range pending {
			ev.Upsert = append(ev.Upsert, r)
		}
		for uid := range deleted {
			ev.Delete = append(ev.Delete, uid)
		}
		pending, deleted = map[string]Row{}, map[string]struct{}{}
		s.emit(EventRows, ev)
	}
	defer flush()

	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for ctx.Err() == nil {
		stream, err := s.rc.Get().AbsPath(apiPathNS(res, ns)).SetHeader("Accept", tableAccept).
			Param("watch", "true").Param("resourceVersion", rv).
			Param("allowWatchBookmarks", "true").Param("timeoutSeconds", strconv.Itoa(watchTimeout)).
			Stream(ctx)
		if err != nil {
			if apierrors.IsResourceExpired(err) || apierrors.IsGone(err) {
				return nil // relist
			}
			return err
		}

		events := make(chan watchEvent, 256)
		readErr := make(chan error, 1)
		go func() {
			defer close(events)
			defer stream.Close()
			readErr <- readEvents(ctx, stream, events)
		}()

	loop:
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				flush()
			case ev, ok := <-events:
				if !ok {
					break loop
				}
				switch ev.Type {
				case "ADDED", "MODIFIED", "DELETED":
					var t metav1.Table
					if json.Unmarshal(ev.Object, &t) != nil {
						continue
					}
					if t.ResourceVersion != "" {
						rv = t.ResourceVersion
					}
					for _, r := range toRows(t.Rows) {
						if ev.Type == "DELETED" {
							delete(pending, r.UID)
							deleted[r.UID] = struct{}{}
						} else {
							delete(deleted, r.UID)
							pending[r.UID] = r
						}
					}
				case "BOOKMARK":
					var m struct {
						Metadata struct {
							ResourceVersion string `json:"resourceVersion"`
						} `json:"metadata"`
					}
					if json.Unmarshal(ev.Object, &m) == nil && m.Metadata.ResourceVersion != "" {
						rv = m.Metadata.ResourceVersion
					}
				case "ERROR":
					var st metav1.Status
					_ = json.Unmarshal(ev.Object, &st)
					if st.Code == 410 {
						return nil // resourceVersion too old: relist
					}
					return errors.New(st.Message)
				}
			}
		}
		flush()
		if err := <-readErr; err != nil && ctx.Err() == nil && !errors.Is(err, io.EOF) {
			return err
		}
		// Clean EOF (server-side timeout): resume from rv.
	}
	return nil
}

func readEvents(ctx context.Context, r io.Reader, out chan<- watchEvent) error {
	br := bufio.NewReaderSize(r, 64*1024)
	dec := json.NewDecoder(br)
	for {
		var ev watchEvent
		if err := dec.Decode(&ev); err != nil {
			return err
		}
		select {
		case out <- ev:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
