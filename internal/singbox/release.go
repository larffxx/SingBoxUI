package singbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
)

const (
	// DefaultBaseURL is the release API root.
	DefaultBaseURL = "https://api.github.com"
	// DefaultRepository is upstream sing-box, the only source of official
	// binaries; the settings screen may offer another mirror, but the verified
	// installer always talks to this repository.
	DefaultRepository = "SagerNet/sing-box"
	// releaseScan is how many releases are fetched when looking for the newest
	// stable one. The page has to be wide enough to still contain a stable
	// release during a long pre-release cycle, because beta and release
	// candidates are published far more often than stable releases.
	releaseScan = 30
	// responseCap bounds a release-list response so a hostile or broken API
	// endpoint cannot exhaust memory.
	responseCap = 32 << 20
)

// ErrNoAssetForPlatform marks a release that has no binary for this machine.
//
// It is a sentinel so a caller can tell "upstream did not publish this platform"
// apart from a network failure while still receiving the typed
// BINARY_UNSUPPORTED_PLATFORM code (spec §33).
var ErrNoAssetForPlatform = errors.New("singbox: no release asset for this platform")

// Asset is one downloadable file of a release.
//
// SHA256 comes from the release API's per-asset digest field. Upstream publishes
// no checksum file, so this digest — computed by GitHub over the uploaded bytes —
// is the official verification source (ADR 006, spec §21). An empty SHA256 means
// the API gave no digest, and an installer must refuse to install that asset
// rather than skip verification.
type Asset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"downloadUrl"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
}

// Release is a sing-box release as published upstream.
//
// Only stable releases reach the installer, so Prerelease and Draft are kept to
// make the filter explicit and testable rather than implicit in the query.
type Release struct {
	Version     string    `json:"version"` // "1.14.0", tag without the leading v
	Tag         string    `json:"tag"`     // "v1.14.0"
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
	PublishedAt time.Time `json:"publishedAt"`
	Notes       string    `json:"notes"`
	Assets      []Asset   `json:"assets"`
}

// Client reads releases and downloads their assets.
//
// The endpoint is a field rather than a constant so tests can run against a
// local server and mirrors can be added without a second type; the token is a
// field because an authenticated request is what keeps update checks working
// when the anonymous rate limit is exhausted.
type Client struct {
	// HTTPClient performs the requests. NewClient installs one with a timeout.
	HTTPClient *http.Client
	// BaseURL is the API root, e.g. "https://api.github.com".
	BaseURL string
	// Repository is "owner/name".
	Repository string
	// Token authenticates requests when set.
	Token string
	// UserAgent identifies the application; the API rejects requests without it.
	UserAgent string
	// Logger receives request-level diagnostics; nil is tolerated.
	Logger *slog.Logger
}

// NewClient builds a release client with working defaults.
//
// Every field is filled here so a caller cannot accidentally leave the client
// without a User-Agent (which the API rejects) or without a timeout (which would
// let an update check hang the UI).
func NewClient(httpClient *http.Client, token, userAgent string, logger *slog.Logger) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Minute}
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = "SingBoxUI"
	}
	return &Client{
		HTTPClient: httpClient,
		BaseURL:    DefaultBaseURL,
		Repository: DefaultRepository,
		Token:      strings.TrimSpace(token),
		UserAgent:  userAgent,
		Logger:     logger,
	}
}

// LatestStable returns the newest non-draft, non-prerelease release.
//
// Newest is decided by semantic version, not by the API's ordering: upstream
// keeps shipping maintenance releases on older lines, so a late backport of
// 1.12.x can appear after 1.14.0 and must not be offered as an update (spec §17).
func (c *Client) LatestStable(ctx context.Context) (Release, error) {
	const op = "singbox.LatestStable"
	endpoint := fmt.Sprintf("%s/repos/%s/releases?per_page=%d", c.baseURL(), c.repository(), releaseScan)
	body, err := c.get(ctx, op, endpoint)
	if err != nil {
		return Release{}, err
	}
	var payload []releasePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return Release{}, apperr.Wrap(apperr.CodeNetworkUnavailable, op,
			"the release API returned an unexpected response", err)
	}

	var (
		best     Release
		bestVer  Version
		eligible int
	)
	for _, item := range payload {
		if item.Draft || item.Prerelease {
			continue
		}
		version, err := ParseVersion(item.TagName)
		if err != nil || version.Pre != "" {
			// A tag that cannot be parsed cannot be compared, and an
			// unparseable tag is not a release this application will install.
			continue
		}
		eligible++
		if bestVer.Empty() || version.Compare(bestVer) > 0 {
			bestVer, best = version, item.toRelease()
		}
	}
	if bestVer.Empty() {
		return Release{}, apperr.Newf(apperr.CodeNotFound, op,
			"the newest %d releases contain no stable release", releaseScan)
	}
	c.logger().Debug("resolved latest stable sing-box release",
		"version", best.Version, "tag", best.Tag, "considered", eligible)
	return best, nil
}

