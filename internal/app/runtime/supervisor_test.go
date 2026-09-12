package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"log/slog"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/profile"
	domruntime "github.com/larffxx/singboxui/internal/domain/runtime"
	"github.com/larffxx/singboxui/internal/events"
	"github.com/larffxx/singboxui/internal/platform"
	"github.com/larffxx/singboxui/internal/platform/childrun"
	"github.com/larffxx/singboxui/internal/privilege"
	"github.com/larffxx/singboxui/internal/singbox"
	"github.com/larffxx/singboxui/internal/singbox/faketest"
)

// This suite covers the lifecycle of the managed process as the testing
// strategy requires (spec §67, §68, §70): the executable is a real process —
// real fork/exec, real pipes, real signals — but it is the fake sing-box from
// cmd/fakesingbox, never an installed binary. Nothing here needs root, a real
// sing-box or a real user home directory: every path lives under t.TempDir().
//
// The security-relevant properties asserted below are:
//   - quitting the application always ends the managed process, including one
//     that ignores SIGTERM (spec §70);
//   - a launch request is fully absolute and only elevates when the
//     materialised configuration says TUN is required (spec §27, §28);
//   - a missing binary is detected before anything is started.

// fakeBinary is the fake sing-box built once for the whole package.
var fakeBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fakesingbox-runtime-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot create the fake binary directory: %v\n", err)
		os.Exit(1)
	}
	root, err := moduleRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot locate the module root: %v\n", err)
		os.Exit(1)
	}
	// The fixture carries the platform's executable name: Windows only starts a
	// file whose extension is in PATHEXT, so a fixture called "sing-box" cannot
	// be run there at all.
	fakeName := singbox.ExecutableName(runtime.GOOS)
	build := exec.Command("go", "build", "-o", filepath.Join(dir, fakeName), "./cmd/fakesingbox")
	build.Dir = root
	if out, buildErr := build.CombinedOutput(); buildErr != nil {
		fmt.Fprintf(os.Stderr, "cannot build the fake sing-box: %v\n%s\n", buildErr, out)
		os.Exit(1)
	}
	fakeBinary = filepath.Join(dir, fakeName)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// moduleRoot walks up from this file until it finds the go.mod that owns the
// module, so the test never depends on the working directory.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// --- harness --------------------------------------------------------------------------

type options struct {
	scenario        string
	timings         Timings
	requiresTun     bool
	launchErr       error
	binaryErr       error
	emitterDisabled bool
}

func defaultOptions() options {
	return options{
		scenario: faketest.ScenarioRun,
		timings:  Timings{StartGrace: 250 * time.Millisecond, StopGrace: 2 * time.Second, KillGrace: 2 * time.Second},
	}
}

type harness struct {
	t        *testing.T
	dir      string
	paths    platform.Paths
	sup      *Supervisor
	launcher *fakeLauncher
	store    *fakeStore
	config   *fakeConfig
	binaries *fakeBinaries
	observer *fakeObserver
	emitter  *recorder
}

func newHarness(t *testing.T, opts options) *harness {
	t.Helper()
	dir := t.TempDir()
	paths := platform.Paths{
		DataDir:            dir,
		DBPath:             filepath.Join(dir, "app.db"),
		LogPath:            filepath.Join(dir, "app.log"),
		ConfigDir:          filepath.Join(dir, "config"),
		ActiveConfigPath:   filepath.Join(dir, "config", "sing-box.json"),
		LastGoodConfigPath: filepath.Join(dir, "config", "last-good.json"),
		RuntimeDir:         filepath.Join(dir, "runtime"),
		BinDir:             filepath.Join(dir, "bin"),
		TempDir:            filepath.Join(dir, "tmp"),
	}
	h := &harness{
		t:        t,
		dir:      dir,
		paths:    paths,
		launcher: &fakeLauncher{binary: fakeBinary, scenario: opts.scenario, err: opts.launchErr},
		store:    &fakeStore{},
		config:   &fakeConfig{path: paths.ActiveConfigPath, requiresTun: opts.requiresTun},
		binaries: &fakeBinaries{path: fakeBinary, err: opts.binaryErr},
		observer: &fakeObserver{},
		emitter:  &recorder{},
	}
	deps := Deps{
		Store:          h.store,
		Config:         h.config,
		Binaries:       h.binaries,
		Privilege:      h.launcher,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		Paths:          paths,
		Observer:       h.observer,
		Timings:        opts.timings,
		LogFlushPeriod: 10 * time.Millisecond,
	}
	if !opts.emitterDisabled {
		deps.Emitter = h.emitter
	}
	h.sup = New(context.Background(), deps)
	t.Cleanup(func() {
		// Spec §26: every test leaves the supervisor shut down.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.sup.Shutdown(shutdownCtx)
	})
	return h
}

