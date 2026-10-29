package kube

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"k8s.io/streaming/pkg/httpstream"
)

type ForwardTarget struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"` // pod | service | deployment | statefulset | daemonset | replicaset
	Name      string `json:"name"`
}

// ForwardKinds maps the plural resource names the UI uses to forward-target kinds.
var ForwardKinds = map[string]string{
	"pods": "pod", "services": "service", "deployments": "deployment",
	"statefulsets": "statefulset", "daemonsets": "daemonset", "replicasets": "replicaset",
}

var workloadResources = map[string]Resource{
	"deployment":  {Group: "apps", Version: "v1", Name: "deployments", Namespaced: true},
	"statefulset": {Group: "apps", Version: "v1", Name: "statefulsets", Namespaced: true},
	"daemonset":   {Group: "apps", Version: "v1", Name: "daemonsets", Namespaced: true},
	"replicaset":  {Group: "apps", Version: "v1", Name: "replicasets", Namespaced: true},
}

var (
	podRes = Resource{Group: "core", Version: "v1", Name: "pods", Namespaced: true}
	svcRes = Resource{Group: "core", Version: "v1", Name: "services", Namespaced: true}
)

// pickReadyPod returns the newest ready, non-terminating pod (newest follows a rolling update; name breaks ties).
func pickReadyPod(pods []unstructured.Unstructured) *unstructured.Unstructured {
	var ready []*unstructured.Unstructured
	for i := range pods {
		if podReady(&pods[i]) {
			ready = append(ready, &pods[i])
		}
	}
	if len(ready) == 0 {
		return nil
	}
	sort.Slice(ready, func(i, j int) bool {
		ti, tj := ready[i].GetCreationTimestamp(), ready[j].GetCreationTimestamp()
		if !ti.Equal(&tj) {
			return tj.Before(&ti)
		}
		return ready[i].GetName() < ready[j].GetName()
	})
	return ready[0]
}

func (s *Session) listPodsBySelector(ctx context.Context, ns string, sel map[string]string) ([]unstructured.Unstructured, error) {
	list, err := s.client(podRes, ns).List(ctx, metav1.ListOptions{LabelSelector: joinSelector(sel)})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// forwardResolver returns a function that picks a ready pod (and container port) for the target on every call.
// Services follow their selector and port mapping; workloads follow their selector; a pod follows its owning
// workload, so a restarted or rolled pod is replaced by its successor instead of killing the forward.
func (s *Session) forwardResolver(t ForwardTarget, remotePort int) func(ctx context.Context) (string, int, error) {
	var family *podFamily
	return func(ctx context.Context) (string, int, error) {
		ctx, cancel := context.WithTimeout(ctx, opTimeout)
		defer cancel()
		switch t.Kind {
		case "service":
			return s.resolveService(ctx, t, remotePort)
		case "pod":
			if family == nil {
				f, err := s.podFamilyOf(ctx, t.Namespace, t.Name)
				if err != nil {
					return "", 0, err
				}
				family = f
			}
			return s.resolveFamily(ctx, t, family, remotePort)
		default:
			res, ok := workloadResources[t.Kind]
			if !ok {
				return "", 0, fmt.Errorf("unsupported forward target kind %q", t.Kind)
			}
			obj, err := s.client(res, t.Namespace).Get(ctx, t.Name, metav1.GetOptions{})
			if err != nil {
				return "", 0, err
			}
			sel, _, _ := unstructured.NestedStringMap(obj.Object, "spec", "selector", "matchLabels")
			if len(sel) == 0 {
				return "", 0, fmt.Errorf("%s %s has no pod selector", t.Kind, t.Name)
			}
			pods, err := s.listPodsBySelector(ctx, t.Namespace, sel)
			if err != nil {
				return "", 0, err
			}
			p := pickReadyPod(pods)
			if p == nil {
				return "", 0, fmt.Errorf("no ready pods behind %s %s", t.Kind, t.Name)
			}
			return p.GetName(), remotePort, nil
		}
	}
}

func (s *Session) resolveService(ctx context.Context, t ForwardTarget, remotePort int) (string, int, error) {
	svc, err := s.client(svcRes, t.Namespace).Get(ctx, t.Name, metav1.GetOptions{})
	if err != nil {
		return "", 0, err
	}
	sel, _, _ := unstructured.NestedStringMap(svc.Object, "spec", "selector")
	if len(sel) == 0 {
		return "", 0, fmt.Errorf("service %s has no selector", t.Name)
	}
	ports, _, _ := unstructured.NestedSlice(svc.Object, "spec", "ports")
	var targetPort any = int64(remotePort)
	found := false
	for _, p := range ports {
		m, _ := p.(map[string]any)
		if n, ok := m["port"].(int64); ok && int(n) == remotePort {
			found = true
			if tp, ok := m["targetPort"]; ok && tp != nil {
				targetPort = tp
			}
			break
		}
	}
	if !found {
		return "", 0, fmt.Errorf("service %s does not expose port %d", t.Name, remotePort)
	}
	pods, err := s.listPodsBySelector(ctx, t.Namespace, sel)
	if err != nil {
		return "", 0, err
	}
	pod := pickReadyPod(pods)
	if pod == nil {
		return "", 0, fmt.Errorf("no ready pods behind service %s", t.Name)
	}
	switch v := targetPort.(type) {
	case int64:
		return pod.GetName(), int(v), nil
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return pod.GetName(), n, nil
		}
		if p, ok := namedContainerPort(pod, v); ok {
			return pod.GetName(), p, nil
		}
		return "", 0, fmt.Errorf("named port %q not found on pod %s", v, pod.GetName())
	}
	return pod.GetName(), remotePort, nil
}

