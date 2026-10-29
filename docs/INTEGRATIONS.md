# Integrations

Soliton Rudder works with the tools most teams already run in their clusters. Integrations appear under
**Integrations** in the cluster sidebar. **Argo CD** and **Flux** show up only when their CRDs are installed;
**Helm** and **Prometheus** are always available.

None of these integrations needs an extra login or server URL: everything goes through the Kubernetes API using
the same credentials as the rest of the app.

- [Argo CD](#argo-cd)
- [Flux](#flux)
- [Helm](#helm)
- [Prometheus](#prometheus)

## Argo CD

How it works: the app reads and patches Argo CD's `Application` resources directly (the same way the `argocd`
CLI and UI ultimately drive the controller), so you don't need access to the Argo CD API server.

**List**: name/project, **Sync** and **Health** status (with counts of out-of-sync/unhealthy resources), source
(repo, path/chart @ revision), destination, last operation result and age, and badges for *auto-sync* and errors.
Filters: *All / Out of sync / Unhealthy*, plus search. Refreshes every 5 seconds.

**Detail panel**

| Tab | Shows |
|---|---|
| Overview | Error/warning conditions, sources (multi-source apps supported), destination, revision, health message, auto-sync settings (with enable/disable), sync options, last operation and its per-resource results, images, link to the YAML |
| Resources | Every managed resource with sync and health; click to open it in the explorer |
| History | Deployment history (revision, age, source) with **Roll back** |

**Actions**

- **Refresh** / **Hard** refresh: ask the controller to re-compare with git (hard also drops caches).
- **Sync**: optional revision, **Prune**, **Force**, **Dry run**. Refused while another operation is running.
- **Roll back** to a history entry. With auto-sync enabled Argo CD would immediately undo the rollback, so the
  dialog requires you to disable auto-sync first (re-enable it after fixing git).
- Enable/disable **auto-sync** from the Overview tab.

**Limitations:** Argo CD's live diff is computed by its API server and is not available through the Kubernetes API;
you see *which* resources are out of sync, not a line diff. Terminating a running operation isn't supported.

**Permissions:** `get`, `list`, `patch` on `applications.argoproj.io`.

## Flux

**List** every Flux object in the cluster (Kustomization, HelmRelease, GitRepository, HelmRepository,
OCIRepository, Bucket, HelmChart, image automation, notification kinds, …) with a **kind filter**, readiness
(*Ready / Failing / Suspended*), the failure **reason**, revision, source, path, interval and time since last
transition. Refreshes every 5 seconds.

**Detail:** conditions with messages; for Kustomizations the **inventory** of applied objects (click to open).
**Actions:** **Reconcile** (sets `reconcile.fluxcd.io/requestedAt`, like `flux reconcile`) and **Suspend / Resume**
(`spec.suspend`).

**Permissions:** `list`, `get`, `patch` on `*.toolkit.fluxcd.io`.

## Helm

Releases are read straight from Helm's own storage (Secrets named `sh.helm.release.v1.<name>.v<N>`, or ConfigMaps
for the legacy driver), so **inspection needs no `helm` binary and no chart repository access**.

**List:** release, namespace, chart and version, app version, revision, status, last updated.

**Detail**

| Tab | Shows |
|---|---|
| Overview | Chart/app version, description, `NOTES.txt` |
| Values | *Your values* or *All values* (chart defaults merged with yours) |
| Manifest | The rendered manifest |
| Resources | Objects in the release (click to open when the kind is known) |
| History | Up to the 25 latest revisions with status and description |

**Rollback and Uninstall** need Helm's engine, so they run **your local `helm` CLI** with a private temporary
kubeconfig for the cluster (found on `PATH`, `/opt/homebrew/bin`, `/usr/local/bin`, …). Without `helm` the buttons
are disabled with an explanation. Uninstall requires typing the release name.

**Permissions:** `list` (metadata) and `get` on `secrets` labelled `owner=helm` (Helm releases contain your
values; treat access accordingly).

## Prometheus

**Connect:** *Integrations → Prometheus*. The app looks for Prometheus Services (kube-prometheus-stack,
prometheus-operator, plain charts) and offers them, preferring non-headless Services; you can also enter
namespace / service / port (number or name) / scheme / path prefix manually. The connection is tested before it
is saved. Access is through the **API server's service proxy**, so no port-forward or ingress is required and
nothing leaves your cluster network path.

**What you get**

- **History on every chart**: pod, workload and similar usage charts gain 1 h / 6 h / 24 h / 7 d ranges (summed
  over a workload's pods). Nodes keep the live metrics-server view.
- **Query explorer**: PromQL with 15 m – 7 d ranges, preset queries (cluster CPU, memory/CPU by namespace, pod
  restarts, pods not ready, API-server request rate), a multi-series chart with hover read-out, and a table of
  series with last/min/max. Click a series to show or hide it. **⌘ Enter** runs.

**Metrics used by the pod charts:** `container_cpu_usage_seconds_total` and `container_memory_working_set_bytes`
(cAdvisor, scraped by default in kube-prometheus-stack). If they aren't scraped you'll see *"returned no data"*.

**Limits:** up to 50 series and 1,000 points per series are returned; ranges 1 minute – 31 days. Authenticated
external Prometheus URLs are not supported, only in-cluster Services.

**Permissions:** `get` on `services/proxy` for the chosen Service (and `list` on Services for auto-detection).
Disconnect any time from the Prometheus page.
