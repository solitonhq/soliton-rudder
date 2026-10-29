package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Prometheus is reached through the API server's service proxy, so it works with the user's existing
// cluster credentials and needs no port-forward or ingress.

type PromConfig struct {
	Namespace string `json:"namespace"`
	Service   string `json:"service"`
	Port      string `json:"port"`   // number or port name
	Scheme    string `json:"scheme"` // http | https
	Path      string `json:"path"`   // optional prefix, e.g. /prometheus
}

func (c PromConfig) valid() error {
	if c.Namespace == "" || c.Service == "" || c.Port == "" {
		return fmt.Errorf("Prometheus is not configured")
	}
	return nil
}

func (c PromConfig) proxyPath(api string) string {
	scheme := c.Scheme
	if scheme == "" || scheme == "http" {
		scheme = ""
	} else {
		scheme += ":"
	}
	return fmt.Sprintf("/api/v1/namespaces/%s/services/%s%s:%s/proxy%s%s", c.Namespace, scheme, c.Service, c.Port, strings.TrimRight(c.Path, "/"), api)
}

type PromCandidate struct {
	PromConfig
	Headless bool `json:"headless"`
}

var promSkip = regexp.MustCompile(`alertmanager|operator|exporter|pushgateway|adapter|kube-state|blackbox|node-exporter|thanos|grafana|webhook|admission`)

// PromDetect finds Prometheus services (kube-prometheus-stack, prometheus-operator, plain charts).
func (s *Session) PromDetect(ctx context.Context) ([]PromCandidate, error) {
	items, err := s.ListRaw(ctx, "core", "v1", "services", true)
	if err != nil {
		return nil, err
	}
	var out []PromCandidate
	for i := range items {
		sv := &items[i]
		name := strings.ToLower(sv.GetName())
		l := sv.GetLabels()
		isProm := l["app.kubernetes.io/name"] == "prometheus" || l["app"] == "prometheus" || strings.Contains(name, "prometheus")
		if !isProm || promSkip.MatchString(name) {
			continue
		}
		ports, _, _ := unstructured.NestedSlice(sv.Object, "spec", "ports")
		best := ""
		for _, p := range ports {
			pm, _ := p.(map[string]any)
			pn, _ := pm["name"].(string)
			num, _ := pm["port"].(int64)
			switch {
			case pn == "web" || pn == "http-web" || pn == "http" || pn == "prometheus":
				best = strconv.FormatInt(num, 10)
			case num == 9090 && best == "":
				best = "9090"
			}
			if best != "" && (pn == "web" || pn == "http-web") {
				break
			}
		}
		if best == "" {
			continue
		}
		cip, _, _ := unstructured.NestedString(sv.Object, "spec", "clusterIP")
		out = append(out, PromCandidate{PromConfig: PromConfig{Namespace: sv.GetNamespace(), Service: sv.GetName(), Port: best, Scheme: "http"}, Headless: cip == "None"})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Headless != out[j].Headless {
			return !out[i].Headless
		}
		return out[i].Namespace+"/"+out[i].Service < out[j].Namespace+"/"+out[j].Service
	})
	return out, nil
}

