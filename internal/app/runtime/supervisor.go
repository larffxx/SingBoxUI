// Package runtime owns the managed sing-box process.
//
// Exactly one supervisor exists per application (spec §63): it holds the state
// machine, serialises every lifecycle command (spec §23) and pushes typed
// snapshots plus bounded log batches to the frontend (spec §22, §46, §47).
package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/profile"
	domruntime "github.com/larffxx/singboxui/internal/domain/runtime"
	"github.com/larffxx/singboxui/internal/events"
	"github.com/larffxx/singboxui/internal/platform"
	"github.com/larffxx/singboxui/internal/privilege"
	"github.com/larffxx/singboxui/internal/singbox"
)

// Store is the persistence the supervisor needs to resolve a profile.
type Store interface {
	GetProfile(ctx context.Context, id string) (profile.Profile, error)
	ActiveRevision(ctx context.Context, profileID string) (profile.Revision, error)
	TouchLastUsed(ctx context.Context, profileID string) error
}

// ConfigPort materialises the configuration the managed process must read.
// It is implemented by internal/app/config; the interface lives here so the two
// packages never import each other.
type ConfigPort interface {
	// MaterializeActive guarantees that the active configuration file contains
	// exactly the given revision and returns everything the supervisor needs.
	MaterializeActive(ctx context.Context, profileID, revisionID string) (Materialized, error)
}

// Materialized describes the configuration file that is about to be used.
type Materialized struct {
	// Path is the absolute path of the configuration sing-box must read.
	Path string `json:"path"`
	// RequiresPrivilege is true when the configuration needs a TUN device and
	// therefore an elevated launch (spec §27).
	RequiresPrivilege bool `json:"requiresPrivilege"`
	// RevisionID is the revision the file was written from.
	RevisionID string `json:"revisionId"`
	// ClashAPI describes the local control API of this configuration, if any.
	// Traffic monitoring depends on it (spec §45).
	ClashAPI ClashAPI `json:"clashApi"`
}

