package term

import (
	"context"
	"encoding/base64"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/client-go/tools/remotecommand"
)

type rec struct {
	mu   sync.Mutex
	out  strings.Builder
	exit chan ExitEvent
}

func newRec() *rec { return &rec{exit: make(chan ExitEvent, 1)} }

func (r *rec) emit(name string, data any) {
	switch v := data.(type) {
	case OutputEvent:
		b, _ := base64.StdEncoding.DecodeString(v.Data)
		r.mu.Lock()
		r.out.Write(b)
		r.mu.Unlock()
	case ExitEvent:
		r.exit <- v
	}
}

func (r *rec) text() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.out.String()
}

func TestLocalPTYEchoAndExitCode(t *testing.T) {
	r := newRec()
	h := NewHub(r.emit)
	cleaned := make(chan struct{})
	id, err := h.StartLocal(exec.Command("sh", "-c", "echo hello-pty; read x; echo got:$x; exit 3"), Size{80, 24}, func() { close(cleaned) })
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := h.Write(id, []byte("abc\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-r.exit:
		if !strings.Contains(ev.Error, "code 3") {
			t.Fatalf("exit = %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no exit")
	}
	<-cleaned
	if out := r.text(); !strings.Contains(out, "hello-pty") || !strings.Contains(out, "got:abc") {
		t.Fatalf("output = %q", out)
	}
	if err := h.Write(id, []byte("x")); err == nil {
		t.Fatal("write to finished session must fail")
	}
}

func TestRemoteSessionRelaysAndCloses(t *testing.T) {
	r := newRec()
	h := NewHub(r.emit)
	var gotSize *remotecommand.TerminalSize
	id := h.StartRemote(Size{100, 30}, func(ctx context.Context, in io.Reader, out io.Writer, q remotecommand.TerminalSizeQueue) error {
		gotSize = q.Next()
		buf := make([]byte, 16)
		n, _ := in.Read(buf)
		out.Write([]byte("echo:" + string(buf[:n])))
		<-ctx.Done()
		return ctx.Err()
	})
	h.Write(id, []byte("ping"))
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(r.text(), "echo:ping") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(r.text(), "echo:ping") {
		t.Fatalf("output = %q", r.text())
	}
	h.Close(id)
	select {
	case ev := <-r.exit:
		if ev.Error != "" {
			t.Fatalf("user close should not be an error: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no exit")
	}
	if gotSize == nil || gotSize.Width != 100 || gotSize.Height != 30 {
		t.Fatalf("size = %+v", gotSize)
	}
}
