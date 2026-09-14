package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	appconfig "github.com/larffxx/singboxui/internal/app/config"
	appruntime "github.com/larffxx/singboxui/internal/app/runtime"
	"github.com/larffxx/singboxui/internal/app/settings"
	"github.com/larffxx/singboxui/internal/app/share"
	"github.com/larffxx/singboxui/internal/app/traffic"
	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/applications"
	"github.com/larffxx/singboxui/internal/domain/profile"
	domruntime "github.com/larffxx/singboxui/internal/domain/runtime"
	domsettings "github.com/larffxx/singboxui/internal/domain/settings"
	"github.com/larffxx/singboxui/internal/events"
	"github.com/larffxx/singboxui/internal/platform"
	"github.com/larffxx/singboxui/internal/privilege"
	"github.com/larffxx/singboxui/internal/singbox/faketest"
	"github.com/larffxx/singboxui/internal/storage/sqlite"
	"github.com/larffxx/singboxui/internal/tray"
)

// The desktop layer is reachable without a webview because App.New takes its
// whole world as an argument (spec §63): a fake platform, a fake privileged
// launcher and a recorder standing in for the Wails event runtime. Nothing in
// this file touches the real home directory, starts a process or opens a socket
// other than an httptest server on the loopback interface.

const testVersion = "0.1.0-test"

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// sameJSON reports whether two configurations carry the same JSON value: the
// services may reserialise what they store, so byte equality is too strict.
func sameJSON(t *testing.T, left, right string) bool {
	t.Helper()
	var l, r any
	if err := json.Unmarshal([]byte(left), &l); err != nil {
		t.Fatalf("left is not JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(right), &r); err != nil {
		t.Fatalf("right is not JSON: %v", err)
	}
	return reflect.DeepEqual(l, r)
}

// -- recorder -----------------------------------------------------------------

type recordedEvent struct {
	name    string
	payload any
}

// recorder is an events.Emitter sink that keeps every payload in order.
type recorder struct {
	mu     sync.Mutex
	events []recordedEvent
}

func (r *recorder) sink(_ context.Context, name string, payload any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordedEvent{name: name, payload: payload})
}

func (r *recorder) all() []recordedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedEvent(nil), r.events...)
}

func (r *recorder) names() []string {
	out := make([]string, 0, len(r.events))
	for _, ev := range r.all() {
		out = append(out, ev.name)
	}
	return out
}

