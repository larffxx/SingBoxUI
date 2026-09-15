package privrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"time"

	"github.com/larffxx/singboxui/internal/privilege"
)

// Elevator is the single operation a platform provides for privileged launches:
// run one already-validated helper invocation with the rights the operating
// system grants after asking the user (internal-contracts.md §3).
//
// The invocation is an argument array that has been through privhelper.Validate,
// so a platform never has to decide what is safe to run.
type Elevator interface {
	// HelperPath returns the absolute path of the helper executable, or a typed
	// error when the helper is not installed beside the application.
	HelperPath() (string, error)
	// Elevate runs one helper invocation, asking for administrator rights.
	//
	// A dismissal is reported as privilege.ErrCancelled; a platform that cannot
	// escalate at all reports privilege.ErrUnsupported.
	Elevate(ctx context.Context, argv []string) (Elevated, error)
}

// Elevated is a running elevated program, which for this application is always a
// privileged helper invocation.
//
// The helper's lifetime equals the lifetime of the sing-box process it
// supervises, which is what makes this handle a usable liveness signal even when
// the child itself cannot be inspected by an unprivileged process.
type Elevated interface {
	// PID reports the launcher's process id, or 0 when the mechanism does not
	// report one. The pid of sing-box is always read from the pid file.
	PID() int
	// Exited reports whether the elevated program has ended.
	Exited() bool
	// Status reports how the elevated program ended. It is only final once
	// Exited reports true.
	Status() error
	// Release drops the session, ending the elevated program if it still runs.
	Release()
}

// Operations of this package as they appear in typed errors.
const (
	opStart = "platform.privilege.start"
	opStop  = "platform.privilege.stop"
)

// Timing of the privileged launch path.
const (
	// readinessPoll is how often the pid file is checked while the helper starts.
	readinessPoll = 25 * time.Millisecond
	// readinessTimeout bounds the wait for the helper to record the process.
	readinessTimeout = 30 * time.Second
	// stopTimeout bounds an elevated stop, including the elevation prompt.
	stopTimeout = 30 * time.Second
	// statusGrace is how long the exit record may lag behind the helper's exit.
	statusGrace = 2 * time.Second
	// exitCodeUnknown matches childrun's unknown code.
	exitCodeUnknown = -1
)

// Runner is the privileged-execution port for one application data directory.
//
// It satisfies privilege.Runner: starting without Request.Elevate runs sing-box
// directly, and starting with it goes through the helper, so the two paths
// behave identically (spec §27, §28).
type Runner struct {
	dataDir  string
	elevator Elevator
}

// New returns the runner of one data directory. The data directory must be
// absolute: every path of a request is resolved against it.
func New(dataDir string, elevator Elevator) (*Runner, error) {
	if dataDir == "" || !isAbs(dataDir) {
		return nil, invalid("the application data directory must be an absolute path, got %q", dataDir)
	}
	if elevator == nil {
		return nil, invalid("a privileged launch needs an elevation mechanism")
	}
	return &Runner{dataDir: dataDir, elevator: elevator}, nil
}

// Start launches the requested process and returns a handle to it.
func (r *Runner) Start(ctx context.Context, req privilege.Request) (privilege.Process, error) {
	spec := newSpec(req, r.dataDir)
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if err := prepare(spec); err != nil {
		return nil, err
	}
	if !req.Elevate {
		return r.startLocal(ctx, spec)
	}
	return r.startElevated(ctx, spec)
}

// Supported reports whether privileged launches are possible on this system and
// why not when they are not.
func (r *Runner) Supported() (bool, string) {
	if _, err := r.elevator.HelperPath(); err != nil {
		return false, describe(err)
	}
	return true, ""
}

// startLocal supervises sing-box with the rights this process already has. It is
// the same supervision the helper uses, so the runtime supervisor cannot tell
// the paths apart.
func (r *Runner) startLocal(ctx context.Context, spec specType) (privilege.Process, error) {
	return childrunStart(ctx, spec)
}

