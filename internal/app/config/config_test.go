package config

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	domainconfig "github.com/larffxx/singboxui/internal/domain/config"
	"github.com/larffxx/singboxui/internal/domain/profile"
	domruntime "github.com/larffxx/singboxui/internal/domain/runtime"
	"github.com/larffxx/singboxui/internal/events"
	"github.com/larffxx/singboxui/internal/platform"
	"github.com/larffxx/singboxui/internal/singbox"
	"github.com/larffxx/singboxui/internal/singbox/faketest"
)

// The config use cases are the transactional heart of the application: an
// invalid draft must never reach the disk, a saved revision must never change,
// and a failed apply must leave the machine exactly as it was (spec §12, §13,
// §14, §15). Every test below asserts one of those properties against real
// bytes on disk, the real `sing-box check` contract (through the fixture from
// internal/singbox/faketest) and a store that records what it was asked to do.

// --- the fake sing-box executable (spec §67) --------------------------------

// fakeSingBoxPath is the fixture built once for this package's tests. Naming it
// "sing-box" makes the adapter see exactly what it sees in production, and no
// scenario in this file ever runs a real sing-box or needs root.
var fakeSingBoxPath string

func TestMain(m *testing.M) {
	path, cleanup, err := buildFakeSingBox()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config: test setup failed:", err)
		os.Exit(1)
	}
	fakeSingBoxPath = path
	code := m.Run()
	cleanup()
	os.Exit(code)
}

func buildFakeSingBox() (string, func(), error) {
	root, err := moduleRoot()
	if err != nil {
		return "", func() {}, err
	}
	goTool, err := goBinary()
	if err != nil {
		return "", func() {}, err
	}
	dir, err := os.MkdirTemp("", "config-fakesingbox-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { os.RemoveAll(dir) }

	// The fixture carries the platform's executable name: Windows only starts a
	// file whose extension is in PATHEXT, so a fixture called "sing-box" cannot
	// be run there at all, and every test that runs it would fail for that reason.
	path := filepath.Join(dir, singbox.ExecutableName(runtime.GOOS))
	build := exec.Command(goTool, "build", "-o", path, "./cmd/fakesingbox")
	build.Dir = root
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("building cmd/fakesingbox: %w", err)
	}
	return path, cleanup, nil
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("cannot find the module root: no go.mod above the test directory")
		}
		dir = parent
	}
}