// podFamily describes which pods can replace a forwarded pod.
type podFamily struct {
	selector map[string]string // owning workload's selector ("" for bare/StatefulSet pods)
	sameName bool              // StatefulSet and bare pods come back under the same name
}

func (s *Session) podFamilyOf(ctx context.Context, ns, name string) (*podFamily, error) {
	p, err := s.client(podRes, ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	fam := &podFamily{sameName: true}
	for _, ref := range p.GetOwnerReferences() {
		kind, owner := ref.Kind, ref.Name
		if kind == "ReplicaSet" { // follow Deployment → ReplicaSet so rollouts (new ReplicaSets) are covered
			if rs, err := s.client(workloadResources["replicaset"], ns).Get(ctx, owner, metav1.GetOptions{}); err == nil {
				for _, r2 := range rs.GetOwnerReferences() {
					if r2.Kind == "Deployment" {
						kind, owner = "Deployment", r2.Name
					}
				}
			}
		}
		res, ok := workloadResources[strings.ToLower(kind)]
		if !ok {
			continue
		}
		obj, err := s.client(res, ns).Get(ctx, owner, metav1.GetOptions{})
		if err != nil {
			continue
		}
		if sel, _, _ := unstructured.NestedStringMap(obj.Object, "spec", "selector", "matchLabels"); len(sel) > 0 && kind != "StatefulSet" {
			fam.selector, fam.sameName = sel, false
		}
	}
	return fam, nil
}

func (s *Session) resolveFamily(ctx context.Context, t ForwardTarget, fam *podFamily, remotePort int) (string, int, error) {
	// the pod itself, while it is healthy
	if p, err := s.client(podRes, t.Namespace).Get(ctx, t.Name, metav1.GetOptions{}); err == nil && podReady(p) {
		return t.Name, remotePort, nil
	}
	if fam.selector != nil {
		pods, err := s.listPodsBySelector(ctx, t.Namespace, fam.selector)
		if err != nil {
			return "", 0, err
		}
		if p := pickReadyPod(pods); p != nil {
			return p.GetName(), remotePort, nil
		}
		return "", 0, fmt.Errorf("pod %s is gone and no ready replacement exists yet", t.Name)
	}
	return "", 0, fmt.Errorf("pod %s is not ready; waiting for it to come back", t.Name)
}

func podReady(p *unstructured.Unstructured) bool {
	if p.GetDeletionTimestamp() != nil {
		return false
	}
	phase, _, _ := unstructured.NestedString(p.Object, "status", "phase")
	if phase != "Running" {
		return false
	}
	conds, _, _ := unstructured.NestedSlice(p.Object, "status", "conditions")
	for _, c := range conds {
		m, _ := c.(map[string]any)
		if m["type"] == "Ready" {
			return m["status"] == "True"
		}
	}
	return false
}

func namedContainerPort(p *unstructured.Unstructured, name string) (int, bool) {
	cs, _, _ := unstructured.NestedSlice(p.Object, "spec", "containers")
	for _, c := range cs {
		cm, _ := c.(map[string]any)
		ps, _ := cm["ports"].([]any)
		for _, pp := range ps {
			pm, _ := pp.(map[string]any)
			if pm["name"] == name {
				if n, ok := pm["containerPort"].(int64); ok {
					return int(n), true
				}
			}
		}
	}
	return 0, false
}

// RunForward keeps localPort forwarded to the target until ctx is cancelled. The local listener stays open the
// whole time; the backing pod is replaced transparently (see forward_engine.go).
// localPort 0 picks a free port once and keeps it.
func (s *Session) RunForward(ctx context.Context, t ForwardTarget, remotePort, localPort int, report func(ForwardState)) {
	resolve := s.forwardResolver(t, remotePort)
	deps := forwardDeps{
		resolve: resolve,
		healthy: func(ctx context.Context, pod string) (bool, string) {
			cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			p, err := s.client(podRes, t.Namespace).Get(cctx, pod, metav1.GetOptions{})
			switch {
			case err != nil && ctx.Err() == nil && isNotFound(err):
				return false, "was deleted"
			case err != nil:
				return true, "" // API hiccup: don't flap the tunnel on a transient error
			case p.GetDeletionTimestamp() != nil:
				return false, "is terminating"
			case !podReady(p):
				return false, "is not ready"
			}
			return true, ""
		},
		start: func(ctx context.Context, pod string, port int) (*tunnel, error) {
			return s.startTunnel(ctx, t.Namespace, pod, port)
		},
	}
	runForward(ctx, deps, localPort, report, defaultForwardTimings)
}

// startTunnel opens a port-forward to the pod on an internal loopback port.
func (s *Session) startTunnel(ctx context.Context, ns, pod string, remote int) (*tunnel, error) {
	u := podSubresourceURL(s, ns, pod, "portforward", nil)
	rt, upgrader, err := spdy.RoundTripperFor(s.cfg)
	if err != nil {
		return nil, err
	}
	spdyDialer := spdy.NewDialer(upgrader, &http.Client{Transport: rt}, http.MethodPost, u)
	tunnelDialer, err := portforward.NewSPDYOverWebsocketDialer(u, s.cfg)
	if err != nil {
		return nil, err
	}
	dialer := portforward.NewFallbackDialer(tunnelDialer, spdyDialer, func(err error) bool {
		return httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err)
	})

	stop := make(chan struct{})
	ready := make(chan struct{})
	var errBuf strings.Builder
	pf, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, []string{fmt.Sprintf("0:%d", remote)}, stop, ready, io.Discard, &errBuf)
	if err != nil {
		return nil, err
	}
	t := &tunnel{pod: pod, done: make(chan struct{})}
	var once sync.Once
	t.stop = func() { once.Do(func() { close(stop) }) }
	go func() {
		err := pf.ForwardPorts()
		t.finish(err)
	}()
	select {
	case <-ready:
		ports, err := pf.GetPorts()
		if err != nil || len(ports) == 0 {
			t.stop()
			return nil, fmt.Errorf("could not determine the tunnel port: %v", err)
		}
		t.addr = fmt.Sprintf("127.0.0.1:%d", ports[0].Local)
		return t, nil
	case <-t.done:
		return nil, fmt.Errorf("%v %s", t.err, strings.TrimSpace(errBuf.String()))
	case <-ctx.Done():
		t.stop()
		return nil, ctx.Err()
	case <-time.After(20 * time.Second):
		t.stop()
		return nil, fmt.Errorf("timed out opening the tunnel")
	}
}

func isNotFound(err error) bool { return apierrors.IsNotFound(err) }
