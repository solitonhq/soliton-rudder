package runbook

// Builtins are the runbooks shipped with the app. They are guidance plus safe read-only checks.
func Builtins() []Runbook {
	rbs := []Runbook{
		{
			ID: "crashloop", Title: "Container keeps crashing (CrashLoopBackOff)",
			Description: "{{name}} starts, exits and is restarted with growing delays.",
			Match:       Match{Kinds: []string{"Pod"}, Reasons: []string{"CrashLoopBackOff"}},
			Steps: []Step{
				{Title: "Read the health summary", Do: DoHealth, Text: "Shows exit codes, probe failures and what Kubernetes reports."},
				{Title: "Read the logs of the crashed container", Do: DoPreviousLogs, Text: "The previous run usually contains the fatal error. If the app logs nothing, it may be killed before it starts (bad command, missing file, OOM)."},
				{Title: "Check recent events", Do: DoEvents, Text: "Look for failed probes (Unhealthy), OOMKilled or mount errors."},
				{Title: "Review image, command, probes and limits", Do: DoYAML, Text: "A too-strict liveness probe or a memory limit that is too low is a common cause."},
				{Title: "Debug inside the pod namespace", Do: DoDebug, Text: "Attach an ephemeral container (netshoot/busybox) to test DNS, connectivity and files."},
				{Title: "Recreate the pod", Do: DoDelete, Text: "Only after you understand the cause: its controller creates a fresh one. This does not fix a bad configuration."},
			},
		},
		{
			ID: "image-pull", Title: "Image cannot be pulled",
			Description: "{{name}} is stuck because its image cannot be downloaded.",
			Match:       Match{Reasons: []string{"ImagePullBackOff", "ErrImagePull", "InvalidImageName", "ErrImageNeverPull"}},
			Steps: []Step{
				{Title: "Read the pull error in the events", Do: DoEvents, Text: "The message says whether the image does not exist, access is denied, or the registry is unreachable."},
				{Title: "Check the image name and tag", Do: DoYAML, Text: "Look for typos, a missing tag, or a tag that was never pushed."},
				{Title: "Verify registry credentials", Do: DoManual, Text: "For private registries the namespace needs an imagePullSecret that is referenced by the pod or its ServiceAccount, and the secret must be valid."},
			},
		},
		{
			ID: "oom-killed", Title: "Container was killed for using too much memory (OOMKilled)",
			Description: "{{name}} exceeded its memory limit.",
			Match:       Match{Reasons: []string{"OOMKilled"}},
			Steps: []Step{
				{Title: "Read the health summary", Do: DoHealth},
				{Title: "Compare usage with the memory limit", Do: DoYAML, Text: "Look at resources.limits.memory. Compare with the usage chart of the pod in the detail panel."},
				{Title: "Check the logs of the previous run", Do: DoPreviousLogs, Text: "A leak shows as steadily growing allocations; a spike shows as a burst of work right before the kill."},
				{Title: "Decide", Do: DoManual, Text: "Raise the limit (and request) if the usage is legitimate, or fix the leak. Cluster Doctor's right-sizing shows observed usage."},
			},
		},
		{
			ID: "pod-pending", Title: "Pod cannot be scheduled (Pending)",
			Description: "{{name}} is waiting for a node.",
			Match:       Match{Kinds: []string{"Pod"}, Reasons: []string{"Pending", "FailedScheduling", "Unschedulable"}},
			Steps: []Step{
				{Title: "Read the scheduler's explanation", Do: DoEvents, Text: "FailedScheduling lists how many nodes were rejected and why (insufficient cpu/memory, taints, affinity, volumes)."},
				{Title: "Read the health summary", Do: DoHealth},
				{Title: "Check requests, nodeSelector, tolerations and affinity", Do: DoYAML, Text: "A request larger than any node's free capacity, or a selector nothing matches, keeps a pod Pending forever."},
				{Title: "Check cluster capacity", Do: DoManual, Text: "Use Simulator → Node drain to see free CPU/memory per node, or add capacity."},
			},
		},
		{
			ID: "node-not-ready", Title: "Node is not ready",
			Description: "Node {{name}} stopped reporting as Ready.",
			Match:       Match{Kinds: []string{"Node"}, Reasons: []string{"NotReady", "NodeNotReady", "Cordoned"}},
			Steps: []Step{
				{Title: "Read the node conditions", Do: DoHealth, Text: "MemoryPressure, DiskPressure, PIDPressure or a lost kubelet heartbeat explain most cases."},
				{Title: "Check recent node events", Do: DoEvents},
				{Title: "Check the kubelet and the machine", Do: DoManual, Text: "On the node: `systemctl status kubelet`, `journalctl -u kubelet`, disk space (`df -h`) and network. Pods are evicted after the toleration period (5 minutes by default)."},
				{Title: "Find out what will move", Do: DoManual, Text: "Simulator → Node drain shows which pods are affected and whether the rest of the cluster has room."},
			},
		},
		{
			ID: "workload-not-ready", Title: "Workload has fewer ready replicas than desired",
			Description: "{{kind}} {{name}} is not fully available.",
			Match:       Match{Kinds: []string{"Deployment", "StatefulSet", "DaemonSet"}, Status: "error"},
			Steps: []Step{
				{Title: "Read the health summary", Do: DoHealth, Text: "Shows rollout progress and why replicas are not ready."},
				{Title: "Check recent events", Do: DoEvents, Text: "Failed creates (quota, admission webhooks), scaling decisions and image problems appear here."},
				{Title: "Inspect the pods", Do: DoOpen, Text: "Open the pods of this workload and run the matching runbook on the failing ones."},
				{Title: "Review the spec", Do: DoYAML},
				{Title: "Restart the rollout", Do: DoRestart, Text: "A rolling restart helps when pods are stuck in a bad state. It does not fix a bad configuration."},
			},
		},
		{
			ID: "workload-degraded", Title: "Workload is only partly available",
			Description: "{{kind}} {{name}} has some replicas not ready.",
			Match:       Match{Kinds: []string{"Deployment", "StatefulSet", "DaemonSet"}, Status: "warn"},
			Steps: []Step{
				{Title: "Read the health summary", Do: DoHealth},
				{Title: "Check recent events", Do: DoEvents},
				{Title: "Inspect the pods", Do: DoOpen, Text: "A rollout in progress is normal; replicas that stay unready are not."},
			},
		},
		{
			ID: "evicted", Title: "Pod was evicted",
			Description: "{{name}} was removed from its node.",
			Match:       Match{Reasons: []string{"Evicted", "Preempted"}},
			Steps: []Step{
				{Title: "Read why in the events", Do: DoEvents, Text: "Eviction is usually node pressure (memory, disk) or preemption by a higher-priority pod."},
				{Title: "Check requests and limits", Do: DoYAML, Text: "Pods without requests are evicted first under pressure (BestEffort)."},
				{Title: "Check the node", Do: DoManual, Text: "Open the node the pod ran on and look at its conditions and free disk."},
			},
		},
		{
			ID: "volume-mount", Title: "Volume cannot be attached or mounted",
			Description: "{{name}} is waiting for a volume.",
			Match:       Match{Reasons: []string{"FailedMount", "FailedAttachVolume", "ProvisioningFailed"}},
			Steps: []Step{
				{Title: "Read the mount error in the events", Do: DoEvents},
				{Title: "Check the PVC and the referenced ConfigMaps/Secrets", Do: DoYAML, Text: "A missing ConfigMap or Secret, a PVC that is Pending, or a volume still attached to another node blocks the pod."},
				{Title: "Check the storage class", Do: DoManual, Text: "A PVC stuck in Pending usually means no matching StorageClass or no capacity in the zone of the pod."},
			},
		},
	}
	for i := range rbs {
		rbs[i].Builtin = true
	}
	return rbs
}
