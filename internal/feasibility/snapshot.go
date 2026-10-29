package feasibility

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"soliton-rudder/internal/kube"
)

// Snapshot is a point-in-time copy of the cluster state that rules evaluate.
// Rules never talk to the API server, which keeps them fast and unit-testable.
type Snapshot struct {
	ServerVersion string
	Lists         map[string][]unstructured.Unstructured
	Missing       map[string]string     // list name -> error, for kinds that could not be read
	Usage         map[string]kube.Usage // metrics-server, current sample, key ns/pod
	UsageErr      string
	PromUsage     map[string]kube.Usage // Prometheus p95 over the window, key ns/pod (nil when unavailable)
	PromNote      string
	Cfg           Config

	wl        []workload
	prodCache map[string]bool
	nsLabels  map[string]map[string]string
}

type listSpec struct {
	name, group, version, resource string
	namespaced                     bool
	required                       bool
}

var listSpecs = []listSpec{
	{"nodes", "core", "v1", "nodes", false, true},
	{"pods", "core", "v1", "pods", true, true},
	{"namespaces", "core", "v1", "namespaces", false, false},
	{"deployments", "apps", "v1", "deployments", true, false},
	{"statefulsets", "apps", "v1", "statefulsets", true, false},
	{"daemonsets", "apps", "v1", "daemonsets", true, false},
	{"replicasets", "apps", "v1", "replicasets", true, false},
	{"jobs", "batch", "v1", "jobs", true, false},
	{"cronjobs", "batch", "v1", "cronjobs", true, false},
	{"services", "core", "v1", "services", true, false},
	{"persistentvolumeclaims", "core", "v1", "persistentvolumeclaims", true, false},
	{"persistentvolumes", "core", "v1", "persistentvolumes", false, false},
	{"horizontalpodautoscalers", "autoscaling", "v2", "horizontalpodautoscalers", true, false},
	{"poddisruptionbudgets", "policy", "v1", "poddisruptionbudgets", true, false},
	{"ingresses", "networking.k8s.io", "v1", "ingresses", true, false},
	{"networkpolicies", "networking.k8s.io", "v1", "networkpolicies", true, false},
	{"tlssecrets", "core", "v1", "secrets", true, false},
}

// listSelectors narrows specific lists server-side (TLS secrets only, so other Secrets are never fetched).
var listSelectors = map[string]string{"tlssecrets": "type=kubernetes.io/tls"}

// Collect reads everything the rules need. Optional kinds that fail are recorded in Missing
// (rules depending on them are skipped rather than reporting a false "all clear").
// CollectOptions adds optional data sources to a collection.
type CollectOptions struct {
	Prom        *kube.PromConfig // when set, 95th-percentile usage is read from Prometheus
	UsageWindow time.Duration
}

func Collect(ctx context.Context, s *kube.Session, progress func(phase string, pct int)) (*Snapshot, error) {
	return CollectWith(ctx, s, progress, CollectOptions{})
}

func CollectWith(ctx context.Context, s *kube.Session, progress func(phase string, pct int), opts CollectOptions) (*Snapshot, error) {
	snap := &Snapshot{Lists: map[string][]unstructured.Unstructured{}, Missing: map[string]string{}}
	if v, err := s.ServerVersion(ctx); err == nil {
		snap.ServerVersion = v
	}
	var (
		mu    sync.Mutex
		wg    sync.WaitGroup
		done  int
		sem   = make(chan struct{}, 4)
		fatal error
	)
	for _, sp := range listSpecs {
		wg.Add(1)
		go func(sp listSpec) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			items, err := s.ListRawSelector(ctx, sp.group, sp.version, sp.resource, sp.namespaced, "", listSelectors[sp.name])
			if sp.name == "tlssecrets" {
				kube.StripPrivateKeys(items) // only certificates are needed; drop private keys immediately
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if sp.required && fatal == nil {
					fatal = fmt.Errorf("cannot list %s: %w", sp.name, err)
				}
				snap.Missing[sp.name] = err.Error()
			} else {
				snap.Lists[sp.name] = items
			}
			done++
			progress("Reading "+sp.name, 5+done*80/len(listSpecs))
		}(sp)
	}
	wg.Wait()
	if fatal != nil {
		return nil, fatal
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	progress("Reading metrics", 88)
	if u, err := s.UsageMap(ctx, "pods", ""); err == nil {
		snap.Usage = u
	} else {
		snap.UsageErr = err.Error()
	}
	if opts.Prom != nil {
		progress("Reading Prometheus usage history", 90)
		w := opts.UsageWindow
		if w <= 0 {
			w = 24 * time.Hour
		}
		if p95, err := s.PodUsageP95(ctx, *opts.Prom, w); err == nil {
			snap.PromUsage = p95
		} else {
			snap.PromNote = "Prometheus usage history unavailable (" + shorten(err.Error(), 140) + "); using metrics-server."
		}
	}
	return snap, nil
}

