#!/usr/bin/env bash
#
# Regenerate the Wails TypeScript bindings consumed by the frontend.
# Run after changing an exported binding/DTO in internal/desktop.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

if ! command -v wails >/dev/null 2>&1; then
  echo "wails CLI not found — install with:" >&2
  echo "  go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0" >&2
  exit 1
fi

echo "==> wails generate module"
wails generate module

target="$(sed -n 's/.*"wailsjsdir":[[:space:]]*"\([^"]*\)".*/\1/p' wails.json | head -n1)"
echo "==> bindings written to ${target:-frontend/wailsjs}"
echo "==> commit the regenerated bindings; never edit them by hand (docs/adr/001-wails-v2.md)"
