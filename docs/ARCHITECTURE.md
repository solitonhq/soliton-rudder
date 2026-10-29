# Architecture

A guide for contributors. For setup and workflow see [DEVELOPMENT.md](DEVELOPMENT.md).

## Overview

Soliton Rudder is a [Wails v2](https://wails.io) desktop app: a **Go** backend and a **React/TypeScript** frontend
rendered in the system WebView (WKWebView on macOS, no bundled browser).

```
┌─────────────── WebView (React 19, TypeScript, Tailwind v4) ────────────────┐
│  zustand stores · TanStack Virtual tables · xterm.js · CodeMirror 6        │
│          ▲  Wails bindings (window.go.main.App.*)      ▲ Wails events     │
└──────────┼─────────────────────────────────────────────┼───────────────────┘
           ▼                                             │
┌────────────────────────── Go (package main: App facade) ───────────────────┐
│ internal/cluster   registered clusters, kubeconfig import, health probe     │
│ internal/kube      sessions, live table streams, ops, ingress, health,      │
│                    metrics, Argo/Flux/Helm/Prometheus readers               │
│ internal/term      PTY + remote exec terminals        internal/portfwd      │
│ internal/feasibility  assessment rules, scoring, history, export            │
│ internal/plugins   manifest, JSON-RPC, supervisor, broker, UI asset server  │
│ internal/integrations · helmcli · secrets · settings · shellenv · updater   │
└──────────────────────────────┬──────────────────────────────────────────────┘
                               ▼ client-go (REST / watch / SPDY / WebSocket)
                        Kubernetes API servers
```

## Key design decisions

**1. Table API + watch instead of informers.** Lists are fetched with `Accept: application/json;as=Table` so the
API server returns the same printer columns as `kubectl get` for *every* kind, including CRDs, with no per-kind
code. A stream (`internal/kube/stream.go`) does *list → emit snapshot → watch from `resourceVersion`*, applies
events, batches them every 100 ms, and relists on `410 Gone`/errors with backoff. Only the open kind is watched
(lazy), and no object cache is kept, so memory stays flat on large clusters. Rows reach the UI as `rows` events;
the UI keeps them in a `Map` outside React state (`lib/rowstore.ts`) and notifies at most once per animation frame.

**2. Frontend row pipeline** (`lib/tablerows.ts`): filter → sort with a cached `Intl.Collator`, precomputed sort
keys and a per-row search-text cache. 20,000 rows sort in ~10 ms. Rendering is virtualized.

**3. One facade, many packages.** `app.go`/`app_*.go` form the only Wails-bound struct; each method is a thin call
into an `internal/*` package. Packages don't import `main`; they receive dependencies (emitters, session
providers) as function values or small interfaces, which keeps them unit-testable without a cluster or Wails.

**4. Sessions.** `kube.Manager` owns one `Session` per connected cluster (rest client, dynamic client, discovery,
sampler goroutines). Credentials come from `cluster.Service.RestConfig` (kubeconfig file reference or Keychain
copy). Background work is bound to the session context and stops on `Close`.

**5. Read-mostly, explicit writes.** Anything that changes a cluster goes through a handful of audited functions
(`ops.go`, `apply.go`, `argo.go`, `flux.go`, `helmcli`) and is confirmed in the UI.

**6. Pure assessment engine.** `internal/feasibility` first *collects* a snapshot (paged lists), then rules are
pure functions `Snapshot → Outcome`. This makes them fast and fully unit-testable, and a failed list marks
dependent rules *skipped* instead of falsely passing.

**7. Port-forward engine** (`kube/forward_engine.go`): a persistent local listener proxies each connection to the
current loopback *tunnel* (a client-go port-forward to one pod). A supervisor loop resolves a ready pod, swaps the
tunnel in, and replaces it when the tunnel dies or the pod becomes unhealthy, draining the old one. Resolution,
health and tunnel creation are injected, so the whole state machine is tested with fake pods over real TCP.

**8. Limited access is a first-class case** (`kube/access.go`). Credentials often cover only some namespaces, so a
session resolves its access once (access reviews, with a real request as fallback) and every list goes through
`ListScoped` / `ListRawSelector`, which query the readable namespaces in parallel and merge them, skipping forbidden
ones. Table streams become one stream per namespace feeding the same table, and a snapshot carries its namespace so one
relist never wipes the others. **Rule for new code: never call `client(r, "").List` for a namespaced kind; use the scoped
helpers**, and make new pages work from the namespaces in `Access`.

