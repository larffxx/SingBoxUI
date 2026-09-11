# ADR 005 — Privilege boundary

## Context

The legacy `PrivilegeEscalation` service relaunched the **entire application** through
`osascript … with administrator privileges` (macOS) or `Start-Process -Verb RunAs` (Windows) whenever
the config contained TUN, guarded only by a `SINGBOXUI_ELEVATED=1` environment variable. That makes
the whole GUI — file handling, JSON parsing, the embedded webview — run as root/Administrator. The
specification forbids whole-application elevation, forbids generic command execution from the
privileged path, requires cancellation to be a normal failure, and requires the elevated process to
remain stoppable (§27, §28, §29, §83).

## Decision

Keep the UI unprivileged and isolate elevation to one narrow, validated operation.

* `internal/privilege` defines the port: `Request` (absolute `BinaryPath`, `ConfigPath`, `WorkDir`,
  `LogPath`, `PIDPath`, `StatusPath`, `Elevate`, `Reason`), `Process` and `Runner`. Every path is
  validated to be absolute and inside the application data directory; implementations re-validate.
* Privileged launches start the dedicated helper `cmd/singboxui-priv`, whose **only** operations are
  `start` and `stop` of one already-validated sing-box binary/config pair. It accepts no other
  operation and no free-form arguments — there is no generic command execution behind the boundary.
* Elevation uses the OS mechanism from the platform layer: macOS `osascript … with administrator
  privileges`; Windows `ShellExecuteExW(…, "runas")`. Credentials are never captured or stored.
* The helper writes `PIDPath` (pid + start time), `StatusPath` (exit code JSON) and forwards merged
  stdout+stderr into `LogPath`, so `Process.Wait()`/`Logs()`/`Terminate()` work identically to an
  unprivileged launch. Termination signals the recorded PID after verifying process identity.
* User cancellation of the elevation prompt maps to `privilege.ErrCancelled` →
  `apperr.CodePrivilegeDenied` (`PRIVILEGE_DENIED`) — an ordinary, surfaced failure, never a crash.
* `singboxui-priv` is built by the Makefile/CI and shipped beside the main binary (macOS
  `Contents/MacOS/singboxui-priv`, Windows `singboxui-priv.exe`).

## Alternatives

* **Whole-app elevation (legacy)** — a compromised config or webview runs as root; explicitly
  forbidden. Rejected.
* **`sudo`/`runas` with a saved password, or a sudoers/`/etc/sudoers.d` entry** — retains
  credentials or grants standing privilege; contradicts "no credential retention" and widens the
  boundary. Rejected.
* **A root LaunchDaemon/Windows service privileged helper (SMJobBless / `--runas` daemon)** — the
  "correct" Apple/Microsoft pattern, but it is a large amount of platform machinery (code-signing
  requirements, XPC/service installation, lifetime management) and cannot be validated for the
  rewrite's scope. Rejected for the initial rewrite; the narrow-request `Runner` interface leaves
  room to swap the macOS mechanism without touching callers.
* **Requiring the user to run sing-box manually for TUN** — removes the product's core value.
  Rejected.
* **Disabling TUN support** — out of scope; the feature is retained (§8).

## Consequences

* The unprivileged process parses untrusted config and drives the webview; only the sing-box child
  ever runs elevated, and only with a validated path inside the data directory.
* TUN launch always goes through one code path, testable without root using the fake sing-box fixture
  (`-pid-file`, `-status-file`, `-log-file`, ADR 009).
* macOS builds that use `osascript` need the automation entitlement when signed (see
  `packaging/macos/entitlements.plist`); unsigned local development builds keep working.
* The privilege decision lives in the platform adapter (`platform.PrivilegeRunner()`); the
  application layer only ever calls `Runner.Start`, so Windows and macOS can differ without a branch
  in business logic.
