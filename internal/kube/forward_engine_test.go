package kube

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeCluster simulates pods behind a Service: each ready pod is a TCP server that answers "<pod>:<line>".
type fakePod struct {
	name   string
	ln     net.Listener
	ready  bool
	closed bool
	conns  []net.Conn
}

type fakeCluster struct {
	mu      sync.Mutex
	pods    map[string]*fakePod
	tunnels map[string]*tunnel
	starts  []string
	failTo  map[string]bool // pods whose tunnel cannot be started
}

func newFakeCluster(t *testing.T, names ...string) *fakeCluster {
	c := &fakeCluster{pods: map[string]*fakePod{}, tunnels: map[string]*tunnel{}, failTo: map[string]bool{}}
	for _, n := range names {
		c.addPod(t, n, true)
	}
	t.Cleanup(func() {
		for _, p := range c.pods {
			p.ln.Close()
			for _, cn := range p.conns {
				cn.Close()
			}
		}
	})
	return c
}

func (c *fakeCluster) addPod(t *testing.T, name string, ready bool) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &fakePod{name: name, ln: ln, ready: ready}
	c.mu.Lock()
	c.pods[name] = p
	c.mu.Unlock()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			c.mu.Lock()
			p.conns = append(p.conns, conn)
			c.mu.Unlock()
			go func(conn net.Conn) {
				defer conn.Close()
				sc := bufio.NewScanner(conn)
				for sc.Scan() {
					fmt.Fprintf(conn, "%s:%s\n", name, sc.Text())
				}
			}(conn)
		}
	}()
}

func (c *fakeCluster) setReady(name string, ready bool) {
	c.mu.Lock()
	c.pods[name].ready = ready
	c.mu.Unlock()
}

// crash kills the pod's tunnel (the connection breaks) and the pod itself.
func (c *fakeCluster) crash(name string) {
	c.mu.Lock()
	p := c.pods[name]
	p.ready, p.closed = false, true
	tn := c.tunnels[name]
	conns := append([]net.Conn{}, p.conns...)
	c.mu.Unlock()
	p.ln.Close()
	for _, cn := range conns {
		cn.Close()
	}
	if tn != nil {
		tn.finish(fmt.Errorf("pod %s crashed", name))
	}
}

func (c *fakeCluster) deps() forwardDeps {
	return forwardDeps{
		resolve: func(ctx context.Context) (string, int, error) {
			c.mu.Lock()
			defer c.mu.Unlock()
			var ready []string
			for n, p := range c.pods {
				if p.ready {
					ready = append(ready, n)
				}
			}
			if len(ready) == 0 {
				return "", 0, fmt.Errorf("no ready pods")
			}
			sort.Strings(ready)
			return ready[len(ready)-1], 80, nil // newest = highest name in these tests
		},
		healthy: func(ctx context.Context, pod string) (bool, string) {
			c.mu.Lock()
			defer c.mu.Unlock()
			if p := c.pods[pod]; p == nil || p.closed {
				return false, "was deleted"
			} else if !p.ready {
				return false, "is terminating"
			}
			return true, ""
		},
		start: func(ctx context.Context, pod string, port int) (*tunnel, error) {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.starts = append(c.starts, pod)
			if c.failTo[pod] || c.pods[pod].closed {
				return nil, fmt.Errorf("tunnel refused")
			}
			tn := &tunnel{pod: pod, addr: c.pods[pod].ln.Addr().String(), done: make(chan struct{}), stop: func() {}}
			c.tunnels[pod] = tn
			return tn, nil
		},
	}
}

var fastTimings = forwardTimings{
	poll: 20 * time.Millisecond, queueMax: 3 * time.Second, drainMax: 2 * time.Second,
	retryMin: 10 * time.Millisecond, retryMax: 40 * time.Millisecond, bindRetry: 30 * time.Millisecond,
}

type stateLog struct {
	mu     sync.Mutex
	states []ForwardState
}

func (l *stateLog) add(s ForwardState) { l.mu.Lock(); l.states = append(l.states, s); l.mu.Unlock() }
func (l *stateLog) seen(status string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range l.states {
		if s.Status == status {
			return true
		}
	}
	return false
}

