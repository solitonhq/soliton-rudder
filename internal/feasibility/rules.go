package feasibility

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"

	"soliton-rudder/internal/kube"
)

// Outcome is what a rule returns: how many objects it looked at and what it found.
type Outcome struct {
	Checked  int
	Findings []Finding
	Skipped  string
}

type Rule struct {
	ID          string
	Category    Category
	Title       string
	Description string
	Remediation string
	Needs       []string // snapshot lists the rule depends on
	Run         func(*Snapshot) Outcome
}

// Rules is the built-in rule set, in display order.
var Rules = []Rule{
	// ---- Resources ----
	{ID: "node-request-saturation", Category: CatResources, Title: "Node resource requests near capacity",
		Description: "Pod CPU/memory requests are close to (or above) a node's allocatable resources, leaving no room to schedule or burst.",
		Remediation: "Add nodes, move workloads, or right-size requests. Above 100% pods stay Pending.", Needs: []string{"nodes", "pods"}, Run: ruleNodeSaturation},
	{ID: "cluster-headroom", Category: CatResources, Title: "Capacity to survive losing a node",
		Description: "Checks whether the remaining nodes could host all requested resources if the largest node failed.",
		Remediation: "Add capacity so requests fit without the largest node (N+1), or reduce requests.", Needs: []string{"nodes", "pods"}, Run: ruleHeadroom},
	{ID: "no-requests", Category: CatResources, Title: "Containers without resource requests",
		Description: "Containers with no CPU or memory request are scheduled blindly and are the first to be evicted.",
		Remediation: "Set resources.requests for cpu and memory on every container.", Run: ruleNoRequests},
	{ID: "no-memory-limit", Category: CatResources, Title: "Containers without a memory limit",
		Description: "A container without a memory limit can consume a whole node's memory and trigger evictions.",
		Remediation: "Set resources.limits.memory (usually close to the request).", Run: ruleNoMemLimit},
	{ID: "memory-near-limit", Category: CatResources, Title: "Pods using most of their memory limit",
		Description: "Pods running at 90%+ of their memory limit are at risk of being OOMKilled.",
		Remediation: "Raise the memory limit or reduce memory use.", Needs: []string{"pods"}, Run: ruleMemNearLimit},
	{ID: "overprovisioned", Category: CatResources, Title: "Over-provisioned pods",
		Description: "Pods using under 10% of their CPU or memory requests waste schedulable capacity.",
		Remediation: "Lower the requests to match real usage (leave headroom for peaks).", Needs: []string{"pods"}, Run: ruleOverprovisioned},

	// ---- Reliability ----
	{ID: "node-health", Category: CatReliability, Title: "Unhealthy or cordoned nodes",
		Description: "Nodes that are NotReady, under pressure or cordoned reduce capacity and can evict pods.",
		Remediation: "Investigate kubelet/network/disk on the node, or uncordon if the maintenance is finished.", Needs: []string{"nodes"}, Run: ruleNodeHealth},
	{ID: "single-replica", Category: CatReliability, Title: "Single-replica workloads",
		Description: "Deployments and StatefulSets with one replica have downtime during node drains, upgrades and failures.",
		Remediation: "Run at least 2 replicas for anything that should stay available.", Run: ruleSingleReplica},
	{ID: "no-pdb", Category: CatReliability, Title: "Replicated workloads without a PodDisruptionBudget",
		Description: "Without a PDB, voluntary disruptions (drains, upgrades) may take down all replicas at once.",
		Remediation: "Create a PodDisruptionBudget (e.g. minAvailable: 1) selecting the workload.", Needs: []string{"poddisruptionbudgets"}, Run: ruleNoPDB},
	{ID: "no-readiness-probe", Category: CatReliability, Title: "Containers without readiness probes",
		Description: "Without a readiness probe traffic is sent to pods before they can serve it, and rollouts cannot detect bad versions.",
		Remediation: "Add a readinessProbe to long-running containers.", Run: ruleNoReadiness},
	{ID: "no-liveness-probe", Category: CatReliability, Title: "Containers without liveness probes",
		Description: "Without a liveness probe a hung process (deadlock, stuck event loop) keeps running and serving errors until someone notices.",
		Remediation: "Add a livenessProbe that detects a stuck process, with a generous timeout so it never restarts a merely slow one.", Run: ruleNoLiveness},
	{ID: "probe-misconfigured", Category: CatReliability, Title: "Probes pointing at undeclared ports",
		Description: "A probe that targets a port the container does not declare (or a port name that does not exist) usually fails or checks the wrong endpoint, causing restarts or traffic to unready pods.",
		Remediation: "Point the probe at a port the container actually listens on and declare it under ports.", Run: ruleProbeConfig},
	{ID: "hpa-maxed", Category: CatReliability, Title: "Autoscalers at their maximum",
		Description: "HorizontalPodAutoscalers already running at maxReplicas cannot absorb more load.",
		Remediation: "Raise maxReplicas (and check node capacity) or optimise the workload.", Needs: []string{"horizontalpodautoscalers"}, Run: ruleHPAMaxed},
	{ID: "crashing-pods", Category: CatReliability, Title: "Crashing or failing-to-start pods",
		Description: "Pods in CrashLoopBackOff, image pull errors, config errors or with many restarts.",
		Remediation: "Open the pod's Health and Logs (Previous) in Soliton Rudder to find the cause.", Needs: []string{"pods"}, Run: ruleCrashing},
	{ID: "unhealthy-workloads", Category: CatReliability, Title: "Workloads with unavailable replicas",
		Description: "Deployments, StatefulSets or DaemonSets where fewer replicas are ready than desired.",
		Remediation: "Check the workload's pods, events and rollout status.", Run: ruleUnhealthyWorkloads},
	{ID: "stuck-pending", Category: CatReliability, Title: "Pods stuck in Pending",
		Description: "Pods Pending for over 10 minutes usually cannot be scheduled (resources, taints, affinity, volumes).",
		Remediation: "Check the pod's Scheduling health check and the cluster's free capacity.", Needs: []string{"pods"}, Run: ruleStuckPending},
	{ID: "failed-pods", Category: CatReliability, Title: "Failed or evicted pods lingering",
		Description: "Pods in Failed phase (often Evicted) that nobody cleaned up hide real problems and clutter the cluster.",
		Remediation: "Delete the failed pods and fix the cause (usually node pressure).", Needs: []string{"pods"}, Run: ruleFailedPods},
	{ID: "pending-pvc", Category: CatReliability, Title: "Unbound PersistentVolumeClaims",
		Description: "PVCs stuck in Pending block the pods that use them.",
		Remediation: "Check the StorageClass, provisioner and available volumes.", Needs: []string{"persistentvolumeclaims"}, Run: rulePendingPVC},
	{ID: "replicas-one-node", Category: CatReliability, Title: "Replicas concentrated on one node",
		Description: "All replicas of a workload run on the same node, so a single node failure takes the service down.",
		Remediation: "Use topologySpreadConstraints or pod anti-affinity to spread replicas.", Needs: []string{"nodes", "pods", "replicasets"}, Run: ruleOneNode},

	{ID: "certificate-expiry", Category: CatReliability, Title: "TLS certificates expiring or expired",
		Description: "kubernetes.io/tls Secrets whose certificate has expired or expires within 30 days cause outages when it lapses.",
		Remediation: "Renew the certificate (check cert-manager's Certificate/Issuer status) or replace the Secret. See the Certificates view.", Needs: []string{"tlssecrets"}, Run: ruleCertExpiry},

	// ---- Security ----
	{ID: "privileged", Category: CatSecurity, Title: "Privileged containers",
		Description: "Privileged containers have nearly full access to the host.",
		Remediation: "Remove privileged: true; grant only the specific capabilities needed.", Run: rulePrivileged},
	{ID: "host-namespaces", Category: CatSecurity, Title: "Host network / PID / IPC sharing",
		Description: "Sharing host namespaces lets a container observe or interfere with the node and other pods.",
		Remediation: "Remove hostNetwork, hostPID and hostIPC unless strictly required.", Run: ruleHostNS},
	{ID: "host-path", Category: CatSecurity, Title: "hostPath volumes",
		Description: "hostPath mounts expose node files to the container; mounting the root or the container runtime socket is equivalent to node access.",
		Remediation: "Use emptyDir, PersistentVolumes or CSI volumes instead.", Run: ruleHostPath},
	{ID: "run-as-root", Category: CatSecurity, Title: "Containers explicitly running as root",
		Description: "runAsUser: 0 means a compromised process is root inside the container.",
		Remediation: "Run as a non-root user (runAsNonRoot: true and a numeric runAsUser).", Run: ruleRoot},
	{ID: "no-run-as-nonroot", Category: CatSecurity, Title: "Containers that may run as root",
		Description: "Without runAsNonRoot (or a non-zero runAsUser) the container runs as whatever user the image defines, often root.",
		Remediation: "Set securityContext.runAsNonRoot: true and a numeric runAsUser; make sure the image supports it.", Run: ruleNonRoot},
	{ID: "no-readonly-rootfs", Category: CatSecurity, Title: "Writable root filesystems",
		Description: "A writable root filesystem lets an attacker who gets code execution drop tools and persist changes inside the container.",
		Remediation: "Set securityContext.readOnlyRootFilesystem: true and mount emptyDir volumes for paths the app must write (e.g. /tmp).", Run: ruleReadOnlyRootFS},
	{ID: "no-seccomp", Category: CatSecurity, Title: "No seccomp profile",
		Description: "Without a seccomp profile the container may call any syscall the kernel allows, widening the attack surface.",
		Remediation: "Set securityContext.seccompProfile.type: RuntimeDefault on the pod.", Run: ruleSeccomp},
	{ID: "privilege-escalation", Category: CatSecurity, Title: "Privilege escalation not disabled",
		Description: "Containers that do not set allowPrivilegeEscalation: false can gain more privileges than their parent process.",
		Remediation: "Set securityContext.allowPrivilegeEscalation: false.", Run: ruleEscalation},
	{ID: "dangerous-capabilities", Category: CatSecurity, Title: "Dangerous Linux capabilities",
		Description: "Capabilities such as SYS_ADMIN, NET_ADMIN or ALL greatly widen the container's power.",
		Remediation: "Drop ALL capabilities and add back only what is required.", Run: ruleCaps},
	{ID: "latest-tag", Category: CatSecurity, Title: "Images using :latest or no tag",
		Description: "Mutable tags make deployments non-reproducible and can silently pull unreviewed code.",
		Remediation: "Pin images to a version tag or, better, a digest.", Run: ruleLatest},
	{ID: "no-network-policy", Category: CatSecurity, Title: "Namespaces without NetworkPolicies",
		Description: "Without any NetworkPolicy, every pod can talk to every other pod in the cluster.",
		Remediation: "Add a default-deny policy and allow only the traffic you need.", Needs: []string{"namespaces", "networkpolicies", "pods"}, Run: ruleNoNetPol},
	{ID: "ingress-no-tls", Category: CatSecurity, Title: "Ingress hosts without TLS",
		Description: "Ingress hosts that are not covered by a TLS section serve traffic over plain HTTP.",
		Remediation: "Add spec.tls with a certificate secret (e.g. via cert-manager).", Needs: []string{"ingresses"}, Run: ruleIngressTLS},

	// ---- Maintenance ----
	{ID: "deprecated-apis", Category: CatMaintenance, Title: "Manifests using removed or deprecated APIs",
		Description: "Objects last applied with API versions that were removed (or soon will be) fail when re-applied or after upgrades.",
		Remediation: "Update the manifests to the supported apiVersion and re-apply.", Run: ruleDeprecatedAPIs},
	{ID: "version-skew", Category: CatMaintenance, Title: "Node / control-plane version skew",
		Description: "Kubelets older than the control plane by more than two minor versions are unsupported.",
		Remediation: "Upgrade or replace the lagging nodes.", Needs: []string{"nodes"}, Run: ruleSkew},
	{ID: "k8s-support", Category: CatMaintenance, Title: "Kubernetes version support",
		Description: "Upstream Kubernetes supports only the three most recent minor releases.",
		Remediation: "Plan an upgrade to a supported minor version.", Run: ruleSupport},
	{ID: "released-volumes", Category: CatMaintenance, Title: "Released or failed PersistentVolumes",
		Description: "Volumes left in Released/Failed state keep costing money and hold stale data.",
		Remediation: "Reclaim or delete the volumes once their data is no longer needed.", Needs: []string{"persistentvolumes"}, Run: ruleOrphanPV},
}

