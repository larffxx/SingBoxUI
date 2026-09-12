package binary

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/settings"
	"github.com/larffxx/singboxui/internal/events"
	"github.com/larffxx/singboxui/internal/platform"
	"github.com/larffxx/singboxui/internal/singbox"
	"github.com/larffxx/singboxui/internal/storage/sqlite"
)

// These tests drive the managed-binary use cases against a local release API and
// a real archive: asset selection per platform, checksum verification, and the
// rule that nothing unverified ever replaces the installed binary (spec §17–§21,
// §76–§77). No test reaches the network and none needs a real sing-box.

// ---------------------------------------------------------------- fake store

type fakeStore struct {
	mu      sync.Mutex
	values  settings.Settings
	managed sqlite.ManagedBinary
	saved   []sqlite.ManagedBinary
	marked  []string

	loadErr   error
	saveErr   error
	markErr   error
	markCount int64
}

func (f *fakeStore) LoadSettings(context.Context) (settings.Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.values, f.loadErr
}

func (f *fakeStore) SaveSettings(_ context.Context, value settings.Settings) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return f.saveErr
	}
	f.values = value
	return nil
}

func (f *fakeStore) GetManagedBinary(context.Context) (sqlite.ManagedBinary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.managed, nil
}

func (f *fakeStore) SaveManagedBinary(_ context.Context, value sqlite.ManagedBinary) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, value)
	f.managed = value
	return nil
}

func (f *fakeStore) MarkRevisionsStale(_ context.Context, version string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.markErr != nil {
		return 0, f.markErr
	}
	f.marked = append(f.marked, version)
	return f.markCount, nil
}

// ------------------------------------------------------------- fake upstream

type ghAsset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
	State              string `json:"state"`
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Body        string    `json:"body"`
	Assets      []ghAsset `json:"assets"`
}

type assetBody struct {
	name   string
	body   []byte
	digest string
	omit   bool
}

type fakeUpstream struct {
	mu        sync.Mutex
	releases  []ghRelease
	bodies    map[string]assetBody
	downloads int
	apiStatus int
	dlStatus  int
	truncate  bool
}

