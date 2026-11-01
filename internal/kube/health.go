package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok | warn | fail
	Detail string `json:"detail"`
}

// HealthReport is a plain-language health summary of one object.
type HealthReport struct {
	Status  string   `json:"status"` // healthy | degraded | failing | unknown
	Summary string   `json:"summary"`
	Checks  []Check  `json:"checks"`
	Hints   []string `json:"hints"`
}

// Health inspects an object and explains what is wrong, if anything.
func (s *Session) Health(ctx context.Context, key, ns, name string) (HealthReport, error) {
	res, err := s.resource(key)
	if err != nil {
		return HealthReport{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	obj, err := s.client(res, ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return HealthReport{}, err
	}
	return buildHealth(res.Name, obj), nil
}

type reporter struct {
	checks []Check
	hints  []string
	seen   map[string]bool
}

func (r *reporter) add(status, name, detail string) {
	r.checks = append(r.checks, Check{Name: name, Status: status, Detail: detail})
}

func (r *reporter) hint(h string) {
	if r.seen == nil {
		r.seen = map[string]bool{}
	}
	if !r.seen[h] {
		r.seen[h] = true
		r.hints = append(r.hints, h)
	}
}

func buildHealth(resource string, obj *unstructured.Unstructured) HealthReport {
	r := &reporter{}
	if obj.GetDeletionTimestamp() != nil {
		r.add("warn", "Terminating", "The object is being deleted"+finalizerNote(obj))
	}
	switch resource {
	case "pods":
		podHealth(r, obj)
	case "deployments", "replicasets", "statefulsets":
		replicaHealth(r, obj, resource)
	case "daemonsets":
		daemonSetHealth(r, obj)
	case "nodes":
		nodeHealth(r, obj)
	case "jobs":
		jobHealth(r, obj)
	case "persistentvolumeclaims":
		pvcHealth(r, obj)
	default:
		conditionHealth(r, obj)
	}
	return finish(r)
}

func finish(r *reporter) HealthReport {
	rep := HealthReport{Status: "unknown", Checks: r.checks, Hints: r.hints}
	if rep.Checks == nil {
		rep.Checks = []Check{}
	}
	if rep.Hints == nil {
		rep.Hints = []string{}
	}
	var fails, warns int
	for _, c := range r.checks {
		switch c.Status {
		case "fail":
			fails++
		case "warn":
			warns++
		}
	}
	switch {
	case len(r.checks) == 0:
		rep.Summary = "No health information available for this kind."
	case fails > 0:
		rep.Status, rep.Summary = "failing", plural(fails, "problem")+" need attention"
		if fails == 1 {
			rep.Summary = "1 problem needs attention"
		}
	case warns > 0:
		rep.Status, rep.Summary = "degraded", plural(warns, "warning")
	default:
		rep.Status, rep.Summary = "healthy", "All checks passed"
	}
	return rep
}

func plural(n int, w string) string { return fmt.Sprintf("%d %ss", n, w) }

func finalizerNote(o *unstructured.Unstructured) string {
	if f := o.GetFinalizers(); len(f) > 0 {
		return " (waiting on finalizers: " + strings.Join(f, ", ") + ")"
	}
	return ""
}

var reasonHints = map[string]string{
	"CrashLoopBackOff":           "The container keeps exiting. Open Logs → Previous to see why it died, and check its exit code.",
	"ImagePullBackOff":           "The image can't be pulled. Verify the image name and tag, registry access and imagePullSecrets.",
	"ErrImagePull":               "The image can't be pulled. Verify the image name and tag, registry access and imagePullSecrets.",
	"InvalidImageName":           "The image reference is malformed. Check the image field.",
	"CreateContainerConfigError": "A referenced ConfigMap, Secret or key is missing. Check the pod's env/volume references.",
	"CreateContainerError":       "The runtime could not create the container. Check events for the underlying error.",
	"RunContainerError":          "The container failed to start. Check the command/entrypoint and events.",
	"OOMKilled":                  "The container exceeded its memory limit and was killed. Raise the limit or investigate memory growth.",
	"Evicted":                    "The pod was evicted, usually due to node pressure. Check node conditions and resource requests.",
	"Unschedulable":              "No node can run this pod. Check resource requests, nodeSelector/affinity, taints and node capacity.",
}

func podHealth(r *reporter, p *unstructured.Unstructured) {
	phase, _, _ := unstructured.NestedString(p.Object, "status", "phase")
	reason, _, _ := unstructured.NestedString(p.Object, "status", "reason")
	switch phase {
	case "Running", "Succeeded":
		r.add("ok", "Phase", phase)
	case "Pending":
		r.add("warn", "Phase", "Pending")
	case "Failed":
		r.add("fail", "Phase", "Failed"+suffix(reason))
		if reason == "Evicted" {
			r.hint(reasonHints["Evicted"])
		}
	default:
		r.add("warn", "Phase", orDefault(phase, "Unknown"))
	}

	conds, _, _ := unstructured.NestedSlice(p.Object, "status", "conditions")
	for _, c := range conds {
		cm, _ := c.(map[string]any)
		if str(cm["type"]) == "PodScheduled" && str(cm["status"]) == "False" {
			r.add("fail", "Scheduling", orDefault(str(cm["message"]), str(cm["reason"])))
			r.hint(reasonHints["Unschedulable"])
		}
	}

	limits := containerMemLimits(p)
	for _, group := range []struct{ field, label string }{{"initContainerStatuses", "Init container"}, {"containerStatuses", "Container"}} {
		sts, _, _ := unstructured.NestedSlice(p.Object, "status", group.field)
		for _, st := range sts {
			containerCheck(r, group.label, st.(map[string]any), limits)
		}
	}
	if phase == "Running" {
		ready := false
		for _, c := range conds {
			cm, _ := c.(map[string]any)
			if str(cm["type"]) == "Ready" {
				ready = str(cm["status"]) == "True"
			}
		}
		if ready {
			r.add("ok", "Ready", "All containers are ready")
		} else {
			r.add("warn", "Ready", "Pod is not ready; it receives no traffic")
		}
	}
}

func containerCheck(r *reporter, label string, st map[string]any, memLimits map[string]string) {
	name := str(st["name"])
	restarts, _ := st["restartCount"].(int64)
	title := fmt.Sprintf("%s %s", label, name)
	state, _ := st["state"].(map[string]any)

	if w, ok := state["waiting"].(map[string]any); ok {
		reason := str(w["reason"])
		switch reason {
		case "", "ContainerCreating", "PodInitializing":
			r.add("warn", title, orDefault(reason, "Waiting"))
		default:
			r.add("fail", title, reason+suffix(truncate(str(w["message"]), 160)))
			if h, ok := reasonHints[reason]; ok {
				r.hint(h)
			}
		}
	} else if t, ok := state["terminated"].(map[string]any); ok {
		code, _ := t["exitCode"].(int64)
		reason := str(t["reason"])
		if code == 0 {
			r.add("ok", title, "Completed")
		} else {
			r.add("fail", title, fmt.Sprintf("Exited with code %d%s", code, suffix(reason)))
			if h, ok := reasonHints[reason]; ok {
				r.hint(h)
			}
		}
	} else if _, ok := state["running"]; ok {
		if ready, _ := st["ready"].(bool); ready || label == "Init container" {
			r.add("ok", title, "Running")
		} else {
			r.add("warn", title, "Running but not ready (readiness probe failing?)")
		}
	}

	if restarts > 0 {
		last, _ := st["lastState"].(map[string]any)
		lt, _ := last["terminated"].(map[string]any)
		detail := fmt.Sprintf("%d restart%s", restarts, map[bool]string{true: "", false: "s"}[restarts == 1])
		lreason := str(lt["reason"])
		if lreason != "" {
			code, _ := lt["exitCode"].(int64)
			detail += fmt.Sprintf("; last exit: %s (code %d)", lreason, code)
		}
		status := "ok"
		if restarts >= 5 {
			status = "fail"
		} else if restarts >= 2 || lreason == "OOMKilled" {
			status = "warn"
		}
		if status != "ok" {
			r.add(status, title+" restarts", detail)
		}
		if lreason == "OOMKilled" {
			h := reasonHints["OOMKilled"]
			if l := memLimits[name]; l != "" {
				h += " Current limit: " + l + "."
			}
			r.hint(h)
		} else if restarts >= 3 {
			r.hint(reasonHints["CrashLoopBackOff"])
		}
	}
}

func containerMemLimits(p *unstructured.Unstructured) map[string]string {
	out := map[string]string{}
	cs, _, _ := unstructured.NestedSlice(p.Object, "spec", "containers")
	for _, c := range cs {
		cm, _ := c.(map[string]any)
		if res, ok := cm["resources"].(map[string]any); ok {
			if lim, ok := res["limits"].(map[string]any); ok {
				if m, ok := lim["memory"].(string); ok {
					out[str(cm["name"])] = m
				}
			}
		}
	}
	return out
}

func replicaHealth(r *reporter, o *unstructured.Unstructured, resource string) {
	desired, found, _ := unstructured.NestedInt64(o.Object, "spec", "replicas")
	if !found {
		desired = 1
	}
	ready, _, _ := unstructured.NestedInt64(o.Object, "status", "readyReplicas")
	updated, _, _ := unstructured.NestedInt64(o.Object, "status", "updatedReplicas")
	available, _, _ := unstructured.NestedInt64(o.Object, "status", "availableReplicas")

	switch {
	case desired == 0:
		r.add("warn", "Replicas", "Scaled to 0 — nothing is running")
	case ready == desired:
		r.add("ok", "Replicas", fmt.Sprintf("%d/%d ready", ready, desired))
	case ready == 0:
		r.add("fail", "Replicas", fmt.Sprintf("0/%d ready", desired))
		r.hint("No replica is ready. Open a pod of this workload and check its Health and Logs.")
	default:
		r.add("warn", "Replicas", fmt.Sprintf("%d/%d ready", ready, desired))
	}
	if resource == "deployments" {
		if updated < desired && desired > 0 {
			r.add("warn", "Rollout", fmt.Sprintf("%d/%d replicas updated — rollout in progress", updated, desired))
		}
		if available < desired && desired > 0 && ready == desired {
			r.add("warn", "Availability", fmt.Sprintf("%d/%d available (minReadySeconds not elapsed?)", available, desired))
		}
		if paused, _, _ := unstructured.NestedBool(o.Object, "spec", "paused"); paused {
			r.add("warn", "Paused", "Rollouts are paused")
		}
	}
	conditionHealth(r, o)
}

func daemonSetHealth(r *reporter, o *unstructured.Unstructured) {
	desired, _, _ := unstructured.NestedInt64(o.Object, "status", "desiredNumberScheduled")
	ready, _, _ := unstructured.NestedInt64(o.Object, "status", "numberReady")
	mis, _, _ := unstructured.NestedInt64(o.Object, "status", "numberMisscheduled")
	unavail, _, _ := unstructured.NestedInt64(o.Object, "status", "numberUnavailable")
	switch {
	case desired == 0:
		r.add("warn", "Pods", "No nodes are eligible")
	case ready == desired:
		r.add("ok", "Pods", fmt.Sprintf("%d/%d ready", ready, desired))
	case ready == 0:
		r.add("fail", "Pods", fmt.Sprintf("0/%d ready", desired))
	default:
		r.add("warn", "Pods", fmt.Sprintf("%d/%d ready", ready, desired))
	}
	if unavail > 0 {
		r.add("warn", "Unavailable", fmt.Sprintf("%d pods unavailable", unavail))
	}
	if mis > 0 {
		r.add("warn", "Misscheduled", fmt.Sprintf("%d pods run on nodes that should not host them", mis))
	}
}

func nodeHealth(r *reporter, n *unstructured.Unstructured) {
	conds, _, _ := unstructured.NestedSlice(n.Object, "status", "conditions")
	sawReady := false
	for _, c := range conds {
		cm, _ := c.(map[string]any)
		typ, st := str(cm["type"]), str(cm["status"])
		switch typ {
		case "Ready":
			sawReady = true
			if st == "True" {
				r.add("ok", "Ready", "Node is ready")
			} else {
				r.add("fail", "Ready", orDefault(str(cm["message"]), "Node is not ready"))
				r.hint("The node is not reporting Ready. Check kubelet, network and the node's events; pods on it may be rescheduled.")
			}
		case "MemoryPressure", "DiskPressure", "PIDPressure":
			if st == "True" {
				r.add("fail", typ, orDefault(str(cm["message"]), typ))
				r.hint("The node is under resource pressure and may evict pods. Free resources or add capacity.")
			} else {
				r.add("ok", typ, "No pressure")
			}
		case "NetworkUnavailable":
			if st == "True" {
				r.add("fail", typ, orDefault(str(cm["message"]), typ))
			}
		}
	}
	if !sawReady {
		r.add("warn", "Ready", "Node reports no Ready condition")
	}
	if un, _, _ := unstructured.NestedBool(n.Object, "spec", "unschedulable"); un {
		r.add("warn", "Schedulable", "Node is cordoned — no new pods will be scheduled")
	}
	if taints, _, _ := unstructured.NestedSlice(n.Object, "spec", "taints"); len(taints) > 0 {
		var ts []string
		for _, t := range taints {
			tm, _ := t.(map[string]any)
			ts = append(ts, fmt.Sprintf("%s=%s:%s", str(tm["key"]), str(tm["value"]), str(tm["effect"])))
		}
		sort.Strings(ts)
		r.add("ok", "Taints", strings.Join(ts, ", "))
	}
}

func jobHealth(r *reporter, o *unstructured.Unstructured) {
	succeeded, _, _ := unstructured.NestedInt64(o.Object, "status", "succeeded")
	failed, _, _ := unstructured.NestedInt64(o.Object, "status", "failed")
	active, _, _ := unstructured.NestedInt64(o.Object, "status", "active")
	conds, _, _ := unstructured.NestedSlice(o.Object, "status", "conditions")
	done := false
	for _, c := range conds {
		cm, _ := c.(map[string]any)
		if str(cm["status"]) != "True" {
			continue
		}
		switch str(cm["type"]) {
		case "Complete":
			r.add("ok", "Job", fmt.Sprintf("Completed (%d succeeded)", succeeded))
			done = true
		case "Failed":
			r.add("fail", "Job", orDefault(str(cm["message"]), str(cm["reason"])))
			done = true
		}
	}
	if !done {
		r.add("ok", "Job", fmt.Sprintf("Running: %d active, %d succeeded", active, succeeded))
	}
	if failed > 0 && !done {
		r.add("warn", "Retries", fmt.Sprintf("%d pod failure(s) so far", failed))
	}
}

func pvcHealth(r *reporter, o *unstructured.Unstructured) {
	phase, _, _ := unstructured.NestedString(o.Object, "status", "phase")
	switch phase {
	case "Bound":
		r.add("ok", "Phase", "Bound")
	case "Pending":
		r.add("warn", "Phase", "Pending — waiting for a volume")
		r.hint("Check that the StorageClass exists and can provision, or that a matching PersistentVolume is available.")
	case "Lost":
		r.add("fail", "Phase", "Lost — the bound volume is gone")
	}
}

// conditionHealth reports status.conditions: "True" is good except for negative-polarity types.
func conditionHealth(r *reporter, o *unstructured.Unstructured) {
	conds, _, _ := unstructured.NestedSlice(o.Object, "status", "conditions")
	for _, c := range conds {
		cm, _ := c.(map[string]any)
		typ, st := str(cm["type"]), str(cm["status"])
		if typ == "" {
			continue
		}
		bad := st == "False"
		if strings.HasSuffix(typ, "Pressure") || typ == "Failed" || typ == "ReplicaFailure" {
			bad = st == "True"
		}
		switch {
		case bad && (typ == "Available" || typ == "Ready" || typ == "ReplicaFailure" || typ == "Failed"):
			r.add("fail", typ, orDefault(str(cm["message"]), str(cm["reason"])))
		case bad && typ == "Progressing":
			r.add("fail", typ, orDefault(str(cm["message"]), str(cm["reason"])))
			if str(cm["reason"]) == "ProgressDeadlineExceeded" {
				r.hint("The rollout exceeded its progress deadline. Check the new pods' Health and Logs; roll back if needed.")
			}
		case bad:
			r.add("warn", typ, orDefault(str(cm["message"]), str(cm["reason"])))
		}
	}
}

func suffix(s string) string {
	if s == "" {
		return ""
	}
	return " (" + s + ")"
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ---- Timeline ----

// Timeline returns events for the object and the pods/replicasets it owns, newest first.
func (s *Session) Timeline(ctx context.Context, key, ns, name string) ([]Event, error) {
	res, err := s.resource(key)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	evRes := Resource{Group: "core", Version: "v1", Name: "events", Namespaced: true}
	opts := metav1.ListOptions{Limit: 1000}
	scope := ns
	// By default an object's events are those about an object of the same kind and name.
	matches := func(ev *unstructured.Unstructured) bool {
		io, _, _ := unstructured.NestedMap(ev.Object, "involvedObject")
		return str(io["kind"]) == res.Kind && str(io["name"]) == name
	}

	switch res.Name {
	case "nodes":
		scope = ""
		opts.FieldSelector = fmt.Sprintf("involvedObject.kind=Node,involvedObject.name=%s", name)
		matches = func(*unstructured.Unstructured) bool { return true }
	case "deployments", "statefulsets", "daemonsets", "replicasets", "jobs", "cronjobs":
		// the workload, its replica sets / jobs and their pods, found through ownerReferences
		if rel, err := s.relatedObjects(ctx, res, ns, name); err == nil {
			matches = func(ev *unstructured.Unstructured) bool { return rel.matches(*ev) }
		}
	}
	list, err := s.client(evRes, scope).List(ctx, opts)
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, 32)
	for _, it := range list.Items {
		if !matches(&it) {
			continue
		}
		io, _, _ := unstructured.NestedMap(it.Object, "involvedObject")
		out = append(out, eventFrom(&it, fmt.Sprintf("%s/%s", str(io["kind"]), str(io["name"]))))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last > out[j].Last })
	if len(out) > 200 {
		out = out[:200]
	}
	return out, nil
}

func eventFrom(it *unstructured.Unstructured, object string) Event {
	typ, _, _ := unstructured.NestedString(it.Object, "type")
	reason, _, _ := unstructured.NestedString(it.Object, "reason")
	msg, _, _ := unstructured.NestedString(it.Object, "message")
	count, _, _ := unstructured.NestedInt64(it.Object, "count")
	last, _, _ := unstructured.NestedString(it.Object, "lastTimestamp")
	if last == "" {
		last, _, _ = unstructured.NestedString(it.Object, "eventTime")
	}
	if last == "" {
		last = it.GetCreationTimestamp().UTC().Format(time.RFC3339)
	}
	src, _, _ := unstructured.NestedString(it.Object, "source", "component")
	return Event{Type: typ, Reason: reason, Message: strings.TrimSpace(msg), Count: count, Last: last, Source: src, Object: object}
}