// latestMinor is the newest Kubernetes minor release this build knows about.
// Update when a new minor ships; it only feeds the "version support" rule.
const latestMinor = 36

// ---------- helpers ----------

type res struct{ cpu, mem int64 }

func podRequests(spec map[string]any) res {
	var r res
	for _, c := range containers(spec, false) {
		if q, ok := qty(c, "resources", "requests", "cpu"); ok {
			r.cpu += q.MilliValue()
		}
		if q, ok := qty(c, "resources", "requests", "memory"); ok {
			r.mem += q.Value()
		}
	}
	return r
}

func pct(a, b int64) float64 {
	if b <= 0 {
		return 0
	}
	return float64(a) / float64(b) * 100
}

func fmtCPU(m int64) string { return fmt.Sprintf("%.1f cores", float64(m)/1000) }
func fmtMem(b int64) string { return fmt.Sprintf("%.1f GiB", float64(b)/(1<<30)) }

func nodeAlloc(n *unstructured.Unstructured) res {
	var r res
	if q, ok := qty(n.Object, "status", "allocatable", "cpu"); ok {
		r.cpu = q.MilliValue()
	}
	if q, ok := qty(n.Object, "status", "allocatable", "memory"); ok {
		r.mem = q.Value()
	}
	return r
}

func activePods(s *Snapshot) []unstructured.Unstructured {
	var out []unstructured.Unstructured
	for _, p := range s.list("pods") {
		ph, _, _ := unstructured.NestedString(p.Object, "status", "phase")
		if ph == "Running" || ph == "Pending" {
			out = append(out, p)
		}
	}
	return out
}

