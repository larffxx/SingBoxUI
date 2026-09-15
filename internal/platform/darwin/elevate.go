//go:build darwin

package darwin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/platform/privrun"
	"github.com/larffxx/singboxui/internal/privilege"
)

// osascriptPath is the interpreter that can ask macOS for administrator rights.
const osascriptPath = "/usr/bin/osascript"

// releaseGrace is how long a released launcher has to end on its own before it is
// killed.
const releaseGrace = 2 * time.Second

// waitDelay bounds how long Wait may block on a pipe a descendant inherited. The
// shell osascript starts can fork the helper and outlive the launcher while
// holding that pipe open, which would otherwise keep the session un-exited.
const waitDelay = 2 * time.Second

// releasePoll is the interval at which a released launcher is re-checked.
const releasePoll = 25 * time.Millisecond

// cancelCode is AppleScript's userCanceledErr, printed by osascript in the
// "execution error: … (-128)" line when the administrator dialog is dismissed.
const cancelCode = "(-128)"

// Elevator runs the privileged helper with administrator rights through
// osascript: `do shell script "<invocation>" with administrator privileges` shows
// the system password dialog and runs the command as root for exactly as long as
// the command lives (target-state.md §7).
type Elevator struct {
	// executable is osascript. It is a field so tests can exercise the mechanism
	// without a password dialog.
	executable string
}

// A compile-time check that the elevator fulfils the port privrun expects.
var _ privrun.Elevator = (*Elevator)(nil)

// NewElevator returns the osascript elevator. It does not look for the helper:
// HelperPath resolves that per call, so building the platform cannot fail merely
// because the helper is not installed yet.
func NewElevator() *Elevator { return &Elevator{executable: osascriptPath} }

// NewElevatorWith returns an elevator that uses the given interpreter. It exists
// for tests and for a future signed build that ships its own launcher.
func NewElevatorWith(executable string) *Elevator { return &Elevator{executable: executable} }

// HelperPath returns the absolute path of the privileged helper.
func (e *Elevator) HelperPath() (string, error) { return HelperPath() }

// Elevate starts one helper invocation and returns the session that watches it.
//
// argv[0] is the program to run and the remaining elements are its arguments; the
// helper therefore runs with exactly the arguments the caller passed, none of
// which can be interpreted as shell syntax (spec §24, §83).
func (e *Elevator) Elevate(ctx context.Context, argv []string) (privrun.Elevated, error) {
	if len(argv) == 0 {
		return nil, apperr.New(apperr.CodeInvalidArgument, opElevate, "no helper invocation was given")
	}
	if !filepath.IsAbs(argv[0]) {
		return nil, apperr.Newf(apperr.CodeInvalidArgument, opElevate, "the helper path must be absolute, got %q", argv[0])
	}
	for _, arg := range argv {
		if hasControl(arg) {
			return nil, apperr.New(apperr.CodeInvalidArgument, opElevate, "an argument of the helper invocation contains control characters")
		}
	}
	cmd := exec.Command(e.executable, "-e", AppleScript(LaunchScript(argv)))
	// Its own process group, so that releasing the session can end the shell and
	// the helper it started without signalling this process.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = waitDelay
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, apperr.Wrap(apperr.CodeRuntimeStartFailed, opElevate, "osascript could not be started", err)
	}
	session := &session{cmd: cmd, pid: cmd.Process.Pid, stderr: &stderr}
	// Cancelling the context ends the whole process group, exactly as Release does:
	// osascript, the shell it started and the helper that shell forks. Ending only
	// the launcher would leave the privileged helper running (spec §62).
	session.stop = context.AfterFunc(ctx, session.terminate)
	// The goroutine is bound to the command, and collect unregisters the callback
	// above once the launcher has been reaped.
	go session.collect()
	return session, nil
}

