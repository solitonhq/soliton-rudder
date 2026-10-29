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

// Argo CD is driven through its Application custom resources, so no Argo server login is needed:
// the cluster credentials the user already has are enough.
var argoAppRes = Resource{Group: "argoproj.io", Version: "v1alpha1", Name: "applications", Kind: "Application", Namespaced: true}

type ArgoApp struct {
	Name           string `json:"name"`
	Namespace      string `json:"namespace"`
	Project        string `json:"project"`
	Repo           string `json:"repo"`
	Path           string `json:"path"` // path, or chart name for Helm sources
	TargetRevision string `json:"targetRevision"`
	DestServer     string `json:"destServer"`
	DestName       string `json:"destName"`
	DestNamespace  string `json:"destNamespace"`
	Sync           string `json:"sync"`   // Synced | OutOfSync | Unknown
	Health         string `json:"health"` // Healthy | Progressing | Degraded | Suspended | Missing | Unknown
	HealthMessage  string `json:"healthMessage"`
	Revision       string `json:"revision"`
	Auto           bool   `json:"auto"`
	Prune          bool   `json:"prune"`
	SelfHeal       bool   `json:"selfHeal"`
	Phase          string `json:"phase"` // last operation phase
	OpMessage      string `json:"opMessage"`
	OpFinished     string `json:"opFinished"`
	ReconciledAt   string `json:"reconciledAt"`
	Created        string `json:"created"`
	Resources      int    `json:"resources"`
	OutOfSync      int    `json:"outOfSync"`
	Unhealthy      int    `json:"unhealthy"`
	Errors         int    `json:"errors"` // error conditions
	MultiSource    bool   `json:"multiSource"`
}