**9. Observation and the blackbox recorder** (`kube/observe.go`, `internal/blackbox`). A recorder polls cheap summaries
(pod, node and workload health plus warning events), diffs them against the last known state and stores only *changes* in
SQLite, with age and row-count pruning that keeps the latest state of every live object so any retained moment can be
reconstructed. A kind that could not be read is "unknown", never "deleted", so a flaky API server cannot invent history.

**10. Applications are recognised, not declared** (`kube/applications.go`). Grouping is a pure function over lists the
session already reads: Helm release, then `part-of`, `instance`, the legacy `app` label and an opt-in namespace fallback.
Every application records *how* it was grouped. Argo CD and Flux are meant to become further providers of the same shape.

**11. Runbooks** (`internal/runbook`) are data (YAML-compatible JSON), matched to an incident by kind, reason and status.
The backend only ever executes a fixed set of **read-only** checks (`RunCheck`); steps that change the cluster run
through the normal, confirmed operations in the UI.

**12. Plugins** are out-of-process (JSON-RPC over stdio) with UI in sandboxed iframes. Every host API call, from a
backend or an iframe, passes through one `Broker` that checks the plugin's grants. See [PLUGINS.md](PLUGINS.md).

## Repository layout

```
.
├─ main.go, app.go, app_*.go     Wails entry point and bound methods
├─ cmd/gendocs/                  generates docs/ASSESSMENT-RULES.md
├─ internal/
│  ├─ appinfo/                   version + repository (set via -ldflags)
│  ├─ cluster/                   cluster registry (SQLite), kubeconfig parsing, probes
│  ├─ kube/                      Kubernetes access: streams, ops, insights, integrations
│  ├─ term/ portfwd/ helmcli/    terminals, port forwards, helm CLI wrapper
│  ├─ feasibility/               assessments (rules, scoring, fix proposals, cost)
│  ├─ blackbox/                  flight recorder: change-only history with pruning
│  ├─ runbook/                   troubleshooting guides and their matching
│  ├─ plugins/                   plugin runtime
│  ├─ integrations/ secrets/ settings/ shellenv/ updater/
├─ frontend/src/
│  ├─ components/ dialogs/ views/   UI
│  ├─ store/                        zustand stores (explorer, clusters, dock, plugins, …)
│  ├─ lib/                          backend wrapper, row store/pipeline, formatting, charts helpers
│  └─ themes/                       CSS-variable themes (dark-black, dark-blue, light)
├─ examples/plugins/             a working example plugin
├─ scripts/                      DMG build, notices, branding, repo setup, release guards
├─ docs/                         documentation
└─ build/                        icons and macOS Info.plist template
```

## Frontend conventions

- **Theming:** components use semantic tokens (`bg-surface`, `text-muted`, `border-border`, …) mapped to CSS
  variables per theme (`themes/themes.css`). Never use raw colours in components.
- **State:** zustand stores; hot data (rows, logs, terminal output) lives *outside* React state in classes with a
  `version` + `useSyncExternalStore`.
- **Backend calls** go through `lib/backend.ts`, which also provides graceful fallbacks when running in a plain
  browser (`npm run dev`) and a single place to update when a bound method changes.
- **Accessibility:** dialogs trap focus and restore it; tables are keyboard-navigable; interactive icons have labels.

## Testing strategy

| Layer | How |
|---|---|
| Kubernetes behaviour | Go tests against `httptest` fake API servers (list/watch/table, discovery, patches, proxy) |
| Real protocols | A fake kubelet speaking the SPDY port-forward protocol carries real HTTP traffic through the tunnel code |
| Permissions | A fake API server that behaves like a namespace-restricted user (forbidden cluster-wide lists, access reviews) |
| Plugin runtime | A real child process (the test binary re-executed as a plugin) plus a shipped Node example |
| Assessment rules | Table-driven snapshots built from YAML; scoring invariants |
| PTY | A real PTY running `sh` |
| Frontend | Vitest + Testing Library for pure logic and accessibility behaviour |
| Docs | A test keeps `docs/ASSESSMENT-RULES.md` in sync with the rules |

There is intentionally no test that needs a live cluster, so CI stays hermetic. Beyond tests, CI runs `go vet`,
`staticcheck`, `govulncheck`, `npm audit`, CodeQL and a dependency/license review (see [RELEASING.md](RELEASING.md)).
