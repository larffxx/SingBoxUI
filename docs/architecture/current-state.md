# Current state — SingBoxUI (legacy Java/Spring prototype)

Audit basis: repository inspection at branch `dev`, commit `76ab0ca`, plus a live file-system check of the
data directory the running application creates (`~/.singboxui/`).

## 1. What the program is today

A Spring Boot 3.x application (single Maven module, `pom.xml` + `mvnw`) that:

1. binds an HTTP server on `localhost:8000`;
2. serves a Thymeleaf page (`src/main/resources/templates/index.html`) plus two static assets
   (`static/app.js` = 543 lines, `static/style.css` = 58 lines);
3. exposes a REST API under `/api/*` (`ConfigApiController`, 421 lines);
4. stores the sing-box configuration as one mutable JSON file on disk, held in memory as
   `ObjectNode config` inside `SingBoxConfigService`;
5. starts/stops a sing-box process with `ProcessBuilder`;
6. downloads sing-box from GitHub into `./bin/` (or `~/.singboxui/bin/`);
7. polls sing-box's Clash API every 2 s for traffic counters;
8. installs an AWT `SystemTray` icon with start/stop/quit;
9. on startup, optionally **relaunches the entire application as root/Administrator** if the config
   contains a `tun` inbound (`tray/PrivilegeEscalation.java`);
10. optionally installs itself for autostart (macOS LaunchAgent plist, Windows `HKCU\...\Run` key).

Java sources: 17 files, 2801 lines. Frontend: 3 files, 757 lines. No test sources exist
(`src/test/java/.../SingBoxUiApplicationTests.java` is the Spring context placeholder only).

## 2. Inventory

### 2.1 REST endpoints (all in `web/ConfigApiController.java`) — verdict

| Method | Path | Purpose | Verdict |
|---|---|---|---|
| GET | `/` | Thymeleaf editor page (`web/UiController.java`) | REMOVE |
| GET | `/api/config` | whole config JSON | REPLACE (binding `ConfigAPI.GetDraftSource`) |
| POST | `/api/config` | replace whole config, validate, save | REPLACE (`ConfigAPI.SaveRevision` + `ApplyRevision`) |
| POST | `/api/config/template/{name}` | overwrite config from a bundled template | REPLACE (`ConfigAPI.ApplyTemplate`) |
| GET | `/api/export` | download `config.json` | REPLACE (`ConfigAPI.Export`) |
| POST | `/api/import` | whole-config import | REPLACE (`ProfileAPI.Import`) |
| POST | `/api/inbounds` | upsert inbound by tag | REPLACE (structured editor, client-side patch) |
| DELETE | `/api/inbounds/{tag}` | delete inbound | REPLACE |
| POST | `/api/outbounds` | upsert outbound by tag | REPLACE |
| DELETE | `/api/outbounds/{tag}` | delete outbound | REPLACE |
| POST | `/api/dns/servers` | upsert DNS server by tag | REPLACE |
| DELETE | `/api/dns/servers/{tag}` | delete DNS server | REPLACE |
| POST | `/api/dns|/api/route|/api/log|/api/experimental|/api/ntp` | always returns an error string | REMOVE |
| POST | `/api/section/{name}` | replace `dns`/`route`/`log`/`experimental`/`ntp` object | REPLACE (`ConfigAPI.SaveRevision` on draft) |
| POST | `/api/route/rules` | append route rule | REPLACE (routing editor) |
| DELETE | `/api/route/rules/{index}` | delete route rule by index | REPLACE |
| POST | `/api/split/rules` | split-tunneling rule builder (domain/ip/geo/process/port/protocol → outbound) | REPLACE (`features/split-tunneling`) |
| GET | `/api/validate` | structural validation of in-memory config | REPLACE (`ConfigAPI.Validate`) |
| GET | `/api/status` | binary path + version + structural validity | REPLACE (`BinaryAPI.GetBinaryStatus`, `ConfigAPI.Validate`) |
| POST | `/api/check` | runs `sing-box check -c config.json` | REPLACE (`ConfigAPI.Check`) |
| GET | `/api/proc/status` | process status map | REPLACE (`RuntimeAPI.GetRuntimeStatus`) |
| POST | `/api/proc/start` | start sing-box | REPLACE (`RuntimeAPI.StartRuntime`) |
| POST | `/api/proc/stop` | stop sing-box | REPLACE (`RuntimeAPI.StopRuntime`) |
| POST | `/api/proc/restart` | restart sing-box | REPLACE (`RuntimeAPI.RestartRuntime`) |
| GET | `/api/proc/logs?lines=N` | polled log tail | REPLACE (Wails event `runtime:log`) |
| GET | `/api/traffic` | Clash API traffic snapshot | REPLACE (Wails event `traffic:snapshot`) |
| GET | `/api/settings` | `{autoConnect}` | REPLACE (`SettingsAPI.GetSettings`) |
| POST | `/api/settings` | set `autoConnect` | REPLACE (`SettingsAPI.UpdateSettings`) |
| GET | `/api/autostart` | `{supported, platform, enabled}` | REPLACE (`SettingsAPI.GetAutostart`) |
| POST | `/api/autostart` | enable/disable autostart | REPLACE (`SettingsAPI.SetAutostart`) |
| GET | `/api/singbox` | binary info (found, path, version, source, os, arch) | REPLACE (`BinaryAPI.GetBinaryStatus`) |
| POST | `/api/singbox/install` | download+extract latest release | REPLACE (`BinaryAPI.CheckForUpdates` + `InstallStableUpdate`) |
| POST | `/api/share/import` | share link → outbound | REPLACE (`ShareAPI.ParseShareLink` + `InsertShareOutbound`) |
| GET | `/api/share/export/{tag}` | outbound → share link | REPLACE (`ShareAPI.BuildShareLink`) |
| GET | `/api/meta` | enum lists for the UI (`tunDefault`, inbound/outbound/dns types, rule actions) | REPLACE (static TS enums in `shared/`) |

