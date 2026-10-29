package kube

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/client-go/rest"
)

type recorder struct {
	mu      sync.Mutex
	patches []string // "PATH BODY"
}

func (r *recorder) add(p, body string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.patches = append(r.patches, p+" "+body)
}

func (r *recorder) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.patches) == 0 {
		return ""
	}
	return r.patches[len(r.patches)-1]
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func argoApp(name string, extra map[string]any) map[string]any {
	spec := map[string]any{
		"project":     "default",
		"source":      map[string]any{"repoURL": "https://git/x.git", "path": "apps/web", "targetRevision": "main"},
		"destination": map[string]any{"server": "https://kubernetes.default.svc", "namespace": "web"},
	}
	status := map[string]any{
		"sync":   map[string]any{"status": "OutOfSync", "revision": "abc123"},
		"health": map[string]any{"status": "Degraded", "message": "pods failing"},
		"resources": []any{
			map[string]any{"group": "apps", "kind": "Deployment", "name": "web", "namespace": "web", "status": "OutOfSync", "health": map[string]any{"status": "Degraded"}},
			map[string]any{"kind": "Service", "name": "web", "namespace": "web", "status": "Synced", "health": map[string]any{"status": "Healthy"}},
		},
		"history": []any{
			map[string]any{"id": 1, "revision": "r1", "deployedAt": "2026-01-01T00:00:00Z", "source": map[string]any{"repoURL": "https://git/x.git", "path": "apps/web", "targetRevision": "main"}},
			map[string]any{"id": 2, "revision": "r2", "deployedAt": "2026-01-02T00:00:00Z"},
		},
		"conditions": []any{map[string]any{"type": "ComparisonError", "message": "boom"}},
	}
	o := map[string]any{"apiVersion": "argoproj.io/v1alpha1", "kind": "Application", "metadata": map[string]any{"name": name, "namespace": "argocd"}, "spec": spec, "status": status}
	for k, v := range extra {
		if k == "spec" {
			for sk, sv := range v.(map[string]any) {
				spec[sk] = sv
			}
		} else if k == "operation" {
			o["operation"] = v
		}
	}
	return o
}

