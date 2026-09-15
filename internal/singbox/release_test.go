package singbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
)

// releaseFixture is one release exactly as the API returns it, including the
// per-asset "digest" field upstream publishes instead of a checksum file.
func releaseFixture(t *testing.T, tag string, draft, prerelease bool, publishedAt string, assets ...map[string]any) map[string]any {
	t.Helper()
	for _, asset := range assets {
		if _, ok := asset["digest"]; !ok {
			asset["digest"] = "sha256:" + strings.Repeat("ab", 32)
		}
		if _, ok := asset["state"]; !ok {
			asset["state"] = "uploaded"
		}
	}
	return map[string]any{
		"tag_name":     tag,
		"draft":        draft,
		"prerelease":   prerelease,
		"published_at": publishedAt,
		"body":         "release notes for " + tag,
		"assets":       assets,
	}
}

func assetFixture(name string, size int, digest string) map[string]any {
	return map[string]any{
		"name":                 name,
		"size":                 size,
		"browser_download_url": "https://example.invalid/" + name,
		"digest":               digest,
	}
}

// newReleaseServer serves a release list and records the headers it received.
func newReleaseServer(t *testing.T, status int, body string) (*httptest.Server, *http.Header, *string) {
	t.Helper()
	var (
		seenHeader http.Header
		seenPath   string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenHeader = r.Header.Clone()
		seenPath = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server, &seenHeader, &seenPath
}

func TestLatestStable(t *testing.T) {
	// The interesting shape: a prerelease with the highest version, a draft,
	// an unparseable tag, and a late backport on an older line that must not
	// win over 1.14.0 (spec §17).
	list := []map[string]any{
		releaseFixture(t, "v1.15.0-beta.1", false, true, "2026-09-01T10:00:00Z"),
		releaseFixture(t, "v1.16.0", true, false, "2026-09-02T10:00:00Z"),
		releaseFixture(t, "nightly", false, false, "2026-09-03T10:00:00Z"),
		releaseFixture(t, "v1.14.0", false, false, "2026-08-20T10:00:00Z",
			assetFixture("sing-box-1.14.0-darwin-arm64.tar.gz", 42, "SHA256:"+strings.Repeat("AB", 32)),
			assetFixture("sing-box-1.14.0-darwin-arm64-legacy-macos-10.13.tar.gz", 43, "sha256:"+strings.Repeat("cd", 32)),
			assetFixture("sing-box-1.14.0-windows-amd64.zip", 44, "sha256:"+strings.Repeat("ef", 32)),
			assetFixture("sing-box-1.14.0-linux-amd64.tar.gz", 45, "sha256:"+strings.Repeat("11", 32)),
		),
		releaseFixture(t, "v1.12.9", false, false, "2026-09-10T10:00:00Z"),
	}
	encoded, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("cannot encode the fixture: %v", err)
	}
	server, header, path := newReleaseServer(t, http.StatusOK, string(encoded))

	client := NewClient(server.Client(), "token-123", "SingBoxUI-test", nil)
	client.BaseURL = server.URL

	release, err := client.LatestStable(context.Background())
	if err != nil {
		t.Fatalf("LatestStable() failed: %v", err)
	}
	if release.Tag != "v1.14.0" || release.Version != "1.14.0" {
		t.Errorf("release = %s (%s), want v1.14.0 (1.14.0)", release.Tag, release.Version)
	}
	if release.Prerelease || release.Draft {
		t.Errorf("release flags = prerelease %v draft %v, want both false", release.Prerelease, release.Draft)
	}
	if want := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC); !release.PublishedAt.Equal(want) {
		t.Errorf("published at = %s, want %s", release.PublishedAt, want)
	}
	if release.Notes == "" {
		t.Error("release notes are empty")
	}
	if len(release.Assets) != 4 {
		t.Fatalf("assets = %d, want 4 (%+v)", len(release.Assets), release.Assets)
	}
	// Assets come back in a stable order, and the API digest loses its
	// algorithm prefix and its case.
	for i := 1; i < len(release.Assets); i++ {
		if release.Assets[i-1].Name >= release.Assets[i].Name {
			t.Errorf("assets are not sorted: %q before %q", release.Assets[i-1].Name, release.Assets[i].Name)
		}
	}
	var selected *Asset
	for i := range release.Assets {
		if release.Assets[i].Name == "sing-box-1.14.0-darwin-arm64.tar.gz" {
			selected = &release.Assets[i]
		}
	}
	if selected == nil {
		t.Fatal("the darwin-arm64 asset is missing")
	}
	if got := selected.SHA256; got != strings.Repeat("ab", 32) {
		t.Errorf("digest = %q, want the lower-cased hex digest without the algorithm", got)
	}

	// The request has to look like a GitHub API call; without a User-Agent the
	// API answers 403 and update checks silently stop working.
	if got := header.Get("User-Agent"); got != "SingBoxUI-test" {
		t.Errorf("User-Agent = %q, want SingBoxUI-test", got)
	}
	if got := header.Get("Accept"); got != "application/vnd.github+json" {
		t.Errorf("Accept = %q, want application/vnd.github+json", got)
	}
	if got := header.Get("Authorization"); got != "Bearer token-123" {
		t.Errorf("Authorization = %q, want the bearer token", got)
	}
	if !strings.Contains(*path, "/repos/SagerNet/sing-box/releases") {
		t.Errorf("request path = %q, want the sing-box release list", *path)
	}
	if !strings.Contains(*path, fmt.Sprintf("per_page=%d", releaseScan)) {
		t.Errorf("request path = %q, want a per_page of %d", *path, releaseScan)
	}
}

