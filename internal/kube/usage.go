package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
)

// ContainerUsage returns observed usage per container name for the pods behind a pod/workload (the maximum across
// replicas, the conservative choice when sizing requests). It prefers Prometheus' 95th percentile when a
// Prometheus is given, and falls back to the current metrics-server sample. source describes where the data came from.
func (s *Session) ContainerUsage(ctx context.Context, key, ns, name string, prom *PromConfig, window time.Duration) (map[string]Usage, string, error) {
	res, err := s.resource(key)
	if err != nil {
		return nil, "", err
	}
	gctx, cancel := context.WithTimeout(ctx, opTimeout)
	pods, err := s.podsFor(gctx, res, ns, name)
	cancel()
	if err != nil {
		return nil, "", err
	}
	names := map[string]bool{}
	var list []string
	for i := range pods {
		names[pods[i].GetName()] = true
		list = append(list, pods[i].GetName())
	}
	if len(list) == 0 {
		return nil, "", fmt.Errorf("no pods to measure")
	}
	if prom != nil {
		if u, err := s.containerP95(ctx, *prom, ns, list, window); err == nil && len(u) > 0 {
			return u, fmt.Sprintf("Prometheus p95 over %dh", int(window.Hours())), nil
		}
	}
	u, err := s.containerMetricsServer(ctx, ns, names)
	if err != nil {
		return nil, "", err
	}
	if len(u) == 0 {
		return nil, "", fmt.Errorf("no usage data for these pods")
	}
	return u, "metrics-server (current sample)", nil
}

func (s *Session) containerP95(ctx context.Context, c PromConfig, ns string, pods []string, window time.Duration) (map[string]Usage, error) {
	if window < time.Hour {
		window = time.Hour
	}
	w := fmt.Sprintf("%dh", int(window.Hours()))
	sel := fmt.Sprintf(`namespace="%s",pod=~"%s",container!="",container!="POD"`, quoteLabel(ns), podRegex(pods))
	now := time.Now()
	mem, err := s.PromInstant(ctx, c, fmt.Sprintf(`max by (container) (quantile_over_time(0.95, container_memory_working_set_bytes{%s}[%s]))`, sel, w), now)
	if err != nil {
		return nil, err
	}
	cpu, err := s.PromInstant(ctx, c, fmt.Sprintf(`max by (container) (quantile_over_time(0.95, rate(container_cpu_usage_seconds_total{%s}[5m])[%s:5m]))`, sel, w), now)
	if err != nil {
		return nil, err
	}
	out := map[string]Usage{}
	for _, m := range mem {
		u := out[m.Metric["container"]]
		u.Mem = int64(m.Value)
		out[m.Metric["container"]] = u
	}
	for _, m := range cpu {
		u := out[m.Metric["container"]]
		u.CPU = int64(m.Value*1000 + 0.5)
		out[m.Metric["container"]] = u
	}
	return out, nil
}

func (s *Session) containerMetricsServer(ctx context.Context, ns string, pods map[string]bool) (map[string]Usage, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	raw, err := s.rc.Get().AbsPath("/apis/metrics.k8s.io/v1beta1/namespaces/" + ns + "/pods").Do(ctx).Raw()
	if err != nil {
		return nil, errors.New(noMetrics)
	}
	var list struct {
		Items []struct {
			Metadata   struct{ Name string } `json:"metadata"`
			Containers []struct {
				Name  string            `json:"name"`
				Usage map[string]string `json:"usage"`
			} `json:"containers"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	out := map[string]Usage{}
	for _, it := range list.Items {
		if !pods[it.Metadata.Name] {
			continue
		}
		for _, c := range it.Containers {
			cur := out[c.Name]
			if q, err := resource.ParseQuantity(c.Usage["cpu"]); err == nil && q.MilliValue() > cur.CPU {
				cur.CPU = q.MilliValue()
			}
			if q, err := resource.ParseQuantity(c.Usage["memory"]); err == nil && q.Value() > cur.Mem {
				cur.Mem = q.Value()
			}
			out[c.Name] = cur
		}
	}
	return out, nil
}