// runDir is the per-launch scratch directory the supervisor chose.
func (h *harness) runDir() string {
	h.launcher.mu.Lock()
	defer h.launcher.mu.Unlock()
	if len(h.launcher.requests) == 0 {
		h.t.Fatalf("no launch request was recorded")
	}
	return filepath.Dir(h.launcher.requests[len(h.launcher.requests)-1].StatusPath)
}

// fakeStatusFile is where the fake process records its own exit code.
func (h *harness) fakeStatusFile() string { return filepath.Join(h.runDir(), "fake.status") }

// fakePIDFile is where the fake process records its own pid.
func (h *harness) fakePIDFile() string { return filepath.Join(h.runDir(), "fake.pid") }

func (h *harness) fakePID() int {
	h.t.Helper()
	waitFor(h.t, "the fake process to write its pid", processWaitBudget, func() bool {
		_, err := os.Stat(h.fakePIDFile())
		return err == nil
	})
	data, err := os.ReadFile(h.fakePIDFile())
	if err != nil {
		h.t.Fatalf("reading the fake pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		h.t.Fatalf("the fake pid file is not a pid: %q", string(data))
	}
	return pid
}

func (h *harness) statuses() []domruntime.Status { return h.emitter.statuses() }

func (h *harness) states() []domruntime.State {
	var out []domruntime.State
	for _, s := range h.statuses() {
		out = append(out, s.State)
	}
	return out
}

func (h *harness) sawState(state domruntime.State) bool {
	for _, s := range h.states() {
		if s == state {
			return true
		}
	}
	return false
}

// --- fakes ----------------------------------------------------------------------------

type fakeLauncher struct {
	mu       sync.Mutex
	requests []privilege.Request
	binary   string
	scenario string
	err      error
}

func (f *fakeLauncher) Start(ctx context.Context, req privilege.Request) (privilege.Process, error) {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	err := f.err
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	// The real launcher enforces this too; asserting it here is what makes the
	// absolute-path requirement (spec §24) a testable property.
	if vErr := req.Validate(); vErr != nil {
		return nil, vErr
	}
	if req.Elevate {
		return nil, privilege.ErrUnsupported
	}
	runDir := filepath.Dir(req.StatusPath)
	return childrun.Start(ctx, childrun.Config{
		BinaryPath: req.BinaryPath,
		ConfigPath: req.ConfigPath,
		WorkDir:    req.WorkDir,
		LogPath:    req.LogPath,
		PIDPath:    req.PIDPath,
		StatusPath: req.StatusPath,
		Args: []string{
			"run", "-c", req.ConfigPath,
			"-scenario", f.scenario,
			"-pid-file", filepath.Join(runDir, "fake.pid"),
			"-status-file", filepath.Join(runDir, "fake.status"),
			"-log-file", filepath.Join(runDir, "fake.log"),
		},
	})
}

func (f *fakeLauncher) Supported() (bool, string) { return true, "" }

func (f *fakeLauncher) recorded() []privilege.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]privilege.Request(nil), f.requests...)
}

type fakeStore struct {
	mu        sync.Mutex
	touched   []string
	getErr    error
	profileID string
}