func (u *fakeUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	defer u.mu.Unlock()
	switch {
	case strings.HasPrefix(r.URL.Path, "/repos/"):
		if u.apiStatus != 0 {
			w.WriteHeader(u.apiStatus)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(u.releases)
	case strings.HasPrefix(r.URL.Path, "/dl/"):
		u.downloads++
		if u.dlStatus != 0 {
			w.WriteHeader(u.dlStatus)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/dl/")
		asset, ok := u.bodies[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if u.truncate {
			// Promise more than is sent: the client must refuse a short read.
			w.Header().Set("Content-Length", "4096")
			_, _ = w.Write(asset.body)
			return
		}
		_, _ = w.Write(asset.body)
	default:
		http.NotFound(w, r)
	}
}

// ------------------------------------------------------------------- harness

type harness struct {
	svc      *Service
	store    *fakeStore
	paths    platform.Paths
	upstream *fakeUpstream
	server   *httptest.Server
	emitted  *recorder

	probeVersion singbox.Version
	probeErr     error
	probePaths   []string

	now time.Time
}

type recorder struct {
	mu       sync.Mutex
	names    []string
	payloads []any
}

func (r *recorder) Emit(name string, payload any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.names = append(r.names, name)
	r.payloads = append(r.payloads, payload)
}

// stages returns the deduplicated progress stages in the order they were
// emitted; the download stage legitimately repeats as bytes arrive.
func (r *recorder) stages(t *testing.T) []string {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for i, name := range r.names {
		if name != events.BinaryProgress {
			continue
		}
		payload, ok := r.payloads[i].(map[string]any)
		if !ok {
			t.Fatalf("progress payload %#v is not a map", r.payloads[i])
		}
		stage, _ := payload["stage"].(string)
		if len(out) == 0 || out[len(out)-1] != stage {
			out = append(out, stage)
		}
	}
	return out
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	paths := platform.Paths{
		DataDir:   root,
		DBPath:    filepath.Join(root, "state.db"),
		LogPath:   filepath.Join(root, "app.log"),
		BinDir:    filepath.Join(root, "bin"),
		TempDir:   filepath.Join(root, "tmp"),
		ConfigDir: filepath.Join(root, "config"),
	}
	for _, dir := range []string{paths.BinDir, paths.TempDir, paths.ConfigDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("cannot create %s: %v", dir, err)
		}
	}

	upstream := &fakeUpstream{bodies: map[string]assetBody{}}
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)

	h := &harness{
		store:    &fakeStore{values: settings.Default()},
		paths:    paths,
		upstream: upstream,
		server:   server,
		emitted:  &recorder{},
		now:      time.Date(2026, 5, 4, 3, 2, 1, 0, time.UTC),
	}
	client := singbox.NewClient(server.Client(), "", "SingBoxUI-test", nil)
	client.BaseURL = server.URL
	client.Repository = "SagerNet/sing-box"

	h.probeVersion = mustVersion(t, "1.15.0")
	h.svc = New(Deps{
		Store:   h.store,
		Paths:   paths,
		Emitter: h.emitted,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Client:  client,
		Now:     func() time.Time { return h.now },
		Probe: func(_ context.Context, path string) (singbox.Version, error) {
			h.probePaths = append(h.probePaths, path)
			if h.probeErr != nil {
				return singbox.Version{}, h.probeErr
			}
			return h.probeVersion, nil
		},
		GOOS:   "darwin",
		GOARCH: "arm64",
	})
	return h
}

// publish registers releases and their downloadable bodies.
func (h *harness) publish(t *testing.T, releases []ghRelease, bodies ...assetBody) {
	t.Helper()
	h.upstream.mu.Lock()
	defer h.upstream.mu.Unlock()
	for _, body := range bodies {
		h.upstream.bodies[body.name] = body
	}
	for i := range releases {
		for j := range releases[i].Assets {
			asset := &releases[i].Assets[j]
			body, ok := h.upstream.bodies[asset.Name]
			if !ok {
				continue
			}
			if asset.Size == 0 {
				asset.Size = int64(len(body.body))
			}
			if asset.BrowserDownloadURL == "" {
				asset.BrowserDownloadURL = h.server.URL + "/dl/" + asset.Name
			}
			if !body.omit && body.digest == "" {
				asset.Digest = "sha256:" + digestOf(body.body)
			} else if body.digest != "" {
				asset.Digest = body.digest
			}
			asset.State = "uploaded"
		}
	}
	h.upstream.releases = releases
}

func (h *harness) release(version string, prerelease bool, names ...string) ghRelease {
	assets := make([]ghAsset, 0, len(names))
	for _, name := range names {
		assets = append(assets, ghAsset{Name: name, BrowserDownloadURL: h.server.URL + "/dl/" + name})
	}
	return ghRelease{
		TagName:     "v" + version,
		Prerelease:  prerelease,
		PublishedAt: h.now.Add(-24 * time.Hour),
		Body:        "release notes for " + version,
		Assets:      assets,
	}
}

func (h *harness) downloads(t *testing.T) int {
	t.Helper()
	h.upstream.mu.Lock()
	defer h.upstream.mu.Unlock()
	return h.upstream.downloads
}

func (h *harness) record(t *testing.T) sqlite.ManagedBinary {
	t.Helper()
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	return h.store.managed
}

func (h *harness) setManagedBinary(t *testing.T, version, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("cannot create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatalf("cannot write %s: %v", path, err)
	}
	installedAt := h.now.Add(-48 * time.Hour)
	h.store.mu.Lock()
	h.store.managed = sqlite.ManagedBinary{
		Version: version, Path: path, AssetName: "sing-box-" + version + ".tar.gz",
		SHA256: strings.Repeat("a", 64), InstalledAt: &installedAt,
	}
	h.store.mu.Unlock()
}

// ------------------------------------------------------------------ builders

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func tarGzArchive(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	tw := tar.NewWriter(gz)
	// A stable order keeps the archive (and the digest) deterministic.
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	for _, name := range names {
		body := entries[name]
		header := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if strings.HasSuffix(name, "/") {
			header = &tar.Header{Name: name, Mode: 0o755, Typeflag: tar.TypeDir}
			body = nil
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatalf("tar header %s: %v", name, err)
		}
		if len(body) > 0 {
			if _, err := tw.Write(body); err != nil {
				t.Fatalf("tar body %s: %v", name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("closing the tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("closing the gzip writer: %v", err)
	}
	return buffer.Bytes()
}

func mustVersion(t *testing.T, raw string) singbox.Version {
	t.Helper()
	version, err := singbox.ParseVersion(raw)
	if err != nil {
		t.Fatalf("singbox.ParseVersion(%q) = %v", raw, err)
	}
	return version
}

func wantCode(t *testing.T, err error, code apperr.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("error is nil, want %s", code)
	}
	if got := apperr.CodeOf(err); got != code {
		t.Fatalf("error code = %s (%v), want %s", got, err, code)
	}
}

// ------------------------------------------------------------ asset selection

func TestCheckForUpdatePicksTheAssetForTheTargetPlatform(t *testing.T) {
	const version = "1.15.0"
	tests := []struct {
		name      string
		goos      string
		goarch    string
		wantAsset string
		wantUnsup bool
	}{
		{name: "darwin arm64", goos: "darwin", goarch: "arm64", wantAsset: "sing-box-1.15.0-darwin-arm64.tar.gz"},
		{name: "darwin amd64", goos: "darwin", goarch: "amd64", wantAsset: "sing-box-1.15.0-darwin-amd64.tar.gz"},
		{name: "windows amd64", goos: "windows", goarch: "amd64", wantAsset: "sing-box-1.15.0-windows-amd64.zip"},
		{name: "unsupported linux", goos: "linux", goarch: "amd64", wantUnsup: true},
		{name: "unsupported windows 386", goos: "windows", goarch: "386", wantUnsup: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			h.svc.deps.GOOS, h.svc.deps.GOARCH = test.goos, test.goarch
			h.setManagedBinary(t, "1.14.0", filepath.Join(h.paths.BinDir, "1.14.0", "sing-box"), []byte("managed 1.14.0"))

			// The release carries the real assets plus the legacy builds that
			// share a prefix with them (spec §21).
			names := []string{
				"sing-box-1.15.0-darwin-arm64.tar.gz",
				"sing-box-1.15.0-darwin-amd64.tar.gz",
				"sing-box-1.15.0-darwin-amd64-legacy-macos-10.13.tar.gz",
				"sing-box-1.15.0-windows-amd64.zip",
				"sing-box-1.15.0-windows-386.zip",
				"sing-box-1.15.0-windows-amd64-legacy-windows-7.zip",
				"sing-box-1.15.0-linux-amd64.tar.gz",
			}
			bodies := make([]assetBody, 0, len(names))
			for _, name := range names {
				bodies = append(bodies, assetBody{name: name, body: []byte("archive for " + name)})
			}
			h.publish(t, []ghRelease{h.release(version, false, names...)}, bodies...)

			result, err := h.svc.CheckForUpdate(context.Background())
			if err != nil {
				t.Fatalf("CheckForUpdate() = %v, want nil", err)
			}
			if test.wantUnsup {
				if !result.Unsupported {
					t.Errorf("Unsupported = false, want true for %s/%s", test.goos, test.goarch)
				}
				if result.Update != nil || result.UpdateAvailable {
					t.Errorf("Update = %+v, want none on an unsupported platform", result.Update)
				}
				status, err := h.svc.Status(context.Background())
				if err != nil {
					t.Fatalf("Status() = %v", err)
				}
				if !status.Unsupported || status.Platform != test.goos+"/"+test.goarch {
					t.Errorf("Status = %+v, want Unsupported on %s/%s", status, test.goos, test.goarch)
				}
				return
			}
			if !result.UpdateAvailable || result.Update == nil {
				t.Fatalf("CheckForUpdate() = %+v, want an available update", result)
			}
			if result.Update.AssetName != test.wantAsset {
				t.Errorf("asset = %q, want %q", result.Update.AssetName, test.wantAsset)
			}
			if result.Update.Version != version || result.Update.Tag != "v"+version {
				t.Errorf("version/tag = %q/%q, want %s", result.Update.Version, result.Update.Tag, version)
			}
			if result.Update.SHA256 == "" {
				t.Error("the update carries no checksum; the installer would have to refuse it")
			}
			// Only the host is reported, never the full URL with its query.
			if result.DownloadHost != strings.TrimPrefix(h.server.URL, "http://") {
				t.Errorf("DownloadHost = %q, want the asset host only", result.DownloadHost)
			}
			if got := result.CurrentVersion; got != "1.14.0" {
				t.Errorf("CurrentVersion = %q, want 1.14.0", got)
			}
			if got := h.emitted.stages(t); len(got) != 1 || got[0] != "check" {
				t.Errorf("progress stages = %v, want a single check stage", got)
			}
			if cached, ok := h.svc.LastCheck(); !ok || cached.Update == nil || cached.Update.AssetName != test.wantAsset {
				t.Errorf("LastCheck() = %+v (%v), want the reported result cached", cached, ok)
			}
		})
	}
}

func TestCheckForUpdateIgnoresPrereleasesAndOlderReleaseLines(t *testing.T) {
	h := newHarness(t)
	h.setManagedBinary(t, "1.14.0", filepath.Join(h.paths.BinDir, "1.14.0", "sing-box"), []byte("managed 1.14.0"))

	const asset = "sing-box-1.15.0-darwin-arm64.tar.gz"
	// A late backport of an older line is newer by publication time but must
	// never be offered as an update (spec §17).
	backport := h.release("1.12.5", false, asset)
	backport.PublishedAt = h.now
	prerelease := h.release("2.0.0", true, asset)
	prerelease.TagName = "v2.0.0-beta.1"
	stable := h.release("1.15.0", false, asset)
	h.publish(t, []ghRelease{backport, prerelease, stable}, assetBody{name: asset, body: []byte("1.15.0 archive")})

	result, err := h.svc.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate() = %v, want nil", err)
	}
	if result.Update == nil || result.Update.Version != "1.15.0" {
		t.Fatalf("Update = %+v, want the newest stable release 1.15.0", result.Update)
	}
	if result.CurrentVersion != "1.14.0" || result.NoInstalled {
		t.Errorf("current = %q (noInstalled %v), want the installed 1.14.0", result.CurrentVersion, result.NoInstalled)
	}
}

func TestCheckForUpdateReportsNoUpdateForADowngrade(t *testing.T) {
	h := newHarness(t)
	h.setManagedBinary(t, "1.16.0", filepath.Join(h.paths.BinDir, "1.16.0", "sing-box"), []byte("managed 1.16.0"))
	const asset = "sing-box-1.15.0-darwin-arm64.tar.gz"
	h.publish(t, []ghRelease{h.release("1.15.0", false, asset)}, assetBody{name: asset, body: []byte("1.15.0 archive")})

	result, err := h.svc.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate() = %v, want nil", err)
	}
	if result.Update != nil || result.UpdateAvailable {
		t.Errorf("Update = %+v, want none: 1.15.0 is older than the installed 1.16.0", result.Update)
	}
	if result.CurrentVersion != "1.16.0" {
		t.Errorf("CurrentVersion = %q, want 1.16.0", result.CurrentVersion)
	}
	// Installing anyway must be refused rather than downgrading silently.
	_, err = h.svc.InstallStableUpdate(context.Background())
	wantCode(t, err, apperr.CodeBinaryUpdateUnavailable)
}

func TestCheckForUpdateWithoutAnInstalledBinary(t *testing.T) {
	h := newHarness(t)
	const asset = "sing-box-1.15.0-darwin-arm64.tar.gz"
	h.publish(t, []ghRelease{h.release("1.15.0", false, asset)}, assetBody{name: asset, body: []byte("1.15.0 archive")})

	result, err := h.svc.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate() = %v, want nil", err)
	}
	if !result.NoInstalled {
		t.Error("NoInstalled = false, want true when nothing is installed yet")
	}
	if !result.UpdateAvailable || result.Update == nil || result.Update.Version != "1.15.0" {
		t.Errorf("Update = %+v, want the first install offered", result.Update)
	}
}

