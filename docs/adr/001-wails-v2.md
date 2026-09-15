# ADR 001 — Use Wails v2 as the desktop shell

## Context

The legacy SingBoxUI is a Spring Boot application that serves a Thymeleaf page over an unauthenticated
HTTP server on `localhost:8000` and controls a privileged sing-box process from the same JVM. That shape
forces three problems: a Java runtime requirement, a browser as the primary UI, and a loopback
management API that any local process or web page can reach.

The rewrite must produce a native desktop application for Windows 10/11 and macOS (Intel + Apple
Silicon), with a modern React interface and a Go backend, no HTTP management surface, and no whole-app
elevation. Candidates: Electron, Tauri, Wails v2, Wails v3 beta, JavaFX, Spring Boot + embedded server.

## Decision

Use **Wails v2 stable** with a React/TypeScript/Vite frontend.

* Go is the backend language, so the sing-box supervisor, SQLite persistence and platform code live in
  one process and one language, with `os/exec` argument arrays instead of shell strings.
* Wails generates typed TypeScript bindings and a typed model file from Go structs, which removes the
  hand-maintained frontend protocol layer that the specification calls out (§32, §9).
* Wails provides both directions of communication: bound methods for commands (request/response) and
  runtime events for streams (runtime status, logs, traffic) — measured against the legacy 2–3 s polling.
* The frontend is bundled into the binary; nothing needs a Node or JVM runtime at the user's machine.
* Wails v2 is stable and self-contained (webview2 on Windows, WKWebView on macOS) and its CLI already
  produces `.app`/`.dmg` and Windows installers, which the packaging requirements need.

## Alternatives

* **Electron** — ships a Chromium runtime (~150 MB), needs a separate Node sidecar or native addon for
  process control, and its IPC surface is a network-less but stringly-typed channel. Rejected: runtime
  size, no typed bindings, extra process to supervise.
* **Tauri** — Rust backend. Rejected: the specification mandates Go, and mixing Rust for the shell with
  Go for the backend would double the toolchain and split process ownership.
* **Wails v3 beta** — richer API (multiple windows, services), but beta; the specification explicitly
  forbids it. Rejected for stability.
* **JavaFX / Spring Boot + embedded server** — keeps the Java runtime and (in the Spring case) the
  localhost control surface. Rejected by specification §9.
* **Native Go + a hand-written webview binding (e.g. webview/webview)** — minimal, but we would have to
  hand-roll binding generation, asset embedding and packaging, i.e. rebuild Wails. Rejected as waste.

## Consequences

* The Go module must be the repository root and `wails.json` drives the frontend build; the frontend
  lives in `frontend/` and its `dist/` output is embedded at build time.
* Bindings are generated into `frontend/wailsjs` and must stay in sync with the Go DTOs — this is a
  build step (`make bindings`), not a manually maintained schema. Generated code is never edited by hand
  and never duplicated in `shared/models`.
* Wails events are the only streaming transport, so the backend must batch high-frequency updates
  (log lines, traffic) to avoid flooding the webview event loop (ADR 007).
* The Wails shutdown hook is the single place that guarantees sing-box is stopped before exit (ADR 004).
* Cross-compilation: Wails needs a platform SDK per target, so Windows artifacts are built on Windows CI
  runners and macOS artifacts on macOS runners.
* Backend tests must be runnable without a webview: all logic lives in `internal/app/*`, and
  `internal/desktop/bindings` is a thin adapter that tests can call directly.

## Deviations from §6

§6 presents the layout as approximate (`Use approximately:`), and two points differ in this repository:

* **`main.go` sits at the repository root**, not in `cmd/singboxui/main.go`. The Wails v2 CLI resolves a
  project as *the directory containing `main.go` together with `wails.json`*; keeping `wails.json` at the
  root (as the frontend build requires) and moving `main.go` into a subdirectory makes `wails build`
  report `no Go files in <root>`. Keeping both at the root is the layout Wails v2 supports without
  flags. The privileged helper stays in its own binary, `cmd/singboxui-priv/`, because it must be a
  separate executable.
* **`frontend/src/{app,features,shared}`** instead of `{app,routes,entities,widgets,shared}`: TanStack
  Router owns routing in `src/app/router.tsx` and the screens live under `src/features/<feature>/`, which
  matches the feature-sliced grouping the specification asks for without an extra `routes/` layer that
  would duplicate it.
