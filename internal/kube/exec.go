package kube

import (
	"context"
	"fmt"
	"io"
	"net/url"

	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/streaming/pkg/httpstream"

	"soliton-rudder/internal/term"
)

// DefaultShell prefers bash and falls back to sh.
var DefaultShell = []string{"sh", "-c", "command -v bash >/dev/null 2>&1 && exec bash || exec sh"}

func podSubresourceURL(s *Session, ns, pod, sub string, params url.Values) *url.URL {
	req := s.rc.Post().AbsPath(fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/%s", ns, pod, sub))
	for k, vs := range params {
		for _, v := range vs {
			req = req.Param(k, v)
		}
	}
	return req.URL()
}

// ExecRun returns a terminal RunFunc that execs into a pod container with a TTY.
func (s *Session) ExecRun(ns, pod, container string, command []string) term.RunFunc {
	if len(command) == 0 {
		command = DefaultShell
	}
	return func(ctx context.Context, stdin io.Reader, stdout io.Writer, size remotecommand.TerminalSizeQueue) error {
		params := url.Values{
			"stdin": {"true"}, "stdout": {"true"}, "stderr": {"false"}, "tty": {"true"},
			"command": command,
		}
		if container != "" {
			params.Set("container", container)
		}
		u := podSubresourceURL(s, ns, pod, "exec", params)
		ws, err := remotecommand.NewWebSocketExecutor(s.cfg, "GET", u.String())
		if err != nil {
			return err
		}
		spdyExec, err := remotecommand.NewSPDYExecutor(s.cfg, "POST", u)
		if err != nil {
			return err
		}
		exec, err := remotecommand.NewFallbackExecutor(ws, spdyExec, func(err error) bool {
			return httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err)
		})
		if err != nil {
			return err
		}
		return exec.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdin: stdin, Stdout: stdout, Tty: true, TerminalSizeQueue: size,
		})
	}
}
