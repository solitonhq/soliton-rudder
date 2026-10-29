package kube

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	EventLog    = "log"    // LogBatch
	EventLogEnd = "logend" // LogEnd

	maxLogLine = 256 * 1024
)

// LogOptions mirrors the useful subset of `kubectl logs` flags.
type LogOptions struct {
	Namespace  string `json:"namespace"`
	Pod        string `json:"pod"`
	Container  string `json:"container"`
	Follow     bool   `json:"follow"`
	TailLines  int    `json:"tailLines"` // 0 = all
	SinceSecs  int    `json:"sinceSecs"` // 0 = no limit
	Previous   bool   `json:"previous"`
	Timestamps bool   `json:"timestamps"`
}

type LogLine struct {
	TS   string `json:"ts,omitempty"`
	Text string `json:"text"`
}

type LogBatch struct {
	Sub       string    `json:"sub"`
	Pod       string    `json:"pod"`
	Container string    `json:"container"`
	Lines     []LogLine `json:"lines"`
}

type LogEnd struct {
	Sub   string `json:"sub"`
	Error string `json:"error"`
}

// LogTarget is a pod and its containers that match a workload or service.
type LogTarget struct {
	Pod        string   `json:"pod"`
	Phase      string   `json:"phase"`
	Containers []string `json:"containers"`
	Init       []string `json:"initContainers"`
}

// StreamLogs starts a log stream and returns its subscription id.
func (s *Session) StreamLogs(opts LogOptions) (string, error) {
	if opts.Pod == "" || opts.Namespace == "" {
		return "", fmt.Errorf("pod and namespace are required")
	}
	id := fmt.Sprintf("%s-log%d", s.clusterID, subSeq.Add(1))
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.subs[id] = cancel
	s.mu.Unlock()
	go func() {
		err := s.runLogs(ctx, id, opts)
		s.mu.Lock()
		delete(s.subs, id)
		s.mu.Unlock()
		ev := LogEnd{Sub: id}
		if err != nil && ctx.Err() == nil {
			ev.Error = err.Error()
		}
		s.emit(EventLogEnd, ev)
	}()
	return id, nil
}

func (s *Session) runLogs(ctx context.Context, sub string, o LogOptions) error {
	req := s.rc.Get().AbsPath(fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/log", o.Namespace, o.Pod))
	if o.Container != "" {
		req = req.Param("container", o.Container)
	}
	if o.Follow {
		req = req.Param("follow", "true")
	}
	if o.TailLines > 0 {
		req = req.Param("tailLines", strconv.Itoa(o.TailLines))
	}
	if o.SinceSecs > 0 {
		req = req.Param("sinceSeconds", strconv.Itoa(o.SinceSecs))
	}
	if o.Previous {
		req = req.Param("previous", "true")
	}
	req = req.Param("timestamps", "true") // always request; the UI toggles display
	stream, err := req.Stream(ctx)
	if err != nil {
		return err
	}
	defer stream.Close()

	var (
		mu      sync.Mutex
		pending []LogLine
	)
	flush := func() {
		mu.Lock()
		lines := pending
		pending = nil
		mu.Unlock()
		if len(lines) > 0 {
			s.emit(EventLog, LogBatch{Sub: sub, Pod: o.Pod, Container: o.Container, Lines: lines})
		}
	}
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				flush()
			case <-done:
				return
			}
		}
	}()
	defer func() { close(done); flush() }()

	r := bufio.NewReaderSize(stream, 64*1024)
	for {
		line, err := readLine(r)
		if line != "" || err == nil {
			ll := splitTimestamp(line)
			mu.Lock()
			pending = append(pending, ll)
			big := len(pending) >= 2000
			mu.Unlock()
			if big {
				flush()
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// readLine reads one line, truncating anything beyond maxLogLine.
func readLine(r *bufio.Reader) (string, error) {
	var sb strings.Builder
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return sb.String(), err
		}
		if sb.Len() < maxLogLine {
			sb.Write(chunk)
		}
		if !isPrefix {
			return sb.String(), nil
		}
	}
}

// splitTimestamp separates the RFC3339Nano prefix the API adds with timestamps=true.
func splitTimestamp(line string) LogLine {
	if i := strings.IndexByte(line, ' '); i > 20 && i < 40 && line[4] == '-' && strings.Contains(line[:i], "T") {
		return LogLine{TS: line[:i], Text: line[i+1:]}
	}
	return LogLine{Text: line}
}

// podsFor returns the pods behind a pod, workload or service.
func (s *Session) podsFor(ctx context.Context, res Resource, ns, name string) ([]unstructured.Unstructured, error) {
	if res.Name == "pods" {
		p, err := s.client(res, ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return []unstructured.Unstructured{*p}, nil
	}
	obj, err := s.client(res, ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	var sel string
	if ml, found, _ := unstructured.NestedStringMap(obj.Object, "spec", "selector", "matchLabels"); found && len(ml) > 0 {
		sel = joinSelector(ml)
	} else if ml, found, _ := unstructured.NestedStringMap(obj.Object, "spec", "selector"); found && len(ml) > 0 {
		sel = joinSelector(ml) // services, replicationcontrollers
	}
	if sel == "" {
		return nil, fmt.Errorf("%s has no pod selector", res.Kind)
	}
	list, err := s.client(podRes, ns).List(ctx, metav1.ListOptions{LabelSelector: sel})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// LogTargets lists the pods (and containers) behind a pod, workload or service.
func (s *Session) LogTargets(ctx context.Context, key, ns, name string) ([]LogTarget, error) {
	res, err := s.resource(key)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	pods, err := s.podsFor(ctx, res, ns, name)
	if err != nil {
		return nil, err
	}
	out := make([]LogTarget, 0, len(pods))
	for i := range pods {
		out = append(out, podTarget(&pods[i]))
	}
	return out, nil
}

func joinSelector(m map[string]string) string {
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ",")
}

func podTarget(p *unstructured.Unstructured) LogTarget {
	t := LogTarget{Pod: p.GetName()}
	t.Phase, _, _ = unstructured.NestedString(p.Object, "status", "phase")
	names := func(field string) []string {
		items, _, _ := unstructured.NestedSlice(p.Object, "spec", field)
		var out []string
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				if n, ok := m["name"].(string); ok {
					out = append(out, n)
				}
			}
		}
		return out
	}
	t.Containers, t.Init = names("containers"), names("initContainers")
	return t
}
