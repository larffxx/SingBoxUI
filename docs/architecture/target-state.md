# Target state — SingBoxUI (Wails + Go + React)

Authoritative source: the rewrite specification. This document maps it onto concrete packages and
shows the dependency direction that the implementation must keep.

## 1. Runtime shape

A single native desktop binary per platform (Windows amd64/arm64, macOS amd64/arm64), built by Wails v2.
No HTTP server, no Java, no Node at runtime. React/TS assets are embedded in the binary.

```text
cmd/singboxui/main.go            composition root: flags, paths, SQLite, sing-box adapter,
                                 platform adapters, use cases, Wails app, shutdown hook
```

## 2. Dependency direction

```text
React UI  ──►  Wails bindings (internal/desktop/bindings, transport only)
                    │
                    ▼
               application use cases (internal/app/*)
                    │
                    ▼
               domain + ports (internal/domain/*, small interfaces declared next to their consumer)
                    │
     ┌──────────────┼───────────────┬────────────────┬──────────────────┐
     ▼              ▼               ▼                ▼                  ▼
storage/sqlite  singbox        platform/darwin   platform/windows   logging/redact
                                (autostart, privileges, paths)
```

Rules enforced by review and by package layout:

* bindings import only `internal/app/*` and `internal/desktop/dto`; no SQL, no `os/exec`, no business rules;
* `internal/domain/*` imports nothing but the standard library;
* `internal/app/*` never imports `internal/desktop/*`;
* every worker gets a `context.Context` from the composition root; no package-level mutable state.

## 3. Packages

```text
internal/domain/profile      Profile, Revision, RevisionSource, ValidationStatus
internal/domain/runtime      State (STOPPED..FAILED), Status snapshot
internal/domain/config       structural validation, section/tag helpers, rule descriptors
internal/domain/applications flat application descriptor: name, bundle id, path, process condition
internal/domain/apperr       typed error codes + Error type (code/message/details/operation)

internal/storage/sqlite      database/sql + modernc.org/sqlite (pure Go), migrations, repositories
internal/storage/migrations  embedded SQL (0001_init.sql, …)

internal/singbox             Binary (version probe/exec path), Validator (check), Process (run/terminate), Format

internal/platform            Platform interface, data-dir resolution, Autostart interface, ApplicationCatalog interface
internal/platform/darwin     LaunchAgent autostart, osascript-based privileged launch, SIGTERM termination
internal/platform/windows    registry autostart, ShellExecuteExW(runas) privileged launch, taskkill tree termination
internal/privilege           PrivilegeRunner port + narrow privileged-launch helper mode

internal/app/profiles        profile CRUD, duplicate, switch, delete constraints, import/export
internal/app/config          draft source, validation, revisions, apply pipeline, rollback, templates, legacy import
internal/app/runtime         the single supervisor: state machine, serialized ops, log ring buffer
internal/app/binary          managed/custom binary source, stable release check, verified install, rollback
internal/app/traffic         Clash API collector bound to RUNNING
internal/app/share           share-link parse/build (isolated, table-driven tests)
internal/app/settings        typed settings + autostart/auto-connect orchestration
internal/app/apps           installed programs as routing targets (ADR 011, ADR 013), macOS bundle and
                            Windows program catalogs behind one port

internal/desktop/bindings    ProfileAPI, ConfigAPI, RuntimeAPI, BinaryAPI, SettingsAPI, ShareAPI, TrafficAPI
internal/desktop/dto         frontend-facing structs (Wails generates TS models from these)
internal/events              event names + emitter port (implemented by the Wails runtime)
internal/logging             slog setup + secret redaction
internal/atomicfile          temp write → fsync → atomic rename → dir fsync

frontend/                    Vite + React + TS(strict) + TanStack Router/Query + RHF/Zod + Monaco + Tailwind
packaging/                   macOS .app/.dmg pipeline, Windows installer pipeline, signing hooks
scripts/  Makefile  wails.json  go.mod
```

