package plugins

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"soliton-rudder/internal/kube"
)

type ClusterInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Host is what the application provides to the plugin broker.
type Host interface {
	Clusters() ([]ClusterInfo, error)
	Session(clusterID string) (*kube.Session, error)
	Toast(pluginID, kind, text string)
	Emit(pluginID, name string, data any)
}

// Grants answers whether a permission is currently granted to a plugin.
type Grants func(perm string) bool

// Broker implements the host API. Every method checks the caller's grants first, so the same
// enforcement applies to plugin backends and to their UI panels.
type Broker struct {
	host    Host
	storage *Storage
	http    *http.Client
}

func NewBroker(h Host, st *Storage) *Broker {
	return &Broker{host: h, storage: st, http: &http.Client{Timeout: 30 * time.Second}}
}

type kubeParams struct {
	Cluster    string `json:"cluster"`
	Group      string `json:"group"`
	Version    string `json:"version"`
	Resource   string `json:"resource"`
	Namespaced bool   `json:"namespaced"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	Limit      int    `json:"limit"`
	Replicas   int32  `json:"replicas"`
	Manifest   string `json:"manifest"`
	DryRun     bool   `json:"dryRun"`
	Force      bool   `json:"force"`
}

const maxListItems = 5000

func need(g Grants, perm string) error {
	if !g(perm) {
		return errorf(-32001, "permission denied: this plugin was not granted %q", perm)
	}
	return nil
}

func isSecrets(p kubeParams) bool {
	return p.Resource == "secrets" && (p.Group == "" || p.Group == "core")
}

func (b *Broker) session(p kubeParams) (*kube.Session, kube.Resource, error) {
	if p.Cluster == "" || p.Resource == "" || p.Version == "" {
		return nil, kube.Resource{}, errorf(-32602, "cluster, version and resource are required")
	}
	s, err := b.host.Session(p.Cluster)
	if err != nil {
		return nil, kube.Resource{}, err
	}
	return s, kube.ResourceRef(p.Group, p.Version, p.Resource, p.Namespaced), nil
}

// Call executes a host API method on behalf of a plugin.
func (b *Broker) Call(ctx context.Context, pluginID string, g Grants, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "clusters.list":
		if err := need(g, "clusters.read"); err != nil {
			return nil, err
		}
		return b.host.Clusters()

	case "kube.get", "kube.list":
		if err := need(g, "kube.read"); err != nil {
			return nil, err
		}
		var p kubeParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, errorf(-32602, "invalid params: %v", err)
		}
		if isSecrets(p) {
			if err := need(g, "secrets.read"); err != nil {
				return nil, err
			}
		}
		s, res, err := b.session(p)
		if err != nil {
			return nil, err
		}
		if method == "kube.get" {
			if p.Name == "" {
				return nil, errorf(-32602, "name is required")
			}
			return s.GetObject(ctx, res, p.Namespace, p.Name)
		}
		limit := p.Limit
		if limit <= 0 || limit > maxListItems {
			limit = maxListItems
		}
		items, more, err := s.ListObjects(ctx, res, p.Namespace, limit)
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items, "truncated": more}, nil

	case "kube.delete", "kube.scale", "kube.restart":
		if err := need(g, "kube.write"); err != nil {
			return nil, err
		}
		var p kubeParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, errorf(-32602, "invalid params: %v", err)
		}
		s, res, err := b.session(p)
		if err != nil {
			return nil, err
		}
		if p.Name == "" {
			return nil, errorf(-32602, "name is required")
		}
		switch method {
		case "kube.delete":
			err = s.DeleteObject(ctx, res, p.Namespace, p.Name)
		case "kube.scale":
			err = s.ScaleObject(ctx, res, p.Namespace, p.Name, p.Replicas)
		default:
			err = s.RestartObject(ctx, res, p.Namespace, p.Name)
		}
		return map[string]bool{"ok": err == nil}, err

	case "kube.apply":
		if err := need(g, "kube.write"); err != nil {
			return nil, err
		}
		var p kubeParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, errorf(-32602, "invalid params: %v", err)
		}
		if p.Cluster == "" || strings.TrimSpace(p.Manifest) == "" {
			return nil, errorf(-32602, "cluster and manifest are required")
		}
		s, err := b.host.Session(p.Cluster)
		if err != nil {
			return nil, err
		}
		if p.DryRun {
			return s.PreviewManifest(ctx, p.Manifest, p.Namespace, p.Force)
		}
		return s.ApplyManifest(ctx, p.Manifest, p.Namespace, p.Force)

	case "storage.get", "storage.set", "storage.delete", "storage.list":
		return b.storageCall(pluginID, method, raw)

	case "ui.notify":
		if err := need(g, "ui.notify"); err != nil {
			return nil, err
		}
		var p struct{ Kind, Text string }
		if err := json.Unmarshal(raw, &p); err != nil || p.Text == "" {
			return nil, errorf(-32602, "text is required")
		}
		if p.Kind != "error" {
			p.Kind = "success"
		}
		if len(p.Text) > 500 {
			p.Text = p.Text[:500] + "…"
		}
		b.host.Toast(pluginID, p.Kind, p.Text)
		return map[string]bool{"ok": true}, nil

	case "ui.emit":
		var p struct {
			Name string          `json:"name"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(raw, &p); err != nil || p.Name == "" {
			return nil, errorf(-32602, "name is required")
		}
		b.host.Emit(pluginID, p.Name, p.Data)
		return map[string]bool{"ok": true}, nil

	case "http.fetch":
		if err := need(g, "network"); err != nil {
			return nil, err
		}
		return b.fetch(ctx, raw)
	}
	return nil, errorf(-32601, "unknown method %q", method)
}

