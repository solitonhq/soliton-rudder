# Getting started

A ten-minute tour: add a cluster, look around, and use the features you'll reach for every day.
New to the app? Install it first: [INSTALL.md](INSTALL.md).

## 1. Add your first cluster

1. Open Soliton Rudder. The **Home** screen is empty and shows **Add cluster**.
2. Choose how to add it:
   - **From file** (default `~/.kube/config`): click **Scan**, tick the contexts you want, then **Add**.
     The app keeps a *reference* to the file, so edits you make to it later are picked up. Use **Browse…** for a
     file elsewhere (the file dialog shows hidden folders such as `.kube`).
   - **Paste kubeconfig**: paste the YAML, **Scan**, pick contexts. The credentials are stored in the macOS Keychain,
     not on disk.
3. Each added cluster appears in the **hotbar** (the narrow rail on the left) and as a card on Home. A coloured dot
   shows its connection status and is re-checked every 30 seconds.

> Contexts already added are greyed out and labelled *already added*, so rescanning never creates duplicates.

### Make a cluster recognisable

Right-click a cluster in the hotbar (or double-click its Home card) → **Edit…**: set a friendly **name**, a **colour**
and a 1–2 character **icon**, or **pin** it to the top. Drag clusters in the hotbar to reorder them. **Delete** only
removes the cluster *from Soliton Rudder*; your kubeconfig file is never modified.

## 2. Look around

Click a cluster. The left column lists resource kinds in groups (**Workloads, Network, Config, Storage, Access
Control, Cluster**, then CRDs by API group). Click **Pods**.

- The table is **live**: pods appear, change status and disappear in real time, with no refresh button.
- Use the **namespace picker** (multi-select) and the **search box** (`/` focuses it). Click a column header to sort.
- **Wide** shows extra columns (IP, node, …). **Pods** and **Nodes** also show CPU and Memory when metrics-server is
  installed.
- Click a row to open the **detail panel**:
  - **Overview**: a plain-language *Health* summary (with hints such as "container keeps crashing, check
    previous logs"), usage charts, labels, annotations and a merged event **timeline**.
  - **YAML**: edit and **Save**.
  - **Data** (ConfigMaps and Secrets): readable key/value view. Secrets stay masked until you click the eye icon.
- Use the arrow keys, PgUp/PgDn, Home and End to move through the table once it has focus.

## 3. Do things

Select a **pod** and use the buttons at the top of the detail panel:

| Button | What it does |
|---|---|
| **Shell** | Opens an interactive terminal *inside* the pod (bash if available, else sh) in the bottom dock. |
| **Logs** | Streams logs. Works for workloads too: pick pods/containers, search, filter by level, follow, download. |
| **Debug** | Adds a debug container (netshoot, busybox, …) to the running pod, even if the app image has no shell, and opens a terminal in it. |
| **Forward** | Forwards a local port to the pod (or Service). It heals itself: if the pod restarts or is replaced, the same local port keeps working. |

Hover any Pod, Service or workload row and click the plug icon for a **one-click forward** on a free local port.

Workloads (Deployments, StatefulSets, …) add **Scale** and **Restart** (rolling restart). The bin icon deletes,
after a confirmation.

In the top bar of a resource list:

| Button | What it does |
|---|---|
| **Terminal** | A local shell with `kubectl`/`helm` already pointed at *this* cluster (a private temporary kubeconfig). |
| **Apply** | Paste or load YAML, **Preview changes** (server-side dry-run with a diff), then apply. |

## 4. Find your way with ⌘K

Press **⌘K** anywhere. Type to jump to a resource kind, a namespace, another cluster, switch theme, open the
assessments, or run a plugin command. Arrow keys move, **Enter** runs, **Esc** closes.

## 5. Pick a theme

The three coloured dots at the bottom of the hotbar switch between **Dark Black**, **Dark Blue** and **Light**
instantly. *Settings* (gear icon) shows previews.

## Where next?

- [User guide](USER-GUIDE.md): every feature in detail, plus keyboard shortcuts.
- Cluster health report: **Assessments** in the left column, see [Assessments](USER-GUIDE.md#assessments).
- GitOps and monitoring: [Integrations](INTEGRATIONS.md) (Argo CD, Flux, Helm, Prometheus).
- Something wrong? [Troubleshooting](TROUBLESHOOTING.md).
