# Troubleshooting and FAQ

- [The app won't open ("cannot be opened…")](#the-app-wont-open)
- [A cluster shows Unreachable / Unauthorized](#a-cluster-shows-unreachable-or-unauthorized)
- [executable aws or gcloud not found](#executable-aws-or-gcloud-not-found)
- [Keychain keeps asking for my password](#keychain-keeps-asking-for-my-password)
- [CPU / Memory are missing](#cpu--memory-are-missing)
- [A list is empty or says forbidden](#a-list-is-empty-or-says-forbidden)
- [Shell / exec doesn't work in a pod](#shell--exec-doesnt-work-in-a-pod)
- [Port forward won't start](#port-forward-wont-start)
- [Logs are empty](#logs-are-empty)
- [Argo CD / Flux entries are missing](#argo-cd--flux-entries-are-missing)
- [Helm rollback is disabled](#helm-rollback-is-disabled)
- [A plugin is "crashed"](#a-plugin-is-crashed)
- [The window looks wrong / blank](#the-window-looks-wrong--blank)
- [Resetting everything](#resetting-everything)
- [Getting help and reporting bugs](#getting-help-and-reporting-bugs)
- [FAQ](#faq)

## The app won't open

macOS says *"cannot be opened because Apple cannot check it for malicious software"*: the build isn't notarized.
Follow [Opening an unsigned build](INSTALL.md#opening-an-unsigned-build) (System Settings → Privacy & Security →
**Open Anyway**).

*"is damaged and can't be opened"* usually means the download was interrupted or modified: re-download and check
the SHA-256 (`shasum -a 256 -c …`). If the checksum matches, run
`xattr -dr com.apple.quarantine "/Applications/Soliton Rudder.app"`.

## A cluster shows Unreachable or Unauthorized

The Home card shows the exact error. Try the same cluster with `kubectl --context <ctx> get ns` in Terminal.

| Message | Likely cause |
|---|---|
| `dial tcp … i/o timeout` / `no route to host` | VPN not connected, or the API server isn't reachable from your network. |
| `x509: certificate signed by unknown authority` | The kubeconfig's CA is missing/wrong, or a corporate proxy re-signs TLS. |
| `Unauthorized` | Expired token or SSO session. Log in again with your cloud CLI (`aws sso login`, `gcloud auth login`, …). |
| `Forbidden` | Authenticated, but the user lacks that permission. [Permissions table](USER-GUIDE.md#permissions-your-cluster-user-needs). |
| `executable … not found` | See next section. |

Status is re-checked every 30 s; the refresh button on Home forces it.

## executable aws or gcloud not found

Your kubeconfig authenticates through a helper program. Apps started from Finder don't automatically see your
shell's `PATH`; Soliton Rudder imports it from your **login shell** at startup, which normally fixes this. If it
doesn't:

1. Make sure the tool works in a new Terminal window: `which aws`.
2. Make sure `PATH` is set in a file your **login** shell reads (`~/.zprofile` or `~/.zshrc` for zsh,
   `~/.bash_profile` for bash). Slow or interactive prompts in those files (e.g. a `read`) can time out the import
   (limit 6 s).
3. As a workaround, quit the app and start it from Terminal: `open -a "Soliton Rudder"` keeps the Finder
   environment, but running `"/Applications/Soliton Rudder.app/Contents/MacOS/soliton-rudder"` directly inherits
   Terminal's.
4. Or use an absolute path in the kubeconfig `exec.command` (`/opt/homebrew/bin/aws`).

## Keychain keeps asking for my password

You chose *Allow* instead of **Always Allow**, or the app's code signature changed after an update (each
ad-hoc-signed build has a different identity). Choose **Always Allow** again. Signed releases keep a stable
identity. Kubeconfigs added **from a file** never touch the Keychain.

## CPU / Memory are missing

They require **metrics-server** in the cluster (`kubectl top nodes` must work). Pods/Nodes tables hide the columns
and charts say *"metrics-server is not available"* when it's absent. Managed clusters often need it enabled
explicitly (e.g. as an EKS add-on).

## A list is empty or says forbidden

- *forbidden*: your user can't `list` that kind (in that namespace). Pick a namespace you can access, or ask for a role.
- *Empty but you expect items*: check the **namespace picker** (it may be set to one namespace), the **search
  box**, and that the right context is selected.
- *"All namespaces"* needs cluster-wide `list`. With namespace-scoped RBAC, select your namespaces explicitly.

## Shell / exec doesn't work in a pod

- You need `create` on `pods/exec`.
- Images without `sh`/`bash` (distroless, scratch) can't run a shell; use an ephemeral debug container
  (`kubectl debug`) from the cluster terminal.
- Some clusters block the WebSocket upgrade through proxies; the app falls back to SPDY automatically. If it still
  fails, the error appears inside the terminal.

## Port forward won't start

- *cannot listen on 127.0.0.1:N / address already in use*: another program owns that port. The forward keeps retrying and
  starts as soon as it is free; or pick another port / leave it blank (automatic).
- *Degraded* with *waiting for a ready replica*: no pod behind the target is Ready yet. The forward keeps its local
  port and connects when one appears.
- *no ready pods behind service*: the Service's pods aren't Ready.
- *does not expose port N*: use a port the Service declares.
- Ports below 1024 are not allowed.
- Needs `create` on `pods/portforward`.

## Logs are empty

- The container may not have logged yet (try **Previous** after a restart) or logs were rotated.
- Several containers: open **sources** and make sure the right container is ticked. Init containers aren't included.
- For *terminated* pods the log may be gone once the node garbage-collects it.

## Argo CD / Flux entries are missing

They appear only when their CRDs are present. Reconnect the cluster (select another cluster and back) after
installing them, or reload via ⌘K → switch cluster. If the CRDs exist but you see an error, your user may lack `list` on them.

## Helm rollback is disabled

Rollback/uninstall run the `helm` CLI. Install it (`brew install helm`) and reopen the release. Viewing releases
never needs it.

## A plugin is "crashed"

*Settings → Plugins* → the plugin's **Logs** show its stderr. After three crashes in a minute the app stops
restarting it. Fix the cause (missing `node`/`python3`, a bug), then press **Restart**. A plugin whose program isn't
installed reports *program "node" not found on PATH*.

## The window looks wrong / blank

Quit and reopen. If it persists, *Settings → General → Reset preferences*. A blank window after an upgrade can be
a stale WebView cache: quit the app and remove `~/Library/WebKit/com.solitonrudder.app` and
`~/Library/Caches/com.solitonrudder.app`.

## Resetting everything

Quit the app, then remove the data folder (this forgets clusters, history, forwards and plugins) and optionally the
Keychain items: see [Uninstalling completely](INSTALL.md#uninstalling-completely).

## Getting help and reporting bugs

1. Search the [issues](https://github.com/solitonhq/soliton-rudder/issues).
2. Open a new issue using the **Bug report** template. Include the version (*Settings → About*), macOS version,
   what you did, what you expected, and any error text. **Remove secrets, tokens and private hostnames.**
3. For security problems do **not** open a public issue; see [SECURITY.md](../SECURITY.md).

## FAQ

**Does it modify my kubeconfig?** No. It reads it; pasted credentials go to the Keychain.

**Can it manage several clusters at once?** Yes: each has its own live connection; switch from the hotbar.

**Does it need kubectl?** No. The *cluster terminal* is just your shell with a kubeconfig set, so `kubectl` is on
you for that feature.

**Does it phone home?** No telemetry. See [Security and privacy](SECURITY-AND-PRIVACY.md#network-activity).

**Is it affected by a read-only user?** Read features work; write actions show the API's *forbidden* error.

**What about Windows/Linux?** Not yet; macOS only for now.

**How is it different from Lens / k9s / Headlamp?** It is a native-feeling desktop app focused on speed and
clarity: live tables, plain-language health, an ingress map, cluster assessments with history, and built-in Argo
CD / Flux / Helm / Prometheus views, with a sandboxed plugin system.
