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
func taskkill(pid int, force bool) error {
	args := []string{"/PID", strconv.Itoa(pid), "/T"}
	if force {
		args = append(args, "/F")
	}
	out, err := exec.Command("taskkill", args...).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if strings.Contains(strings.ToLower(message), "not found") || strings.Contains(strings.ToLower(message), "не найден") {
			return fmt.Errorf("childrun: taskkill %d: %w", pid, ErrProcessGone)
		}
		return fmt.Errorf("childrun: taskkill %d (%s): %w: %s", pid, strings.Join(args, " "), err, message)
	}
	return nil
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