func newIntegrationSession(t *testing.T, rec *recorder, apps map[string]map[string]any, extra func(mux *http.ServeMux)) *Session {
	mux := http.NewServeMux()
	mux.HandleFunc("/apis/argoproj.io/v1alpha1/applications", func(w http.ResponseWriter, r *http.Request) {
		items := []any{}
		for _, a := range apps {
			items = append(items, a)
		}
		jsonOK(w, map[string]any{"apiVersion": "argoproj.io/v1alpha1", "kind": "ApplicationList", "items": items})
	})
	mux.HandleFunc("/apis/argoproj.io/v1alpha1/namespaces/argocd/applications/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/apis/argoproj.io/v1alpha1/namespaces/argocd/applications/")
		if r.Method == http.MethodPatch {
			b, _ := io.ReadAll(r.Body)
			rec.add(r.URL.Path, string(b))
		}
		if a, ok := apps[name]; ok {
			jsonOK(w, a)
			return
		}
		w.WriteHeader(404)
		jsonOK(w, map[string]any{"kind": "Status", "apiVersion": "v1", "status": "Failure", "reason": "NotFound", "code": 404})
	})
	if extra != nil {
		extra(mux)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s, err := NewSession("c1", &rest.Config{Host: srv.URL}, func(string, any) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestArgoListAndDetail(t *testing.T) {
	s := newIntegrationSession(t, &recorder{}, map[string]map[string]any{"web": argoApp("web", nil)}, nil)
	apps, err := s.ArgoApps(context.Background())
	if err != nil || len(apps) != 1 {
		t.Fatalf("%v %+v", err, apps)
	}
	a := apps[0]
	if a.Sync != "OutOfSync" || a.Health != "Degraded" || a.Resources != 2 || a.OutOfSync != 1 || a.Unhealthy != 1 || a.Errors != 1 || a.Repo != "https://git/x.git" || a.Path != "apps/web" || a.Auto {
		t.Fatalf("%+v", a)
	}
	d, err := s.ArgoApp(context.Background(), "argocd", "web")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Items) != 2 || len(d.History) != 2 || d.History[0].ID != 2 || len(d.Conditions) != 1 || len(d.Sources) != 1 {
		t.Fatalf("%+v", d)
	}
}

func TestArgoSyncRefreshAndBusy(t *testing.T) {
	rec := &recorder{}
	s := newIntegrationSession(t, rec, map[string]map[string]any{
		"web":  argoApp("web", nil),
		"busy": argoApp("busy", map[string]any{"operation": map[string]any{"sync": map[string]any{}}}),
	}, nil)
	ctx := context.Background()
	if err := s.ArgoSync(ctx, "argocd", "web", ArgoSyncOptions{Prune: true, Force: true, Revision: "v2"}); err != nil {
		t.Fatal(err)
	}
	var p struct {
		Operation struct {
			InitiatedBy map[string]string `json:"initiatedBy"`
			Sync        map[string]any    `json:"sync"`
		} `json:"operation"`
	}
	body := strings.SplitN(rec.last(), " ", 2)[1]
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	if p.Operation.Sync["prune"] != true || p.Operation.Sync["revision"] != "v2" || p.Operation.InitiatedBy["username"] != "soliton-rudder" {
		t.Fatalf("%s", body)
	}
	if st, _ := p.Operation.Sync["syncStrategy"].(map[string]any); st["apply"] == nil {
		t.Fatalf("force must use the apply strategy: %s", body)
	}
	if err := s.ArgoSync(ctx, "argocd", "busy", ArgoSyncOptions{}); err == nil || !strings.Contains(err.Error(), "in progress") {
		t.Fatalf("busy app must be refused: %v", err)
	}
	if err := s.ArgoRefresh(ctx, "argocd", "web", true); err != nil || !strings.Contains(rec.last(), `"argocd.argoproj.io/refresh":"hard"`) {
		t.Fatalf("%v %s", err, rec.last())
	}
}

func TestArgoRollbackRequiresDisablingAutoSync(t *testing.T) {
	rec := &recorder{}
	auto := argoApp("auto", map[string]any{"spec": map[string]any{"syncPolicy": map[string]any{"automated": map[string]any{"prune": true}}}})
	s := newIntegrationSession(t, rec, map[string]map[string]any{"auto": auto, "manual": argoApp("manual", nil)}, nil)
	ctx := context.Background()
	if err := s.ArgoRollback(ctx, "argocd", "auto", 1, false, false); err == nil || !strings.Contains(err.Error(), "auto-sync") {
		t.Fatalf("expected refusal: %v", err)
	}
	if len(rec.patches) != 0 {
		t.Fatal("nothing must be patched on refusal")
	}
	if err := s.ArgoRollback(ctx, "argocd", "auto", 1, true, true); err != nil {
		t.Fatal(err)
	}
	if len(rec.patches) != 2 || !strings.Contains(rec.patches[0], `"automated":null`) || !strings.Contains(rec.patches[1], `"revision":"r1"`) || !strings.Contains(rec.patches[1], `"source"`) {
		t.Fatalf("%v", rec.patches)
	}
	if err := s.ArgoRollback(ctx, "argocd", "manual", 99, false, false); err == nil {
		t.Fatal("unknown history id must fail")
	}
}

func helmRelease(name, ns string, rev int, status, chartVer string) (string, string) {
	rel := map[string]any{
		"name": name, "namespace": ns, "version": rev,
		"info":     map[string]any{"status": status, "last_deployed": "2026-01-0" + string(rune('0'+rev)) + "T10:00:00Z", "description": "Upgrade complete", "notes": "Visit http://x"},
		"chart":    map[string]any{"metadata": map[string]any{"name": "web", "version": chartVer, "appVersion": "1.2.3"}, "values": map[string]any{"replicas": 1, "image": map[string]any{"tag": "a", "repo": "r"}}},
		"config":   map[string]any{"image": map[string]any{"tag": "b"}},
		"manifest": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\n---\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n  namespace: other\n",
	}
	raw, _ := json.Marshal(rel)
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(raw)
	zw.Close()
	helm := base64.StdEncoding.EncodeToString(buf.Bytes())
	return helm, base64.StdEncoding.EncodeToString([]byte(helm)) // helm layer, then the Kubernetes secret layer
}

func TestDecodeHelmReleaseBothStorageDrivers(t *testing.T) {
	helmB64, secretB64 := helmRelease("web", "default", 1, "deployed", "0.1.0")
	r, err := decodeHelmRelease(secretB64, true)
	if err != nil || r.Name != "web" || r.Chart.Metadata.Version != "0.1.0" {
		t.Fatalf("secret: %v %+v", err, r)
	}
	r, err = decodeHelmRelease(helmB64, false)
	if err != nil || r.Info.Status != "deployed" {
		t.Fatalf("configmap: %v %+v", err, r)
	}
	if _, err := decodeHelmRelease("!!!", true); err == nil {
		t.Fatal("garbage must fail")
	}
}

func TestHelmListDetailAndMerge(t *testing.T) {
	metaItem := func(rev int, status string) map[string]any {
		return map[string]any{"metadata": map[string]any{
			"name": "sh.helm.release.v1.web.v" + string(rune('0'+rev)), "namespace": "default",
			"labels": map[string]string{"name": "web", "owner": "helm", "status": status, "version": string(rune('0' + rev))},
		}}
	}
	s := newIntegrationSession(t, &recorder{}, nil, func(mux *http.ServeMux) {
		mux.HandleFunc("/api/v1/secrets", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("labelSelector") != "owner=helm" {
				t.Errorf("selector: %s", r.URL.RawQuery)
			}
			jsonOK(w, map[string]any{"kind": "PartialObjectMetadataList", "apiVersion": "meta.k8s.io/v1", "metadata": map[string]any{}, "items": []any{metaItem(1, "superseded"), metaItem(2, "deployed")}})
		})
		mux.HandleFunc("/api/v1/configmaps", func(w http.ResponseWriter, r *http.Request) {
			jsonOK(w, map[string]any{"kind": "PartialObjectMetadataList", "apiVersion": "meta.k8s.io/v1", "metadata": map[string]any{}, "items": []any{}})
		})
		for rev, ver := range map[int]string{1: "0.1.0", 2: "0.2.0"} {
			_, secretB64 := helmRelease("web", "default", rev, map[int]string{1: "superseded", 2: "deployed"}[rev], ver)
			name := "sh.helm.release.v1.web.v" + string(rune('0'+rev))
			mux.HandleFunc("/api/v1/namespaces/default/secrets/"+name, func(w http.ResponseWriter, r *http.Request) {
				jsonOK(w, map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": name, "namespace": "default"}, "data": map[string]any{"release": secretB64}})
			})
		}
	})
	list, err := s.HelmReleases(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("%v %+v", err, list)
	}
	r := list[0]
	if r.Name != "web" || r.Revision != 2 || r.Status != "deployed" || r.ChartVersion != "0.2.0" || r.AppVersion != "1.2.3" || r.Storage != "secret" {
		t.Fatalf("%+v", r)
	}
	d, err := s.HelmDetail(context.Background(), "default", "web", 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Notes != "Visit http://x" || !strings.Contains(d.Values, "tag: b") || strings.Contains(d.Values, "replicas") {
		t.Fatalf("values: %q", d.Values)
	}
	if !strings.Contains(d.AllValues, "replicas: 1") || !strings.Contains(d.AllValues, "tag: b") || !strings.Contains(d.AllValues, "repo: r") {
		t.Fatalf("merged: %q", d.AllValues)
	}
	if len(d.Objects) != 2 || d.Objects[0].Namespace != "default" || d.Objects[1].Namespace != "other" {
		t.Fatalf("objects: %+v", d.Objects)
	}
	if len(d.History) != 2 || d.History[0].Revision != 2 || d.History[1].ChartVersion != "0.1.0" {
		t.Fatalf("history: %+v", d.History)
	}
	if _, err := s.HelmDetail(context.Background(), "default", "web", 7); err == nil {
		t.Fatal("unknown revision must fail")
	}
	if _, err := s.HelmDetail(context.Background(), "default", "nope", 0); err == nil {
		t.Fatal("unknown release must fail")
	}
}

func TestFluxObjectsActionsAndInventory(t *testing.T) {
	rec := &recorder{}
	s := newIntegrationSession(t, rec, nil, func(mux *http.ServeMux) {
		mux.HandleFunc("/apis/kustomize.toolkit.fluxcd.io/v1/kustomizations", func(w http.ResponseWriter, r *http.Request) {
			jsonOK(w, map[string]any{"kind": "KustomizationList", "apiVersion": "kustomize.toolkit.fluxcd.io/v1", "items": []any{map[string]any{
				"apiVersion": "kustomize.toolkit.fluxcd.io/v1", "kind": "Kustomization", "metadata": map[string]any{"name": "apps", "namespace": "flux-system"},
				"spec": map[string]any{"interval": "10m", "path": "./apps", "sourceRef": map[string]any{"kind": "GitRepository", "name": "fleet"}, "suspend": true},
				"status": map[string]any{"lastAppliedRevision": "main@sha1:abc", "conditions": []any{map[string]any{"type": "Ready", "status": "False", "reason": "BuildFailed", "message": "kustomize build failed"}},
					"inventory": map[string]any{"entries": []any{map[string]any{"id": "web_frontend_apps_Deployment", "v": "v1"}}}},
			}}})
		})
		mux.HandleFunc("/apis/kustomize.toolkit.fluxcd.io/v1/namespaces/flux-system/kustomizations/apps", func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPatch {
				b, _ := io.ReadAll(r.Body)
				rec.add(r.URL.Path, string(b))
			}
			jsonOK(w, map[string]any{
				"apiVersion": "kustomize.toolkit.fluxcd.io/v1", "kind": "Kustomization", "metadata": map[string]any{"name": "apps", "namespace": "flux-system"},
				"status": map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "False"}}, "inventory": map[string]any{"entries": []any{map[string]any{"id": "web_frontend_apps_Deployment"}, map[string]any{"id": "bad"}}}},
			})
		})
	})
	s.catalog["kustomize.toolkit.fluxcd.io/v1/kustomizations"] = Resource{Key: "kustomize.toolkit.fluxcd.io/v1/kustomizations", Group: "kustomize.toolkit.fluxcd.io", Version: "v1", Name: "kustomizations", Kind: "Kustomization", Namespaced: true}
	s.catalog["apps/v1/deployments"] = Resource{Key: "apps/v1/deployments", Group: "apps", Version: "v1", Name: "deployments", Kind: "Deployment", Namespaced: true}
	ctx := context.Background()
	if len(s.FluxKinds()) != 1 {
		t.Fatalf("%+v", s.FluxKinds())
	}
	objs, err := s.FluxObjects(ctx)
	if err != nil || len(objs) != 1 {
		t.Fatalf("%v %+v", err, objs)
	}
	o := objs[0]
	if o.Ready != "False" || o.Reason != "BuildFailed" || !o.Suspended || o.Revision != "main@sha1:abc" || o.Source != "GitRepository/fleet" || o.Path != "./apps" || o.Interval != "10m" {
		t.Fatalf("%+v", o)
	}
	d, err := s.FluxDetail(ctx, o.Key, "flux-system", "apps")
	if err != nil || len(d.Inventory) != 1 || d.Inventory[0].Kind != "Deployment" || d.Inventory[0].Name != "frontend" || d.Inventory[0].Namespace != "web" {
		t.Fatalf("%v %+v", err, d.Inventory)
	}
	if err := s.FluxReconcile(ctx, o.Key, "flux-system", "apps"); err != nil || !strings.Contains(rec.last(), "reconcile.fluxcd.io/requestedAt") {
		t.Fatalf("%v %s", err, rec.last())
	}
	if err := s.FluxSuspend(ctx, o.Key, "flux-system", "apps", false); err != nil || !strings.Contains(rec.last(), `"suspend":false`) {
		t.Fatalf("%v %s", err, rec.last())
	}
	if err := s.FluxReconcile(ctx, "apps/v1/deployments", "x", "y"); err == nil {
		t.Fatal("non-Flux kinds must be refused")
	}
}

