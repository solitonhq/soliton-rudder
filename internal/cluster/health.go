package cluster

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

const probeTimeout = 6 * time.Second

// Probe checks API-server reachability and auth by fetching /version.
func Probe(ctx context.Context, cfg *rest.Config) Health {
	c := rest.CopyConfig(cfg)
	c.Timeout = probeTimeout
	dc, err := discovery.NewDiscoveryClientForConfig(c)
	if err != nil {
		return Health{Status: "error", Error: err.Error()}
	}
	type res struct {
		v   string
		err error
	}
	ch := make(chan res, 1)
	start := time.Now()
	go func() {
		v, err := dc.ServerVersion()
		if err != nil {
			ch <- res{err: err}
			return
		}
		ch <- res{v: v.GitVersion}
	}()
	select {
	case <-ctx.Done():
		return Health{Status: "error", Error: ctx.Err().Error()}
	case r := <-ch:
		ms := time.Since(start).Milliseconds()
		switch {
		case r.err == nil:
			return Health{Status: "connected", Version: r.v, LatencyMs: ms}
		case apierrors.IsUnauthorized(r.err), apierrors.IsForbidden(r.err):
			return Health{Status: "unauthorized", Error: r.err.Error(), LatencyMs: ms}
		default:
			return Health{Status: "unreachable", Error: r.err.Error(), LatencyMs: ms}
		}
	}
}