func (s *Snapshot) list(name string) []unstructured.Unstructured { return s.Lists[name] }

// ---- workloads (templates), so hygiene rules report once per Deployment, not once per replica ----

type workload struct {
	Kind, ResourceKey, Namespace, Name string
	Spec                               map[string]any // pod spec
	Labels                             map[string]string
	Replicas                           int64
	HasReplicas                        bool
	Obj                                *unstructured.Unstructured
}

func (s *Snapshot) workloads() []workload {
	if s.wl != nil {
		return s.wl
	}
	add := func(list, kind, key string, path ...string) {
		items := s.Lists[list]
		for i := range items {
			o := &items[i]
			spec, ok, _ := unstructured.NestedMap(o.Object, path...)
			if !ok {
				continue
			}
			w := workload{Kind: kind, ResourceKey: key, Namespace: o.GetNamespace(), Name: o.GetName(), Spec: spec, Obj: o}
			if len(path) > 1 && path[len(path)-2] == "template" {
				tp := append(append([]string{}, path[:len(path)-1]...), "metadata", "labels")
				w.Labels, _, _ = unstructured.NestedStringMap(o.Object, tp...)
			}
			if r, found, _ := unstructured.NestedInt64(o.Object, "spec", "replicas"); found {
				w.Replicas, w.HasReplicas = r, true
			}
			s.wl = append(s.wl, w)
		}
	}
	s.wl = []workload{}
	add("deployments", "Deployment", "apps/v1/deployments", "spec", "template", "spec")
	add("statefulsets", "StatefulSet", "apps/v1/statefulsets", "spec", "template", "spec")
	add("daemonsets", "DaemonSet", "apps/v1/daemonsets", "spec", "template", "spec")
	add("cronjobs", "CronJob", "batch/v1/cronjobs", "spec", "jobTemplate", "spec", "template", "spec")
	// Jobs not owned by a CronJob, and bare pods (no controller).
	for _, kind := range []struct{ list, kind, key string }{{"jobs", "Job", "batch/v1/jobs"}, {"pods", "Pod", "core/v1/pods"}} {
		items := s.Lists[kind.list]
		for i := range items {
			o := &items[i]
			if hasOwner(o) {
				continue
			}
			path := []string{"spec", "template", "spec"}
			if kind.kind == "Pod" {
				path = []string{"spec"}
			}
			spec, ok, _ := unstructured.NestedMap(o.Object, path...)
			if !ok {
				continue
			}
			w := workload{Kind: kind.kind, ResourceKey: kind.key, Namespace: o.GetNamespace(), Name: o.GetName(), Spec: spec, Obj: o}
			if kind.kind == "Pod" {
				w.Labels = o.GetLabels()
			}
			s.wl = append(s.wl, w)
		}
	}
	return s.wl
}

func hasOwner(o *unstructured.Unstructured) bool { return len(o.GetOwnerReferences()) > 0 }

func isSystemNS(ns string) bool { return strings.HasPrefix(ns, "kube-") }

// containers returns the main containers (and init containers when withInit).
func containers(spec map[string]any, withInit bool) []map[string]any {
	var out []map[string]any
	fields := []string{"containers"}
	if withInit {
		fields = append(fields, "initContainers")
	}
	for _, f := range fields {
		list, _, _ := unstructured.NestedSlice(spec, f)
		for _, c := range list {
			if m, ok := c.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

func cName(c map[string]any) string { s, _ := c["name"].(string); return s }

func qty(m map[string]any, fields ...string) (resource.Quantity, bool) {
	v, ok, _ := unstructured.NestedString(m, fields...)
	if !ok {
		return resource.Quantity{}, false
	}
	q, err := resource.ParseQuantity(v)
	return q, err == nil
}

func finding(sev Severity, kind, key, ns, name, msg string) Finding {
	return Finding{Severity: sev, Kind: kind, ResourceKey: key, Namespace: ns, Name: name, Message: msg}
}

func (w workload) finding(sev Severity, msg string) Finding {
	return finding(sev, w.Kind, w.ResourceKey, w.Namespace, w.Name, msg)
}

func podFinding(sev Severity, p *unstructured.Unstructured, msg string) Finding {
	return finding(sev, "Pod", "core/v1/pods", p.GetNamespace(), p.GetName(), msg)
}

func objFinding(sev Severity, kind, key string, o *unstructured.Unstructured, msg string) Finding {
	return finding(sev, kind, key, o.GetNamespace(), o.GetName(), msg)
}
