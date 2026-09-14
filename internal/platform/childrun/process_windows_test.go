//go:build windows

package childrun

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// The Windows half of the stop contract (ADR 012): what this process is allowed
// to do is asked before anything is signalled, and the answer is the kernel's.
// The refusal an elevated process produces cannot be created by a test running
// as the current user, so the one case that needs it is injected through the
// same variable the implementation uses.

// denyTermination makes every termination attempt answer "access denied", the
// way the kernel answers for a process that runs as administrator.
func denyTermination(t *testing.T) {
	t.Helper()
	previous := openForTermination
	openForTermination = func(int) (windows.Handle, error) { return 0, windows.ERROR_ACCESS_DENIED }
	t.Cleanup(func() { openForTermination = previous })
}

func TestMayTerminateSeesAProcessThatIsGone(t *testing.T) {
	// A pid that no process owns answers ErrProcessGone, not a permission
	// problem: the caller must not escalate an elevation prompt for a process
	// that already exited.
	gone := startAndReap(t)
	err := mayTerminate(gone)
	if !errors.Is(err, ErrProcessGone) {
		t.Fatalf("mayTerminate(%d) = %v, want ErrProcessGone", gone, err)
	}
}

func TestMayTerminateAllowsAProcessThisUserOwns(t *testing.T) {
	cfg := fixture(t)
	asTestChild(t, modeServe)
	child, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = child.Kill() })
	waitUntilServing(t, cfg.LogPath)

	if err := mayTerminate(child.PID()); err != nil {
		t.Fatalf("mayTerminate(%d) = %v, want nil for a process of this user", child.PID(), err)
	}
	if err := Kill(child.PID()); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	waitFor(t, 20*time.Second, "the killed process to disappear", func() bool { return !Alive(child.PID()) })
}

func TestTerminatingAnUnpermittedProcessIsReportedAsNotPermitted(t *testing.T) {
	cfg := fixture(t)
	asTestChild(t, modeServe)
	child, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = child.Kill() })
	waitUntilServing(t, cfg.LogPath)

	// The forced stop is the path that reaches the check on every machine: a
	// graceful stop first tries the console event, which the windowless child of
	// this product cannot receive — under a test console it can, so only the
	// forced path is asserted here.
	denyTermination(t)
	if err := Kill(child.PID()); !errors.Is(err, ErrNotPermitted) {
		t.Errorf("Kill(%d) = %v, want ErrNotPermitted", child.PID(), err)
	}
	if !Alive(child.PID()) {
		t.Error("a process this user may not signal was ended anyway")
	}
}

func TestStopVerifiedHandsTheRefusalToItsCaller(t *testing.T) {
	// The stop of a recorded process must report ErrNotPermitted untouched:
	// privilege.Runner.Stop escalates to the helper on exactly that answer, and
	// on Windows it never saw it before (ADR 012).
	cfg := fixture(t)
	asTestChild(t, modeServe)
	child, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = child.Kill() })
	waitUntilServing(t, cfg.LogPath)

	denyTermination(t)
	if _, err := StopVerified(context.Background(), cfg.PIDPath, true, time.Second); !errors.Is(err, ErrNotPermitted) {
		t.Fatalf("StopVerified = %v, want ErrNotPermitted", err)
	}
	if !Alive(child.PID()) {
		t.Error("StopVerified ended a process it was not permitted to signal")
	}
}

// startAndReap returns the pid of a process that has exited and been waited for,
// which is what a pid file of a previous run describes.
func startAndReap(t *testing.T) int {
	t.Helper()
	asTestChild(t, modeFail)
	cfg := fixture(t)
	child, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid := child.PID()
	if _, err := child.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	waitFor(t, 20*time.Second, "the exited process to disappear", func() bool { return !Alive(pid) })
	return pid
}
