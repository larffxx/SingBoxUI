# Internal contracts

Frozen interfaces between packages. Parallel work depends on these signatures: change them only
together with every caller, and update this file in the same commit.

Existing port packages (already in the tree, do not edit without an ADR):

| Package | Purpose |
|---|---|
| `internal/domain/apperr` | typed error codes (`CONFIG_INVALID`, `RUNTIME_START_FAILED`, …) and the `Error` type |
| `internal/domain/profile` | `Profile`, `Revision` (immutable), `Source`, `ValidationStatus` |
| `internal/domain/runtime` | `State` (`STOPPED`…`FAILED`), `Status` snapshot |
| `internal/domain/settings` | typed settings model, `Marshal`/`Unmarshal`, defaults |
| `internal/domain/config` | `Parse`, `Validate` (structural only), `HasTUN`, `Tags`, `Pretty` |
| `internal/events` | event names + `Emitter` port |
| `internal/privilege` | `Request`, `Process`, `Runner` — the narrow privileged boundary |
| `internal/platform` | `Info`, `Paths`, `Autostart`, `Platform` interface |
| `internal/atomicfile` | temp → fsync → atomic rename → dir fsync |
| `internal/storage/sqlite` | profiles, revisions, settings, managed binary, application state |
| `internal/idgen`, `internal/logging` | UUIDs, slog setup, secret redaction |

## 1. `internal/singbox` — the sing-box adapter

Files: `version.go`, `binary.go`, `validator.go`, `process.go`, `release.go`, `archive.go`.

```go
// --- version.go -----------------------------------------------------------------------------
type Version struct {
    Major, Minor, Patch int
    Pre                 string // "beta.1", "rc.2" — empty for a stable release
    Raw                 string // the string the binary reported
}

func ParseVersion(s string) (Version, error) // accepts "1.14.0", "sing-box version 1.14.0", "1.13.0-beta.4"
func (v Version) String() string             // "1.14.0" / "1.13.0-beta.4"
func (v Version) Stable() bool               // Pre == ""
func (v Version) Compare(o Version) int      // -1, 0, 1
func (v Version) Empty() bool                // all components zero and no raw string

// --- binary.go ------------------------------------------------------------------------------
// Probe runs `<path> version` and parses the reported version. It never trusts
// the file name and always uses an argument array (spec §24).
func Probe(ctx context.Context, path string) (Version, error)
func IsExecutable(path string) bool
// Locate returns the executable resolved from settings (managed or custom); it
// never falls back to PATH (spec §20).
func Locate(managed ManagedBinaryRef, s settings.Settings, p platform.Paths) (string, error)
```

```go
// --- validator.go ---------------------------------------------------------------------------
type CheckResult struct {
    OK      bool     `json:"ok"`
    Version Version  `json:"-"`
    VersionRaw string `json:"version"`
    Output  string   `json:"output"`  // redacted stdout+stderr, bounded
    Errors  []string `json:"errors"`
}

// Check runs `<binary> check -c <config>`. A non-zero exit is not a Go error:
// it returns CheckResult{OK:false} plus an *apperr.Error with CodeConfigCheckFailed.
func Check(ctx context.Context, binaryPath, configPath string, timeout time.Duration) (CheckResult, error)
```

```go
// --- process.go -----------------------------------------------------------------------------
// Foreign reports a sing-box process that SingBoxUI did not start but which is
// running (a manual CLI run, or the legacy Java app's child).
func Foreign(ctx context.Context) ([]int, error)
```

```go
// --- release.go -----------------------------------------------------------------------------
type Asset struct {
    Name        string `json:"name"`
    DownloadURL string `json:"downloadUrl"`
    Size        int64  `json:"size"`
    SHA256      string `json:"sha256"` // from the release API "digest" field
}

type Release struct {
    Version     string    `json:"version"` // "1.14.0", tag without the leading v
    Tag         string    `json:"tag"`     // "v1.14.0"
    Prerelease  bool      `json:"prerelease"`
    Draft       bool      `json:"draft"`
    PublishedAt time.Time `json:"publishedAt"`
    Notes       string    `json:"notes"`
    Assets      []Asset   `json:"assets"`
}

type Client struct{ /* http.Client, base URL, token, logger */ }

func NewClient(httpClient *http.Client, token, userAgent string, logger *slog.Logger) *Client
// LatestStable returns the newest non-draft, non-prerelease release (spec §17).
func (c *Client) LatestStable(ctx context.Context) (Release, error)
// Download streams an asset to dst (same filesystem as the final location),
// returning the SHA-256 of the bytes written and reporting progress in bounded
// steps. Truncated transfers are an error (spec §77).
func (c *Client) Download(ctx context.Context, url, dst string, progress func(done, total int64)) (string, error)

var (
    // ErrNoAssetForPlatform -> BINARY_UNSUPPORTED_PLATFORM. It must be a
    // sentinel value (`var ... = errors.New(...)`), not a const: Go has no
    // constant error values, and callers need `errors.Is`.
    ErrNoAssetForPlatform = errors.New("singbox: no release asset for this platform")
)

// AssetFor picks the exact official asset for the platform, ignoring legacy variants.
func (r Release) AssetFor(goos, goarch string) (Asset, error)
// ExecutableName is "sing-box" on darwin and "sing-box.exe" on windows.
func ExecutableName(goos string) string
```