func TestPromProxyPathAndQuery(t *testing.T) {
	c := PromConfig{Namespace: "monitoring", Service: "prom", Port: "9090", Scheme: "http", Path: "/prometheus/"}
	if p := c.proxyPath("/api/v1/query_range"); p != "/api/v1/namespaces/monitoring/services/prom:9090/proxy/prometheus/api/v1/query_range" {
		t.Fatal(p)
	}
	c.Scheme = "https"
	if p := c.proxyPath("/x"); !strings.Contains(p, "/services/https:prom:9090/proxy") {
		t.Fatal(p)
	}
	if err := (PromConfig{}).valid(); err == nil {
		t.Fatal("empty config must be invalid")
	}

	var seen string
	s := newIntegrationSession(t, &recorder{}, nil, func(mux *http.ServeMux) {
		mux.HandleFunc("/api/v1/namespaces/monitoring/services/prom:9090/proxy/api/v1/query_range", func(w http.ResponseWriter, r *http.Request) {
			seen = r.URL.Query().Get("query")
			jsonOK(w, map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{
				map[string]any{"metric": map[string]string{"pod": "a"}, "values": [][]any{{1700000000, "1.5"}, {1700000015, "NaN"}, {1700000030, "2"}}},
			}}})
		})
		mux.HandleFunc("/api/v1/namespaces/monitoring/services/prom:9090/proxy/api/v1/status/buildinfo", func(w http.ResponseWriter, r *http.Request) {
			jsonOK(w, map[string]any{"status": "success", "data": map[string]string{"version": "2.53.0"}})
		})
	})
	cfg := PromConfig{Namespace: "monitoring", Service: "prom", Port: "9090", Scheme: "http"}
	res, err := s.PromQueryRange(context.Background(), cfg, "up", timeNow().Add(-3600e9), timeNow(), 15e9)
	if err != nil || seen != "up" || len(res.Series) != 1 || len(res.Series[0].Points) != 2 {
		t.Fatalf("%v %q %+v", err, seen, res)
	}
	if v, err := s.PromVersion(context.Background(), cfg); err != nil || v != "2.53.0" {
		t.Fatalf("%v %q", err, v)
	}
}

