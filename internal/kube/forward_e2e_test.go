package kube

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/streaming/pkg/httpstream"
	streamspdy "k8s.io/streaming/pkg/httpstream/spdy"
)

// fakeKubelet speaks the real SPDY port-forward protocol (like the API server does after proxying to the kubelet)
// and connects the data streams to a TCP server standing in for the container.
func fakeKubelet(t *testing.T, container string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/portforward") {
			http.NotFound(w, r)
			return
		}
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "websocket tunnelling not supported by this fake", http.StatusForbidden) // client falls back to SPDY
			return
		}
		if _, err := httpstream.Handshake(r, w, []string{"portforward.k8s.io"}); err != nil {
			return
		}
		var mu sync.Mutex
		errStreams := map[string]httpstream.Stream{}
		upgrader := streamspdy.NewResponseUpgrader()
		conn := upgrader.UpgradeResponse(w, r, func(stream httpstream.Stream, replySent <-chan struct{}) error {
			id := stream.Headers().Get("requestid")
			switch stream.Headers().Get("streamtype") {
			case "error":
				mu.Lock()
				errStreams[id] = stream
				mu.Unlock()
				return nil
			case "data":
				go func() {
					<-replySent // the reply is sent only after this handler returns, so never wait for it here
					defer stream.Close()
					// like the kubelet: the error stream is always closed at the end, with a message when the dial failed
					defer func() {
						mu.Lock()
						es := errStreams[id]
						mu.Unlock()
						if es != nil {
							es.Close()
						}
					}()
					c, err := net.Dial("tcp", container)
					if err != nil {
						mu.Lock()
						es := errStreams[id]
						mu.Unlock()
						if es != nil {
							fmt.Fprintf(es, "an error occurred forwarding to %s: %v", container, err)
						}
						return
					}
					defer c.Close()
					done := make(chan struct{}, 2)
					go func() { _, _ = io.Copy(c, stream); done <- struct{}{} }()
					go func() { _, _ = io.Copy(stream, c); done <- struct{}{} }()
					<-done
				}()
			}
			return nil
		})
		if conn == nil {
			return
		}
		defer conn.Close()
		<-conn.CloseChan()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func containerHTTP(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fail":
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, "not ready")
			return
		case "/hdr":
			fmt.Fprintf(w, "token=%s host=%s ua=%s", r.Header.Get("X-Token"), r.Host, r.Header.Get("User-Agent"))
			return
		}
		fmt.Fprintf(w, "hello from the pod: %s", r.URL.Path)
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return ln.Addr().String()
}

func TestStartTunnelAgainstARealPortForwardProtocol(t *testing.T) {
	api := fakeKubelet(t, containerHTTP(t))
	s, err := NewSession("c1", &rest.Config{Host: api.URL}, func(string, any) {})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tun, err := s.startTunnel(ctx, "shop", "web-1", 8080)
	if err != nil {
		t.Fatalf("the tunnel could not be established: %v", err)
	}
	defer tun.stop()
	resp, err := http.Get("http://" + tun.addr + "/ping")
	if err != nil {
		t.Fatalf("request through the tunnel failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "hello from the pod: /ping" {
		t.Fatalf("got %q", body)
	}
}

func TestRunForwardServesBrowserLikeTrafficOnTheLocalPort(t *testing.T) {
	api := fakeKubelet(t, containerHTTP(t))
	s, err := NewSession("c1", &rest.Config{Host: api.URL}, func(string, any) {})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var st ForwardState
	deps := forwardDeps{
		resolve: func(context.Context) (string, int, error) { return "web-1", 8080, nil },
		healthy: func(context.Context, string) (bool, string) { return true, "" },
		start: func(ctx context.Context, pod string, port int) (*tunnel, error) {
			return s.startTunnel(ctx, "shop", pod, port)
		},
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		runForward(ctx, deps, 0, func(x ForwardState) { mu.Lock(); st = x; mu.Unlock() }, defaultForwardTimings)
	}()
	get := func() ForwardState { mu.Lock(); defer mu.Unlock(); return st }
	deadline := time.Now().Add(8 * time.Second)
	for get().Status != "active" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	cur := get()
	if cur.Status != "active" || cur.LocalPort == 0 {
		t.Fatalf("the forward never became active: %+v", cur)
	}

	// like a browser: several requests, some on a kept-alive connection, some on fresh ones, one concurrent burst
	client := &http.Client{Timeout: 5 * time.Second}
	for i := 0; i < 5; i++ {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/page%d", cur.LocalPort, i))
		if err != nil {
			t.Fatalf("request %d through the forward failed: %v", i, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != fmt.Sprintf("hello from the pod: /page%d", i) {
			t.Fatalf("request %d: %q", i, body)
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
			resp, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/burst%d", cur.LocalPort, i))
			if err != nil {
				errs <- err
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent request failed: %v", err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("the forward did not stop")
	}
}
