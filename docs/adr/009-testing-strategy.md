# ADR 009 — Layered testing with a fake sing-box fixture

## Context

The prototype has no tests. The behaviour that must be trustworthy is almost entirely about a child
process (start/stop/restart, crash, ignored SIGTERM, elevated launch) and the network (release
downloads, checksums, rate limits) — both slow, flaky and privilege-sensitive in tests. The
specification requires meaningful backend tests with **most runtime tests not using the real sing-box
binary** (§67, §68), frontend tests that assert behaviour rather than internals (§69), and
application-level flows including "quit ⇒ managed process is dead" (§70).

## Decision

Test at the lowest layer that can prove the behaviour, with a fake sing-box binary as the shared
fixture.

* **Fake binary fixture.** `cmd/fakesingbox` (driven from `internal/singbox/faketest`) is built on
  demand by tests with `go build` into `t.TempDir()`. Behaviour is selected by `-scenario` or
  `FAKESINGBOX_SCENARIO`: `version`, `version-old`, `version-beta`, `version-fail`, `check-ok`,
  `check-fail`, `run`, `run-slow`, `run-crash`, `run-ignore-term`, `run-foreign`. Tests copy it to a
  path named `sing-box`, so the real code path (including PATH-independence and argument arrays) is
  exercised. Run scenarios honour `-pid-file`, `-status-file`, `-log-file`, so the privileged-helper
  code path is testable without root (ADR 005).
* **Backend levels**: domain unit tests (version parse/compare, structural validation, share links
  table-driven, redaction); storage tests against a temporary SQLite database including migrations
  from empty; application tests with the fake binary (supervisor state machine, duplicate start,
  crash, forced termination, apply/rollback, binary install with checksum mismatch and rollback);
  bindings smoke tests (facades return DTOs; no logic in bindings).
* **Frontend levels**: Vitest + React Testing Library assert user-visible behaviour (profile
  create/switch, structured edit preserves unknown fields, validation errors, raw dirty state, apply,
  rollback, runtime controls, binary update, error presentation) with the Wails bindings mocked at
  the module boundary. Playwright covers application-level flows where a browser is practical
  (`frontend/e2e`, `npm run e2e`).
* **CI gates** (spec §71): `go test ./...`, `go test -race ./...`, `go vet ./...`, backend
  staticcheck, and frontend lint/typecheck/test/build. Releases run after the test pipeline, never
  instead of it.
* **No network in unit tests.** Release HTTP is tested against `httptest` servers and fixtures; the
  live GitHub API is only touched by explicitly-tagged tests.

## Alternatives

* **Only integration tests with the real sing-box binary** — requires downloads, root for TUN, and is
  slow/flaky; the spec forbids depending on it for most runtime tests. Rejected.
* **Mocking `os/exec` instead of a fake executable** — proves the mock's contract, not the real
  process lifecycle (signals, exit codes, log streams). Rejected (§67).
* **Snapshot tests of generated JSON/UI** — brittle and assert internals, contrary to §69. Rejected.
* **No E2E layer** — the "quit kills sing-box" requirement (§70) needs an end-to-end assertion.
  Rejected.
* **Real network tests for downloads** — rate-limited and non-deterministic; the offline/corrupt/
  partial cases that matter most are impossible to trigger reliably. Rejected.

## Consequences

* The suite runs fast, offline and unprivileged, so it is safe as a required CI gate on every PR.
* New runtime scenarios are added by extending the fake binary's scenario table, one place.
* The fake binary and its scenario contract are themselves part of `internal-contracts.md`; changing
  a scenario both breaks callers visibly and is covered by a test.
* Some behaviour (real TUN, real notarization, real Windows installer) still needs manual verification
  before release; that is listed as "manual verification still required" rather than faked in CI.
