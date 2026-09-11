// Package binary owns the sing-box executable: which one is selected, how a
// managed release is verified and installed, and which stable version is
// available upstream (spec §17–§21, §76–§77).
//
// Checking for an update is passive and never installs one; installation is an
// explicit user action, and no unverified artifact is ever executed.
package binary

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/settings"
	"github.com/larffxx/singboxui/internal/events"
	"github.com/larffxx/singboxui/internal/platform"
	"github.com/larffxx/singboxui/internal/singbox"
	"github.com/larffxx/singboxui/internal/storage/sqlite"
)

// checkTimeout bounds a `sing-box check` run.
const checkTimeout = 20 * time.Second

// Store is the persistence this service needs.
type Store interface {
	LoadSettings(ctx context.Context) (settings.Settings, error)
	SaveSettings(ctx context.Context, value settings.Settings) error
	GetManagedBinary(ctx context.Context) (sqlite.ManagedBinary, error)
	SaveManagedBinary(ctx context.Context, value sqlite.ManagedBinary) error
	MarkRevisionsStale(ctx context.Context, version string) (int64, error)
}

// Deps wires the service explicitly; nothing is global (spec §63).
type Deps struct {
	Store   Store
	Paths   platform.Paths
	Emitter events.Emitter
	Logger  *slog.Logger
	// Client talks to the release API. A nil client disables update checks but
	// keeps every local capability working (spec §77).
	Client *singbox.Client
	// Now and Probe are seams for tests.
	Now   func() time.Time
	Probe func(ctx context.Context, path string) (singbox.Version, error)
	// GOOS and GOARCH default to the running platform; tests set them to cover
	// every supported target (spec §19).
	GOOS   string
	GOARCH string
}

// Service is the managed-binary use case set.
type Service struct {
	deps   Deps
	logger *slog.Logger

	mu        sync.Mutex
	probed    map[string]probeEntry
	checking  bool
	lastCheck CheckResult
}

type probeEntry struct {
	version singbox.Version
	err     error
	modTime time.Time
	size    int64
}

// Status is the binary state shown in the UI (spec §20, §53).
type Status struct {
	Source        settings.BinarySource `json:"source"`
	Platform      string                `json:"platform"`
	Unsupported   bool                  `json:"unsupported"`
	ActivePath    string                `json:"activePath"`
	ActiveVersion string                `json:"activeVersion"`
	ActiveOK      bool                  `json:"activeOk"`
	ActiveError   string                `json:"activeError,omitempty"`
	Managed       ManagedInfo           `json:"managed"`
	CustomPath    string                `json:"customPath,omitempty"`
	LastCheck     *time.Time            `json:"lastCheck,omitempty"`
	CheckError    string                `json:"checkError,omitempty"`
	Update        *UpdateInfo           `json:"update,omitempty"`
}

// ManagedInfo describes the installed managed binary.
type ManagedInfo struct {
	Installed   bool       `json:"installed"`
	Version     string     `json:"version,omitempty"`
	Path        string     `json:"path,omitempty"`
	SHA256      string     `json:"sha256,omitempty"`
	InstalledAt *time.Time `json:"installedAt,omitempty"`
}

