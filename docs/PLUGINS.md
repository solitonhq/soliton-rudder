# Writing Soliton Rudder plugins

A plugin can contribute **UI panels**, **command-palette commands** and **assessment rules**. Plugins are plain
folders (or `.zip` files); install them from *Settings → Plugins*. A working example lives in
[`examples/plugins/namespace-report`](../examples/plugins/namespace-report).

```
my-plugin/
├─ plugin.json      manifest (required)
├─ backend.js       optional backend process (any language)
└─ ui/              optional UI served in a sandboxed iframe
   └─ index.html
```

## Safety model

| Part | Isolation |
|---|---|
| **UI panel** | Runs in an `<iframe sandbox="allow-scripts">` with an opaque origin and a CSP of `connect-src 'none'`. It cannot reach the network, the app's DOM or storage. All data goes through the host API below. |
| **Backend** | A separate process, supervised and restarted on crash (3 crashes/minute ⇒ marked *crashed*). It starts with a minimal environment — no kubeconfig, tokens or cloud credentials. **It runs with your user privileges**, so permissions limit what it can do *through Soliton Rudder*, not what the process can do on your machine. Only install backends you trust. |
| **Permissions** | Declared in the manifest, reviewed and granted at install time, changeable later. Enforced by the host on every call, for backends and UIs alike. |

## plugin.json

```jsonc
{
  "id": "io.example.my-plugin",          // [a-z0-9._-], 3–63 chars; also the install folder name
  "name": "My Plugin",
  "version": "1.0.0",
  "apiVersion": 1,                       // must equal the host's plugin API version
  "description": "…", "author": "…",
  "permissions": ["clusters.read", "kube.read", "ui.notify"],

  "backend": { "command": "node", "args": ["backend.js"] },
  //  command: "./relative/script" inside the plugin, or a bare program name (node, python3) found on PATH

  "ui": {
    "root": "ui",                        // directory served to the iframe (default "ui")
    "panels": [
      { "id": "main", "title": "My Panel", "entry": "index.html", "placement": "cluster-nav" }
      //  placement: "cluster-nav" (sidebar entry under *Plugins*) or "dock" (bottom dock tab)
    ]
  },

  "commands": [
    { "id": "scan", "title": "My Plugin: scan" },                 // runs backend `command/run`
    { "id": "open", "title": "My Plugin: open", "panel": "main" } // opens a panel instead
  ],

  "assessmentRules": [
    { "id": "my-rule", "title": "…", "category": "security",      // resources|reliability|security|maintenance
      "severity": "medium", "description": "…", "remediation": "…" }
  ]
}
```

### Permissions

| Permission | Allows | Risk |
|---|---|---|
| `clusters.read` | `clusters.list` | low |
| `kube.read` | `kube.get`, `kube.list` (not Secrets) | medium |
| `secrets.read` | reading Secrets via `kube.get/list` (needs `kube.read` too) | high |
| `kube.write` | `kube.delete`, `kube.scale`, `kube.restart`, `kube.apply` | high |
| `network` | `http.fetch` | medium |
| `ui.notify` | `ui.notify` toasts | low |

`storage.*`, `ui.emit` and `log` need no permission. Storage is private to the plugin (max 500 keys, 256 KB per value, 2 MB total).

## Host API

The same methods are available to the backend (as JSON-RPC requests) and to the UI (`soliton.call(method, params)`).

| Method | Params | Result |
|---|---|---|
| `clusters.list` | – | `[{id, name}]` |
| `kube.list` | `{cluster, group?, version, resource, namespaced?, namespace?, limit?}` | `{items, truncated}` |
| `kube.get` | `{cluster, group?, version, resource, namespaced?, namespace?, name}` | object |
| `kube.delete` / `kube.restart` | as `kube.get` | `{ok}` |
| `kube.scale` | as `kube.get` + `replicas` | `{ok}` |
| `kube.apply` | `{cluster, manifest, namespace?, dryRun?, force?}` | per-document results (before/after YAML) |
| `storage.get/set/delete/list` | `{key, value?}` | JSON value / `{ok}` / keys |
| `ui.notify` | `{text, kind?: "success"\|"error"}` | `{ok}` |
| `ui.emit` | `{name, data}` – delivered to the plugin's own panels as an event | `{ok}` |
| `http.fetch` | `{url, method?, headers?, body?, timeoutMs?}` | `{status, headers, body, encoding}` |

`group` is `""`/`"core"` for the core API (`pods`, `namespaces`, …) and e.g. `"apps"` for deployments. Errors use JSON-RPC error objects; a missing permission returns code `-32001`.

## Backend protocol

JSON-RPC 2.0, **one JSON object per line** on stdin/stdout. Write nothing else to stdout (use stderr or the `log` notification; both appear in *Settings → Plugins → Logs*).

Host → plugin requests:

| Method | Params | Expected result |
|---|---|---|
| `initialize` | `{apiVersion, host, hostVersion, pluginId, dataDir}` | any (reply within 15 s) |
| `shutdown` | – (notification) | exit promptly |
| `command/run` | `{command, context: {clusterId, clusterName, namespaces}}` | `{message?}` (shown as a toast) |
| `assess/run` | `{ruleId, clusterId}` | `{checked, findings: [{severity, kind, namespace, name, resourceKey?, message}]}` |
| `ui/call` | `{method, params}` from `soliton.backend(method, params)` | any JSON |

Requests time out after 60 s. Each plugin may have 8 host calls in flight. Assessment findings are merged into the cluster score like built-in checks; if the backend fails, the rule is reported as *skipped* instead of passing.

## UI panels

Include the SDK and use `window.soliton`:

```html
<script src="/plugins/sdk.js"></script>
<script>
  soliton.onContext(async (ctx) => {          // ctx: {clusterId, clusterName, namespaces, theme}
    const pods = await soliton.kube.list({ cluster: ctx.clusterId, version: 'v1', resource: 'pods', namespaced: true });
    // render with textContent — never innerHTML with cluster data
  });
  soliton.notify('Hello');
  soliton.backend('summary', { clusterId: '…' });   // → backend `ui/call`
  soliton.on('myEvent', (data) => {});               // events from backend `ui.emit`
</script>
```

The SDK applies the app theme as CSS variables (`--bg --surface --surface-2 --surface-hover --border --fg --fg-muted --accent --accent-fg --success --warning --danger`), so panels follow dark-black / dark-blue / light automatically.

## Packaging & updating

Zip the plugin folder (top-level files or a single top-level directory). Installing a plugin with an existing `id` updates it in place and keeps its stored data. Symlinks are never installed; limits are 5000 files / 100 MB.