func (s *fakeStore) GetProfile(_ context.Context, id string) (profile.Profile, error) {
	if s.getErr != nil {
		return profile.Profile{}, s.getErr
	}
	if id == "" {
		return profile.Profile{}, apperr.New(apperr.CodeInvalidArgument, "store.GetProfile", "empty id")
	}
	return profile.Profile{ID: id, Name: "Main", ActiveRevisionID: "rev-1"}, nil
}

func (s *fakeStore) ActiveRevision(_ context.Context, profileID string) (profile.Revision, error) {
	s.mu.Lock()
	s.profileID = profileID
	s.mu.Unlock()
	return profile.Revision{ID: "rev-1", ProfileID: profileID, Source: profile.SourceManual}, nil
}

func (s *fakeStore) TouchLastUsed(_ context.Context, profileID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.touched = append(s.touched, profileID)
	return nil
}

type fakeConfig struct {
	path        string
	requiresTun bool
}

func (c *fakeConfig) MaterializeActive(_ context.Context, profileID, revisionID string) (Materialized, error) {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return Materialized{}, err
	}
	// A real file, so the child process reads exactly what the supervisor
	// handed it.
	if err := os.WriteFile(c.path, []byte(`{"log":{"level":"info"},"outbounds":[]}`), 0o600); err != nil {
		return Materialized{}, err
	}
	return Materialized{
		Path:              c.path,
		RequiresPrivilege: c.requiresTun,
		RevisionID:        revisionID,
		ClashAPI:          ClashAPI{Enabled: true, BaseURL: "http://127.0.0.1:9090", Secret: "local-secret"},
	}, nil
}

type fakeBinaries struct {
	path string
	err  error
}

func (b *fakeBinaries) Path(context.Context) (string, error) {
	if b.err != nil {
		return "", b.err
	}
	return b.path, nil
}

func (b *fakeBinaries) Version(context.Context) (singbox.Version, error) {
	return singbox.Version{Major: 1, Minor: 14, Patch: 0, Raw: "1.14.0"}, nil
}

type fakeObserver struct {
	mu      sync.Mutex
	running []RunInfo
	stopped int
}

func (o *fakeObserver) RuntimeRunning(_ context.Context, info RunInfo) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.running = append(o.running, info)
}

func (o *fakeObserver) RuntimeStopped() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stopped++
}

type recorder struct {
	mu       sync.Mutex
	names    []string
	snaps    []domruntime.Status
	logBatch int
}

func (r *recorder) Emit(name string, payload any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.names = append(r.names, name)
	switch name {
	case events.RuntimeStatus:
		if status, ok := payload.(domruntime.Status); ok {
			r.snaps = append(r.snaps, status)
		}
	case events.RuntimeLog:
		r.logBatch++
	}
}

func (r *recorder) statuses() []domruntime.Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domruntime.Status(nil), r.snaps...)
}

func (r *recorder) logBatches() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.logBatch
}

// --- helpers --------------------------------------------------------------------------

// processWaitBudget bounds only the failure case: waitFor returns the moment its
// condition holds, which is milliseconds on an idle machine. Three seconds was
// too tight -- under load (a parallel build, a busy CI runner) spawning the fake
// process and letting it write its pid can take longer, which surfaced as an
// intermittent failure that had nothing to do with the supervisor.
const processWaitBudget = 30 * time.Second

func waitFor(t *testing.T, what string, deadline time.Duration, cond func() bool) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", deadline, what)
}

// processAlive reports whether a pid still exists. Signal 0 probes without
// delivering anything, so the process state is not perturbed.
func processAlive(pid int) bool {
	// The platform's own answer, not os.Process.Signal(0): Windows does not
	// implement signal 0 for a process handle, so every pid would look alive and
	// "the process is gone" could never be asserted there.
	return childrun.Alive(pid)
}

func assertDead(t *testing.T, pid int) {
	t.Helper()
	waitFor(t, fmt.Sprintf("process %d to disappear", pid), processWaitBudget, func() bool {
		return !processAlive(pid)
	})
}

func readFileIfExists(t *testing.T, path string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false
	}
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return strings.TrimSpace(string(data)), true
}

