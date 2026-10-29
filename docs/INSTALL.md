# Installing Soliton Rudder

This guide covers installing the app on macOS, opening it for the first time, what the permission prompts mean,
updating, uninstalling, and building from source.

- [Requirements](#requirements)
- [Install from the DMG](#install-from-the-dmg)
- [Opening an unsigned build](#opening-an-unsigned-build)
- [First launch: prompts you may see](#first-launch-prompts-you-may-see)
- [Cloud clusters (EKS, GKE, AKS…)](#cloud-clusters-eks-gke-aks)
- [Optional tools](#optional-tools)
- [Where your data lives](#where-your-data-lives)
- [Updating](#updating)
- [Uninstalling completely](#uninstalling-completely)
- [Build from source](#build-from-source)
- [Linux and Windows](#linux-and-windows)

## Requirements

| | |
|---|---|
| **macOS** | 12 Monterey or newer |
| **Mac** | Apple Silicon or Intel (the DMG is a universal binary) |
| **Disk** | ~100 MB for the app, plus a few MB for your data |
| **A kubeconfig** | Any file `kubectl` understands. You do **not** need `kubectl` installed. |
| **Cluster access** | Soliton Rudder uses *your* credentials: whatever your kubeconfig can do, the app can do. |

## Install from the DMG

1. Download `SolitonRudder-<version>-universal.dmg` from the
   [Releases page](https://github.com/solitonhq/soliton-rudder/releases).
2. *(Recommended)* verify the download. Each release has a `.sha256` file next to the DMG:

   ```bash
   cd ~/Downloads
   shasum -a 256 -c SolitonRudder-0.1.0-universal.dmg.sha256
   # SolitonRudder-0.1.0-universal.dmg: OK
   ```
3. Double-click the DMG. A window opens showing **Soliton Rudder** and an **Applications** shortcut.
4. Drag **Soliton Rudder** onto **Applications**.
5. Eject the disk image (drag it to the Trash or press ⌘E in Finder).
6. Open **Soliton Rudder** from Launchpad, Spotlight (⌘ Space) or the Applications folder.

> **Tip:** keep it in the Dock: right-click its Dock icon → *Options* → *Keep in Dock*.

## Opening an unsigned build

Releases that are **signed with an Apple Developer ID and notarized** open with a normal double-click and need
none of this section. If the release notes say the build is *ad-hoc signed* (or you built it yourself), macOS
Gatekeeper blocks the first launch with a message like *"Soliton Rudder cannot be opened because Apple cannot check
it for malicious software."* This is expected for software that is not notarized; it does not mean the file is
damaged. Choose **one** of these:

**Option A: System Settings (works on every macOS version, required on macOS 15 Sequoia and later)**

1. Try to open the app once (it is blocked).
2. Open **System Settings → Privacy & Security**.
3. Scroll to **Security**. You will see *"Soliton Rudder" was blocked…*. Click **Open Anyway** and confirm with
   your password / Touch ID.
4. Open the app again and click **Open**.

**Option B: Terminal**

```bash
xattr -dr com.apple.quarantine "/Applications/Soliton Rudder.app"
```

This removes the "downloaded from the internet" flag. Only do this for a build whose checksum you verified or
that you built yourself.

**Option C: right-click → Open** works on macOS 12–14: Control-click the app, choose **Open**, then **Open** again.

> The app is open source. If you prefer not to trust a prebuilt binary, [build it from source](#build-from-source).

## First launch: prompts you may see

| Prompt | Why it appears | What to do |
|---|---|---|
| **"Soliton Rudder wants to use your confidential information stored in 'login' in your keychain"** | Credentials of kubeconfigs you *paste* into the app are stored in the macOS Keychain (service `soliton-rudder`). The prompt appears when the app reads them back. | Enter your login password and choose **Always Allow** so it asks only once. Kubeconfigs added *from a file* are not copied; only the file path is stored. |
| **"Soliton Rudder would like to find and connect to devices on your local network"** (macOS 15+) | Your clusters' API servers or forwarded ports may be on your LAN/VPN. | Click **Allow**. If you clicked *Don't Allow*, re-enable it in *System Settings → Privacy & Security → Local Network*. |
| **A dialog from `aws`, `gcloud`, `kubelogin`, … or your browser opening** | Your kubeconfig uses an *exec* credential plugin (see below). | Complete the login as you would in Terminal. |

The app asks for **no** Full Disk Access, Accessibility, Screen Recording or Contacts permissions.

## Cloud clusters (EKS, GKE, AKS…)

Most managed clusters authenticate through a helper program referenced in your kubeconfig
(`aws eks get-token`, `gke-gcloud-auth-plugin`, `kubelogin`, …). Install and log in to those tools exactly as you
would for `kubectl`.

Apps started from Finder normally don't see the `PATH` and environment variables from your shell profile, which
would break those helpers. Soliton Rudder therefore **asks your login shell for its environment once at startup**
(`$SHELL -ilc env`) and imports `PATH` plus cloud/proxy variables (`AWS_*`, `AZURE_*`, `GOOGLE_*`, `CLOUDSDK_*`,
`KUBE*`, `HTTP(S)_PROXY`, `NO_PROXY`, `SSL_CERT_*`, …). Nothing else is imported, and plugin backends never receive it.

If a cluster works in Terminal but shows *"executable aws not found"* here, see
[Troubleshooting](TROUBLESHOOTING.md#executable-aws-or-gcloud-not-found). To opt out of the import set
`SOLITON_NO_SHELL_ENV=1` in the environment of the app.

## Optional tools

| Tool | Needed for |
|---|---|
| `helm` (`brew install helm`) | **Rollback / Uninstall** of Helm releases. Browsing releases, values and manifests does not need it. |
| `node` (`brew install node`) | Running the example plugin and any plugin whose backend is written in Node. |
| `metrics-server` *(in the cluster)* | CPU/Memory columns and usage charts. |
| Prometheus *(in the cluster)* | Long-range charts and the PromQL explorer. |

## Where your data lives

| What | Location |
|---|---|
| Cluster list, assessment history, port forwards, integration settings | `~/Library/Application Support/soliton-rudder/soliton-rudder.db` |
| Preferences (theme, update check) | `~/Library/Application Support/soliton-rudder/settings.json` |
| Installed plugins and their data | `~/Library/Application Support/soliton-rudder/plugins/` |
| Credentials of *pasted* kubeconfigs | macOS Keychain → login → items named `soliton-rudder` |
| Your own kubeconfig files | Wherever they are (e.g. `~/.kube/config`). **Never modified or copied.** |

*Settings → General → Show in Finder* opens the data folder.

## Updating

- **Check:** *Settings → General → Check now* (or the optional daily check) tells you when a newer release exists and
  links to its notes. Updates are **never installed automatically**.
- **Install:** download the new DMG and drag the app onto **Applications**, replacing the old one (choose
  **Replace**). Your data folder and Keychain items are untouched, so all clusters, history and plugins carry over.
- Quit the app before replacing it.

## Uninstalling completely

1. Quit Soliton Rudder.
2. Drag **Soliton Rudder** from Applications to the Trash.
3. *(Optional)* remove its data:

   ```bash
   rm -rf ~/Library/Application\ Support/soliton-rudder
   ```
4. *(Optional)* remove stored kubeconfig credentials: open **Keychain Access**, search for `soliton-rudder`,
   select the items and delete them. Or from Terminal, repeat until it reports nothing is found:

   ```bash
   security delete-generic-password -s soliton-rudder
   ```

Your kubeconfig files and clusters are not affected by any of this.

## Build from source

You need:

| Tool | Version |
|---|---|
| Xcode Command Line Tools | `xcode-select --install` |
| Go | the version in [`go.mod`](../go.mod) (1.26 or newer) |
| Node.js | 20.19+ (22 recommended) |
| Wails CLI | v2.17: `go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0` (make sure `$(go env GOPATH)/bin` is on your `PATH`) |

```bash
git clone https://github.com/solitonhq/soliton-rudder.git
cd soliton-rudder
wails doctor              # should end with "Your system is ready for Wails development!"
make build                # → build/bin/Soliton Rudder.app
open "build/bin/Soliton Rudder.app"
```

A locally built app is signed ad-hoc by Wails, so on *your own* Mac it opens without Gatekeeper prompts.

To produce a distributable DMG (universal binary, version stamped from the `VERSION` file):

```bash
make dmg                  # → dist/SolitonRudder-<version>-universal.dmg  (+ .sha256)
ARCH=arm64 make dmg       # Apple Silicon only, faster
```

Signing and notarizing for distribution are covered in [RELEASING.md](RELEASING.md). For day-to-day development
(`make dev` with hot reload) see [DEVELOPMENT.md](DEVELOPMENT.md).

## Linux and Windows

Only macOS is supported today. The stack (Wails + Go + React) is portable and the code avoids macOS-only
dependencies where possible, so Linux support is planned, but there are no Linux or Windows builds yet.
