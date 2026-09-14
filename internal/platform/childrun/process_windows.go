//go:build windows

package childrun

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// filetimeToUnixEpoch is the number of 100ns FILETIME ticks between the Windows
// epoch (1601-01-01) and the unix epoch (1970-01-01).
const filetimeToUnixEpoch = 116444736000000000

// StartTimeNano reports when pid started, as unix nanoseconds, read from the
// kernel. Together with the pid it identifies one process instance, so a
// recycled pid is never signalled (internal-contracts.md §3).
func StartTimeNano(pid int) (int64, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return 0, fmt.Errorf("childrun: open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(handle)
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return 0, fmt.Errorf("childrun: read process times %d: %w", pid, err)
	}
	ticks := int64(creation.HighDateTime)<<32 | int64(creation.LowDateTime)
	return (ticks - filetimeToUnixEpoch) * 100, nil
}

// stillActive is STILL_ACTIVE from <winnt.h>: GetExitCodeProcess reports it for
// a process that has not terminated yet.
const stillActive = 259

// Alive reports whether pid is still running.
func Alive(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == stillActive
}

// Terminate asks the process to stop gracefully.
//
// A console child that shares the console receives CTRL_BREAK_EVENT. sing-box is
// always started windowless (configureChild) and the application and the helper
// own no console to share with it, so in this product that event is normally
// refused — and `taskkill` without /F then refuses a console program outright
// ("This process can only be terminated forcefully"), which used to make every
// stop a hard error: the supervisor waited out its whole stop grace, the
// privileged stop returned exit code 1 and sing-box kept the TUN device.
//
// Windows has no signal to deliver in that case, so the graceful attempt
// degrades to the forced stop. That is the only way "stop" can mean "the process
// is gone", which is what the callers rely on (spec §26); a caller that needs to
// know whether the process cooperated reads it from the result of the stop it
// asked for.
func Terminate(pid int) error {
	if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(pid)); err == nil {
		return nil
	}
	return taskkill(pid, true)
}

// Kill terminates the process tree without waiting for cooperation.
func Kill(pid int) error { return taskkill(pid, true) }

// terminateOwn and killOwn target a child started by this process: it is created
// in its own process group (configureChild), so the same tree commands apply.
func terminateOwn(pid int) error { return Terminate(pid) }

func killOwn(pid int) error { return Kill(pid) }

// taskkill stops pid and its children. Arguments are passed as an array, never
// as a shell command line (spec §24).
//
// Whether this process is allowed to end the target at all is asked first, and
// asked again when taskkill fails: the utility prints a localised message, and
// the answer that matters to the caller — "not permitted" versus "no such
// process" — comes from the kernel, not from that text.
func taskkill(pid int, force bool) error {
	if err := mayTerminate(pid); err != nil {
		return err
	}
	args := []string{"/PID", strconv.Itoa(pid), "/T"}
	if force {
		args = append(args, "/F")
	}
	out, err := exec.Command("taskkill", args...).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if reason := mayTerminate(pid); reason != nil {
			return fmt.Errorf("childrun: taskkill %d (%s): %w: %s", pid, strings.Join(args, " "), reason, message)
		}
		return fmt.Errorf("childrun: taskkill %d (%s): %w: %s", pid, strings.Join(args, " "), err, message)
	}
	return nil
}

// treeStopAccess is what ending a process needs on this platform: `taskkill` identifies the
// process before it ends it, so it asks for the terminate right *and* the full query right.
//
// Both are asked for because one of them is granted where the other is refused: Windows
// answers `permitted` to a probe of PROCESS_TERMINATE alone for a process that runs at a
// higher integrity level — this application's own core, started as administrator — and
// refuses the same probe the moment PROCESS_QUERY_INFORMATION is included. Measured on
// Windows 11 against such a core: the terminate right alone opened it, while
// PROCESS_TERMINATE|PROCESS_QUERY_INFORMATION (and PROCESS_ALL_ACCESS) answered
// "Access is denied", which is exactly what `taskkill` reports for it. A probe of the
// terminate right alone therefore says "allowed" about a process this application cannot end.
const treeStopAccess = windows.PROCESS_TERMINATE | windows.PROCESS_QUERY_INFORMATION

// mayTerminate reports whether this process may end pid the way this platform
// ends processes, before anything is attempted.
//
// It answers ErrNotPermitted for a process the user may not touch — which on
// Windows is every process that runs at a higher integrity level, including this
// application's own sing-box, started as administrator through the privileged
// helper. That answer is what makes `privilege.Runner.Stop` escalate to the
// helper instead of reporting a failure (ADR 012): without it the escalation
// never happened on Windows and an elevated core could not be stopped from the
// application at all.
//
// A pid that no longer exists answers ErrProcessGone, so a process that exited
// between the identity check and the stop is never mistaken for a permission
// problem, and a pid that was recycled is caught by the identity check itself.
func mayTerminate(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("childrun: invalid pid %d", pid)
	}
	handle, err := openForTermination(pid, treeStopAccess)
	if err == nil {
		windows.CloseHandle(handle)
		return nil
	}
	switch {
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		return fmt.Errorf("childrun: terminate %d: %w", pid, ErrNotPermitted)
	case errors.Is(err, windows.ERROR_INVALID_PARAMETER), errors.Is(err, windows.ERROR_NOT_FOUND):
		return fmt.Errorf("childrun: terminate %d: %w", pid, ErrProcessGone)
	default:
		return fmt.Errorf("childrun: open %d for termination: %w", pid, err)
	}
}

// openForTermination asks the kernel for the rights a stop needs. It is a
// variable so that the suite can exercise the refusal the kernel produces for an
// elevated process, which a test running as the current user cannot create.
var openForTermination = func(pid int, access uint32) (windows.Handle, error) {
	return windows.OpenProcess(access, false, uint32(pid))
}

// configureChild detaches the child from the UI's console and gives it its own
// process group so it can be addressed as a tree.
func configureChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}

// exitStatus turns the result of waiting for a child into an exit code. Windows
// has no signals, so the code is never negative.
func exitStatus(err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return exitCodeUnknown, err
	}
	return exitErr.ExitCode(), nil
}