// Download streams an asset to dst and returns the SHA-256 of the bytes written.
//
// Progress is reported in bounded steps — about a hundred callbacks for a known
// length, one per megabyte otherwise — because the UI event is rate limited and
// a per-chunk callback would flood it (spec §78). A transfer that ends early is
// an error even if the file it produced would look plausible: an installed
// binary is only as good as the last byte received (spec §77). dst must live on
// the same filesystem as the final location so the caller can rename it into
// place; nothing is written to the active configuration here.
func (c *Client) Download(ctx context.Context, url, dst string, progress func(done, total int64)) (string, error) {
	const op = "singbox.Download"
	if strings.TrimSpace(url) == "" || strings.TrimSpace(dst) == "" {
		return "", apperr.New(apperr.CodeInvalidArgument, op,
			"both a download URL and a destination path are required")
	}

	req, err := c.newRequest(ctx, http.MethodGet, url)
	if err != nil {
		return "", apperr.Wrap(apperr.CodeBinaryDownloadFailed, op, "cannot build the download request", err)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", apperr.Wrap(apperr.CodeBinaryDownloadFailed, op, "the download failed", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", apperr.Newf(apperr.CodeBinaryDownloadFailed, op,
			"the server answered %s for %s", resp.Status, url)
	}

	file, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", apperr.Wrap(apperr.CodeBinaryDownloadFailed, op, "cannot create "+dst, err)
	}
	digest := sha256.New()
	report := newProgressReporter(resp.ContentLength, progress)
	written, copyErr := io.Copy(io.MultiWriter(file, digest), io.TeeReader(resp.Body, report))
	if copyErr != nil {
		file.Close()
		os.Remove(dst)
		return "", apperr.Wrap(apperr.CodeBinaryDownloadFailed, op,
			"the download was interrupted before it finished", copyErr)
	}
	if resp.ContentLength >= 0 && written != resp.ContentLength {
		file.Close()
		os.Remove(dst)
		return "", apperr.Wrap(apperr.CodeBinaryDownloadFailed, op,
			fmt.Sprintf("the download is truncated: %d of %d bytes arrived", written, resp.ContentLength), io.ErrUnexpectedEOF)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		os.Remove(dst)
		return "", apperr.Wrap(apperr.CodeBinaryDownloadFailed, op, "cannot flush "+dst, err)
	}
	if err := file.Close(); err != nil {
		os.Remove(dst)
		return "", apperr.Wrap(apperr.CodeBinaryDownloadFailed, op, "cannot close "+dst, err)
	}
	report.finish()

	sum := hex.EncodeToString(digest.Sum(nil))
	c.logger().Debug("downloaded sing-box asset", "url", url, "bytes", written, "sha256", sum)
	return sum, nil
}

// AssetFor picks the official asset for a platform.
//
// The asset name is matched exactly, which is what keeps the legacy builds
// (`…-darwin-amd64-legacy-macos-10.13.tar.gz`, `…-windows-386-legacy-windows-7.zip`,
// `windows-386`) out of an install: they are published alongside the real ones
// and differ only by a suffix, so a prefix or contains-match would happily hand
// the user a binary for an operating system they do not run (spec §21).
func (r Release) AssetFor(goos, goarch string) (Asset, error) {
	const op = "singbox.AssetFor"
	extension, err := archiveExtension(goos, goarch)
	if err != nil {
		return Asset{}, err
	}
	version := r.Version
	if version == "" {
		version = strings.TrimPrefix(r.Tag, "v")
	}
	if version == "" {
		return Asset{}, apperr.Wrap(apperr.CodeBinaryUnsupportedPlatform, op,
			"the release does not carry a version", ErrNoAssetForPlatform)
	}
	want := fmt.Sprintf("sing-box-%s-%s-%s.%s", version, goos, goarch, extension)
	for _, asset := range r.Assets {
		if asset.Name == want {
			return asset, nil
		}
	}
	return Asset{}, apperr.Wrap(apperr.CodeBinaryUnsupportedPlatform, op,
		fmt.Sprintf("release %s publishes no asset named %s", version, want), ErrNoAssetForPlatform)
}

// ExecutableName is the file name of the sing-box executable on a platform.
//
// It is the single place that knows about the Windows ".exe" suffix, so archive
// extraction, binary resolution and path probing cannot disagree about it.
func ExecutableName(goos string) string {
	if goos == "windows" {
		return "sing-box.exe"
	}
	return "sing-box"
}

// archiveExtension returns the archive extension upstream uses for a platform,
// or BINARY_UNSUPPORTED_PLATFORM for machines this application does not target.
func archiveExtension(goos, goarch string) (string, error) {
	switch goos {
	case "darwin":
		if goarch == "amd64" || goarch == "arm64" {
			return "tar.gz", nil
		}
	case "windows":
		if goarch == "amd64" || goarch == "arm64" {
			return "zip", nil
		}
	}
	return "", apperr.Wrap(apperr.CodeBinaryUnsupportedPlatform, "singbox.AssetFor",
		fmt.Sprintf("sing-box is only installed on macOS and Windows, not %s/%s", goos, goarch),
		ErrNoAssetForPlatform)
}