// --- tests ----------------------------------------------------------------------------

func TestStartProfileReportsRunningWithARealProcess(t *testing.T) {
	h := newHarness(t, defaultOptions())

	if err := h.sup.StartProfile(context.Background(), "p-1"); err != nil {
		t.Fatalf("StartProfile: %v", err)
	}
	if !h.sup.Running() {
		t.Fatalf("Running() = false after a successful start")
	}

	status := h.sup.Status()
	if status.State != domruntime.StateRunning {
		t.Fatalf("state = %s, want RUNNING", status.State)
	}
	if status.PID <= 0 {
		t.Fatalf("pid = %d, want a real process id", status.PID)
	}
	if status.ActiveProfileID != "p-1" || status.ActiveRevisionID != "rev-1" {
		t.Fatalf("active identity = %q/%q, want p-1/rev-1", status.ActiveProfileID, status.ActiveRevisionID)
	}
	if status.BinaryVersion != "1.14.0" {
		t.Fatalf("binary version = %q, want 1.14.0", status.BinaryVersion)
	}
	if status.ConfigPath != h.paths.ActiveConfigPath {
		t.Fatalf("config path = %q, want %q", status.ConfigPath, h.paths.ActiveConfigPath)
	}
	if status.Elevated {
		t.Fatal("a launch without TUN must not be flagged elevated")
	}
	if status.StartedAt == nil {
		t.Fatal("StartedAt is nil for a running process")
	}
	if !processAlive(status.PID) {
		t.Fatalf("process %d is not alive although the supervisor reports RUNNING", status.PID)
	}

	// The supervisor and the process must agree on the pid.
	if pid := h.fakePID(); pid != status.PID {
		t.Fatalf("the fake process reports pid %d but the supervisor reports %d", pid, status.PID)
	}

	// The observer is what starts the traffic collector (spec §45).
	h.observer.mu.Lock()
	defer h.observer.mu.Unlock()
	if len(h.observer.running) != 1 {
		t.Fatalf("observer RuntimeRunning calls = %d, want 1", len(h.observer.running))
	}
	info := h.observer.running[0]
	if info.PID != status.PID || info.ProfileID != "p-1" || info.RevisionID != "rev-1" {
		t.Fatalf("observer RunInfo = %+v, want the running process", info)
	}
	if !info.ClashAPIEnabled || info.ClashAPIBaseURL == "" {
		t.Fatalf("observer RunInfo lost the clash api description: %+v", info)
	}

	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	if len(h.store.touched) != 1 || h.store.touched[0] != "p-1" {
		t.Fatalf("TouchLastUsed = %v, want [p-1]", h.store.touched)
	}
}

func TestStartEmitsStartingThenRunning(t *testing.T) {
	h := newHarness(t, defaultOptions())
	if err := h.sup.StartProfile(context.Background(), "p-1"); err != nil {
		t.Fatalf("StartProfile: %v", err)
	}
	states := h.states()
	if len(states) < 2 {
		t.Fatalf("emitted states = %v, want at least STARTING and RUNNING", states)
	}
	if states[0] != domruntime.StateStarting {
		t.Fatalf("first emitted state = %s, want STARTING", states[0])
	}
	if states[len(states)-1] != domruntime.StateRunning {
		t.Fatalf("last emitted state = %s, want RUNNING", states[len(states)-1])
	}
}

