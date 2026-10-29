package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"k8s.io/client-go/rest"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"soliton-rudder/internal/appinfo"
	"soliton-rudder/internal/blackbox"
	"soliton-rudder/internal/cluster"
	"soliton-rudder/internal/feasibility"
	"soliton-rudder/internal/helmcli"
	"soliton-rudder/internal/integrations"
	"soliton-rudder/internal/kube"
	"soliton-rudder/internal/plugins"
	"soliton-rudder/internal/portfwd"
	"soliton-rudder/internal/settings"
	"soliton-rudder/internal/shellenv"
	"soliton-rudder/internal/term"
)

// App is the Wails-bound facade exposed to the frontend.
type App struct {
	ctx      context.Context
	settings *settings.Store
	clusters *cluster.Service
	dbPath   string
	kube     *kube.Manager
	terms    *term.Hub
	forwards *portfwd.Manager
	reports  *feasibility.Service
	integr   *integrations.Store
	plugins  *plugins.Manager
	bb       *blackbox.Store
	bbm      *blackbox.Manager
	bbMu     sync.Mutex
}

func NewApp(s *settings.Store, c *cluster.Service, dbPath string) (*App, error) {
	a := &App{settings: s, clusters: c, dbPath: dbPath}
	emit := func(event string, data any) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, event, data)
		}
	}
	a.kube = kube.NewManager(func(id string) (*rest.Config, error) {
		shellenv.Wait() // exec credential plugins (aws, gcloud…) need the login-shell PATH
		return c.RestConfig(id)
	}, emit)
	a.kube.SetScopeProvider(func(id string) kube.ScopeHooks {
		return kube.ScopeHooks{
			Configured: func() []string { return a.accessNamespaces(id) },
			DefaultNS:  func() string { return c.DefaultNamespace(id) },
		}
	})
	a.terms = term.NewHub(emit)
	pf, err := portfwd.NewManager(dbPath, a.kube.Ensure, emit)
	if err != nil {
		return nil, err
	}
	a.forwards = pf
	fstore, err := feasibility.OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	istore, err := integrations.Open(dbPath)
	if err != nil {
		return nil, err
	}
	a.integr = istore
	a.reports = feasibility.NewService(fstore, a.kube.Ensure, func(id string) string {
		if c, err := c.Get(id); err == nil {
			return c.Name
		}
		return id
	}, emit)
	if err := a.initPlugins(filepath.Dir(dbPath)); err != nil {
		return nil, err
	}
	bb, err := blackbox.Open(dbPath)
	if err != nil {
		return nil, err
	}
	a.bb = bb
	a.bbm = blackbox.NewManager(bb, func(ctx context.Context, id string, ns []string) (kube.Observation, error) {
		s, err := a.kube.EnsureBackground(id) // never reopens a cluster the user disconnected
		if err != nil {
			return kube.Observation{}, err
		}
		return s.Observe(ctx, ns)
	})
	return a, nil
}

func (a *App) shutdown(context.Context) {
	a.bbm.Stop()
	_ = a.bb.Close()
	a.plugins.Shutdown()
	a.terms.CloseAll()
	a.forwards.StopAll()
	_ = a.reports.Close()
	_ = a.integr.Close()
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.plugins.Start()
	a.bbm.Apply(a.blackboxConfig())
	a.autoCheckUpdates()
}

func (a *App) GetVersion() string { return appinfo.Version }

func (a *App) GetSettings() settings.Settings { return a.settings.Get() }

func (a *App) SetTheme(t settings.Theme) error { return a.settings.SetTheme(t) }

// ---- Clusters ----

func (a *App) ListClusters() ([]cluster.Cluster, error) { return a.clusters.List() }

func (a *App) DefaultKubeconfigPath() string { return cluster.DefaultKubeconfig() }

// PickKubeconfigFile opens a native file dialog; returns "" if cancelled.
func (a *App) PickKubeconfigFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:                      "Select kubeconfig",
		DefaultDirectory:           filepath.Dir(cluster.DefaultKubeconfig()),
		ShowHiddenFiles:            true,
		TreatPackagesAsDirectories: true,
	})
}

