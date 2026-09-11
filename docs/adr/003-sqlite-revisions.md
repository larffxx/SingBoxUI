# ADR 003 — SQLite with immutable configuration revisions

## Context

The legacy prototype kept the sing-box configuration as mutable in-memory state and wrote it back to
a single `config.json`, so an edit could destroy the previous good configuration with no history and
no way to recover. The specification requires profiles, immutable revisions with a `parentRevisionID`,
typed settings, a managed-binary record, migrations from day one, and explicitly forbids mutable
in-memory config as the source of truth (§11, §12, §16, §83).

## Decision

Persist application metadata in SQLite via `database/sql` and the pure-Go driver
`modernc.org/sqlite`, with embedded, versioned migrations. Schema in `internal/storage/migrations`
(`0001_init.sql`): `profiles`, `config_revisions`, `settings`, `managed_binary`, `application_state`.

* **Revisions are immutable.** Editing produces a new row; `config_json` is never `UPDATE`d. Rollback
  reads an old revision and inserts a *new* revision based on it (spec §55).
* Profiles own revisions through `ON DELETE CASCADE`; the active revision is a column
  (`active_revision_id`), and only one profile is active for the managed runtime at a time.
* **Runtime process state is not persisted as truth.** It is reconciled from the actual process and
  exposed as a typed in-memory snapshot (ADR 004). The database stores intent and history only.
* Settings are typed key/value rows validated by `internal/domain/settings`; unrelated dynamic values
  are never dumped into one untyped JSON map (§49).
* Migrations are embedded (`go:embed`) and applied transactionally on open; `schema_migrations`
  records applied versions. Migrations run from an empty database and are forward-only.
* The driver is pure Go, so both Windows and macOS builds need no cgo and no system SQLite.

## Alternatives

* **JSON files** (one per profile plus a metadata file) — no transactions, no referential integrity,
  and concurrent reads/writes need a hand-rolled locking scheme. Rejected.
* **A key/value store (bbolt, Badger)** — fast, but revisions, profiles and settings are relational;
  querying history would mean reimplementing indexes. Rejected.
* **A cgo SQLite driver (`mattn/go-sqlite3`)** — breaks the "no cgo" packaging goal for Windows
  cross-arch and complicates CI. Rejected.
* **An ORM (GORM/ent)** — see ADR 002; the schema is small and explicit SQL is clearer. Rejected.
* **PostgreSQL** — explicitly forbidden, and a desktop app has no server to run it on (§16).
* **Storing revisions as files on disk** — the filesystem is where materialised configs live, not
  where metadata belongs; atomicity across metadata and files is harder. Rejected.

## Consequences

* Every profile change is auditable and reversible; the history UI can render revisions and diffs
  without a separate log.
* Apply/rollback (ADR 008) can retain a failed candidate revision for inspection while keeping the
  active revision unchanged.
* A schema change means a new numbered migration; the embedded set is the only schema authority.
* The database is a single file under the platform data dir, which surfaces a clear error when a
  second instance opens it (single-instance detection is not otherwise required, feature matrix 30).
