#!/usr/bin/env bash
# Builds Soliton Rudder.app and packages it into a DMG.
#
#   scripts/build-dmg.sh                      # universal (arm64 + x86_64), ad-hoc signed
#   ARCH=arm64 scripts/build-dmg.sh           # single architecture (faster)
#   SIGN_IDENTITY="Developer ID Application: Name (TEAMID)" scripts/build-dmg.sh
#   SIGN_IDENTITY=… NOTARY_PROFILE=rudder-notary scripts/build-dmg.sh   # also notarize + staple
#
# Environment:
#   VERSION          defaults to the VERSION file
#   REPO             GitHub "owner/name" baked into the build (update checks, links);
#                    defaults to $GITHUB_REPOSITORY, then the .github-repo file, then a placeholder
#   ARCH             universal | arm64 | amd64            (default: universal)
#   SIGN_IDENTITY    Developer ID Application identity    (default: ad-hoc "-")
#   NOTARY_PROFILE   notarytool keychain profile name     (default: skip notarization)
#
# Output: dist/SolitonRudder-<version>-<arch>.dmg and a .sha256 next to it.
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${VERSION:-$(tr -d '[:space:]' < VERSION)}"
REPO="${REPO:-${GITHUB_REPOSITORY:-$( [ -f .github-repo ] && tr -d '[:space:]' < .github-repo || echo 'solitonhq/soliton-rudder')}}"
ARCH="${ARCH:-universal}"
SIGN_IDENTITY="${SIGN_IDENTITY:--}"
APP_NAME="Soliton Rudder"
APP="build/bin/${APP_NAME}.app"
OUT_DIR="dist"
DMG="${OUT_DIR}/SolitonRudder-${VERSION}-${ARCH}.dmg"

say() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
need() { command -v "$1" >/dev/null 2>&1 || { echo "error: '$1' is required ($2)" >&2; exit 1; }; }

[ "$(uname)" = "Darwin" ] || { echo "error: DMG packaging only works on macOS" >&2; exit 1; }
need wails "install with: go install github.com/wailsapp/wails/v2/cmd/wails@latest"
need node "install Node.js 20+"
need hdiutil "ships with macOS"
need codesign "install Xcode command line tools: xcode-select --install"

case "$ARCH" in universal|arm64|amd64) ;; *) echo "error: ARCH must be universal, arm64 or amd64" >&2; exit 1;; esac

say "Building ${APP_NAME} ${VERSION} (${ARCH}) for ${REPO}"
export MACOSX_DEPLOYMENT_TARGET=12.0
wails build -clean -platform "darwin/${ARCH}" -trimpath \
  -ldflags "-s -w -X soliton-rudder/internal/appinfo.Version=${VERSION} -X soliton-rudder/internal/appinfo.Repo=${REPO}"
[ -d "$APP" ] || { echo "error: $APP was not produced" >&2; exit 1; }

say "Stamping version and bundling licenses"
PLIST="$APP/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString ${VERSION%%-*}" "$PLIST"
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion ${VERSION%%-*}" "$PLIST"
cp LICENSE "$APP/Contents/Resources/LICENSE"
[ -f NOTICE ] && cp NOTICE "$APP/Contents/Resources/NOTICE"
if [ -f THIRD_PARTY_NOTICES.txt ]; then cp THIRD_PARTY_NOTICES.txt "$APP/Contents/Resources/"; else echo "warning: THIRD_PARTY_NOTICES.txt missing (run make notices)"; fi

say "Code signing (${SIGN_IDENTITY/#-/ad-hoc})"
if [ "$SIGN_IDENTITY" = "-" ]; then
  codesign --force --deep --sign - "$APP"
else
  codesign --force --deep --options runtime --timestamp --sign "$SIGN_IDENTITY" "$APP"
fi
codesign --verify --deep --strict --verbose=2 "$APP"
echo "architectures: $(lipo -archs "$APP/Contents/MacOS/soliton-rudder")"

say "Creating ${DMG}"
mkdir -p "$OUT_DIR"
rm -f "$DMG"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
cp -R "$APP" "$STAGE/"
if command -v create-dmg >/dev/null 2>&1; then
  # nicer window layout when create-dmg (brew install create-dmg) is available
  create-dmg --volname "${APP_NAME} ${VERSION}" --window-size 560 360 --icon-size 96 \
    --icon "${APP_NAME}.app" 150 170 --app-drop-link 410 170 --hide-extension "${APP_NAME}.app" \
    --no-internet-enable "$DMG" "$STAGE" >/dev/null
else
  ln -s /Applications "$STAGE/Applications"
  hdiutil create -volname "${APP_NAME} ${VERSION}" -srcfolder "$STAGE" -fs HFS+ -format UDZO -imagekey zlib-level=9 -ov "$DMG" >/dev/null
fi
[ "$SIGN_IDENTITY" = "-" ] || codesign --force --timestamp --sign "$SIGN_IDENTITY" "$DMG"
hdiutil verify "$DMG" >/dev/null && echo "dmg verified"

if [ -n "${NOTARY_PROFILE:-}" ]; then
  [ "$SIGN_IDENTITY" != "-" ] || { echo "error: notarization needs a Developer ID SIGN_IDENTITY" >&2; exit 1; }
  say "Notarizing (this can take a few minutes)"
  xcrun notarytool submit "$DMG" --keychain-profile "$NOTARY_PROFILE" --wait
  xcrun stapler staple "$DMG"
  xcrun stapler validate "$DMG"
fi

( cd "$OUT_DIR" && shasum -a 256 "$(basename "$DMG")" > "$(basename "$DMG").sha256" )
say "Done"
ls -lh "$DMG"
cat "${DMG}.sha256"
if [ "$SIGN_IDENTITY" = "-" ]; then
  echo
  echo "note: ad-hoc signed and not notarized. macOS Gatekeeper will warn on other Macs;"
  echo "      see docs/INSTALL.md ('Opening an unsigned build') or sign + notarize for distribution."
fi