func (a *App) PreviewKubeconfigFile(path string) ([]cluster.ContextInfo, error) {
	return a.clusters.PreviewFile(path)
}

func (a *App) PreviewKubeconfigText(text string) ([]cluster.ContextInfo, error) {
	return a.clusters.PreviewText(text)
}

func (a *App) ImportKubeconfigFile(path string, contexts []string) ([]cluster.Cluster, error) {
	return a.clusters.ImportFile(path, contexts)
}

func (a *App) ImportKubeconfigText(text string, contexts []string) ([]cluster.Cluster, error) {
	return a.clusters.ImportText(text, contexts)
}

func (a *App) UpdateCluster(id string, patch cluster.Patch) (cluster.Cluster, error) {
	return a.clusters.Update(id, patch)
}

func (a *App) DeleteCluster(id string) error {
	a.forgetBlackbox(id)
	a.forwards.RemoveCluster(id)
	a.reports.DeleteCluster(id)
	_ = a.integr.DeleteCluster(id)
	a.kube.Disconnect(id)
	return a.clusters.Delete(id)
}

func (a *App) ReorderClusters(ids []string) error { return a.clusters.Reorder(ids) }

func (a *App) CheckCluster(id string) cluster.Health { return a.clusters.Check(a.ctx, id) }

// ---- Live resources ----

// ConnectCluster opens (or reuses) a session and returns the discovered resource catalog.
func (a *App) ConnectCluster(id string) ([]kube.Resource, error) {
	res, err := a.kube.Connect(a.ctx, id)
	if err == nil {
		a.forwards.StartAuto(id)
	}
	return res, err
}

// GetConnected lists the clusters that currently have a live connection.
func (a *App) GetConnected() []string { return a.kube.Connected() }

func (a *App) DisconnectCluster(id string) {
	a.forwards.StopCluster(id)
	a.kube.Disconnect(id)
}

// SubscribeResource starts a live table stream; rows arrive via "rows"/"rowstatus" events.
func (a *App) SubscribeResource(clusterID, key string) (string, error) {
	return a.kube.Subscribe(clusterID, key)
}

func (a *App) UnsubscribeResource(subID string) { a.kube.Unsubscribe(subID) }

func (a *App) withSession(id string) (*kube.Session, error) { return a.kube.Session(id) }

func (a *App) GetResourceYAML(clusterID, key, ns, name string) (string, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return "", err
	}
	return s.GetYAML(a.ctx, key, ns, name)
}

func (a *App) UpdateResourceYAML(clusterID, key, ns, name, doc string) (string, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return "", err
	}
	return s.UpdateYAML(a.ctx, key, ns, name, doc)
}

func (a *App) DeleteResource(clusterID, key, ns, name string) error {
	s, err := a.withSession(clusterID)
	if err != nil {
		return err
	}
	return s.Delete(a.ctx, key, ns, name)
}

func (a *App) ScaleResource(clusterID, key, ns, name string, replicas int) error {
	s, err := a.withSession(clusterID)
	if err != nil {
		return err
	}
	return s.Scale(a.ctx, key, ns, name, int32(replicas))
}

func (a *App) RestartResource(clusterID, key, ns, name string) error {
	s, err := a.withSession(clusterID)
	if err != nil {
		return err
	}
	return s.Restart(a.ctx, key, ns, name)
}

func (a *App) GetResourceEvents(clusterID, key, ns, name string) ([]kube.Event, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return nil, err
	}
	return s.Events(a.ctx, key, ns, name)
}

func (a *App) GetResourceMeta(clusterID, key, ns, name string) (kube.Meta, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return kube.Meta{}, err
	}
	return s.Meta(a.ctx, key, ns, name)
}

// ---- Terminals ----

// OpenPodTerminal execs an interactive shell in a pod container.
func (a *App) OpenPodTerminal(clusterID, ns, pod, container string, cols, rows int) (string, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return "", err
	}
	return a.terms.StartRemote(term.Size{Cols: uint16(cols), Rows: uint16(rows)}, s.ExecRun(ns, pod, container, nil)), nil
}

