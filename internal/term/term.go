// Package term runs interactive terminal sessions (local PTY or remote exec streams)
// and relays their I/O to the UI as events.
package term

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/creack/pty"
	"k8s.io/client-go/tools/remotecommand"
)

type Emitter func(event string, data any)

const (
	EventOutput = "term"     // {id, data: base64}
	EventExit   = "termexit" // {id, error}
)

type OutputEvent struct {
	ID   string `json:"id"`
	Data string `json:"data"`
}

type ExitEvent struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

// Size is a terminal size in cells.
type Size struct{ Cols, Rows uint16 }

// RunFunc runs a remote streaming session until it ends or ctx is cancelled.
type RunFunc func(ctx context.Context, stdin io.Reader, stdout io.Writer, size remotecommand.TerminalSizeQueue) error

type session struct {
	id     string
	write  func([]byte) error
	resize func(Size)
	cancel context.CancelFunc
}

type Hub struct {
	emit     Emitter
	mu       sync.Mutex
	sessions map[string]*session
}

var seq atomic.Uint64

func NewHub(emit Emitter) *Hub { return &Hub{emit: emit, sessions: map[string]*session{}} }

func (h *Hub) add(s *session) {
	h.mu.Lock()
	h.sessions[s.id] = s
	h.mu.Unlock()
}

func (h *Hub) finish(id string, err error) {
	h.mu.Lock()
	delete(h.sessions, id)
	h.mu.Unlock()
	ev := ExitEvent{ID: id}
	if err != nil {
		ev.Error = err.Error()
	}
	h.emit(EventExit, ev)
}

func (h *Hub) Write(id string, data []byte) error {
	h.mu.Lock()
	s := h.sessions[id]
	h.mu.Unlock()
	if s == nil {
		return fmt.Errorf("terminal %s not found", id)
	}
	return s.write(data)
}

func (h *Hub) Resize(id string, cols, rows int) {
	h.mu.Lock()
	s := h.sessions[id]
	h.mu.Unlock()
	if s != nil && cols > 0 && rows > 0 {
		s.resize(Size{uint16(cols), uint16(rows)})
	}
}

func (h *Hub) Close(id string) {
	h.mu.Lock()
	s := h.sessions[id]
	h.mu.Unlock()
	if s != nil {
		s.cancel()
	}
}

func (h *Hub) CloseAll() {
	h.mu.Lock()
	all := make([]*session, 0, len(h.sessions))
	for _, s := range h.sessions {
		all = append(all, s)
	}
	h.mu.Unlock()
	for _, s := range all {
		s.cancel()
	}
}

// StartRemote runs a remote exec stream (e.g. kubectl-exec style) as a session.
func (h *Hub) StartRemote(size Size, run RunFunc) string {
	id := fmt.Sprintf("t%d", seq.Add(1))
	ctx, cancel := context.WithCancel(context.Background())
	pr, pw := io.Pipe()
	q := newSizeQueue(ctx)
	q.push(size)
	out := newBatchWriter(func(b []byte) {
		h.emit(EventOutput, OutputEvent{ID: id, Data: base64.StdEncoding.EncodeToString(b)})
	})
	h.add(&session{
		id:     id,
		write:  func(b []byte) error { _, err := pw.Write(b); return err },
		resize: q.push,
		cancel: cancel,
	})
	go func() {
		err := run(ctx, pr, out, q)
		out.Flush()
		pr.Close()
		cancel()
		if ctx.Err() != nil && err != nil {
			err = nil // closed by the user
		}
		h.finish(id, err)
	}()
	return id
}

// StartLocal runs a command attached to a PTY. cleanup runs once the session ends.
func (h *Hub) StartLocal(cmd *exec.Cmd, size Size, cleanup func()) (string, error) {
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: size.Cols, Rows: size.Rows})
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return "", err
	}
	id := fmt.Sprintf("t%d", seq.Add(1))
	ctx, cancel := context.WithCancel(context.Background())
	out := newBatchWriter(func(b []byte) {
		h.emit(EventOutput, OutputEvent{ID: id, Data: base64.StdEncoding.EncodeToString(b)})
	})
	h.add(&session{
		id:     id,
		write:  func(b []byte) error { _, err := f.Write(b); return err },
		resize: func(s Size) { _ = pty.Setsize(f, &pty.Winsize{Cols: s.Cols, Rows: s.Rows}) },
		cancel: cancel,
	})
	go func() {
		<-ctx.Done()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()
	go func() {
		_, _ = io.Copy(out, f)
		werr := cmd.Wait()
		out.Flush()
		f.Close()
		cancel()
		if cleanup != nil {
			cleanup()
		}
		var exitErr error
		if ee, ok := werr.(*exec.ExitError); ok && ee.ExitCode() > 0 {
			exitErr = fmt.Errorf("exited with code %d", ee.ExitCode())
		}
		h.finish(id, exitErr)
	}()
	return id, nil
}

// UserShell returns the user's login shell.
func UserShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/zsh"
}

// ---- helpers ----

type sizeQueue struct {
	ctx context.Context
	ch  chan remotecommand.TerminalSize
}

func newSizeQueue(ctx context.Context) *sizeQueue {
	return &sizeQueue{ctx: ctx, ch: make(chan remotecommand.TerminalSize, 8)}
}

func (q *sizeQueue) push(s Size) {
	select {
	case q.ch <- remotecommand.TerminalSize{Width: s.Cols, Height: s.Rows}:
	default: // drop: a newer size will follow
	}
}

func (q *sizeQueue) Next() *remotecommand.TerminalSize {
	select {
	case s := <-q.ch:
		return &s
	case <-q.ctx.Done():
		return nil
	}
}

// batchWriter coalesces small writes into one flush per ~8ms to keep UI event volume low.
type batchWriter struct {
	mu    sync.Mutex
	buf   []byte
	timer *time.Timer
	send  func([]byte)
}

func newBatchWriter(send func([]byte)) *batchWriter { return &batchWriter{send: send} }

func (w *batchWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.buf = append(w.buf, p...)
	if w.timer == nil {
		w.timer = time.AfterFunc(8*time.Millisecond, w.Flush)
	}
	w.mu.Unlock()
	return len(p), nil
}

func (w *batchWriter) Flush() {
	w.mu.Lock()
	b := w.buf
	w.buf = nil
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	w.mu.Unlock()
	if len(b) > 0 {
		w.send(b)
	}
}
