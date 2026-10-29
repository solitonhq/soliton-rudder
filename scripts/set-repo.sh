#!/usr/bin/env bash
# Points the project at its GitHub repository:  scripts/set-repo.sh your-user/soliton-rudder
# Replaces the OWNER/soliton-rudder placeholder (and the @OWNER maintainer handle) in docs and sources, and records it in .github-repo
# (used by scripts/build-dmg.sh so local builds link to the right place).
set -euo pipefail
cd "$(dirname "$0")/.."
REPO="${1:-}"
[[ "$REPO" =~ ^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+$ ]] || { echo "usage: $0 owner/name" >&2; exit 1; }
OLD="OWNER/soliton-rudder"
files=$(grep -rl --exclude-dir=node_modules --exclude-dir=.git --exclude-dir=dist --exclude-dir=bin --exclude=set-repo.sh "$OLD" . || true)
for f in $files; do
  sed -i.bak "s#${OLD}#${REPO}#g" "$f" && rm -f "$f.bak"
done
# the maintainer handle placeholder (@OWNER in MAINTAINERS.md and CODEOWNERS)
owner="${REPO%%/*}"
for f in MAINTAINERS.md .github/CODEOWNERS; do
  [ -f "$f" ] && sed -i.bak "s#@OWNER#@${owner}#g" "$f" && rm -f "$f.bak"
done
echo "$REPO" > .github-repo
echo "Updated $(echo "$files" | grep -c . || true) file(s). Repository is now: $REPO"