// OpenLocalTerminal starts the user's shell; with a clusterID, KUBECONFIG points at that cluster.
func (a *App) OpenLocalTerminal(clusterID string, cols, rows int) (string, error) {
	shellenv.Wait()
	cmd := exec.Command(term.UserShell(), "-l")
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home
	}
	var cleanup func()
	if clusterID != "" {
		path, c, err := a.clusters.TempKubeconfig(clusterID)
		if err != nil {
			return "", err
		}
		cleanup = c
		cmd.Env = append(cmd.Env, "KUBECONFIG="+path)
	}
	return a.terms.StartLocal(cmd, term.Size{Cols: uint16(cols), Rows: uint16(rows)}, cleanup)
}

func (a *App) TerminalWrite(id, data string) error { return a.terms.Write(id, []byte(data)) }

func (a *App) TerminalResize(id string, cols, rows int) { a.terms.Resize(id, cols, rows) }

func (a *App) TerminalClose(id string) { a.terms.Close(id) }

// ---- Logs ----

func (a *App) GetLogTargets(clusterID, key, ns, name string) ([]kube.LogTarget, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return nil, err
	}
	return s.LogTargets(a.ctx, key, ns, name)
}

func (a *App) StartLogs(clusterID string, opts kube.LogOptions) (string, error) {
	return a.kube.StreamLogs(clusterID, opts)
}

func (a *App) StopLogs(sub string) { a.kube.Unsubscribe(sub) }

// ---- Port forwarding ----

func (a *App) ListPortForwards(clusterID string) ([]portfwd.Runtime, error) {
	return a.forwards.List(clusterID)
}

func (a *App) AddPortForward(f portfwd.Forward, start bool) (portfwd.Runtime, error) {
	return a.forwards.Add(f, start)
}

func (a *App) StartPortForward(id string) error { return a.forwards.Start(id) }

func (a *App) StopPortForward(id string) { a.forwards.Stop(id) }

func (a *App) RemovePortForward(id string) error { return a.forwards.Remove(id) }

func (a *App) SetPortForwardAuto(id string, auto bool) error { return a.forwards.SetAuto(id, auto) }

// ---- Manifests ----

const maxManifestBytes = 5 << 20

func (a *App) PreviewManifest(clusterID, doc, defaultNS string, force bool) ([]kube.ApplyResult, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return nil, err
	}
	return s.PreviewManifest(a.ctx, doc, defaultNS, force)
}

func (a *App) ApplyManifest(clusterID, doc, defaultNS string, force bool) ([]kube.ApplyResult, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return nil, err
	}
	return s.ApplyManifest(a.ctx, doc, defaultNS, force)
}

// PickManifestFile lets the user choose a YAML/JSON file and returns its content ("" if cancelled).
func (a *App) PickManifestFile() (string, error) {
	p, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Select manifest",
		Filters: []runtime.FileFilter{{DisplayName: "Manifests (*.yaml, *.yml, *.json)", Pattern: "*.yaml;*.yml;*.json"}},
	})
	if err != nil || p == "" {
		return "", err
	}
	st, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if st.Size() > maxManifestBytes {
		return "", fmt.Errorf("file is larger than %d MB", maxManifestBytes>>20)
	}
	b, err := os.ReadFile(p)
	return string(b), err
}

// FetchManifestURL downloads a manifest over http(s).
func (a *App) FetchManifestURL(url string) (string, error) {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return "", fmt.Errorf("URL must start with http:// or https://")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestBytes+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxManifestBytes {
		return "", fmt.Errorf("manifest is larger than %d MB", maxManifestBytes>>20)
	}
	return string(b), nil
}

func (a *App) GetResourcePorts(clusterID, key, ns, name string) ([]kube.PortInfo, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return nil, err
	}
	return s.Ports(a.ctx, key, ns, name)
}

// SaveTextFile asks where to save and writes content; returns the path ("" if cancelled).
func (a *App) SaveTextFile(suggestedName, content string) (string, error) {
	p, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "Save", DefaultFilename: suggestedName})
	if err != nil || p == "" {
		return "", err
	}
	return p, os.WriteFile(p, []byte(content), 0o644)
}