func TestLatestStableDefaultsAreFilled(t *testing.T) {
	client := NewClient(nil, "  ", "  ", nil)
	if client.HTTPClient == nil || client.HTTPClient.Timeout == 0 {
		t.Error("NewClient() left the HTTP client without a timeout")
	}
	if client.BaseURL != DefaultBaseURL || client.Repository != DefaultRepository {
		t.Errorf("defaults = %s / %s, want %s / %s", client.BaseURL, client.Repository, DefaultBaseURL, DefaultRepository)
	}
	if client.UserAgent != "SingBoxUI" || client.Token != "" {
		t.Errorf("user agent/token = %q/%q, want SingBoxUI/empty", client.UserAgent, client.Token)
	}
	// A nil logger must not panic: an update check is not worth a crash.
	if client.Logger != nil {
		t.Error("NewClient() invented a logger")
	}
}

func TestLatestStableFailures(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantCode apperr.Code
	}{
		{name: "no stable release in the page", status: http.StatusOK, body: `[{"tag_name":"v1.15.0-beta.1","prerelease":true},{"tag_name":"nightly"}]`, wantCode: apperr.CodeNotFound},
		{name: "empty page", status: http.StatusOK, body: `[]`, wantCode: apperr.CodeNotFound},
		{name: "malformed json", status: http.StatusOK, body: `{"message":`, wantCode: apperr.CodeNetworkUnavailable},
		{name: "unknown repository", status: http.StatusNotFound, body: `{"message":"Not Found"}`, wantCode: apperr.CodeNotFound},
		{name: "rate limited", status: http.StatusForbidden, body: `{"message":"rate limit"}`, wantCode: apperr.CodeNetworkUnavailable},
		{name: "too many requests", status: http.StatusTooManyRequests, body: `{"message":"slow down"}`, wantCode: apperr.CodeNetworkUnavailable},
		{name: "server error", status: http.StatusInternalServerError, body: `{"message":"boom"}`, wantCode: apperr.CodeNetworkUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, _, _ := newReleaseServer(t, test.status, test.body)
			client := NewClient(server.Client(), "", "", nil)
			client.BaseURL = server.URL
			_, err := client.LatestStable(context.Background())
			if err == nil {
				t.Fatal("LatestStable() succeeded, want an error")
			}
			if code := apperr.CodeOf(err); code != test.wantCode {
				t.Errorf("error code = %s, want %s (%v)", code, test.wantCode, err)
			}
		})
	}
}

func TestLatestStableUnreachableEndpoint(t *testing.T) {
	server, _, _ := newReleaseServer(t, http.StatusOK, `[]`)
	url := server.URL
	server.Close()

	client := NewClient(nil, "", "", nil)
	client.BaseURL = url
	_, err := client.LatestStable(context.Background())
	if err == nil {
		t.Fatal("LatestStable() succeeded against a closed server")
	}
	if code := apperr.CodeOf(err); code != apperr.CodeNetworkUnavailable {
		t.Errorf("error code = %s, want %s (%v)", code, apperr.CodeNetworkUnavailable, err)
	}
}