func TestShutdownTerminatesTheManagedProcess(t *testing.T) {
	h := newHarness(t, defaultOptions())
	ctx := context.Background()
	if err := h.sup.StartProfile(ctx, "p-1"); err != nil {
		t.Fatalf("StartProfile: %v", err)
	}
	pid := h.sup.Status().PID
	if pid <= 0 || !processAlive(pid) {
		t.Fatalf("the managed process is not alive before shutdown")
	}

	if err := h.sup.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// Spec §70: quitting the application leaves no managed process behind.
	assertDead(t, pid)
	if h.sup.Running() {
		t.Fatal("Running() = true after Shutdown")
	}
	if !h.sup.ShuttingDown() {
		t.Fatal("ShuttingDown() = false after Shutdown")
	}
	status := h.sup.Status()
	if status.State != domruntime.StateStopped {
		t.Fatalf("state = %s, want STOPPED", status.State)
	}
	if status.PID != 0 {
		t.Fatalf("pid = %d, want 0 after Shutdown", status.PID)
	}
	if status.LastExitCode == nil || *status.LastExitCode != 0 {
		t.Fatalf("LastExitCode = %v, want 0 (the fake exits 0 on SIGTERM)", status.LastExitCode)
	}

	// The process recorded its own graceful exit: it was asked to stop, not
	// killed.
	if raw, ok := readFileIfExists(t, h.fakeStatusFile()); !ok {
		t.Fatal("the process wrote no status file, so it was killed instead of stopped")
	} else {
		// The status file carries the helper's JSON payload ({"exitCode":N}),
		// not a bare number.
		var recorded struct {
			ExitCode int `json:"exitCode"`
		}
		if err := json.Unmarshal([]byte(raw), &recorded); err != nil {
			t.Fatalf("status file %q is not the helper's JSON payload: %v", raw, err)
		}
		if recorded.ExitCode != 0 {
			t.Fatalf("the process recorded exit code %d, want 0", recorded.ExitCode)
		}
	}

	if states := h.states(); len(states) == 0 || states[len(states)-1] != domruntime.StateStopped {
		t.Fatalf("final emitted state = %v, want STOPPED", states)
	}
	h.observer.mu.Lock()
	defer h.observer.mu.Unlock()
	if h.observer.stopped != 1 {
		t.Fatalf("observer RuntimeStopped calls = %d, want 1", h.observer.stopped)
	}

	// Shutdown is idempotent (spec §26).
	if err := h.sup.Shutdown(ctx); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
}

func TestShutdownKillsAProcessThatIgnoresTheGracefulSignal(t *testing.T) {
	opts := defaultOptions()
	opts.scenario = faketest.ScenarioRunIgnoreTerm
	opts.timings.StopGrace = 150 * time.Millisecond
	h := newHarness(t, opts)

	if err := h.sup.StartProfile(context.Background(), "p-1"); err != nil {
		t.Fatalf("StartProfile: %v", err)
	}
	pid := h.sup.Status().PID
	if pid <= 0 {
		t.Fatal("no pid was reported")
	}

	if err := h.sup.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// The process ignored SIGTERM, so only the forced kill can explain this.
	assertDead(t, pid)
	status := h.sup.Status()
	if status.State != domruntime.StateStopped {
		t.Fatalf("state = %s, want STOPPED", status.State)
	}
	if status.LastExitCode == nil {
		t.Fatal("no exit code was recorded for the killed process")
	}
	// A killed process reports a negative code (the signal) only where signals
	// exist; Windows reports the code of the killer, so the assertion is about the
	// platform it can be made on.
	if runtime.GOOS != "windows" && *status.LastExitCode >= 0 {
		t.Fatalf("LastExitCode = %d, want a negative code for a signalled process", *status.LastExitCode)
	}
	if _, ok := readFileIfExists(t, h.fakeStatusFile()); ok {
		t.Fatal("the process wrote a graceful status file although it ignored SIGTERM")
	}
}

func TestStartAfterShutdownIsRefused(t *testing.T) {
	h := newHarness(t, defaultOptions())
	ctx := context.Background()
	if err := h.sup.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	err := h.sup.StartProfile(ctx, "p-1")
	if !apperr.IsCode(err, apperr.CodeAppShuttingDown) {
		t.Fatalf("StartProfile after shutdown = %v, want APP_SHUTTING_DOWN", err)
	}
	if got := h.launcher.recorded(); len(got) != 0 {
		t.Fatalf("a process was launched during shutdown: %+v", got)
	}
}

