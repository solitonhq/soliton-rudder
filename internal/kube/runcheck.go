package kube

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	maxCheckBytes = 64 * 1024
	checkLogLines = 100
)

type CheckOutput struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// RunCheck runs one read-only runbook step (logs, previous-logs, events, health, yaml) against an object.
// Anything that could change the cluster is refused here by construction: it only knows these five.
func (s *Session) RunCheck(ctx context.Context, do, key, ns, name string) (CheckOutput, error) {
	switch do {
	case "events", "health", "yaml", "logs", "previous-logs":
	default:
		return CheckOutput{}, fmt.Errorf("%q is not a read-only check", do)
	}
	if key == "core/v1/secrets" {
		return CheckOutput{}, fmt.Errorf("Secrets are never shown by runbooks")
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	switch do {
	case "events":
		ev, err := s.Events(ctx, key, ns, name)
		if err != nil {
			return CheckOutput{}, err
		}
		return CheckOutput{Title: "Events", Text: formatEvents(ev)}, nil
	case "health":
		h, err := s.Health(ctx, key, ns, name)
		if err != nil {
			return CheckOutput{}, err
		}
		return CheckOutput{Title: "Health", Text: formatHealth(h)}, nil
	case "yaml":
		y, err := s.GetYAML(ctx, key, ns, name)
		if err != nil {
			return CheckOutput{}, err
		}
		return CheckOutput{Title: "Manifest", Text: capText(y)}, nil
	case "logs", "previous-logs":
		if key != "core/v1/pods" {
			return CheckOutput{}, fmt.Errorf("logs can only be read from a pod")
		}
		return s.checkLogs(ctx, ns, name, do == "previous-logs")
	}
	return CheckOutput{}, fmt.Errorf("%q is not a read-only check", do)
}

func (s *Session) checkLogs(ctx context.Context, ns, name string, previous bool) (CheckOutput, error) {
	p, err := s.client(podRes, ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return CheckOutput{}, err
	}
	c := pickContainer(p, previous)
	title := "Logs"
	if previous {
		title = "Previous logs"
	}
	if c != "" {
		title += " · " + c
	}
	req := s.rc.Get().AbsPath(fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/log", ns, name)).
		Param("tailLines", strconv.Itoa(checkLogLines)).Param("timestamps", "true")
	if c != "" {
		req = req.Param("container", c)
	}
	if previous {
		req = req.Param("previous", "true")
	}
	raw, err := req.DoRaw(ctx)
	if err != nil {
		// no previous container, or a container that has not started: normal, so explain instead of failing
		return CheckOutput{Title: title, Text: "No logs available: " + strings.TrimSpace(err.Error())}, nil
	}
	if len(raw) == 0 {
		return CheckOutput{Title: title, Text: "(the container wrote no log lines)"}, nil
	}
	return CheckOutput{Title: title, Text: tailText(string(raw))}, nil
}

// pickContainer chooses the container most likely to hold the answer: for previous logs the one that restarted,
// otherwise the first one that is not ready.
func pickContainer(p *unstructured.Unstructured, previous bool) string {
	css, _, _ := unstructured.NestedSlice(p.Object, "status", "containerStatuses")
	first := ""
	for _, c := range css {
		cm, _ := c.(map[string]any)
		n := str(cm["name"])
		if first == "" {
			first = n
		}
		restarts, _ := cm["restartCount"].(int64)
		ready, _ := cm["ready"].(bool)
		if (previous && restarts > 0) || (!previous && !ready) {
			return n
		}
	}
	if first != "" {
		return first
	}
	cs, _, _ := unstructured.NestedSlice(p.Object, "spec", "containers")
	if len(cs) > 0 {
		if m, ok := cs[0].(map[string]any); ok {
			return str(m["name"])
		}
	}
	return ""
}

func capText(s string) string {
	if len(s) > maxCheckBytes {
		return s[:maxCheckBytes] + "\n… (truncated)"
	}
	return s
}

// tailText keeps the end of long output: for logs the most recent lines matter.
func tailText(s string) string {
	if len(s) <= maxCheckBytes {
		return s
	}
	t := s[len(s)-maxCheckBytes:]
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = t[i+1:]
	}
	return "… (earlier lines omitted)\n" + t
}

func formatEvents(ev []Event) string {
	if len(ev) == 0 {
		return "No events (the cluster keeps them for about an hour)."
	}
	var b strings.Builder
	for _, e := range ev {
		cnt := ""
		if e.Count > 1 {
			cnt = fmt.Sprintf(" ×%d", e.Count)
		}
		fmt.Fprintf(&b, "%s  %-7s %s%s: %s\n", e.Last, e.Type, e.Reason, cnt, e.Message)
	}
	return capText(strings.TrimRight(b.String(), "\n"))
}

func formatHealth(h HealthReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s\n", strings.ToUpper(h.Status), h.Summary)
	for _, c := range h.Checks {
		fmt.Fprintf(&b, "  [%s] %s: %s\n", c.Status, c.Name, c.Detail)
	}
	if len(h.Hints) > 0 {
		b.WriteString("Hints:\n")
		for _, x := range h.Hints {
			fmt.Fprintf(&b, "  - %s\n", x)
		}
	}
	return capText(strings.TrimRight(b.String(), "\n"))
}