func TestAssetFor(t *testing.T) {
	release := Release{
		Version: "1.14.0",
		Tag:     "v1.14.0",
		Assets: []Asset{
			{Name: "sing-box-1.14.0-darwin-arm64.tar.gz"},
			{Name: "sing-box-1.14.0-darwin-arm64-legacy-macos-10.13.tar.gz"},
			{Name: "sing-box-1.14.0-darwin-amd64.tar.gz"},
			{Name: "sing-box-1.14.0-darwin-amd64-legacy-macos-10.13.tar.gz"},
			{Name: "sing-box-1.14.0-windows-amd64.zip"},
			{Name: "sing-box-1.14.0-windows-arm64.zip"},
			{Name: "sing-box-1.14.0-windows-amd64-legacy-windows-7.zip"},
			{Name: "sing-box-1.14.0-windows-386.zip"},
			{Name: "sing-box-1.14.0-linux-amd64.tar.gz"},
			{Name: "sing-box-1.14.0-source.tar.gz"},
		},
	}
	tests := []struct {
		name     string
		goos     string
		goarch   string
		wantName string
		wantCode apperr.Code
	}{
		{name: "macOS arm64", goos: "darwin", goarch: "arm64", wantName: "sing-box-1.14.0-darwin-arm64.tar.gz"},
		{name: "macOS intel", goos: "darwin", goarch: "amd64", wantName: "sing-box-1.14.0-darwin-amd64.tar.gz"},
		{name: "windows intel", goos: "windows", goarch: "amd64", wantName: "sing-box-1.14.0-windows-amd64.zip"},
		{name: "windows arm64", goos: "windows", goarch: "arm64", wantName: "sing-box-1.14.0-windows-arm64.zip"},
		{name: "linux is not installed", goos: "linux", goarch: "amd64", wantCode: apperr.CodeBinaryUnsupportedPlatform},
		{name: "32-bit windows is not installed", goos: "windows", goarch: "386", wantCode: apperr.CodeBinaryUnsupportedPlatform},
		{name: "32-bit macOS is not installed", goos: "darwin", goarch: "386", wantCode: apperr.CodeBinaryUnsupportedPlatform},
		{name: "unknown architecture", goos: "darwin", goarch: "riscv64", wantCode: apperr.CodeBinaryUnsupportedPlatform},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := release.AssetFor(test.goos, test.goarch)
			if test.wantCode != "" {
				if err == nil {
					t.Fatalf("AssetFor() = %+v, want an error", got)
				}
				if code := apperr.CodeOf(err); code != test.wantCode {
					t.Errorf("error code = %s, want %s (%v)", code, test.wantCode, err)
				}
				if !errors.Is(err, ErrNoAssetForPlatform) {
					t.Errorf("error %v does not wrap ErrNoAssetForPlatform", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("AssetFor() failed: %v", err)
			}
			if got.Name != test.wantName {
				t.Fatalf("AssetFor() = %q, want %q", got.Name, test.wantName)
			}
			// The legacy builds differ from a supported one only by a suffix,
			// so an exact match is the only safe rule.
			if strings.Contains(got.Name, "legacy") || strings.Contains(got.Name, "386") {
				t.Errorf("AssetFor() selected %q", got.Name)
			}
		})
	}
}

func TestAssetForMissingAssetIsSentinel(t *testing.T) {
	release := Release{Tag: "v1.14.0", Assets: []Asset{{Name: "sing-box-1.14.0-darwin-amd64.tar.gz"}}}
	_, err := release.AssetFor("darwin", "arm64")
	if err == nil {
		t.Fatal("AssetFor() succeeded for a platform the release does not publish")
	}
	if !errors.Is(err, ErrNoAssetForPlatform) {
		t.Errorf("error %v does not wrap ErrNoAssetForPlatform", err)
	}
	if code := apperr.CodeOf(err); code != apperr.CodeBinaryUnsupportedPlatform {
		t.Errorf("error code = %s, want %s", code, apperr.CodeBinaryUnsupportedPlatform)
	}

	// A release without a version cannot name an asset either.
	if _, err := (Release{}).AssetFor("darwin", "arm64"); err == nil {
		t.Error("AssetFor() accepted a release without a version")
	}
}

func TestAssetForUsesTheTagWhenTheVersionIsMissing(t *testing.T) {
	release := Release{Tag: "v1.14.0", Assets: []Asset{{Name: "sing-box-1.14.0-darwin-arm64.tar.gz"}}}
	got, err := release.AssetFor("darwin", "arm64")
	if err != nil {
		t.Fatalf("AssetFor() failed: %v", err)
	}
	if got.Name != "sing-box-1.14.0-darwin-arm64.tar.gz" {
		t.Errorf("AssetFor() = %q", got.Name)
	}
}