func (b *Broker) storageCall(id, method string, raw json.RawMessage) (any, error) {
	var p struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, errorf(-32602, "invalid params: %v", err)
		}
	}
	switch method {
	case "storage.get":
		v, ok, err := b.storage.Get(id, p.Key)
		if err != nil || !ok {
			return nil, err // null when missing
		}
		return v, nil
	case "storage.set":
		return map[string]bool{"ok": true}, b.storage.Set(id, p.Key, p.Value)
	case "storage.delete":
		return map[string]bool{"ok": true}, b.storage.Delete(id, p.Key)
	default:
		return b.storage.Keys(id)
	}
}

const maxFetchBody = 5 << 20

func (b *Broker) fetch(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		URL       string            `json:"url"`
		Method    string            `json:"method"`
		Headers   map[string]string `json:"headers"`
		Body      string            `json:"body"`
		TimeoutMs int               `json:"timeoutMs"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errorf(-32602, "invalid params: %v", err)
	}
	u, err := url.Parse(p.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errorf(-32602, "url must be http(s)")
	}
	method := strings.ToUpper(p.Method)
	if method == "" {
		method = http.MethodGet
	}
	if method != "GET" && method != "POST" && method != "PUT" && method != "PATCH" && method != "DELETE" && method != "HEAD" {
		return nil, errorf(-32602, "unsupported method")
	}
	timeout := 30 * time.Second
	if p.TimeoutMs > 0 && time.Duration(p.TimeoutMs)*time.Millisecond < timeout {
		timeout = time.Duration(p.TimeoutMs) * time.Millisecond
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, method, p.URL, strings.NewReader(p.Body))
	if err != nil {
		return nil, err
	}
	for k, v := range p.Headers {
		req.Header.Set(k, v)
	}
	resp, err := b.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxFetchBody {
		return nil, fmt.Errorf("response is larger than %d MB", maxFetchBody>>20)
	}
	hdr := map[string]string{}
	for k := range resp.Header {
		hdr[strings.ToLower(k)] = resp.Header.Get(k)
	}
	out := map[string]any{"status": resp.StatusCode, "headers": hdr}
	if utf8.Valid(body) {
		out["body"], out["encoding"] = string(body), "utf8"
	} else {
		out["body"], out["encoding"] = base64.StdEncoding.EncodeToString(body), "base64"
	}
	return out, nil
}
