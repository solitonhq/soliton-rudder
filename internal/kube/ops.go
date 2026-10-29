package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"
)

const opTimeout = 30 * time.Second

func (s *Session) client(r Resource, ns string) dynamic.ResourceInterface {
	g := r.Group
	if g == "core" {
		g = ""
	}
	ri := s.dyn.Resource(schema.GroupVersionResource{Group: g, Version: r.Version, Resource: r.Name})
	if r.Namespaced {
		return ri.Namespace(ns)
	}
	return ri
}

// GetYAML returns the object as YAML. managedFields are omitted: they are noise for humans.
func (s *Session) GetYAML(ctx context.Context, key, ns, name string) (string, error) {
	res, err := s.resource(key)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	obj, err := s.client(res, ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	unstructured.RemoveNestedField(obj.Object, "metadata", "managedFields")
	out, err := yaml.Marshal(obj.Object)
	return string(out), err
}

// UpdateYAML replaces the object with the edited YAML (optimistic concurrency via resourceVersion).
func (s *Session) UpdateYAML(ctx context.Context, key, ns, name, doc string) (string, error) {
	res, err := s.resource(key)
	if err != nil {
		return "", err
	}
	var m map[string]any
	if err := yaml.Unmarshal([]byte(doc), &m); err != nil {
		return "", fmt.Errorf("invalid YAML: %w", err)
	}
	obj := &unstructured.Unstructured{Object: m}
	if obj.GetName() != name {
		return "", fmt.Errorf("metadata.name must stay %q", name)
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	updated, err := s.client(res, ns).Update(ctx, obj, metav1.UpdateOptions{FieldManager: "soliton-rudder"})
	if err != nil {
		return "", err
	}
	unstructured.RemoveNestedField(updated.Object, "metadata", "managedFields")
	out, err := yaml.Marshal(updated.Object)
	return string(out), err
}

func (s *Session) Delete(ctx context.Context, key, ns, name string) error {
	res, err := s.resource(key)
	if err != nil {
		return err
	}
	return s.DeleteObject(ctx, res, ns, name)
}

func (s *Session) DeleteObject(ctx context.Context, res Resource, ns, name string) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	policy := metav1.DeletePropagationBackground
	return s.client(res, ns).Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: &policy})
}

// Scale sets spec.replicas through the scale subresource.
func (s *Session) Scale(ctx context.Context, key, ns, name string, replicas int32) error {
	res, err := s.resource(key)
	if err != nil {
		return err
	}
	return s.ScaleObject(ctx, res, ns, name, replicas)
}

func (s *Session) ScaleObject(ctx context.Context, res Resource, ns, name string, replicas int32) error {
	var err error
	if replicas < 0 {
		return fmt.Errorf("replicas must be >= 0")
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	patch := fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas)
	_, err = s.client(res, ns).Patch(ctx, name, types.MergePatchType, []byte(patch), metav1.PatchOptions{FieldManager: "soliton-rudder"}, "scale")
	return err
}

// Restart triggers a rolling restart, like `kubectl rollout restart`.
func (s *Session) Restart(ctx context.Context, key, ns, name string) error {
	res, err := s.resource(key)
	if err != nil {
		return err
	}
	return s.RestartObject(ctx, res, ns, name)
}

