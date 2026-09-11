# Feature matrix — legacy behaviour → target implementation

Legend: **KEEP** (same feature, new implementation) · **IMPROVE** (feature kept, semantics fixed) ·
**ADD** (new) · **REMOVE** (dropped deliberately).

| # | Feature | Legacy implementation | Target package / binding | Verdict |
|---|---|---|---|---|
| 1 | Single sing-box config editing | `SingBoxConfigService.getConfig/replaceConfig` (mutates + saves on every edit) | `app/config` drafts + immutable revisions, `ConfigAPI` | IMPROVE |
| 2 | Structured outbound editing | modal forms from client-side field table, `POST /api/outbounds` | `frontend/features/outbound-editor` (RHF+Zod, patch-preserving) | IMPROVE |
| 3 | Structured inbound editing | `POST /api/inbounds` | `frontend/features/inbound-editor` | IMPROVE |
| 4 | Structured DNS editing | `POST /api/dns/servers`, `POST /api/section/dns` | `frontend/features/dns-editor`, `ConfigAPI.SaveRevision` | IMPROVE |
| 5 | Routing rules editing | `POST /api/route/rules`, delete by index | `frontend/features/routing-editor` | IMPROVE |
| 6 | Split tunneling helper | `POST /api/split/rules` (domain/ip/geo/process/port/protocol → outbound) | `frontend/features/split-tunneling` (rule descriptors → JSON) | IMPROVE |
| 7 | Raw JSON editing | `<textarea>` + load/validate/save | `frontend/features/raw-editor` (Monaco, markers, dirty state, diff vs active) | IMPROVE |
| 8 | Whole-config import | `POST /api/import` (overwrites immediately) | `ProfileAPI.Import` → new profile + revision | IMPROVE |
| 9 | Whole-config export | `GET /api/export` | `ConfigAPI.ExportRevision` (plain sing-box JSON) | KEEP |
| 10 | Share link import | `POST /api/share/import` | `ShareAPI.ParseShareLink` → preview → insert as revision | IMPROVE |
| 11 | Share link export | `GET /api/share/export/{tag}` | `ShareAPI.BuildShareLink` | KEEP |
| 12 | Structural validation | `SingBoxConfigService.validate` (Java) | `domain/config` validation (Go, pure, no persistence) | KEEP |
| 13 | Canonical validation | `POST /api/check` → `sing-box check` | `singbox.Validator` used by the apply pipeline | IMPROVE |
| 14 | `sing-box version` / `format` | `SingBoxBinaryService` | `singbox.Binary.Version`, `singbox.Format` | KEEP |
| 15 | Start sing-box | `POST /api/proc/start` | `RuntimeAPI.StartRuntime` → supervisor | IMPROVE |
| 16 | Stop sing-box | `POST /api/proc/stop` | `RuntimeAPI.StopRuntime` | IMPROVE |
| 17 | Restart sing-box | `POST /api/proc/restart` (stop+start, no rollback) | `RuntimeAPI.RestartRuntime` (serialized, rollback on failure) | IMPROVE |
| 18 | Runtime status | `Map<String,Object>` from `proc.status()` | typed `RuntimeStatus` snapshot from the state machine | IMPROVE |
| 19 | Live logs | `GET /api/proc/logs?lines=N`, 3 s polling, 1000-line RAM tail | `runtime:log` Wails events + start/pause/clear/search/limit | IMPROVE |
| 20 | Traffic monitoring | Clash API polled every 2 s from the frontend, immortal poller | `app/traffic` collector bound to RUNNING, `traffic:snapshot` events | IMPROVE |
| 21 | Managed sing-box download | `POST /api/singbox/install` (latest, no checksum, `tar -xf`) | `BinaryAPI.CheckForUpdates` + `InstallStableUpdate` (stable only, SHA-256, safe extract, probe, atomic install, rollback) | IMPROVE |
| 22 | Custom sing-box binary | `--singbox.binary=...` only via CLI flag | persisted `binarySource=managed\|custom` + `customBinaryPath`, explicit switch | IMPROVE |
| 23 | Templates | 3 bundled JSONs, overwrite config | 5 starters (`Empty`, `TUN basic`, `TUN + VLESS Reality`, `SOCKS local proxy`, `Selector`) → new revision | IMPROVE |
| 24 | Autostart | macOS LaunchAgent (`java -jar`), Windows `HKCU\...\Run` (`javaw`) | `platform.Autostart` interface, points at the native app | KEEP |
| 25 | Auto-connect | `StartupTasks` delayed start | settings-driven auto-connect with validation gate and persistent error surface | IMPROVE |
| 26 | System tray | AWT `SystemTray` icon | — | REMOVE (§10) |
| 27 | Whole-app elevation | `PrivilegeEscalation` relaunch as root/Administrator | narrow privileged runtime launch (`internal/privilege`, ADR 005) | REMOVE/ADD |
| 28 | Browser UI on `localhost:8000` | Spring MVC + Thymeleaf + `app.js` | Wails webview + React | REMOVE |
| 29 | REST API | 30+ `/api/*` endpoints | Wails bindings (no network surface) | REMOVE |
| 30 | Single-instance detection | HTTP probe of `/api/status` | not required (desktop app; SQLite file lock surfaces a clear error) | REMOVE |
| 31 | Java runtime requirement | uber-jar / jpackage | single native binary, frontend assets embedded | REMOVE |
| 32 | Multiple profiles | — | `profiles` table + `ProfileAPI` | ADD |
| 33 | Configuration revisions | — | `config_revisions` table, immutable, `parentRevisionID` | ADD |
| 34 | Draft/apply lifecycle | — | `ConfigAPI.SaveRevision` / `ApplyRevision` with atomic replace + health check + rollback (ADR 008) | ADD |
| 35 | Revision history + rollback | — | `frontend/routes/…/history`, `ConfigAPI.Rollback` | ADD |
| 36 | Config diff | — | Monaco diff: draft vs active, revision vs revision | ADD |
| 37 | Typed error codes | `{error: "..."}` strings | `domain/apperr` codes driving frontend behaviour | ADD |
| 38 | Typed settings | `{autoConnect}` in JSON file | `settings` table (9 typed keys) | ADD |
| 39 | Legacy config migration | — | first-launch import offer (`app/config.ImportLegacyConfig`) | ADD |
| 40 | Secret redaction | — | `internal/logging/redact` used for every log/debug export | ADD |
| 41 | Stable-channel updates with pinned version | — | `managed_binary` table (installed version, asset, sha256, previous binary for rollback) | ADD |
| 42 | Graceful shutdown stops sing-box | `@PreDestroy` only removes the tray | mandatory ordered shutdown (ADR 004/008) | ADD |
| 43 | Windows arm64 / macOS universal packaging | jpackage dmg/exe | `packaging/` + CI matrix (Wails), universal `.app` where practical | IMPROVE |
| 44 | Tests | none | Go unit/integration + fake sing-box fixture + Vitest/RTL + Playwright | ADD |
