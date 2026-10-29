# Releasing

How to cut a release, from a version bump to a notarized DMG on GitHub. Maintainers only.

## Overview

1. Update `VERSION` and `CHANGELOG.md`, commit, and push a tag `vX.Y.Z`.
2. `.github/workflows/release.yml` builds a **universal** `.app`, signs it, creates the DMG, optionally notarizes
   it, and publishes a GitHub Release with the DMG, its SHA-256, a **CycloneDX SBOM** and a signed **build provenance
   attestation** (see *Verifying a download* in [SECURITY.md](../SECURITY.md)).
3. Users get notified by the in-app update check (it reads the *latest* non-prerelease, non-draft release).

## One-time setup

### Before the first publish: the repository checklist

1. Run `scripts/set-repo.sh your-user/soliton-rudder` and fill in every `TODO(maintainer)` (maintainer name, conduct contact).
   `make check-placeholders` lists what is left; **the release workflow refuses to run while placeholders remain.**
2. *Settings → General*: upload `assets/branding/social-preview.png` as the **social preview**; enable **Discussions**.
3. *Settings → Code security*: enable **private vulnerability reporting**, **Dependabot alerts and security updates**,
   **secret scanning** and **push protection**. CodeQL and Scorecard workflows are already in `.github/workflows/`.
4. *Settings → Branches*: protect `main`: require a pull request (and one code-owner review once the project has more
   than one maintainer), require the checks **Go tests (ubuntu-latest)**, **Go tests (macos-latest)**, **Frontend**,
   **Wails build (macOS)**, **Signed-off-by**, **Analyze (go)** and **Analyze (javascript-typescript)**, require linear
   history, and disallow force pushes and deletions.
5. *Settings → Actions*: allow actions from GitHub and verified creators only, and set the default workflow token to
   read-only. Workflows pin third-party actions to commit SHAs; Dependabot proposes updates.
6. Add the topics `kubernetes`, `desktop`, `macos`, `wails`, `devops`, and a one-line description.

### Point the project at your GitHub repo

```bash
scripts/set-repo.sh your-user/soliton-rudder   # replaces the solitonhq/soliton-rudder placeholder everywhere
```

CI passes `REPO=${{ github.repository }}` at build time, so the shipped app links to the right place regardless.

### Optional: Apple Developer ID signing and notarization

Without these, releases are **ad-hoc signed**: they work, but users must follow
[Opening an unsigned build](INSTALL.md#opening-an-unsigned-build). A paid
[Apple Developer Program](https://developer.apple.com/programs/) membership (USD 99/year) removes that friction.

1. In *Certificates, Identifiers & Profiles* create a **Developer ID Application** certificate and install it in
   your Keychain. Export it as `.p12` with a password.
2. Create an **app-specific password** for your Apple ID (appleid.apple.com → Sign-In and Security).
3. Add these **GitHub repository secrets** (*Settings → Secrets and variables → Actions*):

   | Secret | Value |
   |---|---|
   | `MACOS_CERTIFICATE` | `base64 -i DeveloperID.p12 \| pbcopy` |
   | `MACOS_CERTIFICATE_PASSWORD` | the `.p12` password |
   | `MACOS_SIGN_IDENTITY` | e.g. `Developer ID Application: Your Name (TEAMID)` |
   | `NOTARY_APPLE_ID` | your Apple ID email |
   | `NOTARY_TEAM_ID` | your 10-character team id |
   | `NOTARY_PASSWORD` | the app-specific password |

The workflow signs and notarizes automatically when the secrets exist, and falls back to ad-hoc otherwise.

## Cutting a release

```bash
# 1. choose the version
echo 0.2.0 > VERSION
# 2. update CHANGELOG.md (move "Unreleased" entries under the new version and date)
# 3. refresh generated files
make docs notices
# 4. verify locally
make lint test
make dmg                 # optional: smoke test the DMG on your machine
# 5. commit, tag, push
git add -A && git commit -m "release: v0.2.0"
git tag v0.2.0 && git push origin main v0.2.0
```

`VERSION` must equal the tag without the leading `v`, **including any suffix** (tag `v0.2.0-rc1` ⇒ `VERSION` contains
`0.2.0-rc1`); the workflow refuses to build otherwise.

Watch the **Release** workflow. When it finishes, edit the generated release notes if needed. Pre-releases
(tags like `v0.2.0-rc1`) are marked as such automatically and are ignored by the in-app update check.

## Building locally

```bash
make dmg                                              # universal, ad-hoc signed
ARCH=arm64 make dmg                                   # faster, Apple Silicon only
SIGN_IDENTITY="Developer ID Application: Name (TEAM)" make dmg           # signed
SIGN_IDENTITY="…" NOTARY_PROFILE=rudder-notary make dmg                  # signed + notarized + stapled
```

Create the notarytool profile once:

```bash
xcrun notarytool store-credentials rudder-notary \
  --apple-id you@example.com --team-id TEAMID --password <app-specific-password>
```

What `scripts/build-dmg.sh` does: `wails build` (universal, `-trimpath`, version/repo injected with `-ldflags`) →
stamps `Info.plist` → bundles `LICENSE`, `NOTICE`, `THIRD_PARTY_NOTICES.txt` → `codesign` (hardened runtime +
timestamp when using a Developer ID) → DMG (with a drag-to-Applications layout; uses `create-dmg` if installed) →
verify → notarize + staple → SHA-256.

## Verifying a release

```bash
codesign --verify --deep --strict --verbose=2 "/Applications/Soliton Rudder.app"
spctl --assess --type execute --verbose "/Applications/Soliton Rudder.app"   # "accepted, source=Notarized Developer ID"
lipo -archs "/Applications/Soliton Rudder.app/Contents/MacOS/soliton-rudder"  # x86_64 arm64
```

## Versioning

[Semantic Versioning](https://semver.org). While `0.x`, minor versions may include breaking changes (documented in
the changelog). The plugin API has its own `apiVersion` (currently 1); changing it incompatibly requires a major
bump of that number and a migration note in [PLUGINS.md](PLUGINS.md).

## Checklist

- [ ] `VERSION` and `CHANGELOG.md` updated
- [ ] `make docs notices` produce no diff
- [ ] `make lint test` green
- [ ] Smoke test on a real cluster: add, browse, logs, shell, apply, assessments
- [ ] Tag pushed; Release workflow green; DMG downloads, opens, and shows the right version in *Settings → About*