func TestStartRefusesWhileAlreadyRunning(t *testing.T) {
	h := newHarness(t, defaultOptions())
	ctx := context.Background()
	if err := h.sup.StartProfile(ctx, "p-1"); err != nil {
		t.Fatalf("StartProfile: %v", err)
	}
	err := h.sup.StartProfile(ctx, "p-1")
	if !apperr.IsCode(err, apperr.CodeRuntimeAlreadyRunning) {
		t.Fatalf("second StartProfile = %v, want RUNTIME_ALREADY_RUNNING", err)
	}
	if got := len(h.launcher.recorded()); got != 1 {
		t.Fatalf("launch requests = %d, want 1 (no second process)", got)
	}
}

func TestStartFailsWhenTheProcessExitsImmediately(t *testing.T) {
	opts := defaultOptions()
	opts.scenario = faketest.ScenarioRunCrash
	h := newHarness(t, opts)

	err := h.sup.StartProfile(context.Background(), "p-1")
	if !apperr.IsCode(err, apperr.CodeRuntimeStartFailed) {
		t.Fatalf("StartProfile = %v, want RUNTIME_START_FAILED", err)
	}
	// The supervisor must report why the process died, not just that it did.
	if details := apperr.DetailsOf(err); len(details) == 0 {
		t.Fatalf("error carries no details: %v", err)
	}
	if h.sup.Running() {
		t.Fatal("Running() = true although the process exited during startup")
	}
	if h.sawState(domruntime.StateRunning) {
		t.Fatalf("RUNNING was emitted for a process that never became healthy: %v", h.states())
	}
	status := h.sup.Status()
	if status.State != domruntime.StateFailed {
		t.Fatalf("state = %s, want FAILED", status.State)
	}
	if status.LastErrorCode != string(apperr.CodeRuntimeStartFailed) {
		t.Fatalf("LastErrorCode = %q, want %q", status.LastErrorCode, apperr.CodeRuntimeStartFailed)
	}
	if status.PID != 0 {
		t.Fatalf("pid = %d, want 0 after a failed start", status.PID)
	}
}

func TestStartFailsWhenTheBinaryIsMissing(t *testing.T) {
	opts := defaultOptions()
	opts.binaryErr = apperr.New(apperr.CodeBinaryNotFound, "binary.Path", "no managed sing-box is installed")
	h := newHarness(t, opts)

	err := h.sup.StartProfile(context.Background(), "p-1")
	if !apperr.IsCode(err, apperr.CodeBinaryNotFound) {
		t.Fatalf("StartProfile = %v, want BINARY_NOT_FOUND to survive", err)
	}
	if got := h.launcher.recorded(); len(got) != 0 {
		t.Fatalf("a process was launched although the binary is missing: %+v", got)
	}
	if h.sup.Status().State != domruntime.StateFailed {
		t.Fatalf("state = %s, want FAILED", h.sup.Status().State)
	}
}

func TestElevationRefusalBecomesPrivilegeDenied(t *testing.T) {
	opts := defaultOptions()
	opts.requiresTun = true
	opts.launchErr = privilege.ErrCancelled
	h := newHarness(t, opts)

	err := h.sup.StartProfile(context.Background(), "p-1")
	if !apperr.IsCode(err, apperr.CodePrivilegeDenied) {
		t.Fatalf("StartProfile = %v, want PRIVILEGE_DENIED when the user dismisses the prompt", err)
	}
	requests := h.launcher.recorded()
	if len(requests) != 1 {
		t.Fatalf("launch requests = %d, want 1", len(requests))
	}
	if !requests[0].Elevate {
		t.Fatal("a TUN configuration was launched without requesting elevation")
	}
	if requests[0].Reason == "" {
		t.Fatal("the elevation request carries no reason to show the user")
	}
}

