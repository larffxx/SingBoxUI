#!/usr/bin/env bash
#
# Full local/CI gate (rewrite spec §71/§72) in one command:
#   go test ; go test -race ; go vet ; staticcheck   (first-party packages)
#   frontend lint -> typecheck -> test -> build
#
# The Go scope is explicit rather than `./...`: frontend/node_modules contains Go
# sources shipped by npm packages (flatted does), which are not ours to build.
#
# Run from anywhere; the script locates the repository root itself.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

step() { printf '\n==> %s\n' "$1"; }

pkgs=(. ./cmd/... ./internal/...)

step "go test"
go test "${pkgs[@]}"

step "go test -race"
go test -race "${pkgs[@]}"

step "go vet"
go vet "${pkgs[@]}"

step "staticcheck"
if command -v staticcheck >/dev/null 2>&1; then
  staticcheck "${pkgs[@]}"
else
  echo "!! staticcheck not installed; CI installs it. Locally: go install honnef.co/go/tools/cmd/staticcheck@latest" >&2
fi

step "gofmt check"
unformatted="$(gofmt -l . cmd internal || true)"
if [ -n "$unformatted" ]; then
  echo "gofmt required for:" >&2
  echo "$unformatted" >&2
  exit 1
fi

step "frontend: install"
cd frontend
npm ci --no-audit --no-fund

step "frontend: lint"
npm run lint

step "frontend: typecheck"
npm run typecheck

step "frontend: test"
npm test

step "frontend: build"
npm run build

printf '\nAll checks passed.\n'