func TestPromDetectRanksNonHeadlessAndSkipsExporters(t *testing.T) {
	svc := func(name, ns, clusterIP string, ports ...map[string]any) map[string]any {
		return map[string]any{"apiVersion": "v1", "kind": "Service", "metadata": map[string]any{"name": name, "namespace": ns}, "spec": map[string]any{"clusterIP": clusterIP, "ports": ports}}
	}
	web := map[string]any{"name": "web", "port": 9090}
	s := newIntegrationSession(t, &recorder{}, nil, func(mux *http.ServeMux) {
		mux.HandleFunc("/api/v1/services", func(w http.ResponseWriter, r *http.Request) {
			jsonOK(w, map[string]any{"kind": "ServiceList", "apiVersion": "v1", "items": []any{
				svc("prometheus-operated", "monitoring", "None", web),
				svc("kps-prometheus", "monitoring", "10.0.0.1", web, map[string]any{"name": "reloader-web", "port": 8080}),
				svc("kps-prometheus-node-exporter", "monitoring", "10.0.0.2", map[string]any{"name": "metrics", "port": 9100}),
				svc("kps-alertmanager", "monitoring", "10.0.0.3", map[string]any{"name": "web", "port": 9093}),
				svc("prometheus-operator", "monitoring", "10.0.0.4", map[string]any{"name": "https", "port": 443}),
				svc("unrelated", "default", "10.0.0.5", web),
			}})
		})
	})
	c, err := s.PromDetect(context.Background())
	if err != nil || len(c) != 2 {
		t.Fatalf("%v %+v", err, c)
	}
	if c[0].Service != "kps-prometheus" || c[0].Port != "9090" || c[1].Service != "prometheus-operated" || !c[1].Headless {
		t.Fatalf("%+v", c)
	}
}

