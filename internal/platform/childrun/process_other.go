//go:build !darwin && !windows

package childrun

import (
	"fmt"
	"os/exec"
	"runtime"
)

// This file keeps the package compiling on operating systems the application
// does not target (spec §1). Everything fails closed: a process is never
// started where it could not be supervised.

func unsupported() error {
	return fmt.Errorf("childrun: process supervision is not supported on %s", runtime.GOOS)
}

// StartTimeNano is unavailable off the supported platforms.
func StartTimeNano(pid int) (int64, error) { return 0, unsupported() }

// Alive reports false where processes cannot be inspected.
func Alive(pid int) bool { return false }

// Terminate is unavailable off the supported platforms.
func Terminate(pid int) error { return unsupported() }

// Kill is unavailable off the supported platforms.
func Kill(pid int) error { return unsupported() }

func terminateOwn(pid int) error { return unsupported() }

func killOwn(pid int) error { return unsupported() }

func configureChild(cmd *exec.Cmd) {}

func exitStatus(err error) (int, error) { return exitCodeUnknown, unsupported() }