func (l *stateLog) waitFor(t *testing.T, what string, pred func(ForwardState) bool) ForwardState {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		l.mu.Lock()
		n := len(l.states)
		var s ForwardState
		if n > 0 {
			s = l.states[n-1]
		}
		l.mu.Unlock()
		if n > 0 && pred(s) {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; states: %+v", what, l.states)
	return ForwardState{}
}

func startForward(t *testing.T, c *fakeCluster, port int) (*stateLog, context.CancelFunc, chan struct{}) {
	log := &stateLog{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runForward(ctx, c.deps(), port, log.add, fastTimings)
		close(done)
	}()
	t.Cleanup(func() { cancel(); <-done })
	return log, cancel, done
}

func ask(t *testing.T, port int, msg string) string {
	t.Helper()
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(4 * time.Second))
	fmt.Fprintln(conn, msg)
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return strings.TrimSpace(line)
}

func TestForwardServesThroughNewestReadyPod(t *testing.T) {
	c := newFakeCluster(t, "web-a", "web-b")
	log, _, _ := startForward(t, c, 0)
	st := log.waitFor(t, "active", func(s ForwardState) bool { return s.Status == "active" })
	if st.Pod != "web-b" || st.LocalPort == 0 || st.Reconnects != 0 {
		t.Fatalf("%+v", st)
	}
	if got := ask(t, st.LocalPort, "hi"); got != "web-b:hi" {
		t.Fatalf("got %q", got)
	}
}

func TestPodCrashRebindsKeepingTheSameLocalPort(t *testing.T) {
	c := newFakeCluster(t, "web-a", "web-b")
	log, _, _ := startForward(t, c, 0)
	first := log.waitFor(t, "active", func(s ForwardState) bool { return s.Status == "active" })
	port := first.LocalPort

	c.crash("web-b") // the backing pod dies; web-a is still ready
	healed := log.waitFor(t, "healed onto web-a", func(s ForwardState) bool { return s.Status == "active" && s.Pod == "web-a" })
	if healed.LocalPort != port {
		t.Fatalf("the local port must not change: %d → %d", port, healed.LocalPort)
	}
	if healed.Reconnects != 1 || !strings.Contains(healed.LastHealedReason, "connection to the pod was lost") || healed.LastHealedAt == 0 {
		t.Fatalf("metrics: %+v", healed)
	}
	if !log.seen("healing") {
		t.Fatal("the healing phase should have been reported")
	}
	if got := ask(t, port, "again"); got != "web-a:again" {
		t.Fatalf("got %q", got)
	}
}

