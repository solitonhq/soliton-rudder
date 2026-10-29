package kube

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// Helm v3 keeps one Secret (or ConfigMap) per release revision. We read them directly, so release
// inspection needs neither the helm binary nor chart repositories.

type HelmRelease struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Revision     int    `json:"revision"`
	Status       string `json:"status"`
	Chart        string `json:"chart"`
	ChartVersion string `json:"chartVersion"`
	AppVersion   string `json:"appVersion"`
	Description  string `json:"description"`
	Updated      string `json:"updated"`
	Storage      string `json:"storage"` // secret | configmap
}

type HelmRevision struct {
	Revision     int    `json:"revision"`
	Status       string `json:"status"`
	ChartVersion string `json:"chartVersion"`
	AppVersion   string `json:"appVersion"`
	Description  string `json:"description"`
	Updated      string `json:"updated"`
}

type HelmObject struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type HelmDetail struct {
	HelmRelease
	Notes     string         `json:"notes"`
	Values    string         `json:"values"`    // user-supplied values (YAML)
	AllValues string         `json:"allValues"` // chart defaults merged with user values
	Manifest  string         `json:"manifest"`
	Objects   []HelmObject   `json:"objects"`
	History   []HelmRevision `json:"history"`
}

type helmRelFile struct {
	Name string `json:"name"`
	Info struct {
		Status        string `json:"status"`
		FirstDeployed string `json:"first_deployed"`
		LastDeployed  string `json:"last_deployed"`
		Description   string `json:"description"`
		Notes         string `json:"notes"`
	} `json:"info"`
	Chart struct {
		Metadata struct {
			Name       string `json:"name"`
			Version    string `json:"version"`
			AppVersion string `json:"appVersion"`
		} `json:"metadata"`
		Values map[string]any `json:"values"`
	} `json:"chart"`
	Config    map[string]any `json:"config"`
	Manifest  string         `json:"manifest"`
	Version   int            `json:"version"`
	Namespace string         `json:"namespace"`
}

// decodeHelmRelease reverses Helm's storage encoding: base64 → (gzip) → JSON. Secrets carry an extra
// base64 layer from Kubernetes itself (which the dynamic client leaves in place).
func decodeHelmRelease(raw string, secret bool) (*helmRelFile, error) {
	b := []byte(raw)
	var err error
	if secret {
		if b, err = base64.StdEncoding.DecodeString(raw); err != nil {
			return nil, fmt.Errorf("secret data: %w", err)
		}
	}
	if b, err = base64.StdEncoding.DecodeString(string(b)); err != nil {
		return nil, fmt.Errorf("release data: %w", err)
	}
	if len(b) > 2 && b[0] == 0x1f && b[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		if b, err = io.ReadAll(io.LimitReader(zr, 64<<20)); err != nil {
			return nil, err
		}
	}
	var rel helmRelFile
	if err := json.Unmarshal(b, &rel); err != nil {
		return nil, fmt.Errorf("release json: %w", err)
	}
	return &rel, nil
}

type helmMeta struct {
	name, ns, status, storage string
	revision                  int
	updated                   string
	objName                   string // object name holding the release
}

