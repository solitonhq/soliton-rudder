package kube

import (
	"context"
	"regexp"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// relatedSet is an object and everything it owns, resolved through ownerReferences (Deployment -> ReplicaSets -> Pods,
// CronJob -> Jobs -> Pods, ...). It decides which Events belong in an object's event list.
type relatedSet struct {
	uids    map[string]bool
	objects map[string]bool // "Kind/name"
	// Pods that are already gone still have Events for a while; they are recognised by their parent's name.
	podPrefixes []string
	podOrdinal  *regexp.Regexp // StatefulSet pods are named <set>-<ordinal>
}

func (r *relatedSet) add(o unstructured.Unstructured) {
	if uid := string(o.GetUID()); uid != "" {
		r.uids[uid] = true
	}
	r.objects[o.GetKind()+"/"+o.GetName()] = true
	switch o.GetKind() {
	case "ReplicaSet", "Job":
		r.podPrefixes = append(r.podPrefixes, o.GetName()+"-")
	case "StatefulSet":
		r.podOrdinal = regexp.MustCompile(`^` + regexp.QuoteMeta(o.GetName()) + `-\d+$`)
	}
}

// relatedTo resolves root and the objects among candidates that it owns, directly or through other candidates.
func relatedTo(root unstructured.Unstructured, candidates []unstructured.Unstructured) relatedSet {
	r := relatedSet{uids: map[string]bool{}, objects: map[string]bool{}}
	r.add(root)
	taken := make([]bool, len(candidates))
	// repeat until stable, so the order of candidates does not matter
	for changed := true; changed; {
		changed = false
		for i, c := range candidates {
			if taken[i] {
				continue
			}
			for _, o := range c.GetOwnerReferences() {
				if r.uids[string(o.UID)] {
					r.add(c)
					taken[i] = true
					changed = true
					break
				}
			}
		}
	}
	return r
}

// matches reports whether an Event is about the object or something it owns.
func (r relatedSet) matches(ev unstructured.Unstructured) bool {
	io, _, _ := unstructured.NestedMap(ev.Object, "involvedObject")
	if uid := str(io["uid"]); uid != "" && r.uids[uid] {
		return true
	}
	kind, name := str(io["kind"]), str(io["name"])
	if r.objects[kind+"/"+name] {
		return true
	}
	if kind == "Pod" {
		for _, p := range r.podPrefixes {
			if strings.HasPrefix(name, p) {
				return true
			}
		}
		if r.podOrdinal != nil && r.podOrdinal.MatchString(name) {
			return true
		}
	}
	return false
}

// relatedObjects loads a workload and the pods, replica sets and jobs of its namespace and resolves what it owns.
// A list the user may not read is skipped: the result is then smaller, never an error.
func (s *Session) relatedObjects(ctx context.Context, res Resource, ns, name string) (relatedSet, error) {
	root, err := s.client(res, ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return relatedSet{}, err
	}
	var candidates []unstructured.Unstructured
	for _, r := range []Resource{
		{Group: "core", Version: "v1", Name: "pods", Namespaced: true},
		{Group: "apps", Version: "v1", Name: "replicasets", Namespaced: true},
		{Group: "batch", Version: "v1", Name: "jobs", Namespaced: true},
	} {
		if list, err := s.client(r, ns).List(ctx, metav1.ListOptions{}); err == nil {
			candidates = append(candidates, list.Items...)
		}
	}
	return relatedTo(*root, candidates), nil
}