func timeNow() time.Time { return time.Now() }

func TestPromSeriesForWorkload(t *testing.T) {
	var queries []string
	s := newIntegrationSession(t, &recorder{}, nil, func(mux *http.ServeMux) {
		mux.HandleFunc("/apis/apps/v1/namespaces/default/deployments/web", func(w http.ResponseWriter, r *http.Request) {
			jsonOK(w, map[string]any{"kind": "Deployment", "apiVersion": "apps/v1", "metadata": map[string]any{"name": "web"}, "spec": map[string]any{"selector": map[string]any{"matchLabels": map[string]string{"app": "web"}}}})
		})
		mux.HandleFunc("/api/v1/namespaces/default/pods", func(w http.ResponseWriter, r *http.Request) {
			pod := func(n string) map[string]any {
				return map[string]any{"kind": "Pod", "apiVersion": "v1", "metadata": map[string]any{"name": n, "namespace": "default"}, "spec": map[string]any{"containers": []any{map[string]any{"name": "c", "resources": map[string]any{"requests": map[string]string{"cpu": "100m"}}}}}}
			}
			jsonOK(w, map[string]any{"kind": "PodList", "apiVersion": "v1", "items": []any{pod("web-1"), pod("web-2")}})
		})
		mux.HandleFunc("/api/v1/namespaces/mon/services/prom:9090/proxy/api/v1/query_range", func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query().Get("query")
			queries = append(queries, q)
			val := "250"
			if strings.Contains(q, "memory") {
				val = "1048576"
			}
			jsonOK(w, map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{
				map[string]any{"metric": map[string]string{}, "values": [][]any{{1700000000, val}, {1700000015, val}}},
			}}})
		})
	})
	s.catalog["apps/v1/deployments"] = Resource{Key: "apps/v1/deployments", Group: "apps", Version: "v1", Name: "deployments", Kind: "Deployment", Namespaced: true}
	ser, err := s.PromSeries(context.Background(), PromConfig{Namespace: "mon", Service: "prom", Port: "9090", Scheme: "http"}, "apps/v1/deployments", "default", "web", 3600)
	if err != nil {
		t.Fatal(err)
	}
	if !ser.Available || ser.Source != "prometheus" || ser.Members != 2 || len(ser.Points) != 2 || ser.Points[0].CPU != 250 || ser.Points[0].Mem != 1048576 || ser.CPURequest != 200 {
		t.Fatalf("%+v", ser)
	}
	if len(queries) != 2 || !strings.Contains(queries[0], `pod=~"web-1|web-2"`) || !strings.Contains(queries[0], `namespace="default"`) {
		t.Fatalf("queries: %v", queries)
	}
}