func TestCheckForUpdateFailsWhenOnlyPrereleasesExist(t *testing.T) {
	h := newHarness(t)
	const asset = "sing-box-2.0.0-darwin-arm64.tar.gz"
	h.publish(t, []ghRelease{h.release("2.0.0", true, asset)}, assetBody{name: asset, body: []byte("beta archive")})

	_, err := h.svc.CheckForUpdate(context.Background())
	wantCode(t, err, apperr.CodeNotFound)
	if _, ok := h.svc.LastCheck(); ok {
		t.Error("a failed check must not be cached as the last result")
	}
}

func TestCheckForUpdateWithoutAClientIsAnExplicitError(t *testing.T) {
	h := newHarness(t)
	h.svc.deps.Client = nil

	_, err := h.svc.CheckForUpdate(context.Background())
	wantCode(t, err, apperr.CodeBinaryUpdateCheckFailed)
	_, err = h.svc.InstallStableUpdate(context.Background())
	wantCode(t, err, apperr.CodeBinaryUpdateCheckFailed)
}

// ------------------------------------------------------------------ install

func TestInstallStableUpdateInstallsTheVerifiedBinary(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	previous := filepath.Join(h.paths.BinDir, "1.14.0", "sing-box")
	h.setManagedBinary(t, "1.14.0", previous, []byte("managed 1.14.0"))
	h.store.markCount = 3

	const asset = "sing-box-1.15.0-darwin-arm64.tar.gz"
	payload := []byte("#!/bin/sh\necho 'sing-box version 1.15.0'\n")
	archive := tarGzArchive(t, map[string][]byte{
		"sing-box-1.15.0-darwin-arm64/":         nil,
		"sing-box-1.15.0-darwin-arm64/LICENSE":  []byte("GPL-3.0"),
		"sing-box-1.15.0-darwin-arm64/sing-box": payload,
	})
	h.publish(t, []ghRelease{h.release("1.15.0", false, asset)}, assetBody{name: asset, body: archive})

	result, err := h.svc.InstallStableUpdate(ctx)
	if err != nil {
		t.Fatalf("InstallStableUpdate() = %v, want nil", err)
	}

	target := h.paths.ManagedBinaryPath("1.15.0", "sing-box")
	if result.Version != "1.15.0" || result.Path != target {
		t.Errorf("result = %+v, want version 1.15.0 at %s", result, target)
	}
	if result.SHA256 != digestOf(archive) {
		t.Errorf("SHA256 = %q, want the digest of the verified archive", result.SHA256)
	}
	// Releases live in per-version directories, so nothing had to be moved
	// aside: the previous binary stays where it is as the rollback target.
	if result.PreviousPath != "" {
		t.Errorf("PreviousPath = %q, want empty for a cross-version install", result.PreviousPath)
	}
	kept, err := os.ReadFile(previous)
	if err != nil {
		t.Fatalf("the previous release was removed: %v", err)
	}
	if string(kept) != "managed 1.14.0" {
		t.Errorf("previous binary = %q, want it left untouched", kept)
	}
	if result.StaleRevision != 3 {
		t.Errorf("StaleRevision = %d, want 3", result.StaleRevision)
	}
	if !strings.HasPrefix(result.Path, h.paths.BinDir) {
		t.Errorf("installed outside the bin directory: %q", result.Path)
	}
	if result.Status.ActivePath != target || !result.Status.ActiveOK || result.Status.ActiveVersion != "1.15.0" {
		t.Errorf("Status = %+v, want the new binary active", result.Status)
	}

	installed, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("reading the installed binary: %v", err)
	}
	if !bytes.Equal(installed, payload) {
		t.Errorf("installed %q, want the archive's executable %q", installed, payload)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat %s: %v", target, err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("installed mode = %v, want 0755", info.Mode().Perm())
	}
	if current := h.record(t); current.Version != "1.15.0" || current.Path != target {
		t.Errorf("recorded binary = %+v, want the new install", current)
	}
	if current := h.record(t); current.PreviousVersion != "1.14.0" || current.PreviousPath != previous {
		t.Errorf("recorded previous = %q at %q, want the rollback target", current.PreviousVersion, current.PreviousPath)
	}
	if len(h.store.marked) != 1 || h.store.marked[0] != "1.15.0" {
		t.Errorf("MarkRevisionsStale calls = %v, want exactly one for 1.15.0", h.store.marked)
	}
	if stages := h.emitted.stages(t); !equalStrings(stages, []string{"check", "download", "extract", "verify", "install", "done"}) {
		t.Errorf("progress stages = %v, want the documented order", stages)
	}
	// Neither the download work directory nor the staging directory may survive.
	entries, err := os.ReadDir(h.paths.TempDir)
	if err != nil {
		t.Fatalf("reading the temp dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("temp dir holds %v, want no download leftovers", entries)
	}
	staging, err := filepath.Glob(filepath.Join(h.paths.BinDir, "staging-*"))
	if err != nil {
		t.Fatalf("globbing staging dirs: %v", err)
	}
	if len(staging) != 0 {
		t.Errorf("staging directories left behind: %v", staging)
	}
	if _, err := os.Stat(previous); err != nil {
		t.Errorf("the previous binary was removed: %v", err)
	}
}