func TestLaunchRequestIsAbsoluteAndNeverElevatesWithoutTun(t *testing.T) {
	h := newHarness(t, defaultOptions())
	if err := h.sup.StartProfile(context.Background(), "p-1"); err != nil {
		t.Fatalf("StartProfile: %v", err)
	}
	requests := h.launcher.recorded()
	if len(requests) != 1 {
		t.Fatalf("launch requests = %d, want 1", len(requests))
	}
	req := requests[0]
	for name, value := range map[string]string{
		"binaryPath": req.BinaryPath,
		"configPath": req.ConfigPath,
		"logPath":    req.LogPath,
		"pidPath":    req.PIDPath,
		"statusPath": req.StatusPath,
		"workDir":    req.WorkDir,
	} {
		if value == "" {
			t.Fatalf("%s is empty in the launch request", name)
		}
		if !filepath.IsAbs(value) {
			t.Fatalf("%s = %q, want an absolute path", name, value)
		}
	}
	if req.Elevate {
		t.Fatal("elevation was requested for a configuration that does not need it")
	}
	// Everything the child needs lives under the application data directory.
	dataDir := h.dir
	for name, value := range map[string]string{"workDir": req.WorkDir, "configPath": req.ConfigPath, "statusPath": req.StatusPath} {
		if !strings.HasPrefix(value, dataDir) {
			t.Fatalf("%s = %q, want a path inside the data directory %q", name, value, dataDir)
		}
	}
	// A path filepath.Clean would rewrite is refused by the privileged helper on
	// both sides of the boundary. On Windows a literal "/" is exactly that — the
	// run files must be joined onto the run directory, never concatenated to it
	// with a slash — so this assertion only ever fails on a Windows run, which is
	// why the suite runs there as well.
	for name, value := range map[string]string{
		"binaryPath": req.BinaryPath,
		"configPath": req.ConfigPath,
		"logPath":    req.LogPath,
		"pidPath":    req.PIDPath,
		"statusPath": req.StatusPath,
		"workDir":    req.WorkDir,
	} {
		if cleaned := filepath.Clean(value); cleaned != value {
			t.Errorf("%s = %q is not in its cleaned form %q: the helper refuses such a path", name, value, cleaned)
		}
	}
	// The run files sit directly in the run directory, next to each other.
	runDir := filepath.Dir(req.StatusPath)
	for name, value := range map[string]string{"logPath": req.LogPath, "pidPath": req.PIDPath} {
		if got := filepath.Dir(value); got != runDir {
			t.Errorf("%s = %q, want it directly in the run directory %q", name, value, runDir)
		}
	}
}

func TestStopIsRefusedWhenNothingRuns(t *testing.T) {
	h := newHarness(t, defaultOptions())
	err := h.sup.Stop(context.Background())
	if !apperr.IsCode(err, apperr.CodeRuntimeNotRunning) {
		t.Fatalf("Stop = %v, want RUNTIME_NOT_RUNNING", err)
	}
}

func TestRestartReplacesTheRunningProcess(t *testing.T) {
	h := newHarness(t, defaultOptions())
	ctx := context.Background()
	if err := h.sup.StartProfile(ctx, "p-1"); err != nil {
		t.Fatalf("StartProfile: %v", err)
	}
	first := h.sup.Status().PID
	if first <= 0 {
		t.Fatal("no pid after the first start")
	}

	if err := h.sup.Restart(ctx); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	second := h.sup.Status().PID
	if second <= 0 {
		t.Fatal("no pid after the restart")
	}
	if second == first {
		t.Fatalf("Restart reused pid %d; a fresh process is required", first)
	}
	if !h.sup.Running() {
		t.Fatal("Running() = false after Restart")
	}
	if !processAlive(second) {
		t.Fatalf("process %d is not alive after Restart", second)
	}
	assertDead(t, first)
	if got := len(h.launcher.recorded()); got != 2 {
		t.Fatalf("launch requests = %d, want 2", got)
	}
	h.observer.mu.Lock()
	defer h.observer.mu.Unlock()
	if len(h.observer.running) != 2 || h.observer.stopped != 1 {
		t.Fatalf("observer calls = running:%d stopped:%d, want running:2 stopped:1",
			len(h.observer.running), h.observer.stopped)
	}
}

