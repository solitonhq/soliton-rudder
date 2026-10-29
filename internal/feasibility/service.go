package feasibility

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"soliton-rudder/internal/kube"
)

type SessionFunc func(clusterID string) (*kube.Session, error)

// Service runs assessments in the background and exposes history.
type Service struct {
	store   *Store
	session SessionFunc
	name    func(clusterID string) string
	emit    func(string, any)
	extra   func(ctx context.Context, clusterID string) []RuleResult
	config  func() Config
	prom    func(clusterID string) (kube.PromConfig, bool)

	mu      sync.Mutex
	running map[string]*runState // by cluster id
}

type runState struct {
	id     string
	cancel context.CancelFunc
}

func NewService(store *Store, session SessionFunc, clusterName func(string) string, emit func(string, any)) *Service {
	return &Service{store: store, session: session, name: clusterName, emit: emit, running: map[string]*runState{}}
}

// SetExtraRules registers a provider of additional rule results (plugins) evaluated on every run.
func (s *Service) SetExtraRules(f func(ctx context.Context, clusterID string) []RuleResult) {
	s.extra = f
}

// SetConfigProvider supplies the Doctor configuration (prices, production namespaces) read at the start of each run.
func (s *Service) SetConfigProvider(f func() Config) { s.config = f }

// SetPromProvider tells the service where a cluster's Prometheus is, if configured, for usage history.
func (s *Service) SetPromProvider(f func(clusterID string) (kube.PromConfig, bool)) { s.prom = f }

