# ADR 004 — Runtime supervisor

## Context

The legacy application represented sing-box lifecycle with several unrelated booleans and performed
start/stop/restart from Spring request handlers, so two starts could race, restart and stop could
interleave, and `@PreDestroy` merely removed the tray icon instead of stopping the process. The
specification requires exactly one authoritative supervisor, a five-state machine, a typed
`RuntimeStatus` snapshot, serialized lifecycle commands, ordered application shutdown, and no leaked
child processes (§22, §23, §25, §26).

## Decision

One supervisor in `internal/app/runtime` owns the process, exposed as the `Runtime` port.

* **State machine**: `STOPPED → STARTING → RUNNING → STOPPING → STOPPED`, plus `FAILED`
  (`internal/domain/runtime.State`). The state is the truth; booleans are not used to represent
  lifecycle.
* **Serialized commands**: every lifecycle request (start, stop, restart, apply-triggered restart,
  shutdown) goes through a single command entry point (mutex plus command handling), so two starts,
  restart-vs-stop, apply-vs-restart and shutdown-vs-manual-restart cannot interleave. Concurrent
  requests receive `RUNTIME_BUSY` rather than racing.
* **Typed snapshot**: `runtime.Status` (state, pid, startedAt, uptime, active profile/revision,
  binary version, last exit code, last error) is pushed as the `runtime:status` event on every
  change (ADR 007).
* **Logs**: stdout/stderr are read by a cancellable reader into a bounded ring buffer; the frontend
  receives batched `runtime:log` events. Logs are never persisted in an unbounded array (§47).
* **Shutdown**: the Wails shutdown hook cancels the root context; the supervisor marks shutting-down,
  rejects new operations, terminates sing-box gracefully, waits with a timeout, kills the process
  tree, then stops traffic/log workers and the database (ADR 004/008, spec §26).
* **Process control** uses `os/exec` argument arrays through the `privilege.Process`/`Runner` port —
  never `sh -c` or a constructed shell string (§24).

## Alternatives

* **Scattered booleans plus per-call locks** — exactly the legacy failure mode; ordering bugs are
  undetectable. Rejected.
* **A goroutine per command** — concurrency without a serialization point; duplicates and races stay
  possible. Rejected (§23).
* **An external supervisor (systemd/launchd/nssm) or sidecar daemon** — the app is a single desktop
  binary with a mandatory "quit means quit" lifecycle (§10); a daemon would own the process across
  app exits, which is precisely what the spec forbids. Rejected.
* **Driving sing-box from a long-lived `sh -c` pipeline** — forbidden shell composition and loses the
  exit status/PID. Rejected (§24).

## Consequences

* Runtime state is always a function of a single in-process owner; the snapshot can be reconciled
  against the real process on startup, including foreign sing-box processes it did not start
  (`RUNTIME_ALREADY_RUNNING`).
* Quitting the application provably stops the managed sing-box; this is covered by the shutdown
  integration test (spec §26, §70).
* The same serialization point is what the config apply pipeline uses to restart after an apply
  (ADR 008), so apply cannot fight a manual restart.
* Traffic collection and log reading are owned workers bound to `RUNNING`/the root context, so they
  cannot outlive the runtime (ADR 007).
