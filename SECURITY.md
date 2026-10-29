# Security policy

## Supported versions

Soliton Rudder is pre-1.0; security fixes are provided for the **latest release** only. Update to the latest release to
receive them; the app tells you when one is available.

## Reporting a vulnerability

**Please do not open a public issue for security problems.**

Report privately using GitHub's
[*Report a vulnerability*](https://github.com/solitonhq/soliton-rudder/security/advisories/new) form (Security tab →
Advisories). Include:

- the affected version (*Settings → About*) and macOS version,
- a description and the impact (what could an attacker do?),
- steps or a proof of concept,
- any suggested fix.

## What to expect

- **Acknowledgement** within **5 working days**, and a first assessment (accepted, needs more information, or not a
  vulnerability) within **10 working days**.
- **A fix or mitigation** for confirmed high-severity issues within **30 days** where that is technically possible; others
  are scheduled by severity. We keep you informed of progress.
- **Coordinated disclosure**: we publish an advisory (and a CVE when appropriate) together with the fixed release. Please
  keep the details private until then; we ask for at most **90 days** from your report, and will say so if a fix needs longer.
- **Credit** in the advisory and release notes unless you prefer to stay anonymous.
- **Good faith**: research that follows this policy (no access to other people's data, no service disruption, no public
  disclosure before a fix) will not be met with legal action.

## Scope

In scope: the desktop application, its plugin sandbox and permission model, handling of credentials/kubeconfigs,
the release process (DMG integrity), and the update check.

Particularly interesting:

- plugin sandbox escapes (iframe → host, permission bypass, path traversal in plugin install/asset serving),
- leaking credentials or Secret values (logs, files, UI, network),
- command or argument injection (helm CLI wrapper, terminals, plugin backends' environment),
- unintended network destinations.

Out of scope: vulnerabilities in Kubernetes or third-party tools themselves, issues requiring a malicious *local*
user with your account, and a plugin backend doing things with **your own user privileges** (documented behaviour:
only install plugins you trust).

See [docs/SECURITY-AND-PRIVACY.md](docs/SECURITY-AND-PRIVACY.md) for the design.

## Verifying a download

Every release publishes, next to the DMG:

- a **SHA-256** checksum file: `shasum -a 256 -c SolitonRudder-<version>-universal.dmg.sha256`;
- a **build provenance attestation** that ties the DMG to the exact commit and workflow that built it:
  `gh attestation verify SolitonRudder-<version>-universal.dmg --repo solitonhq/soliton-rudder`;
- a **software bill of materials** (`SolitonRudder-sbom.cdx.json`, CycloneDX) listing every component.

Dependencies are scanned continuously (Dependabot, `govulncheck`, `npm audit`, CodeQL and dependency review in CI).