// ---- Insight views ----

func (a *App) GetIngressMap(clusterID, ns string) (kube.IngressView, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return kube.IngressView{}, err
	}
	return s.IngressMap(a.ctx, ns)
}

// GetTopology returns the ingress → service → workload → pod → node graph of ns ("" = all).
func (a *App) GetTopology(clusterID, ns string) (kube.Topology, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return kube.Topology{}, err
	}
	return s.Topology(a.ctx, ns)
}

// GetApplications finds the applications of a cluster (Helm releases, labelled groups, optionally namespaces).
func (a *App) GetApplications(clusterID string, groupByNamespace bool) (kube.Applications, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return kube.Applications{}, err
	}
	return s.Applications(a.ctx, groupByNamespace)
}

const appLinksKey = "app-links"

// GetAppLinks returns the user's link templates for a cluster's applications.
func (a *App) GetAppLinks(clusterID string) []kube.AppLinkTemplate {
	raw, ok, err := a.integr.GetRaw(clusterID, appLinksKey)
	out := []kube.AppLinkTemplate{}
	if err == nil && ok {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

// SetAppLinks validates and saves the link templates.
func (a *App) SetAppLinks(clusterID string, links []kube.AppLinkTemplate) ([]kube.AppLinkTemplate, error) {
	clean, err := kube.ValidateAppLinkTemplates(links)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(clean)
	if err := a.integr.SetRaw(clusterID, appLinksKey, string(b)); err != nil {
		return nil, err
	}
	return clean, nil
}

// GetClusterOverview returns the health, capacity, attention list and recent warnings of a cluster.
func (a *App) GetClusterOverview(clusterID string) (kube.Overview, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return kube.Overview{}, err
	}
	return s.ClusterOverview(a.ctx)
}

// GetPodProbes lists a pod's liveness, readiness and startup probes and its container states.
func (a *App) GetPodProbes(clusterID, ns, pod string) (kube.PodProbes, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return kube.PodProbes{}, err
	}
	return s.PodProbes(a.ctx, ns, pod)
}

// CheckProbe runs one HTTP or TCP probe against a pod through a temporary port-forward.
func (a *App) CheckProbe(clusterID, ns, pod string, req kube.ProbeRequest) (kube.ProbeResult, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return kube.ProbeResult{}, err
	}
	return s.CheckProbe(a.ctx, ns, pod, req)
}

// ForwardPodPort forwards one pod port to a free local port (reusing an identical forward) and returns its state.
func (a *App) ForwardPodPort(clusterID, ns, pod string, port int) (portfwd.Runtime, error) {
	if port < 1 || port > 65535 {
		return portfwd.Runtime{}, fmt.Errorf("invalid port %d", port)
	}
	return a.forwards.Ensure(portfwd.Forward{ClusterID: clusterID, Namespace: ns, Kind: "pod", Name: pod, RemotePort: port})
}

// GetSimTargets lists namespaces, pods, services and nodes for the simulators.
func (a *App) GetSimTargets(clusterID string) (kube.SimTargets, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return kube.SimTargets{}, err
	}
	return s.SimulatorTargets(a.ctx)
}

// SimulateNetwork evaluates the cluster's NetworkPolicies for one connection. Read-only.
func (a *App) SimulateNetwork(clusterID string, q kube.NetQuery) (kube.NetResult, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return kube.NetResult{}, err
	}
	return s.SimulateNetwork(a.ctx, q)
}

// SimulateDrain predicts the effect of draining a node. Read-only.
func (a *App) SimulateDrain(clusterID, node string) (kube.DrainResult, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return kube.DrainResult{}, err
	}
	return s.SimulateDrain(a.ctx, node)
}

// ---- Multi-cluster drift and promotion ----

func (a *App) GetDriftKinds() []kube.DriftKindInfo { return kube.DriftKinds() }

// driftSessions connects to both clusters on demand: neither has to be open in the explorer.
func (a *App) driftSessions(srcID, dstID string) (*kube.Session, *kube.Session, error) {
	src, err := a.kube.EnsureReady(a.ctx, srcID)
	if err != nil {
		return nil, nil, fmt.Errorf("source cluster: %w", err)
	}
	dst, err := a.kube.EnsureReady(a.ctx, dstID)
	if err != nil {
		return nil, nil, fmt.Errorf("target cluster: %w", err)
	}
	return src, dst, nil
}

