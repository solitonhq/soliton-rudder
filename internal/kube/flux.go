package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

const fluxSuffix = ".toolkit.fluxcd.io"

type FluxObject struct {
	Key            string `json:"key"` // resource key (group/version/plural) for navigation
	Kind           string `json:"kind"`
	Name           string `json:"name"`
	Namespace      string `json:"namespace"`
	Ready          string `json:"ready"` // True | False | Unknown
	Reason         string `json:"reason"`
	Message        string `json:"message"`
	Suspended      bool   `json:"suspended"`
	Revision       string `json:"revision"`
	Source         string `json:"source"`
	Path           string `json:"path"`
	Interval       string `json:"interval"`
	LastTransition string `json:"lastTransition"`
	Created        string `json:"created"`
}

type FluxCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	At      string `json:"at"`
}

type FluxInventoryItem struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Group     string `json:"group"`
	Kind      string `json:"kind"`
}

type FluxDetail struct {
	FluxObject
	Conditions []FluxCondition     `json:"conditions"`
	Inventory  []FluxInventoryItem `json:"inventory"`
}

// FluxKinds returns the Flux resources present on the cluster.
func (s *Session) FluxKinds() []Resource {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Resource{}
	for _, r := range s.catalog {
		if strings.HasSuffix(r.Group, fluxSuffix) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (s *Session) FluxObjects(ctx context.Context) ([]FluxObject, error) {
	out := []FluxObject{}
	for _, r := range s.FluxKinds() {
		items, err := s.ListRaw(ctx, r.Group, r.Version, r.Name, r.Namespaced)
		if err != nil {
			continue // one inaccessible Flux kind should not hide the rest
		}
		for i := range items {
			out = append(out, parseFlux(r, &items[i]))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Namespace+"/"+a.Name < b.Namespace+"/"+b.Name
	})
	return out, nil
}

func parseFlux(r Resource, o *unstructured.Unstructured) FluxObject {
	f := FluxObject{
		Key: r.Key, Kind: r.Kind, Name: o.GetName(), Namespace: o.GetNamespace(), Created: o.GetCreationTimestamp().UTC().Format(time.RFC3339),
		Suspended: nbool(o.Object, "spec", "suspend"), Interval: nstr(o.Object, "spec", "interval"), Path: nstr(o.Object, "spec", "path"),
		Ready: "Unknown",
	}
	conds, _, _ := unstructured.NestedSlice(o.Object, "status", "conditions")
	for _, c := range conds {
		cm, _ := c.(map[string]any)
		if nstr(cm, "type") == "Ready" {
			f.Ready, f.Reason, f.Message, f.LastTransition = nstr(cm, "status"), nstr(cm, "reason"), nstr(cm, "message"), nstr(cm, "lastTransitionTime")
		}
	}
	for _, path := range [][]string{{"status", "lastAppliedRevision"}, {"status", "artifact", "revision"}, {"status", "lastAttemptedRevision"}} {
		if v := nstr(o.Object, path...); v != "" {
			f.Revision = v
			break
		}
	}
	if f.Revision == "" {
		if h, _, _ := unstructured.NestedSlice(o.Object, "status", "history"); len(h) > 0 {
			hm, _ := h[0].(map[string]any)
			f.Revision = nstr(hm, "chartVersion")
		}
	}
	for _, path := range [][]string{{"spec", "sourceRef"}, {"spec", "chart", "spec", "sourceRef"}, {"spec", "chartRef"}} {
		if ref, ok, _ := unstructured.NestedMap(o.Object, path...); ok {
			f.Source = nstr(ref, "kind") + "/" + nstr(ref, "name")
			break
		}
	}
	if f.Source == "" {
		f.Source = nstr(o.Object, "spec", "url")
	}
	return f
}

func (s *Session) fluxResource(key string) (Resource, error) {
	r, err := s.resource(key)
	if err != nil {
		return r, err
	}
	if !strings.HasSuffix(r.Group, fluxSuffix) {
		return r, fmt.Errorf("%s is not a Flux resource", r.Kind)
	}
	return r, nil
}

func (s *Session) FluxDetail(ctx context.Context, key, ns, name string) (FluxDetail, error) {
	r, err := s.fluxResource(key)
	if err != nil {
		return FluxDetail{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	o, err := s.client(r, ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return FluxDetail{}, err
	}
	d := FluxDetail{FluxObject: parseFlux(r, o), Conditions: []FluxCondition{}, Inventory: []FluxInventoryItem{}}
	conds, _, _ := unstructured.NestedSlice(o.Object, "status", "conditions")
	for _, c := range conds {
		cm, _ := c.(map[string]any)
		d.Conditions = append(d.Conditions, FluxCondition{Type: nstr(cm, "type"), Status: nstr(cm, "status"), Reason: nstr(cm, "reason"), Message: nstr(cm, "message"), At: nstr(cm, "lastTransitionTime")})
	}
	entries, _, _ := unstructured.NestedSlice(o.Object, "status", "inventory", "entries")
	for _, e := range entries {
		em, _ := e.(map[string]any)
		if it, ok := parseInventoryID(nstr(em, "id")); ok {
			d.Inventory = append(d.Inventory, it)
		}
	}
	return d, nil
}

// parseInventoryID decodes Flux's "<namespace>_<name>_<group>_<kind>" object ids.
func parseInventoryID(id string) (FluxInventoryItem, bool) {
	parts := strings.Split(id, "_")
	if len(parts) != 4 {
		return FluxInventoryItem{}, false
	}
	return FluxInventoryItem{Namespace: parts[0], Name: strings.ReplaceAll(parts[1], "__", "/"), Group: parts[2], Kind: parts[3]}, true
}

// FluxReconcile requests an immediate reconciliation (same annotation `flux reconcile` sets).
func (s *Session) FluxReconcile(ctx context.Context, key, ns, name string) error {
	r, err := s.fluxResource(key)
	if err != nil {
		return err
	}
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{
		"reconcile.fluxcd.io/requestedAt": time.Now().UTC().Format(time.RFC3339Nano)}}})
	return s.fluxPatch(ctx, r, ns, name, patch)
}

func (s *Session) FluxSuspend(ctx context.Context, key, ns, name string, suspend bool) error {
	r, err := s.fluxResource(key)
	if err != nil {
		return err
	}
	patch, _ := json.Marshal(map[string]any{"spec": map[string]any{"suspend": suspend}})
	return s.fluxPatch(ctx, r, ns, name, patch)
}

func (s *Session) fluxPatch(ctx context.Context, r Resource, ns, name string, patch []byte) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	_, err := s.client(r, ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{FieldManager: fieldManager})
	return err
}