// Start begins an assessment and returns immediately with its run id.
func (s *Service) Start(clusterID string) (string, error) {
	sess, err := s.session(clusterID)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	if cur, ok := s.running[clusterID]; ok {
		s.mu.Unlock()
		return cur.id, errors.New("an assessment is already running for this cluster")
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &runState{id: uuid.NewString(), cancel: cancel}
	s.running[clusterID] = run
	s.mu.Unlock()

	go func() {
		rep, err := s.safeExecute(ctx, sess, clusterID, run.id)
		s.mu.Lock()
		delete(s.running, clusterID)
		s.mu.Unlock()
		// read this BEFORE cancel(): afterwards ctx.Err() is always set and every run would look cancelled
		wasCancelled := ctx.Err() != nil
		cancel()
		d := Done{Run: run.id}
		switch {
		case wasCancelled:
			d.Error = "cancelled"
		case err != nil:
			d.Error = err.Error()
		default:
			d.Report = rep.ID
		}
		s.emit(EventDone, d)
	}()
	return run.id, nil
}

// safeExecute turns a panic anywhere in a run into an error shown to the user instead of closing the app.
func (s *Service) safeExecute(ctx context.Context, sess *kube.Session, clusterID, runID string) (rep Report, err error) {
	defer func() {
		if p := recover(); p != nil {
			rep, err = Report{}, fmt.Errorf("the assessment hit an internal error: %v", p)
		}
	}()
	return s.execute(ctx, sess, clusterID, runID)
}

func (s *Service) execute(ctx context.Context, sess *kube.Session, clusterID, runID string) (Report, error) {
	start := time.Now()
	progress := func(phase string, pct int) { s.emit(EventProgress, Progress{Run: runID, Phase: phase, Pct: pct}) }
	progress("Starting", 1)
	var opts CollectOptions
	if s.prom != nil {
		if pc, ok := s.prom(clusterID); ok {
			opts.Prom = &pc
		}
	}
	snap, err := CollectWith(ctx, sess, progress, opts)
	if err != nil {
		return Report{}, err
	}
	progress("Evaluating rules", 92)
	cfg := DefaultConfig()
	if s.config != nil {
		cfg = s.config()
	}
	rep := EvaluateWith(snap, cfg)
	if s.extra != nil {
		progress("Running plugin rules", 94)
		AppendRules(&rep, s.extra(ctx, clusterID))
	}
	rep.ID, rep.ClusterID, rep.ClusterName = runID, clusterID, s.name(clusterID)
	rep.StartedAt, rep.DurationMs = start.Unix(), time.Since(start).Milliseconds()
	progress("Saving", 97)
	if err := s.store.Save(rep); err != nil {
		return Report{}, err
	}
	progress("Done", 100)
	return rep, nil
}

// Cancel stops the running assessment of a cluster, if any.
func (s *Service) Cancel(clusterID string) {
	s.mu.Lock()
	run := s.running[clusterID]
	s.mu.Unlock()
	if run != nil {
		run.cancel()
	}
}

// Running returns the id of the cluster's in-flight run ("" if none).
func (s *Service) Running(clusterID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.running[clusterID]; r != nil {
		return r.id
	}
	return ""
}

func (s *Service) List(clusterID string) ([]Summary, error) { return s.store.List(clusterID) }
func (s *Service) Get(id string) (Report, error)            { return s.store.Get(id) }
func (s *Service) Delete(id string) error                   { return s.store.Delete(id) }
func (s *Service) DeleteCluster(clusterID string) {
	s.Cancel(clusterID)
	_ = s.store.DeleteCluster(clusterID)
}

func (s *Service) Compare(fromID, toID string) (Diff, error) {
	a, err := s.store.Get(fromID)
	if err != nil {
		return Diff{}, err
	}
	b, err := s.store.Get(toID)
	if err != nil {
		return Diff{}, err
	}
	if a.StartedAt > b.StartedAt {
		a, b = b, a
	}
	return Compare(a, b), nil
}

func (s *Service) ClearAll() error { return s.store.DeleteAll() }

func (s *Service) Close() error { return s.store.Close() }

// Fixes proposes changes for a finding, computed from the live object (and observed usage where sizing matters).
// A finding on a pod whose controller can be fixed is redirected to that controller, since pods are immutable.
func (s *Service) Fixes(ctx context.Context, clusterID, ruleID, key, ns, name string) ([]Fix, error) {
	if !isFixable(ruleID) {
		return nil, fmt.Errorf("no automatic fix is available for this check")
	}
	sess, err := s.session(clusterID)
	if err != nil {
		return nil, err
	}
	obj, err := s.fetchObject(ctx, sess, key, ns, name)
	if err != nil {
		return nil, err
	}
	workloadKey := key
	if obj.GetKind() == "Pod" && ruleID != "failed-pods" {
		if ctrl, ckey, err := s.controllerOf(ctx, sess, obj); err == nil && ctrl != nil {
			obj, workloadKey = ctrl, ckey
		}
	}
	cfg := DefaultConfig()
	if s.config != nil {
		cfg = s.config()
	}
	fc := FixContext{Cfg: cfg.Normalize()}
	needsUsage := map[string]bool{"no-requests": true, "no-memory-limit": true, "overprovisioned": true, RightSizeRuleID: true}
	if needsUsage[ruleID] && obj.GetKind() != "Pod" {
		var pc *kube.PromConfig
		if s.prom != nil {
			if c, ok := s.prom(clusterID); ok {
				pc = &c
			}
		}
		if u, src, err := sess.ContainerUsage(ctx, workloadKey, obj.GetNamespace(), obj.GetName(), pc, 24*time.Hour); err == nil {
			fc.Usage, fc.UsageSource = u, src
		} else if ruleID == RightSizeRuleID || ruleID == "overprovisioned" {
			return nil, fmt.Errorf("right-sizing needs usage data: %w", err)
		}
	}
	fixes := ProposeFixes(ruleID, obj, fc)
	if fixes == nil {
		fixes = []Fix{}
	}
	return fixes, nil
}

func (s *Service) fetchObject(ctx context.Context, sess *kube.Session, key, ns, name string) (*unstructured.Unstructured, error) {
	parts := strings.Split(key, "/")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid resource key %q", key)
	}
	ref := kube.ResourceRef(parts[0], parts[1], parts[2], parts[2] != "namespaces")
	m, err := sess.GetObject(ctx, ref, ns, name)
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: m}, nil
}

// controllerOf returns the Deployment/StatefulSet/DaemonSet/CronJob-less owner of a pod (following ReplicaSet → Deployment).
func (s *Service) controllerOf(ctx context.Context, sess *kube.Session, pod *unstructured.Unstructured) (*unstructured.Unstructured, string, error) {
	for _, ref := range pod.GetOwnerReferences() {
		kind, name := ref.Kind, ref.Name
		if kind == "ReplicaSet" {
			rs, err := s.fetchObject(ctx, sess, "apps/v1/replicasets", pod.GetNamespace(), name)
			if err != nil {
				return nil, "", err
			}
			for _, r2 := range rs.GetOwnerReferences() {
				if r2.Kind == "Deployment" {
					kind, name = "Deployment", r2.Name
				}
			}
		}
		key := map[string]string{"Deployment": "apps/v1/deployments", "StatefulSet": "apps/v1/statefulsets", "DaemonSet": "apps/v1/daemonsets"}[kind]
		if key == "" {
			continue
		}
		o, err := s.fetchObject(ctx, sess, key, pod.GetNamespace(), name)
		if err != nil {
			return nil, "", err
		}
		return o, key, nil
	}
	return nil, "", nil
}
