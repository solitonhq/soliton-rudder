# Governance

Soliton Rudder is an open source project under the [Apache License 2.0](LICENSE). This document says how decisions are made
so that contributors know what to expect.

## Roles

- **Users and contributors**: anyone who uses the project, reports an issue, improves the documentation or sends a pull
  request. No permission is needed to take part; see [CONTRIBUTING.md](CONTRIBUTING.md).
- **Maintainers**: people with write access who review and merge changes, triage issues, cut releases and look after the
  health of the project. They are listed in [MAINTAINERS.md](MAINTAINERS.md).

## How decisions are made

- **Day to day**: by **lazy consensus**. A change is merged when a maintainer approves it and nobody has raised a
  blocking concern. Anything non-trivial starts as an issue, so the approach is agreed before the work is done.
- **Significant changes** (architecture, a new dependency with a different license, anything that changes what data
  leaves the machine, changes to this document) are proposed in an issue, left open for at least **seven days**, and need
  the approval of a **majority of maintainers**. A single maintainer's blocking concern has to be answered, not outvoted.
- **Disagreement** is settled by discussion first, then by a vote of the maintainers. With a single maintainer, that
  maintainer decides and explains the reasoning in the issue.

## Principles that decisions are measured against

1. **Safe by default**: read-only unless the user acts, destructive actions confirm, secrets are never logged or stored.
2. **Private by default**: no telemetry, no accounts; data stays on the user's machine.
3. **Works on real clusters**: large, slow and permission-restricted ones; degrade gracefully and say what was skipped.
4. **No single tool assumed**: Helm, Argo CD, Flux or plain YAML are all first-class.

## Becoming a maintainer

Sustained, high-quality contributions over several months (code, reviews, triage, documentation) and good judgement about
the principles above. An existing maintainer nominates the candidate in an issue; the maintainers approve by consensus.

A maintainer who has been inactive for six months may be moved to *emeritus* status (listed, without write access); they
can return at any time by the same consensus.

## Releases and security

Maintainers cut releases as described in [docs/RELEASING.md](docs/RELEASING.md). Vulnerabilities are handled privately as
described in [SECURITY.md](SECURITY.md).

## Code of Conduct

Everyone taking part follows the [Code of Conduct](CODE_OF_CONDUCT.md).

## Changing this document

Through a pull request, following the process for significant changes above.