// GetNamespaces lists a cluster's namespaces, connecting to it first if it is not open (used by the comparison).
func (a *App) GetNamespaces(clusterID string) ([]string, error) {
	s, err := a.kube.Ensure(clusterID)
	if err != nil {
		return nil, err
	}
	return s.Namespaces(a.ctx)
}

// CompareClusters compares the chosen kinds between two namespaces (of the same or of different clusters). Read-only.
func (a *App) CompareClusters(srcID, srcNS, dstID, dstNS string, keys []string) (kube.DriftReport, error) {
	src, dst, err := a.driftSessions(srcID, dstID)
	if err != nil {
		return kube.DriftReport{}, err
	}
	return kube.CompareClusters(a.ctx, src, dst, srcNS, dstNS, keys), nil
}

// CompareObject returns the field-level differences of one object. Read-only.
func (a *App) CompareObject(srcID, srcNS, dstID, dstNS, key, name string) (kube.DriftObject, error) {
	src, dst, err := a.driftSessions(srcID, dstID)
	if err != nil {
		return kube.DriftObject{}, err
	}
	return kube.CompareObject(a.ctx, src, dst, srcNS, dstNS, key, name)
}

// BuildPromotion prepares (but does not apply) the manifest that carries the chosen fields to the target.
func (a *App) BuildPromotion(srcID, srcNS, dstID, dstNS, key, name string, paths []string) (kube.Promotion, error) {
	src, dst, err := a.driftSessions(srcID, dstID)
	if err != nil {
		return kube.Promotion{}, err
	}
	return kube.BuildPromotion(a.ctx, src, dst, srcNS, dstNS, key, name, paths)
}

func (a *App) GetConfigData(clusterID, key, ns, name string) (kube.ConfigData, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return kube.ConfigData{}, err
	}
	return s.ConfigData(a.ctx, key, ns, name)
}

func (a *App) RevealSecretKey(clusterID, ns, name, k string) (kube.DataEntry, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return kube.DataEntry{}, err
	}
	return s.RevealSecretKey(a.ctx, ns, name, k)
}

func (a *App) GetHealth(clusterID, key, ns, name string) (kube.HealthReport, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return kube.HealthReport{}, err
	}
	return s.Health(a.ctx, key, ns, name)
}

func (a *App) GetTimeline(clusterID, key, ns, name string) ([]kube.Event, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return nil, err
	}
	return s.Timeline(a.ctx, key, ns, name)
}

// GetMetricsSeries returns usage history. A rangeSecs beyond the in-memory window uses Prometheus when configured.
func (a *App) GetMetricsSeries(clusterID, key, ns, name string, rangeSecs int) (kube.Series, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return kube.Series{}, err
	}
	if rangeSecs > 0 {
		if cfg, ok, _ := a.integr.PromConfig(clusterID); ok {
			return s.PromSeries(a.ctx, cfg, key, ns, name, rangeSecs)
		}
	}
	return s.MetricsSeries(a.ctx, key, ns, name)
}

// GetUsage returns current CPU/memory for all pods (or one namespace) or nodes, keyed "ns/name" / "name".
func (a *App) GetUsage(clusterID, kind, ns string) (map[string]kube.Usage, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return nil, err
	}
	return s.UsageMap(a.ctx, kind, ns)
}

// ---- Assessments (feasibility reports) ----

func (a *App) StartAssessment(clusterID string) (string, error) { return a.reports.Start(clusterID) }

func (a *App) CancelAssessment(clusterID string) { a.reports.Cancel(clusterID) }

func (a *App) AssessmentRunning(clusterID string) string { return a.reports.Running(clusterID) }

func (a *App) ListAssessments(clusterID string) ([]feasibility.Summary, error) {
	return a.reports.List(clusterID)
}

func (a *App) GetAssessment(id string) (feasibility.Report, error) { return a.reports.Get(id) }

