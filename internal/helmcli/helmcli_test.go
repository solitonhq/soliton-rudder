package helmcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeHelm(t *testing.T, body string) string {
	dir := t.TempDir()
	p := filepath.Join(dir, "helm")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRollbackArgs(t *testing.T) {
	bin := fakeHelm(t, `echo "$@"`)
	out, err := Rollback(context.Background(), bin, "/tmp/kc", "prod", "web", 3)
	if err != nil {
		t.Fatal(err)
	}
	want := "--kubeconfig /tmp/kc rollback web 3 -n prod --timeout 120s"
	if strings.TrimSpace(out) != want {
		t.Fatalf("args = %q, want %q", out, want)
	}
}

func TestUninstallArgsAndFailure(t *testing.T) {
	out, err := Uninstall(context.Background(), fakeHelm(t, `echo "$@"`), "/tmp/kc", "prod", "web")
	if err != nil || !strings.Contains(out, "uninstall web -n prod") {
		t.Fatalf("%v %q", err, out)
	}
	_, err = Uninstall(context.Background(), fakeHelm(t, `echo "Error: release not found" >&2; exit 1`), "/tmp/kc", "prod", "web")
	if err == nil || !strings.Contains(err.Error(), "release not found") {
		t.Fatalf("stderr should surface: %v", err)
	}
}

func TestRejectsFlagInjectionAndBadRevision(t *testing.T) {
	bin := fakeHelm(t, `echo ran`)
	for _, name := range []string{"--debug", "-n", "a b", "x;rm", ""} {
		if _, err := Rollback(context.Background(), bin, "/k", "ns", name, 1); err == nil {
			t.Errorf("name %q must be rejected", name)
		}
		if _, err := Uninstall(context.Background(), bin, "/k", name, "web"); err == nil {
			t.Errorf("namespace %q must be rejected", name)
		}
	}
	if _, err := Rollback(context.Background(), bin, "/k", "ns", "web", 0); err == nil {
		t.Error("revision 0 must be rejected")
	}
}
