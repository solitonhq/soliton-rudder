package cluster

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"soliton-rudder/internal/secrets"
)

const kubeconfigFmt = `apiVersion: v1
kind: Config
current-context: dev
clusters:
- name: c1
  cluster: {server: %s}
- name: c2
  cluster: {server: https://prod.example:6443}
users:
- name: u1
  user: {token: abc}
contexts:
- name: dev
  context: {cluster: c1, user: u1, namespace: default}
- name: prod
  context: {cluster: c2, user: u1}
`

func kc(server string) string { return fmt.Sprintf(kubeconfigFmt, server) }

func newSvc(t *testing.T) (*Service, *secrets.Memory) {
	t.Helper()
	st, err := OpenStore(filepath.Join(t.TempDir(), "d.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	sec := secrets.NewMemory()
	return NewService(st, sec), sec
}

func TestImportFileAndDedupe(t *testing.T) {
	svc, _ := newSvc(t)
	p := filepath.Join(t.TempDir(), "config")
	os.WriteFile(p, []byte(kc("https://dev.example")), 0o600)

	infos, err := svc.PreviewFile(p)
	if err != nil || len(infos) != 2 {
		t.Fatalf("preview: %v %v", infos, err)
	}
	added, err := svc.ImportFile(p, []string{"dev", "prod"})
	if err != nil || len(added) != 2 {
		t.Fatalf("import: %v %v", added, err)
	}
	again, _ := svc.ImportFile(p, []string{"dev"})
	if len(again) != 0 {
		t.Fatal("duplicate import should be skipped")
	}
	infos, _ = svc.PreviewFile(p)
	if !infos[0].Exists {
		t.Fatal("preview should flag existing")
	}
}

func TestInlineImportStoresSecretAndDeleteRemovesIt(t *testing.T) {
	svc, sec := newSvc(t)
	added, err := svc.ImportText(kc("https://dev.example"), []string{"dev"})
	if err != nil || len(added) != 1 {
		t.Fatalf("%v %v", added, err)
	}
	id := added[0].ID
	raw, err := sec.Get(secretKey(id))
	if err != nil || raw == "" {
		t.Fatal("secret not stored")
	}
	if _, err := svc.RestConfig(id); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(id); err != nil {
		t.Fatal(err)
	}
	if _, err := sec.Get(secretKey(id)); err == nil {
		t.Fatal("secret should be deleted")
	}
}

func TestRenameValidateAndReorder(t *testing.T) {
	svc, _ := newSvc(t)
	added, _ := svc.ImportText(kc("https://x"), []string{"dev", "prod"})
	empty := "  "
	if _, err := svc.Update(added[0].ID, Patch{Name: &empty}); err == nil {
		t.Fatal("empty name must fail")
	}
	n := "Staging"
	c, err := svc.Update(added[0].ID, Patch{Name: &n})
	if err != nil || c.Name != "Staging" {
		t.Fatal(err)
	}
	if err := svc.Reorder([]string{added[1].ID, added[0].ID}); err != nil {
		t.Fatal(err)
	}
	l, _ := svc.List()
	if l[0].ID != added[1].ID {
		t.Fatal("reorder not applied")
	}
	if err := svc.Delete("nope"); err != ErrNotFound {
		t.Fatalf("got %v", err)
	}
}

func TestPinnedSortsFirst(t *testing.T) {
	svc, _ := newSvc(t)
	added, _ := svc.ImportText(kc("https://x"), []string{"dev", "prod"})
	yes := true
	svc.Update(added[1].ID, Patch{Pinned: &yes})
	l, _ := svc.List()
	if l[0].ID != added[1].ID {
		t.Fatal("pinned should sort first")
	}
}

func TestProbeConnectedAndUnauthorized(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"major":"1","minor":"30","gitVersion":"v1.30.1"}`))
	}))
	defer ok.Close()
	svc, _ := newSvc(t)
	added, _ := svc.ImportText(kc(ok.URL), []string{"dev"})
	h := svc.Check(context.Background(), added[0].ID)
	if h.Status != "connected" || h.Version != "v1.30.1" {
		t.Fatalf("%+v", h)
	}

	deny := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer deny.Close()
	added, _ = svc.ImportText(kc(deny.URL), []string{"dev"})
	h = svc.Check(context.Background(), added[0].ID)
	if h.Status != "unauthorized" {
		t.Fatalf("%+v", h)
	}
}

func TestProbeUnreachable(t *testing.T) {
	svc, _ := newSvc(t)
	added, _ := svc.ImportText(kc("http://127.0.0.1:1"), []string{"dev"})
	if h := svc.Check(context.Background(), added[0].ID); h.Status != "unreachable" {
		t.Fatalf("%+v", h)
	}
}