type promResp struct {
	Status    string `json:"status"`
	ErrorType string `json:"errorType"`
	Error     string `json:"error"`
	Data      struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Values [][2]any          `json:"values"`
			Value  [2]any            `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

type PromSeries struct {
	Metric map[string]string `json:"metric"`
	Points [][2]float64      `json:"points"` // [unix seconds, value]
}

type PromResult struct {
	Series    []PromSeries `json:"series"`
	Truncated bool         `json:"truncated"`
}

const (
	promMaxSeries = 50
	promMaxPoints = 1000
)

func (s *Session) promGet(ctx context.Context, c PromConfig, api string, params url.Values) (*promResp, error) {
	if err := c.valid(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req := s.rc.Get().AbsPath(c.proxyPath(api))
	for k, vs := range params {
		for _, v := range vs {
			req = req.Param(k, v)
		}
	}
	raw, err := req.Do(ctx).Raw()
	if err != nil {
		return nil, fmt.Errorf("prometheus request failed: %w", err)
	}
	var r promResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("unexpected response from Prometheus (is the service/port correct?)")
	}
	if r.Status != "success" {
		return &r, fmt.Errorf("prometheus: %s", orDefault(r.Error, r.ErrorType))
	}
	return &r, nil
}

// PromVersion verifies connectivity and returns the Prometheus version.
func (s *Session) PromVersion(ctx context.Context, c PromConfig) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := c.valid(); err != nil {
		return "", err
	}
	raw, err := s.rc.Get().AbsPath(c.proxyPath("/api/v1/status/buildinfo")).Do(cctx).Raw()
	if err != nil {
		return "", fmt.Errorf("cannot reach Prometheus: %w", err)
	}
	var r struct {
		Status string `json:"status"`
		Data   struct{ Version string }
	}
	if json.Unmarshal(raw, &r) != nil || r.Status != "success" {
		return "", fmt.Errorf("that service does not look like Prometheus")
	}
	return r.Data.Version, nil
}

// PromQueryRange runs a PromQL range query.
func (s *Session) PromQueryRange(ctx context.Context, c PromConfig, query string, start, end time.Time, step time.Duration) (PromResult, error) {
	if step < time.Second {
		step = time.Second
	}
	r, err := s.promGet(ctx, c, "/api/v1/query_range", url.Values{
		"query": {query}, "start": {strconv.FormatInt(start.Unix(), 10)}, "end": {strconv.FormatInt(end.Unix(), 10)}, "step": {strconv.FormatFloat(step.Seconds(), 'f', -1, 64)},
	})
	if err != nil {
		return PromResult{}, err
	}
	out := PromResult{Series: []PromSeries{}}
	for i, it := range r.Data.Result {
		if i >= promMaxSeries {
			out.Truncated = true
			break
		}
		ps := PromSeries{Metric: it.Metric, Points: make([][2]float64, 0, len(it.Values))}
		vals := it.Values
		if it.Values == nil && it.Value[0] != nil { // instant vector fallback
			vals = [][2]any{it.Value}
		}
		stride := 1
		if len(vals) > promMaxPoints {
			stride = len(vals)/promMaxPoints + 1
		}
		for j := 0; j < len(vals); j += stride {
			ts, _ := vals[j][0].(float64)
			v, err := strconv.ParseFloat(fmt.Sprint(vals[j][1]), 64)
			if err != nil || v != v { // skip NaN
				continue
			}
			ps.Points = append(ps.Points, [2]float64{ts, v})
		}
		out.Series = append(out.Series, ps)
	}
	return out, nil
}

func quoteLabel(s string) string { return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) }

func podRegex(pods []string) string {
	esc := make([]string, len(pods))
	for i, p := range pods {
		esc[i] = regexp.QuoteMeta(p)
	}
	return quoteLabel(strings.Join(esc, "|"))
}

// PromSeries builds a usage history for a pod or workload from cAdvisor metrics.
func (s *Session) PromSeries(ctx context.Context, c PromConfig, key, ns, name string, rangeSecs int) (Series, error) {
	res, err := s.resource(key)
	if err != nil {
		return Series{}, err
	}
	out := Series{Source: "prometheus", Points: []Point{}}
	switch res.Name {
	case "pods", "deployments", "statefulsets", "daemonsets", "replicasets", "jobs", "replicationcontrollers", "services":
	default:
		return out, fmt.Errorf("Prometheus history is not available for %s", res.Kind)
	}
	gctx, cancel := context.WithTimeout(ctx, opTimeout)
	pods, err := s.podsFor(gctx, res, ns, name)
	cancel()
	if err != nil {
		return out, err
	}
	var names []string
	for i := range pods {
		names = append(names, pods[i].GetName())
		addPodSizing(&out, &pods[i])
	}
	out.Members = len(names)
	if len(names) == 0 {
		out.Available = true
		return out, nil
	}
	sel := fmt.Sprintf(`namespace="%s",pod=~"%s",container!="",container!="POD"`, quoteLabel(ns), podRegex(names))
	end := time.Now()
	start := end.Add(-time.Duration(rangeSecs) * time.Second)
	step := time.Duration(rangeSecs/120) * time.Second
	if step < 15*time.Second {
		step = 15 * time.Second
	}
	out.IntervalSecs = int(step / time.Second)
	cpu, err := s.PromQueryRange(ctx, c, fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{%s}[2m]))*1000`, sel), start, end, step)
	if err != nil {
		out.Error = err.Error()
		return out, nil
	}
	mem, err := s.PromQueryRange(ctx, c, fmt.Sprintf(`sum(container_memory_working_set_bytes{%s})`, sel), start, end, step)
	if err != nil {
		out.Error = err.Error()
		return out, nil
	}
	byT := map[int64]*Point{}
	add := func(r PromResult, set func(p *Point, v float64)) {
		for _, ser := range r.Series {
			for _, pt := range ser.Points {
				t := int64(pt[0]) * 1000
				p := byT[t]
				if p == nil {
					p = &Point{T: t}
					byT[t] = p
				}
				set(p, pt[1])
			}
		}
	}
	add(cpu, func(p *Point, v float64) { p.CPU = int64(v + 0.5) })
	add(mem, func(p *Point, v float64) { p.Mem = int64(v) })
	for _, p := range byT {
		out.Points = append(out.Points, *p)
	}
	sort.Slice(out.Points, func(i, j int) bool { return out.Points[i].T < out.Points[j].T })
	out.Available = len(out.Points) > 0
	if !out.Available {
		out.Error = "Prometheus returned no data for these pods (are cAdvisor metrics scraped?)"
	}
	return out, nil
}

