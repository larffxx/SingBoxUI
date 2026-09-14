//go:build !windows

package console

import "os/exec"

// windowless does nothing here: this platform starts a child without giving it a window, so
// there is nothing to keep off the desktop.
func windowless(*exec.Cmd) {}
