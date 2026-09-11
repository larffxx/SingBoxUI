# ADR 002 — Go application architecture

## Context

The legacy application is a Spring Boot program where controllers, services, sing-box process
control, persistence and platform code all live in one JVM with no enforced dependency direction.
The specification forbids transliterating that shape into Go god objects (§7), forbids DI
frameworks, ORMs and web frameworks (§3), and requires a strict dependency direction from the Wails
transport down to domain/ports and adapters (§5).

## Decision

Use a flat, explicit package layout under `internal/` with one composition root.

* `cmd/singboxui/main.go` is the composition root: it parses flags, resolves paths through
  `internal/platform`, opens SQLite, constructs the sing-box adapter, the platform adapter, every
  application use case and the Wails app, then owns the root `context.Context` and the shutdown hook.
  No other package performs wiring.
* Dependency direction is one-way: `internal/desktop/bindings` → `internal/app/*` → domain and
  ports. `internal/domain/*` imports only the standard library; `internal/app/*` never imports
  `internal/desktop/*`; bindings contain no SQL, no `os/exec` and no business rules.
* Interfaces are small ports declared next to their consumer and named after the boundary
  (`ConfigStore`, `Runtime`, `BinarySource`, `PrivilegeRunner`, `Autostart`, `TrafficSource`,
  `events.Emitter`), not generic factories or repositories (§84).
* Use cases are plain structs constructed in `main` with explicit fields. There is no service
  locator, no reflection and no container.
* Every long-running worker takes a `context.Context` from the composition root; packages hold no
  mutable package-level state (§62, §63).
* Typed errors (`internal/domain/apperr`) cross boundaries; the frontend branches on `Code`, never on
  message text (§33).

## Alternatives

* **One `internal/app` package** — simpler at first, but the seven use-case areas (profiles, config,
  runtime, binary, traffic, share, settings) would tangle and could not be worked on in parallel.
  Rejected.
* **Spring-style layered packages** (`controllers/`, `services/`, `repositories/`) — the exact shape
  the rewrite removes; encourages god services and shares mutable state across features. Rejected
  (§7, §83).
* **Hexagonal over-abstraction** (ports package, `IProfileServiceFactoryProvider`) — ceremony with no
  boundary behind it. Rejected by §84.
* **A DI framework (wire, fx, dig)** — hides the dependency graph the composition root is supposed to
  make readable. Rejected (§3).
* **An ORM** — the schema is five tables with hand-written SQL and migrations; an ORM adds a
  dependency and hides SQL without replacing it. Rejected (§3).

## Consequences

* Reading `main.go` is enough to understand what the process is made of.
* Parallel workstreams can own `app/config`, `app/runtime`, `app/binary` without merge conflicts
  because the package boundary is also the ownership boundary.
* Testability does not require a webview: bindings are thin facades callable directly, and all logic
  lives in `internal/app/*` (ADR 009).
* The layout is enforced by review and by import direction; a future `internal/arch` test can assert
  the forbidden edges mechanically.
* The `internal/desktop/dto` types are the single source of the generated TypeScript models (ADR 007);
  duplicating them in the frontend is forbidden (§32, §83).