func requestsByNode(s *Snapshot) map[string]res {
	out := map[string]res{}
	for _, p := range activePods(s) {
		node, _, _ := unstructured.NestedString(p.Object, "spec", "nodeName")
		if node == "" {
			continue
		}
		spec, _, _ := unstructured.NestedMap(p.Object, "spec")
		r := podRequests(spec)
		cur := out[node]
		cur.cpu += r.cpu
		cur.mem += r.mem
		out[node] = cur
	}
	return out
}

func appWorkloads(s *Snapshot, kinds ...string) []workload {
	var out []workload
	for _, w := range s.workloads() {
		if isSystemNS(w.Namespace) {
			continue
		}
		if len(kinds) == 0 {
			out = append(out, w)
			continue
		}
		for _, k := range kinds {
			if w.Kind == k {
				out = append(out, w)
				break
			}
		}
	}
	return out
}

// ---------- resources ----------

func ruleNodeSaturation(s *Snapshot) Outcome {
	o := Outcome{Checked: len(s.list("nodes"))}
	byNode := requestsByNode(s)
	for i := range s.list("nodes") {
		n := &s.list("nodes")[i]
		alloc, req := nodeAlloc(n), byNode[n.GetName()]
		cp, mp := pct(req.cpu, alloc.cpu), pct(req.mem, alloc.mem)
		worst := cp
		if mp > worst {
			worst = mp
		}
		sev := Severity("")
		switch {
		case worst >= 100:
			sev = High
		case worst >= 85:
			sev = Medium
		default:
			continue
		}
		o.Findings = append(o.Findings, objFinding(sev, "Node", "core/v1/nodes", n,
			fmt.Sprintf("Requests use %.0f%% of CPU (%s of %s) and %.0f%% of memory (%s of %s)", cp, fmtCPU(req.cpu), fmtCPU(alloc.cpu), mp, fmtMem(req.mem), fmtMem(alloc.mem))))
	}
	return o
}

func ruleHeadroom(s *Snapshot) Outcome {
	nodes := s.list("nodes")
	o := Outcome{Checked: 1}
	if len(nodes) == 0 {
		return o
	}
	var total, biggestCPU, biggestMem res
	for i := range nodes {
		a := nodeAlloc(&nodes[i])
		total.cpu += a.cpu
		total.mem += a.mem
		if a.cpu > biggestCPU.cpu {
			biggestCPU = a
		}
		if a.mem > biggestMem.mem {
			biggestMem = a
		}
	}
	var req res
	for _, r := range requestsByNode(s) {
		req.cpu += r.cpu
		req.mem += r.mem
	}
	cluster := func(sev Severity, msg string) Outcome {
		o.Findings = append(o.Findings, finding(sev, "Cluster", "", "", "", msg))
		return o
	}
	if len(nodes) == 1 {
		return cluster(Medium, "Single-node cluster: any node failure or upgrade means full downtime")
	}
	remCPU, remMem := total.cpu-biggestCPU.cpu, total.mem-biggestMem.mem
	if req.cpu > remCPU || req.mem > remMem {
		return cluster(High, fmt.Sprintf("If the largest node failed, requests (CPU %s, memory %s) would exceed the remaining capacity (CPU %s, memory %s)",
			fmtCPU(req.cpu), fmtMem(req.mem), fmtCPU(remCPU), fmtMem(remMem)))
	}
	return o
}

func ruleNoRequests(s *Snapshot) Outcome {
	ws := appWorkloads(s)
	o := Outcome{Checked: len(ws)}
	for _, w := range ws {
		var bad []string
		for _, c := range containers(w.Spec, false) {
			_, hc := qty(c, "resources", "requests", "cpu")
			_, hm := qty(c, "resources", "requests", "memory")
			switch {
			case !hc && !hm:
				bad = append(bad, cName(c)+" (cpu, memory)")
			case !hc:
				bad = append(bad, cName(c)+" (cpu)")
			case !hm:
				bad = append(bad, cName(c)+" (memory)")
			}
		}
		if len(bad) > 0 {
			o.Findings = append(o.Findings, w.finding(Medium, "No requests: "+strings.Join(bad, ", ")))
		}
	}
	return o
}

func ruleNoMemLimit(s *Snapshot) Outcome {
	ws := appWorkloads(s)
	o := Outcome{Checked: len(ws)}
	for _, w := range ws {
		var bad []string
		for _, c := range containers(w.Spec, false) {
			if _, ok := qty(c, "resources", "limits", "memory"); !ok {
				bad = append(bad, cName(c))
			}
		}
		if len(bad) > 0 {
			o.Findings = append(o.Findings, w.finding(Low, "No memory limit: "+strings.Join(bad, ", ")))
		}
	}
	return o
}

func ruleMemNearLimit(s *Snapshot) Outcome {
	if s.Usage == nil {
		return Outcome{Skipped: "metrics-server is not available"}
	}
	var o Outcome
	for i := range s.list("pods") {
		p := &s.list("pods")[i]
		if isSystemNS(p.GetNamespace()) {
			continue
		}
		u, ok := s.Usage[p.GetNamespace()+"/"+p.GetName()]
		if !ok {
			continue
		}
		spec, _, _ := unstructured.NestedMap(p.Object, "spec")
		var limit int64
		complete := true
		for _, c := range containers(spec, false) {
			q, ok := qty(c, "resources", "limits", "memory")
			if !ok {
				complete = false
				break
			}
			limit += q.Value()
		}
		if !complete || limit == 0 {
			continue
		}
		o.Checked++
		if r := pct(u.Mem, limit); r >= 90 {
			o.Findings = append(o.Findings, podFinding(High, p, fmt.Sprintf("Using %.0f%% of its memory limit (%s of %s)", r, fmtMem(u.Mem), fmtMem(limit))))
		}
	}
	return o
}