## 4. Persistence (SQLite, migrations from day one)

```text
schema_migrations(id, applied_at)
profiles(id, name UNIQUE, description, created_at, updated_at, active_revision_id, last_used_at)
config_revisions(id, profile_id→profiles ON DELETE CASCADE, parent_revision_id, created_at, source,
                 config_json, structural_validation_status, singbox_validation_status, singbox_version)
settings(key PRIMARY KEY, value)
managed_binary(id=1, version, path, asset_name, sha256, installed_at, previous_path, previous_version)
application_state(key PRIMARY KEY, value)
```

Revisions are immutable: an edit creates a row, never an UPDATE of `config_json`.
Runtime state is **not** persisted as truth — it is reconciled from the actual process.

## 5. Config apply pipeline (transactional)

```text
candidate JSON ─► JSON parse ─► structural validation (pure) ─► materialise temp file (same FS)
    ─► fsync ─► sing-box check ─► atomic rename over active config ─► restart if RUNNING
    ─► health confirmation ─► mark revision active
failure at any step after the rename ─► restore last-known-good file + previous runtime ─► typed error,
    candidate revision retained, not marked active
```

## 6. Runtime supervisor

One owner of the process. States `STOPPED → STARTING → RUNNING → STOPPING → STOPPED`, plus `FAILED`.
All lifecycle commands pass through a single serialized command entry point (mutex + command channel),
so two starts, restart-vs-stop, apply-vs-restart and shutdown-vs-manual-restart cannot interleave.
Status snapshots are typed (`RuntimeStatus`) and pushed as `runtime:status` events.

## 7. Privilege boundary

The UI process stays unprivileged. When the active revision needs TUN, the supervisor asks the platform
privilege runner for a **narrow launch**: `singboxui --privileged-run --config <validated path>`
(official sing-box binary resolved by the unprivileged side, path constrained to the app data dir),
elevated through the OS (macOS: `osascript … with administrator privileges`; Windows:
`ShellExecuteExW("runas")`). No generic command execution, no credential retention, cancellation is a
normal `PRIVILEGE_DENIED` failure, and the PID is tracked so it can always be stopped.

## 8. Events

| Event | Payload | Frequency |
|---|---|---|
| `runtime:status` | `RuntimeStatus` | on every state change |
| `runtime:log` | `LogRecord[]` | batched, ≤ ~10/s |
| `traffic:snapshot` | `TrafficSnapshot` | ≤ 1/s while RUNNING |
| `binary:progress` | `BinaryProgress` | during download/install |
| `config:apply` | `ApplyProgress` | per apply stage |
| `app:notice` | `Notice` | errors that need a persistent surface (auto-connect failure) |

## 9. Frontend information architecture

```text
/dashboard        runtime state, active profile/revision, sing-box version, update availability, traffic, last error
/profiles         list + create/import
/profiles/$id     workspace: Overview | Outbounds | Inbounds | Routing | DNS | Raw JSON | History
/routing          не отдельный экран: правила маршрутизации, включая выбор приложений по названию,
                  принадлежат профилю (ADR 011, ADR 013)
/dns              global DNS helper
/runtime          controls, live log console, traffic
/settings         binary source/update, autostart, auto-connect, theme, log level
```

State ownership: TanStack Query for backend state, RHF+Zod for forms, router for navigation, local state
for ephemeral UI. No global store.

## 10. Definition of done (mapped to verification)

* `go test ./...`, `go test -race ./...`, `go vet ./...` green;
* frontend `npm run lint`, `npm run typecheck`, `npm test`, `npm run build` green;
* `wails build` produces a launching macOS `.app`; CI matrix builds Windows/macOS (amd64/arm64);
* quitting the app provably kills the managed sing-box (integration test with the fake binary);
* no legacy Java/Maven/Thymeleaf/EPS/static assets and no `/api/*` routes remain;
* architecture docs and ADRs match the implementation.
