# Security and privacy

What Soliton Rudder stores, what it sends over the network, and how its sandboxes work. To report a
vulnerability see [SECURITY.md](../SECURITY.md).

## The short version

- It acts as **you**, with the credentials in **your kubeconfig**. It cannot do anything your user can't.
- It has **no telemetry, no analytics, no account and no cloud service**.
- The only network destinations are: your clusters' API servers (and whatever your kubeconfig's auth plugins
  contact), GitHub's release API (once a day, *optional*), and URLs **you** enter (manifest-from-URL, plugin
  requests you permitted).
- Credentials you paste live in the **macOS Keychain**; kubeconfig *files* are referenced, not copied.

## What is stored, and where

| Data | Location | Notes |
|---|---|---|
| Cluster list (name, colour, source, context) | `~/Library/Application Support/soliton-rudder/soliton-rudder.db` (SQLite) | Contains **no credentials**. |
| Kubeconfig added **from a file** | stays where it is | Only its path + context name are stored. Never modified. |
| Kubeconfig **pasted** in | macOS Keychain, service `soliton-rudder`, key `kubeconfig:<cluster id>` | Reduced to the one selected context, certificates embedded. |
| Assessment reports | same SQLite DB (gzip-compressed JSON) | Resource *names and messages* from your cluster, **not** Secret values. Latest 100 per cluster. |
| Blackbox recordings (only if you turn recording on per cluster) | same SQLite DB | Health changes and Kubernetes events: object names, namespaces, reasons and event messages. **No Secret values, no logs, no object contents.** Bounded by your retention (default 24 h, max 7 d) and record limit (default 200 000 per cluster); *Delete recordings* removes them, and removing a cluster deletes its recordings. |
| Your runbooks | same SQLite DB | The YAML you wrote. |
| Port-forward definitions, Prometheus endpoint | same SQLite DB | |
| Preferences | `settings.json` | Theme, update check flag, last check time. |
| Plugins and their key/value data | `…/soliton-rudder/plugins/` | Per-plugin storage ≤ 2 MB. |
| Remembered UI choices (last kind, namespaces, dock height) | WebView local storage | *Settings → General → Reset preferences* clears them. |

Temporary files: a private (`0600`) single-context kubeconfig is created while a **cluster terminal** or a
**Helm rollback/uninstall** runs, and deleted afterwards.

The **Certificates** view and the certificate-expiry assessment list `kubernetes.io/tls` Secrets (selected server-side by
type). The response contains `tls.key`; the app **drops private keys immediately on receipt** and keeps only the public
certificate details. Debug containers add a container to the target pod using your credentials (`pods/ephemeralcontainers`)
and cannot be removed until the pod is recreated.

**Blackbox** is off until you enable it for a cluster. It only reads (list of pods, nodes, workloads and events) with
your credentials, and only while the app is open. **Runbooks** run read-only checks (events, health, manifest, pod logs;
never Secrets) on demand; any step that changes the cluster (restart, scale, delete) asks for a second confirmation.

**Cluster Doctor fixes** are only applied when you press *Apply fix*, after a server-side dry-run, with your own
credentials (create/patch/delete on the affected objects). Usage figures come from metrics-server or your Prometheus
through the API-server proxy; cost rates and production-namespace patterns are stored locally.

**Permission checks.** To adapt to limited rights the app sends *SelfSubjectAccessReview* requests ("may I list pods in this
namespace?") with your own credentials; they change nothing. The namespaces you choose in *Access…* are stored locally next to
the cluster's other settings.

**Compare clusters** reads the chosen kinds from both clusters with your credentials. **Secrets are excluded**: they are
never listed, compared or copied. Promotion writes to the *target* cluster only after a server-side dry-run you confirm,
using only the fields you selected (create/patch on the affected objects). Comparison results are held in memory only.

**Secrets from your cluster** are never written to disk by the app. In the Data tab, Secret values are fetched only
when you click reveal/copy and are held in memory only while shown.

## Network activity

| Destination | When | Why |
|---|---|---|
| Kubernetes API servers (via your kubeconfig, including its proxy settings) | While using a cluster | Everything the app does |
| `api.github.com` (`/repos/<owner>/<repo>/releases/latest`) | Once a day at most, only if enabled in *Settings → General* | Tells you a newer version exists. Sends no identifying data (GitHub sees your IP). Never downloads or installs anything. |
| URLs you type into *Apply → From URL* | On click | Fetches the manifest (http/https, ≤ 5 MB) |
| Prometheus (through the API server proxy) | When you use it | Queries |
| Anything a **plugin** requests | Only if you granted it `network` | See below |

There is no crash reporting.

## Environment import

When started from Finder, the app asks your login shell for its environment once (`$SHELL -ilc env`) so exec-based
credential plugins (`aws`, `gcloud`, …) work. Only `PATH` and an allow-list of cloud/proxy/locale variables are
imported (see [INSTALL.md](INSTALL.md#cloud-clusters-eks-gke-aks)); everything else in your environment is ignored.
Set `SOLITON_NO_SHELL_ENV=1` to disable. **Plugin backends never inherit this environment.**

## Plugins

| Layer | Isolation |
|---|---|
| **UI panels** | Sandboxed `<iframe sandbox="allow-scripts">`: opaque origin, no access to the app's DOM or storage, CSP `connect-src 'none'` (no network of its own). All data goes through the host API. |
| **Backends** | Separate processes with a *minimal* environment (no kubeconfig, tokens or cloud variables). **They run with your user privileges**: permissions restrict what a plugin can do *through Soliton Rudder*, not what the program can do on your Mac. Install backends only from sources you trust. |
| **Permissions** | Declared in the manifest, shown at install, individually revocable. Enforced by the host on every call. Reading Secrets and modifying the cluster are marked *high risk* and are off by default at install. |
| **Packaging** | Zip-slip and symlink protection, size limits; the plugin UI server serves only the plugin's declared UI folder. |

Details: [PLUGINS.md](PLUGINS.md).

## Hardening notes

- Release builds are built with `-trimpath` and signed (ad-hoc, or Developer ID + notarized when the maintainer
  provides the certificate; see [RELEASING.md](RELEASING.md)). Verify downloads with the published SHA-256.
- Destructive actions (delete, uninstall, rollback, prune) always require confirmation. Helm uninstall requires
  typing the release name.
- Use a read-only kubeconfig (or a read-only role) for clusters where you only need to look.
- The app does not run with the macOS App Sandbox because it must read your kubeconfig, spawn `kubectl`-style
  credential plugins and optional plugin backends.
