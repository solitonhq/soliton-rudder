// Package shellenv imports the user's login-shell environment into this process.
//
// Apps launched from Finder inherit launchd's bare environment (PATH=/usr/bin:/bin:/usr/sbin:/sbin), so
// kubeconfig "exec" credential plugins (aws, gke-gcloud-auth-plugin, kubelogin, …) and variables such as
// AWS_PROFILE would be missing — a cluster that works in Terminal would fail here. Like VS Code, we ask the
// login shell for its environment once at startup.
package shellenv

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	beginMark = "__SOLITON_ENV_BEGIN__"
	endMark   = "__SOLITON_ENV_END__"
	timeout   = 6 * time.Second
)

// importPrefixes are the variables worth importing besides PATH: cloud/auth/proxy configuration that
// credential plugins and API clients read. Everything else (secrets in unrelated tools) stays out.
var importPrefixes = []string{
	"AWS_", "AZURE_", "ARM_", "GOOGLE_", "GCLOUD_", "CLOUDSDK_", "GKE_", "KUBE", "OCI_", "ALIBABA", "DIGITALOCEAN_",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "SSL_CERT_", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS",
	"XDG_", "LANG", "LC_",
}

var (
	once sync.Once
	done = make(chan struct{})
)

// Start loads the login-shell environment in the background (idempotent). Use Wait before spawning
// credential plugins or child processes.
func Start() {
	once.Do(func() {
		go func() {
			defer close(done)
			if os.Getenv("SOLITON_NO_SHELL_ENV") == "1" {
				return
			}
			env, err := Load(context.Background(), os.Getenv("SHELL"))
			if err != nil {
				return // keep the inherited environment; the app still works for static-token kubeconfigs
			}
			Apply(env)
		}()
	})
}

// Wait blocks until Start has finished (or the timeout passed).
func Wait() {
	Start()
	select {
	case <-done:
	case <-time.After(timeout + time.Second):
	}
}

// Load runs `<shell> -ilc env` and returns the variables it printed.
func Load(ctx context.Context, shell string) (map[string]string, error) {
	if shell == "" {
		shell = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	script := `printf '%s\n' ` + beginMark + `; env; printf '%s\n' ` + endMark
	cmd := exec.CommandContext(ctx, shell, "-ilc", script)
	cmd.Stdin = nil
	var out bytes.Buffer
	cmd.Stdout = &out // stderr (rc-file chatter, "no tty" warnings) is discarded
	if err := cmd.Run(); err != nil && out.Len() == 0 {
		return nil, err
	}
	return Parse(out.String()), nil
}

// Parse extracts KEY=VALUE lines between the markers, ignoring any noise printed by shell startup files.
func Parse(s string) map[string]string {
	if i := strings.Index(s, beginMark); i >= 0 {
		s = s[i+len(beginMark):]
	}
	if i := strings.Index(s, endMark); i >= 0 {
		s = s[:i]
	}
	env := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok || !validName(k) {
			continue // continuation of a multi-line value or garbage
		}
		env[k] = v
	}
	return env
}

func validName(k string) bool {
	if k == "" {
		return false
	}
	for i, r := range k {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// MergePath puts the login shell's PATH entries first and keeps any extra entries already present,
// plus the usual Homebrew locations, without duplicates.
func MergePath(login, current string) string {
	home, _ := os.UserHomeDir()
	extra := []string{"/opt/homebrew/bin", "/opt/homebrew/sbin", "/usr/local/bin", filepath.Join(home, ".local", "bin"), filepath.Join(home, "bin")}
	seen := map[string]bool{}
	var out []string
	add := func(list []string) {
		for _, p := range list {
			if p != "" && !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	add(filepath.SplitList(login))
	add(filepath.SplitList(current))
	add(extra)
	return strings.Join(out, string(os.PathListSeparator))
}

// Apply merges PATH and sets allow-listed variables that are not already set.
func Apply(env map[string]string) {
	if p, ok := env["PATH"]; ok {
		_ = os.Setenv("PATH", MergePath(p, os.Getenv("PATH")))
	} else {
		_ = os.Setenv("PATH", MergePath("", os.Getenv("PATH")))
	}
	for k, v := range env {
		if k == "PATH" || os.Getenv(k) != "" {
			continue
		}
		for _, pre := range importPrefixes {
			if strings.HasPrefix(k, pre) {
				_ = os.Setenv(k, v)
				break
			}
		}
	}
}
