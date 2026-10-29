// Package helmcli runs the user's own `helm` binary for the few write operations that need Helm's
// engine (rollback, uninstall). Inspection never needs it.
package helmcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

const timeout = 3 * time.Minute

var nameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// Find locates helm. GUI apps inherit a minimal PATH, so common install dirs are searched too.
func Find() (string, error) {
	if p, err := exec.LookPath("helm"); err == nil {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", filepath.Join(home, "bin"), filepath.Join(home, ".local", "bin")} {
		p := filepath.Join(dir, "helm")
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", errors.New("the helm CLI was not found; install it (e.g. brew install helm) to enable this action")
}

func run(ctx context.Context, bin, kubeconfig string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	full := append([]string{"--kubeconfig", kubeconfig}, args...)
	cmd := exec.CommandContext(ctx, bin, full...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		msg := out.String()
		if msg == "" {
			msg = err.Error()
		}
		return msg, fmt.Errorf("helm %s failed: %s", args[0], trim(msg))
	}
	return out.String(), nil
}

func trim(s string) string {
	if len(s) > 600 {
		return s[:600] + "…"
	}
	return s
}

func check(ns, name string) error {
	if !nameRe.MatchString(ns) || !nameRe.MatchString(name) {
		return errors.New("invalid release or namespace name")
	}
	return nil
}

func Rollback(ctx context.Context, bin, kubeconfig, ns, name string, revision int) (string, error) {
	if err := check(ns, name); err != nil {
		return "", err
	}
	if revision < 1 {
		return "", errors.New("invalid revision")
	}
	return run(ctx, bin, kubeconfig, "rollback", name, strconv.Itoa(revision), "-n", ns, "--timeout", "120s")
}

func Uninstall(ctx context.Context, bin, kubeconfig, ns, name string) (string, error) {
	if err := check(ns, name); err != nil {
		return "", err
	}
	return run(ctx, bin, kubeconfig, "uninstall", name, "-n", ns)
}