func TestPodUsageP95FromPrometheus(t *testing.T) {
	var queries []string
	s := newIntegrationSession(t, &recorder{}, nil, func(mux *http.ServeMux) {
		mux.HandleFunc("/api/v1/namespaces/mon/services/prom:9090/proxy/api/v1/query", func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query().Get("query")
			queries = append(queries, q)
			val := "0.25"
			if strings.Contains(q, "memory") {
				val = "104857600"
			}
			jsonOK(w, map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": []any{
				map[string]any{"metric": map[string]string{"namespace": "shop", "pod": "api-1"}, "value": []any{1700000000, val}},
				map[string]any{"metric": map[string]string{"namespace": "shop", "pod": "api-2"}, "value": []any{1700000000, "NaN"}},
			}}})
		})
	})
	u, err := s.PodUsageP95(context.Background(), PromConfig{Namespace: "mon", Service: "prom", Port: "9090", Scheme: "http"}, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got := u["shop/api-1"]; got.CPU != 250 || got.Mem != 104857600 {
		t.Fatalf("%+v", got)
	}
	if _, ok := u["shop/api-2"]; ok {
		t.Fatal("NaN samples must be skipped")
	}
	if len(queries) != 2 || !strings.Contains(queries[0], "quantile_over_time(0.95") || !strings.Contains(queries[0], "[24h]") || !strings.Contains(queries[1], "[24h:5m]") {
		t.Fatalf("queries: %v", queries)
	}
}