// startElevated asks for administrator rights and waits for the helper to record
// the process it started.
func (r *Runner) startElevated(ctx context.Context, spec specType) (privilege.Process, error) {
	helperPath, err := r.elevator.HelperPath()
	if err != nil {
		return nil, err
	}
	session, err := r.elevator.Elevate(ctx, spec.StartArgv(helperPath))
	if err != nil {
		return nil, r.elevationError(err)
	}
	pid, err := r.awaitChild(ctx, spec, session)
	if err != nil {
		// The helper may still be starting; releasing the session stops it and,
		// with it, whatever it was told to start.
		session.Release()
		return nil, err
	}
	return &process{ctx: ctx, runner: r, spec: spec, pid: pid, session: session}, nil
}

// awaitChild waits for the helper to record the process it started. The pid file
// is the readiness signal on every platform, because the request carries its path
// (internal-contracts.md §3).
func (r *Runner) awaitChild(ctx context.Context, spec specType, session Elevated) (int, error) {
	deadline := time.Now().Add(readinessTimeout)
	for {
		record, err := childrunReadPID(spec.PIDPath)
		switch {
		case err == nil && record.PID > 0:
			return record.PID, nil
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			return 0, err
		}
		if session.Exited() {
			return 0, r.helperFailure(spec, session)
		}
		if err := ctx.Err(); err != nil {
			return 0, shuttingDown(err)
		}
		if !time.Now().Before(deadline) {
			return 0, timeoutFailure(spec)
		}
		select {
		case <-ctx.Done():
			return 0, shuttingDown(ctx.Err())
		case <-time.After(readinessPoll):
		}
	}
}

// helperFailure explains a helper that ended before it recorded a process.
func (r *Runner) helperFailure(spec specType, session Elevated) error {
	reason := ""
	if status, err := childrunReadStatus(spec.StatusPath); err == nil {
		reason = status.Error
	}
	err := session.Status()
	if errors.Is(err, privilege.ErrCancelled) {
		return cancelledError(err)
	}
	message := "the privileged helper could not start sing-box"
	if reason != "" {
		message = "the privileged helper could not start sing-box: " + reason
	}
	return wrapStart(message, err)
}

// elevationError turns the mechanism's failure into a typed error. A dismissal
// is PRIVILEGE_DENIED; anything else is a start failure (spec §28).
func (r *Runner) elevationError(err error) error {
	switch {
	case errors.Is(err, privilege.ErrCancelled):
		return cancelledError(err)
	case errors.Is(err, privilege.ErrUnsupported):
		return apperrWrap(apperrCodeRuntimeStartFailed, opStart,
			"privileged launches are not supported on this system", err)
	default:
		return wrapStart("administrator rights could not be obtained", err)
	}
}

// Stop stops the process recorded in pidPath (ADR 012).
//
// A process this user owns is signalled directly, so no elevation prompt appears
// for it; a process that runs as administrator answers ErrNotPermitted, and only
// then is the helper asked to do it — an unprivileged application cannot signal
// one that runs as administrator, and the narrow helper is the only way
// (internal-contracts.md §3).
func (r *Runner) Stop(ctx context.Context, pidPath string, force bool) error {
	if _, err := childrunStopVerified(ctx, pidPath, force); err == nil {
		return nil
	} else if !childrunNotPermitted(err) {
		return err
	}
	return r.stopProcess(ctx, pidPath, force)
}

