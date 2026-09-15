# ADR 007 — Wails events as the only streaming transport

## Context

The legacy UI polled the backend every 2–3 seconds for logs, traffic and runtime status, and kept
state in a mutable global JS object. The specification requires the React frontend to communicate
through Wails bindings/events, forbids polling logs/traffic, forbids a giant frontend store, and
demands bounded update frequency so the webview event loop is not flooded (§31, §46, §47, §58, §83).

## Decision

Wails events are the only push transport; bound methods are request/response only.

* `internal/events` declares the stable event names and the `Emitter` port; the application layer
  never imports the Wails runtime — the composition root supplies a Wails-backed `Emitter`.
* Events: `runtime:status` (every state change), `runtime:log` (batched, ≤ ~10/s), `traffic:snapshot`
  (≤ 1/s while RUNNING), `binary:progress`, `config:apply` (per apply stage), `app:notice`
  (persistent, user-visible failures such as a failed auto-connect), plus `profiles:changed`.
* Payloads are typed Go DTOs in `internal/desktop/dto`; Wails generates the TypeScript models, so the
  frontend never maintains a duplicate protocol schema (§32, §83).
* High-frequency producers batch before emitting (log lines are buffered into arrays; traffic is
  sampled at a fixed ceiling). Unbounded arrays and per-line events are forbidden.
* Frontend state ownership follows §58: **TanStack Query** caches bound-call results and invalidates
  them on events; **RHF + Zod** own forms; the router owns navigation; local state owns ephemeral UI.
  There is no Redux/Zustand global store.
* Subscriptions are registered on mount and unsubscribed on unmount; a component never leaves an
  event listener behind.

## Alternatives

* **Request/response polling of log/traffic methods (legacy)** — the measured 2–3 s latency the
  rewrite removes; also wasteful. Rejected (§46, §47).
* **A local WebSocket/HTTP channel for streaming** — recreates a network control surface the spec
  forbids; Wails IPC already provides events. Rejected (§9, §30).
* **Frontend-held shared mutable state (a global store) mirrored to the backend** — the anti-pattern
  the spec calls out; produces divergent sources of truth. Rejected (§58, §83).
* **One event per log line** — floods the webview; rejected in favour of batching.
* **Hand-written TypeScript protocol types** — duplication and drift; Wails generation is the source
  (ADR 001). Rejected (§32).

## Consequences

* The UI updates are push-driven and idle-quiet: no polling timers, no request storms.
* Event names and payload shapes are frozen contracts; changing one is a breaking frontend change and
  must be updated in `internal/events` and the frontend together.
* Backend tests can emit into a fake `Emitter` and assert batching/ordering without a webview
  (`events.Noop` exists for headless paths).
* Per-second/per-change rate limits are the backend's responsibility, not the frontend's, so a slow
  renderer cannot make the backend worse.