func goBinary() (string, error) {
	if path, err := exec.LookPath("go"); err == nil {
		return path, nil
	}
	// PATH-free fallback: the GOROOT of the running binary is not necessarily
	// the toolchain that built it, so probe the conventional install locations.
	for _, candidate := range []string{
		"/usr/local/go/bin/go",
		filepath.Join(os.Getenv("HOME"), "go", "bin", "go"),
		"/opt/homebrew/bin/go",
		"/usr/bin/go",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", errors.New("cannot find the go tool to build the fake sing-box")
}

// --- test doubles -----------------------------------------------------------

// memStore is an in-memory Store. It keeps revisions as values and records the
// validation calls, so a test can assert what the service asked for without a
// database in the loop (the SQLite integration lives in config_sqlite_test.go).
type memStore struct {
	profiles      map[string]profile.Profile
	revisions     map[string]profile.Revision
	order         []string // revision ids, creation order (oldest first)
	validation    []validationCall
	activations   []string
	failCreate    error
	failSetActive error
}

type validationCall struct {
	revisionID string
	structural profile.ValidationStatus
	singbox    profile.ValidationStatus
	version    string
}

func newMemStore() *memStore {
	return &memStore{profiles: map[string]profile.Profile{}, revisions: map[string]profile.Revision{}}
}

func (s *memStore) addProfile(p profile.Profile) { s.profiles[p.ID] = p }

func (s *memStore) revisionCount() int { return len(s.order) }

func (s *memStore) GetProfile(_ context.Context, id string) (profile.Profile, error) {
	p, ok := s.profiles[id]
	if !ok {
		return profile.Profile{}, apperr.Newf(apperr.CodeProfileNotFound, "storage.GetProfile", "profile %s does not exist", id)
	}
	return p, nil
}

func (s *memStore) UpdateProfile(_ context.Context, p profile.Profile) error {
	if _, ok := s.profiles[p.ID]; !ok {
		return apperr.Newf(apperr.CodeProfileNotFound, "storage.UpdateProfile", "profile %s does not exist", p.ID)
	}
	s.profiles[p.ID] = p
	return nil
}

func (s *memStore) GetRevision(_ context.Context, profileID, revisionID string) (profile.Revision, error) {
	rev, ok := s.revisions[revisionID]
	if !ok || rev.ProfileID != profileID {
		return profile.Revision{}, apperr.Newf(apperr.CodeRevisionNotFound, "storage.GetRevision", "revision %s does not exist", revisionID)
	}
	return rev, nil
}

func (s *memStore) ListRevisions(_ context.Context, profileID string, limit int) ([]profile.Revision, error) {
	var out []profile.Revision
	for i := len(s.order) - 1; i >= 0; i-- { // newest first, like the SQL store
		rev := s.revisions[s.order[i]]
		if rev.ProfileID != profileID {
			continue
		}
		out = append(out, rev)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out, nil
}

func (s *memStore) ActiveRevision(_ context.Context, profileID string) (profile.Revision, error) {
	p, ok := s.profiles[profileID]
	if !ok {
		return profile.Revision{}, apperr.Newf(apperr.CodeProfileNotFound, "storage.ActiveRevision", "profile %s does not exist", profileID)
	}
	if p.ActiveRevisionID == "" {
		return profile.Revision{}, apperr.Newf(apperr.CodeRevisionNotFound, "storage.ActiveRevision", "profile %s has no active revision", profileID)
	}
	return s.GetRevision(context.Background(), profileID, p.ActiveRevisionID)
}

func (s *memStore) CreateRevision(_ context.Context, rev profile.Revision, makeActive bool) error {
	if s.failCreate != nil {
		return s.failCreate
	}
	if _, exists := s.revisions[rev.ID]; exists {
		return apperr.Newf(apperr.CodeInvalidArgument, "storage.CreateRevision", "revision %s already exists", rev.ID)
	}
	s.revisions[rev.ID] = rev
	s.order = append(s.order, rev.ID)
	if makeActive {
		p := s.profiles[rev.ProfileID]
		p.ActiveRevisionID = rev.ID
		s.profiles[rev.ProfileID] = p
	}
	return nil
}

func (s *memStore) SetActiveRevision(_ context.Context, profileID, revisionID string) error {
	if s.failSetActive != nil {
		return s.failSetActive
	}
	if _, err := s.GetRevision(context.Background(), profileID, revisionID); err != nil {
		return err
	}
	p := s.profiles[profileID]
	p.ActiveRevisionID = revisionID
	s.profiles[profileID] = p
	s.activations = append(s.activations, revisionID)
	return nil
}

func (s *memStore) SetRevisionValidation(_ context.Context, revisionID string, structural, singbox profile.ValidationStatus, version string) error {
	rev, ok := s.revisions[revisionID]
	if !ok {
		return apperr.Newf(apperr.CodeRevisionNotFound, "storage.SetRevisionValidation", "revision %s does not exist", revisionID)
	}
	rev.StructuralValidation = structural
	rev.SingBoxValidation = singbox
	rev.SingBoxVersion = version
	s.revisions[revisionID] = rev
	s.validation = append(s.validation, validationCall{revisionID: revisionID, structural: structural, singbox: singbox, version: version})
	return nil
}

func (s *memStore) activeRevisionID(profileID string) string {
	return s.profiles[profileID].ActiveRevisionID
}

// fakeBinaries is the BinaryPort. A zero Version (or a set err) makes the
// service behave exactly as it does on a machine with no managed binary.
type fakeBinaries struct {
	path      string
	version   singbox.Version
	err       error
	pathCalls int
}

func (b *fakeBinaries) Path(context.Context) (string, error) {
	b.pathCalls++
	if b.err != nil {
		return "", b.err
	}
	return b.path, nil
}

func (b *fakeBinaries) Version(context.Context) (singbox.Version, error) {
	if b.err != nil {
		return singbox.Version{}, b.err
	}
	return b.version, nil
}

// fakeRuntime records how often the supervisor was asked to restart, which is
// what decides whether an apply rolls the filesystem back.
type fakeRuntime struct {
	running    bool
	restartErr error
	startErr   error
	restarts   int
	starts     int
	stops      int
	started    []string
}

func (r *fakeRuntime) Status() domruntime.Status {
	if r.running {
		return domruntime.Status{State: domruntime.StateRunning}
	}
	return domruntime.Stopped()
}

func (r *fakeRuntime) Running() bool { return r.running }

func (r *fakeRuntime) StartProfile(_ context.Context, profileID string) error {
	r.starts++
	r.started = append(r.started, profileID)
	if r.startErr != nil {
		return r.startErr
	}
	r.running = true
	return nil
}

func (r *fakeRuntime) Stop(context.Context) error {
	r.stops++
	if r.running {
		r.running = false
	}
	return nil
}

func (r *fakeRuntime) Restart(context.Context) error {
	r.restarts++
	if r.restartErr != nil {
		return r.restartErr
	}
	r.running = true
	return nil
}

// recorder captures the events the service publishes (spec §46: progress is
// pushed, not polled) so their order and payload can be asserted.
type recorder struct {
	names    []string
	payloads []any
}

func (r *recorder) Emit(name string, payload any) {
	r.names = append(r.names, name)
	r.payloads = append(r.payloads, payload)
}

// mark records the current event count so a test can assert on the events of
// one operation even when several ran through the same harness.
func (r *recorder) mark() int { return len(r.names) }

func (r *recorder) stages(t *testing.T) []string { return r.stagesFrom(t, 0) }

func (r *recorder) stagesFrom(t *testing.T, from int) []string {
	t.Helper()
	out := make([]string, 0, len(r.payloads)-from)
	for i := from; i < len(r.payloads); i++ {
		if r.names[i] != events.ConfigApplyProgress {
			t.Fatalf("event %d = %q, want %q", i, r.names[i], events.ConfigApplyProgress)
		}
		progress, ok := r.payloads[i].(Progress)
		if !ok {
			t.Fatalf("event %d payload is %T, want config.Progress", i, r.payloads[i])
		}
		if strings.TrimSpace(progress.Message) == "" {
			t.Errorf("event %d (%s) has an empty message", i, progress.Stage)
		}
		if progress.Occurred.IsZero() {
			t.Errorf("event %d (%s) has no timestamp", i, progress.Stage)
		}
		out = append(out, progress.Stage)
	}
	return out
}

func (r *recorder) percentsFrom(t *testing.T, from int) []int {
	t.Helper()
	out := make([]int, 0, len(r.payloads)-from)
	for i := from; i < len(r.payloads); i++ {
		progress, ok := r.payloads[i].(Progress)
		if !ok {
			t.Fatalf("event %d payload is %T, want config.Progress", i, r.payloads[i])
		}
		out = append(out, progress.Percent)
	}
	return out
}

// --- harness ----------------------------------------------------------------

type harness struct {
	svc   *Service
	store *memStore
	bin   *fakeBinaries
	rt    *fakeRuntime
	em    *recorder
	paths platform.Paths
	root  string
	ids   int
	now   time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	h := &harness{
		store: newMemStore(),
		bin:   &fakeBinaries{path: fakeSingBoxPath, version: mustVersion(t, "1.14.0")},
		rt:    &fakeRuntime{},
		em:    &recorder{},
		root:  root,
		paths: testPaths(root),
		now:   time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC),
	}
	h.store.addProfile(profile.Profile{ID: "p-1", Name: "Work", CreatedAt: h.clock(), UpdatedAt: h.clock()})
	h.svc = New(Deps{
		Store:        h.store,
		Binaries:     h.bin,
		Runtime:      h.rt,
		Paths:        h.paths,
		Emitter:      h.em,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		CheckTimeout: 20 * time.Second,
		NewID: func() string {
			h.ids++
			return fmt.Sprintf("id-%d", h.ids)
		},
		Now: h.clock,
	})
	return h
}

// clock returns a strictly increasing time so revision ordering is
// deterministic and never depends on the machine clock.
func (h *harness) clock() time.Time {
	h.now = h.now.Add(time.Second)
	return h.now
}

func testPaths(root string) platform.Paths {
	configDir := filepath.Join(root, "config")
	return platform.Paths{
		DataDir:            root,
		DBPath:             filepath.Join(root, "state.db"),
		LogPath:            filepath.Join(root, "app.log"),
		ConfigDir:          configDir,
		ActiveConfigPath:   filepath.Join(configDir, "active.json"),
		LastGoodConfigPath: filepath.Join(configDir, "last-good.json"),
		RuntimeDir:         filepath.Join(root, "run"),
		BinDir:             filepath.Join(root, "bin"),
		TempDir:            filepath.Join(root, "tmp"),
	}
}

// --- helpers ----------------------------------------------------------------

func mustVersion(t *testing.T, raw string) singbox.Version {
	t.Helper()
	version, err := singbox.ParseVersion(raw)
	if err != nil {
		t.Fatalf("ParseVersion(%q) = %v, want a version", raw, err)
	}
	return version
}

func wantCode(t *testing.T, err error, want apperr.Code) *apperr.Error {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %s", want)
	}
	typed, ok := apperr.As(err)
	if !ok {
		t.Fatalf("error %v is not a typed apperr, want %s", err, want)
	}
	if typed.Code != want {
		t.Fatalf("error code = %s, want %s (err = %v)", typed.Code, want, err)
	}
	return typed
}

