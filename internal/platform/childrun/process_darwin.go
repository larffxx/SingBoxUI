//go:build darwin

package childrun

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// StartTimeNano reports when pid started, as unix nanoseconds, read from the
// kernel. Together with the pid it identifies one process instance, so a
// recycled pid is never signalled (internal-contracts.md §3).
func StartTimeNano(pid int) (int64, error) {
	state, err := procState(pid)
	if err != nil {
		return 0, err
	}
	return state.startNano, nil
}

// Alive reports whether pid is a running (not zombie) process.
func Alive(pid int) bool {
	state, err := procState(pid)
	if err != nil {
		return false
	}
	return !state.zombie
}

// procStateZombie is SZOMB from <sys/proc.h>: the process has exited and only
// its exit status is still pending, so it must not be treated as running.
const procStateZombie = 5

// processState is the slice of kernel process information the supervisor needs.
type processState struct {
	startNano int64
	zombie    bool
}

func procState(pid int) (processState, error) {
	if pid <= 0 {
		return processState{}, fmt.Errorf("childrun: invalid pid %d", pid)
	}
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return processState{}, fmt.Errorf("childrun: read process %d: %w", pid, err)
	}
	start := info.Proc.P_starttime
	return processState{
		startNano: start.Sec*int64(time.Second) + int64(start.Usec)*int64(time.Microsecond),
		zombie:    info.Proc.P_stat == procStateZombie,
	}, nil
}

// Terminate asks the process to stop gracefully.
func Terminate(pid int) error { return signalProcess(pid, unix.SIGTERM) }

// Kill terminates the process without waiting for cooperation.
func Kill(pid int) error { return signalProcess(pid, unix.SIGKILL) }

// terminateOwn and killOwn target a child started by this process. On macOS the
// child runs in its own session (configureChild) yet is still signalled by pid,
// so both are the same calls as the generic ones.
func terminateOwn(pid int) error { return Terminate(pid) }

func killOwn(pid int) error { return Kill(pid) }

func signalProcess(pid int, signal unix.Signal) error {
	if pid <= 0 {
		return fmt.Errorf("childrun: invalid pid %d", pid)
	}
	err := unix.Kill(pid, signal)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, unix.ESRCH):
		return fmt.Errorf("childrun: signal %d with %s: %w", pid, signal, ErrProcessGone)
	case errors.Is(err, unix.EPERM), errors.Is(err, unix.EACCES):
		return fmt.Errorf("childrun: signal %d with %s: %w", pid, signal, ErrNotPermitted)
	default:
		return fmt.Errorf("childrun: signal %d with %s: %w", pid, signal, err)
	}
}

// configureChild puts the child in its own session: it is a long-lived daemon
// that must not inherit the UI's terminal signals, and it is stopped explicitly.
func configureChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// exitStatus turns the result of waiting for a child into an exit code. A
// negative code means the process was terminated by a signal.
func exitStatus(err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return exitCodeUnknown, err
	}
	if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return -int(status.Signal()), nil
	}
	return exitErr.ExitCode(), nil
}
