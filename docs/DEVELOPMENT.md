# Development

How to build, run, test and contribute code. Read [ARCHITECTURE.md](ARCHITECTURE.md) first for the big picture.

## Prerequisites

Same as [building from source](INSTALL.md#build-from-source): Xcode Command Line Tools, Go (see `go.mod`),
Node.js 20.19+, and the Wails CLI v2.17.

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0
export PATH="$PATH:$(go env GOPATH)/bin"
wails doctor
```

## Everyday commands

| Command | What it does |
|---|---|
| `make dev` | `wails dev`: Go + Vite hot reload in a native window |
| `make test` | Go tests (race detector) + frontend tests |
| `make lint` | `go vet`, `staticcheck`, `gofmt` check, ESLint, TypeScript |
| `make vuln` | `govulncheck` and `npm audit`: known vulnerabilities in dependencies (needs network) |
| `make build` | Release-style `.app` in `build/bin/` |
| `make dmg` | Universal DMG in `dist/` ([RELEASING.md](RELEASING.md)) |
| `make docs` | Regenerate generated docs (`docs/ASSESSMENT-RULES.md`) |
| `make notices` | Regenerate `THIRD_PARTY_NOTICES.txt` |
| `make branding` | Regenerate every logo asset from `assets/branding/logo-source.png` |
| `make check-placeholders` | Fail while `OWNER` / `TODO(maintainer)` placeholders remain (also runs in the release workflow) |
| `make clean` | Remove build output |

Frontend only (browser, no Go): `cd frontend && npm install && npm run dev`. Backend calls then return empty
fallbacks, which is enough for layout work.

## Trying it without a cluster

The hermetic tests cover most logic. To click through the UI you need a cluster. Easy options:
[kind](https://kind.sigs.k8s.io), [minikube](https://minikube.sigs.k8s.io) or Docker Desktop's Kubernetes.
Install `metrics-server` to see CPU/Memory, and Argo CD / Flux / kube-prometheus-stack to try the integrations.

## Adding things

**A new backend method.** Write the logic in `internal/<pkg>` with a unit test. Expose it on `App` (in `app.go` or
a new `app_<area>.go`), then regenerate bindings (`wails generate module`, done automatically by `wails dev/build`).
Add a typed wrapper in `frontend/src/lib/backend.ts` and use it from a store/component.

**A new assessment rule.** Add a `Rule` to `internal/feasibility/rules.go` (pure function over `*Snapshot`),
add its severity text to `docs.go`, add a test case, run `make docs`. If it needs a new resource list, add it to
`listSpecs` in `snapshot.go`.

**A new theme token.** Define it in all three themes in `frontend/src/themes/themes.css`, map it in `@theme inline`,
and (if plugins should receive it) add it to `THEME_VARS` in `components/PluginFrame.tsx`.

**A plugin capability.** Add the permission to `permissionCatalog` (`internal/plugins/manifest.go`), the method to
`Broker.Call`, a test in `plugins_test.go`, and document it in `docs/PLUGINS.md`.

## Code style

- **Go:** `gofmt`; wrap errors with context; no global state except where noted; accept `context.Context`;
  keep packages free of Wails imports (only `app*.go` imports Wails).
- **TypeScript:** strict mode; Prettier (`npm run format`); ESLint must pass with zero warnings; prefer pure
  helpers in `lib/` with tests; no raw colours.
- **Comments** explain *why*, not *what*.
- **Security:** never log credentials; sanitize anything rendered from cluster data (use text nodes, not HTML);
  validate user-supplied paths/URLs/names at the boundary.

## Commits and pull requests

Small, focused PRs with tests. Use clear commit messages (`area: what and why`). See
[CONTRIBUTING.md](../CONTRIBUTING.md) for the checklist.

## Continuous integration

`.github/workflows/ci.yml` runs on every push/PR: Go tests on macOS and Linux, frontend lint/tests/build, a docs
freshness check, and a full Wails build on macOS. `release.yml` builds and publishes the DMG for tags (`v*`).