// pretty is the byte-for-byte rendering the service must store and write.
func prettyConfig(t *testing.T, raw string) string {
	t.Helper()
	out, err := domainconfig.Pretty([]byte(raw))
	if err != nil {
		t.Fatalf("Pretty(%q) = %v, want nil", raw, err)
	}
	return string(out)
}

func readActive(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the active configuration: %v", err)
	}
	return string(raw)
}

func requireMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s exists (err = %v), want no file", path, err)
	}
}

// scratchFiles lists the temporary artefacts a failed or finished operation
// must not leave behind: atomic-write temporaries and staged candidates.
func scratchFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		t.Fatalf("reading %s: %v", dir, err)
	}
	var out []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.Contains(name, ".tmp-") || strings.HasPrefix(name, ".candidate-") {
			out = append(out, name)
		}
	}
	return out
}

const (
	configA = `{"log":{"level":"info"},"outbounds":[{"type":"direct","tag":"direct"}]}`
	configB = `{"log":{"level":"debug"},"outbounds":[{"type":"direct","tag":"direct"}]}`
	// configTun needs administrator privileges and exposes a control API.
	configTun = `{"inbounds":[{"type":"tun","tag":"tun-in"}],` +
		`"outbounds":[{"type":"direct","tag":"direct"}],` +
		`"experimental":{"clash_api":{"external_controller":"127.0.0.1:9099","secret":"s3cret"}}}`
)

// --- drafts and history -----------------------------------------------------

func TestDraftReturnsTheActiveRevisionBase(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	saved, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configA, Comment: "first", Apply: true})
	if err != nil {
		t.Fatalf("SaveRevision() = %v, want nil", err)
	}

	draft, err := h.svc.Draft(ctx, "p-1")
	if err != nil {
		t.Fatalf("Draft() = %v, want nil", err)
	}
	if draft.ProfileID != "p-1" || draft.ProfileName != "Work" {
		t.Errorf("draft identity = %q/%q, want p-1/Work", draft.ProfileID, draft.ProfileName)
	}
	if draft.BaseRevisionID != saved.ID {
		t.Errorf("BaseRevisionID = %q, want the active revision %q", draft.BaseRevisionID, saved.ID)
	}
	if draft.ConfigJSON != prettyConfig(t, configA) {
		t.Errorf("draft ConfigJSON = %q, want the stored bytes", draft.ConfigJSON)
	}
	if !draft.StructuralResult.OK {
		t.Errorf("draft structural result = %+v, want OK", draft.StructuralResult)
	}
	if draft.Validation != string(profile.StatusPassed) {
		t.Errorf("draft validation = %q, want %q", draft.Validation, profile.StatusPassed)
	}

	if _, err := h.svc.Draft(ctx, "missing"); !apperr.IsCode(err, apperr.CodeProfileNotFound) {
		t.Errorf("Draft(unknown) = %v, want PROFILE_NOT_FOUND", err)
	}
}

func TestSavedRevisionsAreImmutableAndOrderedNewestFirst(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	first, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configA, Comment: "a", Apply: true})
	if err != nil {
		t.Fatalf("first SaveRevision() = %v, want nil", err)
	}
	firstBytes := h.store.revisions[first.ID].ConfigJSON
	firstCreated := h.store.revisions[first.ID].CreatedAt

	second, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configB, Comment: "b"})
	if err != nil {
		t.Fatalf("second SaveRevision() = %v, want nil", err)
	}
	// Saving again must not rewrite history: the first revision keeps its bytes,
	// its identity and its creation time (spec §12).
	after := h.store.revisions[first.ID]
	if after.ConfigJSON != firstBytes {
		t.Errorf("the first revision was rewritten:\n before %q\n after  %q", firstBytes, after.ConfigJSON)
	}
	if !after.CreatedAt.Equal(firstCreated) {
		t.Errorf("the first revision's CreatedAt changed: %s -> %s", firstCreated, after.CreatedAt)
	}
	if after.Source != profile.SourceManual || after.StructuralValidation != profile.StatusPassed {
		t.Errorf("first revision metadata changed: %+v", after)
	}
	if second.ParentRevisionID != first.ID {
		t.Errorf("second revision parent = %q, want the active revision %q", second.ParentRevisionID, first.ID)
	}
	if second.ConfigJSON != prettyConfig(t, configB) {
		t.Errorf("second revision bytes = %q, want the pretty-printed draft", second.ConfigJSON)
	}
	if second.SingBoxValidation != profile.StatusPassed || second.SingBoxVersion != "1.14.0" {
		t.Errorf("second revision validation = %q/%q, want passed/1.14.0", second.SingBoxValidation, second.SingBoxVersion)
	}

	history, err := h.svc.ListRevisions(ctx, "p-1", 0)
	if err != nil {
		t.Fatalf("ListRevisions() = %v, want nil", err)
	}
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2", len(history))
	}
	if history[0].ID != second.ID || history[1].ID != first.ID {
		t.Errorf("history order = %q,%q; want newest first (%q,%q)", history[0].ID, history[1].ID, second.ID, first.ID)
	}
	// Only the revision that was actually applied carries the active marker.
	if history[0].Active || !history[1].Active {
		t.Errorf("active markers = %v,%v; want only the applied revision marked active", history[0].Active, history[1].Active)
	}
	if history[0].Size != len(second.ConfigJSON) || history[1].Size != len(first.ConfigJSON) {
		t.Errorf("sizes = %d,%d; want %d,%d", history[0].Size, history[1].Size, len(second.ConfigJSON), len(first.ConfigJSON))
	}

	limited, err := h.svc.ListRevisions(ctx, "p-1", 1)
	if err != nil {
		t.Fatalf("ListRevisions(limit 1) = %v, want nil", err)
	}
	if len(limited) != 1 || limited[0].ID != second.ID {
		t.Errorf("limit 1 returned %d revisions, want only the newest", len(limited))
	}

	// Applying the newer revision moves the active marker without rewriting
	// either revision's bytes.
	if _, err := h.svc.Apply(ctx, ApplyInput{ProfileID: "p-1", RevisionID: second.ID}); err != nil {
		t.Fatalf("Apply(second) = %v, want nil", err)
	}
	applied, err := h.svc.ListRevisions(ctx, "p-1", 0)
	if err != nil {
		t.Fatalf("ListRevisions() after apply = %v, want nil", err)
	}
	if !applied[0].Active || applied[1].Active {
		t.Errorf("active markers after apply = %v,%v; want the newest revision active", applied[0].Active, applied[1].Active)
	}
	if applied[0].ConfigJSON != second.ConfigJSON || applied[1].ConfigJSON != firstBytes {
		t.Error("applying a revision rewrote history: revisions must stay immutable (spec §12)")
	}
	if _, err := h.svc.ListRevisions(ctx, "missing", 0); !apperr.IsCode(err, apperr.CodeProfileNotFound) {
		t.Errorf("ListRevisions(unknown profile) = %v, want PROFILE_NOT_FOUND", err)
	}
}