// UpdateInfo describes an available stable release.
type UpdateInfo struct {
	Version     string    `json:"version"`
	Tag         string    `json:"tag"`
	AssetName   string    `json:"assetName"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	DownloadURL string    `json:"-"`
	PublishedAt time.Time `json:"publishedAt"`
	Notes       string    `json:"notes,omitempty"`
}

// CheckResult is the outcome of a release check (spec §76).
type CheckResult struct {
	CurrentVersion  string      `json:"currentVersion"`
	CheckedAt       time.Time   `json:"checkedAt"`
	Update          *UpdateInfo `json:"update,omitempty"`
	Unsupported     bool        `json:"unsupported"`
	NoInstalled     bool        `json:"noInstalledBinary"`
	DownloadHost    string      `json:"downloadHost,omitempty"`
	UpdateAvailable bool        `json:"updateAvailable"`
}

// InstallResult reports a completed installation.
type InstallResult struct {
	Version       string    `json:"version"`
	Path          string    `json:"path"`
	SHA256        string    `json:"sha256"`
	PreviousPath  string    `json:"previousPath,omitempty"`
	StaleRevision int64     `json:"staleRevisions"`
	Installed     time.Time `json:"installedAt"`
	Status        Status    `json:"status"`
}

// New builds the service.
func New(deps Deps) *Service {
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	if deps.Probe == nil {
		deps.Probe = singbox.Probe
	}
	if deps.GOOS == "" {
		deps.GOOS = runtime.GOOS
	}
	if deps.GOARCH == "" {
		deps.GOARCH = runtime.GOARCH
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{deps: deps, logger: logger, probed: map[string]probeEntry{}}
}

// Path resolves the executable the runtime must launch (spec §20). PATH is never
// consulted implicitly: only the managed binary or the configured custom one.
func (s *Service) Path(ctx context.Context) (string, error) {
	values, err := s.deps.Store.LoadSettings(ctx)
	if err != nil {
		return "", err
	}
	managed, err := s.deps.Store.GetManagedBinary(ctx)
	if err != nil {
		return "", err
	}
	return singbox.Locate(managedRef(managed), values, s.deps.Paths)
}

// managedRef narrows the storage record to the view the adapter needs.
func managedRef(record sqlite.ManagedBinary) singbox.ManagedBinaryRef {
	return singbox.ManagedBinaryRef{Version: record.Version, Path: record.Path}
}

// Version probes the selected executable, caching the result while the file is
// unchanged on disk.
func (s *Service) Version(ctx context.Context) (singbox.Version, error) {
	path, err := s.Path(ctx)
	if err != nil {
		return singbox.Version{}, err
	}
	return s.VersionOf(ctx, path)
}

// VersionOf probes one executable by absolute path.
func (s *Service) VersionOf(ctx context.Context, path string) (singbox.Version, error) {
	info, statErr := os.Stat(path)
	if statErr != nil {
		return singbox.Version{}, apperr.Wrap(apperr.CodeBinaryNotFound, "binary.VersionOf",
			"the sing-box executable is missing or not readable", statErr)
	}
	s.mu.Lock()
	if entry, ok := s.probed[path]; ok && entry.modTime.Equal(info.ModTime()) && entry.size == info.Size() {
		s.mu.Unlock()
		return entry.version, entry.err
	}
	s.mu.Unlock()

	version, probeErr := s.deps.Probe(ctx, path)
	s.mu.Lock()
	s.probed[path] = probeEntry{version: version, err: probeErr, modTime: info.ModTime(), size: info.Size()}
	s.mu.Unlock()
	return version, probeErr
}

// Check validates a candidate configuration with the selected binary
// (spec §37). It never persists anything.
func (s *Service) Check(ctx context.Context, configPath string) (singbox.CheckResult, error) {
	path, err := s.Path(ctx)
	if err != nil {
		return singbox.CheckResult{}, err
	}
	return singbox.Check(ctx, path, configPath, checkTimeout)
}

// Status renders the current binary state. A missing or broken binary is
// reported in the payload instead of failing the call, because it must still be
// visible in the UI.
func (s *Service) Status(ctx context.Context) (Status, error) {
	values, err := s.deps.Store.LoadSettings(ctx)
	if err != nil {
		return Status{}, err
	}
	managed, err := s.deps.Store.GetManagedBinary(ctx)
	if err != nil {
		return Status{}, err
	}
	status := Status{
		Source:     values.BinarySource,
		Platform:   s.deps.GOOS + "/" + s.deps.GOARCH,
		CustomPath: values.CustomBinaryPath,
		Managed: ManagedInfo{
			Installed:   !managed.Empty(),
			Version:     managed.Version,
			Path:        managed.Path,
			SHA256:      managed.SHA256,
			InstalledAt: managed.InstalledAt,
		},
	}
	if !platformSupported(s.deps.GOOS, s.deps.GOARCH) {
		status.Unsupported = true
	}
	path, locateErr := singbox.Locate(managedRef(managed), values, s.deps.Paths)
	if locateErr != nil {
		status.ActiveError = apperr.MessageOf(locateErr)
		return status, nil
	}
	status.ActivePath = path
	version, probeErr := s.VersionOf(ctx, path)
	if probeErr != nil {
		status.ActiveError = apperr.MessageOf(probeErr)
		return status, nil
	}
	status.ActiveVersion = version.String()
	status.ActiveOK = true
	if managed.Version != "" && version.String() != managed.Version {
		status.ActiveError = "the installed binary reports " + version.String() +
			" but version " + managed.Version + " was installed"
	}
	return status, nil
}

// SetSource switches between the managed and the custom executable (spec §20).
// A custom path is probed before it is accepted.
func (s *Service) SetSource(ctx context.Context, source settings.BinarySource, customPath string) (Status, error) {
	const op = "binary.SetSource"
	if !source.Valid() {
		return Status{}, apperr.New(apperr.CodeSettingsInvalid, op, "unknown binary source")
	}
	values, err := s.deps.Store.LoadSettings(ctx)
	if err != nil {
		return Status{}, err
	}
	values.BinarySource = source
	if source == settings.BinaryCustom {
		cleaned := strings.TrimSpace(customPath)
		if cleaned == "" {
			return Status{}, apperr.New(apperr.CodeSettingsInvalid, op,
				"a path to a sing-box executable is required for a custom binary")
		}
		if !singbox.IsExecutable(cleaned) {
			return Status{}, apperr.New(apperr.CodeBinaryNotFound, op,
				"the selected file is not an executable sing-box binary")
		}
		if _, err := s.VersionOf(ctx, cleaned); err != nil {
			return Status{}, err
		}
		values.CustomBinaryPath = cleaned
	} else {
		values.CustomBinaryPath = ""
	}
	if err := values.Validate(); err != nil {
		return Status{}, apperr.Wrap(apperr.CodeSettingsInvalid, op, "the settings were rejected", err)
	}
	if err := s.deps.Store.SaveSettings(ctx, values); err != nil {
		return Status{}, err
	}
	if source == settings.BinaryManaged {
		s.mu.Lock()
		s.probed = map[string]probeEntry{}
		s.mu.Unlock()
	}
	return s.Status(ctx)
}

// CheckForUpdate asks upstream for the newest stable release and compares it
// with the installed version. It never downloads or installs (spec §76, §77).
func (s *Service) CheckForUpdate(ctx context.Context) (CheckResult, error) {
	const op = "binary.CheckForUpdate"
	if s.deps.Client == nil {
		return CheckResult{}, apperr.New(apperr.CodeBinaryUpdateCheckFailed, op, "no release client is configured")
	}
	s.mu.Lock()
	if s.checking {
		s.mu.Unlock()
		return CheckResult{}, apperr.New(apperr.CodeBinaryUpdateCheckFailed, op, "a release check is already running")
	}
	s.checking = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.checking = false
		s.mu.Unlock()
	}()

	release, err := s.deps.Client.LatestStable(ctx)
	if err != nil {
		return CheckResult{}, err
	}
	result := CheckResult{CheckedAt: s.deps.Now()}
	if release.Prerelease || release.Draft {
		// Defensive: LatestStable filters, and a regression here must not offer a
		// pre-release build (spec §17).
		return result, apperr.New(apperr.CodeBinaryUpdateCheckFailed, op,
			"the release feed returned a pre-release build")
	}
	asset, assetErr := release.AssetFor(s.deps.GOOS, s.deps.GOARCH)
	if assetErr != nil {
		result.Unsupported = true
		return result, nil
	}
	result.DownloadHost = hostOf(asset.DownloadURL)

	managed, err := s.deps.Store.GetManagedBinary(ctx)
	if err != nil {
		return result, err
	}
	result.CurrentVersion = managed.Version
	if managed.Version == "" {
		result.NoInstalled = true
	}
	if newerRelease(managed.Version, release.Version) {
		result.Update = &UpdateInfo{
			Version:     release.Version,
			Tag:         release.Tag,
			AssetName:   asset.Name,
			Size:        asset.Size,
			SHA256:      asset.SHA256,
			DownloadURL: asset.DownloadURL,
			PublishedAt: release.PublishedAt,
			Notes:       release.Notes,
		}
		result.UpdateAvailable = true
	}
	s.emit(events.BinaryProgress, map[string]any{
		"stage":     "check",
		"current":   result.CurrentVersion,
		"available": versionOfUpdate(result.Update),
		"checkedAt": result.CheckedAt,
	})
	s.mu.Lock()
	s.lastCheck = result
	s.mu.Unlock()
	return result, nil
}

// LastCheck returns the cached result of the most recent successful check.
func (s *Service) LastCheck() (CheckResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastCheck.CheckedAt.IsZero() {
		return CheckResult{}, false
	}
	return s.lastCheck, true
}

// InstallStableUpdate downloads, verifies and installs the newest stable release
// (spec §18, §21). The previous binary is kept until the new one passed every
// verification, so a failed installation rolls back.
func (s *Service) InstallStableUpdate(ctx context.Context) (InstallResult, error) {
	const op = "binary.InstallStableUpdate"
	if s.deps.Client == nil {
		return InstallResult{}, apperr.New(apperr.CodeBinaryUpdateCheckFailed, op, "no release client is configured")
	}
	check, err := s.CheckForUpdate(ctx)
	if err != nil {
		return InstallResult{}, err
	}
	if check.Update == nil {
		return InstallResult{}, apperr.New(apperr.CodeBinaryUpdateUnavailable, op,
			"the installed binary is already the newest stable release")
	}
	update := *check.Update
	if strings.TrimSpace(update.SHA256) == "" {
		return InstallResult{}, apperr.New(apperr.CodeBinaryChecksumFailed, op,
			"the release published no checksum for this asset, so it cannot be verified")
	}
	if err := ensureDir(s.deps.Paths.BinDir); err != nil {
		return InstallResult{}, err
	}
	// The download happens in a private directory; the final step is a rename
	// inside the bin directory (spec §15, §21).
	workDir, err := os.MkdirTemp(s.deps.Paths.TempDir, "singbox-download-")
	if err != nil {
		return InstallResult{}, apperr.Wrap(apperr.CodeBinaryDownloadFailed, op,
			"a temporary directory for the download could not be created", err)
	}
	defer os.RemoveAll(workDir)

	archivePath := filepath.Join(workDir, update.AssetName)
	s.emit(events.BinaryProgress, map[string]any{"stage": "download", "version": update.Version, "percent": 0})
	var lastPercent = -1
	digest, err := s.deps.Client.Download(ctx, update.DownloadURL, archivePath, func(done, total int64) {
		percent := percentOf(done, total)
		if percent != lastPercent {
			lastPercent = percent
			s.emit(events.BinaryProgress, map[string]any{"stage": "download", "version": update.Version, "percent": percent})
		}
	})
	if err != nil {
		return InstallResult{}, err
	}
	if digest == "" {
		digest, err = fileSHA256(archivePath)
		if err != nil {
			return InstallResult{}, err
		}
	}
	if !strings.EqualFold(digest, update.SHA256) {
		return InstallResult{}, apperr.New(apperr.CodeBinaryChecksumFailed, op,
			"the downloaded archive does not match the checksum published by GitHub")
	}
	s.emit(events.BinaryProgress, map[string]any{"stage": "extract", "version": update.Version})
	staging := filepath.Join(s.deps.Paths.BinDir, "staging-"+update.Version)
	if err := os.RemoveAll(staging); err != nil {
		return InstallResult{}, apperr.Wrap(apperr.CodeBinaryInstallFailed, op, "the staging directory could not be cleared", err)
	}
	if err := ensureDir(staging); err != nil {
		return InstallResult{}, err
	}
	extracted, err := singbox.ExtractBinary(archivePath, staging, singbox.ExecutableName(s.deps.GOOS))
	if err != nil {
		return InstallResult{}, err
	}
	s.emit(events.BinaryProgress, map[string]any{"stage": "verify", "version": update.Version})
	probed, err := s.deps.Probe(ctx, extracted)
	if err != nil {
		return InstallResult{}, err
	}
	expected, err := singbox.ParseVersion(update.Version)
	if err != nil {
		return InstallResult{}, err
	}
	if probed.Compare(expected) != 0 {
		return InstallResult{}, apperr.New(apperr.CodeBinaryInstallFailed, op,
			"the downloaded binary reports "+probed.String()+" instead of "+update.Version)
	}

	s.emit(events.BinaryProgress, map[string]any{"stage": "install", "version": update.Version})
	previous, err := s.deps.Store.GetManagedBinary(ctx)
	if err != nil {
		return InstallResult{}, err
	}
	// Releases live in per-version directories, so the previous binary stays on
	// disk as the rollback target (spec §18).
	executable := singbox.ExecutableName(s.deps.GOOS)
	target := s.deps.Paths.ManagedBinaryPath(update.Version, executable)
	versionDir := filepath.Dir(target)
	if err := ensureDir(versionDir); err != nil {
		return InstallResult{}, err
	}
	// Re-installing the same version overwrites the file, so it is preserved
	// next to the new one until the installation is confirmed.
	previousPath := ""
	if previous.Version == update.Version {
		if _, statErr := os.Stat(target); statErr == nil {
			previousPath = filepath.Join(versionDir, "sing-box.previous")
			if err := copyFile(target, previousPath); err != nil {
				return InstallResult{}, apperr.Wrap(apperr.CodeBinaryInstallFailed, op,
					"the binary being replaced could not be preserved", err)
			}
		}
	}
	if err := moveFile(extracted, target); err != nil {
		return InstallResult{}, apperr.Wrap(apperr.CodeBinaryInstallFailed, op, "the new binary could not be installed", err)
	}
	if err := os.Chmod(target, 0o755); err != nil {
		s.rollbackBinary(ctx, previous, target, previousPath)
		return InstallResult{}, apperr.Wrap(apperr.CodeBinaryInstallFailed, op, "the new binary could not be made executable", err)
	}
	if _, err := s.deps.Probe(ctx, target); err != nil {
		s.rollbackBinary(ctx, previous, target, previousPath)
		return InstallResult{}, err
	}
	os.RemoveAll(staging)

	record := sqlite.ManagedBinary{
		Version:         update.Version,
		Path:            target,
		AssetName:       update.AssetName,
		SHA256:          strings.ToLower(digest),
		InstalledAt:     ptr(s.deps.Now()),
		PreviousPath:    previousInstalledPath(previous, previousPath),
		PreviousVersion: previous.Version,
	}
	if err := s.deps.Store.SaveManagedBinary(ctx, record); err != nil {
		s.rollbackBinary(ctx, previous, target, previousPath)
		return InstallResult{}, err
	}
	stale, err := s.deps.Store.MarkRevisionsStale(ctx, update.Version)
	if err != nil {
		return InstallResult{}, err
	}
	s.mu.Lock()
	s.probed = map[string]probeEntry{}
	s.mu.Unlock()
	status, err := s.Status(ctx)
	if err != nil {
		return InstallResult{}, err
	}
	s.emit(events.BinaryProgress, map[string]any{"stage": "done", "version": update.Version, "percent": 100})
	return InstallResult{
		Version:       update.Version,
		Path:          target,
		SHA256:        record.SHA256,
		PreviousPath:  previousPath,
		StaleRevision: stale,
		Installed:     *record.InstalledAt,
		Status:        status,
	}, nil
}

// rollbackBinary restores the previous executable after a failed installation
// (spec §18).
//
// The previous release normally lives in its own version directory and needs no
// restoration; only a re-install of the same version has to put the preserved
// file back. The failed file is removed either way so the layout never contains
// a binary that failed verification.
func (s *Service) rollbackBinary(ctx context.Context, previous sqlite.ManagedBinary, target, previousPath string) {
	if previousPath != "" {
		if err := moveFile(previousPath, target); err != nil {
			s.logger.Error("the preserved sing-box binary could not be restored", "error", err)
		}
	} else if previous.Version != "" && !strings.HasPrefix(target, previous.Path) {
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			s.logger.Warn("the failed sing-box binary could not be removed", "error", err)
		}
	}
	if previous.Path == "" {
		return
	}
	if err := s.deps.Store.SaveManagedBinary(ctx, previous); err != nil {
		s.logger.Error("the managed binary record could not be restored", "error", err)
	}
}

// previousInstalledPath records where the replaced binary stays available for a
// manual rollback.
func previousInstalledPath(previous sqlite.ManagedBinary, preserved string) string {
	if preserved != "" {
		return preserved
	}
	return previous.Path
}

// platformSupported mirrors the platforms upstream publishes binaries for
// (spec §19). It is used for the offline status report; the release check uses
// the actual asset list.
func platformSupported(goos, goarch string) bool {
	if goarch != "amd64" && goarch != "arm64" {
		return false
	}
	return goos == "darwin" || goos == "windows"
}

// newerRelease reports whether candidate is a newer version than current.
func newerRelease(current, candidate string) bool {
	if strings.TrimSpace(candidate) == "" {
		return false
	}
	candidateVersion, err := singbox.ParseVersion(candidate)
	if err != nil {
		return false
	}
	currentVersion, err := singbox.ParseVersion(current)
	if err != nil {
		// Nothing installed, or an unparsable record: any stable release is an
		// improvement.
		return true
	}
	return candidateVersion.Compare(currentVersion) > 0
}

func versionOfUpdate(update *UpdateInfo) string {
	if update == nil {
		return ""
	}
	return update.Version
}

func (s *Service) emit(name string, payload any) {
	if s.deps.Emitter != nil {
		s.deps.Emitter.Emit(name, payload)
	}
}