// waitFor polls until a payload of the named event satisfies want.
func (r *recorder) waitFor(t *testing.T, name string, want func(any) bool) any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, ev := range r.all() {
			if ev.name == name && want(ev.payload) {
				return ev.payload
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no %q event matched within the deadline; saw %v", name, r.names())
	return nil
}

// -- fakes --------------------------------------------------------------------

type fakeAutostart struct {
	supported  bool
	enabled    bool
	legacy     string
	execPath   string
	enableErr  error
	disableErr error
	legacyErr  error

	mu      sync.Mutex
	enables int
}

func (a *fakeAutostart) Supported() bool { return a.supported }

func (a *fakeAutostart) Enabled() (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.enabled, nil
}

func (a *fakeAutostart) Enable(execPath string) error {
	if a.enableErr != nil {
		return a.enableErr
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.execPath = execPath
	a.enabled = true
	a.enables++
	return nil
}

func (a *fakeAutostart) Disable() error {
	if a.disableErr != nil {
		return a.disableErr
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.enabled = false
	return nil
}

func (a *fakeAutostart) enableCalls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.enables
}

func (a *fakeAutostart) LegacyEntry() string { return a.legacy }

func (a *fakeAutostart) RemoveLegacy() error {
	if a.legacyErr != nil {
		return a.legacyErr
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.legacy = ""
	return nil
}

// fakePrivilege is the narrow privileged-launch adapter (spec §27). It records
// every attempt so a test can assert that a refusal happens *before* elevation
// is ever requested.
type fakePrivilege struct {
	supported bool
	reason    string

	mu     sync.Mutex
	starts []privilege.Request
}

func (p *fakePrivilege) Start(ctx context.Context, req privilege.Request) (privilege.Process, error) {
	p.mu.Lock()
	p.starts = append(p.starts, req)
	p.mu.Unlock()
	return nil, privilege.ErrUnsupported
}

func (p *fakePrivilege) Supported() (bool, string) { return p.supported, p.reason }

func (p *fakePrivilege) startCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.starts)
}

type fakePlatform struct {
	info  platform.Info
	paths platform.Paths
	auto  *fakeAutostart
	apps  platform.ApplicationCatalog
	priv  *fakePrivilege

	mu      sync.Mutex
	ensured int
}

func (p *fakePlatform) Info() platform.Info { return p.info }

func (p *fakePlatform) Paths() platform.Paths { return p.paths }

func (p *fakePlatform) Autostart() platform.Autostart { return p.auto }

func (p *fakePlatform) Applications() platform.ApplicationCatalog { return p.apps }

func (p *fakePlatform) PrivilegeRunner() privilege.Runner { return p.priv }

// EnsureDirs creates the layout inside the temporary directory only.
func (p *fakePlatform) EnsureDirs() error {
	p.mu.Lock()
	p.ensured++
	p.mu.Unlock()
	for _, dir := range []string{
		p.paths.DataDir,
		p.paths.ConfigDir,
		p.paths.RuntimeDir,
		p.paths.BinDir,
		p.paths.TempDir,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// testPaths mirrors the real on-disk layout, rooted in a temporary directory.
func testPaths(dir string) platform.Paths {
	dataDir := filepath.Join(dir, "data")
	configDir := filepath.Join(dataDir, "config")
	return platform.Paths{
		DataDir:            dataDir,
		DBPath:             filepath.Join(dataDir, "singboxui.db"),
		LogPath:            filepath.Join(dataDir, "singboxui.log"),
		ConfigDir:          configDir,
		ActiveConfigPath:   filepath.Join(configDir, "active.json"),
		LastGoodConfigPath: filepath.Join(configDir, "last-good.json"),
		RuntimeDir:         filepath.Join(dataDir, "runtime"),
		BinDir:             filepath.Join(dataDir, "bin"),
		TempDir:            filepath.Join(dataDir, "tmp"),
	}
}

func newFakePlatform(t *testing.T, paths platform.Paths) *fakePlatform {
	t.Helper()
	return &fakePlatform{
		info:  platform.Info{OS: "darwin", Arch: "arm64", Supported: true},
		paths: paths,
		auto:  &fakeAutostart{supported: true},
		apps:  &fakeApplications{},
		priv:  &fakePrivilege{supported: false, reason: "privileged helper is not installed"},
	}
}

// fakeApplications is a catalog with one application; a test that cares about
// the listing replaces it (ADR 011).
type fakeApplications struct {
	supported bool
	reason    string
	items     []applications.Application
	err       error
}

func (a *fakeApplications) Supported() bool { return a.supported }

func (a *fakeApplications) UnsupportedReason() string { return a.reason }

func (a *fakeApplications) List() ([]applications.Application, error) {
	if a.err != nil {
		return nil, a.err
	}
	return a.items, nil
}

// -- harness ------------------------------------------------------------------

type harness struct {
	app   *App
	plat  *fakePlatform
	rec   *recorder
	store *sqlite.Store
	paths platform.Paths
	dir   string
	// tray records what the menu bar was told to draw (spec §10).
	tray *fakeTrayDriver
}

func newHarness(t *testing.T, options ...func(*Deps)) *harness {
	t.Helper()
	dir := t.TempDir()
	paths := testPaths(dir)
	store, err := sqlite.Open(context.Background(), paths.DBPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	plat := newFakePlatform(t, paths)
	if err := plat.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	rec := &recorder{}
	emitter := NewEmitterWithSink(quietLogger(), rec.sink)
	driver := &fakeTrayDriver{}
	deps := Deps{
		Logger:     quietLogger(),
		Store:      store,
		Platform:   plat,
		Privilege:  plat.priv,
		Emitter:    emitter,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
		ExecPath:   filepath.Join(dir, "SingBoxUI.app", "Contents", "MacOS", "SingBoxUI"),
		Version:    testVersion,
		GOOS:       "darwin",
		GOARCH:     "arm64",
		// The menu bar is a real status item on macOS: a test that runs the
		// startup hook would otherwise put one into the developer's menu bar.
		NewTray: func(opts tray.Options) (tray.Driver, error) {
			driver.configure(opts)
			return driver, nil
		},
	}
	// A test that needs a seam the real process supplies at runtime — the native
	// file picker, for instance — overrides it before the graph is built.
	for _, option := range options {
		option(&deps)
	}
	app := New(deps)
	// Application services emit while they work; the sink is attached here the
	// way the Wails startup hook attaches the real one.
	emitter.Attach(context.Background())
	t.Cleanup(func() { app.OnShutdown(context.Background()) })
	return &harness{app: app, plat: plat, rec: rec, store: store, paths: paths, dir: dir, tray: driver}
}

// createProfile creates a profile through the bound facade and fails the test if
// it does not succeed.
func (h *harness) createProfile(t *testing.T, name, templateID string) string {
	t.Helper()
	payload := h.app.ProfileAPI.CreateProfile(CreateProfileRequest{Name: name, TemplateID: templateID})
	if payload.Error != nil {
		t.Fatalf("CreateProfile(%q, %q) failed: %+v", name, templateID, payload.Error)
	}
	if payload.Profile.ID == "" {
		t.Fatalf("CreateProfile(%q, %q) returned an empty id", name, templateID)
	}
	return payload.Profile.ID
}

func codeOf(err *apperr.Error) apperr.Code {
	if err == nil {
		return ""
	}
	return err.Code
}

// -- App construction ---------------------------------------------------------

func TestNewBuildsTheGraphWithoutCreatingTheLayout(t *testing.T) {
	dir := t.TempDir()
	paths := testPaths(dir)
	// Only the database directory exists: New must not create anything itself.
	if err := os.MkdirAll(paths.DataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	store, err := sqlite.Open(context.Background(), paths.DBPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	plat := newFakePlatform(t, paths)
	app := New(Deps{
		Logger:    quietLogger(),
		Store:     store,
		Platform:  plat,
		Privilege: plat.priv,
		Version:   testVersion,
	})
	t.Cleanup(func() { app.OnShutdown(context.Background()) })

	if app.ProfileAPI == nil || app.ConfigAPI == nil || app.RuntimeAPI == nil ||
		app.BinaryAPI == nil || app.SettingsAPI == nil || app.ShareAPI == nil ||
		app.TrafficAPI == nil {
		t.Fatal("New left a facade nil")
	}
	if app.emitter == nil {
		t.Fatal("New left the emitter nil")
	}
	s, supervisor, cfg, bins := app.Services()
	if s == nil || supervisor == nil || cfg == nil || bins == nil {
		t.Fatalf("Services() = %v %v %v %v, want all non-nil", s, supervisor, cfg, bins)
	}
	if app.ShuttingDown() {
		t.Error("ShuttingDown() = true on a fresh App")
	}
	if app.ShutdownError() != nil {
		t.Errorf("ShutdownError() = %v, want nil", app.ShutdownError())
	}
	// The layout is created by the shell through platform.EnsureDirs(), never as
	// a side effect of building the object graph.
	for _, dirName := range []string{paths.ConfigDir, paths.RuntimeDir, paths.BinDir, paths.TempDir} {
		if _, err := os.Stat(dirName); !os.IsNotExist(err) {
			t.Errorf("New created %s (stat err = %v); the layout is the platform's job", dirName, err)
		}
	}
	if app.deps.Version != testVersion {
		t.Errorf("New changed the injected version to %q, want %q", app.deps.Version, testVersion)
	}
	if app.deps.GOOS == "" || app.deps.GOARCH == "" || app.deps.HTTPClient == nil || app.deps.Now == nil {
		t.Errorf("New left unset defaults: goos=%q goarch=%q client=%v now=%v",
			app.deps.GOOS, app.deps.GOARCH, app.deps.HTTPClient, app.deps.Now != nil)
	}
}

// -- profile facade -----------------------------------------------------------

func TestProfileFacadeListsTemplatesAndAnEmptyCatalog(t *testing.T) {
	h := newHarness(t)

	templates := h.app.ProfileAPI.ListTemplates()
	if templates.Error != nil {
		t.Fatalf("ListTemplates: %+v", templates.Error)
	}
	wantIDs := []string{"empty", "tun-basic", "tun-vless-reality", "socks-local", "selector"}
	if len(templates.Templates) != len(wantIDs) {
		t.Fatalf("got %d templates, want %d", len(templates.Templates), len(wantIDs))
	}
	for i, want := range wantIDs {
		if got := templates.Templates[i].ID; got != want {
			t.Errorf("template[%d].ID = %q, want %q", i, got, want)
		}
	}

	list := h.app.ProfileAPI.ListProfiles()
	if list.Error != nil {
		t.Fatalf("ListProfiles: %+v", list.Error)
	}
	if len(list.Profiles) != 0 {
		t.Errorf("got %d profiles on a fresh installation, want 0", len(list.Profiles))
	}
	if list.ActiveID != "" || list.Running {
		t.Errorf("ActiveID = %q, Running = %v; want \"\" and false", list.ActiveID, list.Running)
	}
}

func TestProfileFacadeLifecycle(t *testing.T) {
	h := newHarness(t)
	api := h.app.ProfileAPI

	created := api.CreateProfile(CreateProfileRequest{Name: "Work", Description: "office", TemplateID: "socks-local"})
	if created.Error != nil {
		t.Fatalf("CreateProfile: %+v", created.Error)
	}
	id := created.Profile.ID
	if created.Profile.Name != "Work" || created.Profile.Description != "office" {
		t.Errorf("profile = %+v, want name Work and description office", created.Profile)
	}
	if created.Profile.ActiveRevisionID == "" {
		t.Error("CreateProfile did not create an active revision")
	}
	if created.Profile.CreatedAt.IsZero() {
		t.Error("CreateProfile left CreatedAt zero")
	}

	list := api.ListProfiles()
	if len(list.Profiles) != 1 || list.Profiles[0].ID != id {
		t.Fatalf("ListProfiles = %+v, want the created profile", list.Profiles)
	}

	revisions := api.ListRevisions(id, 0)
	if revisions.Error != nil {
		t.Fatalf("ListRevisions: %+v", revisions.Error)
	}
	if len(revisions.Revisions) != 1 {
		t.Fatalf("got %d revisions after creation, want 1", len(revisions.Revisions))
	}
	first := revisions.Revisions[0]
	if !first.Active {
		t.Error("the first revision is not marked active")
	}
	if first.ID != created.Profile.ActiveRevisionID {
		t.Errorf("active revision = %q, profile points at %q", first.ID, created.Profile.ActiveRevisionID)
	}
	if first.Size == 0 || first.Size != len(first.ConfigJSON) {
		t.Errorf("RevisionView.Size = %d, len(ConfigJSON) = %d", first.Size, len(first.ConfigJSON))
	}

	// Applying a template appends history; it never rewrites it.
	applied := api.ApplyTemplateToProfile(id, "tun-basic")
	if applied.Error != nil {
		t.Fatalf("ApplyTemplateToProfile: %+v", applied.Error)
	}
	if applied.Profile.ActiveRevisionID == first.ID {
		t.Error("ApplyTemplateToProfile did not move the active revision")
	}
	afterApply := api.ListRevisions(id, 0)
	if len(afterApply.Revisions) != 2 {
		t.Fatalf("got %d revisions after applying a template, want 2", len(afterApply.Revisions))
	}
	activeCount := 0
	for _, rev := range afterApply.Revisions {
		if rev.ID == first.ID {
			if rev.Active {
				t.Error("the original revision is still marked active")
			}
			if rev.ConfigJSON != first.ConfigJSON {
				t.Error("applying a template rewrote the original revision content")
			}
		}
		if rev.Active {
			activeCount++
		}
	}
	if activeCount != 1 {
		t.Errorf("%d revisions are marked active, want exactly 1", activeCount)
	}

	// Activating a profile is remembered for auto-connect.
	activated := api.SetActiveProfile(id)
	if activated.Error != nil {
		t.Fatalf("SetActiveProfile: %+v", activated.Error)
	}
	// ActiveID tracks the profile the runtime is using; nothing is running in
	// this test, so the marker stays empty and the choice is remembered in the
	// settings for auto-connect instead.
	if activated.ActiveID != "" {
		t.Errorf("ActiveID = %q while nothing is running, want the empty string", activated.ActiveID)
	}
	if activated.Running {
		t.Error("Running = true although no runtime was started")
	}
	if ghost := api.SetActiveProfile("does-not-exist"); ghost.Error == nil {
		t.Error("SetActiveProfile accepted an unknown profile")
	} else if ghost.Error.Code != apperr.CodeProfileNotFound {
		t.Errorf("SetActiveProfile(unknown) code = %q, want %q", ghost.Error.Code, apperr.CodeProfileNotFound)
	}
	stored, err := h.store.LoadSettings(context.Background())
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if stored.LastProfileID != id {
		t.Errorf("LastProfileID = %q, want %q", stored.LastProfileID, id)
	}

	renamed := api.RenameProfile(id, "Home", "second thoughts")
	if renamed.Error != nil {
		t.Fatalf("RenameProfile: %+v", renamed.Error)
	}
	if renamed.Profile.Name != "Home" || renamed.Profile.Description != "second thoughts" {
		t.Errorf("after rename: %+v", renamed.Profile)
	}

	duplicated := api.DuplicateProfile(id, "Home copy")
	if duplicated.Error != nil {
		t.Fatalf("DuplicateProfile: %+v", duplicated.Error)
	}
	if duplicated.Profile.ID == id || duplicated.Profile.ID == "" {
		t.Fatalf("DuplicateProfile returned id %q", duplicated.Profile.ID)
	}
	copied := api.ListRevisions(duplicated.Profile.ID, 0)
	if len(copied.Revisions) != 1 {
		t.Fatalf("the copy has %d revisions, want 1", len(copied.Revisions))
	}
	if copied.Revisions[0].ConfigJSON != afterApply.Revisions[0].ConfigJSON &&
		copied.Revisions[0].ConfigJSON == first.ConfigJSON {
		t.Error("the copy uses the first revision instead of the active one")
	}

	deleted := api.DeleteProfile(id)
	if deleted.Error != nil {
		t.Fatalf("DeleteProfile: %+v", deleted.Error)
	}
	if len(deleted.Profiles) != 1 || deleted.Profiles[0].ID != duplicated.Profile.ID {
		t.Fatalf("after delete: %+v", deleted.Profiles)
	}

	missing := api.GetProfile("does-not-exist")
	if missing.Error == nil {
		t.Fatal("GetProfile(unknown) returned no error")
	}
	if code := codeOf(missing.Error); code != apperr.CodeProfileNotFound {
		t.Errorf("GetProfile(unknown) code = %q, want %q", code, apperr.CodeProfileNotFound)
	}
}

func TestProfileFacadeRejectsBadInput(t *testing.T) {
	h := newHarness(t)
	api := h.app.ProfileAPI

	cases := []struct {
		name string
		req  CreateProfileRequest
		want apperr.Code
	}{
		{
			name: "empty name",
			req:  CreateProfileRequest{Name: "   ", TemplateID: "empty"},
			want: apperr.CodeInvalidArgument,
		},
		{
			name: "unknown template",
			req:  CreateProfileRequest{Name: "Work", TemplateID: "no-such-template"},
			want: apperr.CodeNotFound,
		},
		{
			name: "configuration is not JSON",
			req:  CreateProfileRequest{Name: "Work", ConfigJSON: "{oh no"},
			want: apperr.CodeConfigInvalid,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := api.CreateProfile(tc.req)
			if got.Error == nil {
				t.Fatalf("CreateProfile(%+v) returned no error", tc.req)
			}
			if code := codeOf(got.Error); code != tc.want {
				t.Errorf("code = %q, want %q (%s)", code, tc.want, got.Error.Message)
			}
		})
	}

	before := api.ListProfiles()
	if len(before.Profiles) != 0 {
		t.Errorf("a rejected create left %d profiles behind", len(before.Profiles))
	}
}

func TestProfileFacadeExportAndImport(t *testing.T) {
	h := newHarness(t)
	api := h.app.ProfileAPI
	id := h.createProfile(t, "Work", "socks-local")

	exported := api.ExportProfile(id)
	if exported.Error != nil {
		t.Fatalf("ExportProfile: %+v", exported.Error)
	}
	if exported.Export == nil {
		t.Fatal("ExportProfile returned no export")
	}
	if exported.Export.FileName == "" {
		t.Error("ExportResult.FileName is empty")
	}
	if !json.Valid([]byte(exported.Export.ConfigJSON)) {
		t.Fatalf("the exported configuration is not valid JSON: %q", exported.Export.ConfigJSON)
	}
	if exported.Export.RevisionID == "" {
		t.Error("ExportResult.RevisionID is empty")
	}

	imported := api.ImportProfile(CreateProfileRequest{
		Name:        "Imported",
		ConfigJSON:  exported.Export.ConfigJSON,
		Description: "from an export",
	})
	if imported.Error != nil {
		t.Fatalf("ImportProfile: %+v", imported.Error)
	}
	if imported.Profile.ID == "" || imported.Profile.ID == id {
		t.Errorf("ImportProfile returned id %q", imported.Profile.ID)
	}
	if imported.Profile.Name != "Imported" {
		t.Errorf("imported name = %q", imported.Profile.Name)
	}
	revisions := api.ListRevisions(imported.Profile.ID, 0)
	if len(revisions.Revisions) != 1 {
		t.Fatalf("the imported profile has %d revisions, want 1", len(revisions.Revisions))
	}
	if !sameJSON(t, revisions.Revisions[0].ConfigJSON, exported.Export.ConfigJSON) {
		t.Errorf("import did not preserve the configuration content:\ngot  %s\nwant %s",
			revisions.Revisions[0].ConfigJSON, exported.Export.ConfigJSON)
	}
}

// -- config facade ------------------------------------------------------------

func TestConfigFacadeDraftValidateAndApply(t *testing.T) {
	h := newHarness(t)
	cfg := h.app.ConfigAPI
	id := h.createProfile(t, "Work", "socks-local")

	draft := cfg.GetDraft(id)
	if draft.Error != nil {
		t.Fatalf("GetDraft: %+v", draft.Error)
	}
	if draft.Draft.ProfileID != id {
		t.Errorf("draft profile = %q, want %q", draft.Draft.ProfileID, id)
	}
	if draft.Draft.ProfileName != "Work" {
		t.Errorf("draft profile name = %q", draft.Draft.ProfileName)
	}
	if draft.Draft.BaseRevisionID == "" {
		t.Error("draft has no base revision")
	}
	if !draft.Draft.StructuralResult.OK {
		t.Errorf("the template draft is not structurally valid: %+v", draft.Draft.StructuralResult.Errors)
	}
	if !json.Valid([]byte(draft.Draft.ConfigJSON)) {
		t.Fatalf("the draft is not valid JSON: %q", draft.Draft.ConfigJSON)
	}

	// A draft for a profile that does not exist is a typed error, not a panic.
	if got := cfg.GetDraft("ghost"); got.Error == nil {
		t.Error("GetDraft(unknown) returned no error")
	} else if got.Error.Code != apperr.CodeProfileNotFound {
		t.Errorf("GetDraft(unknown) code = %q", got.Error.Code)
	}

	// Structural validation never needs the binary.
	valid := cfg.ValidateConfig(appconfig.ValidateInput{ProfileID: id, ConfigJSON: draft.Draft.ConfigJSON, SkipSingBoxCheck: true})
	if valid.Error != nil {
		t.Fatalf("ValidateConfig(valid): %+v", valid.Error)
	}
	if !valid.Result.Valid || !valid.Result.Structural.OK {
		t.Errorf("a template draft did not validate: %+v", valid.Result)
	}

	// Unparseable input is refused with a typed error rather than a result the
	// frontend would have to interpret.
	broken := cfg.ValidateConfig(appconfig.ValidateInput{ProfileID: id, ConfigJSON: "{not json", SkipSingBoxCheck: true})
	if broken.Error == nil {
		t.Fatal("ValidateConfig accepted invalid JSON")
	}
	if broken.Error.Code != apperr.CodeConfigInvalid {
		t.Errorf("ValidateConfig(broken) code = %q, want %q", broken.Error.Code, apperr.CodeConfigInvalid)
	}

	// JSON that parses but breaks the schema is refused with the structural
	// reasons attached, so the frontend can list them.
	wrongShape := cfg.ValidateConfig(appconfig.ValidateInput{
		ProfileID:        id,
		ConfigJSON:       `{"outbounds":[{"type":"direct"}]}`,
		SkipSingBoxCheck: true,
	})
	if wrongShape.Error == nil {
		t.Fatal("ValidateConfig accepted an outbound without a tag")
	}
	if wrongShape.Error.Code != apperr.CodeConfigInvalid {
		t.Errorf("ValidateConfig(wrong shape) code = %q, want %q", wrongShape.Error.Code, apperr.CodeConfigInvalid)
	}
	if !strings.Contains(strings.Join(wrongShape.Error.Details, "; "), "missing tag") {
		t.Errorf("the structural reasons are missing from %v", wrongShape.Error.Details)
	}

	// Saving an edit appends a revision and leaves the active one alone.
	edited := strings.Replace(draft.Draft.ConfigJSON, `"direct"`, `"direct-edited"`, 1)
	if edited == draft.Draft.ConfigJSON {
		t.Fatal("the test fixture did not change the configuration")
	}
	saved := cfg.SaveRevision(appconfig.SaveInput{ProfileID: id, ConfigJSON: edited, Comment: "rename the outbound"})
	if saved.Error != nil {
		t.Fatalf("SaveRevision: %+v", saved.Error)
	}
	if saved.Revision.ID == "" || saved.Revision.ID == draft.Draft.BaseRevisionID {
		t.Fatalf("SaveRevision returned revision id %q", saved.Revision.ID)
	}
	if saved.Revision.Active {
		t.Error("a save without Apply marked the revision active")
	}
	// The stored revision is the canonical (pretty-printed) form of the edit.
	if !sameJSON(t, saved.Revision.ConfigJSON, edited) {
		t.Errorf("the saved revision does not contain the edited configuration:\ngot  %s\nwant %s",
			saved.Revision.ConfigJSON, edited)
	}
	if saved.Revision.Size != len(saved.Revision.ConfigJSON) {
		t.Errorf("Size = %d, want the stored length %d", saved.Revision.Size, len(saved.Revision.ConfigJSON))
	}

	invalid := cfg.SaveRevision(appconfig.SaveInput{ProfileID: id, ConfigJSON: "{oops"})
	if invalid.Error == nil {
		t.Fatal("SaveRevision accepted invalid JSON")
	}
	if invalid.Error.Code != apperr.CodeConfigInvalid {
		t.Errorf("SaveRevision(invalid) code = %q, want %q", invalid.Error.Code, apperr.CodeConfigInvalid)
	}

	// Applying runs the validator, so the application needs a sing-box to point
	// at: a stand-in that answers "version" and accepts everything it is asked
	// to check.
	fakeBinary := placeFakeSingbox(t, h.dir, "sing-box")
	if source := h.app.BinaryAPI.SetBinarySource("custom", fakeBinary); source.Error != nil {
		t.Fatalf("SetBinarySource: %+v", source.Error)
	}

	// Apply is transactional: the file on disk and the active pointer move
	// together, and the runtime is not restarted because nothing is running.
	applied := cfg.ApplyRevision(appconfig.ApplyInput{ProfileID: id, RevisionID: saved.Revision.ID})
	if applied.Error != nil {
		t.Fatalf("ApplyRevision: %+v", applied.Error)
	}
	if applied.Result.RevisionID != saved.Revision.ID {
		t.Errorf("ApplyRevision returned revision %q, want %q", applied.Result.RevisionID, saved.Revision.ID)
	}
	if applied.Result.ProfileID != id {
		t.Errorf("ApplyRevision reported profile %q, want %q", applied.Result.ProfileID, id)
	}
	if applied.Result.ActiveConfig != h.paths.ActiveConfigPath {
		t.Errorf("ApplyRevision reported active config %q, want %q",
			applied.Result.ActiveConfig, h.paths.ActiveConfigPath)
	}
	if applied.Result.Restarted {
		t.Error("ApplyRevision claims a restart although no runtime was running")
	}
	if applied.Result.Warning != "" {
		t.Errorf("a clean apply carries warning %q", applied.Result.Warning)
	}
	onDisk, err := os.ReadFile(h.paths.ActiveConfigPath)
	if err != nil {
		t.Fatalf("the active configuration was not written: %v", err)
	}
	if !sameJSON(t, string(onDisk), saved.Revision.ConfigJSON) {
		t.Errorf("active.json = %q, want the applied revision content", string(onDisk))
	}
	if cfg.ActiveConfigPath() != h.paths.ActiveConfigPath {
		t.Errorf("ActiveConfigPath() = %q, want %q", cfg.ActiveConfigPath(), h.paths.ActiveConfigPath)
	}

	active := cfg.GetActiveConfig()
	if active.Error != nil {
		t.Fatalf("GetActiveConfig: %+v", active.Error)
	}
	if !sameJSON(t, active.ConfigJSON, saved.Revision.ConfigJSON) {
		t.Errorf("GetActiveConfig returned %q, want the applied revision", active.ConfigJSON)
	}
	if active.Path != h.paths.ActiveConfigPath {
		t.Errorf("GetActiveConfig path = %q, want %q", active.Path, h.paths.ActiveConfigPath)
	}
	// The payload names the profile the *runtime* is using, which is empty while
	// nothing is running even though a revision is active in the store.
	if active.ProfileID != "" {
		t.Errorf("GetActiveConfig profile = %q while nothing is running, want the empty string", active.ProfileID)
	}

	history := cfg.ListRevisions(id, 0)
	if history.Error != nil {
		t.Fatalf("ListRevisions: %+v", history.Error)
	}
	if len(history.Revisions) != 2 {
		t.Fatalf("got %d revisions, want 2", len(history.Revisions))
	}
	activeSeen := 0
	for _, rev := range history.Revisions {
		if rev.Active {
			activeSeen++
			if rev.ID != saved.Revision.ID {
				t.Errorf("active revision = %q, want %q", rev.ID, saved.Revision.ID)
			}
		}
	}
	if activeSeen != 1 {
		t.Errorf("%d revisions are active, want 1", activeSeen)
	}

	// The draft now derives from the applied revision.
	refreshed := cfg.GetDraft(id)
	if refreshed.Error != nil {
		t.Fatalf("GetDraft after apply: %+v", refreshed.Error)
	}
	if refreshed.Draft.BaseRevisionID != saved.Revision.ID {
		t.Errorf("draft base = %q, want %q", refreshed.Draft.BaseRevisionID, saved.Revision.ID)
	}

	// Rolling back never deletes history: it appends a revision carrying the
	// historical content. The call returns it inactive — applying it is a
	// separate step — so the active file is left exactly as it was.
	rolled := cfg.RollbackToRevision(appconfig.ApplyInput{ProfileID: id, RevisionID: history.Revisions[0].ID})
	if rolled.Error != nil {
		t.Fatalf("RollbackToRevision: %+v", rolled.Error)
	}
	if rolled.Revision.ID == saved.Revision.ID {
		t.Error("rollback reused the existing revision instead of creating one")
	}
	if rolled.Revision.Source != profile.SourceRollback {
		t.Errorf("rollback source = %q, want %q", rolled.Revision.Source, profile.SourceRollback)
	}
	if rolled.Revision.Active {
		t.Error("RollbackToRevision reported the new revision as active before it was applied")
	}
	if !sameJSON(t, rolled.Revision.ConfigJSON, history.Revisions[0].ConfigJSON) {
		t.Error("the rollback revision does not carry the historical content")
	}
	if !strings.Contains(rolled.Revision.Comment, history.Revisions[0].ID) {
		t.Errorf("the rollback comment does not name the source revision: %q", rolled.Revision.Comment)
	}
	if onDisk, err := os.ReadFile(h.paths.ActiveConfigPath); err != nil {
		t.Errorf("reading the active configuration: %v", err)
	} else if !sameJSON(t, string(onDisk), saved.Revision.ConfigJSON) {
		t.Error("rollback rewrote the active configuration without an apply")
	}
	after := cfg.ListRevisions(id, 0)
	if len(after.Revisions) != 3 {
		t.Fatalf("got %d revisions after rollback, want 3", len(after.Revisions))
	}
	stillThere := false
	for _, rev := range after.Revisions {
		if rev.ID == saved.Revision.ID {
			stillThere = true
		}
	}
	if !stillThere {
		t.Error("rollback destroyed the newer revision")
	}
}

func TestConfigFacadeComparesConfigurations(t *testing.T) {
	h := newHarness(t)
	cfg := h.app.ConfigAPI
	id := h.createProfile(t, "Work", "socks-local")
	draft := cfg.GetDraft(id)

	same := cfg.CompareWithActive(id, draft.Draft.BaseRevisionID, draft.Draft.ConfigJSON)
	if same.Error != nil {
		t.Fatalf("CompareWithActive: %+v", same.Error)
	}
	if !same.Diff.Identical {
		t.Errorf("comparing a draft with its own base is not identical: %+v", same.Diff)
	}
	if same.Diff.Added != 0 || same.Diff.Removed != 0 {
		t.Errorf("identical diff has added=%d removed=%d", same.Diff.Added, same.Diff.Removed)
	}

	changed := strings.Replace(draft.Draft.ConfigJSON, `"direct"`, `"other"`, 1)
	diff := cfg.CompareWithActive(id, draft.Draft.BaseRevisionID, changed)
	if diff.Error != nil {
		t.Fatalf("CompareWithActive(changed): %+v", diff.Error)
	}
	if diff.Diff.Identical {
		t.Error("a changed configuration compared identical")
	}
	if diff.Diff.Added == 0 || diff.Diff.Removed == 0 {
		t.Errorf("added=%d removed=%d, want both non-zero", diff.Diff.Added, diff.Diff.Removed)
	}
	if diff.Diff.Left == "" || diff.Diff.Right == "" {
		t.Errorf("diff labels are empty: %+v", diff.Diff)
	}

	if _, err := os.Stat(h.paths.ActiveConfigPath); !os.IsNotExist(err) {
		t.Errorf("comparing wrote the active configuration (stat err = %v)", err)
	}
}

func TestConfigFacadeDetectsAndImportsThePrototypeFiles(t *testing.T) {
	h := newHarness(t)
	cfg := h.app.ConfigAPI

	// The prototype's layout (spec §65): config.json, ui-settings.json and
	// bin/sing-box in the directory it was run from. t.Chdir keeps the search
	// inside a temporary directory, so the developer's own ~/.singboxui is never
	// read and never written.
	proto := t.TempDir()
	legacyConfig := `{"log":{"level":"info"},"inbounds":[{"type":"mixed","tag":"mixed-in","listen":"127.0.0.1","listen_port":2080}],"outbounds":[{"type":"direct","tag":"direct"}]}`
	configPath := filepath.Join(proto, "config.json")
	if err := os.WriteFile(configPath, []byte(legacyConfig), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(proto, "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// The prototype shipped bin/sing-box; the platform name is what the detection
	// looks for, and only a program file can be started on Windows.
	binaryPath := placeFakeSingbox(t, filepath.Join(proto, "bin"), "sing-box")
	settingsPath := filepath.Join(proto, "ui-settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"autoConnect":true}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Chdir(proto)
	// Keep the fallback directory (the real ~/.singboxui) out of this test.
	t.Setenv("HOME", t.TempDir())

	legacy := cfg.DetectLegacyConfig()
	if legacy.Error != nil {
		t.Fatalf("DetectLegacyConfig: %+v", legacy.Error)
	}
	candidate := legacy.Candidate
	if !candidate.Found {
		t.Fatal("the prototype configuration in the working directory was not found")
	}
	if candidate.ConfigPath != configPath {
		t.Errorf("ConfigPath = %q, want %q", candidate.ConfigPath, configPath)
	}
	if !candidate.Valid {
		t.Errorf("a valid sing-box configuration was reported invalid: %v", candidate.Warnings)
	}
	if candidate.Size != len(legacyConfig) {
		t.Errorf("Size = %d, want %d", candidate.Size, len(legacyConfig))
	}
	if candidate.Modified.IsZero() {
		t.Error("the candidate carries no modification time")
	}
	if candidate.SettingsPath != settingsPath {
		t.Errorf("SettingsPath = %q, want %q", candidate.SettingsPath, settingsPath)
	}
	if candidate.AutoConnect == nil || !*candidate.AutoConnect {
		t.Errorf("AutoConnect = %v, want the prototype's true", candidate.AutoConnect)
	}
	if candidate.BinaryPath != binaryPath {
		t.Errorf("BinaryPath = %q, want %q", candidate.BinaryPath, binaryPath)
	}

	// Importing creates a profile from the file; the file itself is only read.
	imported := cfg.ImportLegacyConfig("From the prototype")
	if imported.Error != nil {
		t.Fatalf("ImportLegacyConfig: %+v", imported.Error)
	}
	if imported.Profile.ID == "" {
		t.Fatal("ImportLegacyConfig returned no profile")
	}
	revisions := cfg.ListRevisions(imported.Profile.ID, 0)
	if len(revisions.Revisions) != 1 {
		t.Fatalf("the imported profile has %d revisions, want 1", len(revisions.Revisions))
	}
	if revisions.Revisions[0].Source != profile.SourceMigration {
		t.Errorf("imported revision source = %q, want %q", revisions.Revisions[0].Source, profile.SourceMigration)
	}
	if !revisions.Revisions[0].Active {
		t.Error("the imported revision is not the active one")
	}
	if !sameJSON(t, revisions.Revisions[0].ConfigJSON, legacyConfig) {
		t.Errorf("the imported revision does not match the prototype configuration:\n%s",
			revisions.Revisions[0].ConfigJSON)
	}

	onDisk, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("the prototype configuration was removed: %v", err)
	}
	if string(onDisk) != legacyConfig {
		t.Error("the import modified the prototype configuration")
	}
	entries, err := os.ReadDir(proto)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		switch entry.Name() {
		case "config.json", "ui-settings.json", "bin":
		default:
			t.Errorf("the import left %q in the prototype directory", entry.Name())
		}
	}
	// The imported profile went into the application database instead.
	if _, err := os.Stat(h.paths.DBPath); err != nil {
		t.Errorf("the imported profile was not written to %q: %v", h.paths.DBPath, err)
	}
}

func TestConfigFacadeWithoutAPrototypeFile(t *testing.T) {
	h := newHarness(t)
	cfg := h.app.ConfigAPI
	// Detection falls back to ~/.singboxui, so point HOME at the temporary
	// directory as well; the developer's own prototype files stay out of reach.
	// os.UserHomeDir reads %USERPROFILE% on Windows, so HOME alone left the real
	// prototype directory in reach and the test failed on any Windows machine
	// that has one — CI only passed because a fresh runner has none.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Chdir(t.TempDir())

	legacy := cfg.DetectLegacyConfig()
	if legacy.Error != nil {
		t.Fatalf("DetectLegacyConfig: %+v", legacy.Error)
	}
	if legacy.Candidate.Found || legacy.Candidate.Valid || legacy.Candidate.ConfigPath != "" {
		t.Errorf("an empty directory produced the candidate %+v", legacy.Candidate)
	}

	imported := cfg.ImportLegacyConfig("Prototype")
	if imported.Error == nil {
		t.Fatal("ImportLegacyConfig with no legacy file returned no error")
	}
	if imported.Error.Code != apperr.CodeNotFound {
		t.Errorf("code = %q, want %q", imported.Error.Code, apperr.CodeNotFound)
	}
	if imported.Profile.ID != "" {
		t.Error("a failed import still returned a profile")
	}
}

// -- runtime facade -----------------------------------------------------------

func TestRuntimeFacadeReportsAStoppedRuntime(t *testing.T) {
	h := newHarness(t)
	api := h.app.RuntimeAPI

	status := api.GetRuntimeStatus()
	if status.Error != nil {
		t.Fatalf("GetRuntimeStatus: %+v", status.Error)
	}
	if status.Status.State != domruntime.StateStopped {
		t.Errorf("state = %q, want %q", status.Status.State, domruntime.StateStopped)
	}
	if status.Status.PID != 0 || status.Status.UptimeSeconds != 0 {
		t.Errorf("a stopped runtime reports pid=%d uptime=%d", status.Status.PID, status.Status.UptimeSeconds)
	}
	if status.Status.ActiveProfileID != "" || status.Status.Elevated {
		t.Errorf("a stopped runtime reports profile=%q elevated=%v", status.Status.ActiveProfileID, status.Status.Elevated)
	}
	if status.ShuttingDown {
		t.Error("ShuttingDown = true on a running application")
	}
	if status.TrafficAvailable {
		t.Error("TrafficAvailable = true before the collector is started")
	}

	logs := api.GetLogs(appruntime.LogQuery{})
	if logs.Error != nil {
		t.Fatalf("GetLogs: %+v", logs.Error)
	}
	if len(logs.Records) != 0 {
		t.Errorf("got %d log records from a stopped runtime, want 0", len(logs.Records))
	}
	if tail := api.TailLogs(10); tail.Error != nil || len(tail.Records) != 0 {
		t.Errorf("TailLogs = %+v, want an empty buffer without error", tail)
	}
	if clear := api.ClearLogs(); clear.Error != nil {
		t.Errorf("ClearLogs: %+v", clear.Error)
	}

	if stopped := api.StopRuntime(); stopped.Error != nil {
		if stopped.Error.Code != apperr.CodeRuntimeNotRunning {
			t.Errorf("StopRuntime on a stopped runtime = %q, want nil or RUNTIME_NOT_RUNNING", stopped.Error.Code)
		}
	}
}

func TestRuntimeFacadeRefusesToStartWithoutABinary(t *testing.T) {
	h := newHarness(t)
	api := h.app.RuntimeAPI
	id := h.createProfile(t, "Work", "socks-local")

	started := api.StartRuntime(id)
	if started.Error == nil {
		t.Fatal("StartRuntime succeeded although no sing-box binary exists")
	}
	if started.Status.State == domruntime.StateRunning {
		t.Error("the runtime reports RUNNING after a failed start")
	}
	if started.Error.Operation == "" {
		t.Error("the start failure carries no operation for diagnostics")
	}
	// The security-relevant half of spec §27: an unusable configuration must be
	// rejected before any elevation is requested.
	if n := h.plat.priv.startCount(); n != 0 {
		t.Errorf("the privileged launcher was called %d times for a start that cannot succeed", n)
	}

	if got := api.StartRuntime("ghost-profile"); got.Error == nil {
		t.Error("StartRuntime(unknown profile) returned no error")
	}

	if restarted := api.RestartRuntime(""); restarted.Error == nil {
		// Restarting nothing is allowed to fail; it must not report RUNNING.
		if restarted.Status.State == domruntime.StateRunning {
			t.Error("RestartRuntime with nothing running reported a running runtime")
		}
	}
}

// -- traffic facade -----------------------------------------------------------

// clashConnections is the payload the collector reads from the local Clash API.
const clashConnections = `{
  "downloadTotal": 4096,
  "uploadTotal": 2048,
  "connections": [
    {
      "id": "c1",
      "upload": 512,
      "download": 1024,
      "start": "2026-09-11T10:00:00Z",
      "rule": "MATCH",
      "chains": ["proxy"],
      "metadata": {"host": "example.com", "destinationIP": "5.6.7.8", "network": "tcp", "type": "HTTP"}
    },
    {
      "id": "c2",
      "upload": 256,
      "download": 512,
      "start": "2026-09-11T10:00:01Z",
      "rule": "DOMAIN-SUFFIX",
      "chains": ["proxy"],
      "metadata": {"host": "api.example.com", "destinationIP": "5.6.7.9", "network": "tcp", "type": "HTTPS"}
    }
  ]
}`

// startClashAPI serves the connections payload and reports the Authorization
// header it saw for every request.
func startClashAPI(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	seen := make([]string, 0, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		if r.URL.Path != "/connections" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, clashConnections)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func waitUntil(t *testing.T, what string, pred func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pred() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s did not happen within the deadline", what)
}

func TestTrafficFacadeReportsAStoppedCollector(t *testing.T) {
	h := newHarness(t)

	got := h.app.TrafficAPI.GetTrafficSnapshot()
	if got.Error != nil {
		t.Fatalf("GetTrafficSnapshot: %+v", got.Error)
	}
	if got.Collector {
		t.Error("Collector = true before the runtime started")
	}
	if got.Snapshot.Available {
		t.Error("the snapshot is available although nothing is running")
	}
	if got.Snapshot.Connections != 0 || got.Snapshot.UploadTotal != 0 {
		t.Errorf("a stopped collector reported traffic: %+v", got.Snapshot)
	}
}

func TestTrafficCollectorFollowsTheRuntimeLifecycle(t *testing.T) {
	h := newHarness(t)
	srv, authSeen := startClashAPI(t)
	observer := &runtimeObserver{app: h.app}

	// A runtime that comes up without a Clash API must not start the collector.
	observer.RuntimeRunning(context.Background(), appruntime.RunInfo{
		ClashAPIEnabled: false,
		ProfileID:       "p1",
		RevisionID:      "r1",
	})
	if h.app.traffic.Running() {
		t.Fatal("the collector started although the Clash API is disabled")
	}

	observer.RuntimeRunning(context.Background(), appruntime.RunInfo{
		ClashAPIEnabled: true,
		ClashAPIBaseURL: srv.URL,
		ClashAPISecret:  "topsecret",
		ProfileID:       "p1",
		RevisionID:      "r1",
	})
	defer h.app.traffic.Stop()

	waitUntil(t, "the collector to publish an available snapshot", func() bool {
		return h.app.traffic.Latest().Available
	})
	snapshot := h.app.traffic.Latest()
	if snapshot.Connections != 2 {
		t.Errorf("Connections = %d, want 2", snapshot.Connections)
	}
	if snapshot.UploadTotal != 2048 || snapshot.DownloadTotal != 4096 {
		t.Errorf("totals = %d/%d, want 2048/4096", snapshot.UploadTotal, snapshot.DownloadTotal)
	}
	if snapshot.ProfileID != "p1" || snapshot.RevisionID != "r1" {
		t.Errorf("snapshot identifies profile %q revision %q, want p1/r1",
			snapshot.ProfileID, snapshot.RevisionID)
	}

	// The secret never goes into the snapshot, only into the request header.
	for _, auth := range authSeen() {
		if auth != "Bearer topsecret" {
			t.Errorf("Authorization sent to the control API = %q, want %q", auth, "Bearer topsecret")
		}
	}
	if len(authSeen()) == 0 {
		t.Error("the collector never called the control API")
	}

	// The facade reflects the collector, and the injected emitter delivered the
	// same snapshot to the frontend.
	payload := h.app.TrafficAPI.GetTrafficSnapshot()
	if payload.Error != nil {
		t.Fatalf("GetTrafficSnapshot while collecting: %+v", payload.Error)
	}
	if !payload.Collector || !payload.Snapshot.Available {
		t.Errorf("payload = %+v, want a collecting, available snapshot", payload)
	}
	if payload.Snapshot.Connections != 2 {
		t.Errorf("facade snapshot has %d connections, want 2", payload.Snapshot.Connections)
	}

	delivered := h.rec.waitFor(t, events.TrafficSnapshot, func(p any) bool {
		sn, ok := p.(traffic.Snapshot)
		return ok && sn.Available && sn.Connections == 2
	})
	sn, ok := delivered.(traffic.Snapshot)
	if !ok {
		t.Fatalf("delivered payload is %T, want traffic.Snapshot", delivered)
	}
	if sn.UploadTotal != 2048 || sn.DownloadTotal != 4096 {
		t.Errorf("delivered snapshot totals = %d/%d, want 2048/4096", sn.UploadTotal, sn.DownloadTotal)
	}
	if len(sn.Active) != 2 {
		t.Errorf("delivered snapshot has %d active connections, want 2", len(sn.Active))
	}

	// A target without an endpoint is refused rather than polled forever.
	observer.RuntimeRunning(context.Background(), appruntime.RunInfo{
		ClashAPIEnabled: true,
		ClashAPIBaseURL: "   ",
		ProfileID:       "p1",
	})
	waitUntil(t, "the collector to stop for an empty endpoint", func() bool {
		return !h.app.traffic.Running()
	})

	// Stopping the runtime stops the collector.
	h.app.traffic.Start(context.Background(), traffic.Target{BaseURL: srv.URL})
	waitUntil(t, "the collector to restart", func() bool { return h.app.traffic.Running() })
	observer.RuntimeStopped()
	waitUntil(t, "the collector to stop with the runtime", func() bool {
		return !h.app.traffic.Running()
	})
}

// -- settings facade ----------------------------------------------------------

func TestSettingsFacadeRoundTrip(t *testing.T) {
	h := newHarness(t)
	api := h.app.SettingsAPI
	ctx := context.Background()

	initial := api.GetSettings()
	if initial.Error != nil {
		t.Fatalf("GetSettings: %+v", initial.Error)
	}
	wantDefaults := domsettings.Default()
	if initial.State.Values != wantDefaults {
		t.Errorf("initial settings = %+v, want %+v", initial.State.Values, wantDefaults)
	}
	if initial.State.DataDir != h.paths.DataDir || initial.State.LogPath != h.paths.LogPath {
		t.Errorf("state paths = %q/%q, want %q/%q",
			initial.State.DataDir, initial.State.LogPath, h.paths.DataDir, h.paths.LogPath)
	}
	if !initial.State.Autostart.Supported {
		t.Error("autostart is reported unsupported on a platform that supports it")
	}
	if initial.State.Autostart.Enabled {
		t.Error("autostart starts out enabled")
	}

	dark := domsettings.ThemeDark
	debug := domsettings.LogDebug
	updated := api.UpdateSettings(settings.UpdateInput{Theme: &dark, LogLevel: &debug})
	if updated.Error != nil {
		t.Fatalf("UpdateSettings: %+v", updated.Error)
	}
	if updated.State.Values.Theme != domsettings.ThemeDark || updated.State.Values.LogLevel != domsettings.LogDebug {
		t.Errorf("after update: %+v", updated.State.Values)
	}
	persisted, err := h.store.LoadSettings(ctx)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if persisted.Theme != domsettings.ThemeDark || persisted.LogLevel != domsettings.LogDebug {
		t.Errorf("stored settings = %+v, want the update", persisted)
	}
	if persisted.AutoConnect != wantDefaults.AutoConnect {
		t.Error("a partial update reset a field the caller did not mention")
	}
	// A partial update leaves the untouched fields alone.
	reloaded := api.GetSettings()
	if reloaded.State.Values.Theme != domsettings.ThemeDark {
		t.Errorf("reread theme = %q, want %q", reloaded.State.Values.Theme, domsettings.ThemeDark)
	}

	bogus := domsettings.Theme("neon")
	rejected := api.UpdateSettings(settings.UpdateInput{Theme: &bogus})
	if rejected.Error == nil {
		t.Fatal("UpdateSettings accepted an unknown theme")
	}
	if rejected.Error.Code != apperr.CodeInvalidArgument {
		t.Errorf("a rejected theme carries code %q, want %q", rejected.Error.Code, apperr.CodeInvalidArgument)
	}
	after, err := h.store.LoadSettings(ctx)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if after.Theme != domsettings.ThemeDark {
		t.Errorf("a rejected update changed the stored theme to %q", after.Theme)
	}
}

func TestSettingsFacadeAutostart(t *testing.T) {
	h := newHarness(t)
	api := h.app.SettingsAPI

	on := api.SetAutostart(true)
	if on.Error != nil {
		t.Fatalf("SetAutostart(true): %+v", on.Error)
	}
	if !on.State.Autostart.Enabled || !on.State.Values.AutoStartApplication {
		t.Errorf("after enabling: %+v", on.State)
	}
	if h.plat.auto.execPath != h.app.deps.ExecPath {
		t.Errorf("autostart registered %q, want this executable %q", h.plat.auto.execPath, h.app.deps.ExecPath)
	}

	off := api.SetAutostart(false)
	if off.Error != nil {
		t.Fatalf("SetAutostart(false): %+v", off.Error)
	}
	if off.State.Autostart.Enabled || off.State.Values.AutoStartApplication {
		t.Errorf("after disabling: %+v", off.State)
	}

	// The prototype's entry lives outside the typed model and can be removed.
	h.plat.auto.legacy = "com.larffxx.singboxui.prototype"
	cleaned := api.RemoveLegacyAutostart()
	if cleaned.Error != nil {
		t.Fatalf("RemoveLegacyAutostart: %+v", cleaned.Error)
	}
	if h.plat.auto.legacy != "" {
		t.Errorf("the legacy entry survived: %q", h.plat.auto.legacy)
	}
	if cleaned.State.Autostart.LegacyEntry != "" {
		t.Errorf("the state still reports legacy entry %q", cleaned.State.Autostart.LegacyEntry)
	}
}

func TestSettingsFacadeClearsALastProfileThatNoLongerExists(t *testing.T) {
	h := newHarness(t)
	api := h.app.SettingsAPI
	ctx := context.Background()

	ghost := "profile-that-was-deleted"
	updated := api.UpdateSettings(settings.UpdateInput{LastProfileID: &ghost})
	if updated.Error != nil {
		t.Fatalf("UpdateSettings: %+v", updated.Error)
	}
	if updated.State.Values.LastProfileID != "" {
		t.Errorf("LastProfileID = %q, want it cleared because the profile does not exist",
			updated.State.Values.LastProfileID)
	}
	stored, err := h.store.LoadSettings(ctx)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if stored.LastProfileID != "" {
		t.Errorf("the dangling id was persisted as %q", stored.LastProfileID)
	}

	// A profile that does exist is remembered.
	id := h.createProfile(t, "Work", "socks-local")
	kept := api.UpdateSettings(settings.UpdateInput{LastProfileID: &id})
	if kept.Error != nil {
		t.Fatalf("UpdateSettings(existing): %+v", kept.Error)
	}
	if kept.State.Values.LastProfileID != id {
		t.Errorf("LastProfileID = %q, want %q", kept.State.Values.LastProfileID, id)
	}
}

func TestSettingsFacadeRefusesAutostartOnAnUnsupportedPlatform(t *testing.T) {
	h := newHarness(t)
	h.plat.auto.supported = false

	got := h.app.SettingsAPI.SetAutostart(true)
	if got.Error == nil {
		t.Fatal("SetAutostart succeeded on a platform without autostart support")
	}
	if got.Error.Code != apperr.CodeInvalidArgument {
		t.Errorf("code = %q, want %q", got.Error.Code, apperr.CodeInvalidArgument)
	}
	if got.State.Autostart.Supported {
		t.Error("the state claims autostart is supported")
	}
	if n := h.plat.auto.enableCalls(); n != 0 {
		t.Errorf("the platform adapter was asked to enable autostart %d times", n)
	}
	if got.State.Autostart.Enabled {
		t.Error("the state reports autostart as enabled although the request was refused")
	}
	// Note: the preference itself is written before the platform is consulted,
	// so the frontend must branch on this error, not on the returned values.
}

func TestSettingsFacadeReportsAutostartFailures(t *testing.T) {
	h := newHarness(t)
	h.plat.auto.enableErr = errors.New("launchctl refused the job")

	got := h.app.SettingsAPI.SetAutostart(true)
	if got.Error == nil {
		t.Fatal("SetAutostart hid a platform failure")
	}
	if got.Error.Code != apperr.CodeInternal {
		t.Errorf("code = %q, want %q", got.Error.Code, apperr.CodeInternal)
	}
	if h.plat.auto.enabled {
		t.Error("the adapter reports autostart as enabled after a failed enable")
	}
	if got.State.Autostart.Enabled {
		t.Error("the state reports autostart as enabled after a failure")
	}
	if !got.State.Autostart.Supported {
		t.Error("a failed enable made a supported platform look unsupported")
	}
}

func TestSettingsFacadeEnvironment(t *testing.T) {
	h := newHarness(t)

	got := h.app.SettingsAPI.GetEnvironment()
	if got.Error != nil {
		t.Fatalf("GetEnvironment: %+v", got.Error)
	}
	env := got.Environment
	if env.Version != testVersion {
		t.Errorf("version = %q, want %q", env.Version, testVersion)
	}
	if env.OS != "darwin" || env.Arch != "arm64" {
		t.Errorf("platform = %s/%s, want darwin/arm64", env.OS, env.Arch)
	}
	if !env.Supported {
		t.Error("a supported platform is reported unsupported")
	}
	if env.UnsupportedReason != "" {
		t.Errorf("a supported platform carries reason %q", env.UnsupportedReason)
	}
	if env.DataDir != h.paths.DataDir || env.ConfigDir != h.paths.ConfigDir ||
		env.ActiveConfigPath != h.paths.ActiveConfigPath || env.BinDir != h.paths.BinDir ||
		env.RuntimeDir != h.paths.RuntimeDir || env.LogPath != h.paths.LogPath {
		t.Errorf("environment paths do not match the injected layout: %+v", env)
	}
	if diff := compareStrings(env.ShareSchemes, share.Supported()); diff != "" {
		t.Errorf("share schemes %v, want %v", env.ShareSchemes, share.Supported())
	}
	// Migration mirrors whatever the prototype left on this machine, so assert
	// the payload is coherent instead of assuming the machine is clean.
	if env.Migration != nil && env.Migration.Found && env.Migration.ConfigPath == "" {
		t.Errorf("the migration reports a found candidate without a path: %+v", env.Migration)
	}
}

func TestSettingsFacadeReportsAnUnsupportedPlatform(t *testing.T) {
	h := newHarness(t)
	h.plat.info = platform.Info{OS: "darwin", Arch: "386", Supported: false, UnsupportedReason: "no build for this architecture"}

	got := h.app.SettingsAPI.GetEnvironment()
	if got.Error != nil {
		t.Fatalf("GetEnvironment: %+v", got.Error)
	}
	if got.Environment.Supported {
		t.Error("the environment claims support on an unsupported platform")
	}
	if got.Environment.UnsupportedReason == "" {
		t.Error("the refusal carries no reason for the user")
	}
}

func compareStrings(got, want []string) string {
	if len(got) != len(want) {
		return "length differs"
	}
	for i := range got {
		if got[i] != want[i] {
			return "element " + got[i] + " != " + want[i]
		}
	}
	return ""
}

// -- binary facade ------------------------------------------------------------

func TestBinaryFacadeStatusAndSourceSelection(t *testing.T) {
	h := newHarness(t)
	api := h.app.BinaryAPI

	status := api.GetBinaryStatus()
	if status.Error != nil {
		t.Fatalf("GetBinaryStatus: %+v", status.Error)
	}
	if status.Status.Source != domsettings.BinaryManaged {
		t.Errorf("source = %q, want %q", status.Status.Source, domsettings.BinaryManaged)
	}
	if status.Status.Platform != "darwin/arm64" {
		t.Errorf("platform = %q, want darwin/arm64", status.Status.Platform)
	}
	if status.Status.Managed.Installed {
		t.Error("a managed binary is reported installed although BinDir is empty")
	}
	if status.Status.ActiveOK {
		t.Error("ActiveOK = true without a binary")
	}
	if status.Status.ActiveError == "" {
		t.Error("no explanation was given for the missing binary")
	}
	if status.Status.Unsupported {
		t.Error("darwin/arm64 is reported unsupported")
	}

	// A custom source must name an executable.
	if got := api.SetBinarySource("custom", "   "); got.Error == nil {
		t.Error("SetBinarySource(custom, empty) succeeded")
	} else if got.Error.Code != apperr.CodeBinarySourceInvalid {
		t.Errorf("code = %q, want %q", got.Error.Code, apperr.CodeBinarySourceInvalid)
	}

	if got := api.SetBinarySource("nonsense", ""); got.Error == nil {
		t.Error("SetBinarySource accepted an unknown source")
	} else if got.Error.Code != apperr.CodeBinarySourceInvalid {
		t.Errorf("SetBinarySource(unknown) code = %q, want %q", got.Error.Code, apperr.CodeBinarySourceInvalid)
	}

	// A custom path that is not a sing-box is refused, and the previous
	// selection stays in force.
	// A program that exists but reports junk instead of a sing-box version. It is
	// the same stand-in with a scenario, not a shell script, so the case exists on
	// every platform.
	t.Setenv(faketest.EnvScenario, faketest.ScenarioVersionFail)
	impostor := placeFakeSingbox(t, h.dir, "impostor")
	if got := api.SetBinarySource("custom", impostor); got.Error == nil {
		t.Error("SetBinarySource accepted a program that is not sing-box")
	} else if got.Error.Code != apperr.CodeBinarySourceInvalid {
		t.Errorf("SetBinarySource(impostor) code = %q, want %q", got.Error.Code, apperr.CodeBinarySourceInvalid)
	}
	if after := api.GetBinaryStatus(); after.Status.Source != domsettings.BinaryManaged {
		t.Errorf("a refused selection changed the source to %q", after.Status.Source)
	}

	// Selecting a custom binary probes it first, so the file has to be a working
	// sing-box.
	os.Unsetenv(faketest.EnvScenario)
	custom := placeFakeSingbox(t, h.dir, "sing-box")
	switched := api.SetBinarySource("custom", "  "+custom+"  ")
	if switched.Error != nil {
		t.Fatalf("SetBinarySource(custom): %+v", switched.Error)
	}
	if switched.Status.Source != domsettings.BinaryCustom {
		t.Errorf("source = %q, want %q", switched.Status.Source, domsettings.BinaryCustom)
	}
	if switched.Status.CustomPath != custom {
		t.Errorf("custom path = %q, want the trimmed %q", switched.Status.CustomPath, custom)
	}
	reread := api.GetBinaryStatus()
	if reread.Status.CustomPath != custom || reread.Status.Source != domsettings.BinaryCustom {
		t.Errorf("the source selection did not persist: %+v", reread.Status)
	}
}

func TestBinaryFacadeProbesAnExecutable(t *testing.T) {
	h := newHarness(t)
	api := h.app.BinaryAPI

	t.Run("missing file", func(t *testing.T) {
		got := api.ProbeBinary("")
		if got.Error == nil {
			t.Fatal("ProbeBinary(\"\") succeeded")
		}
		if got.Error.Code != apperr.CodeBinaryNotFound {
			t.Errorf("code = %q, want %q", got.Error.Code, apperr.CodeBinaryNotFound)
		}
	})

	t.Run("a file that is not executable", func(t *testing.T) {
		path := filepath.Join(h.dir, "notes.txt")
		if err := os.WriteFile(path, []byte("sing-box version 9.9.9\n"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		got := api.ProbeBinary(path)
		if got.Error == nil {
			t.Fatal("ProbeBinary accepted a non-executable file")
		}
	})

	t.Run("the wrong program with an executable bit", func(t *testing.T) {
		path := filepath.Join(h.dir, "not-singbox")
		if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'hello from another tool'\n"), 0o755); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		got := api.ProbeBinary(path)
		if got.Error == nil {
			t.Fatal("ProbeBinary accepted a program that is not sing-box")
		}
	})

	t.Run("a sing-box reporting its version", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			// The fixture records its own arguments, which only a shell script can
			// do here, and Windows starts nothing but a program file. The probe
			// arguments are asserted on the platforms that can run it; the version
			// a probe reads is asserted by the tests above, which use the
			// package's stand-in sing-box.
			t.Skip("recording the probe arguments needs a script, which Windows cannot start")
		}
		marker := filepath.Join(h.dir, "args.txt")
		path := filepath.Join(h.dir, "sing-box")
		script := "#!/bin/sh\nprintf '%s' \"$*\" > " + marker + "\necho 'sing-box version 9.9.9'\n"
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		got := api.ProbeBinary(path)
		if got.Error != nil {
			t.Fatalf("ProbeBinary: %+v", got.Error)
		}
		if got.Version.Major != 9 || got.Version.Minor != 9 || got.Version.Patch != 9 {
			t.Errorf("version = %+v, want 9.9.9", got.Version)
		}
		if got.Version.Raw != "9.9.9" {
			t.Errorf("raw version = %q, want %q", got.Version.Raw, "9.9.9")
		}
		if !got.Version.Stable() || got.Version.Pre != "" {
			t.Errorf("9.9.9 was not treated as a stable release: %+v", got.Version)
		}
		args, err := os.ReadFile(marker)
		if err != nil {
			t.Fatalf("the probe did not run the binary: %v", err)
		}
		if !strings.Contains(string(args), "version") {
			t.Errorf("the probe ran the binary with %q, want a version subcommand", string(args))
		}
	})
}

func TestBinaryFacadeWithoutAReleaseClient(t *testing.T) {
	h := newHarness(t)
	api := h.app.BinaryAPI

	// A nil release client disables update checks; the facade must say so
	// instead of panicking (spec §77).
	check := api.CheckForUpdates()
	if check.Error == nil {
		t.Error("CheckForUpdates succeeded without a release client")
	}
	if !api.LastUpdateCheck().Check.CheckedAt.IsZero() {
		t.Error("LastUpdateCheck reports a completed check although none ran")
	}
	if api.LastUpdateCheck().Check.UpdateAvailable {
		t.Error("an update is reported available although no check ran")
	}

	install := api.InstallStableUpdate()
	if install.Error == nil {
		t.Error("InstallStableUpdate succeeded without a release client")
	}
}

// -- share facade -------------------------------------------------------------

func TestShareFacadeParsesLinks(t *testing.T) {
	h := newHarness(t)
	api := h.app.ShareAPI
	const link = "vless://11111111-1111-1111-1111-111111111111@vpn.example.com:443?encryption=none&type=tcp&security=tls&sni=vpn.example.com#Tokyo"

	if got := api.SupportedShareSchemes(); compareStrings(got, share.Supported()) != "" {
		t.Errorf("SupportedShareSchemes() = %v, want %v", got, share.Supported())
	}

	parsed := api.ParseShareLink(link)
	if parsed.Error != nil {
		t.Fatalf("ParseShareLink: %+v", parsed.Error)
	}
	if parsed.Parsed == nil {
		t.Fatal("ParseShareLink returned no candidate")
	}
	if parsed.Parsed.Kind != "vless" {
		t.Errorf("kind = %q, want vless", parsed.Parsed.Kind)
	}
	if parsed.Parsed.Outbound["type"] != "vless" {
		t.Errorf("outbound type = %v, want vless", parsed.Parsed.Outbound["type"])
	}
	if parsed.Parsed.Tag == "" || parsed.Parsed.DisplayName != "Tokyo" {
		t.Errorf("parsed = %+v, want a tag and the display name Tokyo", parsed.Parsed)
	}

	bad := api.ParseShareLink("this is not a link")
	if bad.Error == nil {
		t.Error("ParseShareLink accepted plain text")
	}
	if bad.Parsed != nil {
		t.Error("a failed parse still returned a candidate")
	}

	batch := api.ParseShareLinks(link + "\n" + "garbage line\n\n" + link)
	if batch.Error != nil {
		t.Fatalf("ParseShareLinks: %+v", batch.Error)
	}
	if len(batch.Parsed) != 2 {
		t.Errorf("parsed %d links, want 2", len(batch.Parsed))
	}
	if len(batch.Errors) == 0 {
		t.Error("the unusable line produced no message")
	}
	if batch.Parsed[0].Tag == batch.Parsed[1].Tag {
		t.Errorf("two links of the same hostname share the tag %q", batch.Parsed[0].Tag)
	}
}

func TestShareFacadeBuildsLinks(t *testing.T) {
	h := newHarness(t)
	api := h.app.ShareAPI

	parsed := api.ParseShareLink("vless://11111111-1111-1111-1111-111111111111@vpn.example.com:443?encryption=none&type=tcp&security=tls&sni=vpn.example.com#Tokyo")
	if parsed.Error != nil {
		t.Fatalf("ParseShareLink: %+v", parsed.Error)
	}
	raw, err := json.Marshal(parsed.Parsed.Outbound)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	built := api.BuildShareLink(string(raw))
	if built.Error != nil {
		t.Fatalf("BuildShareLink: %+v", built.Error)
	}
	if !strings.HasPrefix(built.Link, "vless://") {
		t.Errorf("link = %q, want a vless:// link", built.Link)
	}
	again, err := share.Parse(built.Link)
	if err != nil {
		t.Fatalf("the built link does not parse: %v", err)
	}
	if again.Outbound["server"] != "vpn.example.com" {
		t.Errorf("round-tripped server = %v, want vpn.example.com", again.Outbound["server"])
	}

	if got := api.BuildShareLink("{not json"); got.Error == nil {
		t.Error("BuildShareLink accepted invalid JSON")
	} else if got.Error.Code != apperr.CodeShareLinkInvalid {
		t.Errorf("code = %q, want %q", got.Error.Code, apperr.CodeShareLinkInvalid)
	}

	unsupported := api.BuildShareLink(`{"type":"direct","tag":"direct"}`)
	if unsupported.Error == nil {
		t.Error("BuildShareLink produced a link for an outbound that cannot be shared")
	}
}

// -- startup, notices and shutdown -------------------------------------------

func TestOnStartupWarnsAboutAnUnsupportedPlatform(t *testing.T) {
	h := newHarness(t)
	h.plat.info = platform.Info{
		OS:                "linux",
		Arch:              "amd64",
		Supported:         false,
		UnsupportedReason: "SingBoxUI targets Windows and macOS only",
	}

	h.app.OnStartup(context.Background())
	defer h.app.OnShutdown(context.Background())

	payload := h.rec.waitFor(t, events.AppNotice, func(p any) bool {
		notice, ok := p.(events.Notice)
		return ok && notice.Title == "Unsupported operating system"
	})
	notice, ok := payload.(events.Notice)
	if !ok {
		t.Fatalf("notice payload is %T, want events.Notice", payload)
	}
	if notice.Level != "warning" {
		t.Errorf("level = %q, want warning", notice.Level)
	}
	if !notice.Persistent {
		t.Error("the warning is transient; the user may never see it")
	}
	if len(notice.Details) == 0 || !strings.Contains(notice.Details[0], "Windows and macOS") {
		t.Errorf("the warning does not explain the reason: %+v", notice.Details)
	}
	if notice.OccurredAt == "" {
		t.Error("the warning carries no timestamp")
	}
	if notice.Code != "" {
		t.Errorf("an informational warning carries code %q", notice.Code)
	}
}

func TestOnStartupAutoConnectFailureBecomesANotice(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	id := h.createProfile(t, "Work", "socks-local")

	values, err := h.store.LoadSettings(ctx)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	values.AutoConnect = true
	values.LastProfileID = id
	if err := h.store.SaveSettings(ctx, values); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	h.app.OnStartup(ctx)
	defer h.app.OnShutdown(ctx)

	// Auto-connect cannot succeed without a binary, and the failure must reach
	// the user instead of only the log file.
	payload := h.rec.waitFor(t, events.AppNotice, func(p any) bool {
		notice, ok := p.(events.Notice)
		return ok && notice.Title == "Auto-connect failed"
	})
	notice, ok := payload.(events.Notice)
	if !ok {
		t.Fatalf("notice payload is %T, want events.Notice", payload)
	}
	if notice.Level != "error" {
		t.Errorf("level = %q, want error", notice.Level)
	}
	if notice.Code == "" {
		t.Error("the failure notice carries no code for the frontend")
	}
	if notice.Message == "" {
		t.Error("the failure notice carries no message")
	}
	// The privileged launcher must not have been invoked by that failure.
	if n := h.plat.priv.startCount(); n != 0 {
		t.Errorf("the privileged launcher was called %d times by a failed auto-connect", n)
	}
	if status := h.app.RuntimeAPI.GetRuntimeStatus(); status.Status.State == domruntime.StateRunning {
		t.Error("the runtime reports RUNNING after a failed auto-connect")
	}
}

func TestShutdownSequenceGuardsEveryMutation(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	id := h.createProfile(t, "Work", "socks-local")

	h.app.OnShutdown(ctx)
	if !h.app.ShuttingDown() {
		t.Fatal("ShuttingDown() = false after OnShutdown")
	}
	if err := h.app.ShutdownError(); err != nil {
		t.Errorf("ShutdownError() = %v, want nil for a clean shutdown", err)
	}
	if h.app.traffic.Running() {
		t.Error("the traffic collector survived shutdown")
	}

	// Reads that only touch memory keep working.
	if got := h.app.TrafficAPI.GetTrafficSnapshot(); got.Error != nil {
		t.Errorf("GetTrafficSnapshot after shutdown: %+v", got.Error)
	}
	if got := h.app.RuntimeAPI.GetRuntimeStatus(); got.ShuttingDown != true {
		t.Error("the runtime snapshot does not report the shutting-down state")
	}

	// Mutations are refused with a code the frontend can branch on.
	tests := []struct {
		name string
		call func() *apperr.Error
	}{
		{"start", func() *apperr.Error { return h.app.RuntimeAPI.StartRuntime(id).Error }},
		{"stop", func() *apperr.Error { return h.app.RuntimeAPI.StopRuntime().Error }},
		{"restart", func() *apperr.Error { return h.app.RuntimeAPI.RestartRuntime(id).Error }},
		{"install update", func() *apperr.Error { return h.app.BinaryAPI.InstallStableUpdate().Error }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("a mutating call was accepted after shutdown")
			}
			if err.Code != apperr.CodeAppShuttingDown {
				t.Errorf("code = %q, want %q", err.Code, apperr.CodeAppShuttingDown)
			}
		})
	}
	if n := h.plat.priv.startCount(); n != 0 {
		t.Errorf("the privileged launcher was called %d times after shutdown", n)
	}

	// Database-backed reads fail loudly rather than silently returning nothing.
	if got := h.app.ProfileAPI.ListProfiles(); got.Error == nil {
		t.Error("ListProfiles succeeded against a closed database")
	}

	// Shutdown is idempotent.
	h.app.OnShutdown(ctx)
	if !h.app.ShuttingDown() {
		t.Error("the second shutdown cleared the flag")
	}
}

// -- emitter ------------------------------------------------------------------

func TestEmitterDropsEventsUntilItIsAttached(t *testing.T) {
	var mu sync.Mutex
	var got []recordedEvent
	emitter := NewEmitterWithSink(quietLogger(), func(_ context.Context, name string, payload any) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, recordedEvent{name: name, payload: payload})
	})

	// Before the Wails context exists, emitting must be a no-op and must never
	// panic: application services emit during startup.
	emitter.Emit(events.ProfilesChanged, map[string]any{"early": true})
	emitter.Detach()
	emitter.Emit(events.ProfilesChanged, nil)
	mu.Lock()
	if len(got) != 0 {
		t.Errorf("a detached emitter delivered %d events", len(got))
	}
	mu.Unlock()

	emitter.Attach(context.Background())
	emitter.Emit(events.ProfilesChanged, map[string]any{"profileId": "p1"})
	mu.Lock()
	if len(got) != 1 {
		mu.Unlock()
		t.Fatalf("an attached emitter delivered %d events, want 1", len(got))
	}
	if got[0].name != events.ProfilesChanged {
		t.Errorf("event name = %q, want %q", got[0].name, events.ProfilesChanged)
	}
	mu.Unlock()

	// Detach is the shutdown path: nothing may be delivered afterwards.
	emitter.Detach()
	emitter.Emit(events.ProfilesChanged, nil)
	mu.Lock()
	if len(got) != 1 {
		t.Errorf("a detached emitter delivered %d events, want 1", len(got))
	}
	mu.Unlock()
}

func TestEmitterSurvivesAPanickingSink(t *testing.T) {
	emitter := NewEmitterWithSink(quietLogger(), func(context.Context, string, any) {
		panic("the frontend bridge exploded")
	})
	emitter.Attach(context.Background())
	defer emitter.Detach()

	// Emit is called from application code that must not take the process down.
	emitter.Emit(events.AppNotice, events.Notice{Level: "info", Title: "hello"})
}

func TestEmitterIsSafeUnderConcurrentUse(t *testing.T) {
	emitter := NewEmitterWithSink(quietLogger(), func(context.Context, string, any) {})
	emitter.Attach(context.Background())

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				emitter.Emit(events.TrafficSnapshot, map[string]any{"n": j})
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				emitter.Detach()
				emitter.Attach(context.Background())
			}
		}()
	}
	wg.Wait()
	emitter.Detach()
}