### 2.2 Java services — verdict

| File | Lines | Responsibility | Verdict |
|---|---|---|---|
| `SingBoxConfigService` | 411 | in-memory config + file persistence + structural validation + defaults + templates + known types | REPLACE by `app/config` + `app/profiles` + `domain/*` + SQLite revisions |
| `SingBoxProcessService` | 173 | start/stop/restart sing-box, 1000-line log ring, ANSI strip | REPLACE by `internal/app/runtime` supervisor |
| `SingBoxProvisioningService` | 293 | binary discovery (`settings → ./bin → PATH → download`), GitHub release download, tar/zip extract, version probe | REPLACE by `internal/app/binary` + `internal/singbox` (managed/custom source, checksum verification) |
| `SingBoxBinaryService` | 131 | `version`, `check`, `format` wrappers | REPLACE by `internal/singbox` (`version.go`, `validator.go`) |
| `TrafficService` | 146 | polls Clash API `/connections` every 2 s, aggregates up/down, per-outbound, vpn vs direct | REPLACE by `internal/app/traffic` (cancellable, event-driven, runtime-lifecycle-bound) |
| `ShareLinkService` | 468 | parse/build `vless/vmess/trojan/ss/hy2/tuic` links | KEEP (feature), reimplemented in `internal/app/share` with table-driven tests |
| `SettingsService` | 54 | single `{autoConnect}` flag in `ui-settings.json` | REPLACE by typed SQLite settings |
| `AutostartService` | 176 | macOS LaunchAgent plist / Windows `HKCU\...\Run` (`reg.exe`) | KEEP (feature), reimplemented behind `Autostart` interface in `internal/platform` |
| `StartupTasks` | 36 | delayed auto-connect on startup | KEEP (feature) → `app/settings` + `app/runtime` auto-connect |
| `TrayService` | 178 | AWT tray icon, start/stop/quit, 3 s refresh watcher | REPLACE by `internal/tray` — a native macOS menu bar item with no watcher: the menu follows events (ADR 007, ADR 010) |
| `PrivilegeEscalation` | 203 | detects port already used → opens browser; if config has `tun` and running from a packaged `.app`/`.exe` → relaunches the whole app via `osascript ... with administrator privileges` / `powershell Start-Process -Verb RunAs`; `SINGBOXUI_ELEVATED=1` guard | REMOVE (spec §9/§27) — replaced by a narrow privileged runtime launch (ADR 005) |
| `SingBoxProperties`, `PropsConfig` | 63 | config-path resolution, data dir (`cwd` if writable else `~/.singboxui`) | REPLACE by `internal/platform` data-dir resolution |
| `ApiErrors` | 15 | error DTO | REPLACE by typed error codes in `domain/apperr` |

### 2.3 Persistent files