// PromSample is one element of an instant vector.
type PromSample struct {
	Metric map[string]string
	Value  float64
}

const promMaxSamples = 50000

// PromInstant runs an instant query and returns the whole vector (large, unlike PromQueryRange's chart-oriented cap).
func (s *Session) PromInstant(ctx context.Context, c PromConfig, query string, at time.Time) ([]PromSample, error) {
	r, err := s.promGet(ctx, c, "/api/v1/query", url.Values{"query": {query}, "time": {strconv.FormatInt(at.Unix(), 10)}})
	if err != nil {
		return nil, err
	}
	out := make([]PromSample, 0, len(r.Data.Result))
	for i, it := range r.Data.Result {
		if i >= promMaxSamples {
			break
		}
		v, err := strconv.ParseFloat(fmt.Sprint(it.Value[1]), 64)
		if err != nil || v != v {
			continue
		}
		out = append(out, PromSample{Metric: it.Metric, Value: v})
	}
	return out, nil
}

// PodUsageP95 returns each pod's 95th-percentile CPU (millicores) and memory (bytes) over the window,
// from cAdvisor metrics, keyed "namespace/pod".
func (s *Session) PodUsageP95(ctx context.Context, c PromConfig, window time.Duration) (map[string]Usage, error) {
	w := fmt.Sprintf("%dh", int(window.Hours()))
	if window < time.Hour {
		w = "1h"
	}
	sel := `container!="",container!="POD"`
	cpuQ := fmt.Sprintf(`sum by (namespace, pod) (quantile_over_time(0.95, rate(container_cpu_usage_seconds_total{%s}[5m])[%s:5m]))`, sel, w)
	memQ := fmt.Sprintf(`sum by (namespace, pod) (quantile_over_time(0.95, container_memory_working_set_bytes{%s}[%s]))`, sel, w)
	now := time.Now()
	mem, err := s.PromInstant(ctx, c, memQ, now)
	if err != nil {
		return nil, err
	}
	cpu, err := s.PromInstant(ctx, c, cpuQ, now)
	if err != nil {
		return nil, err
	}
	out := map[string]Usage{}
	for _, m := range mem {
		k := m.Metric["namespace"] + "/" + m.Metric["pod"]
		u := out[k]
		u.Mem = int64(m.Value)
		out[k] = u
	}
	for _, c := range cpu {
		k := c.Metric["namespace"] + "/" + c.Metric["pod"]
		u := out[k]
		u.CPU = int64(c.Value*1000 + 0.5)
		out[k] = u
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("Prometheus returned no cAdvisor data (are container_* metrics scraped?)")
	}
	return out, nil
}