func TestContainerUsageMetricsServerMaxAcrossReplicas(t *testing.T) {
	s := newIntegrationSession(t, &recorder{}, nil, func(mux *http.ServeMux) {
		mux.HandleFunc("/apis/apps/v1/namespaces/default/deployments/web", func(w http.ResponseWriter, r *http.Request) {
			jsonOK(w, map[string]any{"kind": "Deployment", "apiVersion": "apps/v1", "metadata": map[string]any{"name": "web"}, "spec": map[string]any{"selector": map[string]any{"matchLabels": map[string]string{"app": "web"}}}})
		})
		mux.HandleFunc("/api/v1/namespaces/default/pods", func(w http.ResponseWriter, r *http.Request) {
			pod := func(n string) map[string]any {
				return map[string]any{"kind": "Pod", "apiVersion": "v1", "metadata": map[string]any{"name": n, "namespace": "default"}}
			}
			jsonOK(w, map[string]any{"kind": "PodList", "apiVersion": "v1", "items": []any{pod("web-1"), pod("web-2")}})
		})
		mux.HandleFunc("/apis/metrics.k8s.io/v1beta1/namespaces/default/pods", func(w http.ResponseWriter, r *http.Request) {
			c := func(n, cpu, mem string) map[string]any {
				return map[string]any{"name": n, "usage": map[string]string{"cpu": cpu, "memory": mem}}
			}
			jsonOK(w, map[string]any{"items": []any{
				map[string]any{"metadata": map[string]any{"name": "web-1"}, "containers": []any{c("app", "100m", "200Mi"), c("side", "5m", "10Mi")}},
				map[string]any{"metadata": map[string]any{"name": "web-2"}, "containers": []any{c("app", "300m", "100Mi")}},
				map[string]any{"metadata": map[string]any{"name": "other"}, "containers": []any{c("app", "9000m", "9Gi")}},
			}})
		})
	})
	s.catalog["apps/v1/deployments"] = Resource{Key: "apps/v1/deployments", Group: "apps", Version: "v1", Name: "deployments", Kind: "Deployment", Namespaced: true}
	u, src, err := s.ContainerUsage(context.Background(), "apps/v1/deployments", "default", "web", nil, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if u["app"].CPU != 300 || u["app"].Mem != 200<<20 || u["side"].CPU != 5 || !strings.Contains(src, "metrics-server") {
		t.Fatalf("max across replicas, ignoring pods outside the workload: %+v %q", u, src)
	}
}
