package kube

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ResourceRef builds a Resource from an explicit group/version/plural, for callers (plugins) that do not
// rely on the discovered catalog.
func ResourceRef(group, version, name string, namespaced bool) Resource {
	if group == "" {
		group = "core"
	}
	return Resource{Group: group, Version: version, Name: name, Namespaced: namespaced, Key: resourceKey(group, version, name)}
}

func scrub(o *unstructured.Unstructured) map[string]any {
	unstructured.RemoveNestedField(o.Object, "metadata", "managedFields")
	return o.Object
}

// GetObject returns one object as a plain map (managedFields removed).
func (s *Session) GetObject(ctx context.Context, r Resource, ns, name string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	o, err := s.client(r, ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return scrub(o), nil
}

// ListObjects lists up to limit objects (ns "" = all namespaces) and reports whether more existed.
func (s *Session) ListObjects(ctx context.Context, r Resource, ns string, limit int) ([]map[string]any, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*opTimeout)
	defer cancel()
	if targets := s.listTargets(ctx, r, ns); len(targets) > 1 {
		// a user who can only read some namespaces: merge them, then apply the limit
		items, err := s.ListScoped(ctx, r, ns, metav1.ListOptions{})
		if err != nil {
			return nil, false, err
		}
		more := limit > 0 && len(items) > limit
		if more {
			items = items[:limit]
		}
		out := make([]map[string]any, 0, len(items))
		for i := range items {
			out = append(out, scrub(&items[i]))
		}
		return out, more, nil
	}
	list, err := s.client(r, ns).List(ctx, metav1.ListOptions{Limit: int64(limit)})
	if err != nil {
		return nil, false, err
	}
	out := make([]map[string]any, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, scrub(&list.Items[i]))
	}
	return out, list.GetContinue() != "", nil
}