func TestSaveRevisionRejectsInvalidDraftsBeforeAnyWrite(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		source  profile.Source
		want    apperr.Code
		details bool
	}{
		{name: "empty", config: "", want: apperr.CodeConfigInvalid},
		{name: "whitespace only", config: "   \n\t", want: apperr.CodeConfigInvalid},
		{name: "not json", config: `{"log":`, want: apperr.CodeConfigInvalid},
		{name: "trailing garbage", config: `{"log":{"level":"info"}}{"extra":1}`, want: apperr.CodeConfigInvalid},
		{name: "json array is not a config", config: `[1,2,3]`, want: apperr.CodeConfigInvalid},
		{
			name: "unknown source is refused", config: configA,
			source: profile.Source("not-a-source"), want: apperr.CodeInvalidArgument,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			ctx := context.Background()

			_, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: tc.config, Source: tc.source})
			wantCode(t, err, tc.want)

			if got := h.store.revisionCount(); got != 0 {
				t.Errorf("revisions persisted = %d, want 0: the draft was rejected", got)
			}
			requireMissing(t, h.paths.ActiveConfigPath)
			requireMissing(t, h.paths.ConfigDir)
			if left := scratchFiles(t, h.paths.ConfigDir); len(left) != 0 {
				t.Errorf("scratch files left behind: %v", left)
			}
		})
	}
}

// TestSaveRevisionRefusesToApplyARevisionSingBoxRejected covers the case that
// used to leave a profile pointing at a configuration that cannot start: the
// caller asks to apply a draft the validator refused, and the revision must be
// recorded for inspection (spec §14.4) without becoming the active one.
func TestSaveRevisionRefusesToApplyARevisionSingBoxRejected(t *testing.T) {
	t.Setenv(faketest.EnvScenario, faketest.ScenarioCheckFail)

	h := newHarness(t)
	ctx := context.Background()

	view, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configA, Apply: true})
	typed := wantCode(t, err, apperr.CodeConfigCheckFailed)

	stored, ok := h.store.revisions[view.ID]
	if !ok {
		t.Fatalf("the refused revision %q was not kept", view.ID)
	}
	if stored.SingBoxValidation != profile.StatusFailed {
		t.Errorf("sing-box validation = %q, want failed", stored.SingBoxValidation)
	}
	if view.Active {
		t.Error("a revision the validator rejected was reported active")
	}
	if got := h.store.activeRevisionID("p-1"); got != "" {
		t.Errorf("active revision = %q, want none: the refused draft must not be applied", got)
	}
	if !strings.Contains(typed.Message, "not applied") {
		t.Errorf("message = %q, want it to say the draft was saved but not applied", typed.Message)
	}
	if details := strings.Join(typed.Details, "\n"); !strings.Contains(details, "FATAL") {
		t.Errorf("details = %q, want the decoder output", details)
	}
	requireMissing(t, h.paths.ActiveConfigPath)
}

func TestSaveRevisionRecordsAFailedSingBoxCheckWithoutApplying(t *testing.T) {
	t.Setenv(faketest.EnvScenario, faketest.ScenarioCheckFail)

	h := newHarness(t)
	ctx := context.Background()

	view, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configA})
	if err != nil {
		t.Fatalf("SaveRevision() = %v, want the revision to be kept", err)
	}
	stored := h.store.revisions[view.ID]
	if stored.StructuralValidation != profile.StatusPassed {
		t.Errorf("structural = %q, want passed (the JSON parsed)", stored.StructuralValidation)
	}
	if stored.SingBoxValidation != profile.StatusFailed {
		t.Errorf("sing-box validation = %q, want failed", stored.SingBoxValidation)
	}
	if view.Active {
		t.Error("a revision the validator rejected was reported active")
	}
	if h.store.activeRevisionID("p-1") != "" {
		t.Errorf("active revision = %q, want none", h.store.activeRevisionID("p-1"))
	}
	requireMissing(t, h.paths.ActiveConfigPath)
	if left := scratchFiles(t, h.paths.ConfigDir); len(left) != 0 {
		t.Errorf("staged candidates were not cleaned up: %v", left)
	}
}

func TestSaveRevisionWithoutABinaryRecordsTheUnknownVerdict(t *testing.T) {
	h := newHarness(t)
	h.bin.err = apperr.New(apperr.CodeBinaryNotFound, "binary.Path", "no sing-box is installed yet")
	ctx := context.Background()

	view, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configA})
	if err != nil {
		t.Fatalf("SaveRevision() = %v, want nil: saving must work without a binary", err)
	}
	stored := h.store.revisions[view.ID]
	if stored.SingBoxValidation != profile.StatusUnknown {
		t.Errorf("sing-box validation = %q, want unknown", stored.SingBoxValidation)
	}
	if stored.SingBoxVersion != "" {
		t.Errorf("sing-box version = %q, want empty", stored.SingBoxVersion)
	}
}

// --- validation -------------------------------------------------------------