// stopProcess stops a process through the helper. An unprivileged application
// cannot signal a process that runs as administrator, so another elevated
// invocation of the same narrow helper is the only way (internal-contracts.md
// §3).
func (r *Runner) stopProcess(ctx context.Context, pidPath string, force bool) error {
	req := stopRequest(r.dataDir, pidPath, force)
	if err := req.Validate(); err != nil {
		return err
	}
	record, err := childrunReadPID(pidPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	case !childrunAlivePID(record.PID):
		// Nothing to stop: no elevation prompt for a no-op.
		return nil
	}

	helperPath, err := r.elevator.HelperPath()
	if err != nil {
		return err
	}
	stopCtx, cancel := context.WithTimeout(ctx, stopTimeout)
	defer cancel()
	session, err := r.elevator.Elevate(stopCtx, stopArgv(helperPath, r.dataDir, pidPath, force))
	if err != nil {
		return r.elevationError(err)
	}
	defer session.Release()
	for !session.Exited() {
		select {
		case <-stopCtx.Done():
			return apperrWrap(apperrCodeRuntimeStopFailed, opStop,
				"the privileged stop did not finish", stopCtx.Err())
		case <-time.After(readinessPoll):
		}
	}
	if err := session.Status(); err != nil {
		if errors.Is(err, privilege.ErrCancelled) {
			return cancelledError(err)
		}
		return apperrWrap(apperrCodeRuntimeStopFailed, opStop,
			"the privileged helper could not stop sing-box", err)
	}
	return nil
}

// process is the handle of a privileged sing-box process.
type process struct {
	ctx     context.Context
	runner  *Runner
	spec    specType
	pid     int
	session Elevated
}

// PID returns the process id recorded by the helper.
func (p *process) PID() int { return p.pid }

// Elevated reports that the process runs with administrator privileges.
func (p *process) Elevated() bool { return true }

// Logs returns the merged stdout+stderr stream of the privileged process. The
// helper writes the same log file a local child would, so this is the same
// reader with the same cancellation behaviour (spec §62).
func (p *process) Logs() io.ReadCloser {
	return childrunTail(p.ctx, p.spec.LogPath, p.Exited)
}

// Exited reports whether the process has terminated.
//
// For a privileged process the helper's lifetime is the signal: the helper
// supervises sing-box and exits with it (the helper's supervision is what makes
// the exit code readable from the status file). This also keeps the answer
// correct where an unprivileged process is refused information about a
// privileged one.
func (p *process) Exited() bool {
	if p.session != nil {
		return p.session.Exited()
	}
	return !childrunAlivePID(p.pid)
}

// Wait blocks until the privileged process exits and returns the exit code the
// helper recorded.
func (p *process) Wait() (int, error) {
	deadline := time.Now().Add(statusGrace)
	for {
		if p.Exited() {
			status, err := childrunReadStatus(p.spec.StatusPath)
			switch {
			case err == nil && status.Error != "":
				return status.ExitCode, apperrWrap(apperrCodeRuntimeStartFailed, opStart,
					"the privileged process ended abnormally", errors.New(status.Error))
			case err == nil:
				return status.ExitCode, nil
			case errors.Is(err, fs.ErrNotExist):
				if time.Now().After(deadline) {
					return exitCodeUnknown, apperrWrap(apperrCodeRuntimeStartFailed, opStart,
						"the privileged process ended without recording an exit code", err)
				}
			default:
				return exitCodeUnknown, err
			}
		}
		select {
		case <-p.ctx.Done():
			return exitCodeUnknown, shuttingDown(p.ctx.Err())
		case <-time.After(readinessPoll):
		}
	}
}

// Terminate asks the privileged process to stop gracefully.
func (p *process) Terminate() error { return p.stop(false) }

// Kill stops the privileged process without waiting for cooperation.
func (p *process) Kill() error { return p.stop(true) }

// stop stops the process through the helper. Stopping an already stopped process
// succeeds, so the supervisor may call it again after a failure.
func (p *process) stop(force bool) error {
	return p.runner.stopProcess(p.ctx, p.spec.PIDPath, force)
}

// describe is the human-readable half of Supported: a typed error's message, or
// the error itself when it is not typed.
func describe(err error) string {
	if message := apperrMessage(err); message != "" {
		return message
	}
	return fmt.Sprintf("%v", err)
}