func TestConnectionsWaitForReplacementInsteadOfBeingRefused(t *testing.T) {
	c := newFakeCluster(t, "web-a")
	log, _, _ := startForward(t, c, 0)
	st := log.waitFor(t, "active", func(s ForwardState) bool { return s.Status == "active" })
	port := st.LocalPort

	c.crash("web-a") // no replacement yet
	log.waitFor(t, "degraded", func(s ForwardState) bool { return s.Status == "degraded" })

	// the listener is still open: this client connects and waits
	result := make(chan string, 1)
	go func() {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
		if err != nil {
			result <- "dial error: " + err.Error()
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(4 * time.Second))
		fmt.Fprintln(conn, "queued")
		line, _ := bufio.NewReader(conn).ReadString('\n')
		result <- strings.TrimSpace(line)
	}()
	time.Sleep(200 * time.Millisecond)
	select {
	case r := <-result:
		t.Fatalf("the connection should still be waiting, got %q", r)
	default:
	}
	c.addPod(t, "web-c", true) // a replacement comes up
	select {
	case r := <-result:
		if r != "web-c:queued" {
			t.Fatalf("got %q", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the queued connection was never served")
	}
	final := log.waitFor(t, "active on web-c", func(s ForwardState) bool { return s.Status == "active" && s.Pod == "web-c" })
	if final.Reconnects != 1 || final.LocalPort != port {
		t.Fatalf("%+v", final)
	}
}

func TestTerminatingPodTriggersProactiveFailoverAndDrains(t *testing.T) {
	c := newFakeCluster(t, "web-a", "web-b")
	log, _, _ := startForward(t, c, 0)
	st := log.waitFor(t, "active", func(s ForwardState) bool { return s.Status == "active" && s.Pod == "web-b" })

	// a long-lived connection on web-b
	long, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", st.LocalPort))
	if err != nil {
		t.Fatal(err)
	}
	defer long.Close()
	fmt.Fprintln(long, "before")
	rd := bufio.NewReader(long)
	long.SetDeadline(time.Now().Add(4 * time.Second))
	if l, _ := rd.ReadString('\n'); strings.TrimSpace(l) != "web-b:before" {
		t.Fatalf("got %q", l)
	}

	c.setReady("web-b", false) // pod starts terminating but its tunnel is still alive
	healed := log.waitFor(t, "failover to web-a", func(s ForwardState) bool { return s.Status == "active" && s.Pod == "web-a" })
	if !strings.Contains(healed.LastHealedReason, "web-b") || !strings.Contains(healed.LastHealedReason, "terminating") {
		t.Fatalf("reason: %q", healed.LastHealedReason)
	}
	// new connections go to web-a …
	if got := ask(t, st.LocalPort, "new"); got != "web-a:new" {
		t.Fatalf("got %q", got)
	}
	// … while the existing one keeps working on the draining tunnel
	fmt.Fprintln(long, "after")
	long.SetDeadline(time.Now().Add(4 * time.Second))
	if l, _ := rd.ReadString('\n'); strings.TrimSpace(l) != "web-b:after" {
		t.Fatalf("in-flight connection should survive the failover, got %q", l)
	}
}

func TestDegradedWhileTerminatingWithoutReplacementThenRecovers(t *testing.T) {
	c := newFakeCluster(t, "web-a")
	log, _, _ := startForward(t, c, 0)
	log.waitFor(t, "active", func(s ForwardState) bool { return s.Status == "active" })
	c.setReady("web-a", false)
	st := log.waitFor(t, "degraded", func(s ForwardState) bool { return s.Status == "degraded" })
	if !strings.Contains(st.Message, "waiting for a ready replica") {
		t.Fatalf("%+v", st)
	}
	c.addPod(t, "web-z", true)
	log.waitFor(t, "recovered", func(s ForwardState) bool { return s.Status == "active" && s.Pod == "web-z" })
}

func TestStartFailuresAreRetried(t *testing.T) {
	c := newFakeCluster(t, "web-a")
	c.failTo["web-a"] = true
	log, _, _ := startForward(t, c, 0)
	st := log.waitFor(t, "degraded", func(s ForwardState) bool { return s.Status == "degraded" })
	if !strings.Contains(st.Message, "tunnel refused") {
		t.Fatalf("%+v", st)
	}
	c.mu.Lock()
	c.failTo["web-a"] = false
	c.mu.Unlock()
	log.waitFor(t, "active", func(s ForwardState) bool { return s.Status == "active" })
}

func TestPortInUseIsReportedThenForwardStartsWhenFreed(t *testing.T) {
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := blocker.Addr().(*net.TCPAddr).Port
	c := newFakeCluster(t, "web-a")
	log, _, _ := startForward(t, c, port)
	st := log.waitFor(t, "error", func(s ForwardState) bool { return s.Status == "error" })
	if !strings.Contains(st.Message, "cannot listen") {
		t.Fatalf("%+v", st)
	}
	blocker.Close()
	active := log.waitFor(t, "active", func(s ForwardState) bool { return s.Status == "active" })
	if active.LocalPort != port {
		t.Fatalf("%+v", active)
	}
}

func TestCancelReleasesThePort(t *testing.T) {
	c := newFakeCluster(t, "web-a")
	log, cancel, done := startForward(t, c, 0)
	st := log.waitFor(t, "active", func(s ForwardState) bool { return s.Status == "active" })
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runForward did not return after cancel")
	}
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", st.LocalPort))
	if err != nil {
		t.Fatalf("port should be free after stop: %v", err)
	}
	l.Close()
}

func TestRepeatedHealsKeepCounting(t *testing.T) {
	c := newFakeCluster(t, "web-a", "web-b", "web-c")
	log, _, _ := startForward(t, c, 0)
	log.waitFor(t, "active c", func(s ForwardState) bool { return s.Status == "active" && s.Pod == "web-c" })
	c.crash("web-c")
	log.waitFor(t, "active b", func(s ForwardState) bool { return s.Status == "active" && s.Pod == "web-b" })
	c.crash("web-b")
	st := log.waitFor(t, "active a", func(s ForwardState) bool { return s.Status == "active" && s.Pod == "web-a" })
	if st.Reconnects != 2 {
		t.Fatalf("%+v", st)
	}
}