Verified facts (2025-09, release v1.14.0, re-verify against the live API in tests where cheap):

* asset names are `sing-box-<version>-darwin-arm64.tar.gz`, `sing-box-<version>-darwin-amd64.tar.gz`,
  `sing-box-<version>-windows-amd64.zip`, `sing-box-<version>-windows-arm64.zip`;
  legacy variants (`…-legacy-macos-10.13.tar.gz`, `…-legacy-windows-7.zip`, `windows-386`) must be ignored;
* **the release publishes no checksum file**; the GitHub API returns a per-asset
  `digest: "sha256:<hex>"` field, which is the official equivalent used for verification
  (spec §21). If a checksums file ever exists it is used as a secondary source. ADR 006 records this.

```go
// --- archive.go -----------------------------------------------------------------------------
// ExtractBinary extracts exactly one file (the sing-box executable) from a
// .tar.gz or .zip into destDir. Path traversal, symlinks, absolute paths and
// extra files are refused (spec §21).
func ExtractBinary(archivePath, destDir, executableName string) (string, error)
```

## 2. `internal/platform` implementations

* `internal/platform/darwin` — build tag `darwin`: data dir
  `~/Library/Application Support/SingBoxUI`, LaunchAgent autostart
  (`~/Library/LaunchAgents/com.larffxx.singboxui.plist`), legacy-entry detection of the Java plist,
  `PrivilegeRunner()`.
* `internal/platform/windows` — build tag `windows`: data dir `%LOCALAPPDATA%\SingBoxUI`, registry
  autostart under `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, `PrivilegeRunner()`.
* both expose `New() (platform.Platform, error)`; a build-tagged `platform.New()` in the parent
  package dispatches to them.

## 3. Privileged launch (macOS and Windows)

The UI is never elevated. `privilege.Runner.Start` with `Request.Elevate=true` starts
`singboxui-priv` (`cmd/singboxui-priv`) — a helper whose *only* operations are `start` and `stop` of
one validated sing-box process — through the OS elevation mechanism, and:

* the helper validates that every path is absolute and that the binary is the sing-box executable it
  was asked to run; it accepts no other operation and no free-form arguments;
* the helper writes `PIDPath` (pid + start time) and `StatusPath` (exit code JSON when the child ends)
  and forwards stdout+stderr into `LogPath`;
* `Process.Logs()` for an elevated launch tails `LogPath`; `Wait()` waits on the child of the helper
  through `StatusPath`;
* `Terminate()`/`Kill()` signal the PID from `PIDPath` after verifying it is the same process;
* user cancellation of the elevation prompt maps to `privilege.ErrCancelled` →
  `apperr.CodePrivilegeDenied`.

`cmd/singboxui-priv` is built by the Makefile/CI and shipped next to the main binary (macOS:
`Contents/MacOS/singboxui-priv`; Windows: `singboxui-priv.exe`).

## 4. Fake sing-box test fixture (spec §67)

`internal/singbox/faketest` (package built into a helper binary, `cmd/fakesingbox`, built on demand
by tests with `go build` into `t.TempDir()`), driven by the scenario name in its `--scenario` flag or
the `FAKESINGBOX_SCENARIO` environment variable:

| scenario | behaviour |
|---|---|
| `version` | prints `sing-box version 1.14.0` |
| `version-old` | prints `1.13.0` |
| `version-beta` | prints `1.15.0-beta.1` |
| `version-fail` | prints junk and exits 1 |
| `check-ok` | `check` exits 0 |
| `check-fail` | `check` prints a validation error and exits 1 |
| `run` | prints logs on stdout/stderr, exits on SIGTERM with code 0 |
| `run-slow` | waits 3s before printing its first log and recording readiness |
| `run-crash` | exits 2 after printing one line |
| `run-ignore-term` | ignores SIGTERM and must be killed |
| `run-foreign` | a long-lived process used as a pre-existing sing-box |

Scenarios are selected by an argument (`-scenario run`) or by `FAKESINGBOX_SCENARIO`, so a test can
copy the binary to a path named `sing-box` and exercise the real code path. `run*` scenarios write
their PID and honour `-pid-file`, `-status-file`, `-log-file` so the privileged-helper code path can
be tested without root.

## 5. Application layer (owned by the integrator)

`internal/app/{profiles,config,runtime,binary,traffic,share,settings}` expose use-case types
constructed in `cmd/singboxui/main.go`; they depend on the ports above and on `*sqlite.Store`, never on
`internal/desktop/*`. Wails bindings (`internal/desktop/bindings`) are thin facades over them.

Method names reserved for the bindings (do not rename without updating the frontend):

```text
ProfileAPI : List, Get, Create, Rename, Update, Duplicate, Delete, SetActive, Import, Export, Templates
ConfigAPI  : Draft, Validate, SaveRevision, Apply, Rollback, Revisions, Compare, DeleteRevision?, ImportLegacy
RuntimeAPI : Status, Start, Stop, Restart, Logs, ClearLogs, SaveActiveConfig
BinaryAPI  : Status, CheckForUpdates, Install, SetSource, ProbeCustom, UpdateProgress, AvailableVersion
SettingsAPI: Get, Update, Autostart, SetAutostart, SetAutoConnect
ShareAPI   : Parse, Build, Insert
TrafficAPI : Snapshot, History
```
