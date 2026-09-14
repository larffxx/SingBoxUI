//go:build windows

package console

import (
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// TestWindowlessAsksForNoConsole pins the two flags that were measured to keep the window
// away: CREATE_NO_WINDOW, which stops Windows from allocating a console for the child at all,
// and the hidden-window attribute, which covers the terminal host that creates a window of
// its own regardless of the console attributes.
func TestWindowlessAsksForNoConsole(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "exit", "0")
	Windowless(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("Windowless() left SysProcAttr nil, want the console flags")
	}
	if cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Errorf("CreationFlags = %#x, want CREATE_NO_WINDOW (%#x) set",
			cmd.SysProcAttr.CreationFlags, windows.CREATE_NO_WINDOW)
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Error("HideWindow = false, want true")
	}
}

// TestWindowlessKeepsFlagsACallerSet pins that the flags are merged, not replaced: childrun
// gives the core a process group of its own, and a stop of that core depends on it.
func TestWindowlessKeepsFlagsACallerSet(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "exit", "0")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	Windowless(cmd)

	flags := cmd.SysProcAttr.CreationFlags
	if flags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Errorf("CreationFlags = %#x, want CREATE_NEW_PROCESS_GROUP kept", flags)
	}
	if flags&windows.CREATE_NO_WINDOW == 0 {
		t.Errorf("CreationFlags = %#x, want CREATE_NO_WINDOW added", flags)
	}
}

// TestWindowlessToleratesACommandWithoutAttributes keeps the helper usable on a command that
// was built with a SysProcAttr of its own or none at all.
func TestWindowlessToleratesACommandWithoutAttributes(t *testing.T) {
	Windowless(nil) // must not panic: the call sites build commands, they never pass nil
}

// TestWindowlessChildStillTalksToItsParent is the other half of the contract: the flags
// change nothing about the child except its console. Its output still arrives, and a non-zero
// exit status still means what it meant.
func TestWindowlessChildStillTalksToItsParent(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "echo hello")
	Windowless(cmd)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("windowless child failed: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "hello" {
		t.Errorf("output = %q, want %q", got, "hello")
	}

	failing := exec.Command("cmd.exe", "/c", "exit 3")
	Windowless(failing)
	err = failing.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Errorf("exit error = %v, want an exit status of 3", err)
	}
}
