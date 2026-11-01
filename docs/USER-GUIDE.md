# User guide

Everything Soliton Rudder can do, grouped by what you're trying to accomplish. For a quick tour see
[Getting started](GETTING-STARTED.md); for GitOps/monitoring see [Integrations](INTEGRATIONS.md).

- [Clusters and the hotbar](#clusters-and-the-hotbar)
- [Browsing resources](#browsing-resources)
- [The detail panel](#the-detail-panel)
- [Editing, scaling, restarting, deleting](#editing-scaling-restarting-deleting)
- [Terminals](#terminals)
- [Logs](#logs)
- [Port forwarding](#port-forwarding)
- [Applying manifests](#applying-manifests)
- [Debug containers](#debug-containers)
- [Ingress map](#ingress-map)
- [Topology](#topology)
- [Simulator](#simulator)
- [Compare clusters and promote](#compare-clusters-and-promote)
- [Blackbox and runbooks](#blackbox-and-runbooks)
- [Certificates](#certificates)
- [ConfigMaps and Secrets](#configmaps-and-secrets)
- [Health, events and metrics](#health-events-and-metrics)
- [Assessments](#assessments)
- [Cluster overview](#cluster-overview)
- [Applications](#applications)
- [Limited access](#limited-access-namespace-scoped-permissions)
- [Going back and forward](#going-back-and-forward)
- [Command palette](#command-palette)
- [Themes and settings](#themes-and-settings)
- [Plugins](#plugins)
- [Keyboard shortcuts](#keyboard-shortcuts)
- [Permissions your cluster user needs](#permissions-your-cluster-user-needs)

## Clusters and the hotbar

The **hotbar** is the rail on the left. From top to bottom: the logo (Home), **Home**, your **pinned** clusters and the
ones that are **connected**, **+** (add), the three theme dots, **Compare clusters**, and **Settings**. **Home lists every
cluster**; pin the ones you use most with the pin button on their card (or right-click → *Pin to hotbar*). Pinned clusters
come first; clusters that are only open (connected but not pinned) follow below a thin divider and leave the hotbar when
you disconnect them. A small bar on the rail's outer edge marks the cluster you are looking at.

**Connecting.** A cluster connects when you open it, or with the plug button on its Home card (or right-click →
*Connect*). *Disconnect* (plug button or right-click) closes the connection: live views stop, the cluster's port-forwards
stop, and an unpinned cluster leaves the hotbar. Opening it again reconnects. Two things respect a disconnect: the
**Blackbox recorder** pauses (it shows "disconnected" and resumes when you connect again) and it never reopens the
cluster behind your back. Anything you start yourself (a comparison, an assessment) connects the cluster again.

**The ring around each icon** shows the state: **green** = connected and healthy · **red** = disconnected, or connected
but not reachable (hover for the reason) · **amber** = checking or unauthorized. A small bar at the left edge marks the
cluster you are looking at. Unpinning a cluster that is still connected keeps it on the hotbar until you disconnect it.

On **Home**, the search box filters clusters as you type (words are matched against the name, the context and the status,
so `unreachable` finds the broken ones); **Esc** clears it and **Enter** opens the cluster when exactly one matches.
The **Cards / List** switch changes the layout: the list shows name, context, status, version and the error text in
columns. The choice is remembered.

- **Add**: *From file* (a reference; changes to the file are picked up) or *Paste kubeconfig* (credentials go to the
  macOS Keychain). A kubeconfig with many contexts lets you tick several at once.
- **Reachability** is checked every 30 s while the window is visible, for every cluster (connected or not). The Home card
  shows the status and the error text.
- **Edit / pin / delete**: right-click a cluster. Drag pinned clusters to reorder them.
- **Deleting** removes the cluster, its port-forwards and assessment history from Soliton Rudder only.

## Browsing resources

- **The sidebar** starts with the cluster's two front pages, **Overview** and **Applications**, which are always visible. Below
  them are three collapsible sections: **Tools** (Assessments, Topology, Compare clusters, Blackbox, Simulator, Ingress Map,
  Certificates, Port Forwards), **Integrations** (Argo CD, Flux, Helm, Prometheus; plus **Plugins** when a plugin adds panels)
  and **Resources**. Click a section's title to fold it away; the choice is remembered, and a dot on a folded title shows
  that the page you are on is inside it.
- **Resources** lists every kind the cluster serves, grouped as **Cluster**, **Workloads**, **Network**, **Config**,
  **Storage**, **Access Control** and **Custom Resources** (grouped by API group). The **Filter kinds…** box accepts kind
  names and short names, and shows its matches even inside folded groups.
- Data arrives by **watch**: only the kind you have open is watched, and changes are batched ~10×/second, so large
  clusters stay smooth. If the connection drops it reconnects with backoff; the dot next to the title shows
  *live / connecting / error*.
- **Namespaces**: pick one or several, or *All*. Your choice is remembered per cluster.
- **Search** filters across all visible columns. **Sort** by clicking a header (click again to reverse, a third
  time to clear). **Wide** toggles extra columns the API marks as secondary.
- Status columns are colour-coded; **Age** ticks live. Right-click a row for *Edit YAML*, *Copy name*, *Delete…*.

## The detail panel

Click a row. Tabs: **Overview**, **Data** (ConfigMaps/Secrets only), **YAML**.

**Overview** shows:

1. **Health**: a verdict (healthy / degraded / failing) with the individual checks and *what to do next*. Covers
   pods (phase, scheduling, per-container state, restarts, OOMKilled with the memory limit, readiness), Deployments,
   StatefulSets, ReplicaSets, DaemonSets, Jobs, Nodes (Ready, pressure, cordon, taints) and PVCs.
2. **Probes** (pods): every container's liveness, readiness and startup probes, with the container's state and restarts.
   Each probe shows what Kubernetes calls (`GET /healthz :8080 (HTTPS)`, `TCP :5432`, `exec …`), how often, the timeout and
   the failure threshold, and has buttons:
   - **Check** runs the probe now, from your Mac through a temporary port-forward, and shows Kubernetes' verdict (HTTP 200–399
     is success), the status, the time and the start of the response body. For TCP probes it tells whether anything listens.
   - **Open** forwards the port and opens the endpoint in the browser (HTTP probes).
   - **Copy** copies a `curl` / `nc` command that reproduces the probe from inside the cluster.
   Exec probes run inside the container (use *Shell*); a probe on a named port the container does not declare is flagged.
3. **Usage** (pods, nodes, workloads): CPU and memory over time with request/limit lines (and capacity for nodes).
   Without Prometheus you get the last ~15 minutes from metrics-server. With
   [Prometheus connected](INTEGRATIONS.md#prometheus) you can choose 1 h / 6 h / 24 h / 7 d.
4. Metadata (created, namespace, owner, finalizers, UID), **labels**, **annotations**.
4. **Timeline**: events for the object *and* the pods/ReplicaSets it owns, newest first.

## Editing, scaling, restarting, deleting

- **YAML tab**: edit, then **Save**. **⌘F** / **Ctrl+F** opens find (**⌘G** / **F3** next, **⇧F3** previous, **Esc** closes). It performs an update with optimistic concurrency, so a conflicting change
  made elsewhere is reported instead of overwritten. `managedFields` are hidden to keep the text readable.
- **Scale** (Deployments, StatefulSets, ReplicaSets, ReplicationControllers) and **Restart** (Deployments,
  StatefulSets, DaemonSets; equivalent to `kubectl rollout restart`).
- **Delete** always asks for confirmation. Deletes use background propagation.

## Terminals

Terminals live in the **dock** at the bottom (toggle with **⌃`**, resize by dragging its top edge, several tabs).

The dock belongs to the **open cluster**: its tab bar starts with that cluster's name and colour and lists only that
cluster's terminals, logs and apply tabs, so a shell can never be mistaken for another cluster's. Switching to another
cluster hides these tabs without closing them; they are exactly as you left them when you come back (the hotbar icon
shows a small counter while a cluster still has open tabs). Disconnecting or removing a cluster closes its tabs.

- **Pod shell**: *Shell* in a pod's detail panel. Uses bash if present, otherwise sh. Containers without a shell
  (e.g. distroless images) report an error in the terminal.
- **Cluster terminal**: *Terminal* in the list toolbar starts your login shell with `KUBECONFIG` pointing at a
  private, temporary, single-context copy of the selected cluster. `kubectl` and `helm` act on that cluster. The
  temporary file is deleted when the terminal closes.
- Closed or ended sessions show **Reconnect**.

## Logs

Open from **Logs** on a pod, Deployment, StatefulSet, DaemonSet, ReplicaSet, Job or Service.

- **Sources**: choose pods/containers (all selected by default, up to 20 streams at once). Multi-source output is
  colour-tagged per pod.
- **Range**: last 100/500/1000/5000/all lines; since 5 min/1 h/24 h.
- **Toggles**: *Follow*, *Time* (timestamps), *Wrap*, *JSON* (formats JSON log lines as message + fields),
  *Previous* (the prior container instance, for crash investigations).
- **Levels**: error / warn / info / debug chips filter lines; levels are detected from JSON fields and common text
  patterns.
- **Search** highlights matches; **Filter** hides non-matching lines. **Latest** jumps back to live tail after you
  scroll up. The viewer keeps the newest 50,000 lines.
- **Download** saves what is shown to a file.

## Port forwarding

**One click:** hover a Pod, Service, Deployment, StatefulSet, DaemonSet or ReplicaSet in any list and click the plug
icon (or right-click → *Forward (auto port)*). Soliton Rudder picks a free local port and shows
`localhost:54321 → web:80`. If the object declares several ports you are asked which one; clicking again reuses the
same forward instead of creating copies. *Port Forwards* in the sidebar lists everything, and **Add forward** /
the **Forward** button in the detail panel let you choose ports and a fixed local port (1024–65535).

**Opening the browser.** When the target port looks like a web port (80, 443, 3000, 8000–8999, 9090…), the forward opens
`http://localhost:<port>` (https for 443/8443) in your browser as soon as it is ready: automatically after the one-click
forward, and with the *Open in the browser when ready* option of the Forward dialog (on by default for web ports, off for
databases and caches; you can switch it either way). Forwards in the list always have an *Open in browser* button.

**Self-healing.** A forward is bound to what the target *is*, not to one pod:

- **Services** follow their selector and map `port → targetPort` (including named ports); **workloads** follow their
  selector; a **pod** follows the workload that owns it, so when a Deployment pod is replaced or rolled, the
  forward moves to the newest ready replica. (StatefulSet and bare pods come back under the same name, so the
  forward waits for that exact pod.)
- The **local port stays open** the whole time. When the pod crashes, is OOM-killed, evicted, starts terminating or
  the rollout replaces it, the tunnel is rebuilt on a healthy pod without you touching anything; **new connections
  wait (up to 30 s) for the replacement instead of being refused.** A pod that is merely *terminating* is replaced
  proactively and its in-flight connections are allowed to finish (draining) before the old tunnel closes.
  Connections that were already open to a pod that dies are broken by the network, as with any tunnel; clients
  should reconnect.
- **Status** per forward: *Active*, *Healing* (switching pods), *Degraded* (no ready pod yet; the reason is shown and
  connections queue), *Connecting*, *Error* (e.g. the local port is taken; it keeps retrying) or *Stopped*.
- **Health metrics**: an **auto-healed** badge for two minutes after each repair, and a line such as
  *healed 2× · last 14 s ago: pod web-6d9f: is terminating*. The header sums all repairs.
- **Auto** starts the forward whenever the cluster connects; **Open in browser** appears when it is active.
  Forwards stop when you quit the app (their definitions are kept).

Needs `create` on `pods/portforward` and `get`/`list` on pods and Services (plus `get` on the owning workloads for
pod forwards).

## Debug containers

For pods whose image has no shell or tools (distroless, scratch), select the pod and click **Debug**. Soliton Rudder
uses Kubernetes' *ephemeral containers* API (the same mechanism as `kubectl debug`) to add a temporary container to the
**running** pod and opens a terminal in it.

1. Pick an **image** (presets: `nicolaka/netshoot` with curl/dig/tcpdump/ss/nmap, `busybox`, `alpine`, `ubuntu`, or
   any image you type; your last choice is remembered).
2. **Share processes of container** selects the app container whose processes you want to see (`ps`, `strace`,
   `/proc/<pid>/root`). The debug container always shares the pod's network namespace, so `curl localhost:8080` and
   `ss -tlnp` show what the app sees.
3. **Add NET_ADMIN and SYS_PTRACE** enables packet capture (`tcpdump`) and tracing. Turn it off if an admission
   policy (Pod Security *restricted*) rejects it.
4. **Start debug container** waits until it is running and opens a terminal tab titled *Debug: <pod>*.

Good to know:

- **Debug containers cannot be removed.** They stay (idle) until the pod is recreated. Re-open *Debug* to **Attach**
  to existing ones instead of adding more.
- Needs Kubernetes **1.23+**, the `patch` permission on `pods/ephemeralcontainers` and `create` on `pods/exec`.
  The cluster must be able to pull the image (private/air-gapped clusters need a mirrored image).
- Only running pods can be debugged. If the container can't start (image pull error) you get the reason instead of a
  hanging dialog.

## Applying manifests

**Apply** (list toolbar, ⌘K → *Apply manifest…*) opens an editor in the dock.

1. Paste YAML/JSON (multiple documents separated by `---`, or a `kind: List`), **Open file**, or load **From URL**
   (http/https, up to 5 MB).
2. **Preview changes**: a server-side dry-run. Each document is labelled *create / configure / unchanged / error*,
   with a before/after diff (unchanged sections collapsed).
3. **Apply N changes**: server-side apply with field manager `soliton-rudder`. Enable **Force conflicts** to take
   over fields owned by another tool.

Namespaced objects without a namespace use the namespace selected in the list (or `default`).

## Ingress map

*Ingress Map* (shown when the cluster has Ingress) draws each route as **host → path → Service:port → pods**, with
ready pods as green dots. It also flags problems:

- missing Service or wrong port; no endpoints or no *ready* pods;
- duplicate/conflicting host+path across ingresses;
- TLS: missing secret, **certificate expiry** (warning < 30 days, error < 14 days / expired), certificate not
  covering the host (wildcards understood), hosts without TLS;
- no address assigned, no ingress class.

Filter by namespace, search by host/path/service, or show **Issues only**. Click a Service to open it.

**Opening URLs.** The host, the path chip and the arrow icon are links: they open the address in your default browser
(`https://` for hosts with TLS, `http://` otherwise). A rule without a host uses the ingress's load balancer address.

**Which addresses open.** Path patterns written for nginx (`/api(/|$)(.*)`) open their literal part (`/api`). Wildcard hosts
(`*.example.com`) have no single address, so they are shown as plain text. If an address cannot be opened you get a message
instead of nothing.

**In the Ingresses table:** the **Hosts** and **Address** columns are links too (https when the ingress listens on 443);
clicking one opens it without selecting the row. Select a row and the **Overview** tab of the side panel has a **Routing**
section with the same details as below.

**Details.** Click an ingress's header to expand it. Sections, top to bottom:
- **URLs**: all routable URLs of the ingress, with a lock for TLS and a copy button;
- **Backends**: for every route, the Service and port, the **Service IP** (ClusterIP; *headless* when there is none),
  the Service type if it is not ClusterIP, and the **pod IPs** behind it (green = ready, red = not ready);
- **TLS**: secret, issuer, days left and covered hosts;
- **Addresses** (last): every address the controller reported, marked **IP** or **Hostname**, each with *copy* and *open* buttons.

## Certificates

*Certificates* (sidebar → Tools) lists every `kubernetes.io/tls` Secret in the cluster with:

- **time left** (red when expired or under 14 days, amber under 30), validity dates and a progress bar;
- **domains/addresses** (the certificate's SANs), subject, **issuer** with a badge for *Let's Encrypt*, *staging*,
  *self-signed*; key algorithm, serial and chain length;
- **cert-manager** status when a `Certificate` owns the Secret: Ready, planned renewal time and the failure message,
  which is often the quickest way to see why a renewal is stuck;
- **Used by**: the Ingresses that reference the Secret.

Filter by *Expired*, *Expiring < 30d* or *Unreadable* (missing/invalid `tls.crt`), search by name/domain/issuer, and
restrict to namespaces. Click a row for details and **Copy** the domain list. The same certificate summary also
appears at the top of a certificate key in a Secret's **Data** tab. Expiring certificates are included in
[Assessments](#assessments) as well.

The list needs `list` on `secrets`. Private keys (`tls.key`) are discarded the moment the response arrives and are
never shown, stored or logged.

## ConfigMaps and Secrets

The **Data** tab shows one card per key with an auto-detected format: **JSON** (pretty-printed), **YAML**, **env**
(a table), **certificate** (subject, issuer, expiry), plain text or binary. Values over 1 MiB are truncated for display.

For **Secrets** the values are not even sent to the UI until you click the eye icon for that key; the copy button
fetches the value on demand. Reading Secrets requires `get` on `secrets`; each reveal is a normal API read.

## Health, events and metrics

- *Health* (see [The detail panel](#the-detail-panel)) is computed from the object's status, so it explains
  *why*, not just *what*.
- **CPU / Memory columns** and charts need **metrics-server**. Without it the UI says so and carries on.
- Charts hover to read exact values. Dashed lines are the pod's CPU/memory *request* (amber) and *limit* (red);
  node charts scale to allocatable capacity.

## Assessments

*Assessments* in the sidebar runs **36 read-only checks** (see [the full list](ASSESSMENT-RULES.md)) across
**Resources, Reliability, Security, Maintenance** and gives the cluster a **score from 0 to 100**.

- **Run assessment** reads the cluster once (progress bar, cancellable), evaluates, and saves the report.
  Nothing is changed in the cluster.
- **Score**: each failing check costs points by severity (critical 20, high 10, medium 5, low 2), scaled from 40 %
  (a few objects affected) to 100 % (all of them). Category scores use the same formula within the category.
  Labels: 90+ Excellent · 75+ Good · 60+ Needs attention · 40+ At risk · below 40 Critical (the gauge is green from 85, amber from 60, red below).
- **Findings** are grouped by check with *how to fix*; click an object to open it. Filter by category, severity or
  text. Checks that couldn't run (missing permission, no metrics-server) are listed as **skipped**: never silently
  "passed".
- **History**: every run is kept (latest 100 per cluster). The **trend chart** can show the overall score or one
  category. **Changes since…** compares two runs: *new*, *resolved* and unchanged findings per check.
- **Export**: HTML (print-ready: *File → Print → Save as PDF*) or raw JSON.
- Plugins can add their own checks to the report ([PLUGINS.md](PLUGINS.md)).

### Fix proposals

Findings from fixable checks (failed pods, missing requests/limits/probes, security context, NetworkPolicy,
single replica, PodDisruptionBudget, over-provisioning…) have a **Fix…** button. It builds a YAML proposal, runs a
**server-side dry-run** and shows the before/after diff; nothing is changed until you press **Apply fix**.

- Each proposal states its **risk** (high-risk ones need an explicit acknowledgement) and notes, e.g. *pods fail to
  start if the image runs as root*.
- If **Helm, Argo CD or Flux** manages the workload, a warning explains the change will be reverted: fix it in Git.
- *Take over fields* (force) is offered only when the dry-run reports a field-ownership conflict.
- Applies use your own credentials and the field manager `soliton-rudder`.
- Pod findings are fixed on the owning controller, not the pod.

### Cost (FinOps) tab

An **estimate** of monthly cost = requested CPU/memory × rates (defaults from OpenCost: $0.031611 per vCPU-hour,
$0.004237 per GB-hour, 730 h/month). **Potential savings** compare requests with real usage plus headroom (default
20 %). Usage is the **Prometheus 95th percentile over 24 h** when a Prometheus integration exists, otherwise a single
metrics-server sample (lower confidence; shown on the card). Without usage data no savings are claimed. Rates,
headroom and *production namespaces* (patterns; also detected from `env`/`environment` labels and names such as
`prod`) are set in *Settings → Cluster Doctor*. Production namespaces get stricter severity for single-replica
workloads and missing PodDisruptionBudgets.

## Topology

*Topology* in the sidebar draws how traffic and ownership connect, left to right:
**Ingress → Service (and HPA) → Workload → Pod → Node**, with optional ConfigMaps, Secrets and PVCs (*Config* button).

- Every node carries a status colour (green / amber / red / grey) computed from live state: unready or crash-looping
  pods, a Service whose selector matches nothing, a Deployment with fewer ready replicas than desired, a cordoned or
  NotReady node, an HPA at its maximum, an Ingress pointing at a Service that does not exist (drawn dashed).
- **Click a node** to light up everything upstream and downstream of it and see its details; *Open in table* jumps to it.
- **Problems only** keeps just the paths that lead to a failing object, so a broken Ingress → Service → Pod chain is
  visible at a glance. The search box keeps the matching objects and everything connected to them.
- The graph refreshes every 5 seconds. With one namespace selected only that namespace is read; otherwise all of them.
- Pods are grouped under their Deployment / StatefulSet / DaemonSet / CronJob (ReplicaSets and Jobs are folded in).
  At most 6 pods per workload are drawn, failing ones first; the workload says how many exist. Very large clusters are
  capped at 800 objects (errors are kept) with a note asking you to pick a namespace.
- Only object *names* are used for Secrets and ConfigMaps; no values are read.

## Simulator

*Simulator* answers "what would happen if…" questions from the cluster's current state. It is **read-only**: nothing
is sent over the network, cordoned or evicted.

**Network policy**: pick a source (a Pod or an external IP), a destination (Pod, Service or external IP), a port and
protocol, and press *Simulate*. The source's **egress** and the destination's **ingress** are both evaluated against
all NetworkPolicies, and the result names the policy that allows the traffic, or the policies that isolate the pod when
no rule matches. Selectors (`matchLabels`, `matchExpressions`), namespace selectors, `ipBlock` with `except`, port ranges
(`endPort`) and **named ports** are supported; a Service is expanded to its backing pods, translating the Service port to
the target port, and the result can be *partial* when only some pods are reachable.
Limits: enforcement depends on your CNI (Flannel without a policy add-on ignores NetworkPolicies), and CNI-specific
policies (Calico, Cilium) are not evaluated.

**Node drain**: pick a node to see what `kubectl drain` would do:
- **Pods**: which are evicted and where the scheduler is expected to put them (CPU/memory requests, taints and
  nodeSelector are considered); DaemonSet and static pods are ignored; bare pods and pods with no room are flagged
  as blocking; emptyDir data loss and single-replica downtime are flagged as warnings.
- **PodDisruptionBudgets**: a PDB that allows **no disruption right now** is reported as blocking (the classic drain that
  never finishes); a PDB that allows fewer disruptions than pods on the node means evictions proceed one at a time.
- **Capacity**: CPU and memory the evicted pods need versus what is free on the remaining schedulable nodes.
- **Autoscaling after the drain**: whether each HPA can still reach its `maxReplicas` on the remaining capacity.

Pod affinity, topology spread and volume zones are not modelled, so treat *safe* as "no problem found", not a guarantee.

## Compare clusters and promote

*Compare clusters* (hotbar button, *Tools → Compare clusters* inside a cluster, the button on **Home**, or *⌘K → Compare clusters…*) shows what differs between two environments
and lets you carry **selected** changes from one to the other, for example from *staging* to *production*.

1. Pick the **source** and **target** cluster and namespace (they can be two namespaces of the same cluster). Clusters that
   are not open are connected to on demand; the namespace list shows *Loading namespaces…* or the reason it failed, with *Retry*, choose the
   kinds to compare (Deployments, StatefulSets, DaemonSets, CronJobs, ConfigMaps, Services, Ingresses, HPAs, PDBs,
   NetworkPolicies, ServiceAccounts) and press *Compare*. Comparing only reads.
2. The list shows each object as **differs (N fields)**, **missing in target**, **only in target** or *same*
   (*Only differences* is on by default).
3. Click an object: the **Fields** tab lists every differing field with the target value and the source value;
   **YAML diff** shows the whole object. Server-assigned data (status, uid, resourceVersion, managedFields, clusterIP,
   nodePort…) and noise such as the `deployment.kubernetes.io/revision` annotation are ignored. Lists such as containers,
   env and ports are matched by name/port, so adding one variable does not look like the whole list changed.
4. **Nothing is selected by default.** Tick the fields to promote. Fields that usually differ on purpose (replicas,
   resources, hostnames, node placement, storage class) are tagged **env-specific** and are skipped by
   *Select all except environment-specific*. Fields that exist only in the target cannot be promoted (removing fields is
   not supported).
5. *Promote N fields…* builds the **smallest possible manifest** containing only those fields and runs a **server-side
   dry-run on the target**, showing the exact before/after. Only when you press **Apply to <target>** is it written, with
   your credentials and the field manager `soliton-rudder`. If another tool (Helm, Argo CD, kubectl) owns a field you will
   see a conflict and can choose *Take over fields*. In GitOps-managed targets, change Git instead.
6. An object **missing in the target** can be created there with *Create in <target>…* (same dry-run and confirmation).

**Secrets are never read, compared or copied** and are not offered as a kind. ConfigMaps can contain sensitive data, so
review them like any other value. Objects owned by a controller (for example generated ConfigMaps) are skipped.

## Blackbox and runbooks

*Blackbox* is a flight recorder for a cluster: you go back to the moment something broke, see what was failing around it,
and follow a step-by-step guide.

**Recording.** Open *Blackbox → Recording*, choose how often to check (5 s – 2 min), optionally which namespaces, how long
to keep data (1 h – 7 days) and a record cap, then *Start recording*. Pods, nodes, Deployments, StatefulSets and
DaemonSets are watched, and **only changes are stored** (a pod going from Running to CrashLoopBackOff, a node becoming
NotReady, a restart counter increasing) together with the cluster's Warning events and the Normal events that explain what
happened (scaling, scheduling, kills, evictions). A quiet cluster costs almost nothing. Old data is pruned automatically
and the last known state of every live object is kept so the past can always be reconstructed.
- Recording happens **only while Soliton Rudder is open**. There is no data for the time it was closed; when it starts
  again, the changes that happened in between are recorded at that moment.
- Data stays on your Mac. Secrets, logs and object contents are never recorded. *Delete recordings…* removes everything.
- Events older than about an hour are no longer kept by Kubernetes itself, so what happened before you started recording
  is mostly lost.

**Time travel.** The density strip shows when problems occurred (red = errors, amber = warnings). Drag the slider (or press
play, at 10×, 1 min/s or 5 min/s) to move to any moment. The left pane reconstructs **what was failing at that time** (and
counts what was healthy); the right pane shows **what happened just before it** (1 min – 1 h window, filter by severity or
text). *Back to live* returns to the present.

**Runbooks.** Click any failing object or event to open the matching runbook: CrashLoopBackOff, image pull errors,
OOMKilled, Pending/unschedulable pods, NotReady nodes, unavailable or degraded workloads, evictions and volume mount
problems are built in. Each step explains what to look for.
- **Run check** steps are read-only and run in place: health summary, events, manifest, current or previous container logs.
  Their output is shown under the step. Secrets are never shown.
- Steps that **change the cluster** (restart a workload, scale, delete a pod) need a second explicit confirmation. *Debug*
  opens the ephemeral debug container dialog, and *Open* jumps to the object.
- If nothing matches, *Browse all runbooks* lists every runbook for the object.
- **Your own runbooks**: *Runbooks…* opens a YAML editor (with an example). A runbook has an `id`, `title`, `match`
  (`kinds`, case-insensitive `reasons`, `status`) and `steps` (`do`: `logs`, `previous-logs`, `events`, `health`, `yaml`,
  `restart`, `scale` with `replicas`, `delete`, `debug`, `open`, `manual`). `{{name}}`, `{{namespace}}` and `{{kind}}` are
  replaced in titles and texts. Invalid YAML is rejected with the reason; an `id` equal to a built-in's replaces it.

## Cluster overview

A cluster **opens on its Overview** (sidebar → *Overview*, also in ⌘K). It refreshes every 15 seconds and shows:

- **Nodes, Pods, Workloads, Namespaces and Warnings** as cards, each saying what is wrong (*1 not ready · 1 cordoned*,
  *4 not ready · 3 pending · 2 failed*, *2 failing · 4 degraded*). A workload scaled to zero is deliberate and counts as healthy.
- **Capacity**: CPU and memory *requested* against what the nodes can allocate (translucent bar) and, when metrics-server is
  available, what is actually *used* (solid bar). The bar turns amber above 75 % and red above 90 % requested.
- **Assessment**: the latest score, how long ago, and a trend line; one click opens the report (or offers to run one).
- **Needs attention**: NotReady or cordoned nodes, failing and degraded workloads, crash-looping, image-pull-failing and
  long-pending pods (new pods get a 5 minute grace period). Each row has **Runbook** and an *open* button. Errors come
  first, then nodes, workloads and pods.
- **Recent warnings**: Warning events of the last hour, merged per object and reason with a count; click one to open the object.
- Shortcuts to the active **port forwards** and the **Blackbox** state.

Opening a cluster no longer starts a table stream: the table of a kind (for example Pods) starts when you pick it.

## Applications

*Applications* (sidebar, under Overview) answers: *is this application running, how was it deployed, what changed?* Kubernetes
has no "application" object, so one is recognised from what the tools that deploy it leave behind. In this order:

1. a **Helm release** (the `meta.helm.sh/release-name` annotation on its objects, and the release's own history);
2. the `app.kubernetes.io/part-of` label, then `app.kubernetes.io/instance`;
3. the legacy `app` / `k8s-app` label;
4. optionally the **namespace** (*Group the rest by namespace*, remembered per cluster): useful when each team or product
   has its own namespace, misleading elsewhere, so it is off by default.

Objects that match none of these are listed under *Not part of any application*. Every application says **how it was
grouped**. Environments never merge: the same name in two namespaces is two applications.

**The list** shows health (healthy workloads and ready pods, restarts), version, last change and the first URL; problems come
first. Filter by text (name, namespace, version, chart, image), by namespace (the picker), or *Problems only*.

**An application** has three tabs:
- **Overview**: workloads, pods, restarts and last change; **Needs attention** (a failed or stuck Helm release, many kept
  revisions, failing workloads and pods, each with *Runbook*); **Links** (ingress URLs, links declared on its objects, your
  own templates); versions (chart, app version, images).
- **Resources**: its workloads, services, ingresses and autoscalers, plus pods that have a problem. Click one to open it.
- **History** (Helm applications): the revisions with status, chart and description, and **what changed** between any two:
  *Values* (what was supplied at deploy time) or *Manifest* (what Helm rendered), side by side. **Roll back…** shows the
  manifest difference first and only then asks for confirmation; Helm redeploys the old revision as a new one. It needs the
  `helm` command on your Mac.

**Links.** *Links…* defines links shown on every application of the cluster, for example the CI job that deploys it or its
dashboard: `https://ci.example.com/job/{release}`. Placeholders: `{name}`, `{namespace}`, `{release}`, `{version}`. Objects can
also declare their own links with annotations `link.argocd.argoproj.io/<title>: <url>`. Only `http` and `https` addresses are accepted.

How a Helm application's version is chosen: the `app.kubernetes.io/version` label, else the single image tag, else the chart
version. Works with [limited access](#limited-access-namespace-scoped-permissions) (Helm releases are read per namespace).
Argo CD and Flux applications are not grouped yet; their objects appear through their labels, and the Argo CD and Flux views remain.

## Limited access (namespace-scoped permissions)

Many teams only get access to their own namespaces. Soliton Rudder detects this when a cluster opens (it asks the API server
whether your credentials may list pods in all namespaces, using *SelfSubjectAccessReview*; if that is not available it tries
the request) and then **works namespace by namespace** instead of failing:

- The namespaces it uses are, in this order: the ones **you chose** (hotbar → right-click a cluster → *Access…*, the *Choose
  namespaces…* link in the namespace picker or on the Overview banner), the ones it **discovered** (when you may list
  namespaces, it keeps those where you may list pods), or the **namespace of your kubeconfig context**. With none of these it
  tells you to choose.
- **Resource tables** stream each namespace and merge them into one table; a namespace you may not read is skipped without
  turning the table into an error. If nothing at all is readable you get a message that says so.
- **Overview, Topology, Ingress map, Simulator, Assessments, Blackbox and the plugin API** use the same namespaces. The Overview
  shows a *Limited access* banner and counts only what you can see. Cluster-scoped objects you cannot read (nodes) are left out
  rather than reported as problems; assessment rules that need them are listed as skipped.
- The namespace picker offers the namespaces you can use ("My namespaces") even when the cluster's namespace list cannot be read.
- Nothing here grants anything: it only uses your own rights. If the check itself fails (network trouble) the app assumes full
  access and asks again next time. Permissions are checked again every five minutes, and when you change the namespaces.

## Going back and forward

The **◀ ▶ buttons** at the left of the title bar (and **⌘[** / **⌘]**, or the mouse's back and forward buttons) walk through the
places you have been: Home, a cluster, a tool such as Assessments or Topology, a resource kind. Going back to another cluster
reconnects it first. Opening a cluster counts as one place, going back and then somewhere new drops the forward history, and the
last 50 places are kept. The keys are left alone while you type in a text field.

## Command palette

**⌘K**: resource kinds, namespaces (sets the filter), clusters, *Home*, *Add cluster*, *Settings*, themes, *New
terminal*, *Apply manifest…*, *Ingress map*, *Topology map*, *Simulator*, *Blackbox*, *Cluster assessments*, *Port forwards*, integrations, and commands
contributed by plugins. Fuzzy matching; the most relevant result is first.

## Themes and settings

Three themes: **Dark Black**, **Dark Blue**, **Light**. Switch from the hotbar dots or *Settings → Appearance*.

*Settings* (gear icon):

| Section | Contents |
|---|---|
| **Appearance** | Theme previews. |
| **General** | Daily update check on/off and *Check now*; data folder (*Show in Finder*); *Clear assessment history*; *Reset preferences*. |
| **Shortcuts** | The list below. |
| **Plugins** | Install, enable, permissions, logs, uninstall. |
| **About** | Version, links, third-party licenses. |

## Plugins

Plugins add panels, ⌘K commands and assessment checks. Install from *Settings → Plugins* (folder or `.zip`);
review and grant permissions during install, change them later. Backends are separate processes that run with
**your** user privileges, so install only plugins you trust. Details and how to write one: [PLUGINS.md](PLUGINS.md).

## Keyboard shortcuts

| Keys | Action |
|---|---|
| **⌘ K** | Command palette |
| **⌘ [** / **⌘ ]** | Back / forward |
| **/** | Focus the table's search box |
| **↑ ↓ PgUp PgDn Home End** | Move selection in a resource table (focus the table first) |
| **Esc** | Close detail panel / dialog / palette |
| **Tab / ⇧ Tab** | Next / previous control (focus stays inside dialogs) |
| **⌃ `** | Show / hide the dock |
| **⌘ Enter** | Run query in the Prometheus explorer |

## Permissions your cluster user needs

Soliton Rudder can only do what your kubeconfig user may do. Read-only access is enough for browsing. Features
degrade gracefully when a permission is missing (you'll see the API's *forbidden* message).

| Feature | Kubernetes permission |
|---|---|
| Browse / live tables | `list`, `watch` on the resource (cluster-wide for "All namespaces") |
| YAML view, health, events | `get` on the object; `list` on `events` |
| Edit / apply | `update` / `patch` (and `create`) |
| Delete / scale / restart | `delete` · `patch` on `…/scale` · `patch` on the workload |
| Pod shell | `create` on `pods/exec` |
| Logs | `get` on `pods/log` (+ `list` on pods to resolve workloads) |
| Port forward | `create` on `pods/portforward` (+ `get`/`list` on Services/pods) |
| CPU/Memory | `list` on `metrics.k8s.io` `pods`/`nodes` |
| Ingress map | `list` on `ingresses`, `services`, `endpointslices`; `get` on TLS `secrets` for expiry |
| Secrets (Data tab) | `get` on `secrets` |
| Certificates view, certificate-expiry check | `list` on `secrets` (TLS only; keys discarded), optionally `list` on `certificates.cert-manager.io`, `ingresses` |
| Debug container | `patch` on `pods/ephemeralcontainers`, `create` on `pods/exec`, Kubernetes 1.23+ |
| Assessments | `list` on nodes, pods, namespaces, workloads, services, PVC/PV, HPA, PDB, ingresses, networkpolicies |
| Helm (read) | `list`, `get` on Helm release `secrets` (or `configmaps`) |
| Argo CD / Flux | `list`/`get`/`patch` on their custom resources |
| Prometheus | `get` on `services/proxy` for the Prometheus Service |