func (s *Session) RestartObject(ctx context.Context, res Resource, ns, name string) error {
	var err error
	switch res.Name {
	case "deployments", "statefulsets", "daemonsets":
	default:
		return fmt.Errorf("restart is not supported for %s", res.Name)
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`,
		time.Now().UTC().Format(time.RFC3339))
	_, err = s.client(res, ns).Patch(ctx, name, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{FieldManager: "soliton-rudder"})
	return err
}

// Events lists Kubernetes Events involving the object, newest first.
func (s *Session) Events(ctx context.Context, key, ns, name string) ([]Event, error) {
	res, err := s.resource(key)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	evRes := Resource{Group: "core", Version: "v1", Name: "events", Namespaced: true}
	sel := fmt.Sprintf("involvedObject.name=%s,involvedObject.kind=%s", name, res.Kind)
	list, err := s.client(evRes, ns).List(ctx, metav1.ListOptions{FieldSelector: sel})
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(list.Items))
	for _, it := range list.Items {
		typ, _, _ := unstructured.NestedString(it.Object, "type")
		reason, _, _ := unstructured.NestedString(it.Object, "reason")
		msg, _, _ := unstructured.NestedString(it.Object, "message")
		count, _, _ := unstructured.NestedInt64(it.Object, "count")
		last, _, _ := unstructured.NestedString(it.Object, "lastTimestamp")
		if last == "" {
			last, _, _ = unstructured.NestedString(it.Object, "eventTime")
		}
		if last == "" {
			last = it.GetCreationTimestamp().UTC().Format(time.RFC3339)
		}
		src, _, _ := unstructured.NestedString(it.Object, "source", "component")
		out = append(out, Event{Type: typ, Reason: reason, Message: strings.TrimSpace(msg), Count: count, Last: last, Source: src})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last > out[j].Last })
	return out, nil
}

// Describe returns metadata useful for an overview panel.
type Meta struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	Owners      []string          `json:"owners"`
	Created     string            `json:"created"`
	UID         string            `json:"uid"`
	Finalizers  []string          `json:"finalizers"`
}

func (s *Session) Meta(ctx context.Context, key, ns, name string) (Meta, error) {
	res, err := s.resource(key)
	if err != nil {
		return Meta{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	obj, err := s.client(res, ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return Meta{}, err
	}
	m := Meta{
		Labels: obj.GetLabels(), Annotations: obj.GetAnnotations(), UID: string(obj.GetUID()),
		Created: obj.GetCreationTimestamp().UTC().Format(time.RFC3339), Finalizers: obj.GetFinalizers(),
	}
	for _, o := range obj.GetOwnerReferences() {
		m.Owners = append(m.Owners, o.Kind+"/"+o.Name)
	}
	delete(m.Annotations, "kubectl.kubernetes.io/last-applied-configuration") // huge and redundant
	return m, nil
}

// PortInfo is a port a pod container or service exposes.
type PortInfo struct {
	Name     string `json:"name"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Target   string `json:"target"` // container name (pods) or targetPort (services)
}

// Ports lists forwardable ports of a pod or service.
func (s *Session) Ports(ctx context.Context, key, ns, name string) ([]PortInfo, error) {
	res, err := s.resource(key)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	obj, err := s.client(res, ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	out := []PortInfo{}
	switch res.Name {
	case "pods":
		cs, _, _ := unstructured.NestedSlice(obj.Object, "spec", "containers")
		for _, c := range cs {
			cm, _ := c.(map[string]any)
			ps, _ := cm["ports"].([]any)
			for _, p := range ps {
				pm, _ := p.(map[string]any)
				port, _ := pm["containerPort"].(int64)
				pi := PortInfo{Port: int(port), Protocol: "TCP", Target: fmt.Sprint(cm["name"])}
				pi.Name, _ = pm["name"].(string)
				if proto, ok := pm["protocol"].(string); ok {
					pi.Protocol = proto
				}
				out = append(out, pi)
			}
		}
	case "deployments", "statefulsets", "daemonsets", "replicasets":
		cs, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
		for _, c := range cs {
			cm, _ := c.(map[string]any)
			ps, _ := cm["ports"].([]any)
			for _, p := range ps {
				pm, _ := p.(map[string]any)
				port, _ := pm["containerPort"].(int64)
				pi := PortInfo{Port: int(port), Protocol: "TCP", Target: fmt.Sprint(cm["name"])}
				pi.Name, _ = pm["name"].(string)
				if proto, ok := pm["protocol"].(string); ok {
					pi.Protocol = proto
				}
				out = append(out, pi)
			}
		}
	case "services":
		ps, _, _ := unstructured.NestedSlice(obj.Object, "spec", "ports")
		for _, p := range ps {
			pm, _ := p.(map[string]any)
			port, _ := pm["port"].(int64)
			pi := PortInfo{Port: int(port), Protocol: "TCP", Target: fmt.Sprint(pm["targetPort"])}
			pi.Name, _ = pm["name"].(string)
			if proto, ok := pm["protocol"].(string); ok {
				pi.Protocol = proto
			}
			out = append(out, pi)
		}
	default:
		return nil, fmt.Errorf("port forwarding is not supported for %s", res.Kind)
	}
	return out, nil
}

// ListRaw lists every object of a resource (all namespaces), paging to bound memory per request.
// A missing API (404) yields an empty list so callers can treat optional kinds uniformly.
func (s *Session) ListRaw(ctx context.Context, group, version, name string, namespaced bool) ([]unstructured.Unstructured, error) {
	return s.ListRawSelector(ctx, group, version, name, namespaced, "", "")
}

// ListRawSelector is ListRaw limited to a namespace ("" = all) and/or a field selector. For a user who can only read
// some namespaces, "all" means every namespace they can access.
func (s *Session) ListRawSelector(ctx context.Context, group, version, name string, namespaced bool, ns, fieldSelector string) ([]unstructured.Unstructured, error) {
	r := Resource{Group: group, Version: version, Name: name, Namespaced: namespaced}
	targets := s.listTargets(ctx, r, ns)
	if len(targets) == 1 {
		return s.listPages(ctx, r, targets[0], fieldSelector)
	}
	var out []unstructured.Unstructured
	var firstErr error
	ok := 0
	for _, t := range targets {
		items, err := s.listPages(ctx, r, t, fieldSelector)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue // a namespace that is forbidden or gone must not hide the others
		}
		ok++
		out = append(out, items...)
	}
	if ok == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// listPages lists one namespace (or the whole cluster for "") in pages, to bound memory per request.
func (s *Session) listPages(ctx context.Context, r Resource, ns, fieldSelector string) ([]unstructured.Unstructured, error) {
	var out []unstructured.Unstructured
	cont := ""
	for {
		cctx, cancel := context.WithTimeout(ctx, 2*opTimeout)
		list, err := s.client(r, ns).List(cctx, metav1.ListOptions{Limit: 500, Continue: cont, FieldSelector: fieldSelector})
		cancel()
		if err != nil {
			if apierrors.IsNotFound(err) {
				return out, nil
			}
			return out, err
		}
		out = append(out, list.Items...)
		cont = list.GetContinue()
		if cont == "" {
			return out, nil
		}
	}
}

// ServerVersion returns the API server's git version (e.g. v1.30.2-eks-abc).
func (s *Session) ServerVersion(ctx context.Context) (string, error) {
	type res struct {
		v   string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		v, err := s.disc.ServerVersion()
		if err != nil {
			ch <- res{err: err}
			return
		}
		ch <- res{v: v.GitVersion}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		return r.v, r.err
	}
}
