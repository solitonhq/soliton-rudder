# Roadmap

Where Soliton Rudder is going. This is a statement of intent, not a promise: priorities follow what users need, and the
best way to move something up is to open an issue or add a 👍 to an existing one.

## Shipped (0.1)

- **Clusters**: import from kubeconfig, credentials in the macOS Keychain, health, hotbar, connect/disconnect.
- **Live tables** for every kind (CRDs included), YAML editing, command palette, multi-namespace, back/forward.
- **Operations**: pod shells, logs, ephemeral debug containers, self-healing port forwards, manifest apply with dry-run diff.
- **Insight**: cluster overview, topology map, ingress map, certificates, probes, health explanations, metrics.
- **Assessments**: 36 checks with a score and history, YAML fix proposals with a server-side dry-run, cost (FinOps) estimates.
- **What-if tools**: NetworkPolicy reachability, node-drain / PodDisruptionBudget / autoscaler simulation.
- **Multi-cluster**: field-level drift between clusters or namespaces and selective, dry-run-first promotion.
- **Troubleshooting**: a blackbox recorder with time travel, and runbooks with read-only checks.
- **Applications**: grouped from Helm releases and labels, with health, versions, URLs, history diff and rollback preview.
- **Integrations**: Argo CD, Flux, Helm, Prometheus. **Plugins** with a permission model.
- **Limited access**: works namespace by namespace for credentials that cannot read the whole cluster.

## Next

- **Applications for Argo CD and Flux**: recognise their applications natively (members, sync and health, history).
- **Very large clusters**: one shared list-and-watch cache instead of repeated lists, adaptive refresh intervals, explicit
  limits with notes. Tables are already paged and virtualised; the pages that aggregate are what needs work.
- **Applications polish**: an Overview card and a hint when live objects drifted from their Helm release.
- **Release trust**: Developer ID signing and notarization for the DMG (the pipeline supports it once the secrets exist).

## Later (ideas, not commitments)

- **Linux and Windows builds.** The code is portable; what is macOS-only today is packaging and credential storage.
- **Saved views**, an **audit log** of changes made from the app, and a **production-protect** mode that adds friction to
  destructive actions on clusters you mark as production.
- A **plugin catalogue**, and more plugin extension points.

## Not planned

- Telemetry or analytics of any kind, accounts, or cloud sync: your cluster data stays on your machine.
- Bundling `kubectl`, `helm` or other cluster tools: the app talks to the API directly and uses a local `helm` only for
  rollback and uninstall.

See [CHANGELOG.md](CHANGELOG.md) for what changed in each release and [CONTRIBUTING.md](CONTRIBUTING.md) to help.
