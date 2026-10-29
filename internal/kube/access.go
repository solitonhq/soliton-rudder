package kube

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Access describes how much of a cluster the current credentials can see. Plenty of users only have access to a few
// namespaces; for them cluster-wide lists fail, so every feature has to work namespace by namespace instead.
type Access struct {
	ClusterWide       bool     `json:"clusterWide"`       // can list pods in every namespace
	CanListNamespaces bool     `json:"canListNamespaces"` // can list the Namespace objects
	Namespaces        []string `json:"namespaces"`        // what is used when ClusterWide is false
	Source            string   `json:"source"`            // cluster | configured | discovered | kubeconfig | none
	Note              string   `json:"note"`
}

// ScopeHooks supply what the cluster cannot tell us itself.
type ScopeHooks struct {
	Configured func() []string // namespaces the user chose to work with
	DefaultNS  func() string   // the namespace of the kubeconfig context
}

const (
	accessTTL          = 5 * time.Minute
	maxProbeNamespaces = 1000
	probeParallel      = 8
)

func (s *Session) SetScopeHooks(h ScopeHooks) {
	s.accMu.Lock()
	s.hooks, s.acc = h, nil
	s.accMu.Unlock()
}

// RefreshAccess forgets what was learned about permissions (after the user changed the namespaces, or got new rights).
func (s *Session) RefreshAccess() {
	s.accMu.Lock()
	s.acc = nil
	s.accMu.Unlock()
}

// Access returns the access level of the current credentials, resolved once and cached.
func (s *Session) Access(ctx context.Context) Access {
	s.accMu.Lock()
	defer s.accMu.Unlock()
	if s.acc != nil && time.Since(s.accAt) < accessTTL {
		return *s.acc
	}
	a, cacheable := s.resolveAccess(ctx)
	if cacheable {
		s.acc, s.accAt = &a, time.Now()
	}
	return a
}