func ruleOverprovisioned(s *Snapshot) Outcome {
	if s.Usage == nil {
		return Outcome{Skipped: "metrics-server is not available"}
	}
	var o Outcome
	for i := range s.list("pods") {
		p := &s.list("pods")[i]
		if isSystemNS(p.GetNamespace()) {
			continue
		}
		if ph, _, _ := unstructured.NestedString(p.Object, "status", "phase"); ph != "Running" {
			continue
		}
		u, ok := s.Usage[p.GetNamespace()+"/"+p.GetName()]
		if !ok {
			continue
		}
		spec, _, _ := unstructured.NestedMap(p.Object, "spec")
		req := podRequests(spec)
		o.Checked++
		var msgs []string
		if req.cpu >= 200 && pct(u.CPU, req.cpu) < 10 {
			msgs = append(msgs, fmt.Sprintf("CPU %dm used of %dm requested", u.CPU, req.cpu))
		}
		if req.mem >= 256<<20 && pct(u.Mem, req.mem) < 10 {
			msgs = append(msgs, fmt.Sprintf("memory %s used of %s requested", fmtMem(u.Mem), fmtMem(req.mem)))
		}
		if len(msgs) > 0 {
			o.Findings = append(o.Findings, podFinding(Low, p, strings.Join(msgs, "; ")))
		}
	}
	return o
}

// ---------- reliability ----------

func ruleNodeHealth(s *Snapshot) Outcome {
	o := Outcome{Checked: len(s.list("nodes"))}
	for i := range s.list("nodes") {
		n := &s.list("nodes")[i]
		conds, _, _ := unstructured.NestedSlice(n.Object, "status", "conditions")
		ready := false
		var pressure []string
		for _, c := range conds {
			cm, _ := c.(map[string]any)
			t, st := fmt.Sprint(cm["type"]), fmt.Sprint(cm["status"])
			if t == "Ready" {
				ready = st == "True"
			} else if strings.HasSuffix(t, "Pressure") && st == "True" {
				pressure = append(pressure, t)
			}
		}
		switch {
		case !ready:
			o.Findings = append(o.Findings, objFinding(Critical, "Node", "core/v1/nodes", n, "Node is not Ready"))
		case len(pressure) > 0:
			o.Findings = append(o.Findings, objFinding(High, "Node", "core/v1/nodes", n, "Under "+strings.Join(pressure, ", ")))
		}
		if un, _, _ := unstructured.NestedBool(n.Object, "spec", "unschedulable"); un {
			o.Findings = append(o.Findings, objFinding(Low, "Node", "core/v1/nodes", n, "Cordoned: no new pods will be scheduled"))
		}
	}
	return o
}

func ruleSingleReplica(s *Snapshot) Outcome {
	ws := appWorkloads(s, "Deployment", "StatefulSet")
	o := Outcome{Checked: len(ws)}
	for _, w := range ws {
		if w.HasReplicas && w.Replicas == 1 {
			if s.isProd(w.Namespace) {
				o.Findings = append(o.Findings, w.finding(High, "Runs a single replica in a production namespace: any node drain, upgrade or crash means downtime"))
			} else {
				o.Findings = append(o.Findings, w.finding(Medium, "Runs a single replica"))
			}
		}
	}
	return o
}

func ruleNoPDB(s *Snapshot) Outcome {
	ws := appWorkloads(s, "Deployment", "StatefulSet")
	o := Outcome{}
	pdbs := s.list("poddisruptionbudgets")
	for _, w := range ws {
		if !w.HasReplicas || w.Replicas < 2 {
			continue
		}
		o.Checked++
		covered := false
		for i := range pdbs {
			if pdbs[i].GetNamespace() != w.Namespace {
				continue
			}
			sel, found, _ := unstructured.NestedMap(pdbs[i].Object, "spec", "selector")
			if !found {
				continue
			}
			var ls metav1.LabelSelector
			b, _ := json.Marshal(sel)
			if json.Unmarshal(b, &ls) != nil {
				continue
			}
			if selector, err := metav1.LabelSelectorAsSelector(&ls); err == nil && !selector.Empty() && selector.Matches(labels.Set(w.Labels)) {
				covered = true
				break
			}
		}
		if !covered {
			sev := Low
			if s.isProd(w.Namespace) {
				sev = Medium
			}
			o.Findings = append(o.Findings, w.finding(sev, fmt.Sprintf("%d replicas but no PodDisruptionBudget", w.Replicas)))
		}
	}
	return o
}

func ruleNoReadiness(s *Snapshot) Outcome {
	ws := appWorkloads(s, "Deployment", "StatefulSet", "DaemonSet", "Pod")
	o := Outcome{Checked: len(ws)}
	for _, w := range ws {
		var bad []string
		for _, c := range containers(w.Spec, false) {
			if _, ok := c["readinessProbe"]; !ok {
				bad = append(bad, cName(c))
			}
		}
		if len(bad) > 0 {
			o.Findings = append(o.Findings, w.finding(Medium, "No readiness probe: "+strings.Join(bad, ", ")))
		}
	}
	return o
}

func ruleHPAMaxed(s *Snapshot) Outcome {
	o := Outcome{Checked: len(s.list("horizontalpodautoscalers"))}
	for i := range s.list("horizontalpodautoscalers") {
		h := &s.list("horizontalpodautoscalers")[i]
		max, _, _ := unstructured.NestedInt64(h.Object, "spec", "maxReplicas")
		cur, _, _ := unstructured.NestedInt64(h.Object, "status", "currentReplicas")
		des, _, _ := unstructured.NestedInt64(h.Object, "status", "desiredReplicas")
		if max > 0 && cur >= max && des >= max {
			o.Findings = append(o.Findings, objFinding(Medium, "HorizontalPodAutoscaler", "autoscaling/v2/horizontalpodautoscalers", h, fmt.Sprintf("Running at its maximum of %d replicas", max)))
		}
	}
	return o
}

