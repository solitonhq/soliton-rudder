package shellenv

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseIgnoresShellNoise(t *testing.T) {
	out := "Welcome to zsh!\nsome rc output=1\n" + beginMark + "\nPATH=/a:/b\nAWS_PROFILE=prod\nMULTI=line1\ncontinued line\n1BAD=x\nEMPTY=\n" + endMark + "\ntrailing=junk\n"
	env := Parse(out)
	if env["PATH"] != "/a:/b" || env["AWS_PROFILE"] != "prod" || env["EMPTY"] != "" {
		t.Fatalf("%v", env)
	}
	if _, ok := env["trailing"]; ok {
		t.Fatal("content after the end marker must be ignored")
	}
	if _, ok := env["some rc output"]; ok {
		t.Fatal("noise before the begin marker must be ignored")
	}
	if _, ok := env["1BAD"]; ok {
		t.Fatal("invalid names are skipped")
	}
	if env["MULTI"] != "line1" {
		t.Fatalf("multi-line values keep only their first line: %q", env["MULTI"])
	}
}

func TestMergePathOrderAndDedupe(t *testing.T) {
	got := strings.Split(MergePath("/opt/aws/bin:/usr/bin", "/usr/bin:/bin"), ":")
	if got[0] != "/opt/aws/bin" || got[1] != "/usr/bin" || got[2] != "/bin" {
		t.Fatalf("%v", got)
	}
	seen := map[string]bool{}
	for _, p := range got {
		if seen[p] {
			t.Fatalf("duplicate %q in %v", p, got)
		}
		seen[p] = true
	}
	if !seen["/opt/homebrew/bin"] || !seen["/usr/local/bin"] {
		t.Fatalf("homebrew locations should always be present: %v", got)
	}
}

func TestApplyOnlyAllowlistedAndNeverOverrides(t *testing.T) {
	t.Setenv("AWS_REGION", "eu-west-1") // already set: must win
	os.Unsetenv("AWS_PROFILE")
	os.Unsetenv("RANDOM_SECRET_TOKEN")
	t.Cleanup(func() { os.Unsetenv("AWS_PROFILE"); os.Unsetenv("GOOGLE_APPLICATION_CREDENTIALS") })
	Apply(map[string]string{"PATH": "/x/bin", "AWS_REGION": "us-east-1", "AWS_PROFILE": "dev", "GOOGLE_APPLICATION_CREDENTIALS": "/k.json", "RANDOM_SECRET_TOKEN": "hunter2"})
	if os.Getenv("AWS_REGION") != "eu-west-1" || os.Getenv("AWS_PROFILE") != "dev" || os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "/k.json" {
		t.Fatal("allow-listed variables should be imported without overriding existing ones")
	}
	if os.Getenv("RANDOM_SECRET_TOKEN") != "" {
		t.Fatal("unrelated variables must not be imported")
	}
	if !strings.HasPrefix(os.Getenv("PATH"), "/x/bin") {
		t.Fatalf("PATH = %q", os.Getenv("PATH"))
	}
}

func TestLoadFromARealShell(t *testing.T) {
	dir := t.TempDir()
	shell := filepath.Join(dir, "fakeshell")
	// behaves like a login shell whose rc files print noise and export AWS_PROFILE
	os.WriteFile(shell, []byte("#!/bin/sh\necho 'rc noise'\nexport AWS_PROFILE=from-rc PATH=/custom/bin:$PATH\neval \"$2\"\n"), 0o755)
	env, err := Load(context.Background(), shell)
	if err != nil {
		t.Fatal(err)
	}
	if env["AWS_PROFILE"] != "from-rc" || !strings.HasPrefix(env["PATH"], "/custom/bin") {
		t.Fatalf("%v", env)
	}
	if _, err := Load(context.Background(), filepath.Join(dir, "missing-shell")); err == nil {
		t.Fatal("a missing shell must be an error (the caller then keeps the inherited environment)")
	}
}