func (a *App) DeleteAssessment(id string) error { return a.reports.Delete(id) }

func (a *App) CompareAssessments(fromID, toID string) (feasibility.Diff, error) {
	return a.reports.Compare(fromID, toID)
}

// ExportAssessment saves a report as "json" or print-friendly "html"; returns the path ("" if cancelled).
func (a *App) ExportAssessment(id, format string) (string, error) {
	rep, err := a.reports.Get(id)
	if err != nil {
		return "", err
	}
	var content string
	switch format {
	case "json":
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return "", err
		}
		content = string(b)
	case "html":
		if content, err = feasibility.ExportHTML(rep); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unsupported format %q", format)
	}
	day := time.Unix(rep.StartedAt, 0).Format("2006-01-02")
	return a.SaveTextFile(fmt.Sprintf("assessment-%s-%s.%s", rep.ClusterName, day, format), content)
}

// ---- Integrations ----

type Integrations struct {
	ArgoCD               bool `json:"argocd"`
	Flux                 bool `json:"flux"`
	Helm                 bool `json:"helm"`
	PrometheusConfigured bool `json:"prometheusConfigured"`
}

func (a *App) GetIntegrations(clusterID string) (Integrations, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return Integrations{}, err
	}
	out := Integrations{Helm: true, Flux: len(s.FluxKinds()) > 0}
	_, out.ArgoCD = s.HasResource("argoproj.io/v1alpha1/applications")
	_, out.PrometheusConfigured, _ = a.integr.PromConfig(clusterID)
	return out, nil
}

// Argo CD

func (a *App) ListArgoApps(clusterID string) ([]kube.ArgoApp, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return nil, err
	}
	return s.ArgoApps(a.ctx)
}

func (a *App) GetArgoApp(clusterID, ns, name string) (kube.ArgoDetail, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return kube.ArgoDetail{}, err
	}
	return s.ArgoApp(a.ctx, ns, name)
}

func (a *App) RefreshArgoApp(clusterID, ns, name string, hard bool) error {
	s, err := a.withSession(clusterID)
	if err != nil {
		return err
	}
	return s.ArgoRefresh(a.ctx, ns, name, hard)
}

func (a *App) SyncArgoApp(clusterID, ns, name string, opts kube.ArgoSyncOptions) error {
	s, err := a.withSession(clusterID)
	if err != nil {
		return err
	}
	return s.ArgoSync(a.ctx, ns, name, opts)
}

func (a *App) RollbackArgoApp(clusterID, ns, name string, historyID int, prune, disableAuto bool) error {
	s, err := a.withSession(clusterID)
	if err != nil {
		return err
	}
	return s.ArgoRollback(a.ctx, ns, name, int64(historyID), prune, disableAuto)
}

func (a *App) SetArgoAutoSync(clusterID, ns, name string, enable, prune, selfHeal bool) error {
	s, err := a.withSession(clusterID)
	if err != nil {
		return err
	}
	return s.ArgoSetAutoSync(a.ctx, ns, name, enable, prune, selfHeal)
}

// Helm

func (a *App) ListHelmReleases(clusterID string) ([]kube.HelmRelease, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return nil, err
	}
	return s.HelmReleases(a.ctx)
}

func (a *App) GetHelmRelease(clusterID, ns, name string, revision int) (kube.HelmDetail, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return kube.HelmDetail{}, err
	}
	return s.HelmDetail(a.ctx, ns, name, revision)
}

// HelmCLIAvailable reports whether rollback/uninstall can be offered.
func (a *App) HelmCLIAvailable() bool {
	_, err := helmcli.Find()
	return err == nil
}

func (a *App) helmRun(clusterID string, fn func(bin, kubeconfig string) (string, error)) (string, error) {
	shellenv.Wait()
	bin, err := helmcli.Find()
	if err != nil {
		return "", err
	}
	path, cleanup, err := a.clusters.TempKubeconfig(clusterID)
	if err != nil {
		return "", err
	}
	defer cleanup()
	return fn(bin, path)
}