var badWaiting = map[string]bool{"CrashLoopBackOff": true, "ImagePullBackOff": true, "ErrImagePull": true, "CreateContainerConfigError": true, "InvalidImageName": true, "RunContainerError": true}

func ruleCrashing(s *Snapshot) Outcome {
	o := Outcome{Checked: len(s.list("pods"))}
	for i := range s.list("pods") {
		p := &s.list("pods")[i]
		var worstSev Severity
		var msg string
		for _, f := range []string{"initContainerStatuses", "containerStatuses"} {
			sts, _, _ := unstructured.NestedSlice(p.Object, "status", f)
			for _, st := range sts {
				sm, _ := st.(map[string]any)
				name := fmt.Sprint(sm["name"])
				restarts, _ := sm["restartCount"].(int64)
				state, _ := sm["state"].(map[string]any)
				if w, ok := state["waiting"].(map[string]any); ok && badWaiting[fmt.Sprint(w["reason"])] {
					worstSev, msg = High, fmt.Sprintf("Container %s: %v", name, w["reason"])
				} else if restarts >= 5 && worstSev != High {
					worstSev, msg = Medium, fmt.Sprintf("Container %s restarted %d times", name, restarts)
				}
			}
		}
		if worstSev != "" {
			o.Findings = append(o.Findings, podFinding(worstSev, p, msg))
		}
	}
	return o
}

func ruleUnhealthyWorkloads(s *Snapshot) Outcome {
	var o Outcome
	for _, w := range s.workloads() {
		switch w.Kind {
		case "Deployment", "StatefulSet":
			if !w.HasReplicas || w.Replicas == 0 {
				continue
			}
			o.Checked++
			ready, _, _ := unstructured.NestedInt64(w.Obj.Object, "status", "readyReplicas")
			if ready < w.Replicas {
				sev := Medium
				if ready == 0 {
					sev = High
				}
				o.Findings = append(o.Findings, w.finding(sev, fmt.Sprintf("%d of %d replicas ready", ready, w.Replicas)))
			}
		case "DaemonSet":
			o.Checked++
			desired, _, _ := unstructured.NestedInt64(w.Obj.Object, "status", "desiredNumberScheduled")
			ready, _, _ := unstructured.NestedInt64(w.Obj.Object, "status", "numberReady")
			if ready < desired {
				sev := Medium
				if ready == 0 {
					sev = High
				}
				o.Findings = append(o.Findings, w.finding(sev, fmt.Sprintf("%d of %d pods ready", ready, desired)))
			}
		}
	}
	return o
}

func ruleStuckPending(s *Snapshot) Outcome {
	o := Outcome{Checked: len(s.list("pods"))}
	for i := range s.list("pods") {
		p := &s.list("pods")[i]
		if ph, _, _ := unstructured.NestedString(p.Object, "status", "phase"); ph != "Pending" {
			continue
		}
		if age := time.Since(p.GetCreationTimestamp().Time); age > 10*time.Minute {
			o.Findings = append(o.Findings, podFinding(Medium, p, fmt.Sprintf("Pending for %s", age.Round(time.Minute))))
		}
	}
	return o
}

func ruleFailedPods(s *Snapshot) Outcome {
	o := Outcome{Checked: len(s.list("pods"))}
	for i := range s.list("pods") {
		p := &s.list("pods")[i]
		if ph, _, _ := unstructured.NestedString(p.Object, "status", "phase"); ph == "Failed" {
			reason, _, _ := unstructured.NestedString(p.Object, "status", "reason")
			if reason == "" {
				reason = "Failed"
			}
			o.Findings = append(o.Findings, podFinding(Low, p, reason))
		}
	}
	return o
}

func rulePendingPVC(s *Snapshot) Outcome {
	o := Outcome{Checked: len(s.list("persistentvolumeclaims"))}
	for i := range s.list("persistentvolumeclaims") {
		c := &s.list("persistentvolumeclaims")[i]
		if ph, _, _ := unstructured.NestedString(c.Object, "status", "phase"); ph == "Pending" && time.Since(c.GetCreationTimestamp().Time) > 2*time.Minute {
			o.Findings = append(o.Findings, objFinding(Medium, "PersistentVolumeClaim", "core/v1/persistentvolumeclaims", c, "Not bound (Pending)"))
		}
	}
	return o
}

func ruleOneNode(s *Snapshot) Outcome {
	schedulable := 0
	for _, n := range s.list("nodes") {
		if un, _, _ := unstructured.NestedBool(n.Object, "spec", "unschedulable"); !un {
			schedulable++
		}
	}
	o := Outcome{}
	if schedulable < 2 {
		o.Skipped = "needs at least two schedulable nodes"
		return o
	}
	rsOwner := map[string]string{} // rs ns/name -> deployment name
	for _, rs := range s.list("replicasets") {
		for _, ref := range rs.GetOwnerReferences() {
			if ref.Kind == "Deployment" {
				rsOwner[rs.GetNamespace()+"/"+rs.GetName()] = ref.Name
			}
		}
	}
	type group struct {
		kind, key, ns, name string
		nodes               map[string]bool
		pods                int
	}
	groups := map[string]*group{}
	for i := range s.list("pods") {
		p := &s.list("pods")[i]
		if isSystemNS(p.GetNamespace()) {
			continue
		}
		if ph, _, _ := unstructured.NestedString(p.Object, "status", "phase"); ph != "Running" {
			continue
		}
		node, _, _ := unstructured.NestedString(p.Object, "spec", "nodeName")
		for _, ref := range p.GetOwnerReferences() {
			kind, key, name := "", "", ""
			switch ref.Kind {
			case "ReplicaSet":
				if d := rsOwner[p.GetNamespace()+"/"+ref.Name]; d != "" {
					kind, key, name = "Deployment", "apps/v1/deployments", d
				}
			case "StatefulSet":
				kind, key, name = "StatefulSet", "apps/v1/statefulsets", ref.Name
			}
			if kind == "" {
				continue
			}
			id := kind + "/" + p.GetNamespace() + "/" + name
			g := groups[id]
			if g == nil {
				g = &group{kind: kind, key: key, ns: p.GetNamespace(), name: name, nodes: map[string]bool{}}
				groups[id] = g
			}
			g.nodes[node] = true
			g.pods++
		}
	}
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		g := groups[id]
		if g.pods < 2 {
			continue
		}
		o.Checked++
		if len(g.nodes) == 1 {
			o.Findings = append(o.Findings, finding(Medium, g.kind, g.key, g.ns, g.name, fmt.Sprintf("All %d replicas run on one node", g.pods)))
		}
	}
	return o
}

