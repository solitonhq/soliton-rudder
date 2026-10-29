package portfwd

import (
	"path/filepath"
	"testing"

	"soliton-rudder/internal/kube"
)

func newMgr(t *testing.T) *Manager {
	m, err := NewManager(filepath.Join(t.TempDir(), "d.db"), func(string) (*kube.Session, error) { return nil, nil }, func(string, any) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	return m
}

func TestAddListRemoveAndValidation(t *testing.T) {
	m := newMgr(t)
	good := Forward{ClusterID: "c1", Namespace: "default", Kind: "service", Name: "web", RemotePort: 80, LocalPort: 8080, Auto: true}
	rt, err := m.Add(good, false)
	if err != nil || rt.Status != "stopped" {
		t.Fatalf("%v %+v", err, rt)
	}
	list, _ := m.List("c1")
	if len(list) != 1 || list[0].Name != "web" || !list[0].Auto {
		t.Fatalf("%+v", list)
	}
	if other, _ := m.List("c2"); len(other) != 0 {
		t.Fatal("forwards leaked across clusters")
	}
	for _, bad := range []Forward{
		{ClusterID: "c1", Namespace: "d", Kind: "node", Name: "x", RemotePort: 80},
		{ClusterID: "c1", Namespace: "d", Kind: "pod", Name: "x", RemotePort: 0},
		{ClusterID: "c1", Namespace: "d", Kind: "pod", Name: "x", RemotePort: 80, LocalPort: 80},
		{ClusterID: "c1", Kind: "pod", Name: "x", RemotePort: 80},
	} {
		if _, err := m.Add(bad, false); err == nil {
			t.Fatalf("expected validation error for %+v", bad)
		}
	}
	if err := m.SetAuto(rt.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove(rt.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := m.List("c1"); len(list) != 0 {
		t.Fatal("not removed")
	}
	if err := m.Start("missing"); err == nil {
		t.Fatal("start of missing forward must fail")
	}
}

func TestEnsureReusesAnExistingForwardAndAcceptsWorkloads(t *testing.T) {
	m := newMgr(t)
	f := Forward{ClusterID: "c1", Namespace: "default", Kind: "deployment", Name: "web", RemotePort: 8080}
	a, err := m.Add(f, false)
	if err != nil {
		t.Fatalf("workload kinds are valid forward targets: %v", err)
	}
	// Ensure must find the saved forward instead of adding a duplicate (starting fails here: no session in this test)
	_, _ = m.Ensure(f)
	list, _ := m.List("c1")
	if len(list) != 1 || list[0].ID != a.ID {
		t.Fatalf("Ensure must not create duplicates: %+v", list)
	}
	f2 := f
	f2.RemotePort = 9090 // another port of the same workload is a different forward
	_, _ = m.Ensure(f2)
	if list, _ := m.List("c1"); len(list) != 2 {
		t.Fatalf("%+v", list)
	}
	if _, err := m.Ensure(Forward{ClusterID: "c1", Namespace: "d", Kind: "cronjob", Name: "x", RemotePort: 80}); err == nil {
		t.Fatal("unsupported kind")
	}
}