// canI asks the API server whether the current user may perform verb on a resource (SelfSubjectAccessReview).
func (s *Session) canI(ctx context.Context, verb, group, resource, ns string) (bool, error) {
	body, _ := json.Marshal(map[string]any{
		"apiVersion": "authorization.k8s.io/v1", "kind": "SelfSubjectAccessReview",
		"spec": map[string]any{"resourceAttributes": map[string]any{"namespace": ns, "verb": verb, "group": group, "resource": resource}},
	})
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	raw, err := s.rc.Post().AbsPath("/apis/authorization.k8s.io/v1/selfsubjectaccessreviews").
		SetHeader("Content-Type", "application/json").Body(body).Do(cctx).Raw()
	if err != nil {
		return false, err
	}
	var r struct {
		Status struct {
			Allowed bool `json:"allowed"`
		} `json:"status"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return false, err
	}
	return r.Status.Allowed, nil
}

// probeClusterWide is the fallback when access reviews are unavailable: try the real request.
func (s *Session) probeClusterWide(ctx context.Context) (bool, error) {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := s.rc.Get().AbsPath("/api/v1/pods").Param("limit", "1").Do(cctx).Raw()
	switch {
	case err == nil:
		return true, nil
	case apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err):
		return false, nil
	}
	return false, err
}

func cleanNamespaces(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, n := range in {
		n = strings.TrimSpace(n)
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// resolveAccess works out what the credentials allow. The bool says whether the answer is certain enough to cache: when
// permissions cannot be determined (network trouble) we assume cluster-wide access, as before, and ask again next time.
func (s *Session) resolveAccess(ctx context.Context) (Access, bool) {
	wide, err := s.canI(ctx, "list", "", "pods", "")
	if err != nil {
		wide, err = s.probeClusterWide(ctx)
		if err != nil {
			return Access{ClusterWide: true, Namespaces: []string{}, Source: "cluster", Note: "Permissions could not be checked: " + shortErr(err)}, false
		}
	}
	canNS, _ := s.canI(ctx, "list", "", "namespaces", "")
	if wide {
		return Access{ClusterWide: true, CanListNamespaces: canNS, Namespaces: []string{}, Source: "cluster"}, true
	}
	a := Access{CanListNamespaces: canNS, Namespaces: []string{}, Source: "none"}
	if s.hooks.Configured != nil {
		if ns := cleanNamespaces(s.hooks.Configured()); len(ns) > 0 {
			a.Namespaces, a.Source = ns, "configured"
			return a, true
		}
	}
	if canNS {
		if ns := s.discoverNamespaces(ctx); len(ns) > 0 {
			a.Namespaces, a.Source = ns, "discovered"
			return a, true
		}
	}
	if s.hooks.DefaultNS != nil {
		if ns := strings.TrimSpace(s.hooks.DefaultNS()); ns != "" {
			a.Namespaces, a.Source = []string{ns}, "kubeconfig"
			return a, true
		}
	}
	a.Note = "No namespace could be found that these credentials can read. Choose the namespaces to work with in the cluster's access settings."
	return a, true
}

// discoverNamespaces lists the namespaces in which pods can be listed.
func (s *Session) discoverNamespaces(ctx context.Context) []string {
	names, err := s.Namespaces(ctx)
	if err != nil {
		return nil
	}
	if len(names) > maxProbeNamespaces {
		names = names[:maxProbeNamespaces]
	}
	allowed := make([]bool, len(names))
	sem := make(chan struct{}, probeParallel)
	var wg sync.WaitGroup
	for i, n := range names {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, n string) {
			defer wg.Done()
			defer func() { <-sem }()
			ok, err := s.canI(ctx, "list", "", "pods", n)
			allowed[i] = err == nil && ok
		}(i, n)
	}
	wg.Wait()
	out := []string{}
	for i, n := range names {
		if allowed[i] {
			out = append(out, n)
		}
	}
	return out
}

// listTargets returns the namespace arguments to query for a resource: "" for one cluster-wide request, otherwise the
// namespaces the user can access.
func (s *Session) listTargets(ctx context.Context, r Resource, ns string) []string {
	if ns != "" || !r.Namespaced {
		return []string{ns}
	}
	a := s.Access(ctx)
	if a.ClusterWide || len(a.Namespaces) == 0 {
		return []string{""}
	}
	return a.Namespaces
}

// ListScoped lists a resource like a cluster-wide List would, but works for users who can only read some namespaces:
// the namespaces are queried in parallel and merged, and a namespace that turns out to be forbidden is skipped.
func (s *Session) ListScoped(ctx context.Context, r Resource, ns string, opts metav1.ListOptions) ([]unstructured.Unstructured, error) {
	return s.listScoped(ctx, r, ns, opts, false)
}

// ListScopedStrict is ListScoped for callers that treat a missing object as a fact (the blackbox recorder): besides
// forbidden namespaces, any other failure of one namespace fails the whole list instead of leaving a silent hole.
func (s *Session) ListScopedStrict(ctx context.Context, r Resource, ns string, opts metav1.ListOptions) ([]unstructured.Unstructured, error) {
	return s.listScoped(ctx, r, ns, opts, true)
}

func (s *Session) listScoped(ctx context.Context, r Resource, ns string, opts metav1.ListOptions, strict bool) ([]unstructured.Unstructured, error) {
	targets := s.listTargets(ctx, r, ns)
	if len(targets) == 1 {
		l, err := s.client(r, targets[0]).List(ctx, opts)
		if err != nil {
			return nil, err
		}
		return l.Items, nil
	}
	type result struct {
		items []unstructured.Unstructured
		err   error
	}
	results := make([]result, len(targets))
	sem := make(chan struct{}, probeParallel)
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, t string) {
			defer wg.Done()
			defer func() { <-sem }()
			l, err := s.client(r, t).List(ctx, opts)
			if err != nil {
				results[i].err = err
				return
			}
			results[i].items = l.Items
		}(i, t)
	}
	wg.Wait()
	var out []unstructured.Unstructured
	var firstErr error
	ok := 0
	for _, r := range results {
		if r.err != nil {
			if strict && !apierrors.IsForbidden(r.err) {
				return nil, r.err
			}
			if firstErr == nil || (apierrors.IsForbidden(firstErr) && !apierrors.IsForbidden(r.err)) {
				firstErr = r.err
			}
			continue
		}
		ok++
		out = append(out, r.items...)
	}
	if ok == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}