func (a *App) RollbackHelmRelease(clusterID, ns, name string, revision int) (string, error) {
	return a.helmRun(clusterID, func(bin, kc string) (string, error) {
		return helmcli.Rollback(a.ctx, bin, kc, ns, name, revision)
	})
}

func (a *App) UninstallHelmRelease(clusterID, ns, name string) (string, error) {
	return a.helmRun(clusterID, func(bin, kc string) (string, error) {
		return helmcli.Uninstall(a.ctx, bin, kc, ns, name)
	})
}

// Flux

func (a *App) ListFluxObjects(clusterID string) ([]kube.FluxObject, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return nil, err
	}
	return s.FluxObjects(a.ctx)
}

func (a *App) GetFluxObject(clusterID, key, ns, name string) (kube.FluxDetail, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return kube.FluxDetail{}, err
	}
	return s.FluxDetail(a.ctx, key, ns, name)
}

func (a *App) ReconcileFlux(clusterID, key, ns, name string) error {
	s, err := a.withSession(clusterID)
	if err != nil {
		return err
	}
	return s.FluxReconcile(a.ctx, key, ns, name)
}

func (a *App) SuspendFlux(clusterID, key, ns, name string, suspend bool) error {
	s, err := a.withSession(clusterID)
	if err != nil {
		return err
	}
	return s.FluxSuspend(a.ctx, key, ns, name, suspend)
}

// Prometheus

func (a *App) DetectPrometheus(clusterID string) ([]kube.PromCandidate, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return nil, err
	}
	return s.PromDetect(a.ctx)
}

type PromState struct {
	Configured bool            `json:"configured"`
	Config     kube.PromConfig `json:"config"`
}

func (a *App) GetPrometheusConfig(clusterID string) (PromState, error) {
	c, ok, err := a.integr.PromConfig(clusterID)
	return PromState{Configured: ok, Config: c}, err
}

// SavePrometheusConfig verifies the endpoint first and returns the Prometheus version.
func (a *App) SavePrometheusConfig(clusterID string, cfg kube.PromConfig) (string, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return "", err
	}
	if cfg.Scheme == "" {
		cfg.Scheme = "http"
	}
	ver, err := s.PromVersion(a.ctx, cfg)
	if err != nil {
		return "", err
	}
	return ver, a.integr.SetPromConfig(clusterID, cfg)
}

func (a *App) ClearPrometheusConfig(clusterID string) error {
	return a.integr.ClearPromConfig(clusterID)
}

func (a *App) QueryPrometheus(clusterID, query string, rangeSecs, stepSecs int) (kube.PromResult, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return kube.PromResult{}, err
	}
	cfg, ok, err := a.integr.PromConfig(clusterID)
	if err != nil {
		return kube.PromResult{}, err
	}
	if !ok {
		return kube.PromResult{}, fmt.Errorf("Prometheus is not configured for this cluster")
	}
	if rangeSecs < 60 || rangeSecs > 31*24*3600 {
		return kube.PromResult{}, fmt.Errorf("range must be between 1 minute and 31 days")
	}
	if stepSecs <= 0 {
		stepSecs = rangeSecs / 240
	}
	end := time.Now()
	return s.PromQueryRange(a.ctx, cfg, query, end.Add(-time.Duration(rangeSecs)*time.Second), end, time.Duration(stepSecs)*time.Second)
}

// ---- Debug containers & certificates ----

// ListDebugContainers returns the ephemeral containers already present in a pod (for re-attaching).
func (a *App) ListDebugContainers(clusterID, ns, pod string) ([]kube.DebugContainer, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return nil, err
	}
	return s.DebugContainers(a.ctx, ns, pod)
}

// StartDebugContainer injects a debug container into a running pod and waits until it is running.
func (a *App) StartDebugContainer(clusterID, ns, pod string, opts kube.DebugOptions) (kube.DebugContainer, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return kube.DebugContainer{}, err
	}
	return s.StartDebug(a.ctx, ns, pod, opts)
}

// GetCertificates inventories TLS secrets (ns "" = all namespaces) with expiry details.
func (a *App) GetCertificates(clusterID, ns string) (kube.CertReport, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return kube.CertReport{}, err
	}
	return s.Certificates(a.ctx, ns)
}