func TestInstallRefusesAnAssetWithoutAPublishedChecksum(t *testing.T) {
	h := newHarness(t)
	h.setManagedBinary(t, "1.14.0", filepath.Join(h.paths.BinDir, "1.14.0", "sing-box"), []byte("managed 1.14.0"))
	before, err := os.ReadFile(filepath.Join(h.paths.BinDir, "1.14.0", "sing-box"))
	if err != nil {
		t.Fatalf("reading the installed binary: %v", err)
	}

	const asset = "sing-box-1.15.0-darwin-arm64.tar.gz"
	h.publish(t, []ghRelease{h.release("1.15.0", false, asset)}, assetBody{name: asset, body: []byte("archive"), omit: true})

	_, err = h.svc.InstallStableUpdate(context.Background())
	wantCode(t, err, apperr.CodeBinaryChecksumFailed)
	if got := h.downloads(t); got != 0 {
		t.Errorf("downloads = %d, want none: an asset without a checksum must never be fetched", got)
	}
	after, readErr := os.ReadFile(filepath.Join(h.paths.BinDir, "1.14.0", "sing-box"))
	if readErr != nil || !bytes.Equal(before, after) {
		t.Errorf("the installed binary changed: %v", readErr)
	}
	if got := h.record(t); got.Version != "1.14.0" {
		t.Errorf("recorded version = %q, want the untouched 1.14.0", got.Version)
	}
	if len(h.store.saved) != 0 {
		t.Errorf("SaveManagedBinary calls = %d, want none", len(h.store.saved))
	}
}

