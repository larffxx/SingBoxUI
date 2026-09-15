# ADR 008 — Transactional config apply and rollback

## Context

The legacy app overwrote the active `config.json` on every edit and restarted sing-box with no
validation gate and no rollback; a bad config could leave the VPN down with the previous good
configuration lost. The specification requires applying configuration to be transactional from the
user's perspective: validate → materialise → `sing-box check` → atomic replace → restart if running →
health confirmation → mark active, with restoration of the last-known-good file and runtime on
failure, and the failed candidate retained but never marked active (§13, §14, §15, §37).

## Decision

The apply pipeline in `internal/app/config` is a single transactional use case over the draft model.

* **Draft never mutates the active revision.** Editing happens on a draft (frontend state and/or a
  server-side draft source); `Apply` is explicit and is the only operation that changes the active
  configuration (§13).
* **Ordered pipeline**: JSON parse → pure structural validation (`internal/domain/config`) → write a
  temporary config file on the **same filesystem** → fsync → `sing-box check` → atomic rename over
  the active config → restart through the runtime supervisor if RUNNING → health confirmation → mark
  the revision active.
* **Atomic filesystem operations** (`internal/atomicfile`): temp write → fsync file → validate →
  atomic rename → dir fsync where supported. The active file is never overwritten in place (§15).
* **Failure handling** at any step after the rename restores the last-known-good materialised config
  and the previous runtime state, returns a typed error (`CONFIG_CHECK_FAILED`, `CONFIG_APPLY_FAILED`,
  `RUNTIME_START_FAILED`), and retains the failed candidate revision for inspection **without**
  marking it active.
* **Validation is canonical**: `internal/singbox.Validator.Check` runs the real selected binary
  (`<binary> check -c <config>`); a non-zero exit is `CheckResult{OK:false}` plus a typed error, not a
  silent Go failure. The app never reimplements sing-box's validator (§37).
* **Progress is streamed** as `config:apply` events per stage so the UI can show where a failure
  happened (ADR 007).
* **Rollback** reads a historical revision and creates a new revision from its content; history is
  never mutated (§55).

## Alternatives

* **Direct overwrite of the active config (legacy)** — forbidden: no atomicity, no rollback, previous
  good config lost (§15, §83). Rejected.
* **Validate after writing the active file** — leaves a window where the live config is invalid.
  Rejected; validation precedes the replace.
* **Write-then-rename without fsync** — a crash can leave a truncated file. Rejected (§15).
* **Copy-based backup instead of atomic rename** — not crash-safe and duplicates large files on the
  same filesystem. Rejected.
* **No restart / user-managed restart after apply** — the runtime could run a config that no longer
  matches the active revision. Rejected (§14).
* **Marking a revision active optimistically before health confirmation** — contradicts "do not mark
  the failed revision active". Rejected.

## Consequences

* Applying a configuration either fully succeeds or leaves the previous known-good state in place;
  the user can always inspect what failed.
* The apply pipeline depends on two ports only — the sing-box adapter and the runtime supervisor — so
  it is testable end-to-end with the fake sing-box binary (ADR 009) without root or a real TUN.
* The same atomic-write primitive is reused for the initial materialisation and for saved configs, so
  there is one place to audit for crash safety.
* Restarting after apply goes through the supervisor's serialized command entry point (ADR 004), so
  an apply cannot race a manual restart.