// ClashAPI is the local sing-box control endpoint read out of a configuration.
type ClashAPI struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"baseUrl"`
	Secret  string `json:"-"`
}

// BinaryPort resolves the sing-box executable and its version.
// It is implemented by internal/app/binary.
type BinaryPort interface {
	// Path returns the absolute path of the executable to run, or a typed
	// BINARY_NOT_FOUND error.
	Path(ctx context.Context) (string, error)
	// Version returns the version of the executable at Path.
	Version(ctx context.Context) (singbox.Version, error)
}

// Observer is notified when the runtime enters and leaves RUNNING so that
// dependent subsystems (the traffic collector) follow the process lifecycle
// instead of being started by opening a page (spec §45).
type Observer interface {
	RuntimeRunning(ctx context.Context, info RunInfo)
	RuntimeStopped()
}

// RunInfo describes a running instance to observers.
type RunInfo struct {
	ProfileID       string
	RevisionID      string
	PID             int
	ClashAPIEnabled bool
	ClashAPIBaseURL string
	ClashAPISecret  string
}

// Timings are the supervisor's time budgets; tests shrink them.
type Timings struct {
	// StartGrace is how long a freshly started process must survive before it
	// counts as RUNNING.
	StartGrace time.Duration
	// StopGrace is how long a process gets to exit after a graceful signal.
	StopGrace time.Duration
	// KillGrace is how long a process gets after being killed.
	KillGrace time.Duration
}

// DefaultTimings are the production budgets.
func DefaultTimings() Timings {
	return Timings{StartGrace: 1500 * time.Millisecond, StopGrace: 6 * time.Second, KillGrace: 3 * time.Second}
}

// Deps are the collaborators of the supervisor.
type Deps struct {
	Store          Store
	Config         ConfigPort
	Binaries       BinaryPort
	Privilege      privilege.Runner
	Emitter        events.Emitter
	Logger         *slog.Logger
	Paths          platform.Paths
	Observer       Observer
	Timings        Timings
	LogCapacity    int
	LogFlushPeriod time.Duration
	// CheckTimeout bounds the pre-start `sing-box check`; zero means the
	// built-in default.
	CheckTimeout time.Duration
	Now          func() time.Time
}

// Supervisor is the single owner of the managed process.
type Supervisor struct {
	deps Deps

	// opMu serialises every lifecycle command; it is held for the whole of a
	// start, stop or restart, so no two transitions can interleave.
	opMu sync.Mutex
	// mu guards the snapshot fields below.
	mu      sync.RWMutex
	status  domruntime.Status
	proc    privilege.Process
	exitCh  chan exitResult
	workers sync.WaitGroup
	// logPath is the file the last launch writes its output to. Startup
	// failures read it back from disk: a process that dies in milliseconds can
	// outrun the live log buffer, and then the reason for the failure would be
	// lost (spec §38: never hide a startup failure).
	logPath string
	// rootCtx is cancelled on shutdown; every worker derives from it.
	rootCtx    context.Context
	cancelRoot context.CancelFunc
	shutDown   bool

	logs *logBuffer
}

type exitResult struct {
	code int
	err  error
}

// New builds the supervisor. The returned context-free supervisor must be
// shut down through Shutdown before the application exits (spec §26).
func New(parent context.Context, deps Deps) *Supervisor {
	if deps.Timings == (Timings{}) {
		deps.Timings = DefaultTimings()
	}
	if deps.LogFlushPeriod <= 0 {
		deps.LogFlushPeriod = 100 * time.Millisecond
	}
	if deps.LogCapacity <= 0 {
		deps.LogCapacity = 4000
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Emitter == nil {
		deps.Emitter = events.Noop{}
	}
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	ctx, cancel := context.WithCancel(parent)
	s := &Supervisor{
		deps:       deps,
		status:     domruntime.Stopped(),
		rootCtx:    ctx,
		cancelRoot: cancel,
	}
	s.logs = newLogBuffer(deps.LogCapacity, deps.LogFlushPeriod, deps.Emitter, deps.Logger, deps.Now)
	return s
}

// Status returns the current typed snapshot (spec §22).
func (s *Supervisor) Status() domruntime.Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.status
	if out.State == domruntime.StateRunning && out.StartedAt != nil {
		out.UptimeSeconds = int64(s.deps.Now().Sub(*out.StartedAt).Seconds())
	}
	return out
}

// Running reports whether a managed process is alive.
func (s *Supervisor) Running() bool {
	state := s.Status().State
	return state == domruntime.StateRunning || state == domruntime.StateStarting
}

// ActiveProfileID returns the profile of the running process, or the last one.
func (s *Supervisor) ActiveProfileID() string { return s.Status().ActiveProfileID }

// StartProfile starts the active revision of a profile.
func (s *Supervisor) StartProfile(ctx context.Context, profileID string) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.startLocked(ctx, profileID)
}

// Stop terminates the managed process if it is running.
func (s *Supervisor) Stop(ctx context.Context) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.stopLocked(ctx, "stop requested")
}

// Restart stops and starts the managed process again. When nothing is running it
// simply starts the given profile.
func (s *Supervisor) Restart(ctx context.Context) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	profileID := s.Status().ActiveProfileID
	if s.Running() {
		if err := s.stopLocked(ctx, "restart requested"); err != nil {
			return err
		}
	}
	if profileID == "" {
		return apperr.New(apperr.CodeRuntimeNotRunning, "runtime.Restart", "nothing is running and no profile is selected")
	}
	return s.startLocked(ctx, profileID)
}

// Shutdown is the mandatory application-shutdown sequence (spec §26): new
// operations are rejected, the process is stopped, workers are cancelled and the
// final state is emitted. It is safe to call more than once.
func (s *Supervisor) Shutdown(ctx context.Context) error {
	s.opMu.Lock()
	if s.shutDown {
		s.opMu.Unlock()
		return nil
	}
	s.shutDown = true
	err := s.stopLocked(ctx, "application shutting down")
	s.opMu.Unlock()

	s.cancelRoot()
	s.workers.Wait()
	s.logs.close()
	// Spec §26: the shutdown sequence must complete from any state, so "nothing
	// was running" is not a shutdown failure.
	if apperr.IsCode(err, apperr.CodeRuntimeNotRunning) {
		return nil
	}
	return err
}

// ShuttingDown reports whether the supervisor has entered shutdown.
func (s *Supervisor) ShuttingDown() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.shutDown
}

func (s *Supervisor) startLocked(ctx context.Context, profileID string) error {
	const op = "runtime.StartProfile"
	if s.isShuttingDown() {
		return apperr.New(apperr.CodeAppShuttingDown, op, "the application is shutting down")
	}
	switch state := s.Status().State; state {
	case domruntime.StateStarting, domruntime.StateRunning, domruntime.StateStopping:
		return apperr.Newf(apperr.CodeRuntimeAlreadyRunning, op, "sing-box is already %s", state)
	}
	if profileID == "" {
		return apperr.New(apperr.CodeInvalidArgument, op, "a profile must be selected")
	}

	p, err := s.deps.Store.GetProfile(ctx, profileID)
	if err != nil {
		return err
	}
	rev, err := s.deps.Store.ActiveRevision(ctx, profileID)
	if err != nil {
		return err
	}
	materialized, err := s.deps.Config.MaterializeActive(ctx, p.ID, rev.ID)
	if err != nil {
		return err
	}
	binaryPath, err := s.deps.Binaries.Path(ctx)
	if err != nil {
		return s.failLocked(op, err)
	}
	version, err := s.deps.Binaries.Version(ctx)
	if err != nil {
		return s.failLocked(op, err)
	}
	// Validate the very file the process is about to read (spec §38). A stored
	// revision can predate the managed sing-box — the app itself used to write
	// the legacy DNS form 1.14 removed — and launching it anyway produces a
	// sixty-millisecond crash plus a status that tells the user nothing.
	if err := s.validateBeforeStart(ctx, op, binaryPath, materialized.Path); err != nil {
		s.deps.Logger.Warn("refusing to start an invalid configuration", "operation", op,
			"profileId", p.ID, "revisionId", rev.ID, "error", err)
		return s.failLocked(op, err)
	}

	runDir, err := s.runtimeDir(rev.ID)
	if err != nil {
		return s.failLocked(op, err)
	}
	request := privilege.Request{
		BinaryPath: binaryPath,
		ConfigPath: materialized.Path,
		WorkDir:    runDir,
		LogPath:    runDir + "/sing-box.log",
		PIDPath:    runDir + "/sing-box.pid",
		StatusPath: runDir + "/sing-box.status",
		Elevate:    materialized.RequiresPrivilege,
		Reason:     "TUN device required by profile " + p.Name,
	}
	if err := request.Validate(); err != nil {
		return s.failLocked(op, apperr.Wrap(apperr.CodeInternal, op, "invalid launch request", err))
	}

	s.startingLocked(p.ID, rev.ID, binaryPath, version, materialized.Path)
	s.setLogPath(request.LogPath)
	s.logs.reset(rev.ID)

	proc, err := s.deps.Privilege.Start(s.rootCtx, request)
	if err != nil {
		if errors.Is(err, privilege.ErrCancelled) {
			return s.failLocked(op, apperr.Wrap(apperr.CodePrivilegeDenied, op,
				"administrator privileges are required to start this configuration", err))
		}
		return s.failLocked(op, apperr.Wrap(apperr.CodeRuntimeStartFailed, op, "could not start sing-box", err))
	}
	s.setProcess(proc)
	s.logs.attach(proc.Logs(), s.rootCtx)
	s.watchExit(proc)

	if err := s.awaitHealthy(proc); err != nil {
		if stopErr := s.stopLocked(context.WithoutCancel(ctx), "failed health check"); stopErr != nil {
			s.deps.Logger.Warn("could not stop the unhealthy process", "operation", op, "error", stopErr)
		}
		return s.failLocked(op, err)
	}

	s.runningLocked(proc)
	s.deps.Logger.Info("sing-box started", "operation", op,
		"profileId", p.ID, "revisionId", rev.ID, "pid", proc.PID(), "elevated", proc.Elevated())
	if s.deps.Observer != nil {
		s.deps.Observer.RuntimeRunning(s.rootCtx, RunInfo{
			ProfileID:       p.ID,
			RevisionID:      rev.ID,
			PID:             proc.PID(),
			ClashAPIEnabled: materialized.ClashAPI.Enabled,
			ClashAPIBaseURL: materialized.ClashAPI.BaseURL,
			ClashAPISecret:  materialized.ClashAPI.Secret,
		})
	}
	if err := s.deps.Store.TouchLastUsed(ctx, p.ID); err != nil {
		s.deps.Logger.Warn("could not record last-used time", "operation", op, "error", err)
	}
	return nil
}

// awaitHealthy confirms the process survived startup instead of reporting a
// process that crashes immediately as RUNNING.
func (s *Supervisor) awaitHealthy(proc privilege.Process) error {
	const op = "runtime.awaitHealthy"
	select {
	case res := <-s.exitCh:
		s.restoreExitResult(res)
		return s.exitError(op, res)
	case <-time.After(s.deps.Timings.StartGrace):
		return nil
	case <-s.rootCtx.Done():
		return apperr.New(apperr.CodeAppShuttingDown, op, "the application is shutting down")
	}
}

// validateBeforeStart runs `sing-box check` on the file the process is about to
// read and turns a rejection into the error the user sees.
//
// Nothing is launched when the binary refuses the file (spec §38: "if validation
// fails, do not start"), so a profile that sing-box cannot even parse stops
// looking like a working profile that mysteriously refuses to come up.
func (s *Supervisor) validateBeforeStart(ctx context.Context, op, binaryPath, configPath string) error {
	timeout := s.deps.CheckTimeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	check, err := singbox.Check(ctx, binaryPath, configPath, timeout)
	if err == nil && check.OK {
		return nil
	}
	details := append([]string{}, check.Errors...)
	if err != nil {
		if typed, ok := apperr.As(err); ok {
			details = append(details, typed.Details...)
		} else {
			details = append(details, err.Error())
		}
	}
	rejected := apperr.New(apperr.CodeConfigCheckFailed, op,
		"sing-box rejected the configuration, so it was not started")
	if len(check.Errors) > 0 {
		rejected.Message = "sing-box rejected the configuration: " + strings.TrimSpace(check.Errors[0])
	}
	return apperr.WithDetails(rejected, details...)
}

// setLogPath records where the current launch writes its output.
func (s *Supervisor) setLogPath(path string) {
	s.mu.Lock()
	s.logPath = path
	s.mu.Unlock()
}

// logFileTail returns the newest lines of the launch log file.
func (s *Supervisor) logFileTail(n int) []string {
	s.mu.RLock()
	path := s.logPath
	s.mu.RUnlock()
	return tailFileLines(path, n)
}

func (s *Supervisor) exitError(op string, res exitResult) error {
	err := apperr.Newf(apperr.CodeRuntimeStartFailed, op, "sing-box exited during startup with code %d", res.code)
	if res.err != nil {
		err.Message = "sing-box exited during startup: " + res.err.Error()
	}
	// Always report the exit status: the log tail is empty when the process dies
	// before any of its output is read, and the caller still needs to know why
	// startup failed.
	details := []string{fmt.Sprintf("exitCode=%d", res.code)}
	if res.err != nil {
		details = append(details, "waitError="+res.err.Error())
	}
	// The live buffer is read first and the file second: a process that dies
	// inside the startup grace can exit before its output reached the buffer,
	// and the user would otherwise be told "code 1" with no explanation.
	seen := make(map[string]bool, 12)
	for _, line := range append(s.logs.tail(6), s.logFileTail(6)...) {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		details = append(details, line)
	}
	return apperr.WithDetails(err, details...)
}

func (s *Supervisor) stopLocked(ctx context.Context, reason string) error {
	const op = "runtime.Stop"
	s.mu.RLock()
	state := s.status.State
	proc := s.proc
	s.mu.RUnlock()

	if proc == nil || (state != domruntime.StateRunning && state != domruntime.StateStarting && state != domruntime.StateFailed) {
		if state == domruntime.StateStopped {
			return apperr.New(apperr.CodeRuntimeNotRunning, op, "sing-box is not running")
		}
		return nil
	}

	s.stoppingLocked(reason)
	if err := proc.Terminate(); err != nil {
		s.deps.Logger.Warn("graceful termination failed", "operation", op, "error", err)
	}

	var code int
	select {
	case res := <-s.exitCh:
		code = res.code
	case <-time.After(s.deps.Timings.StopGrace):
		s.deps.Logger.Warn("sing-box ignored the graceful signal; killing it", "operation", op)
		if err := proc.Kill(); err != nil {
			s.deps.Logger.Error("could not kill sing-box", "operation", op, "error", err)
		}
		select {
		case res := <-s.exitCh:
			code = res.code
		case <-time.After(s.deps.Timings.KillGrace):
			s.deps.Logger.Error("sing-box is still alive after a forced kill", "operation", op, "pid", proc.PID())
			s.stoppedLocked(-1, "sing-box could not be terminated")
			return apperr.New(apperr.CodeRuntimeStopFailed, op, "sing-box could not be terminated")
		}
	}

	s.detachProcess()
	s.logs.detach()
	s.stoppedLocked(code, "")
	s.deps.Logger.Info("sing-box stopped", "operation", op, "exitCode", code, "reason", reason)
	if s.deps.Observer != nil {
		s.deps.Observer.RuntimeStopped()
	}
	return nil
}

// watchExit consumes the single Wait result and reports an unexpected exit as
// FAILED (spec §22).
func (s *Supervisor) watchExit(proc privilege.Process) {
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		code, err := proc.Wait()
		res := exitResult{code: code, err: err}
		select {
		case s.exitCh <- res:
		case <-s.rootCtx.Done():
		}
		s.mu.Lock()
		crashed := s.proc == proc && s.status.State == domruntime.StateRunning
		s.mu.Unlock()
		if crashed {
			s.deps.Logger.Error("sing-box exited unexpectedly", "exitCode", code, "pid", proc.PID())
			s.detachProcess()
			s.logs.detach()
			s.failedLocked(apperr.Newf(apperr.CodeRuntimeStartFailed, "runtime.watch",
				"sing-box exited unexpectedly with code %d", code), code)
			if s.deps.Observer != nil {
				s.deps.Observer.RuntimeStopped()
			}
		}
	}()
}

// restoreExitResult returns a result that was consumed by awaitHealthy to the
// channel so stopLocked can observe it.
func (s *Supervisor) restoreExitResult(res exitResult) {
	select {
	case s.exitCh <- res:
	default:
		go func() {
			select {
			case s.exitCh <- res:
			case <-s.rootCtx.Done():
			}
		}()
	}
}

// --- state transitions -------------------------------------------------------------------

func (s *Supervisor) startingLocked(profileID, revisionID, binaryPath string, version singbox.Version, configPath string) {
	s.mu.Lock()
	s.status.State = domruntime.StateStarting
	s.status.ActiveProfileID = profileID
	s.status.ActiveRevisionID = revisionID
	s.status.BinaryVersion = version.String()
	s.status.ConfigPath = configPath
	s.status.LastError = ""
	s.status.LastErrorCode = ""
	s.status.PID = 0
	s.status.StartedAt = nil
	s.status.UptimeSeconds = 0
	s.status.Elevated = false
	s.exitCh = make(chan exitResult, 1)
	s.mu.Unlock()
	s.emitStatus()
}

func (s *Supervisor) runningLocked(proc privilege.Process) {
	now := s.deps.Now()
	s.mu.Lock()
	s.status.State = domruntime.StateRunning
	s.status.PID = proc.PID()
	s.status.Elevated = proc.Elevated()
	s.status.StartedAt = &now
	s.mu.Unlock()
	s.emitStatus()
}

func (s *Supervisor) stoppingLocked(reason string) {
	s.mu.Lock()
	s.status.State = domruntime.StateStopping
	s.mu.Unlock()
	s.deps.Logger.Info("stopping sing-box", "reason", reason)
	s.emitStatus()
}

func (s *Supervisor) stoppedLocked(exitCode int, message string) {
	s.mu.Lock()
	s.status.State = domruntime.StateStopped
	s.status.PID = 0
	s.status.StartedAt = nil
	s.status.UptimeSeconds = 0
	s.status.Elevated = false
	s.status.LastExitCode = &exitCode
	if message != "" {
		s.status.LastError = message
		s.status.LastErrorCode = string(apperr.CodeRuntimeStopFailed)
	}
	s.mu.Unlock()
	s.emitStatus()
}

func (s *Supervisor) failedLocked(err error, exitCode int) error {
	code := apperr.CodeOf(err)
	s.mu.Lock()
	s.status.State = domruntime.StateFailed
	s.status.PID = 0
	s.status.StartedAt = nil
	s.status.UptimeSeconds = 0
	s.status.LastError = apperr.MessageOf(err)
	s.status.LastErrorCode = string(code)
	if exitCode != 0 {
		s.status.LastExitCode = &exitCode
	}
	s.mu.Unlock()
	s.emitStatus()
	return err
}

func (s *Supervisor) setProcess(proc privilege.Process) {
	s.mu.Lock()
	s.proc = proc
	s.status.PID = proc.PID()
	s.status.Elevated = proc.Elevated()
	s.mu.Unlock()
}

func (s *Supervisor) detachProcess() {
	s.mu.Lock()
	s.proc = nil
	s.mu.Unlock()
}

// failLocked records a start failure that happened before a process existed and
// returns the typed error the caller must propagate (spec §22).
func (s *Supervisor) failLocked(op string, err error) error {
	return s.failedLocked(apperr.From(op, apperr.CodeRuntimeStartFailed, err), 0)
}

// runtimeDir returns the per-launch scratch directory for pid, status and log
// files. It is created with owner-only permissions because sing-box log lines
// can contain user data.
func (s *Supervisor) runtimeDir(revisionID string) (string, error) {
	if revisionID == "" {
		revisionID = "unversioned"
	}
	dir := filepath.Join(s.deps.Paths.RuntimeDir, revisionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", apperr.Wrap(apperr.CodeInternal, "runtime.runtimeDir",
			"the runtime directory could not be created", err)
	}
	return dir, nil
}

func (s *Supervisor) isShuttingDown() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.shutDown
}

func (s *Supervisor) emitStatus() {
	s.deps.Emitter.Emit(events.RuntimeStatus, s.Status())
}

// Logs returns the buffered log records matching the query.
//
// The buffer is bounded and owned by the supervisor (spec §47): the frontend
// asks for a tail once and then follows runtime:log events instead of polling.
func (s *Supervisor) Logs(q LogQuery) []LogRecord {
	if s.logs == nil {
		return nil
	}
	return s.logs.query(q)
}

// ClearLogs empties the in-memory log buffer. The sing-box process keeps
// running and keeps producing output.
func (s *Supervisor) ClearLogs() {
	if s.logs == nil {
		return
	}
	s.logs.clear()
}

// LogTail returns the most recent log lines as plain text.
func (s *Supervisor) LogTail(n int) []string {
	if s.logs == nil {
		return nil
	}
	return s.logs.tail(n)
}