func TestInstallRefusesAMismatchedChecksum(t *testing.T) {
	h := newHarness(t)
	installedPath := filepath.Join(h.paths.BinDir, "1.14.0", "sing-box")
	h.setManagedBinary(t, "1.14.0", installedPath, []byte("managed 1.14.0"))
	before, err := os.ReadFile(installedPath)
	if err != nil {
		t.Fatalf("reading the installed binary: %v", err)
	}

	const asset = "sing-box-1.15.0-darwin-arm64.tar.gz"
	archive := tarGzArchive(t, map[string][]byte{
		"sing-box-1.15.0-darwin-arm64/sing-box": []byte("tampered payload"),
	})
	// The release claims a digest for different bytes: a substituted download.
	h.publish(t, []ghRelease{h.release("1.15.0", false, asset)},
		assetBody{name: asset, body: archive, digest: "sha256:" + strings.Repeat("7", 64)})

	_, err = h.svc.InstallStableUpdate(context.Background())
	wantCode(t, err, apperr.CodeBinaryChecksumFailed)
	if got := h.downloads(t); got != 1 {
		t.Errorf("downloads = %d, want the one attempted transfer", got)
	}
	if _, err := os.Stat(h.paths.ManagedBinaryPath("1.15.0", "sing-box")); !os.IsNotExist(err) {
		t.Errorf("an unverified binary was placed at the install path (stat err %v)", err)
	}
	after, readErr := os.ReadFile(installedPath)
	if readErr != nil || !bytes.Equal(before, after) {
		t.Errorf("the installed binary changed: %v", readErr)
	}
	if got := h.record(t); got.Version != "1.14.0" {
		t.Errorf("recorded version = %q, want the untouched 1.14.0", got.Version)
	}
	if len(h.store.saved) != 0 {
		t.Errorf("SaveManagedBinary calls = %d, want none for a rejected download", len(h.store.saved))
	}
	staging, err := filepath.Glob(filepath.Join(h.paths.BinDir, "staging-*"))
	if err != nil {
		t.Fatalf("globbing staging dirs: %v", err)
	}
	if len(staging) != 0 {
		t.Errorf("staging directories left behind: %v", staging)
	}
}

func TestInstallRefusesATruncatedDownload(t *testing.T) {
	h := newHarness(t)
	installedPath := filepath.Join(h.paths.BinDir, "1.14.0", "sing-box")
	h.setManagedBinary(t, "1.14.0", installedPath, []byte("managed 1.14.0"))
	before, _ := os.ReadFile(installedPath)

	const asset = "sing-box-1.15.0-darwin-arm64.tar.gz"
	archive := tarGzArchive(t, map[string][]byte{"sing-box-1.15.0-darwin-arm64/sing-box": []byte("payload")})
	h.publish(t, []ghRelease{h.release("1.15.0", false, asset)}, assetBody{name: asset, body: archive})
	h.upstream.mu.Lock()
	h.upstream.truncate = true
	h.upstream.mu.Unlock()

	_, err := h.svc.InstallStableUpdate(context.Background())
	wantCode(t, err, apperr.CodeBinaryDownloadFailed)
	after, readErr := os.ReadFile(installedPath)
	if readErr != nil || !bytes.Equal(before, after) {
		t.Errorf("the installed binary changed: %v", readErr)
	}
}