// QuickForwardResult is the outcome of a one-click forward.
type QuickForwardResult struct {
	Forward    *portfwd.Runtime `json:"forward"`
	Ports      []kube.PortInfo  `json:"ports"`
	NeedChoice bool             `json:"needChoice"` // several ports: the UI must ask which one
}

// QuickForward forwards a pod, Service or workload with an automatic local port. With one declared port it
// starts immediately (reusing an existing identical forward); with several it returns them for the UI to choose.
func (a *App) QuickForward(clusterID, key, ns, name string) (QuickForwardResult, error) {
	s, err := a.withSession(clusterID)
	if err != nil {
		return QuickForwardResult{}, err
	}
	res, ok := s.HasResource(key)
	if !ok {
		return QuickForwardResult{}, fmt.Errorf("unknown resource %q", key)
	}
	kind, ok := kube.ForwardKinds[res.Name]
	if !ok {
		return QuickForwardResult{}, fmt.Errorf("port forwarding is not supported for %s", res.Kind)
	}
	ports, err := s.Ports(a.ctx, key, ns, name)
	if err != nil {
		return QuickForwardResult{}, err
	}
	switch len(ports) {
	case 0:
		return QuickForwardResult{}, fmt.Errorf("%s declares no ports; use Forward… to enter one", name)
	case 1:
	default:
		return QuickForwardResult{Ports: ports, NeedChoice: true}, nil
	}
	rt, err := a.forwards.Ensure(portfwd.Forward{ClusterID: clusterID, Namespace: ns, Kind: kind, Name: name, RemotePort: ports[0].Port})
	if err != nil {
		return QuickForwardResult{}, err
	}
	return QuickForwardResult{Forward: &rt, Ports: ports}, nil
}

// ---- Cluster Doctor ----

const doctorScope, doctorKey = "_global", "doctor"

func (a *App) doctorConfig() feasibility.Config {
	cfg := feasibility.DefaultConfig()
	if raw, ok, _ := a.integr.GetRaw(doctorScope, doctorKey); ok {
		var c feasibility.Config
		if json.Unmarshal([]byte(raw), &c) == nil {
			cfg = c.Normalize()
		}
	}
	return cfg
}

// GetDoctorConfig returns the cost rates and production-namespace settings (defaults if never changed).
func (a *App) GetDoctorConfig() feasibility.Config { return a.doctorConfig() }

// SetDoctorConfig validates and saves the Doctor settings.
func (a *App) SetDoctorConfig(cfg feasibility.Config) (feasibility.Config, error) {
	if cfg.CPUHourUSD <= 0 || cfg.CPUHourUSD > 100 || cfg.MemGBHourUSD <= 0 || cfg.MemGBHourUSD > 100 {
		return cfg, fmt.Errorf("prices must be greater than 0 and at most 100 USD per hour")
	}
	if cfg.HeadroomPct < 0 || cfg.HeadroomPct > 300 {
		return cfg, fmt.Errorf("headroom must be between 0 and 300 percent")
	}
	var clean []string
	for _, p := range cfg.ProdNamespaces {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if len(p) > 63 || strings.ContainsAny(p, " \t/,;") {
			return cfg, fmt.Errorf("invalid namespace pattern %q", p)
		}
		clean = append(clean, p)
	}
	if len(clean) > 100 {
		return cfg, fmt.Errorf("too many namespace patterns (max 100)")
	}
	cfg.ProdNamespaces = clean
	cfg = cfg.Normalize()
	b, _ := json.Marshal(cfg)
	return cfg, a.integr.SetRaw(doctorScope, doctorKey, string(b))
}

// ProposeFixes returns reviewable changes for an assessment finding (see feasibility.ProposeFixes).
func (a *App) ProposeFixes(clusterID, ruleID, resourceKey, ns, name string) ([]feasibility.Fix, error) {
	return a.reports.Fixes(a.ctx, clusterID, ruleID, resourceKey, ns, name)
}

// GetFixableRules lists the rule ids that have automatic fixes.
func (a *App) GetFixableRules() []string { return feasibility.FixableRules() }
