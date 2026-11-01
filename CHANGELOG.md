# Changelog

All notable changes are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and the project uses [Semantic Versioning](https://semver.org/) (0.x: minor versions may contain breaking changes).

## [Unreleased]

### Fixed
- **Terminals, logs and apply tabs no longer leak between clusters.** The dock is scoped to the open cluster: its tab bar
  shows that cluster's name and colour and only its tabs; other clusters' tabs stay alive in the background and come back
  as you left them, and the hotbar shows a counter for clusters that still have open tabs. Tabs close when their
  cluster disconnects or is removed.
- **Hotbar**: pinned clusters are now separated from clusters that are only open by a divider (pinned first), and the
  bar that marks the open cluster sits on the rail's outer edge instead of overlapping the icon's status ring.
- **Find in the YAML editor** works: **⌘F** and **Ctrl+F** both open it (on macOS Ctrl+F used to move the cursor), the
  panel follows the app theme and sits on top, with **⌘G** / **F3** for next, **⇧F3** for previous and **Esc** to close.

## [0.1.0] - 2026-10-29

First public release (pre-release): macOS only, unsigned build.

### Added
- **Clusters**: import from kubeconfig files or pasted YAML (credentials in the macOS Keychain), rename/colour/icon,
  pin and reorder, live health status, hotbar and Home screen.
- **Explorer**: live, virtualized tables for every resource kind (including CRDs) using the Table API + watch;
  namespace multi-select, search, sort, wide columns, keyboard navigation; detail panel with YAML editing,
  scale/restart/delete.
- **Operations**: pod shells and a cluster-scoped local terminal, multi-pod log viewer, port forwarding with
  auto-reconnect, manifest apply with server-side dry-run diff.
- **Insights**: plain-language health with hints, event timeline, CPU/memory columns and charts (metrics-server),
  ingress map, ConfigMap/Secret viewer.
- **Assessments**: 30 read-only checks, 0–100 score, history, trend chart, run comparison, HTML/JSON export.
- **Integrations**: Argo CD, Flux, Helm (inspect; rollback/uninstall via local `helm`), Prometheus (history charts, PromQL explorer).
- **Plugins**: out-of-process backends (JSON-RPC), sandboxed UI panels, permission model, custom assessment rules.
- **App**: three themes, ⌘K command palette, settings, optional daily update check, login-shell environment import
  (cloud auth plugins work when launched from Finder), universal DMG packaging.

- **Applications**: applications recognised from Helm releases and the `part-of` / `instance` / `app` labels (optionally by
  namespace), with health, version, URLs, issues and runbooks; a Helm **history** with a values/manifest diff between any two
  revisions and rollback with a preview; user-defined link templates and object-declared links. Works with limited access.
- **Limited-access mode**: for credentials that can only read some namespaces, the app detects it (access reviews) and works
  namespace by namespace: resource tables, Overview, Topology, Ingress map, Simulator, Assessments, Blackbox and the plugin
  API. Namespaces come from your choice (*Access…*), discovery, or the kubeconfig context. Forbidden namespaces are skipped, a
  *Limited access* banner explains the Overview figures, and the namespace picker works without the right to list namespaces.
- **Cluster Overview**: the default page when a cluster opens. Node, pod, workload and namespace health, CPU/memory requests
  and usage against capacity, the latest assessment with its trend, a "needs attention" list with runbooks and the warning
  events of the last hour.
- **Port forwarding 2.0**: the local listener stays open and the tunnel rebinds to a ready replica when the pod crashes, is
  evicted, terminates or is rolled (proactive failover with draining; new connections queue instead of being refused).
  Forwards can target Pods (following their Deployment), Services and workloads. Live status (Active / Healing /
  Degraded), repair count and last repair reason; one-click *Forward* on rows with an automatic local port.
- **Debug containers**: inject an ephemeral container (netshoot, busybox, alpine, ubuntu or custom image) into a running
  pod from the detail panel and open a shell in it; re-attach to existing ones.
- **Certificates view**: inventory of all TLS Secrets with expiry, SANs, issuer detection, cert-manager status and
  consumers; certificate summary in the Secret Data tab; new `certificate-expiry` assessment check .
- **Cluster Doctor 2.0**: 36 checks (liveness probes, probe misconfiguration, run-as-non-root, read-only root FS,
  seccomp, production-aware severities); **Fix…** proposals with server-side dry-run diff, risk notes, GitOps-manager
  warnings and apply; **Cost tab** with monthly estimate, per-namespace/workload breakdown and right-sizing savings
  (Prometheus p95 preferred); configurable rates, headroom and production namespaces.

- **Topology map**: live Ingress → Service → Workload → Pod → Node graph (plus HPA, ConfigMap, Secret, PVC) with health
  colouring, upstream/downstream highlighting, "Problems only" and search; refreshes every 5 s.
- **Simulator**: *Network policy* (can A reach B on port P? egress + ingress evaluation, names the allowing or isolating
  policy, Service port translation, named ports, ipBlock) and *Node drain* (pod fate and destination, PodDisruptionBudget
  blocking, spare capacity, HPA headroom after the drain). Read-only.
- **Compare clusters & selective promote**: compare Deployments, StatefulSets, DaemonSets, CronJobs, ConfigMaps,
  Services, Ingresses, HPAs, PDBs, NetworkPolicies and ServiceAccounts between two clusters or namespaces at field level
  (lists matched by name, server-assigned noise ignored); select individual fields (environment-specific ones flagged)
  and promote them to the target through a minimal server-side-apply manifest with a target dry-run and explicit
  confirmation; create objects missing in the target. Secrets are never read or copied.
- **Blackbox (time travel)**: opt-in per-cluster flight recorder that stores only health changes and notable events
  (bounded retention and record cap, automatic pruning), a density timeline with slider and playback, and reconstruction of
  what was failing at any recorded moment.
- **Runbooks**: built-in guides for crash loops, image pulls, OOM kills, Pending pods, NotReady nodes, unavailable workloads,
  evictions and volume mounts; read-only checks run in place, steps that change the cluster ask for confirmation; custom
  runbooks in YAML.

- **Back / forward** navigation: ◀ ▶ buttons in the title bar, ⌘[ / ⌘] and the mouse's back/forward buttons.
- **Probes** in the pod's Overview: liveness, readiness and startup probes per container with their endpoints and timings,
  *Check* (runs the probe through a temporary port-forward and shows the verdict, status, time and body), *Open* (forward and
  open in the browser) and *Copy* (a `curl`/`nc` command).

### Fixed
- **Ingress URLs could not be clicked**: the Ingress side panel (Overview tab) had no URLs or addresses at all (they were
  only on the Ingress Map); it now has a *Routing* section with clickable URLs, addresses, Service and pod IPs and TLS.
  Also, the system silently refused to open addresses containing characters such as `( ) * | $`, so nginx regex paths
  (`/api(/|$)(.*)`) and wildcard hosts did nothing: paths are reduced to their literal part, wildcard hosts are not
  links, and an address that cannot be opened now says so.
- **Port forwarding and terminals did not work on clusters or proxies that refuse WebSocket streaming** (Lens and kubectl
  did). The fallback to the classic SPDY protocol checked the wrong error type, so it never triggered. Fixed for port
  forwards, the cluster terminal and debug containers, with end-to-end tests against a fake kubelet speaking the real
  port-forward protocol (browser-like traffic, keep-alive, concurrent connections).
- **A finished assessment did not appear in the history and chart until the app was restarted**: every successful run
  was reported to the UI as "cancelled" (the cancellation flag was read after the run's context had been cancelled).
- Forwarding a pod from the detail panel did not open the browser; it now does for web ports (see the user guide).
- **Compare clusters**: namespaces loaded slowly and never loaded for a cluster that was not open. The comparison now
  connects to either cluster on demand, lists namespaces with a single fast call, and shows loading and error states.
- **Ingress Map crashed the app** when a Service's EndpointSlice had no endpoints (the backend sent `null` for the pod list).
  The same class of bug was fixed for port lists, the forward list, Flux lists, runbook matching and plugin rule results,
  the UI now normalises arrays at the API edge, and a view that fails to render shows an error with *Try again* instead of
  blanking the whole window.
- An internal error in the assessment (including the cost estimate) can no longer close the app: it is shown as a failed
  run, and a failing rule is reported as skipped.

### Changed
- **Sidebar**: Overview and Applications sit on top and are never folded away; Tools, Integrations (and Plugins) and Resources
  are collapsible sections whose state is remembered, with a dot on a folded section that contains the current page. Kinds are
  ordered Cluster, Workloads, Network, Config, Storage, Access Control, Custom Resources.
- **Project health**: dependencies updated to fix six reachable vulnerabilities (`golang.org/x/net`, Go toolchain 1.27.3);
  `staticcheck` findings fixed (dead code, a loop and a goroutine result simplified); CI now also runs `staticcheck`,
  `govulncheck`, `npm audit`, CodeQL, OpenSSF Scorecard, a dependency/license review and a DCO check, with actions pinned to
  commit SHAs; releases carry a CycloneDX SBOM and a build provenance attestation. Added GOVERNANCE, MAINTAINERS,
  SUPPORT, ROADMAP and ADOPTERS, a stricter security policy and a release guard against unfinished placeholders.
- **New logo** everywhere: app icon (macOS and Windows), the in-app mark, the README and a GitHub social preview. All assets are
  generated from one source image with `scripts/gen_branding.py` (see `assets/branding/README.md`).
- Ingresses: the **Hosts** and **Address** columns of the table are clickable links; in the Routing details the addresses
  now come last (URLs, backends, TLS, then addresses).
- **Ingress Map**: hosts, paths and icons open in the browser; clicking an ingress expands a details panel with its addresses
  (IP / hostname, copy and open), URLs, Service IPs, pod IPs and TLS.
- **Connect / disconnect**: clusters can be connected and disconnected (Home card button, hotbar right-click). The hotbar
  shows pinned and connected clusters, and the state is a coloured ring around the icon (green connected, red closed or
  unreachable, amber checking) with a bar marking the open cluster. The blackbox recorder respects a disconnect.
- Home has a **search box** (name, context, status) and a **Cards / List** layout switch that is remembered.
- The hotbar shows pinned clusters (and the open one); **Home lists all clusters** and has a pin button on each card.
  Status dots sit under the icons (green reachable, red not). *Compare clusters* is on the hotbar and in *Tools*.
- Sidebar: clearer hierarchy (section labels, category headers with icons and counts, indented kinds) and **all CRDs
  under one "Custom Resources" header**, grouped by API group.
- Clicking a namespace in an all-namespaces table filters to that namespace.
- App icon follows the macOS icon grid (artwork inset with margin), so it no longer looks oversized in the Dock.

[Unreleased]: https://github.com/solitonhq/soliton-rudder/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/solitonhq/soliton-rudder/releases/tag/v0.1.0

