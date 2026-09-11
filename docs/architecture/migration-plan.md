# Migration plan — legacy prototype → new desktop application

This is a **complete rewrite** of the implementation. The only thing preserved is user data.

## 1. What is preserved

| Data | Source | Destination | Mechanism |
|---|---|---|---|
| sing-box configuration | `<dataDir>/config.json` (`~/.singboxui/config.json` when the cwd is not writable, else the cwd) | a new profile + its initial revision | first-launch import offer (`app/config.ImportLegacyConfig`), idempotent per source path, source file **never** modified or deleted |
| `autoConnect` flag | `<dataDir>/ui-settings.json` | `settings.autoConnect` | copied during the same import; legacy file left in place |
| managed binary | `<dataDir>/bin/sing-box` (83 MB, downloaded by the legacy app) | `managed_binary` row pointing at `<newDataDir>/bin/sing-box/<version>/sing-box` | reused in place if executable and version-probeable, otherwise re-downloaded by the user |
| manual backups | `config.backup.json`, `config.with-ipv6-fix.json` | untouched | importable through `ProfileAPI.Import` → creates a separate profile |
| autostart entry | `~/Library/LaunchAgents/com.larffxx.singboxui.plist` (macOS) / `HKCU\...\Run\SingBoxUI` (Windows) | new entry pointing at the native app | the legacy entry is detected and reported in Settings as "old Java entry found — replace?"; deleting it is an explicit user action |

## 2. Data locations after the rewrite

```text
macOS    ~/Library/Application Support/SingBoxUI/  (singboxui.db, config/active.json, bin/<version>/, logs/)
Windows  %LOCALAPPDATA%\SingBoxUI\
```

Both are resolved through `internal/platform` — never derived from the working directory (the legacy
`cwd`-if-writable behaviour is dropped because a `.app` has no meaningful cwd).

## 3. Cutover stages (spec §81)

| Stage | Work | Legacy files replaced | Tests added | Acceptance criterion |
|---|---|---|---|---|
| 1 | Audit | — | — | `docs/architecture/current-state.md`, `feature-matrix.md` committed |
| 2 | Skeleton | — (added alongside) | `go vet`, `go build` smoke | `cmd/singboxui` builds; package boundaries fixed |
| 3 | Persistence: profiles, revisions, settings, migrations | `SingBoxConfigService` (persistence half), `SettingsService` | profile CRUD, delete constraints, revision creation, SQLite migration from empty DB | profile + initial revision created transactionally; revisions immutable |
| 4 | sing-box adapter | `SingBoxBinaryService`, `SingBoxProvisioningService` (probe part) | version parse, check success/failure via fake binary | `sing-box check` result drives validation status |
| 5 | Runtime supervisor | `SingBoxProcessService` | duplicate start, stop, restart, crash, forced termination, shutdown kills runtime | state machine + serialized ops verified with fake binary |
| 6 | Privilege boundary | `PrivilegeEscalation`, `run.sh`/`run.bat`/`SingBoxUI.command` | privilege decisions, cancellation mapping | UI process never elevated; TUN launch goes through one narrow request |
| 7 | Wails shell | `web/UiController`, Spring bootstrap | bindings smoke | `wails build` produces a launching app |
| 8 | React shell | `templates/index.html`, `static/app.js`, `static/style.css` | router/render tests | six top-level screens, dark/light |
| 9 | Editors | same JS | structured-edit preservation, raw dirty state, validation errors | unknown fields survive edits; Monaco-based raw editor |
| 10 | Apply/rollback | `POST /api/config` | atomic write, failed apply restores last-known-good, rollback creates revision | failed apply never marks a revision active |
| 11 | Logs + traffic | `GET /api/proc/logs`, `TrafficService` | ring buffer bounds, collector cancellation | events only, no polling |
| 12 | Binary manager | `POST /api/singbox/install` | stable selection, prerelease exclusion, checksum mismatch, rollback | verified install only; offline still works with the installed binary |
| 13 | Share links | `ShareLinkService` | table-driven parse/build round-trips | `vless/vmess/trojan/ss/hy2/tuic` parse and build |
| 14 | Autostart + auto-connect | `AutostartService`, `StartupTasks` | adapter behaviour, auto-connect failure surfacing | adapter behind the `Autostart` interface |
| 15 | Packaging + CI | `.github/workflows/release.yml`, `jpackage` | — | macOS `.app`/`.dmg`, Windows installer, test pipeline gate |
| 16 | Migration/import | `--singbox.config-path` flag | legacy import creates profile + revision, source untouched | first launch offers the import |
| 17 | Final tests | `SingBoxUiApplicationTests` | E2E flows with the fake binary | `go test -race ./...` + frontend suite green |
| 18 | Delete legacy | `pom.xml`, `mvnw*`, `src/`, `.mvn/`, `bin/`, `run.sh`, `run.bat`, `SingBoxUI.command`, release zips, `HELP.md`, jpackage workflow | — | repository contains no Java/Maven/Thymeleaf/REST code; `rg 'springframework|jpackage|/api/'` is clean |

Intermediate commits may contain both stacks; the final branch may not.

## 4. Rollback during migration

The legacy jar and the committed `singbox-ui-0.2.x` zips stay runnable until stage 18. If the new app
turns out to be unusable in practice, the user can still start the legacy jar; the import never mutates
`config.json`, so both applications can read the same configuration file simultaneously (as long as only
one of them runs sing-box at a time, which the runtime supervisor reports as `RUNTIME_ALREADY_RUNNING`
when it finds a foreign sing-box process holding the TUN device).

## 5. Explicit non-goals in migration

* no transliteration of Java classes into Go equivalents;
* no compatibility shim for `/api/*`;
* no preservation of `ui-settings.json` beyond `autoConnect`;
* no automatic deletion of anything the user wrote by hand.