func TestExecutableName(t *testing.T) {
	tests := []struct {
		goos string
		want string
	}{
		{goos: "darwin", want: "sing-box"},
		{goos: "windows", want: "sing-box.exe"},
		{goos: "linux", want: "sing-box"},
	}
	for _, test := range tests {
		t.Run(test.goos, func(t *testing.T) {
			if got := ExecutableName(test.goos); got != test.want {
				t.Errorf("ExecutableName(%q) = %q, want %q", test.goos, got, test.want)
			}
		})
	}
}

func TestNormalizeDigest(t *testing.T) {
	tests := []struct {
		name   string
		digest string
		want   string
	}{
		{name: "sha256", digest: "sha256:" + strings.Repeat("ab", 32), want: strings.Repeat("ab", 32)},
		{name: "upper case algorithm", digest: "SHA256:" + strings.Repeat("AB", 32), want: strings.Repeat("ab", 32)},
		{name: "surrounding whitespace", digest: " sha256:" + strings.Repeat("ab", 32) + " ", want: strings.Repeat("ab", 32)},
		{name: "no digest published", digest: "", want: ""},
		{name: "another algorithm", digest: "md5:" + strings.Repeat("ab", 16), want: ""},
		{name: "no algorithm prefix", digest: strings.Repeat("ab", 32), want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeDigest(test.digest); got != test.want {
				t.Errorf("normalizeDigest(%q) = %q, want %q", test.digest, got, test.want)
			}
		})
	}
}

func TestLatestStableDropsAssetsWithoutAState(t *testing.T) {
	body := `[{"tag_name":"v1.14.0","assets":[` +
		`{"name":"sing-box-1.14.0-darwin-arm64.tar.gz","size":1,"browser_download_url":"https://example.invalid/a","digest":"sha256:ab","state":"uploaded"},` +
		`{"name":"sing-box-1.14.0-windows-amd64.zip","size":2,"browser_download_url":"https://example.invalid/b","digest":"sha256:cd","state":"starter"}]}]`
	server, _, _ := newReleaseServer(t, http.StatusOK, body)
	client := NewClient(server.Client(), "", "", nil)
	client.BaseURL = server.URL

	release, err := client.LatestStable(context.Background())
	if err != nil {
		t.Fatalf("LatestStable() failed: %v", err)
	}
	if len(release.Assets) != 1 {
		t.Fatalf("assets = %+v, want only the uploaded one", release.Assets)
	}
	if release.Assets[0].Name != "sing-box-1.14.0-darwin-arm64.tar.gz" {
		t.Errorf("kept %q, want the uploaded asset", release.Assets[0].Name)
	}
	if release.Assets[0].DownloadURL != "https://example.invalid/a" {
		t.Errorf("download URL = %q, want the browser_download_url", release.Assets[0].DownloadURL)
	}
	if release.Assets[0].Size != 1 {
		t.Errorf("size = %d, want 1", release.Assets[0].Size)
	}
}

func TestDownload(t *testing.T) {
	payload := []byte(strings.Repeat("sing-box payload ", 20000)) // ~320 KiB
	sum := sha256.Sum256(payload)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got == "" {
			t.Errorf("download request has no User-Agent")
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		_, _ = w.Write(payload)
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.Client(), "", "SingBoxUI-test", nil)
	dst := filepath.Join(t.TempDir(), "sing-box.tar.gz")

	var (
		calls    int
		first    int64
		last     int64
		monotone = true
	)
	got, err := client.Download(context.Background(), server.URL+"/sing-box.tar.gz", dst, func(done, total int64) {
		calls++
		if calls == 1 {
			first = done
		}
		if total != int64(len(payload)) {
			t.Errorf("progress total = %d, want %d", total, len(payload))
		}
		if done < last {
			monotone = false
		}
		last = done
	})
	if err != nil {
		t.Fatalf("Download() failed: %v", err)
	}
	if got != hex.EncodeToString(sum[:]) {
		t.Errorf("sha256 = %q, want %q", got, hex.EncodeToString(sum[:]))
	}
	raw, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("cannot read the download: %v", err)
	}
	if string(raw) != string(payload) {
		t.Errorf("downloaded %d bytes, want %d", len(raw), len(payload))
	}
	// A per-chunk callback would flood the UI event, so the count is bounded.
	if calls > progressSteps+5 {
		t.Errorf("progress callbacks = %d, want about %d", calls, progressSteps)
	}
	if first != 0 {
		t.Errorf("first progress call reported %d, want 0", first)
	}
	if last != int64(len(payload)) {
		t.Errorf("final progress call reported %d, want %d", last, len(payload))
	}
	if !monotone {
		t.Error("progress was not monotonic")
	}
}