func TestValidateReportsBothLayersAndPersistsNothing(t *testing.T) {
	ctx := context.Background()

	t.Run("structural only", func(t *testing.T) {
		h := newHarness(t)
		result, err := h.svc.Validate(ctx, ValidateInput{ProfileID: "p-1", ConfigJSON: configA, SkipSingBoxCheck: true})
		if err != nil {
			t.Fatalf("Validate() = %v, want nil", err)
		}
		if !result.Valid {
			t.Errorf("Valid = false, want true for %s", configA)
		}
		if result.Check != nil {
			t.Errorf("Check = %+v, want nil when the sing-box check was skipped", result.Check)
		}
		if h.store.revisionCount() != 0 {
			t.Error("validation persisted a revision; it must not (spec §68)")
		}
		requireMissing(t, h.paths.ConfigDir)
	})

	t.Run("accepted by sing-box", func(t *testing.T) {
		h := newHarness(t)
		result, err := h.svc.Validate(ctx, ValidateInput{ProfileID: "p-1", ConfigJSON: configA})
		if err != nil {
			t.Fatalf("Validate() = %v, want nil", err)
		}
		if !result.Valid || result.Check == nil || !result.Check.OK {
			t.Fatalf("result = %+v, want valid with an OK check", result)
		}
		if result.Version != "1.14.0" {
			t.Errorf("Version = %q, want 1.14.0", result.Version)
		}
		// VersionRaw is parsed from the validator's own output; the fixture
		// prints no banner for `check`, so it stays empty while Version (the
		// probed binary) is still reported.
		if result.Check.VersionRaw != "" {
			t.Errorf("check VersionRaw = %q, want empty for a check with no version banner", result.Check.VersionRaw)
		}
		if h.store.revisionCount() != 0 {
			t.Error("validation persisted a revision")
		}
		if left := scratchFiles(t, h.paths.ConfigDir); len(left) != 0 {
			t.Errorf("the staged candidate was not removed: %v", left)
		}
	})

	t.Run("rejected by sing-box", func(t *testing.T) {
		t.Setenv(faketest.EnvScenario, faketest.ScenarioCheckFail)
		h := newHarness(t)
		result, err := h.svc.Validate(ctx, ValidateInput{ProfileID: "p-1", ConfigJSON: configA})
		wantCode(t, err, apperr.CodeConfigCheckFailed)
		if result.Valid {
			t.Error("Valid = true although sing-box rejected the configuration")
		}
		if result.Check == nil || result.Check.OK {
			t.Fatalf("Check = %+v, want the validator's refusal", result.Check)
		}
		// The validator's own message is data the UI renders (spec §30).
		if joined := strings.Join(result.Check.Errors, "\n"); !strings.Contains(joined, "FATAL") {
			t.Errorf("check errors = %q, want the fixture's FATAL line", joined)
		}
		if h.store.revisionCount() != 0 {
			t.Error("a rejected validation persisted a revision")
		}
		if left := scratchFiles(t, h.paths.ConfigDir); len(left) != 0 {
			t.Errorf("the staged candidate was not removed: %v", left)
		}
	})

	t.Run("structurally invalid", func(t *testing.T) {
		h := newHarness(t)
		result, err := h.svc.Validate(ctx, ValidateInput{
			ProfileID:  "p-1",
			ConfigJSON: `{"outbounds":[{"type":"vless"}]}`,
		})
		typed := wantCode(t, err, apperr.CodeConfigInvalid)
		if result.Valid {
			t.Error("Valid = true for a structurally invalid configuration")
		}
		if result.Structural.OK || len(result.Structural.Errors) == 0 {
			t.Errorf("structural = %+v, want a failed result with errors", result.Structural)
		}
		if !strings.Contains(strings.Join(typed.Details, "\n"), "missing tag") {
			t.Errorf("details = %v, want the structural reasons", typed.Details)
		}
		if h.store.revisionCount() != 0 {
			t.Error("an invalid draft was persisted")
		}
		requireMissing(t, h.paths.ConfigDir)
	})

	t.Run("no binary installed", func(t *testing.T) {
		h := newHarness(t)
		h.bin.err = apperr.New(apperr.CodeBinaryNotFound, "binary.Path", "no sing-box is installed yet")
		result, err := h.svc.Validate(ctx, ValidateInput{ProfileID: "p-1", ConfigJSON: configA})
		if err != nil {
			t.Fatalf("Validate() = %v, want nil: structural validation still answers", err)
		}
		if !result.Valid {
			t.Error("Valid = false, want the structural verdict")
		}
		if result.Check != nil {
			t.Errorf("Check = %+v, want nil when no binary could run", result.Check)
		}
	})

	t.Run("binary path that is not executable", func(t *testing.T) {
		h := newHarness(t)
		h.bin.path = filepath.Join(h.root, "missing-sing-box")
		result, err := h.svc.Validate(ctx, ValidateInput{ProfileID: "p-1", ConfigJSON: configA})
		wantCode(t, err, apperr.CodeBinaryNotFound)
		if result.Valid {
			t.Error("Valid = true although the check could not run")
		}
	})
}

// --- apply ------------------------------------------------------------------

func TestApplyWritesTheActiveConfigAtomically(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	saved, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configA})
	if err != nil {
		t.Fatalf("SaveRevision() = %v", err)
	}
	result, err := h.svc.Apply(ctx, ApplyInput{ProfileID: "p-1", RevisionID: saved.ID})
	if err != nil {
		t.Fatalf("Apply() = %v, want nil", err)
	}
	if result.ActiveConfig != h.paths.ActiveConfigPath {
		t.Errorf("ActiveConfig = %q, want %q", result.ActiveConfig, h.paths.ActiveConfigPath)
	}
	if result.Restarted {
		t.Error("Restarted = true although the runtime was not running")
	}
	if result.Warning != "" {
		t.Errorf("Warning = %q, want empty on success", result.Warning)
	}

	want := prettyConfig(t, configA)
	if got := readActive(t, h.paths.ActiveConfigPath); got != want {
		t.Errorf("active configuration =\n%s\nwant\n%s", got, want)
	}
	if mode := fileMode(t, h.paths.ActiveConfigPath); mode.Perm() != 0o600 {
		t.Errorf("active configuration mode = %v, want 0600 (it holds credentials)", mode.Perm())
	}
	if h.store.activeRevisionID("p-1") != saved.ID {
		t.Errorf("active revision = %q, want %q", h.store.activeRevisionID("p-1"), saved.ID)
	}
	if h.store.profiles["p-1"].UpdatedAt.IsZero() {
		t.Error("the profile's UpdatedAt was not refreshed")
	}
	// Nothing ran, so there is no last-known-good file to keep yet.
	requireMissing(t, h.paths.LastGoodConfigPath)

	stages := h.em.stages(t)
	wantStages := []string{"validate", "check", "materialize", "commit", "done"}
	if !equalStrings(stages, wantStages) {
		t.Errorf("progress stages = %v, want %v", stages, wantStages)
	}
	percents := h.progressPercents(t)
	if !equalInts(percents, []int{0, 25, 55, 95, 100}) {
		t.Errorf("progress percentages = %v, want 0,25,55,95,100", percents)
	}
	if left := scratchFiles(t, h.paths.ConfigDir); len(left) != 0 {
		t.Errorf("apply left temporary files behind: %v", left)
	}
	// The candidate is staged next to the active file, so `sing-box check` sees
	// the same directory layout it will see at runtime (spec §15).
	names := dirNames(t, h.paths.ConfigDir)
	for _, name := range names {
		if name != "active.json" {
			t.Errorf("unexpected file in the config directory: %q", name)
		}
	}
}