| Path | Written by | Content | Verdict |
|---|---|---|---|
| `<cwd>/config.json` or `~/.singboxui/config.json` | `SingBoxConfigService.saveTo` (direct `Files.writeString`, **not atomic**) | the sing-box config | MIGRATE into profile + initial revision; file itself left untouched |
| `<cwd or ~/.singboxui>/ui-settings.json` | `SettingsService` | `{"autoConnect": bool}` | MIGRATE into `settings` table |
| `<dataDir>/bin/sing-box` | `SingBoxProvisioningService.install` | managed binary (currently 83 MB, downloaded 2026-09-11) | MIGRATE: adopted as the initial managed binary record (version probed at first launch) |
| `<dataDir>/logs/singboxui.log` | logback (`logback-spring.xml`) | application log | replaced by `log/slog` JSON logs in the app data dir (rotating) |
| `~/Library/LaunchAgents/com.larffxx.singboxui.plist` | `AutostartService` | autostart entry pointing at `java -jar` | REPLACED (points at the .app bundle) |
| `HKCU\Software\Microsoft\Windows\CurrentVersion\Run\SingBoxUI` | `AutostartService` | `javaw -jar ...` | REPLACED (points at the .exe) |
| `config.backup.json`, `config.with-ipv6-fix.json` (repo root, untracked) | manual user copies | backup configs | user data — untouched |

### 2.4 External processes

