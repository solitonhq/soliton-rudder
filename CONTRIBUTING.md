# Contributing to Soliton Rudder

Thanks for your interest! Contributions of all kinds are welcome: bug reports, documentation fixes, ideas, new
assessment checks, integrations and code.

By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md). Security problems go through
[SECURITY.md](SECURITY.md), not public issues. How the project is run is described in [GOVERNANCE.md](GOVERNANCE.md);
where to ask questions is in [SUPPORT.md](SUPPORT.md).

## Ways to help

- **Report a bug** using the *Bug report* template. Include your version (*Settings → About*), macOS version and
  the error text. Remove secrets and private hostnames.
- **Suggest a feature** with the *Feature request* template: describe the problem first.
- **Improve the docs**: they live in [`docs/`](docs/); typo fixes are very welcome.
- **Write a plugin**: see [docs/PLUGINS.md](docs/PLUGINS.md). Great for niche or company-specific needs.
- **Pick up an issue** labelled `good first issue` or `help wanted`.

## Development setup

See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md). In short:

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0
git clone https://github.com/solitonhq/soliton-rudder.git && cd soliton-rudder
make dev      # hot-reloading app
make lint test vuln
```

You can work without a cluster for most changes (tests use fake API servers). Use
[kind](https://kind.sigs.k8s.io) for manual testing; please **don't test destructive features against production**.

## Pull request checklist

1. Open (or find) an issue first for anything non-trivial so we can agree on the approach.
2. Keep the PR focused; separate refactors from behaviour changes.
3. Add or update tests. Bug fixes should include a test that fails without the fix.
4. Run `make lint test vuln`. CI runs the same plus a full macOS build, CodeQL and a dependency review.
5. **Sign off your commits** (`git commit -s`): see [Developer Certificate of Origin](#developer-certificate-of-origin).
6. Update docs (`docs/`, `CHANGELOG.md` under *Unreleased*). If you changed assessment rules, run `make docs`.
7. No secrets, tokens, real cluster names or customer data in code, tests, fixtures or screenshots.

## Project principles

- **Fast and calm**: live data, no spinners where avoidable, no layout jumps.
- **Safe by default**: read-only unless the user acts; destructive actions confirm; never log or persist secrets.
- **Honest**: show *skipped*/*unavailable* rather than pretending success.
- **Works on real clusters**: large, permission-restricted, slow. Degrade gracefully.
- **Accessible**: keyboard-friendly, labelled controls, respects reduced-motion.

## Developer Certificate of Origin

Contributions are accepted under the [Developer Certificate of Origin](https://developercertificate.org) (DCO): by adding a
`Signed-off-by` line you certify that you wrote the change or have the right to submit it under the project's license.
There is no CLA to sign. Add the line with:

```bash
git commit -s -m "kube: handle empty EndpointSlices"
# forgot? sign off all commits of your branch:
git rebase --signoff main && git push --force-with-lease
```

The `DCO` check on every pull request verifies it.

## Licensing of contributions

The project is licensed under the [Apache License 2.0](LICENSE). By submitting a contribution you agree that it is
licensed under the same terms (Section 5 of the license). Only submit code you wrote or have the right to
contribute, and keep third-party code's original license headers. If you add a dependency, make sure its license is
permissive (MIT, BSD, ISC, Apache-2.0) and run `make notices`.