func TestApplyRestartsARunningRuntimeAndKeepsLastGood(t *testing.T) {
	h := newHarness(t)
	h.rt.running = true
	ctx := context.Background()

	first, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configA, Apply: true})
	if err != nil {
		t.Fatalf("SaveRevision(apply) = %v", err)
	}
	if h.rt.restarts != 1 {
		t.Errorf("restarts = %d, want 1 for an apply of a running runtime", h.rt.restarts)
	}

	second, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configB})
	if err != nil {
		t.Fatalf("second SaveRevision() = %v", err)
	}
	mark := h.em.mark()
	result, err := h.svc.Apply(ctx, ApplyInput{ProfileID: "p-1", RevisionID: second.ID})
	if err != nil {
		t.Fatalf("Apply() = %v, want nil", err)
	}
	if !result.Restarted {
		t.Error("Restarted = false although the runtime was running")
	}
	if h.store.activeRevisionID("p-1") != second.ID {
		t.Errorf("active revision = %q, want %q", h.store.activeRevisionID("p-1"), second.ID)
	}
	// last-known-good holds the configuration that just started successfully.
	if got := readActive(t, h.paths.LastGoodConfigPath); got != prettyConfig(t, configB) {
		t.Errorf("last-known-good =\n%s\nwant the configuration that just started", got)
	}
	stages := h.em.stagesFrom(t, mark)
	wantStages := []string{"validate", "check", "materialize", "restart", "commit", "done"}
	if !equalStrings(stages, wantStages) {
		t.Errorf("progress stages = %v, want %v", stages, wantStages)
	}
	percents := h.em.percentsFrom(t, mark)
	if !equalInts(percents, []int{0, 25, 55, 75, 95, 100}) {
		t.Errorf("progress percentages = %v, want the restart stage at 75", percents)
	}
	if first.ID == second.ID {
		t.Fatal("the two revisions share an id")
	}
}

func TestFailedApplyRestoresThePreviousConfigByteForByte(t *testing.T) {
	h := newHarness(t)
	h.rt.running = true
	ctx := context.Background()

	previous, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configA, Apply: true})
	if err != nil {
		t.Fatalf("SaveRevision() = %v", err)
	}
	before := readActive(t, h.paths.ActiveConfigPath)
	if before != prettyConfig(t, configA) {
		t.Fatalf("precondition: active configuration = %q", before)
	}
	previousBytes := h.store.revisions[previous.ID].ConfigJSON
	mark := h.em.mark()

	next, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configB})
	if err != nil {
		t.Fatalf("second SaveRevision() = %v", err)
	}

	// The new configuration passes validation and is written, but the runtime
	// refuses to start with it.
	h.rt.restartErr = errors.New("sing-box exited with code 1")
	result, err := h.svc.Apply(ctx, ApplyInput{ProfileID: "p-1", RevisionID: next.ID})
	typed := wantCode(t, err, apperr.CodeConfigApplyFailed)
	if !errors.Is(err, h.rt.restartErr) {
		t.Errorf("the underlying restart failure is not preserved: %v", err)
	}
	if !strings.Contains(result.Warning, "restored") {
		t.Errorf("Warning = %q, want it to report the rollback", result.Warning)
	}
	if !strings.Contains(typed.Message, "could not start") {
		t.Errorf("message = %q, want an explanation of the failure", typed.Message)
	}
	if result.RevisionID != next.ID {
		t.Errorf("RevisionID = %q, want the attempted revision %q", result.RevisionID, next.ID)
	}

	// Byte-for-byte: the file is exactly what it was before the failed apply.
	after := readActive(t, h.paths.ActiveConfigPath)
	if after != before {
		t.Errorf("the previous configuration was not restored byte-for-byte:\n before %q\n after  %q", before, after)
	}
	if after != previousBytes {
		t.Errorf("restored bytes differ from the previous revision's stored bytes:\n file %q\n store %q", after, previousBytes)
	}
	if h.rt.starts != 1 || len(h.rt.started) != 1 || h.rt.started[0] != "p-1" {
		t.Errorf("the runtime was not brought back with the previous configuration: %+v", h.rt.started)
	}
	// The failed revision is not active and its verdict is recorded against it.
	if got := h.store.activeRevisionID("p-1"); got != previous.ID {
		t.Errorf("active revision = %q, want the previous %q", got, previous.ID)
	}
	last := h.store.validation[len(h.store.validation)-1]
	if last.revisionID != next.ID || last.singbox != profile.StatusFailed {
		t.Errorf("last recorded validation = %+v, want the failed revision marked failed", last)
	}

	stages := h.em.stagesFrom(t, mark)
	if !containsString(stages, "rollback") {
		t.Errorf("progress stages = %v, want a rollback stage", stages)
	}
	if containsString(stages, "commit") || containsString(stages, "done") {
		t.Errorf("progress stages = %v: a failed apply must not report a commit", stages)
	}
	if left := scratchFiles(t, h.paths.ConfigDir); len(left) != 0 {
		t.Errorf("scratch files left behind: %v", left)
	}
}

func TestFailedApplyWithoutAPreviousRevisionReportsIt(t *testing.T) {
	h := newHarness(t)
	h.rt.running = true
	ctx := context.Background()

	saved, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configA})
	if err != nil {
		t.Fatalf("SaveRevision() = %v", err)
	}
	h.rt.restartErr = errors.New("sing-box exited with code 1")

	result, err := h.svc.Apply(ctx, ApplyInput{ProfileID: "p-1", RevisionID: saved.ID})
	wantCode(t, err, apperr.CodeConfigApplyFailed)
	if strings.Contains(result.Warning, "was restored") {
		t.Errorf("Warning = %q, want an honest report that nothing could be restored", result.Warning)
	}
	if !strings.Contains(result.Warning, "could not be restored") {
		t.Errorf("Warning = %q, want the degraded outcome", result.Warning)
	}
	if h.store.activeRevisionID("p-1") != "" {
		t.Errorf("active revision = %q, want none: the runtime never started", h.store.activeRevisionID("p-1"))
	}
}

