package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

// Session is a live connection to one cluster.
type Session struct {
	clusterID string
	cfg       *rest.Config
	rc        rest.Interface
	dyn       dynamic.Interface
	disc      discovery.DiscoveryInterface
	emit      Emitter

	mu      sync.RWMutex
	catalog map[string]Resource
	subs    map[string]context.CancelFunc

	accMu sync.Mutex // guards acc, accAt, hooks (see access.go)
	acc   *Access
	accAt time.Time
	hooks ScopeHooks

	ctx          context.Context // cancelled by Close; bounds background samplers
	cancel       context.CancelFunc
	scopes       map[string]*metricScope
	metricsEvery time.Duration
}

var subSeq atomic.Uint64

func NewSession(clusterID string, cfg *rest.Config, emit Emitter) (*Session, error) {
	c := rest.CopyConfig(cfg)
	c.QPS, c.Burst = 50, 100
	c.Timeout = 0 // watches are long-lived; per-call contexts bound everything else
	c.UserAgent = "soliton-rudder"

	rcfg := rest.CopyConfig(c)
	rcfg.APIPath = ""
	rcfg.GroupVersion = nil
	rcfg.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	rc, err := rest.UnversionedRESTClientFor(rcfg)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(c)
	if err != nil {
		return nil, err
	}
	disc, err := discovery.NewDiscoveryClientForConfig(c)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{
		ctx: ctx, cancel: cancel, scopes: map[string]*metricScope{}, metricsEvery: 15 * time.Second,
		clusterID: clusterID, cfg: c, rc: rc, dyn: dyn, disc: disc, emit: emit,
		catalog: map[string]Resource{}, subs: map[string]context.CancelFunc{},
	}, nil
}

// Discover refreshes and returns the resource catalog (sorted by kind).
func (s *Session) Discover(ctx context.Context) ([]Resource, error) {
	type result struct {
		lists []*metaAPIResourceList
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		l, err := discoverPreferred(s.disc)
		ch <- result{l, err}
	}()
	var r result
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r = <-ch:
	}
	// Partial discovery failures (e.g. an unavailable aggregated API) are tolerated.
	if len(r.lists) == 0 && r.err != nil {
		return nil, r.err
	}
	out := flattenResources(r.lists)
	s.mu.Lock()
	s.catalog = make(map[string]Resource, len(out))
	for _, res := range out {
		s.catalog[res.Key] = res
	}
	s.mu.Unlock()
	return out, nil
}

func (s *Session) hasCatalog() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.catalog) > 0
}

func (s *Session) resource(key string) (Resource, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.catalog[key]
	if !ok {
		return Resource{}, fmt.Errorf("unknown resource %q", key)
	}
	return r, nil
}

// Subscribe starts a list+watch stream for a resource and returns its id.
func (s *Session) Subscribe(key string) (string, error) {
	res, err := s.resource(key)
	if err != nil {
		return "", err
	}
	id := fmt.Sprintf("%s-%d", s.clusterID, subSeq.Add(1))
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.subs[id] = cancel
	s.mu.Unlock()
	go func() {
		s.runStream(ctx, id, res)
		s.mu.Lock()
		delete(s.subs, id)
		s.mu.Unlock()
	}()
	return id, nil
}

func (s *Session) Unsubscribe(id string) {
	s.mu.Lock()
	cancel := s.subs[id]
	delete(s.subs, id)
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Close stops every stream.
func (s *Session) Close() {
	s.cancel()
	s.mu.Lock()
	subs := s.subs
	s.subs = map[string]context.CancelFunc{}
	s.mu.Unlock()
	for _, cancel := range subs {
		cancel()
	}
}

func resourceKey(group, version, name string) string {
	if group == "" {
		group = "core"
	}
	return group + "/" + version + "/" + name
}

func apiPath(r Resource) string {
	if r.Group == "" || r.Group == "core" {
		return "/api/" + r.Version + "/" + r.Name
	}
	return "/apis/" + r.Group + "/" + r.Version + "/" + r.Name
}

// apiPathNS is apiPath for one namespace of a namespaced resource ("" = the whole cluster).
func apiPathNS(r Resource, ns string) string {
	if ns == "" || !r.Namespaced {
		return apiPath(r)
	}
	if r.Group == "" || r.Group == "core" {
		return "/api/" + r.Version + "/namespaces/" + ns + "/" + r.Name
	}
	return "/apis/" + r.Group + "/" + r.Version + "/namespaces/" + ns + "/" + r.Name
}

func hasVerb(verbs []string, v string) bool {
	for _, x := range verbs {
		if x == v {
			return true
		}
	}
	return false
}

func sortResources(rs []Resource) {
	sort.Slice(rs, func(i, j int) bool {
		if rs[i].Kind != rs[j].Kind {
			return strings.ToLower(rs[i].Kind) < strings.ToLower(rs[j].Kind)
		}
		return rs[i].Key < rs[j].Key
	})
}

// HasResource reports whether the cluster serves a resource (by key), per the last discovery.
func (s *Session) HasResource(key string) (Resource, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.catalog[key]
	return r, ok
}