| Process | Launched by | Verdict |
|---|---|---|
| `sing-box run -c <config>` | `SingBoxProcessService.start` (`ProcessBuilder`, args array — already not shell-based) | REPLACE (supervisor with state machine + serialized ops) |
| `sing-box version` / `sing-box check -c` / `sing-box format -c` | `SingBoxBinaryService`, `SingBoxProvisioningService.probeVersion` | REPLACE (`sing-box` adapter) |
| `tar -xf <archive> -C <dir>` | `SingBoxProvisioningService.install` — **no checksum verification**, no path-traversal guard | REPLACE (Go archive extraction + SHA-256 verification against the release's checksum file) |
| `osascript - do shell script "..." with administrator privileges` | `PrivilegeEscalation` | REMOVE → narrow privileged launch |
| `powershell -NoProfile -Command Start-Process ... -Verb RunAs` | `PrivilegeEscalation` | REMOVE → `ShellExecuteExW("runas")` on a fixed argument vector |
| `launchctl load/unload <plist>` | `AutostartService` | REPLACE (no `launchctl` needed for LaunchAgents: `RunAtLoad` + login session) |
| `reg add/query/delete HKCU\...\Run` | `AutostartService` | KEEP mechanism (reimplemented with `golang.org/x/sys/windows/registry`) |

### 2.5 Background threads (`Thread` + daemon)

| Thread | Created in | Lifecycle | Verdict |
|---|---|---|---|
| `singbox-log-drain` | `SingBoxProcessService.drain` | dies with the process | REPLACE (context-bound log reader) |
| `traffic-poller` | `TrafficService.current` (lazily, on first read!) | **immortal**, 2 s loop, no stop | REPLACE (started on RUNNING, cancelled on stop/shutdown) |
| `tray-watcher` | `TrayService.init` | 3 s loop until `@PreDestroy` | REMOVE |
| `singbox-install` | `SingBoxProvisioningService.autoInstall` | one-shot on `ApplicationReadyEvent` | REPLACE (background job with context) |

### 2.6 Outbound HTTP calls

| Call | Where | Verdict |
|---|---|---|
| `GET https://api.github.com/repos/SagerNet/sing-box/releases/latest` | provisioning | KEEP feature, REPLACE implementation (`/releases` filtered by `prerelease==false && draft==false`) |
| `GET <asset browser_download_url>` | provisioning | KEEP feature, add checksum verification |
| `GET http://127.0.0.1:9090/connections` | traffic | KEEP feature (Clash API of the locally managed runtime) |
| `GET http://localhost:<port>/api/status` | `PrivilegeEscalation.isOursUp` (single-instance detection) | REMOVE with the HTTP server; single-instance can be re-added as a file lock if needed |

No other network traffic; no telemetry.

### 2.7 Privilege paths

* macOS: `osascript ... with administrator privileges` → whole app relaunched as root
  (env guard `SINGBOXUI_ELEVATED`), browser opened by the unprivileged copy.
* Windows: `powershell -Command Start-Process <exe> -ArgumentList <args> -Verb RunAs` → whole app
  elevated via UAC.
* Windows/Linux fallback: `run.bat`/`run.sh` scripts call UAC/`sudo` before starting the jar.
* Verdict: REMOVE. The GUI must never run elevated (spec §27/§28/§29).

### 2.8 OS-specific behaviour

| Behaviour | Location | Verdict |
|---|---|---|
| macOS: empty `interface_name` for TUN (auto `utunN`), Linux/Windows: `tun0` default | `SingBoxConfigService.tunDefault` | KEEP (template logic) |
| macOS: LaunchAgent plist | `AutostartService` | KEEP feature, move to `internal/platform/darwin` |
| Windows: `HKCU\...\Run`, `javaw.exe`, `.exe` binary name, `.zip` release asset | `AutostartService`, provisioning | KEEP feature, move to `internal/platform/windows` |
| macOS: POSIX exec bit `rwxr-xr-x` after download | provisioning | KEEP in managed-binary install |
| `configHasTun` regex sniff of the config file | `PrivilegeEscalation` | REPLACE (typed structural check on the active revision) |
| Packaged-app detection via `/Contents/MacOS/` or `*.exe` | `PrivilegeEscalation` | REPLACE (`os.Executable()` based) |

### 2.9 User-visible workflows today

1. **Edit config in browser**: open `http://localhost:8000` → tabs Outbounds / Inbounds / Route / DNS /
   Запуск / Raw JSON. Element cards with `{ }` JSON preview, modal forms generated from a client-side
   field-descriptor table (`app.js` ~line 16).
2. **Add outbound**: pick type → modal form → `POST /api/outbounds` → whole config re-fetched.
   Every form edit writes the config file immediately (no draft, no revision, no undo).
3. **Share link import/export**: paste link → outbound appended; export per outbound.
4. **Split tunneling**: "+ правило" modal → kind + values + outbound → `POST /api/split/rules`;
   `geosite-*`/`geoip-*` values auto-register remote rule-sets.
5. **Raw JSON**: textarea, load/validate/save, file import, download `config.json`.
6. **Check**: `POST /api/check` → `sing-box check` output shown in a banner.
7. **Runtime**: Запуск tab → install sing-box button, start/restart/stop, log `<pre>` refreshed by a
   3-second timer while the tab is open, autostart + auto-connect checkboxes.
8. **Traffic**: footer bar polled every 2 s from `/api/traffic`.
9. **Tray**: menu-bar icon with start/stop/quit.
10. **Templates**: three bundled JSON templates applied by overwriting the whole config.
11. **Packaging**: `jpackage` builds `SingBoxUI-*.exe` / `*.dmg` in GitHub Actions; local releases are
    the committed `singbox-ui-0.2.x*.zip` artifacts in the repo root.

### 2.10 Problems this rewrite must fix

* Whole-app root/Administrator execution (macOS password dialog and UAC) — security and UX.
* `localhost:8000` HTTP server, reachable by any local process, with no authentication: any page in the
  browser can rewrite the VPN config. No CSRF protection, no auth, no bind restriction beyond loopback.
* Direct non-atomic overwrite of the live config file; a crash mid-write corrupts the running config.
* Validation that saves: every structural edit persists immediately, so a broken config is on disk.
* No revisions, no history, no rollback, no diff.
* `TrafficService` poller starts as a side effect of a read and never stops.
* Logs delivered by polling; 1000-line in-RAM tail; no search/limit/pause.
* No checksum verification of downloaded sing-box; `tar -xf` extracted without a traversal guard.
* "Always latest release", including the possibility of a prerelease (currently `/releases/latest`
  excludes prereleases, but there is no version pinning, no rollback to a previous binary, no
  "installed version" record).
* Restart = stop + start with no rollback if the new config fails.
* Only one config; no profiles.
* Java runtime requirement + `jpackage` packaging (stale `Dockerfile`/`HELP.md` leftovers, Spring
  uber-jar, native-image-less 200 MB zips).
* Mutable in-memory config as the source of truth, cross-request `synchronized` methods.
* No tests at all.

## 3. Current-state summary

KEEP (as features, reimplemented): profiles-in-spirit (single config), structured inbound/outbound/DNS/
routing editing, split tunneling helper, raw JSON editing, import/export, share links, `sing-box check`,
managed binary download with version probing, templates, runtime start/stop/restart, live logs, traffic
monitoring, autostart, auto-connect.

REPLACE: persistence (SQLite + immutable revisions), runtime management (supervisor + state machine),
binary management (checksum-verified stable channel with rollback), logs/traffic transport (Wails events),
privilege model (narrow privileged launch), packaging (Wails, no Java/Node runtime), frontend
(React/TS/Vite + Monaco), validation (server-authoritative via the real binary).

REMOVE: Spring Boot, Java, Maven, Thymeleaf, `static/app.js` + `static/style.css`, REST API, HTTP server,
AWT tray (replaced by a native menu bar item, ADR 010), whole-app elevation, `run.sh`/`run.bat`/`SingBoxUI.command`, `jpackage` workflow, committed
release zips, stale `Dockerfile`/`HELP.md` references.
