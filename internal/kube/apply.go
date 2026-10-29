package kube

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/restmapper"
	"sigs.k8s.io/yaml"
)

const fieldManager = "soliton-rudder"

// ApplyResult is the per-document outcome of a preview or apply.
type ApplyResult struct {
	Index     int    `json:"index"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Action    string `json:"action"` // create | configure | unchanged | error
	Before    string `json:"before"` // live YAML ("" if it does not exist)
	After     string `json:"after"`  // YAML as it would be (or is now) stored
	Error     string `json:"error"`
}

// ParseManifest splits multi-document YAML/JSON (and expands `kind: List`).
func ParseManifest(doc string) ([]*unstructured.Unstructured, error) {
	dec := utilyaml.NewYAMLOrJSONDecoder(strings.NewReader(doc), 4096)
	var out []*unstructured.Unstructured
	for i := 0; ; i++ {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("document %d: %w", i+1, err)
		}
		if len(m) == 0 {
			continue
		}
		u := &unstructured.Unstructured{Object: m}
		if u.IsList() {
			if err := u.EachListItem(func(o runtime.Object) error {
				out = append(out, o.(*unstructured.Unstructured))
				return nil
			}); err != nil {
				return nil, err
			}
			continue
		}
		if u.GetKind() == "" || u.GetAPIVersion() == "" {
			return nil, fmt.Errorf("document %d: apiVersion and kind are required", i+1)
		}
		out = append(out, u)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no resources found in manifest")
	}
	return out, nil
}

func (s *Session) mapper() meta.RESTMapper {
	return restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(s.disc))
}

// PreviewManifest runs a server-side dry-run apply for every document.
func (s *Session) PreviewManifest(ctx context.Context, doc, defaultNS string, force bool) ([]ApplyResult, error) {
	return s.applyManifest(ctx, doc, defaultNS, force, true)
}

// ApplyManifest applies every document with server-side apply. Failures are per document.
func (s *Session) ApplyManifest(ctx context.Context, doc, defaultNS string, force bool) ([]ApplyResult, error) {
	return s.applyManifest(ctx, doc, defaultNS, force, false)
}

func (s *Session) applyManifest(ctx context.Context, doc, defaultNS string, force, dryRun bool) ([]ApplyResult, error) {
	objs, err := ParseManifest(doc)
	if err != nil {
		return nil, err
	}
	mapper := s.mapper()
	results := make([]ApplyResult, 0, len(objs))
	for i, obj := range objs {
		r := ApplyResult{Index: i, Kind: obj.GetKind(), Name: obj.GetName(), Namespace: obj.GetNamespace()}
		results = append(results, s.applyOne(ctx, mapper, obj, defaultNS, force, dryRun, r))
	}
	return results, nil
}

func (s *Session) applyOne(ctx context.Context, mapper meta.RESTMapper, obj *unstructured.Unstructured, defaultNS string, force, dryRun bool, r ApplyResult) ApplyResult {
	fail := func(err error) ApplyResult {
		r.Action, r.Error = "error", err.Error()
		return r
	}
	if obj.GetName() == "" {
		return fail(fmt.Errorf("metadata.name is required"))
	}
	gvk := obj.GroupVersionKind()
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fail(fmt.Errorf("unknown kind %s: %w", gvk.String(), err))
	}
	ri := s.dyn.Resource(mapping.Resource)
	var client = ri.Namespace("")
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		ns := obj.GetNamespace()
		if ns == "" {
			ns = defaultNS
		}
		if ns == "" {
			ns = "default"
		}
		obj.SetNamespace(ns)
		r.Namespace = ns
		client = ri.Namespace(ns)
	} else {
		obj.SetNamespace("")
		client = ri
	}

	cctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	live, err := client.Get(cctx, obj.GetName(), metav1.GetOptions{})
	switch {
	case err == nil:
		r.Before = cleanYAML(live)
	case apierrors.IsNotFound(err):
	default:
		return fail(err)
	}

	data, err := json.Marshal(obj.Object)
	if err != nil {
		return fail(err)
	}
	po := metav1.PatchOptions{FieldManager: fieldManager, Force: &force}
	if dryRun {
		po.DryRun = []string{metav1.DryRunAll}
	}
	applied, err := client.Patch(cctx, obj.GetName(), types.ApplyPatchType, data, po)
	if err != nil {
		return fail(err)
	}
	r.After = cleanYAML(applied)
	switch {
	case r.Before == "":
		r.Action = "create"
	case r.Before == r.After:
		r.Action = "unchanged"
	default:
		r.Action = "configure"
	}
	return r
}

// cleanYAML renders an object without server-managed noise so diffs show real changes.
func cleanYAML(u *unstructured.Unstructured) string {
	c := u.DeepCopy()
	for _, f := range []string{"managedFields", "resourceVersion", "generation", "uid", "creationTimestamp", "selfLink"} {
		unstructured.RemoveNestedField(c.Object, "metadata", f)
	}
	unstructured.RemoveNestedField(c.Object, "status")
	out, _ := yaml.Marshal(c.Object)
	return string(bytes.TrimSpace(out))
}
