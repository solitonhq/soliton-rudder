package feasibility

import (
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"soliton-rudder/internal/kube"
)

// A Fix is a ready-to-review change that resolves a finding: a (partial) manifest for server-side apply, or a
// delete. Fixes are generated on demand from the *live* object, never stored, and always go through the dry-run
// diff before anything is applied.

type FixTarget struct {
	Kind        string `json:"kind"`
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	ResourceKey string `json:"resourceKey"`
}

type Fix struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Action      string    `json:"action"` // apply | delete
	Manifest    string    `json:"manifest"`
	Risk        string    `json:"risk"` // low | medium | high
	Notes       []string  `json:"notes"`
	ManagedBy   string    `json:"managedBy"` // Helm | Argo CD | Flux | ""
	Target      FixTarget `json:"target"`
}

// FixContext carries what proposals may need beyond the object itself.
type FixContext struct {
	Usage       map[string]kube.Usage // observed usage per container name (max across pods); nil when unknown
	UsageSource string                // human-readable origin of Usage
	Cfg         Config
}

// RightSizeRuleID is the pseudo-rule used by the FinOps "right-size" action.
const RightSizeRuleID = "finops-rightsize"

var podSpecPaths = map[string][]string{
	"Deployment":  {"spec", "template", "spec"},
	"StatefulSet": {"spec", "template", "spec"},
	"DaemonSet":   {"spec", "template", "spec"},
	"CronJob":     {"spec", "jobTemplate", "spec", "template", "spec"},
}

// DetectManagedBy reports which tool owns an object's desired state (changes made here would be reverted by it).
func DetectManagedBy(o *unstructured.Unstructured) string {
	l, a := o.GetLabels(), o.GetAnnotations()
	switch {
	case l["app.kubernetes.io/managed-by"] == "Helm" || a["meta.helm.sh/release-name"] != "":
		return "Helm"
	case l["argocd.argoproj.io/instance"] != "" || a["argocd.argoproj.io/tracking-id"] != "":
		return "Argo CD"
	case l["kustomize.toolkit.fluxcd.io/name"] != "" || l["helm.toolkit.fluxcd.io/name"] != "":
		return "Flux"
	}
	return ""
}

func managedNote(tool string) string {
	switch tool {
	case "Helm":
		return "This workload is managed by Helm: the next `helm upgrade` will revert this change. Put it in the chart values for a lasting fix."
	case "Argo CD":
		return "This workload is managed by Argo CD: with auto-sync/self-heal the change will be reverted. Fix it in git for a lasting change."
	case "Flux":
		return "This workload is managed by Flux: the next reconciliation will revert this change. Fix it in the Flux source."
	}
	return ""
}

// partial builds {apiVersion, kind, metadata:{name,namespace}, <nested at path>} for server-side apply.
func partial(o *unstructured.Unstructured, path []string, leaf map[string]any) map[string]any {
	cur := leaf
	for i := len(path) - 1; i >= 0; i-- {
		cur = map[string]any{path[i]: cur}
	}
	cur["apiVersion"] = o.GetAPIVersion()
	cur["kind"] = o.GetKind()
	meta := map[string]any{"name": o.GetName()}
	if ns := o.GetNamespace(); ns != "" {
		meta["namespace"] = ns
	}
	cur["metadata"] = meta
	return cur
}

func toYAML(m map[string]any) string {
	b, _ := yaml.Marshal(m)
	return string(b)
}

func targetOf(o *unstructured.Unstructured, key string) FixTarget {
	return FixTarget{Kind: o.GetKind(), Namespace: o.GetNamespace(), Name: o.GetName(), ResourceKey: key}
}

func resourceKeyFor(o *unstructured.Unstructured) string {
	gv := strings.SplitN(o.GetAPIVersion(), "/", 2)
	group, version := "core", gv[0]
	if len(gv) == 2 {
		group, version = gv[0], gv[1]
	}
	plural := map[string]string{"Deployment": "deployments", "StatefulSet": "statefulsets", "DaemonSet": "daemonsets", "CronJob": "cronjobs", "Pod": "pods", "Namespace": "namespaces"}[o.GetKind()]
	return group + "/" + version + "/" + plural
}

func qtyStr(q resource.Quantity) string { return q.String() }

func cpuQty(m int64) string { return qtyStr(*resource.NewMilliQuantity(m, resource.DecimalSI)) }

// memQty rounds up to whole MiB so manifests read naturally (e.g. 307Mi, not 322122547).
func memQty(b int64) string {
	mi := (b + (1 << 20) - 1) >> 20
	return fmt.Sprintf("%dMi", mi)
}

func parseQty(c map[string]any, group, name string) (resource.Quantity, bool) {
	return qty(c, "resources", group, name)
}