func TestInstallRollsBackWhenTheProbedVersionDisagrees(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	installedPath := filepath.Join(h.paths.BinDir, "1.14.0", "sing-box")
	h.setManagedBinary(t, "1.14.0", installedPath, []byte("managed 1.14.0"))
	before, err := os.ReadFile(installedPath)
	if err != nil {
		t.Fatalf("reading the installed binary: %v", err)
	}

	const asset = "sing-box-1.15.0-darwin-arm64.tar.gz"
	archive := tarGzArchive(t, map[string][]byte{"sing-box-1.15.0-darwin-arm64/sing-box": []byte("impostor payload")})
	h.publish(t, []ghRelease{h.release("1.15.0", false, asset)}, assetBody{name: asset, body: archive})

	// The archive verifies, but what comes out of it is not the announced release.
	h.probeVersion = mustVersion(t, "1.15.1")

	_, err = h.svc.InstallStableUpdate(ctx)
	wantCode(t, err, apperr.CodeBinaryInstallFailed)
	if !strings.Contains(err.Error(), "1.15.1") || !strings.Contains(err.Error(), "1.15.0") {
		t.Errorf("error %q should report what the binary claimed and what was announced", err)
	}
	if _, statErr := os.Stat(h.paths.ManagedBinaryPath("1.15.0", "sing-box")); !os.IsNotExist(statErr) {
		t.Errorf("a binary that failed verification is still installed (stat err %v)", statErr)
	}
	after, readErr := os.ReadFile(installedPath)
	if readErr != nil || !bytes.Equal(before, after) {
		t.Errorf("the previous binary was damaged: %v", readErr)
	}
	// The failed install must not be recorded, and the record must be restored
	// to the previous release.
	recorded := h.record(t)
	if recorded.Version != "1.14.0" || recorded.Path != installedPath {
		t.Errorf("recorded binary = %+v, want the previous release kept", recorded)
	}
	if len(h.store.marked) != 0 {
		t.Errorf("MarkRevisionsStale calls = %v, want none for a rejected install", h.store.marked)
	}
	if stages := h.emitted.stages(t); contains(stages, "done") {
		t.Errorf("progress stages = %v, want no completed event", stages)
	}
}

// A build that already carries the release version is not reinstalled: the
// service refuses before it touches the filesystem, so a tampered local binary
// cannot be laundered into a "fresh" install by publishing the same version.
func TestInstallingTheSameVersionIsRefused(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	installedPath := filepath.Join(h.paths.BinDir, "1.15.0", "sing-box")
	h.setManagedBinary(t, "1.15.0", installedPath, []byte("the binary that is already installed"))
	previousBytes, err := os.ReadFile(installedPath)
	if err != nil {
		t.Fatalf("reading the installed binary: %v", err)
	}
	before := h.record(t)

	const asset = "sing-box-1.15.0-darwin-arm64.tar.gz"
	archive := tarGzArchive(t, map[string][]byte{"sing-box-1.15.0-darwin-arm64/sing-box": []byte("replacement build")})
	h.publish(t, []ghRelease{h.release("1.15.0", false, asset)}, assetBody{name: asset, body: archive})

	_, err = h.svc.InstallStableUpdate(ctx)
	wantCode(t, err, apperr.CodeBinaryUpdateUnavailable)
	if got := h.downloads(t); got != 0 {
		t.Errorf("downloads = %d, want none when the release is not newer", got)
	}
	restored, readErr := os.ReadFile(installedPath)
	if readErr != nil {
		t.Fatalf("the installed binary is gone: %v", readErr)
	}
	if !bytes.Equal(restored, previousBytes) {
		t.Errorf("the installed binary changed to %q, want %q", restored, previousBytes)
	}
	if after := h.record(t); after != before {
		t.Errorf("record = %+v, want it unchanged (%+v)", after, before)
	}
	if len(h.store.marked) != 0 {
		t.Errorf("MarkRevisionsStale calls = %v, want none for a refused install", h.store.marked)
	}
}

func TestADeclaredDowngradeIsNeverInstalled(t *testing.T) {
	h := newHarness(t)
	h.setManagedBinary(t, "1.16.0", filepath.Join(h.paths.BinDir, "1.16.0", "sing-box"), []byte("managed 1.16.0"))
	const asset = "sing-box-1.15.0-darwin-arm64.tar.gz"
	h.publish(t, []ghRelease{h.release("1.15.0", false, asset)}, assetBody{name: asset, body: []byte("archive")})

	_, err := h.svc.InstallStableUpdate(context.Background())
	wantCode(t, err, apperr.CodeBinaryUpdateUnavailable)
	if got := h.downloads(t); got != 0 {
		t.Errorf("downloads = %d, want none for a downgrade", got)
	}
}

// ------------------------------------------------------------- credentials

