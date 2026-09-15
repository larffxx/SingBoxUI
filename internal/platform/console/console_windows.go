//go:build windows

package console

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// windowless merges the two flags that keep a console away: CREATE_NO_WINDOW tells Windows not
// to allocate one for the child at all, and the hidden-window attribute covers the terminal
// host, which creates a window of its own regardless of the console attributes.
//
// Existing flags are kept, so a caller that needs CREATE_NEW_PROCESS_GROUP of its own still
// gets it.
func windowless(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	attr := cmd.SysProcAttr
	if attr == nil {
		attr = &syscall.SysProcAttr{}
		cmd.SysProcAttr = attr
	}
	attr.CreationFlags |= windows.CREATE_NO_WINDOW
	attr.HideWindow = true
}