// ---------- security ----------

func scBool(c map[string]any, field string) (bool, bool) {
	v, ok, _ := unstructured.NestedBool(c, "securityContext", field)
	return v, ok
}

func rulePrivileged(s *Snapshot) Outcome {
	ws := appWorkloads(s)
	o := Outcome{Checked: len(ws)}
	for _, w := range ws {
		var bad []string
		for _, c := range containers(w.Spec, true) {
			if v, _ := scBool(c, "privileged"); v {
				bad = append(bad, cName(c))
			}
		}
		if len(bad) > 0 {
			o.Findings = append(o.Findings, w.finding(Critical, "Privileged: "+strings.Join(bad, ", ")))
		}
	}
	return o
}

func ruleHostNS(s *Snapshot) Outcome {
	ws := appWorkloads(s)
	o := Outcome{Checked: len(s.workloads())}
	for _, w := range ws {
		var on []string
		for _, f := range []string{"hostNetwork", "hostPID", "hostIPC"} {
			if v, _, _ := unstructured.NestedBool(w.Spec, f); v {
				on = append(on, f)
			}
		}
		if len(on) > 0 {
			o.Findings = append(o.Findings, w.finding(High, "Uses "+strings.Join(on, ", ")))
		}
	}
	return o
}

func ruleHostPath(s *Snapshot) Outcome {
	ws := appWorkloads(s)
	o := Outcome{Checked: len(ws)}
	for _, w := range ws {
		vols, _, _ := unstructured.NestedSlice(w.Spec, "volumes")
		var paths []string
		sev := Medium
		for _, v := range vols {
			vm, _ := v.(map[string]any)
			if hp, ok := vm["hostPath"].(map[string]any); ok {
				p := fmt.Sprint(hp["path"])
				paths = append(paths, p)
				if p == "/" || strings.Contains(p, "docker.sock") || strings.Contains(p, "containerd.sock") || strings.Contains(p, "crio.sock") {
					sev = High
				}
			}
		}
		if len(paths) > 0 {
			o.Findings = append(o.Findings, w.finding(sev, "hostPath: "+strings.Join(paths, ", ")))
		}
	}
	return o
}

func ruleRoot(s *Snapshot) Outcome {
	ws := appWorkloads(s)
	o := Outcome{Checked: len(ws)}
	for _, w := range ws {
		podRoot := false
		if u, ok, _ := unstructured.NestedInt64(w.Spec, "securityContext", "runAsUser"); ok && u == 0 {
			podRoot = true
		}
		var bad []string
		for _, c := range containers(w.Spec, true) {
			u, ok, _ := unstructured.NestedInt64(c, "securityContext", "runAsUser")
			if (ok && u == 0) || (!ok && podRoot) {
				bad = append(bad, cName(c))
			}
		}
		if len(bad) > 0 {
			o.Findings = append(o.Findings, w.finding(High, "Runs as root (uid 0): "+strings.Join(bad, ", ")))
		}
	}
	return o
}

func ruleEscalation(s *Snapshot) Outcome {
	ws := appWorkloads(s)
	o := Outcome{Checked: len(ws)}
	for _, w := range ws {
		var bad []string
		for _, c := range containers(w.Spec, false) {
			if v, ok := scBool(c, "allowPrivilegeEscalation"); !ok || v {
				bad = append(bad, cName(c))
			}
		}
		if len(bad) > 0 {
			o.Findings = append(o.Findings, w.finding(Low, "allowPrivilegeEscalation not false: "+strings.Join(bad, ", ")))
		}
	}
	return o
}

var highCaps = map[string]bool{"ALL": true, "SYS_ADMIN": true, "SYS_MODULE": true, "SYS_PTRACE": true, "DAC_READ_SEARCH": true}
var medCaps = map[string]bool{"NET_ADMIN": true, "NET_RAW": true, "SYS_TIME": true, "SYS_RAWIO": true}

func ruleCaps(s *Snapshot) Outcome {
	ws := appWorkloads(s)
	o := Outcome{Checked: len(ws)}
	for _, w := range ws {
		sev := Severity("")
		var caps []string
		for _, c := range containers(w.Spec, false) {
			add, _, _ := unstructured.NestedStringSlice(c, "securityContext", "capabilities", "add")
			for _, a := range add {
				a = strings.ToUpper(strings.TrimPrefix(strings.ToUpper(a), "CAP_"))
				switch {
				case highCaps[a]:
					sev = High
					caps = append(caps, a)
				case medCaps[a]:
					if sev != High {
						sev = Medium
					}
					caps = append(caps, a)
				}
			}
		}
		if sev != "" {
			o.Findings = append(o.Findings, w.finding(sev, "Adds capabilities: "+strings.Join(caps, ", ")))
		}
	}
	return o
}

// imageMutable reports images without a pinned tag or digest.
func imageMutable(img string) bool {
	if strings.Contains(img, "@sha256:") {
		return false
	}
	last := img[strings.LastIndex(img, "/")+1:]
	i := strings.LastIndex(last, ":")
	return i < 0 || last[i+1:] == "latest"
}

func ruleLatest(s *Snapshot) Outcome {
	ws := appWorkloads(s)
	o := Outcome{Checked: len(ws)}
	for _, w := range ws {
		var bad []string
		for _, c := range containers(w.Spec, true) {
			if img, _ := c["image"].(string); img != "" && imageMutable(img) {
				bad = append(bad, img)
			}
		}
		if len(bad) > 0 {
			o.Findings = append(o.Findings, w.finding(Medium, "Mutable image tag: "+strings.Join(bad, ", ")))
		}
	}
	return o
}

func ruleNoNetPol(s *Snapshot) Outcome {
	withPods := map[string]bool{}
	for _, p := range s.list("pods") {
		if !isSystemNS(p.GetNamespace()) {
			withPods[p.GetNamespace()] = true
		}
	}
	covered := map[string]bool{}
	for _, np := range s.list("networkpolicies") {
		covered[np.GetNamespace()] = true
	}
	o := Outcome{Checked: len(withPods)}
	names := make([]string, 0, len(withPods))
	for ns := range withPods {
		names = append(names, ns)
	}
	sort.Strings(names)
	for _, ns := range names {
		if !covered[ns] {
			o.Findings = append(o.Findings, finding(Medium, "Namespace", "core/v1/namespaces", "", ns, "No NetworkPolicy: all pods can reach each other"))
		}
	}
	return o
}