// session is the launcher of one privileged helper invocation.
type session struct {
	cmd *exec.Cmd
	pid int

	stderr *bytes.Buffer

	// stop unregisters the context callback that ends the session, so the callback
	// does not outlive the launcher it belongs to.
	stop func() bool

	mu   sync.Mutex
	done bool
	err  error
	// released records that Release ran, so Status does not report a kill as an
	// escalation failure.
	released bool
}

// collect waits for the launcher. It must be started as a goroutine.
func (s *session) collect() {
	err := s.cmd.Wait()
	// The launcher is reaped, so the context has nothing left to end.
	if s.stop != nil {
		s.stop()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.released && err != nil {
		err = nil
	}
	s.err = err
	s.done = true
}

// PID reports the process id of the launcher, not of sing-box: the pid of the
// privileged child is only ever read from the pid file (internal-contracts.md §3).
func (s *session) PID() int { return s.pid }

// Exited reports whether the launcher has ended, which is also when the privileged
// helper ends, and with it the child it supervises.
func (s *session) Exited() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done
}

// Status reports how the elevation ended: nil on success, privilege.ErrCancelled
// when the user dismissed the administrator dialog, and an error carrying
// osascript's own message otherwise. It is only meaningful once Exited is true.
func (s *session) Status() error {
	s.mu.Lock()
	done, err, released := s.done, s.err, s.released
	text := strings.TrimSpace(s.stderr.String())
	s.mu.Unlock()
	if !done || released || err == nil {
		return nil
	}
	if cancelled(err, text) {
		return privilege.ErrCancelled
	}
	if text != "" {
		return fmt.Errorf("osascript: %s", text)
	}
	return fmt.Errorf("osascript: %w", err)
}

// terminate signals the launcher's process group. The shell osascript started,
// the helper it forks and anything that helper runs share the group, so one
// signal reaches all of them: killing only the launcher's pid would leave the
// privileged child behind. The group is asked to stop first and killed if it
// refuses.
func (s *session) terminate() {
	if s.cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-s.pid, syscall.SIGTERM)
	deadline := time.Now().Add(releaseGrace)
	for !s.Exited() && time.Now().Before(deadline) {
		time.Sleep(releasePoll)
	}
	if !s.Exited() {
		_ = syscall.Kill(-s.pid, syscall.SIGKILL)
	}
}

// Release ends the session through the same path a cancelled context takes.
//
// A helper that already started sing-box runs as root in its own session and is
// not reached by this signal: privrun stops the child through the helper itself
// (internal-contracts.md §3).
func (s *session) Release() {
	if s.cmd.Process == nil {
		return
	}
	s.mu.Lock()
	s.released = true
	s.mu.Unlock()
	if s.stop != nil {
		s.stop()
	}
	s.terminate()
}

// cancelled reports whether a failed osascript run means "the user dismissed the
// administrator dialog". The numeric AppleScript code is checked first because the
// message around it is localised.
func cancelled(err error, stderr string) bool {
	if strings.Contains(stderr, cancelCode) {
		return true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.ExitStatus() == -128 {
			return true
		}
	}
	lower := strings.ToLower(stderr)
	return strings.Contains(lower, "cancel") || strings.Contains(lower, "отмен")
}

// LaunchScript quotes one command into a /bin/sh script.
//
// This is the single place where an argv becomes a command line: osascript passes
// its argument to /bin/sh, so every element must be quoted, and the helper the
// command names is never built by string concatenation (spec §24, §83).
func LaunchScript(argv []string) string {
	parts := make([]string, 0, len(argv))
	for _, arg := range argv {
		parts = append(parts, PosixQuote(arg))
	}
	return strings.Join(parts, " ")
}

// PosixQuote returns s as one single-quoted shell word: single quotes expand
// nothing, and an embedded quote is closed, escaped and reopened.
func PosixQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// AppleScript wraps a shell script in the statement that asks for administrator
// rights.
func AppleScript(script string) string {
	escaped := strings.ReplaceAll(script, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `do shell script "` + escaped + `" with administrator privileges`
}
