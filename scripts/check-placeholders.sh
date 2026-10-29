#!/usr/bin/env bash
# Fails if the repository still contains template placeholders that must be filled in before anything is published:
#   OWNER                 -> run scripts/set-repo.sh owner/name
#   TODO(maintainer)      -> a decision only the maintainers can make (contact addresses, handles)
set -euo pipefail
cd "$(dirname "$0")/.."

# the placeholders are described, not used, in these files
skip=(':!scripts/check-placeholders.sh' ':!scripts/set-repo.sh' ':!docs/RELEASING.md' ':!Makefile' ':!docs/DEVELOPMENT.md' ':!CHANGELOG.md' ':!frontend/node_modules' ':!THIRD_PARTY_NOTICES.txt')

fail=0
if hits=$(git grep -nI -e 'TODO(maintainer)' -- . "${skip[@]}" 2>/dev/null); then
  echo "Unfinished maintainer decisions:"; echo "$hits"; fail=1
fi
if hits=$(git grep -nIE '(github\.com/|@)OWNER([^A-Za-z0-9_]|$)' -- . "${skip[@]}" 2>/dev/null); then
  echo "The repository owner is still a placeholder (run scripts/set-repo.sh owner/name):"; echo "$hits"; fail=1
fi
[ "$fail" = 0 ] && echo "no placeholders left"
exit "$fail"