func TestErrorsNeverLeakTheReleaseToken(t *testing.T) {
	const token = "ghp_superSecretTokenValue"
	h := newHarness(t)
	h.svc.deps.Client.Token = token

	// A refusing API, a failed download and a rejected checksum must all keep
	// the credential out of the message the UI shows (spec §33, §77).
	h.upstream.mu.Lock()
	h.upstream.apiStatus = http.StatusForbidden
	h.upstream.mu.Unlock()

	_, err := h.svc.CheckForUpdate(context.Background())
	wantCode(t, err, apperr.CodeNetworkUnavailable)
	assertNoSecret(t, err, token)

	h.upstream.mu.Lock()
	h.upstream.apiStatus = 0
	h.upstream.dlStatus = http.StatusInternalServerError
	h.upstream.mu.Unlock()

	const asset = "sing-box-1.15.0-darwin-arm64.tar.gz"
	h.setManagedBinary(t, "1.14.0", filepath.Join(h.paths.BinDir, "1.14.0", "sing-box"), []byte("managed 1.14.0"))
	h.publish(t, []ghRelease{h.release("1.15.0", false, asset)}, assetBody{name: asset, body: []byte("archive")})

	_, err = h.svc.InstallStableUpdate(context.Background())
	wantCode(t, err, apperr.CodeBinaryDownloadFailed)
	assertNoSecret(t, err, token)

	// A URL that carries a credential in its query must only ever be reported
	// by host.
	h.upstream.mu.Lock()
	h.upstream.dlStatus = 0
	h.upstream.mu.Unlock()
	h.upstream.mu.Lock()
	release := h.upstream.releases[0]
	release.Assets[0].BrowserDownloadURL = h.server.URL + "/dl/" + asset + "?token=" + token
	h.upstream.releases[0] = release
	h.upstream.mu.Unlock()

	check, err := h.svc.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate() = %v, want nil", err)
	}
	if strings.Contains(check.DownloadHost, token) || strings.Contains(check.DownloadHost, "?") {
		t.Errorf("DownloadHost = %q, want the host without a query", check.DownloadHost)
	}
}

func assertNoSecret(t *testing.T, err error, secret string) {
	t.Helper()
	if strings.Contains(err.Error(), secret) {
		t.Errorf("error %q leaks the credential", err)
	}
	for _, needle := range []string{"Authorization", "Bearer "} {
		if strings.Contains(err.Error(), needle) {
			t.Errorf("error %q mentions %q", err, needle)
		}
	}
}

// -------------------------------------------------------- selection and status

func TestSetSourceValidatesTheCustomBinary(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	executable := filepath.Join(h.paths.DataDir, "custom-sing-box")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("writing the custom binary: %v", err)
	}
	notExecutable := filepath.Join(h.paths.DataDir, "config.txt")
	if err := os.WriteFile(notExecutable, []byte("not a binary"), 0o644); err != nil {
		t.Fatalf("writing the plain file: %v", err)
	}

	tests := []struct {
		name    string
		source  settings.BinarySource
		path    string
		wantErr apperr.Code
	}{
		{name: "unknown source", source: "whatever", wantErr: apperr.CodeSettingsInvalid},
		{name: "custom without a path", source: settings.BinaryCustom, wantErr: apperr.CodeSettingsInvalid},
		{name: "custom pointing at a plain file", source: settings.BinaryCustom, path: notExecutable, wantErr: apperr.CodeBinaryNotFound},
		{name: "custom pointing at nothing", source: settings.BinaryCustom, path: filepath.Join(h.paths.DataDir, "absent"), wantErr: apperr.CodeBinaryNotFound},
		{name: "custom executable", source: settings.BinaryCustom, path: executable},
		{name: "back to managed", source: settings.BinaryManaged},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, err := h.svc.SetSource(ctx, test.source, test.path)
			if test.wantErr != "" {
				wantCode(t, err, test.wantErr)
				if values, _ := h.store.LoadSettings(ctx); values.BinarySource != settings.BinaryManaged {
					t.Errorf("settings changed to %q despite the rejection", values.BinarySource)
				}
				return
			}
			if err != nil {
				t.Fatalf("SetSource(%q, %q) = %v, want nil", test.source, test.path, err)
			}
			if status.Source != test.source {
				t.Errorf("status source = %q, want %q", status.Source, test.source)
			}
			values, _ := h.store.LoadSettings(ctx)
			if values.BinarySource != test.source {
				t.Errorf("persisted source = %q, want %q", values.BinarySource, test.source)
			}
			if test.source == settings.BinaryCustom {
				if values.CustomBinaryPath != executable {
					t.Errorf("persisted custom path = %q, want %q", values.CustomBinaryPath, executable)
				}
				if status.ActivePath != executable || !status.ActiveOK {
					t.Errorf("status = %+v, want the custom binary active", status)
				}
			} else if values.CustomBinaryPath != "" {
				t.Errorf("persisted custom path = %q, want it cleared when switching to managed", values.CustomBinaryPath)
			}
		})
	}
}