func ruleIngressTLS(s *Snapshot) Outcome {
	o := Outcome{Checked: len(s.list("ingresses"))}
	for i := range s.list("ingresses") {
		ing := &s.list("ingresses")[i]
		covered := map[string]bool{}
		tls, _, _ := unstructured.NestedSlice(ing.Object, "spec", "tls")
		for _, t := range tls {
			tm, _ := t.(map[string]any)
			hosts, _ := tm["hosts"].([]any)
			for _, h := range hosts {
				covered[fmt.Sprint(h)] = true
			}
		}
		rules, _, _ := unstructured.NestedSlice(ing.Object, "spec", "rules")
		var open []string
		for _, r := range rules {
			rm, _ := r.(map[string]any)
			host, _ := rm["host"].(string)
			if host != "" && !tlsCovers(covered, host) {
				open = append(open, host)
			}
		}
		if len(open) > 0 {
			o.Findings = append(o.Findings, objFinding(Medium, "Ingress", "networking.k8s.io/v1/ingresses", ing, "No TLS for: "+strings.Join(open, ", ")))
		}
	}
	return o
}

func tlsCovers(covered map[string]bool, host string) bool {
	if covered[host] {
		return true
	}
	if i := strings.IndexByte(host, '.'); i > 0 && covered["*"+host[i:]] {
		return true
	}
	return false
}

// ---------- maintenance ----------

// removed maps deprecated apiVersion+kind patterns to the Kubernetes minor that removed them.
var removedAPIs = map[string]int{
	"extensions/v1beta1":                   22,
	"apps/v1beta1":                         16,
	"apps/v1beta2":                         16,
	"networking.k8s.io/v1beta1":            22,
	"rbac.authorization.k8s.io/v1beta1":    22,
	"admissionregistration.k8s.io/v1beta1": 22,
	"batch/v1beta1":                        25,
	"policy/v1beta1":                       25,
	"autoscaling/v2beta1":                  25,
	"autoscaling/v2beta2":                  26,
	"flowcontrol.apiserver.k8s.io/v1beta1": 26,
	"flowcontrol.apiserver.k8s.io/v1beta2": 29,
	"storage.k8s.io/v1beta1":               27,
}

var versionRe = regexp.MustCompile(`v?(\d+)\.(\d+)`)

func minorOf(v string) int {
	m := versionRe.FindStringSubmatch(v)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[2])
	return n
}

func ruleDeprecatedAPIs(s *Snapshot) Outcome {
	var o Outcome
	server := minorOf(s.ServerVersion)
	names := make([]string, 0, len(s.Lists))
	for n := range s.Lists {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		for i := range s.Lists[n] {
			obj := &s.Lists[n][i]
			raw := obj.GetAnnotations()["kubectl.kubernetes.io/last-applied-configuration"]
			if raw == "" {
				continue
			}
			o.Checked++
			var la struct {
				APIVersion string `json:"apiVersion"`
			}
			if json.Unmarshal([]byte(raw), &la) != nil {
				continue
			}
			removedIn, bad := removedAPIs[la.APIVersion]
			if !bad {
				continue
			}
			sev, msg := Medium, fmt.Sprintf("Last applied with %s (removed in v1.%d)", la.APIVersion, removedIn)
			if server >= removedIn {
				sev = High
				msg += ": re-applying this manifest fails on this cluster"
			} else {
				msg += ": will break on upgrade"
			}
			kind := obj.GetKind()
			if kind == "" {
				kind = n
			}
			o.Findings = append(o.Findings, objFinding(sev, kind, "", obj, msg))
		}
	}
	return o
}

func ruleSkew(s *Snapshot) Outcome {
	server := minorOf(s.ServerVersion)
	o := Outcome{Checked: len(s.list("nodes"))}
	if server == 0 {
		o.Skipped = "server version unknown"
		return o
	}
	for i := range s.list("nodes") {
		n := &s.list("nodes")[i]
		kv, _, _ := unstructured.NestedString(n.Object, "status", "nodeInfo", "kubeletVersion")
		k := minorOf(kv)
		switch {
		case k == 0:
		case server-k > 2:
			o.Findings = append(o.Findings, objFinding(High, "Node", "core/v1/nodes", n, fmt.Sprintf("Kubelet %s is %d minor versions behind the control plane (%s): unsupported skew", kv, server-k, s.ServerVersion)))
		case k < server:
			o.Findings = append(o.Findings, objFinding(Low, "Node", "core/v1/nodes", n, fmt.Sprintf("Kubelet %s is behind the control plane (%s)", kv, s.ServerVersion)))
		}
	}
	return o
}

func ruleSupport(s *Snapshot) Outcome {
	server := minorOf(s.ServerVersion)
	o := Outcome{Checked: 1}
	if server == 0 {
		o.Skipped = "server version unknown"
		return o
	}
	switch {
	case server < latestMinor-2:
		o.Findings = append(o.Findings, finding(High, "Cluster", "", "", "", fmt.Sprintf("Kubernetes v1.%d is no longer supported upstream (latest is v1.%d)", server, latestMinor)))
	case server == latestMinor-2:
		o.Findings = append(o.Findings, finding(Low, "Cluster", "", "", "", fmt.Sprintf("Kubernetes v1.%d is the oldest supported minor; it falls out of support with the next release", server)))
	}
	return o
}

func ruleOrphanPV(s *Snapshot) Outcome {
	o := Outcome{Checked: len(s.list("persistentvolumes"))}
	for i := range s.list("persistentvolumes") {
		pv := &s.list("persistentvolumes")[i]
		if ph, _, _ := unstructured.NestedString(pv.Object, "status", "phase"); ph == "Released" || ph == "Failed" {
			o.Findings = append(o.Findings, objFinding(Low, "PersistentVolume", "core/v1/persistentvolumes", pv, ph))
		}
	}
	return o
}