type ArgoResource struct {
	Group     string `json:"group"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Sync      string `json:"sync"`
	Health    string `json:"health"`
	Message   string `json:"message"`
	Hook      bool   `json:"hook"`
}

type ArgoHistory struct {
	ID         int64  `json:"id"`
	Revision   string `json:"revision"`
	DeployedAt string `json:"deployedAt"`
	Source     string `json:"source"`
}

type ArgoCondition struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	At      string `json:"at"`
}

type ArgoOpResult struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type ArgoDetail struct {
	ArgoApp
	Sources     []string        `json:"sources"`
	Items       []ArgoResource  `json:"items"`
	History     []ArgoHistory   `json:"history"`
	Conditions  []ArgoCondition `json:"conditions"`
	OpResults   []ArgoOpResult  `json:"opResults"`
	SyncOptions []string        `json:"syncOptions"`
	Images      []string        `json:"images"`
}

func (s *Session) ArgoApps(ctx context.Context) ([]ArgoApp, error) {
	items, err := s.ListRaw(ctx, "argoproj.io", "v1alpha1", "applications", true)
	if err != nil {
		return nil, err
	}
	out := make([]ArgoApp, 0, len(items))
	for i := range items {
		out = append(out, parseArgoApp(&items[i]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Namespace+"/"+out[i].Name < out[j].Namespace+"/"+out[j].Name })
	return out, nil
}

func nstr(o map[string]any, f ...string) string {
	v, _, _ := unstructured.NestedString(o, f...)
	return v
}
func nbool(o map[string]any, f ...string) bool { v, _, _ := unstructured.NestedBool(o, f...); return v }

func parseArgoApp(o *unstructured.Unstructured) ArgoApp {
	a := ArgoApp{
		Name: o.GetName(), Namespace: o.GetNamespace(), Created: o.GetCreationTimestamp().UTC().Format(time.RFC3339),
		Project:       nstr(o.Object, "spec", "project"),
		DestServer:    nstr(o.Object, "spec", "destination", "server"),
		DestName:      nstr(o.Object, "spec", "destination", "name"),
		DestNamespace: nstr(o.Object, "spec", "destination", "namespace"),
		Sync:          orDefault(nstr(o.Object, "status", "sync", "status"), "Unknown"),
		Health:        orDefault(nstr(o.Object, "status", "health", "status"), "Unknown"),
		HealthMessage: nstr(o.Object, "status", "health", "message"),
		Revision:      nstr(o.Object, "status", "sync", "revision"),
		Phase:         nstr(o.Object, "status", "operationState", "phase"),
		OpMessage:     nstr(o.Object, "status", "operationState", "message"),
		OpFinished:    nstr(o.Object, "status", "operationState", "finishedAt"),
		ReconciledAt:  nstr(o.Object, "status", "reconciledAt"),
		Auto:          mapExists(o.Object, "spec", "syncPolicy", "automated"),
		Prune:         nbool(o.Object, "spec", "syncPolicy", "automated", "prune"),
		SelfHeal:      nbool(o.Object, "spec", "syncPolicy", "automated", "selfHeal"),
	}
	if src, ok, _ := unstructured.NestedMap(o.Object, "spec", "source"); ok {
		a.Repo, a.TargetRevision = nstr(src, "repoURL"), nstr(src, "targetRevision")
		a.Path = orDefault(nstr(src, "chart"), nstr(src, "path"))
	} else if srcs, _, _ := unstructured.NestedSlice(o.Object, "spec", "sources"); len(srcs) > 0 {
		a.MultiSource = true
		m, _ := srcs[0].(map[string]any)
		a.Repo, a.TargetRevision = nstr(m, "repoURL"), nstr(m, "targetRevision")
		a.Path = orDefault(nstr(m, "chart"), nstr(m, "path"))
	}
	res, _, _ := unstructured.NestedSlice(o.Object, "status", "resources")
	a.Resources = len(res)
	for _, r := range res {
		rm, _ := r.(map[string]any)
		if st := nstr(rm, "status"); st != "" && st != "Synced" {
			a.OutOfSync++
		}
		if h := nstr(rm, "health", "status"); h != "" && h != "Healthy" && h != "Progressing" {
			a.Unhealthy++
		}
	}
	conds, _, _ := unstructured.NestedSlice(o.Object, "status", "conditions")
	for _, c := range conds {
		cm, _ := c.(map[string]any)
		if strings.HasSuffix(nstr(cm, "type"), "Error") {
			a.Errors++
		}
	}
	return a
}

func mapExists(o map[string]any, f ...string) bool {
	_, ok, _ := unstructured.NestedFieldNoCopy(o, f...)
	return ok
}

func (s *Session) ArgoApp(ctx context.Context, ns, name string) (ArgoDetail, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	o, err := s.client(argoAppRes, ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return ArgoDetail{}, err
	}
	return parseArgoDetail(o), nil
}

func sourceString(m map[string]any) string {
	rev := nstr(m, "targetRevision")
	loc := orDefault(nstr(m, "chart"), nstr(m, "path"))
	return strings.TrimSpace(fmt.Sprintf("%s %s @ %s", nstr(m, "repoURL"), loc, rev))
}

func parseArgoDetail(o *unstructured.Unstructured) ArgoDetail {
	d := ArgoDetail{ArgoApp: parseArgoApp(o), Sources: []string{}, Items: []ArgoResource{}, History: []ArgoHistory{}, Conditions: []ArgoCondition{}, OpResults: []ArgoOpResult{}, SyncOptions: []string{}, Images: []string{}}
	if src, ok, _ := unstructured.NestedMap(o.Object, "spec", "source"); ok {
		d.Sources = append(d.Sources, sourceString(src))
	}
	srcs, _, _ := unstructured.NestedSlice(o.Object, "spec", "sources")
	for _, sv := range srcs {
		m, _ := sv.(map[string]any)
		d.Sources = append(d.Sources, sourceString(m))
	}
	d.SyncOptions, _, _ = unstructured.NestedStringSlice(o.Object, "spec", "syncPolicy", "syncOptions")
	if d.SyncOptions == nil {
		d.SyncOptions = []string{}
	}
	d.Images, _, _ = unstructured.NestedStringSlice(o.Object, "status", "summary", "images")
	if d.Images == nil {
		d.Images = []string{}
	}
	res, _, _ := unstructured.NestedSlice(o.Object, "status", "resources")
	for _, r := range res {
		rm, _ := r.(map[string]any)
		d.Items = append(d.Items, ArgoResource{
			Group: nstr(rm, "group"), Kind: nstr(rm, "kind"), Namespace: nstr(rm, "namespace"), Name: nstr(rm, "name"),
			Sync: nstr(rm, "status"), Health: nstr(rm, "health", "status"), Message: nstr(rm, "health", "message"), Hook: nbool(rm, "hook"),
		})
	}
	hist, _, _ := unstructured.NestedSlice(o.Object, "status", "history")
	for _, h := range hist {
		hm, _ := h.(map[string]any)
		id, _ := hm["id"].(int64)
		src := ""
		if sm, ok := hm["source"].(map[string]any); ok {
			src = sourceString(sm)
		}
		d.History = append(d.History, ArgoHistory{ID: id, Revision: nstr(hm, "revision"), DeployedAt: nstr(hm, "deployedAt"), Source: src})
	}
	sort.Slice(d.History, func(i, j int) bool { return d.History[i].ID > d.History[j].ID })
	conds, _, _ := unstructured.NestedSlice(o.Object, "status", "conditions")
	for _, c := range conds {
		cm, _ := c.(map[string]any)
		d.Conditions = append(d.Conditions, ArgoCondition{Type: nstr(cm, "type"), Message: nstr(cm, "message"), At: nstr(cm, "lastTransitionTime")})
	}
	results, _, _ := unstructured.NestedSlice(o.Object, "status", "operationState", "syncResult", "resources")
	for _, r := range results {
		rm, _ := r.(map[string]any)
		d.OpResults = append(d.OpResults, ArgoOpResult{Kind: nstr(rm, "kind"), Name: nstr(rm, "name"), Status: nstr(rm, "status"), Message: nstr(rm, "message")})
	}
	return d
}

// ArgoRefresh asks the application controller to re-compare with git (hard = also drop manifest caches).
func (s *Session) ArgoRefresh(ctx context.Context, ns, name string, hard bool) error {
	mode := "normal"
	if hard {
		mode = "hard"
	}
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{"argocd.argoproj.io/refresh": mode}}})
	return s.patchArgo(ctx, ns, name, patch)
}

type ArgoSyncOptions struct {
	Revision string `json:"revision"` // "" = the app's target revision
	Prune    bool   `json:"prune"`
	Force    bool   `json:"force"`
	DryRun   bool   `json:"dryRun"`
}

func (s *Session) patchArgo(ctx context.Context, ns, name string, patch []byte) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	_, err := s.client(argoAppRes, ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{FieldManager: fieldManager})
	return err
}

func (s *Session) argoBusy(ctx context.Context, ns, name string) (*unstructured.Unstructured, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	o, err := s.client(argoAppRes, ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if mapExists(o.Object, "operation") || nstr(o.Object, "status", "operationState", "phase") == "Running" {
		return nil, fmt.Errorf("a sync operation is already in progress for %s", name)
	}
	return o, nil
}

// ArgoSync starts a sync by setting the application's operation, exactly as the Argo CD API does.
func (s *Session) ArgoSync(ctx context.Context, ns, name string, o ArgoSyncOptions) error {
	if _, err := s.argoBusy(ctx, ns, name); err != nil {
		return err
	}
	return s.patchArgo(ctx, ns, name, syncPatch(o, nil))
}

func syncPatch(o ArgoSyncOptions, extra map[string]any) []byte {
	sync := map[string]any{"prune": o.Prune, "dryRun": o.DryRun}
	if o.Revision != "" {
		sync["revision"] = o.Revision
	}
	if o.Force {
		sync["syncStrategy"] = map[string]any{"apply": map[string]any{"force": true}}
	}
	for k, v := range extra {
		sync[k] = v
	}
	b, _ := json.Marshal(map[string]any{"operation": map[string]any{
		"initiatedBy": map[string]any{"username": "soliton-rudder"},
		"sync":        sync,
	}})
	return b
}

// ArgoRollback redeploys a recorded history entry. With auto-sync enabled Argo would immediately
// revert it, so it must be disabled (disableAuto) or the call is refused.
func (s *Session) ArgoRollback(ctx context.Context, ns, name string, historyID int64, prune, disableAuto bool) error {
	o, err := s.argoBusy(ctx, ns, name)
	if err != nil {
		return err
	}
	if mapExists(o.Object, "spec", "syncPolicy", "automated") && !disableAuto {
		return fmt.Errorf("auto-sync is enabled and would revert the rollback; disable it first")
	}
	hist, _, _ := unstructured.NestedSlice(o.Object, "status", "history")
	var entry map[string]any
	for _, h := range hist {
		hm, _ := h.(map[string]any)
		if id, _ := hm["id"].(int64); id == historyID {
			entry = hm
		}
	}
	if entry == nil {
		return fmt.Errorf("history entry %d not found", historyID)
	}
	extra := map[string]any{}
	if src, ok := entry["source"]; ok {
		extra["source"] = src
	}
	if srcs, ok := entry["sources"]; ok {
		extra["sources"] = srcs
		extra["revisions"] = entry["revisions"]
	}
	if disableAuto && mapExists(o.Object, "spec", "syncPolicy", "automated") {
		if err := s.patchArgo(ctx, ns, name, []byte(`{"spec":{"syncPolicy":{"automated":null}}}`)); err != nil {
			return err
		}
	}
	return s.patchArgo(ctx, ns, name, syncPatch(ArgoSyncOptions{Revision: nstr(entry, "revision"), Prune: prune}, extra))
}

// ArgoSetAutoSync toggles automated sync (keeping prune/selfHeal choices when enabling).
func (s *Session) ArgoSetAutoSync(ctx context.Context, ns, name string, enable, prune, selfHeal bool) error {
	var patch []byte
	if enable {
		patch, _ = json.Marshal(map[string]any{"spec": map[string]any{"syncPolicy": map[string]any{"automated": map[string]any{"prune": prune, "selfHeal": selfHeal}}}})
	} else {
		patch = []byte(`{"spec":{"syncPolicy":{"automated":null}}}`)
	}
	return s.patchArgo(ctx, ns, name, patch)
}