func TestStatusReportsABrokenBinaryInsteadOfFailing(t *testing.T) {
	h := newHarness(t)
	paths := h.paths
	// The record points at a file that no longer exists: the status must still
	// render so the UI can show the problem (spec §20, §53).
	missing := filepath.Join(paths.BinDir, "1.14.0", "sing-box")
	h.store.managed = sqlite.ManagedBinary{Version: "1.14.0", Path: missing, SHA256: strings.Repeat("b", 64)}

	status, err := h.svc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() = %v, want a report rather than a failure", err)
	}
	if status.ActiveOK || status.ActiveError == "" {
		t.Errorf("status = %+v, want an active error for the missing binary", status)
	}
	if !status.Managed.Installed || status.Managed.Version != "1.14.0" {
		t.Errorf("managed info = %+v, want the record still reported", status.Managed)
	}
	if _, err := h.svc.Path(context.Background()); err == nil {
		t.Error("Path() = nil error, want the missing binary reported")
	}
}

func TestVersionOfCachesUntilTheFileChanges(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	executable := filepath.Join(h.paths.DataDir, "cached-sing-box")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("writing the executable: %v", err)
	}
	probes := 0
	h.svc.deps.Probe = func(context.Context, string) (singbox.Version, error) {
		probes++
		return mustVersion(t, "1.15.0"), nil
	}

	for range 3 {
		if _, err := h.svc.VersionOf(ctx, executable); err != nil {
			t.Fatalf("VersionOf() = %v", err)
		}
	}
	if probes != 1 {
		t.Errorf("probes = %d, want 1 while the file is unchanged", probes)
	}
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n# changed\n"), 0o755); err != nil {
		t.Fatalf("rewriting the executable: %v", err)
	}
	if _, err := h.svc.VersionOf(ctx, executable); err != nil {
		t.Fatalf("VersionOf() after the change = %v", err)
	}
	if probes != 2 {
		t.Errorf("probes = %d, want a re-probe once the file changed", probes)
	}
	if _, err := h.svc.VersionOf(ctx, filepath.Join(h.paths.DataDir, "absent")); err == nil {
		t.Error("VersionOf(missing) = nil error, want BINARY_NOT_FOUND")
	} else {
		wantCode(t, err, apperr.CodeBinaryNotFound)
	}
}

// ------------------------------------------------------------------- helpers

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}

var (
	_ = runtime.GOOS
	_ = errors.Is
)

// zipArchive builds the archive shape upstream publishes for Windows.
func zipArchive(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		handle, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("zip member %s: %v", name, err)
		}
		if _, err := handle.Write(entries[name]); err != nil {
			t.Fatalf("zip body %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("closing the zip writer: %v", err)
	}
	return buffer.Bytes()
}

// TestInstallStableUpdateInstallsTheWindowsLibraries covers the platform-specific
// half of the installation: the Windows archive carries libcronet.dll beside
// sing-box.exe, the naive outbound loads it from that directory at run time, and
// an installation that kept only the executable could not run such a profile.
func TestInstallStableUpdateInstallsTheWindowsLibraries(t *testing.T) {
	h := newHarness(t)
	h.svc.deps.GOOS, h.svc.deps.GOARCH = "windows", "amd64"
	h.svc.deps.Probe = func(context.Context, string) (singbox.Version, error) {
		return mustVersion(t, "1.15.0"), nil
	}
	ctx := context.Background()
	h.setManagedBinary(t, "1.14.0", filepath.Join(h.paths.BinDir, "1.14.0", "sing-box.exe"), []byte("managed 1.14.0"))

	const asset = "sing-box-1.15.0-windows-amd64.zip"
	payload := []byte("MZ fake sing-box 1.15.0")
	archive := zipArchive(t, map[string][]byte{
		"sing-box-1.15.0-windows-amd64/":              nil,
		"sing-box-1.15.0-windows-amd64/LICENSE":       []byte("GPL-3.0"),
		"sing-box-1.15.0-windows-amd64/libcronet.dll": []byte("cronet library"),
		"sing-box-1.15.0-windows-amd64/sing-box.exe":  payload,
	})
	h.publish(t, []ghRelease{h.release("1.15.0", false, asset)}, assetBody{name: asset, body: archive})

	result, err := h.svc.InstallStableUpdate(ctx)
	if err != nil {
		t.Fatalf("InstallStableUpdate() = %v, want nil", err)
	}
	target := h.paths.ManagedBinaryPath("1.15.0", "sing-box.exe")
	if result.Path != target {
		t.Fatalf("Path = %q, want %q", result.Path, target)
	}
	installed, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("reading the installed executable: %v", err)
	}
	if !bytes.Equal(installed, payload) {
		t.Errorf("installed %q, want the archive's executable", installed)
	}
	library := filepath.Join(filepath.Dir(target), "libcronet.dll")
	content, err := os.ReadFile(library)
	if err != nil {
		t.Fatalf("the library sing-box loads on Windows is not installed beside the executable: %v", err)
	}
	if string(content) != "cronet library" {
		t.Errorf("installed library = %q, want the archive's copy", content)
	}
	// The license is not installed: only the files the archive was asked about.
	if entries := readDirNames(t, filepath.Dir(target)); len(entries) != 2 {
		t.Errorf("the version directory holds %v, want the executable and the library", entries)
	}
	// The staging directory is gone: nothing half-installed stays behind.
	if _, err := os.Stat(filepath.Join(h.paths.BinDir, "staging-1.15.0")); !os.IsNotExist(err) {
		t.Errorf("staging directory still present (err = %v)", err)
	}
}

func readDirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
