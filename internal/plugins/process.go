package plugins

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	StateStopped  = "stopped"
	StateStarting = "starting"
	StateRunning  = "running"
	StateCrashed  = "crashed"
	StateNoBack   = "ui-only"

	handshakeTimeout = 15 * time.Second
	callTimeout      = 60 * time.Second
	maxLogLines      = 500
	maxCrashes       = 3
	crashWindow      = time.Minute
	maxInflight      = 8
)

// proc supervises one plugin backend process: start, handshake, restart with backoff, stop.
type proc struct {
	mgr  *Manager
	id   string
	dir  string
	man  *Manifest
	sem  chan struct{}
	done chan struct{}

	mu      sync.Mutex
	state   string
	lastErr string
	peer    *Peer
	logs    []string
	cancel  context.CancelFunc
}

func newProc(m *Manager, id, dir string, man *Manifest) *proc {
	return &proc{mgr: m, id: id, dir: dir, man: man, state: StateStopped, sem: make(chan struct{}, maxInflight), done: make(chan struct{})}
}

func (p *proc) snapshot() (state, lastErr string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state, p.lastErr
}

func (p *proc) setState(s, err string) {
	p.mu.Lock()
	p.state, p.lastErr = s, err
	p.mu.Unlock()
	p.mgr.changed()
}

func (p *proc) logf(format string, a ...any) { p.appendLog(fmt.Sprintf(format, a...)) }

func (p *proc) appendLog(line string) {
	line = strings.TrimRight(line, "\r\n")
	if len(line) > 4000 {
		line = line[:4000] + "…"
	}
	p.mu.Lock()
	p.logs = append(p.logs, time.Now().Format("15:04:05")+" "+line)
	if len(p.logs) > maxLogLines {
		p.logs = p.logs[len(p.logs)-maxLogLines:]
	}
	p.mu.Unlock()
}

func (p *proc) Logs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string{}, p.logs...)
}

// start launches the supervisor loop.
func (p *proc) start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	p.mu.Lock()
	p.cancel = cancel
	p.mu.Unlock()
	go func() {
		defer close(p.done)
		p.supervise(ctx)
	}()
}

// stop ends the process (gracefully first) and waits for the supervisor to finish.
func (p *proc) stop() {
	p.mu.Lock()
	cancel := p.cancel
	p.mu.Unlock()
	if cancel != nil {
		cancel()
		select {
		case <-p.done:
		case <-time.After(5 * time.Second):
		}
	}
	p.setState(StateStopped, "")
}

func (p *proc) supervise(ctx context.Context) {
	backoff := time.Second
	var crashes []time.Time
	for ctx.Err() == nil {
		started := time.Now()
		err := p.runOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		msg := "exited unexpectedly"
		if err != nil {
			msg = err.Error()
		}
		p.logf("backend stopped: %s", msg)
		now := time.Now()
		crashes = append(crashes, now)
		recent := crashes[:0]
		for _, c := range crashes {
			if now.Sub(c) < crashWindow {
				recent = append(recent, c)
			}
		}
		crashes = recent
		if len(crashes) >= maxCrashes {
			p.setState(StateCrashed, fmt.Sprintf("crashed %d times in a minute: %s", len(crashes), msg))
			return
		}
		p.setState(StateStarting, msg)
		if time.Since(started) > 30*time.Second {
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 10*time.Second)
	}
}

func extendedPath() string {
	home, _ := os.UserHomeDir()
	extra := []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", filepath.Join(home, ".local", "bin")}
	cur := os.Getenv("PATH")
	if cur != "" {
		extra = append([]string{cur}, extra...)
	}
	return strings.Join(extra, string(os.PathListSeparator))
}