// listHelmMeta reads only object metadata (labels) of Helm storage objects: cheap even with big releases. For a user who
// can only read some namespaces it asks namespace by namespace.
func (s *Session) listHelmMeta(ctx context.Context) ([]helmMeta, error) {
	var out []helmMeta
	var firstErr error
	for _, st := range []struct {
		res     Resource
		storage string
	}{{helmGVR["secret"], "secret"}, {helmGVR["configmap"], "configmap"}} {
		okTargets := 0
		targets := s.listTargets(ctx, st.res, "")
		for _, target := range targets {
			cont := ""
			failed := false
			for {
				req := s.rc.Get().AbsPath(apiPathNS(st.res, target)).SetHeader("Accept", "application/json;as=PartialObjectMetadataList;g=meta.k8s.io;v=v1,application/json").
					Param("labelSelector", "owner=helm").Param("limit", "500")
				if cont != "" {
					req = req.Param("continue", cont)
				}
				cctx, cancel := context.WithTimeout(ctx, 2*opTimeout)
				raw, err := req.Do(cctx).Raw()
				cancel()
				if err != nil {
					if st.storage == "secret" && firstErr == nil {
						firstErr = err
					}
					failed = true
					break
				}
				var list struct {
					Metadata metav1.ListMeta `json:"metadata"`
					Items    []struct {
						Metadata metav1.ObjectMeta `json:"metadata"`
					} `json:"items"`
				}
				if err := json.Unmarshal(raw, &list); err != nil {
					failed = true
					break
				}
				for _, it := range list.Items {
					l := it.Metadata.Labels
					rev, _ := strconv.Atoi(l["version"])
					if l["name"] == "" || rev == 0 {
						continue
					}
					upd := it.Metadata.CreationTimestamp.UTC().Format(time.RFC3339)
					if m, err := strconv.ParseInt(l["modifiedAt"], 10, 64); err == nil {
						upd = time.Unix(m, 0).UTC().Format(time.RFC3339)
					}
					ns := it.Metadata.Namespace
					if ns == "" {
						ns = target
					}
					out = append(out, helmMeta{name: l["name"], ns: ns, status: l["status"], storage: st.storage, revision: rev, updated: upd, objName: it.Metadata.Name})
				}
				cont = list.Metadata.Continue
				if cont == "" {
					break
				}
			}
			if !failed {
				okTargets++
			}
		}
		if st.storage == "secret" && okTargets > 0 {
			firstErr = nil // some namespaces could be read: a forbidden one is not an error
		}
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

var helmGVR = map[string]Resource{
	"secret":    {Group: "core", Version: "v1", Name: "secrets", Namespaced: true},
	"configmap": {Group: "core", Version: "v1", Name: "configmaps", Namespaced: true},
}

func (s *Session) loadHelmRelease(ctx context.Context, m helmMeta) (*helmRelFile, error) {
	cctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	o, err := s.client(helmGVR[m.storage], m.ns).Get(cctx, m.objName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	raw, _, _ := unstructured.NestedString(o.Object, "data", "release")
	return decodeHelmRelease(raw, m.storage == "secret")
}

func releaseFrom(m helmMeta, rel *helmRelFile) HelmRelease {
	r := HelmRelease{Name: m.name, Namespace: m.ns, Revision: m.revision, Status: m.status, Updated: m.updated, Storage: m.storage}
	if rel != nil {
		r.Chart, r.ChartVersion, r.AppVersion = rel.Chart.Metadata.Name, rel.Chart.Metadata.Version, rel.Chart.Metadata.AppVersion
		r.Description = rel.Info.Description
		if rel.Info.Status != "" {
			r.Status = rel.Info.Status
		}
		if rel.Info.LastDeployed != "" {
			r.Updated = rel.Info.LastDeployed
		}
	}
	return r
}

// HelmReleases lists the latest revision of every release.
func (s *Session) HelmReleases(ctx context.Context) ([]HelmRelease, error) {
	metas, err := s.listHelmMeta(ctx)
	if err != nil {
		return nil, err
	}
	latest := map[string]helmMeta{}
	for _, m := range metas {
		k := m.ns + "/" + m.name
		if cur, ok := latest[k]; !ok || m.revision > cur.revision {
			latest[k] = m
		}
	}
	out := make([]HelmRelease, len(latest))
	keys := make([]string, 0, len(latest))
	for k := range latest {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, k := range keys {
		wg.Add(1)
		go func(i int, m helmMeta) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			rel, _ := s.cachedRelease(ctx, m)
			out[i] = releaseFrom(m, rel)
		}(i, latest[k])
	}
	wg.Wait()
	return out, nil
}

var helmCache sync.Map // "ns/obj" -> *helmRelFile; revisions are immutable, so entries never go stale

func (s *Session) cachedRelease(ctx context.Context, m helmMeta) (*helmRelFile, error) {
	k := s.clusterID + "/" + m.storage + "/" + m.ns + "/" + m.objName
	if v, ok := helmCache.Load(k); ok {
		return v.(*helmRelFile), nil
	}
	rel, err := s.loadHelmRelease(ctx, m)
	if err == nil {
		helmCache.Store(k, rel)
	}
	return rel, err
}

// HelmDetail returns one revision (0 = latest) with values, manifest, notes and history.
func (s *Session) HelmDetail(ctx context.Context, ns, name string, revision int) (HelmDetail, error) {
	metas, err := s.listHelmMeta(ctx)
	if err != nil {
		return HelmDetail{}, err
	}
	var mine []helmMeta
	for _, m := range metas {
		if m.ns == ns && m.name == name {
			mine = append(mine, m)
		}
	}
	if len(mine) == 0 {
		return HelmDetail{}, fmt.Errorf("release %s/%s not found", ns, name)
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].revision > mine[j].revision })
	pick := mine[0]
	if revision > 0 {
		found := false
		for _, m := range mine {
			if m.revision == revision {
				pick, found = m, true
			}
		}
		if !found {
			return HelmDetail{}, fmt.Errorf("revision %d not found", revision)
		}
	}
	rel, err := s.cachedRelease(ctx, pick)
	if err != nil {
		return HelmDetail{}, err
	}
	d := HelmDetail{HelmRelease: releaseFrom(pick, rel), Notes: rel.Info.Notes, Manifest: rel.Manifest, Objects: []HelmObject{}, History: []HelmRevision{}}
	if len(rel.Config) > 0 {
		b, _ := yaml.Marshal(rel.Config)
		d.Values = string(b)
	}
	b, _ := yaml.Marshal(mergeValues(rel.Chart.Values, rel.Config))
	d.AllValues = string(b)
	if docs, err := ParseManifest(rel.Manifest); err == nil {
		for _, o := range docs {
			ns := o.GetNamespace()
			if ns == "" {
				ns = pick.ns
			}
			d.Objects = append(d.Objects, HelmObject{Kind: o.GetKind(), Namespace: ns, Name: o.GetName()})
		}
	}
	// history: newest 25 revisions
	hist := mine
	if len(hist) > 25 {
		hist = hist[:25]
	}
	d.History = make([]HelmRevision, len(hist))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, m := range hist {
		wg.Add(1)
		go func(i int, m helmMeta) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r, _ := s.cachedRelease(ctx, m)
			hr := releaseFrom(m, r)
			d.History[i] = HelmRevision{Revision: m.revision, Status: hr.Status, ChartVersion: hr.ChartVersion, AppVersion: hr.AppVersion, Description: hr.Description, Updated: hr.Updated}
		}(i, m)
	}
	wg.Wait()
	return d, nil
}

// mergeValues overlays user values on chart defaults (maps merge recursively; everything else replaces).
func mergeValues(base, over map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		if om, ok := v.(map[string]any); ok {
			if bm, ok := out[k].(map[string]any); ok {
				out[k] = mergeValues(bm, om)
				continue
			}
		}
		out[k] = v
	}
	return out
}
