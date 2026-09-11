#!/usr/bin/env bash
#
# macOS release packaging: universal .app -> optional codesign + notarize -> .dmg
#
# Signing and notarization are opt-in and driven only by environment variables
# (rewrite spec §74/§75). With none set this produces an unsigned local/dev
# build; nothing here ever stores or hardcodes a credential.
#
#   APPLE_CERTIFICATE            base64-encoded Developer ID .p12
#   APPLE_CERTIFICATE_PASSWORD   password for that .p12
#   APPLE_ID                     Apple ID used for notarization
#   APPLE_PASSWORD               app-specific password for that Apple ID
#   APPLE_TEAM_ID                10-character team identifier
#   KEYCHAIN_PASSWORD            password for the throwaway keychain (optional)
#   SIGN_IDENTITY                codesign identity (default: auto-detected)
#   VERSION                      release version (default: git describe)
#   DIST                         output directory (default: <root>/dist)
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

APP_NAME="singboxui"
BUNDLE_SUFFIX="macos-universal"
VERSION="${VERSION:-$(git describe --tags --always 2>/dev/null || echo dev)}"
DIST="${DIST:-$root/dist}"
ENTITLEMENTS="$root/packaging/macos/entitlements.plist"
KEYCHAIN="${KEYCHAIN:-${RUNNER_TEMP:-/tmp}/singboxui-signing.keychain-db}"
KEYCHAIN_PASSWORD="${KEYCHAIN_PASSWORD:-$(openssl rand -hex 16)}"

mkdir -p "$DIST"

echo "==> building macOS universal app (version $VERSION)"
VERSION="$VERSION" SKIP_FRONTEND="${SKIP_FRONTEND:-0}" \
  bash scripts/build.sh -platform darwin/universal

APP_PATH="$(find build/bin -maxdepth 1 -name "${APP_NAME}.app" -print -quit)"
[ -n "$APP_PATH" ] || APP_PATH="$(find build/bin -maxdepth 1 -name '*.app' -print -quit)"
[ -n "$APP_PATH" ] || { echo "no .app bundle produced under build/bin" >&2; exit 1; }

SIGNED=0
IDENTITY="${SIGN_IDENTITY:-}"

if [ -n "${APPLE_CERTIFICATE:-}" ]; then
  echo "==> importing signing certificate into a temporary keychain"
  cert_path="$(mktemp -t singboxui-cert).p12"
  printf '%s' "$APPLE_CERTIFICATE" | base64 --decode > "$cert_path"

  security create-keychain -p "$KEYCHAIN_PASSWORD" "$KEYCHAIN"
  security set-keychain-settings -lut 21600 "$KEYCHAIN"
  security unlock-keychain -p "$KEYCHAIN_PASSWORD" "$KEYCHAIN"
  security import "$cert_path" -k "$KEYCHAIN" \
    -P "${APPLE_CERTIFICATE_PASSWORD:-}" -T /usr/bin/codesign
  security set-key-partition-list -S apple-tool:,apple: -s \
    -k "$KEYCHAIN_PASSWORD" "$KEYCHAIN" >/dev/null
  rm -f "$cert_path"

  if [ -z "$IDENTITY" ]; then
    IDENTITY="$(security find-identity -v -p codesigning "$KEYCHAIN" \
      | awk -F'"' '/Developer ID Application/ {print $2; exit}')"
  fi
  [ -n "$IDENTITY" ] || { echo "no Developer ID Application identity found" >&2; exit 1; }

  echo "==> codesigning '$IDENTITY'"
  # Nested code is signed explicitly, innermost first: `--deep` is not a
  # supported way to prepare a bundle for distribution, and the helper is a
  # Mach-O executable inside Contents/MacOS that Gatekeeper checks separately.
  if [ -f "$APP_PATH/Contents/MacOS/singboxui-priv" ]; then
    codesign --force --options runtime --timestamp \
      --entitlements "$ENTITLEMENTS" --sign "$IDENTITY" \
      "$APP_PATH/Contents/MacOS/singboxui-priv"
  fi
  codesign --force --options runtime --timestamp \
    --entitlements "$ENTITLEMENTS" --sign "$IDENTITY" "$APP_PATH"
  codesign --verify --strict --verbose=2 "$APP_PATH"
  SIGNED=1
else
  echo "!! APPLE_CERTIFICATE not set — producing an UNSIGNED build (local/dev only)"
fi

if [ "$SIGNED" = "1" ] \
  && [ -n "${APPLE_ID:-}" ] && [ -n "${APPLE_PASSWORD:-}" ] && [ -n "${APPLE_TEAM_ID:-}" ]; then
  echo "==> notarizing (may take several minutes)"
  zip_path="$DIST/${APP_NAME}-${VERSION}.zip"
  ditto -c -k --keepParent "$APP_PATH" "$zip_path"
  xcrun notarytool submit "$zip_path" \
    --apple-id "$APPLE_ID" --password "$APPLE_PASSWORD" --team-id "$APPLE_TEAM_ID" \
    --wait
  xcrun stapler staple "$APP_PATH"
  rm -f "$zip_path"
elif [ "$SIGNED" = "1" ]; then
  echo "!! notarization secrets incomplete — shipping a signed but unnotarized build"
fi

DMG="$DIST/${APP_NAME}-${VERSION}-${BUNDLE_SUFFIX}.dmg"
echo "==> creating $DMG"
rm -f "$DMG"

# The image carries the app plus the usual "drag me onto Applications" symlink.
# Without it the mounted window shows the .app and nothing else, so a first-time
# user has no hint that the app must be copied out — and Finder names the copy
# "SingBoxUI 2.app" whenever an older build already sits in /Applications.
stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT
ditto "$APP_PATH" "$stage/$(basename "$APP_PATH")"
ln -s /Applications "$stage/Applications"

hdiutil create -volname "$(basename "$APP_PATH" .app)" -srcfolder "$stage" \
  -ov -format UDZO "$DMG" >/dev/null

if [ "$SIGNED" = "1" ] && [ -n "$IDENTITY" ]; then
  codesign --force --sign "$IDENTITY" "$DMG"
fi

echo "==> artifact: $DMG"
