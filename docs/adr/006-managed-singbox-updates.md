# ADR 006 — Managed sing-box updates

## Context

The legacy app downloaded "the latest" sing-box with no checksum, no stable-channel filter, no
extraction safety and no rollback, then ran it from `./bin/`. The specification requires: managed and
custom sources with no implicit PATH fallback; stable (non-draft, non-prerelease) releases only;
SHA-256 or the official equivalent verification; path-traversal-safe extraction; version probing;
atomic install with the previous binary kept for rollback; no silent auto-upgrades; and full
usability offline with the already-installed binary (§17–§21, §76, §77).

## Decision

Own the binary channel in `internal/app/binary` over `internal/singbox.Client`.

* **Stable only.** `Client.LatestStable` selects the newest GitHub release that is neither draft nor
  prerelease; prereleases are never installed (§17). Asset selection is explicit per GOOS/GOARCH
  (`sing-box-<version>-<os>-<arch>.tar.gz|.zip`), ignoring legacy/386/win7/macos-10.13 variants.
* **Verify before executing.** The release publishes no checksum file; the GitHub API's per-asset
  `digest: "sha256:<hex>"` is the official equivalent used for verification, with a checksums file
  used as a secondary source if one ever appears (§21). A mismatch aborts with
  `BINARY_CHECKSUM_FAILED`.
* **Safe extraction.** `ExtractBinary` extracts exactly the one sing-box executable from a `.tar.gz`
  or `.zip`; path traversal, symlinks, absolute paths and extra entries are refused.
* **Probe, then atomically install.** The downloaded binary must report the expected version; it is
  installed by atomic rename into `<dataDir>/bin/<version>/`, with the previous binary and version
  retained in the `managed_binary` row for rollback. Failed verification rolls back.
* **Checking and installing are separate operations** (`CheckForUpdate`, `GetAvailableStableVersion`,
  `InstallUpdate`); opening Settings never installs anything, and installation requires explicit user
  action (§76). Checking automatically is allowed.
* **Offline is first-class.** No network, rate limits, timeouts, partial downloads or corrupt
  archives surface as typed errors (`BINARY_DOWNLOAD_FAILED`, `NETWORK_UNAVAILABLE`); the installed
  binary keeps working and is never replaced with a partial file.
* **Source is explicit and persisted.** `Managed` (default) or `Custom` path; PATH is never an
  implicit source (§20).

## Alternatives

* **Unverified "latest" download (legacy)** — forbids executing unverified artifacts per §21 and §83.
  Rejected.
* **Trusting the archive without probing the binary** — file names lie; probing `version` is required
  (§21, §24). Rejected.
* **Auto-upgrading silently on check** — forbidden (§18, §83). Rejected.
* **Vendoring a sing-box binary into the repository or shipping it in the installer** — couples the
  app release to the sing-box release, bloats artifacts (~83 MB per platform) and complicates
  licensing; the spec mandates a managed, user-approved download. Rejected.
* **Relying on Homebrew / winget / PATH** — no auto-install and no pinned version; PATH fallback is
  explicitly forbidden. Rejected (§20).
* **A checksums file as the sole verifier** — it does not exist in the official releases; the API
  digest is used instead (recorded here because the spec assumed a checksum file).

## Consequences

* Updating sing-box is a verified, user-initiated, reversible operation.
* Every revision records the sing-box version that validated it; after a version change, validation
  may be marked stale and re-checked before apply/start (§38).
* CI and tests never need network: version parsing, stable selection, checksum mismatch, extraction
  safety and rollback are exercised against fixtures and the fake binary (ADR 009).