// get performs an authenticated API request and returns the body.
func (c *Client) get(ctx context.Context, op, endpoint string) ([]byte, error) {
	req, err := c.newRequest(ctx, http.MethodGet, endpoint)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeNetworkUnavailable, op, "cannot build the request", err)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeNetworkUnavailable, op, "cannot reach the release API", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case http.StatusNotFound:
			return nil, apperr.Newf(apperr.CodeNotFound, op, "the release API has no repository %s", c.repository())
		case http.StatusForbidden, http.StatusTooManyRequests:
			return nil, apperr.Newf(apperr.CodeNetworkUnavailable, op,
				"the release API refused the request (%s); a token or a later retry is required", resp.Status)
		default:
			return nil, apperr.Newf(apperr.CodeNetworkUnavailable, op,
				"the release API answered %s", resp.Status)
		}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, responseCap))
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeNetworkUnavailable, op, "cannot read the release API response", err)
	}
	return body, nil
}

func (c *Client) newRequest(ctx context.Context, method, endpoint string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", c.userAgent())
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return req, nil
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c *Client) baseURL() string {
	if strings.TrimSpace(c.BaseURL) == "" {
		return DefaultBaseURL
	}
	return strings.TrimRight(c.BaseURL, "/")
}

func (c *Client) repository() string {
	if strings.TrimSpace(c.Repository) == "" {
		return DefaultRepository
	}
	return c.Repository
}

func (c *Client) userAgent() string {
	if strings.TrimSpace(c.UserAgent) == "" {
		return "SingBoxUI"
	}
	return c.UserAgent
}

func (c *Client) logger() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	// A release check is not worth an unconfigured logger panic.
	return slog.New(slog.DiscardHandler)
}

// progressSteps is how many progress callbacks a download of known length
// produces; the UI batches its events, so more would be wasted work.
const progressSteps = 100

// progressReporter throttles download progress to a bounded number of calls.
type progressReporter struct {
	progress func(done, total int64)
	total    int64
	done     int64
	step     int64
	next     int64
}

func newProgressReporter(total int64, progress func(done, total int64)) *progressReporter {
	reporter := &progressReporter{progress: progress, total: total}
	if progress == nil {
		return reporter
	}
	if total > 0 {
		reporter.step = total / progressSteps
	}
	if reporter.step <= 0 {
		// Unknown or tiny length: fall back to megabyte granularity.
		reporter.step = 1 << 20
	}
	progress(0, total)
	reporter.next = reporter.step
	return reporter
}

func (p *progressReporter) Write(data []byte) (int, error) {
	p.done += int64(len(data))
	if p.progress != nil && p.done >= p.next {
		p.progress(p.done, p.total)
		p.next = p.done + p.step
	}
	return len(data), nil
}

// finish reports the exact final size, so the UI never ends on an intermediate
// number when a stream ends between two step boundaries.
func (p *progressReporter) finish() {
	if p.progress != nil {
		p.progress(p.done, p.total)
	}
}

// releasePayload mirrors the subset of the API response the adapter needs.
// Sha256 digests arrive as "sha256:<hex>", the only verification source
// upstream publishes (ADR 006).
type releasePayload struct {
	TagName     string         `json:"tag_name"`
	Draft       bool           `json:"draft"`
	Prerelease  bool           `json:"prerelease"`
	PublishedAt time.Time      `json:"published_at"`
	Body        string         `json:"body"`
	Assets      []assetPayload `json:"assets"`
}

type assetPayload struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
	State              string `json:"state"`
}

func (p releasePayload) toRelease() Release {
	assets := make([]Asset, 0, len(p.Assets))
	for _, raw := range p.Assets {
		if raw.State != "" && raw.State != "uploaded" {
			// A half-uploaded asset has no usable digest; offering it would
			// invite an install that can never verify.
			continue
		}
		assets = append(assets, Asset{
			Name:        raw.Name,
			DownloadURL: raw.BrowserDownloadURL,
			Size:        raw.Size,
			SHA256:      normalizeDigest(raw.Digest),
		})
	}
	// A stable, addressable order keeps the UI and tests deterministic.
	sort.Slice(assets, func(i, j int) bool { return assets[i].Name < assets[j].Name })
	return Release{
		Version:     strings.TrimPrefix(p.TagName, "v"),
		Tag:         p.TagName,
		Prerelease:  p.Prerelease,
		Draft:       p.Draft,
		PublishedAt: p.PublishedAt,
		Notes:       p.Body,
		Assets:      assets,
	}
}

// normalizeDigest turns the API's "sha256:<hex>" into the hex digest an
// installer compares against. Anything else yields an empty digest, which
// callers must treat as "unverifiable" rather than "verified".
func normalizeDigest(digest string) string {
	value := strings.TrimSpace(digest)
	if value == "" {
		return ""
	}
	algorithm, hexDigest, found := strings.Cut(value, ":")
	if !found || !strings.EqualFold(algorithm, "sha256") {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(hexDigest))
}