func TestDownloadFailures(t *testing.T) {
	t.Run("interrupted transfer removes the partial file", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "4096")
			_, _ = w.Write([]byte("only a few bytes"))
		}))
		t.Cleanup(server.Close)

		client := NewClient(server.Client(), "", "", nil)
		dst := filepath.Join(t.TempDir(), "partial.tar.gz")
		_, err := client.Download(context.Background(), server.URL, dst, nil)
		if err == nil {
			t.Fatal("Download() accepted a truncated transfer")
		}
		if code := apperr.CodeOf(err); code != apperr.CodeBinaryDownloadFailed {
			t.Errorf("error code = %s, want %s (%v)", code, apperr.CodeBinaryDownloadFailed, err)
		}
		if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
			t.Error("the truncated download was left on disk")
		}
	})

	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "nope", http.StatusInternalServerError)
		}))
		t.Cleanup(server.Close)

		client := NewClient(server.Client(), "", "", nil)
		_, err := client.Download(context.Background(), server.URL, filepath.Join(t.TempDir(), "x"), nil)
		if err == nil {
			t.Fatal("Download() accepted a 500")
		}
		if code := apperr.CodeOf(err); code != apperr.CodeBinaryDownloadFailed {
			t.Errorf("error code = %s, want %s", code, apperr.CodeBinaryDownloadFailed)
		}
	})

	t.Run("missing arguments", func(t *testing.T) {
		client := NewClient(nil, "", "", nil)
		_, err := client.Download(context.Background(), "", "", nil)
		if err == nil {
			t.Fatal("Download() accepted empty arguments")
		}
		if code := apperr.CodeOf(err); code != apperr.CodeInvalidArgument {
			t.Errorf("error code = %s, want %s", code, apperr.CodeInvalidArgument)
		}
	})

	t.Run("unwritable destination", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("payload"))
		}))
		t.Cleanup(server.Close)

		client := NewClient(server.Client(), "", "", nil)
		dir := t.TempDir()
		_, err := client.Download(context.Background(), server.URL, filepath.Join(dir, "missing", "x"), nil)
		if err == nil {
			t.Fatal("Download() believed it wrote into a directory that does not exist")
		}
		if code := apperr.CodeOf(err); code != apperr.CodeBinaryDownloadFailed {
			t.Errorf("error code = %s, want %s", code, apperr.CodeBinaryDownloadFailed)
		}
	})
}

func TestProgressReporter(t *testing.T) {
	t.Run("unknown length reports progress per megabyte", func(t *testing.T) {
		var calls []int64
		reporter := newProgressReporter(-1, func(done, total int64) { calls = append(calls, done) })
		if _, err := reporter.Write(make([]byte, 1<<20)); err != nil {
			t.Fatalf("Write() failed: %v", err)
		}
		if _, err := reporter.Write(make([]byte, 512)); err != nil {
			t.Fatalf("Write() failed: %v", err)
		}
		reporter.finish()
		want := []int64{0, 1 << 20, 1<<20 + 512}
		if len(calls) != len(want) {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
		for i := range want {
			if calls[i] != want[i] {
				t.Fatalf("calls = %v, want %v", calls, want)
			}
		}
	})

	t.Run("nil progress is tolerated", func(t *testing.T) {
		reporter := newProgressReporter(1024, nil)
		if _, err := reporter.Write(make([]byte, 2048)); err != nil {
			t.Fatalf("Write() failed: %v", err)
		}
		reporter.finish()
	})

	t.Run("zero length falls back to a step", func(t *testing.T) {
		var calls int
		reporter := newProgressReporter(0, func(int64, int64) { calls++ })
		if _, err := reporter.Write(make([]byte, 1<<20)); err != nil {
			t.Fatalf("Write() failed: %v", err)
		}
		reporter.finish()
		if calls != 3 {
			t.Errorf("calls = %d, want 3 (initial, one step, final)", calls)
		}
	})
}
