#!/usr/bin/env bash
#
# Build the Wails desktop app and place the privileged helper binary next to
# the app's own executable.
#
# Env:
#   VERSION        version string injected via -ldflags (default: git describe)
#   SKIP_FRONTEND  set to 1 to reuse an existing frontend/dist
#
# Extra arguments are forwarded to `wails build` (e.g. -platform darwin/universal).
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

PRIV_NAME="singboxui-priv"

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
GIT_COMMIT="${GIT_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
BUILD_DATE="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${GIT_COMMIT} -X main.buildDate=${BUILD_DATE}"

# The bundle metadata (CFBundleShortVersionString) comes from wails.json, while
# -ldflags only reaches the Go binary: packaging with a stale productVersion
# ships an app whose own manifest contradicts its tag. Enforced for release
# versions only, so a `git describe` string on a working tree still builds.
if [[ "$VERSION" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  want="${VERSION#v}"
  have="$(sed -n 's/.*"productVersion"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' wails.json | head -1)"
  if [ "$have" != "$want" ]; then
    echo "wails.json info.productVersion is '$have' but this build is version '$VERSION'." >&2
    echo "Bump info.productVersion in wails.json before packaging the release." >&2
    exit 1
  fi
fi

# macOS deployment target. The Go toolchain on this machine ships objects built
# for macOS 13.0, while Wails appends "-mmacosx-version-min=10.13" to the CGO
# flags — the SDK clamps that to 11.0, so the linker warns that an object "was
# built for newer macOS version" and the bundle advertises a minimum it does not
# really meet. Stating the flag here stops Wails from adding its own.
MACOSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-13.0}"
export MACOSX_DEPLOYMENT_TARGET
if [ "$(go env GOOS)" = "darwin" ]; then
  case "${CGO_CFLAGS:-}" in
    *mmacosx-version-min*) ;;
    *) export CGO_CFLAGS="-mmacosx-version-min=${MACOSX_DEPLOYMENT_TARGET} ${CGO_CFLAGS:-}" ;;
  esac
  case "${CGO_LDFLAGS:-}" in
    *mmacosx-version-min*) ;;
    *) export CGO_LDFLAGS="-mmacosx-version-min=${MACOSX_DEPLOYMENT_TARGET} ${CGO_LDFLAGS:-}" ;;
  esac
fi

if [ "${SKIP_FRONTEND:-0}" != "1" ]; then
  echo "==> frontend: npm ci && npm run build"
  ( cd frontend && npm ci --no-audit --no-fund && npm run build )
fi

if ! command -v wails >/dev/null 2>&1; then
  echo "wails CLI not found — install with:" >&2
  echo "  go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0" >&2
  exit 1
fi

echo "==> wails build (version ${VERSION})"
wails build -ldflags "$LDFLAGS" "$@"

# The privileged helper only exists once cmd/singboxui-priv has been written.
if [ ! -d cmd/singboxui-priv ]; then
  echo "!! cmd/singboxui-priv not found; skipping privileged helper" >&2
  exit 0
fi

host_os="$(go env GOOS)"
host_arch="$(go env GOARCH)"
exe=""
[ "$host_os" = "windows" ] && exe=".exe"

# A universal .app must carry a universal helper: an arm64-only helper would
# refuse to launch on an Intel Mac even though the app itself runs there.
universal=0
for arg in "$@"; do
  case "$arg" in
    -platform=darwin/universal) universal=1 ;;
    darwin/universal) universal=1 ;;
  esac
done

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

if [ "$host_os" = "darwin" ] && [ "$universal" = 1 ]; then
  echo "==> building ${PRIV_NAME} (darwin/universal)"
  for arch in amd64 arm64; do
    GOOS=darwin GOARCH="$arch" go build -trimpath -ldflags "$LDFLAGS" \
      -o "${tmp}/${PRIV_NAME}-${arch}" ./cmd/singboxui-priv
  done
  lipo -create -output "${tmp}/${PRIV_NAME}" \
    "${tmp}/${PRIV_NAME}-amd64" "${tmp}/${PRIV_NAME}-arm64"
  rm -f "${tmp}/${PRIV_NAME}-amd64" "${tmp}/${PRIV_NAME}-arm64"
else
  echo "==> building ${PRIV_NAME} (${host_os}/${host_arch})"
  GOOS="$host_os" GOARCH="$host_arch" go build -trimpath -ldflags "$LDFLAGS" \
    -o "${tmp}/${PRIV_NAME}${exe}" ./cmd/singboxui-priv
fi

if [ "$host_os" = "darwin" ]; then
  app="$(find build/bin -maxdepth 1 -name '*.app' -print -quit 2>/dev/null || true)"
  if [ -n "$app" ] && [ -d "$app/Contents/MacOS" ]; then
    cp "${tmp}/${PRIV_NAME}" "$app/Contents/MacOS/${PRIV_NAME}"
    chmod +x "$app/Contents/MacOS/${PRIV_NAME}"
    echo "==> helper installed at $app/Contents/MacOS/${PRIV_NAME}"
  else
    mkdir -p build/bin
    cp "${tmp}/${PRIV_NAME}" "build/bin/${PRIV_NAME}"
    echo "==> helper installed at build/bin/${PRIV_NAME}"
  fi
else
  mkdir -p build/bin
  cp "${tmp}/${PRIV_NAME}${exe}" "build/bin/${PRIV_NAME}${exe}"
  echo "==> helper installed at build/bin/${PRIV_NAME}${exe}"
fi

echo "==> done (version ${VERSION})"
