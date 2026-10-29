package kube

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// The forward engine keeps a local listener open for the whole life of a forward and proxies each connection
// to whichever backend tunnel is currently healthy. When the backing pod dies, is evicted or starts terminating,
// the engine resolves a new ready pod and swaps the backend in; new connections are never refused, they wait for
// the replacement. Dependencies are injected so the logic is testable without a cluster.

type ForwardState struct {
	Status           string `json:"status"` // connecting | active | healing | degraded | error
	LocalPort        int    `json:"localPort"`
	Pod              string `json:"pod"`
	Message          string `json:"message"`
	Reconnects       int    `json:"reconnects"`       // times the backend was replaced after the first connection
	LastHealedReason string `json:"lastHealedReason"` // why the most recent replacement happened
	LastHealedAt     int64  `json:"lastHealedAt"`     // unix seconds
}

// tunnel is one established path to a pod, reachable on a loopback address.
type tunnel struct {
	pod   string
	addr  string        // 127.0.0.1:<internal port> of the pod tunnel
	done  chan struct{} // closed when the tunnel has ended
	stop  func()
	conns atomic.Int32 // connections currently proxied through this tunnel

	mu  sync.Mutex
	err error
}

func (t *tunnel) finish(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	select {
	case <-t.done:
	default:
		t.err = err
		close(t.done)
	}
}

func (t *tunnel) dead() bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}

func (t *tunnel) cause() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.err != nil {
		return "connection to the pod was lost: " + t.err.Error()
	}
	return "connection to the pod was lost"
}

type forwardDeps struct {
	// resolve picks a ready pod (and the container port to reach) for the target.
	resolve func(ctx context.Context) (pod string, port int, err error)
	// healthy reports whether a pod can still serve; reason explains a negative answer.
	healthy func(ctx context.Context, pod string) (ok bool, reason string)
	// start establishes a tunnel to the pod.
	start func(ctx context.Context, pod string, port int) (*tunnel, error)
}

type forwardTimings struct {
	poll      time.Duration // how often the current pod's health is checked
	queueMax  time.Duration // how long a new connection waits for a backend before being closed
	drainMax  time.Duration // how long a replaced tunnel may keep serving its existing connections
	retryMin  time.Duration
	retryMax  time.Duration
	bindRetry time.Duration
}

var defaultForwardTimings = forwardTimings{
	poll: 2 * time.Second, queueMax: 30 * time.Second, drainMax: 30 * time.Second,
	retryMin: 500 * time.Millisecond, retryMax: 8 * time.Second, bindRetry: 3 * time.Second,
}

type forwarder struct {
	ln  net.Listener
	t   forwardTimings
	mu  sync.RWMutex
	cur *tunnel
}

func (f *forwarder) current() *tunnel {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.cur
}

func (f *forwarder) swap(n *tunnel) *tunnel {
	f.mu.Lock()
	defer f.mu.Unlock()
	old := f.cur
	f.cur = n
	return old
}

// clearIf drops the current tunnel only if it is still b (it may already have been replaced).
func (f *forwarder) clearIf(b *tunnel) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cur == b {
		f.cur = nil
	}
}

// waitBackend blocks until a live tunnel exists, up to the queue limit.
func (f *forwarder) waitBackend(ctx context.Context) *tunnel {
	deadline := time.NewTimer(f.t.queueMax)
	defer deadline.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		if b := f.current(); b != nil && !b.dead() {
			return b
		}
		select {
		case <-ctx.Done():
			return nil
		case <-deadline.C:
			return nil
		case <-tick.C:
		}
	}
}

func (f *forwarder) acceptLoop(ctx context.Context) {
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return // listener closed
		}
		go f.handle(ctx, c)
	}
}

func (f *forwarder) handle(ctx context.Context, client net.Conn) {
	defer client.Close()
	// a tunnel can die between waitBackend and dial; retry once against the next one
	for attempt := 0; attempt < 2; attempt++ {
		b := f.waitBackend(ctx)
		if b == nil {
			return
		}
		b.conns.Add(1)
		backend, err := net.DialTimeout("tcp", b.addr, 3*time.Second)
		if err != nil {
			b.conns.Add(-1)
			continue
		}
		proxy(client, backend)
		b.conns.Add(-1)
		return
	}
}

func proxy(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if tc, ok := dst.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	<-done
	b.Close()
}

// runForward serves localPort until ctx is cancelled, healing the backend as needed.
func runForward(ctx context.Context, d forwardDeps, localPort int, report func(ForwardState), t forwardTimings) {
	st := ForwardState{Status: "connecting", LocalPort: localPort}
	emit := func() { report(st) }
	emit()

	var ln net.Listener
	for ctx.Err() == nil {
		l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(localPort))
		if err == nil {
			ln = l
			break
		}
		st.Status, st.Message = "error", fmt.Sprintf("cannot listen on 127.0.0.1:%d: %v", localPort, err)
		emit()
		if !sleepCtx(ctx, t.bindRetry) {
			return
		}
	}
	if ln == nil {
		return
	}
	st.LocalPort = ln.Addr().(*net.TCPAddr).Port
	st.Status, st.Message = "connecting", ""
	emit()

	f := &forwarder{ln: ln, t: t}
	fctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		ln.Close()
		if b := f.swap(nil); b != nil {
			b.stop()
		}
	}()
	go f.acceptLoop(fctx)

	backoff := t.retryMin
	first := true
	var lastDegraded string
	setDegraded := func(msg string) {
		if msg == lastDegraded && st.Status == "degraded" {
			return
		}
		lastDegraded = msg
		st.Status, st.Message = "degraded", msg
		emit()
	}

	for ctx.Err() == nil {
		pod, port, err := d.resolve(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			setDegraded(err.Error())
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, t.retryMax)
			continue
		}
		b, err := d.start(ctx, pod, port)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			setDegraded(fmt.Sprintf("cannot reach pod %s: %v", pod, err))
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, t.retryMax)
			continue
		}
		backoff = t.retryMin
		if old := f.swap(b); old != nil {
			go drain(old, t.drainMax)
		}
		lastDegraded = ""
		st.Status, st.Pod, st.Message = "active", pod, ""
		if !first {
			st.Reconnects++
			st.LastHealedAt = time.Now().Unix()
		}
		first = false
		emit()

		reason := monitor(ctx, d, t, b, func(msg string) { setDegraded(msg) })
		if ctx.Err() != nil {
			return
		}
		st.LastHealedReason = reason
		if b.dead() {
			f.clearIf(b)
		}
		st.Status, st.Message = "healing", reason
		emit()
	}
}

// monitor returns the reason the tunnel should be replaced: it died, or its pod is no longer healthy and a
// ready replacement exists.
func monitor(ctx context.Context, d forwardDeps, t forwardTimings, b *tunnel, degraded func(string)) string {
	tick := time.NewTicker(t.poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ""
		case <-b.done:
			return b.cause()
		case <-tick.C:
			ok, why := d.healthy(ctx, b.pod)
			if ok || ctx.Err() != nil {
				continue
			}
			if alt, _, err := d.resolve(ctx); err == nil && alt != b.pod {
				return fmt.Sprintf("pod %s: %s", b.pod, why)
			}
			degraded(fmt.Sprintf("pod %s: %s; waiting for a ready replica", b.pod, why))
		}
	}
}

// drain lets a replaced tunnel finish its in-flight connections before closing it.
func drain(b *tunnel, max time.Duration) {
	deadline := time.Now().Add(max)
	for b.conns.Load() > 0 && time.Now().Before(deadline) && !b.dead() {
		time.Sleep(50 * time.Millisecond)
	}
	b.stop()
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