// ProposeFixes returns the changes that would resolve a finding of ruleID on the given live object.
func ProposeFixes(ruleID string, o *unstructured.Unstructured, fc FixContext) []Fix {
	fc.Cfg = fc.Cfg.Normalize()
	kind := o.GetKind()
	key := resourceKeyFor(o)
	managed := DetectManagedBy(o)
	finish := func(f Fix) Fix {
		f.Target = targetOf(o, key)
		if f.Notes == nil {
			f.Notes = []string{}
		}
		if n := managedNote(managed); n != "" && f.Action == "apply" {
			f.Notes = append(f.Notes, n)
			f.ManagedBy = managed
		}
		return f
	}

	switch ruleID {
	case "failed-pods":
		if kind == "Pod" {
			return []Fix{finish(Fix{ID: "delete-pod", Title: "Delete the failed pod", Description: "Removes the leftover pod object. Its controller (if any) is unaffected; this does not restart anything.", Action: "delete", Risk: "low"})}
		}
		return nil
	case "no-network-policy":
		if kind != "Namespace" {
			return nil
		}
		np := map[string]any{
			"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy",
			"metadata": map[string]any{"name": "allow-same-namespace", "namespace": o.GetName()},
			"spec": map[string]any{
				"podSelector": map[string]any{}, "policyTypes": []any{"Ingress"},
				"ingress": []any{map[string]any{"from": []any{map[string]any{"podSelector": map[string]any{}}}}},
			},
		}
		return []Fix{finish(Fix{ID: "netpol-same-namespace", Title: "Isolate the namespace (allow only same-namespace ingress)",
			Description: "Creates a NetworkPolicy that lets pods in this namespace talk to each other and blocks ingress from every other namespace.",
			Action:      "apply", Manifest: toYAML(np), Risk: "high",
			Notes: []string{"This blocks incoming traffic from other namespaces, including your ingress controller, monitoring and service mesh. Add allow rules for those first, or apply it to a test namespace."}})}
	}

	path, ok := podSpecPaths[kind]
	if !ok {
		if kind == "Pod" && isFixable(ruleID) {
			return []Fix{{ID: "pod-immutable", Title: "Fix the controller instead", Description: "Most pod fields cannot be changed in place. Open the owning Deployment/StatefulSet and apply the fix there.", Action: "none", Risk: "low", Notes: []string{}, Target: targetOf(o, key)}}
		}
		return nil
	}
	spec, _, _ := unstructured.NestedMap(o.Object, path...)
	cs := containers(spec, false)
	if len(cs) == 0 {
		return nil
	}

	// containerPatch collects per-container changes and renders one partial manifest.
	patch := func(change func(c map[string]any) map[string]any) (string, []string) {
		var items []any
		var names []string
		for _, c := range cs {
			if m := change(c); m != nil {
				m["name"] = cName(c)
				items = append(items, m)
				names = append(names, cName(c))
			}
		}
		if len(items) == 0 {
			return "", nil
		}
		return toYAML(partial(o, path, map[string]any{"containers": items})), names
	}
	secFix := func(id, title, desc, field string, val any, risk string, notes ...string) []Fix {
		m, names := patch(func(c map[string]any) map[string]any {
			if cur, ok, _ := unstructured.NestedFieldNoCopy(c, "securityContext", field); ok && cur == val {
				return nil
			}
			return map[string]any{"securityContext": map[string]any{field: val}}
		})
		if m == "" {
			return nil
		}
		return []Fix{finish(Fix{ID: id, Title: title, Description: desc + " Containers: " + strings.Join(names, ", ") + ".", Action: "apply", Manifest: m, Risk: risk, Notes: notes})}
	}

	switch ruleID {
	case "privilege-escalation":
		return secFix("no-priv-esc", "Disable privilege escalation", "Sets allowPrivilegeEscalation: false.", "allowPrivilegeEscalation", false, "low")
	case "privileged":
		return secFix("no-privileged", "Drop privileged mode", "Sets privileged: false.", "privileged", false, "high",
			"If the container needs host access (device drivers, CNI/CSI agents) it will stop working. Grant specific capabilities instead.")
	case "no-run-as-nonroot":
		return secFix("run-non-root", "Require a non-root user", "Sets runAsNonRoot: true.", "runAsNonRoot", true, "high",
			"Pods fail to start (CreateContainerConfigError) if the image's user is root. Check the image's USER or also set runAsUser to a numeric id.")
	case "no-readonly-rootfs":
		var extra []string
		m, names := patch(func(c map[string]any) map[string]any {
			if v, _ := scBool(c, "readOnlyRootFilesystem"); v {
				return nil
			}
			return map[string]any{"securityContext": map[string]any{"readOnlyRootFilesystem": true}}
		})
		if m == "" {
			return nil
		}
		extra = append(extra, "Applications that write to the filesystem (temp files, caches, pid files) will fail. Mount an emptyDir for the paths they need (e.g. /tmp) before enabling this.")
		return []Fix{finish(Fix{ID: "readonly-rootfs", Title: "Make the root filesystem read-only", Description: "Sets readOnlyRootFilesystem: true. Containers: " + strings.Join(names, ", ") + ".", Action: "apply", Manifest: m, Risk: "high", Notes: extra})}
	case "no-seccomp":
		if podSecuritySeccomp(spec) {
			return nil
		}
		m := toYAML(partial(o, path, map[string]any{"securityContext": map[string]any{"seccompProfile": map[string]any{"type": "RuntimeDefault"}}}))
		return []Fix{finish(Fix{ID: "seccomp-default", Title: "Use the runtime's default seccomp profile", Description: "Sets securityContext.seccompProfile.type: RuntimeDefault on the pod.", Action: "apply", Manifest: m, Risk: "low",
			Notes: []string{"Workloads that need unusual syscalls (some profilers/debuggers) may be blocked."}})}
	case "no-requests":
		var notes []string
		m, names := patch(func(c map[string]any) map[string]any {
			_, hc := parseQty(c, "requests", "cpu")
			_, hm := parseQty(c, "requests", "memory")
			if hc && hm {
				return nil
			}
			cpuM, mem := int64(100), int64(128<<20)
			if u, ok := fc.Usage[cName(c)]; ok && (u.CPU > 0 || u.Mem > 0) {
				cpuM, mem = RightSize(u.CPU, u.Mem, fc.Cfg.HeadroomPct)
			}
			req := map[string]any{}
			if !hc {
				req["cpu"] = cpuM2(cpuM)
			}
			if !hm {
				req["memory"] = memQty(mem)
			}
			return map[string]any{"resources": map[string]any{"requests": req}}
		})
		if m == "" {
			return nil
		}
		if fc.Usage != nil {
			notes = append(notes, fmt.Sprintf("Sized from observed usage (%s) plus %.0f%% headroom.", fc.UsageSource, fc.Cfg.HeadroomPct))
		} else {
			notes = append(notes, "No usage data was available, so these are generic starting values (100m CPU, 128Mi memory). Review them.")
		}
		return []Fix{finish(Fix{ID: "add-requests", Title: "Add resource requests", Description: "Sets the missing CPU/memory requests. Containers: " + strings.Join(names, ", ") + ".", Action: "apply", Manifest: m, Risk: "medium", Notes: notes})}
	case "no-memory-limit":
		var notes []string
		m, names := patch(func(c map[string]any) map[string]any {
			if _, ok := parseQty(c, "limits", "memory"); ok {
				return nil
			}
			var base int64 = 512 << 20
			if q, ok := parseQty(c, "requests", "memory"); ok {
				base = q.Value()
			}
			if u, ok := fc.Usage[cName(c)]; ok && u.Mem*3/2 > base {
				base = u.Mem * 3 / 2
			}
			return map[string]any{"resources": map[string]any{"limits": map[string]any{"memory": memQty(base)}}}
		})
		if m == "" {
			return nil
		}
		notes = append(notes, "The limit is the memory request (or 1.5× observed usage if higher). A limit that is too low gets the container OOM-killed: review before applying.")
		return []Fix{finish(Fix{ID: "add-memory-limit", Title: "Add a memory limit", Description: "Containers: " + strings.Join(names, ", ") + ".", Action: "apply", Manifest: m, Risk: "medium", Notes: notes})}
	case "no-readiness-probe", "no-liveness-probe":
		field := "readinessProbe"
		if ruleID == "no-liveness-probe" {
			field = "livenessProbe"
		}
		var skipped []string
		m, names := patch(func(c map[string]any) map[string]any {
			if _, ok := c[field]; ok {
				return nil
			}
			if field == "livenessProbe" {
				if _, ok := c["startupProbe"]; ok {
					return nil
				}
			}
			ports, _ := c["ports"].([]any)
			if len(ports) == 0 {
				skipped = append(skipped, cName(c))
				return nil
			}
			p0, _ := ports[0].(map[string]any)
			port, _ := p0["containerPort"].(int64)
			probe := map[string]any{"tcpSocket": map[string]any{"port": port}, "periodSeconds": 10, "timeoutSeconds": 3, "failureThreshold": 3}
			if field == "livenessProbe" {
				probe["initialDelaySeconds"] = 30
			} else {
				probe["initialDelaySeconds"] = 5
			}
			return map[string]any{field: probe}
		})
		var notes []string
		if len(skipped) > 0 {
			notes = append(notes, "No fix for "+strings.Join(skipped, ", ")+": the container declares no ports, so there is nothing safe to probe.")
		}
		if m == "" {
			return nil
		}
		risk := "low"
		notes = append(notes, "A TCP probe only proves the port accepts connections. Replace it with an HTTP check of a real health endpoint when you have one.")
		if field == "livenessProbe" {
			risk = "medium"
			notes = append(notes, "A failing liveness probe restarts the container; the 30 s initial delay assumes a fast start. Add a startupProbe for slow-starting apps.")
		}
		return []Fix{finish(Fix{ID: "add-" + field, Title: "Add a TCP " + strings.TrimSuffix(field, "Probe") + " probe", Description: "On the first declared port. Containers: " + strings.Join(names, ", ") + ".", Action: "apply", Manifest: m, Risk: risk, Notes: notes})}
	case "single-replica":
		if kind != "Deployment" && kind != "StatefulSet" {
			return nil
		}
		m := toYAML(partial(o, []string{"spec"}, map[string]any{"replicas": 2}))
		return []Fix{finish(Fix{ID: "scale-2", Title: "Run 2 replicas", Description: "Sets spec.replicas: 2 so a node drain or crash does not cause downtime.", Action: "apply", Manifest: m, Risk: "medium",
			Notes: []string{"Doubles the workload's resource cost. Stateful or single-writer applications may not support two replicas. If an HorizontalPodAutoscaler controls this workload, change its minReplicas instead."}})}
	case "no-pdb":
		sel, _, _ := unstructured.NestedMap(o.Object, "spec", "selector")
		if len(sel) == 0 || (kind != "Deployment" && kind != "StatefulSet") {
			return nil
		}
		pdb := map[string]any{
			"apiVersion": "policy/v1", "kind": "PodDisruptionBudget",
			"metadata": map[string]any{"name": o.GetName() + "-pdb", "namespace": o.GetNamespace()},
			"spec":     map[string]any{"maxUnavailable": 1, "selector": sel},
		}
		f := finish(Fix{ID: "create-pdb", Title: "Create a PodDisruptionBudget (maxUnavailable: 1)", Description: "Lets node drains and upgrades evict one pod of this workload at a time.", Action: "apply", Manifest: toYAML(pdb), Risk: "low",
			Notes: []string{"With very few replicas a PDB can block a drain until a replacement is ready; that is the intended protection."}})
		f.Target = FixTarget{Kind: "PodDisruptionBudget", Namespace: o.GetNamespace(), Name: o.GetName() + "-pdb", ResourceKey: "policy/v1/poddisruptionbudgets"}
		return []Fix{f}
	case RightSizeRuleID, "overprovisioned":
		if fc.Usage == nil {
			return nil
		}
		var notes []string
		m, names := patch(func(c map[string]any) map[string]any {
			u, ok := fc.Usage[cName(c)]
			if !ok {
				return nil
			}
			rc, rm := RightSize(u.CPU, u.Mem, fc.Cfg.HeadroomPct)
			req := map[string]any{}
			if q, ok := parseQty(c, "requests", "cpu"); ok && q.MilliValue() > rc*13/10 { // only shrink when clearly oversized (>30%)
				req["cpu"] = cpuM2(rc)
			}
			if q, ok := parseQty(c, "requests", "memory"); ok && q.Value() > rm*13/10 {
				req["memory"] = memQty(rm)
			}
			if len(req) == 0 {
				return nil
			}
			return map[string]any{"resources": map[string]any{"requests": req}}
		})
		if m == "" {
			return nil
		}
		notes = append(notes, fmt.Sprintf("New requests = observed usage (%s) plus %.0f%% headroom. Lowering requests shrinks the scheduling reservation; make sure peaks are covered.", fc.UsageSource, fc.Cfg.HeadroomPct))
		return []Fix{finish(Fix{ID: "right-size", Title: "Right-size requests to observed usage", Description: "Containers: " + strings.Join(names, ", ") + ".", Action: "apply", Manifest: m, Risk: "medium", Notes: notes})}
	}
	return nil
}

func cpuM2(m int64) string { return cpuQty(m) }

func isFixable(id string) bool {
	for _, f := range FixableRules() {
		if f == id {
			return true
		}
	}
	return false
}

// FixableRules lists the rule ids ProposeFixes understands (the UI shows a Fix button only for these).
func FixableRules() []string {
	ids := []string{"failed-pods", "no-network-policy", "privilege-escalation", "privileged", "no-run-as-nonroot", "no-readonly-rootfs",
		"no-seccomp", "no-requests", "no-memory-limit", "no-readiness-probe", "no-liveness-probe", "single-replica", "no-pdb", "overprovisioned", RightSizeRuleID}
	sort.Strings(ids)
	return ids
}