func TestFailedSingBoxCheckKeepsTheRunningConfiguration(t *testing.T) {
	t.Setenv(faketest.EnvScenario, faketest.ScenarioCheckFail)

	h := newHarness(t)
	h.rt.running = true
	ctx := context.Background()

	// A revision is inserted directly so the failing check is exercised on the
	// apply path rather than at save time.
	rev := profile.Revision{
		ID: "rev-failing", ProfileID: "p-1", CreatedAt: h.clock(), Source: profile.SourceImport,
		ConfigJSON: prettyConfig(t, configA),
	}
	if err := h.store.CreateRevision(ctx, rev, true); err != nil {
		t.Fatalf("CreateRevision() = %v", err)
	}

	result, err := h.svc.Apply(ctx, ApplyInput{ProfileID: "p-1", RevisionID: rev.ID})
	typed := wantCode(t, err, apperr.CodeConfigCheckFailed)
	if len(typed.Details) == 0 {
		t.Error("the validator's own errors were not attached to the failure")
	}
	if result.RevisionID != "" {
		t.Errorf("RevisionID = %q, want no result for a refused apply", result.RevisionID)
	}
	requireMissing(t, h.paths.ActiveConfigPath)
	if h.rt.restarts != 0 {
		t.Errorf("restarts = %d, want 0: the configuration never passed validation", h.rt.restarts)
	}
	last := h.store.validation[len(h.store.validation)-1]
	if last.singbox != profile.StatusFailed || last.version != "1.14.0" {
		t.Errorf("recorded validation = %+v, want failed against 1.14.0", last)
	}
}

func TestApplyRejectsAStructurallyInvalidRevisionBeforeWriting(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	// JSON that parses but is not a usable configuration: an outbound without a
	// tag or credentials.
	broken := prettyConfig(t, `{"outbounds":[{"type":"vless"}]}`)
	rev := profile.Revision{
		ID: "rev-broken", ProfileID: "p-1", CreatedAt: h.clock(), Source: profile.SourceImport,
		ConfigJSON: broken,
	}
	if err := h.store.CreateRevision(ctx, rev, false); err != nil {
		t.Fatalf("CreateRevision() = %v", err)
	}

	_, err := h.svc.Apply(ctx, ApplyInput{ProfileID: "p-1", RevisionID: rev.ID})
	typed := wantCode(t, err, apperr.CodeConfigInvalid)
	if len(typed.Details) == 0 {
		t.Error("the structural errors were not reported")
	}
	requireMissing(t, h.paths.ActiveConfigPath)
	requireMissing(t, h.paths.ConfigDir)
	// The rejection happens before the pipeline stages that write anything, so
	// the only event is the opening validate stage.
	if stages := h.em.stages(t); !equalStrings(stages, []string{"validate"}) {
		t.Errorf("events = %v, want only the opening validate stage", stages)
	}
}

func TestApplyFailsWhenTheBinaryIsUnavailable(t *testing.T) {
	h := newHarness(t)
	h.bin.err = apperr.New(apperr.CodeBinaryNotFound, "binary.Path", "no sing-box is installed")
	ctx := context.Background()

	rev := profile.Revision{ID: "rev-1", ProfileID: "p-1", CreatedAt: h.clock(), Source: profile.SourceManual, ConfigJSON: prettyConfig(t, configA)}
	if err := h.store.CreateRevision(ctx, rev, false); err != nil {
		t.Fatalf("CreateRevision() = %v", err)
	}
	_, err := h.svc.Apply(ctx, ApplyInput{ProfileID: "p-1", RevisionID: rev.ID})
	wantCode(t, err, apperr.CodeBinaryNotFound)
	requireMissing(t, h.paths.ActiveConfigPath)
}

func TestApplyRefusesAnUnknownRevision(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	_, err := h.svc.Apply(ctx, ApplyInput{ProfileID: "p-1", RevisionID: "nope"})
	if !apperr.IsCode(err, apperr.CodeRevisionNotFound) {
		t.Errorf("Apply(unknown revision) = %v, want REVISION_NOT_FOUND", err)
	}
	_, err = h.svc.Apply(ctx, ApplyInput{ProfileID: "missing", RevisionID: "nope"})
	if !apperr.IsCode(err, apperr.CodeProfileNotFound) {
		t.Errorf("Apply(unknown profile) = %v, want PROFILE_NOT_FOUND", err)
	}
}

// --- rollback ---------------------------------------------------------------

func TestRollbackCreatesANewRevisionFromHistory(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	first, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configA, Apply: true})
	if err != nil {
		t.Fatalf("SaveRevision() = %v", err)
	}
	second, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configB})
	if err != nil {
		t.Fatalf("SaveRevision() = %v", err)
	}
	if err := h.store.SetActiveRevision(ctx, "p-1", second.ID); err != nil {
		t.Fatalf("SetActiveRevision() = %v", err)
	}
	firstBytes := h.store.revisions[first.ID].ConfigJSON

	rolled, err := h.svc.Rollback(ctx, ApplyInput{ProfileID: "p-1", RevisionID: first.ID})
	if err != nil {
		t.Fatalf("Rollback() = %v, want nil", err)
	}
	if rolled.Source != profile.SourceRollback {
		t.Errorf("source = %q, want %q", rolled.Source, profile.SourceRollback)
	}
	if rolled.ConfigJSON != firstBytes {
		t.Errorf("rolled-back bytes = %q, want the historical revision's bytes %q", rolled.ConfigJSON, firstBytes)
	}
	if rolled.ParentRevisionID != second.ID {
		t.Errorf("parent = %q, want the revision that was active (%q)", rolled.ParentRevisionID, second.ID)
	}
	if !strings.Contains(rolled.Comment, first.ID) {
		t.Errorf("comment = %q, want it to name the revision it restored", rolled.Comment)
	}
	if rolled.Active {
		t.Error("rollback marked itself active without applying it")
	}
	// History is append-only: rolling back adds a revision instead of moving a
	// pointer, and the original is untouched.
	if got := h.store.revisions[first.ID].ConfigJSON; got != firstBytes {
		t.Errorf("the historical revision changed: %q -> %q", firstBytes, got)
	}
	history, err := h.svc.ListRevisions(ctx, "p-1", 0)
	if err != nil {
		t.Fatalf("ListRevisions() = %v", err)
	}
	if len(history) != 3 || history[0].ID != rolled.ID {
		t.Errorf("history = %d entries (newest %q), want 3 with the rollback first", len(history), history[0].ID)
	}
	if _, err := h.svc.Rollback(ctx, ApplyInput{ProfileID: "p-1", RevisionID: "nope"}); !apperr.IsCode(err, apperr.CodeRevisionNotFound) {
		t.Errorf("Rollback(unknown) = %v, want REVISION_NOT_FOUND", err)
	}
}

