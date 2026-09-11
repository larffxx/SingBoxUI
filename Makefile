# SingBoxUI — developer entry points.
#
# `make help` lists every target. Nothing in this file stores credentials:
# the packaging targets only read optional environment variables and degrade
# to an unsigned local/dev build when they are absent.

SHELL := /bin/bash

APP_NAME     := singboxui
PRIV_NAME    := singboxui-priv
FRONTEND_DIR := frontend
BIN_DIR      := build/bin
DIST_DIR     := dist

GO    ?= go
NPM   ?= npm
WAILS ?= wails

# First-party packages only. `./...` also walks frontend/node_modules, where some
# npm packages ship Go sources (flatted does), so the pipeline would build, vet
# and test code this repository does not own -- and CI, which does not install
# node_modules for the Go job, would disagree with a developer machine.
GO_PKGS     := . ./cmd/... ./internal/...
GO_FMT_DIRS := . cmd internal

# The macOS deployment target, the CGO version flags Wails would otherwise
# override, and the helper injection all live in scripts/build.sh, which is the
# single build entry point for `make build`, the packaging scripts and CI.
export MACOSX_DEPLOYMENT_TARGET ?= 13.0

# Version metadata injected into the Go binary. -X targets that do not exist
# are ignored by the linker, so this is safe before the version symbols land.
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(GIT_COMMIT) \
	-X main.buildDate=$(BUILD_DATE)

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@echo "SingBoxUI targets:"
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

## Dependencies ---------------------------------------------------------------

.PHONY: deps
deps: ## Resolve Go module dependencies
	$(GO) mod download

.PHONY: frontend-install
frontend-install: ## Install frontend dependencies from the lockfile
	cd $(FRONTEND_DIR) && $(NPM) ci

.PHONY: tools
tools: ## Install build-time tools (wails, staticcheck)
	$(GO) install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0
	$(GO) install honnef.co/go/tools/cmd/staticcheck@latest

## Frontend -------------------------------------------------------------------

.PHONY: frontend-lint
frontend-lint: ## Lint the frontend (ESLint)
	cd $(FRONTEND_DIR) && $(NPM) run lint

.PHONY: frontend-typecheck
frontend-typecheck: ## Type-check the frontend (tsc, strict)
	cd $(FRONTEND_DIR) && $(NPM) run typecheck

.PHONY: frontend-test
frontend-test: ## Run frontend unit tests (Vitest)
	cd $(FRONTEND_DIR) && $(NPM) test

.PHONY: frontend-build
frontend-build: ## Production frontend build (Vite -> frontend/dist)
	cd $(FRONTEND_DIR) && $(NPM) run build

.PHONY: frontend-e2e
frontend-e2e: ## Run Playwright application-level flows
	cd $(FRONTEND_DIR) && $(NPM) run e2e

## Go -------------------------------------------------------------------------

.PHONY: vet
vet: ## Run go vet
	$(GO) vet $(GO_PKGS)

.PHONY: test
test: ## Run Go tests
	$(GO) test $(GO_PKGS)

.PHONY: test-race
test-race: ## Run Go tests with the race detector
	$(GO) test -race $(GO_PKGS)

.PHONY: staticcheck
staticcheck: ## Run staticcheck
	staticcheck $(GO_PKGS)

.PHONY: go-fmt
go-fmt: ## Report unformatted Go files (non-zero exit when dirty)
	@unformatted="$$(gofmt -l $(GO_FMT_DIRS))"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt required for:"; echo "$$unformatted"; exit 1; \
	fi

.PHONY: go-lint
go-lint: go-fmt vet staticcheck ## Format check, vet and staticcheck

.PHONY: fmt
fmt: ## Format Go and frontend sources
	$(GO) fmt $(GO_PKGS)
	cd $(FRONTEND_DIR) && $(NPM) run format

## Build / run ----------------------------------------------------------------

.PHONY: bindings
bindings: ## Regenerate Wails TypeScript bindings (needs a compiling app)
	$(WAILS) generate module

.PHONY: dev
dev: ## Run the app in Wails dev mode with hot reload
	$(WAILS) dev

.PHONY: build
build: ## Build the desktop app (wails build + privileged helper) and its bundle
	bash scripts/build.sh

.PHONY: build-priv
build-priv: ## Build the privileged helper binary
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" \
		-o $(BIN_DIR)/$(PRIV_NAME) ./cmd/singboxui-priv

## Verification ---------------------------------------------------------------

.PHONY: check
check: go-lint test test-race frontend-lint frontend-typecheck frontend-test frontend-build ## Full local gate (mirrors CI)
	@echo "All checks passed."

.PHONY: ci
ci: check ## Alias for check

## Packaging ------------------------------------------------------------------

.PHONY: package-macos
package-macos: ## Build a (optionally signed) macOS .app + .dmg
	bash scripts/package-macos.sh

.PHONY: package-windows
package-windows: ## Build a (optionally signed) Windows .exe + installer
	pwsh -NoProfile -File scripts/package-windows.ps1

## Housekeeping ---------------------------------------------------------------

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) $(DIST_DIR) $(FRONTEND_DIR)/dist

.PHONY: distclean
distclean: clean ## Also remove installed frontend dependencies
	rm -rf $(FRONTEND_DIR)/node_modules
