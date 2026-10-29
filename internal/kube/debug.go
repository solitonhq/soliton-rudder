package kube

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

// Debug containers use Kubernetes' EphemeralContainers API (what `kubectl debug` does): a container with a
// tool-rich image is added to a *running* pod, optionally sharing a target container's process namespace.

var (
	debugPollEvery = 700 * time.Millisecond
	debugWaitMax   = 90 * time.Second
)

type DebugOptions struct {
	Image  string `json:"image"`
	Target string `json:"target"` // container whose process/network namespaces to share ("" = none)
	Caps   bool   `json:"caps"`   // add NET_ADMIN and SYS_PTRACE
}

type DebugContainer struct {
	Name    string `json:"name"`
	Image   string `json:"image"`
	Target  string `json:"target"`
	State   string `json:"state"` // running | waiting | terminated | unknown
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

var imageRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._\-/:@]{0,254}$`)

var failingWaiting = map[string]bool{
	"ErrImagePull": true, "ImagePullBackOff": true, "InvalidImageName": true,
	"CreateContainerConfigError": true, "CreateContainerError": true, "RunContainerError": true,
}

func podGVR() Resource { return podRes }

// DebugContainers lists the ephemeral (debug) containers already present in a pod, so they can be re-attached.
func (s *Session) DebugContainers(ctx context.Context, ns, pod string) ([]DebugContainer, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	p, err := s.client(podGVR(), ns).Get(ctx, pod, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return debugContainersOf(p), nil
}

func debugContainersOf(p *unstructured.Unstructured) []DebugContainer {
	specs, _, _ := unstructured.NestedSlice(p.Object, "spec", "ephemeralContainers")
	statuses, _, _ := unstructured.NestedSlice(p.Object, "status", "ephemeralContainerStatuses")
	out := make([]DebugContainer, 0, len(specs))
	for _, sp := range specs {
		sm, _ := sp.(map[string]any)
		dc := DebugContainer{Name: str(sm["name"]), Image: str(sm["image"]), Target: str(sm["targetContainerName"]), State: "unknown"}
		for _, st := range statuses {
			sm2, _ := st.(map[string]any)
			if str(sm2["name"]) != dc.Name {
				continue
			}
			state, _ := sm2["state"].(map[string]any)
			switch {
			case state["running"] != nil:
				dc.State = "running"
			case state["waiting"] != nil:
				w, _ := state["waiting"].(map[string]any)
				dc.State, dc.Reason, dc.Message = "waiting", str(w["reason"]), str(w["message"])
			case state["terminated"] != nil:
				t, _ := state["terminated"].(map[string]any)
				dc.State, dc.Reason, dc.Message = "terminated", str(t["reason"]), str(t["message"])
			}
		}
		out = append(out, dc)
	}
	return out
}

func randSuffix(n int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		v, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			v = big.NewInt(int64(i))
		}
		b[i] = alphabet[v.Int64()]
	}
	return string(b)
}

// debugContainerName picks "debugger-xxxxx", unique among every container name in the pod.
func debugContainerName(p *unstructured.Unstructured) string {
	used := map[string]bool{}
	for _, f := range []string{"containers", "initContainers", "ephemeralContainers"} {
		list, _, _ := unstructured.NestedSlice(p.Object, "spec", f)
		for _, c := range list {
			if m, ok := c.(map[string]any); ok {
				used[str(m["name"])] = true
			}
		}
	}
	for {
		n := "debugger-" + randSuffix(5)
		if !used[n] {
			return n
		}
	}
}

func debugContainerSpec(name string, o DebugOptions) map[string]any {
	c := map[string]any{
		"name": name, "image": o.Image, "imagePullPolicy": "IfNotPresent",
		"stdin": true, "tty": true, "terminationMessagePolicy": "File",
	}
	if o.Target != "" {
		c["targetContainerName"] = o.Target
	}
	if o.Caps {
		c["securityContext"] = map[string]any{"capabilities": map[string]any{"add": []string{"NET_ADMIN", "SYS_PTRACE"}}}
	}
	return c
}

// StartDebug adds a debug container to the pod and waits until it is running.
// Note: ephemeral containers cannot be removed from a pod; they live until the pod is recreated.
func (s *Session) StartDebug(ctx context.Context, ns, pod string, o DebugOptions) (DebugContainer, error) {
	o.Image = strings.TrimSpace(o.Image)
	if !imageRe.MatchString(o.Image) {
		return DebugContainer{}, errors.New("enter a valid container image, e.g. nicolaka/netshoot")
	}
	if v, err := s.ServerVersion(ctx); err == nil {
		if m := serverMinor(v); m > 0 && m < 23 {
			return DebugContainer{}, fmt.Errorf("this cluster runs Kubernetes %s; debug containers need 1.23 or newer", v)
		}
	}
	gctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	cl := s.client(podGVR(), ns)
	p, err := cl.Get(gctx, pod, metav1.GetOptions{})
	if err != nil {
		return DebugContainer{}, err
	}
	if phase, _, _ := unstructured.NestedString(p.Object, "status", "phase"); phase != "Running" {
		return DebugContainer{}, fmt.Errorf("the pod is %s; debug containers can only be added to running pods", orDefault(phase, "not running"))
	}
	if o.Target != "" {
		found := false
		cs, _, _ := unstructured.NestedSlice(p.Object, "spec", "containers")
		for _, c := range cs {
			if m, ok := c.(map[string]any); ok && str(m["name"]) == o.Target {
				found = true
			}
		}
		if !found {
			return DebugContainer{}, fmt.Errorf("container %q does not exist in this pod", o.Target)
		}
	}
	name := debugContainerName(p)
	patch, err := json.Marshal(map[string]any{"spec": map[string]any{"ephemeralContainers": []any{debugContainerSpec(name, o)}}})
	if err != nil {
		return DebugContainer{}, err
	}
	if _, err := cl.Patch(gctx, pod, types.StrategicMergePatchType, patch, metav1.PatchOptions{FieldManager: fieldManager}, "ephemeralcontainers"); err != nil {
		return DebugContainer{}, friendlyDebugError(err)
	}
	return s.waitDebugRunning(ctx, ns, pod, name)
}

func friendlyDebugError(err error) error {
	switch {
	case apierrors.IsForbidden(err):
		msg := err.Error()
		if strings.Contains(msg, "violates PodSecurity") || strings.Contains(msg, "admission") {
			return fmt.Errorf("blocked by an admission policy (try turning off \"Add NET_ADMIN and SYS_PTRACE\"): %w", err)
		}
		return fmt.Errorf("you need the \"patch\" permission on pods/ephemeralcontainers: %w", err)
	case apierrors.IsNotFound(err):
		return fmt.Errorf("this cluster does not support ephemeral containers (Kubernetes 1.23+ required): %w", err)
	case apierrors.IsInvalid(err):
		return fmt.Errorf("the API server rejected the debug container: %w", err)
	}
	return err
}

func (s *Session) waitDebugRunning(ctx context.Context, ns, pod, name string) (DebugContainer, error) {
	deadline := time.Now().Add(debugWaitMax)
	t := time.NewTicker(debugPollEvery)
	defer t.Stop()
	var last DebugContainer
	for {
		cs, err := s.DebugContainers(ctx, ns, pod)
		if err == nil {
			for _, c := range cs {
				if c.Name != name {
					continue
				}
				last = c
				switch {
				case c.State == "running":
					return c, nil
				case c.State == "terminated":
					return c, fmt.Errorf("the debug container exited (%s %s)", c.Reason, c.Message)
				case c.State == "waiting" && failingWaiting[c.Reason]:
					return c, fmt.Errorf("the debug container cannot start: %s %s (does the cluster have access to the image?)", c.Reason, c.Message)
				}
			}
		}
		if time.Now().After(deadline) {
			return last, fmt.Errorf("timed out waiting for the debug container to start (state: %s %s)", orDefault(last.State, "unknown"), last.Reason)
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-t.C:
		}
	}
}

var minorRe = regexp.MustCompile(`v?(\d+)\.(\d+)`)

func serverMinor(v string) int {
	m := minorRe.FindStringSubmatch(v)
	if m == nil {
		return 0
	}
	n := 0
	fmt.Sscanf(m[2], "%d", &n)
	return n
}