// --- materialisation --------------------------------------------------------

func TestMaterializeActiveWritesTheFileAndReportsRuntimeNeeds(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	rev := profile.Revision{
		ID: "rev-tun", ProfileID: "p-1", CreatedAt: h.clock(), Source: profile.SourceManual,
		ConfigJSON: prettyConfig(t, configTun),
	}
	if err := h.store.CreateRevision(ctx, rev, true); err != nil {
		t.Fatalf("CreateRevision() = %v", err)
	}

	materialized, err := h.svc.MaterializeActive(ctx, "p-1", rev.ID)
	if err != nil {
		t.Fatalf("MaterializeActive() = %v, want nil", err)
	}
	if materialized.Path != h.paths.ActiveConfigPath || materialized.RevisionID != rev.ID {
		t.Errorf("materialized = %+v, want the active path and the revision id", materialized)
	}
	if !materialized.RequiresPrivilege {
		t.Error("RequiresPrivilege = false for a tun configuration (spec §27)")
	}
	clash := materialized.ClashAPI
	if !clash.Enabled || clash.BaseURL != "http://127.0.0.1:9099" || clash.Secret != "s3cret" {
		t.Errorf("ClashAPI = %+v, want the configuration's controller and secret", clash)
	}
	if got := readActive(t, h.paths.ActiveConfigPath); got != rev.ConfigJSON {
		t.Errorf("active configuration =\n%s\nwant\n%s", got, rev.ConfigJSON)
	}

	// Materialising the same revision again is idempotent and does not churn the
	// file or leave scratch files behind.
	again, err := h.svc.MaterializeActive(ctx, "p-1", rev.ID)
	if err != nil {
		t.Fatalf("second MaterializeActive() = %v", err)
	}
	second := readActive(t, h.paths.ActiveConfigPath)
	if again.RevisionID != rev.ID || second != rev.ConfigJSON {
		t.Error("the second materialisation changed the active configuration")
	}
	if left := scratchFiles(t, h.paths.ConfigDir); len(left) != 0 {
		t.Errorf("scratch files left behind: %v", left)
	}

	if _, err := h.svc.MaterializeActive(ctx, "p-1", "nope"); !apperr.IsCode(err, apperr.CodeRevisionNotFound) {
		t.Errorf("MaterializeActive(unknown) = %v, want REVISION_NOT_FOUND", err)
	}
}

func TestClashAPIFromConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      string
		wantEnabled bool
		wantURL     string
		wantSecret  string
	}{
		{name: "no control api", config: configA},
		{name: "experimental without clash api", config: `{"experimental":{"cache_file":{"enabled":true}}}`},
		{
			name:        "explicit controller",
			config:      `{"experimental":{"clash_api":{"external_controller":"127.0.0.1:9999","secret":"tok"}}}`,
			wantEnabled: true, wantURL: "http://127.0.0.1:9999", wantSecret: "tok",
		},
		{
			name:        "sing-box default controller",
			config:      `{"experimental":{"clash_api":{"secret":"tok"}}}`,
			wantEnabled: true, wantURL: "http://127.0.0.1:9090", wantSecret: "tok",
		},
		{name: "invalid json", config: "{"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClashAPIFromConfig([]byte(tc.config))
			if got.Enabled != tc.wantEnabled {
				t.Fatalf("Enabled = %v, want %v", got.Enabled, tc.wantEnabled)
			}
			if got.BaseURL != tc.wantURL {
				t.Errorf("BaseURL = %q, want %q", got.BaseURL, tc.wantURL)
			}
			if got.Secret != tc.wantSecret {
				t.Errorf("Secret = %q, want %q", got.Secret, tc.wantSecret)
			}
		})
	}
}

func TestActiveConfigJSONDistinguishesMissingFromUnreadable(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if _, err := h.svc.ActiveConfigJSON(); !apperr.IsCode(err, apperr.CodeNotFound) {
		t.Errorf("ActiveConfigJSON() before any apply = %v, want NOT_FOUND", err)
	}

	saved, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configA, Apply: true})
	if err != nil {
		t.Fatalf("SaveRevision() = %v", err)
	}
	got, err := h.svc.ActiveConfigJSON()
	if err != nil {
		t.Fatalf("ActiveConfigJSON() = %v, want nil", err)
	}
	if got != prettyConfig(t, configA) {
		t.Errorf("ActiveConfigJSON() = %q, want the configuration of %q", got, saved.ID)
	}
	if h.svc.ActiveConfigPath() != h.paths.ActiveConfigPath {
		t.Errorf("ActiveConfigPath() = %q, want %q", h.svc.ActiveConfigPath(), h.paths.ActiveConfigPath)
	}
}

// TestApplySurfacesAStoreFailure covers the case where the disk write succeeds
// but the commit does not: the caller must see the error and no revision may be
// reported active (spec §14.3).
func TestApplySurfacesAStoreFailure(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	rev := profile.Revision{ID: "rev-1", ProfileID: "p-1", CreatedAt: h.clock(), Source: profile.SourceManual, ConfigJSON: prettyConfig(t, configA)}
	if err := h.store.CreateRevision(ctx, rev, false); err != nil {
		t.Fatalf("CreateRevision() = %v", err)
	}
	commitErr := errors.New("database is locked")
	h.store.failSetActive = commitErr
	before := h.store.profiles["p-1"].UpdatedAt

	_, err := h.svc.Apply(ctx, ApplyInput{ProfileID: "p-1", RevisionID: rev.ID})
	if !errors.Is(err, commitErr) {
		t.Errorf("Apply() = %v, want the store failure to be reported", err)
	}
	// The commit never landed, so the profile still has no active revision and
	// the failure is visible to the caller rather than swallowed.
	if got := h.store.activeRevisionID("p-1"); got != "" {
		t.Errorf("active revision = %q, want none: the commit failed", got)
	}
	if after := h.store.profiles["p-1"].UpdatedAt; !after.Equal(before) {
		t.Errorf("the profile was updated (%s -> %s) although the commit failed", before, after)
	}
	if stages := h.em.stages(t); containsString(stages, "done") {
		t.Errorf("progress stages = %v, want no completion event for a failed commit", stages)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode()
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	return out
}

func (h *harness) progressPercents(t *testing.T) []int {
	t.Helper()
	out := make([]int, 0, len(h.em.payloads))
	for i, payload := range h.em.payloads {
		progress, ok := payload.(Progress)
		if !ok {
			t.Fatalf("event %d payload is %T, want config.Progress", i, payload)
		}
		out = append(out, progress.Percent)
	}
	return out
}

func containsString(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func equalInts(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
