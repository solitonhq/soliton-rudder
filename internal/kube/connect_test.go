package kube

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"k8s.io/client-go/rest"
)

func TestConnectDisconnectLifecycleAndBackgroundWork(t *testing.T) {
	f := newFakeAPI(t)
	var mu sync.Mutex
	var events [][]string
	m := NewManager(func(string) (*rest.Config, error) { return &rest.Config{Host: f.srv.URL}, nil }, func(name string, data any) {
		if name == EventConnections {
			mu.Lock()
			events = append(events, data.([]string))
			mu.Unlock()
		}
	})
	last := func() []string {
		mu.Lock()
		defer mu.Unlock()
		if len(events) == 0 {
			return nil
		}
		return events[len(events)-1]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if got := m.Connected(); len(got) != 0 {
		t.Fatalf("nothing is connected at the start: %v", got)
	}
	if _, err := m.Connect(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Connect(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.Connected(), []string{"a", "b"}) || !reflect.DeepEqual(last(), []string{"a", "b"}) {
		t.Fatalf("connected %v, last event %v", m.Connected(), last())
	}

	m.Disconnect("a")
	if !reflect.DeepEqual(m.Connected(), []string{"b"}) || !reflect.DeepEqual(last(), []string{"b"}) {
		t.Fatalf("after disconnect: %v / %v", m.Connected(), last())
	}
	if _, err := m.Session("a"); err == nil {
		t.Error("a disconnected cluster has no session")
	}

	// the blackbox recorder (and other background work) must not quietly reopen it…
	if _, err := m.EnsureBackground("a"); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("background work must respect the disconnect: %v", err)
	}
	if got := m.Connected(); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("and it must not have connected: %v", got)
	}
	// …but it connects clusters that were never touched, and keeps using connected ones
	if _, err := m.EnsureBackground("fresh"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnsureBackground("b"); err != nil {
		t.Fatal(err)
	}

	// an explicit use (a comparison, an assessment) connects again, and then background work follows
	if _, err := m.EnsureReady(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnsureBackground("a"); err != nil {
		t.Fatalf("after the user used it again: %v", err)
	}
	m.Disconnect("a")
	if _, err := m.Connect(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnsureBackground("a"); err != nil {
		t.Fatalf("Connect clears a deliberate disconnect: %v", err)
	}
	// disconnecting something that is not connected is harmless
	m.Disconnect("never")
	m.Disconnect("never")
}