func resolveCommand(dir string, b *Backend) (string, error) {
	if strings.HasPrefix(b.Command, "./") {
		p := filepath.Join(dir, b.Command)
		st, err := os.Stat(p)
		if err != nil {
			return "", fmt.Errorf("backend %q not found", b.Command)
		}
		if st.Mode()&0o111 == 0 {
			return "", fmt.Errorf("backend %q is not executable", b.Command)
		}
		return p, nil
	}
	// bare program name: look it up on an extended PATH (GUI apps inherit a minimal one)
	for _, d := range filepath.SplitList(extendedPath()) {
		c := filepath.Join(d, b.Command)
		if st, err := os.Stat(c); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return c, nil
		}
	}
	return "", fmt.Errorf("program %q not found on PATH (install it to run this plugin)", b.Command)
}

func (p *proc) runOnce(ctx context.Context) error {
	p.setState(StateStarting, "")
	bin, err := resolveCommand(p.dir, p.man.Backend)
	if err != nil {
		p.setState(StateCrashed, err.Error())
		return errors.New(err.Error())
	}
	dataDir := filepath.Join(p.mgr.dataRoot, p.id)
	_ = os.MkdirAll(dataDir, 0o700)
	cmd := exec.Command(bin, p.man.Backend.Args...)
	cmd.Dir = p.dir
	home, _ := os.UserHomeDir()
	cmd.Env = []string{ // deliberately minimal: no kubeconfig, tokens or cloud credentials from our environment
		"PATH=" + extendedPath(), "HOME=" + home, "LANG=en_US.UTF-8", "TMPDIR=" + os.TempDir(),
		"SOLITON_PLUGIN_ID=" + p.id, "SOLITON_PLUGIN_DIR=" + p.dir, "SOLITON_DATA_DIR=" + dataDir,
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		p.setState(StateCrashed, err.Error())
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	peer := NewPeer(stdin, func(c context.Context, method string, params json.RawMessage) (any, error) {
		return p.handle(c, method, params)
	})
	go peer.Serve(runCtx, stdout)
	go func() {
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			p.appendLog(sc.Text())
		}
	}()
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	hctx, hcancel := context.WithTimeout(runCtx, handshakeTimeout)
	err = peer.Call(hctx, "initialize", map[string]any{
		"apiVersion": APIVersion, "host": "soliton-rudder", "hostVersion": p.mgr.version,
		"pluginId": p.id, "dataDir": dataDir,
	}, nil)
	hcancel()
	if err != nil {
		_ = cmd.Process.Kill()
		<-waitCh
		return fmt.Errorf("handshake failed: %w", err)
	}
	p.mu.Lock()
	p.peer = peer
	p.mu.Unlock()
	p.setState(StateRunning, "")

	var waitErr error // set only when the plugin exits on its own; a stop we asked for is not an error
	select {
	case <-ctx.Done():
		_ = peer.Notify("shutdown", nil)
		select {
		case <-waitCh:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			<-waitCh
		}
	case waitErr = <-waitCh:
	}
	_ = stdin.Close()
	p.mu.Lock()
	p.peer = nil
	p.mu.Unlock()
	if waitErr != nil {
		return waitErr
	}
	if ctx.Err() == nil {
		return io.ErrUnexpectedEOF
	}
	return nil
}

// handle serves requests coming from the plugin backend.
func (p *proc) handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if method == "log" {
		var l struct{ Level, Message string }
		if json.Unmarshal(params, &l) == nil {
			p.appendLog(strings.TrimSpace(l.Level + " " + l.Message))
		}
		return nil, nil
	}
	select {
	case p.sem <- struct{}{}:
		defer func() { <-p.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return p.mgr.broker.Call(ctx, p.id, p.mgr.grantsFor(p.id), method, params)
}

// Call sends a request to the running backend.
func (p *proc) Call(ctx context.Context, method string, params, out any) error {
	p.mu.Lock()
	peer := p.peer
	p.mu.Unlock()
	if peer == nil {
		return errors.New("plugin backend is not running")
	}
	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return peer.Call(cctx, method, params, out)
}