func ruleCertExpiry(s *Snapshot) Outcome {
	secrets := s.list("tlssecrets")
	o := Outcome{Checked: len(secrets)}
	now := time.Now()
	for i := range secrets {
		sec := &secrets[i]
		raw, _, _ := unstructured.NestedString(sec.Object, "data", "tls.crt")
		der, err := base64.StdEncoding.DecodeString(raw)
		if raw == "" || err != nil {
			o.Findings = append(o.Findings, objFinding(Medium, "Secret", "core/v1/secrets", sec, "TLS secret has no readable tls.crt"))
			continue
		}
		cs, err := kube.SummarizeCert(der, now)
		if err != nil {
			o.Findings = append(o.Findings, objFinding(Medium, "Secret", "core/v1/secrets", sec, "Cannot parse certificate: "+err.Error()))
			continue
		}
		switch {
		case cs.Expired:
			o.Findings = append(o.Findings, objFinding(High, "Secret", "core/v1/secrets", sec, fmt.Sprintf("Certificate for %s expired %d days ago", cs.Subject, -cs.DaysLeft)))
		case cs.DaysLeft < 14:
			o.Findings = append(o.Findings, objFinding(High, "Secret", "core/v1/secrets", sec, fmt.Sprintf("Certificate for %s expires in %d days", cs.Subject, cs.DaysLeft)))
		case cs.DaysLeft < 30:
			o.Findings = append(o.Findings, objFinding(Medium, "Secret", "core/v1/secrets", sec, fmt.Sprintf("Certificate for %s expires in %d days", cs.Subject, cs.DaysLeft)))
		}
	}
	return o
}

// ---- Cluster Doctor 2.0 rules ----

func podSecuritySeccomp(spec map[string]any) bool {
	t, _, _ := unstructured.NestedString(spec, "securityContext", "seccompProfile", "type")
	return t == "RuntimeDefault" || t == "Localhost"
}

func ruleSeccomp(s *Snapshot) Outcome {
	ws := appWorkloads(s)
	o := Outcome{Checked: len(ws)}
	for _, w := range ws {
		if podSecuritySeccomp(w.Spec) {
			continue
		}
		var bad []string
		for _, c := range containers(w.Spec, false) {
			if t, _, _ := unstructured.NestedString(c, "securityContext", "seccompProfile", "type"); t != "RuntimeDefault" && t != "Localhost" {
				bad = append(bad, cName(c))
			}
		}
		if len(bad) > 0 {
			o.Findings = append(o.Findings, w.finding(Low, "No seccompProfile: "+strings.Join(bad, ", ")))
		}
	}
	return o
}

func ruleReadOnlyRootFS(s *Snapshot) Outcome {
	ws := appWorkloads(s)
	o := Outcome{Checked: len(ws)}
	for _, w := range ws {
		var bad []string
		for _, c := range containers(w.Spec, false) {
			if v, _ := scBool(c, "readOnlyRootFilesystem"); !v {
				bad = append(bad, cName(c))
			}
		}
		if len(bad) > 0 {
			o.Findings = append(o.Findings, w.finding(Low, "Writable root filesystem: "+strings.Join(bad, ", ")))
		}
	}
	return o
}

// runsAsNonRoot is true when the container or pod demands a non-root user.
func runsAsNonRoot(spec, c map[string]any) bool {
	for _, sc := range []map[string]any{c, spec} {
		if v, ok, _ := unstructured.NestedBool(sc, "securityContext", "runAsNonRoot"); ok {
			return v
		}
		if u, ok, _ := unstructured.NestedInt64(sc, "securityContext", "runAsUser"); ok {
			return u > 0
		}
	}
	return false
}

func explicitRoot(spec, c map[string]any) bool {
	if u, ok, _ := unstructured.NestedInt64(c, "securityContext", "runAsUser"); ok {
		return u == 0
	}
	u, ok, _ := unstructured.NestedInt64(spec, "securityContext", "runAsUser")
	return ok && u == 0
}

func ruleNonRoot(s *Snapshot) Outcome {
	ws := appWorkloads(s)
	o := Outcome{Checked: len(ws)}
	for _, w := range ws {
		var bad []string
		for _, c := range containers(w.Spec, false) {
			// explicit uid 0 is reported (more severely) by run-as-root
			if !runsAsNonRoot(w.Spec, c) && !explicitRoot(w.Spec, c) {
				bad = append(bad, cName(c))
			}
		}
		if len(bad) > 0 {
			o.Findings = append(o.Findings, w.finding(Low, "May run as root (no runAsNonRoot): "+strings.Join(bad, ", ")))
		}
	}
	return o
}

func ruleNoLiveness(s *Snapshot) Outcome {
	ws := appWorkloads(s, "Deployment", "StatefulSet", "DaemonSet", "Pod")
	o := Outcome{Checked: len(ws)}
	for _, w := range ws {
		var bad []string
		for _, c := range containers(w.Spec, false) {
			_, hasLive := c["livenessProbe"]
			_, hasStartup := c["startupProbe"]
			if !hasLive && !hasStartup {
				bad = append(bad, cName(c))
			}
		}
		if len(bad) > 0 {
			o.Findings = append(o.Findings, w.finding(Low, "No liveness probe: "+strings.Join(bad, ", ")))
		}
	}
	return o
}

// probePort returns the numeric/named port a probe targets.
func probePort(probe map[string]any) (any, bool) {
	for _, kind := range []string{"httpGet", "tcpSocket", "grpc"} {
		if m, ok := probe[kind].(map[string]any); ok {
			if p, ok := m["port"]; ok {
				return p, true
			}
		}
	}
	return nil, false
}

func ruleProbeConfig(s *Snapshot) Outcome {
	ws := appWorkloads(s, "Deployment", "StatefulSet", "DaemonSet", "Pod")
	o := Outcome{Checked: len(ws)}
	for _, w := range ws {
		var bad []string
		for _, c := range containers(w.Spec, false) {
			declared := map[int64]bool{}
			names := map[string]bool{}
			ports, _ := c["ports"].([]any)
			for _, p := range ports {
				pm, _ := p.(map[string]any)
				if n, ok := pm["containerPort"].(int64); ok {
					declared[n] = true
				}
				if n, ok := pm["name"].(string); ok {
					names[n] = true
				}
			}
			if len(ports) == 0 {
				continue // nothing declared: cannot tell
			}
			for _, pk := range []string{"readinessProbe", "livenessProbe", "startupProbe"} {
				probe, ok := c[pk].(map[string]any)
				if !ok {
					continue
				}
				switch v := probePortValue(probe).(type) {
				case int64:
					if !declared[v] {
						bad = append(bad, fmt.Sprintf("%s %s probes undeclared port %d", cName(c), strings.TrimSuffix(pk, "Probe"), v))
					}
				case string:
					if !names[v] {
						bad = append(bad, fmt.Sprintf("%s %s probes unknown port name %q", cName(c), strings.TrimSuffix(pk, "Probe"), v))
					}
				}
			}
		}
		if len(bad) > 0 {
			o.Findings = append(o.Findings, w.finding(Medium, strings.Join(bad, "; ")))
		}
	}
	return o
}

func probePortValue(probe map[string]any) any {
	p, _ := probePort(probe)
	return p
}