func TestLogsCaptureTheChildOutputAndCanBeCleared(t *testing.T) {
	h := newHarness(t, defaultOptions())
	if err := h.sup.StartProfile(context.Background(), "p-1"); err != nil {
		t.Fatalf("StartProfile: %v", err)
	}

	// The fake writes a heartbeat every 250ms, so a bounded wait is enough.
	waitFor(t, "the child's log lines to reach the buffer", processWaitBudget, func() bool {
		return len(h.sup.LogTail(50)) > 0
	})
	lines := strings.Join(h.sup.LogTail(50), "\n")
	if !strings.Contains(lines, "sing-box started") {
		t.Fatalf("log tail does not contain the startup line: %q", lines)
	}
	if h.emitter.logBatches() == 0 {
		t.Fatal("no runtime:log batch was emitted; the frontend would have to poll")
	}

	h.sup.ClearLogs()
	if got := h.sup.LogTail(50); len(got) != 0 {
		t.Fatalf("ClearLogs left %d records behind: %v", len(got), got)
	}
}

// TestShutdownWithNothingRunningIsANoOp pins the idle-shutdown contract: an
// application that never started sing-box must still quit cleanly, because spec
// §26 makes the shutdown sequence unconditional. The explicit Stop command keeps
// reporting RUNTIME_NOT_RUNNING, which is where that signal belongs.
func TestShutdownWithNothingRunningIsANoOp(t *testing.T) {
	h := newHarness(t, defaultOptions())
	ctx := context.Background()
	if err := h.sup.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown with nothing running = %v, want nil", err)
	}
	if err := h.sup.Stop(ctx); !apperr.IsCode(err, apperr.CodeRuntimeNotRunning) {
		t.Fatalf("Stop with nothing running = %v, want RUNTIME_NOT_RUNNING", err)
	}
	if h.sup.Running() {
		t.Fatal("Running() = true after an idle shutdown")
	}
	if h.sup.Status().State != domruntime.StateStopped {
		t.Fatalf("state = %s, want STOPPED", h.sup.Status().State)
	}
	if !h.sup.ShuttingDown() {
		t.Fatal("ShuttingDown() = false after Shutdown")
	}
	// The second call is the no-op §26 demands.
	if err := h.sup.Shutdown(ctx); err != nil {
		t.Fatalf("second Shutdown = %v, want nil", err)
	}
}

func TestMissingEmitterStillWorks(t *testing.T) {
	// The supervisor is also constructed in tests and in the CLI path, where
	// there is no frontend; a nil emitter must not panic (spec §46).
	opts := defaultOptions()
	opts.emitterDisabled = true
	h := newHarness(t, opts)
	if err := h.sup.StartProfile(context.Background(), "p-1"); err != nil {
		t.Fatalf("StartProfile without an emitter: %v", err)
	}
	if !h.sup.Running() {
		t.Fatal("Running() = false without an emitter")
	}
}

// TestStartFailureCarriesTheWrappedReason asserts the reason a launch was refused
// survives into the status the interface reads and into the log tail a user is
// asked for. A configuration check that fails has its decoder output; a launch
// that never reached a process has only its error chain, and dropping it leaves
// "could not start sing-box" as the whole report.
func TestStartFailureCarriesTheWrappedReason(t *testing.T) {
	opts := defaultOptions()
	opts.requiresTun = true
	opts.launchErr = apperr.Wrap(apperr.CodeRuntimeStartFailed, "privrun",
		"the privileged helper refused the request as invalid",
		errors.New("--log must not contain \"..\", \".\" or a trailing separator"))
	h := newHarness(t, opts)

	err := h.sup.StartProfile(context.Background(), "p-1")
	if err == nil {
		t.Fatal("StartProfile() = nil, want a refusal")
	}
	if !strings.Contains(err.Error(), "--log must not contain") {
		t.Errorf("the returned error = %q, want it to carry the reason the helper refused", err)
	}
	status := h.sup.Status()
	if status.State != domruntime.StateFailed {
		t.Fatalf("state = %s, want FAILED", status.State)
	}
	if !strings.Contains(status.LastError, "--log must not contain") {
		t.Errorf("LastError = %q, want the wrapped reason, not just the notice", status.LastError)
	}
}
