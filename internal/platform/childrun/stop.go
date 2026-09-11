package childrun

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"
)

// exitCodeUnknown is reported when a child could not be waited for at all.
const exitCodeUnknown = -1

// Graceful-stop behaviour: how long a process may take to react to a graceful
// termination, and how often its disappearance is checked.
const (
	defaultStopGrace = 5 * time.Second
	stopPoll         = 50 * time.Millisecond
	goneGrace        = 2 * time.Second
)

var (
	// ErrNotPermitted means this process may not signal the target. Callers with
	// an escalation path (the privileged helper) use it instead of failing.
	ErrNotPermitted = errors.New("childrun: not permitted to signal the process")
	// ErrProcessGone means the target process is no longer running.
	ErrProcessGone = errors.New("childrun: process is not running")
	// ErrIdentityMismatch means the pid file does not describe the running
	// process: the pid was reused, so it must never be signalled.
	ErrIdentityMismatch = errors.New("childrun: pid file does not describe the running process")
)

// identityTolerance absorbs the rounding differences between the start time
// recorded when the process was launched and the one read back from the kernel.
const identityTolerance = 2 * time.Millisecond

// StopResult describes what a verified stop did.
type StopResult struct {
	// PID is the process the pid file described.
	PID int
	// WasRunning reports whether the process was still alive when it was checked.
	WasRunning bool
	// Stopped reports whether the process is gone when StopVerified returned.
	Stopped bool
	// Forced reports whether the process had to be killed.
	Forced bool
}

// StopVerified stops the process recorded in pidPath.
//
// The recorded start time is compared with the kernel before anything is
// signalled, so a recycled pid is never touched (internal-contracts.md §3). A
// missing pid file, or a pid file describing a process that has already exited,
// is a successful no-op. ErrNotPermitted is returned untouched when this process
// is not allowed to signal the target, so the caller can escalate.
func StopVerified(ctx context.Context, pidPath string, force bool, grace time.Duration) (StopResult, error) {
	record, err := ReadPIDFile(pidPath)
	if errors.Is(err, fs.ErrNotExist) {
		return StopResult{}, nil
	}
	if err != nil {
		return StopResult{}, err
	}
	if grace <= 0 {
		grace = defaultStopGrace
	}
	start, err := StartTimeNano(record.PID)
	if err != nil || !sameIdentity(start, record.StartTime) {
		if !Alive(record.PID) {
			// It exited on its own; there is nothing to stop.
			return StopResult{PID: record.PID}, nil
		}
		return StopResult{PID: record.PID, WasRunning: true}, fmt.Errorf(
			"childrun: pid file %s: %w", pidPath, ErrIdentityMismatch)
	}

	if force {
		if err := Kill(record.PID); err != nil {
			if errors.Is(err, ErrProcessGone) {
				return StopResult{PID: record.PID}, nil
			}
			return StopResult{PID: record.PID, WasRunning: true}, err
		}
		waitGone(ctx, record.PID, goneGrace)
		return StopResult{PID: record.PID, WasRunning: true, Stopped: true, Forced: true}, nil
	}

	if err := Terminate(record.PID); err != nil {
		if errors.Is(err, ErrProcessGone) {
			return StopResult{PID: record.PID}, nil
		}
		return StopResult{PID: record.PID, WasRunning: true}, err
	}
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !Alive(record.PID) {
			return StopResult{PID: record.PID, WasRunning: true, Stopped: true}, nil
		}
		select {
		case <-ctx.Done():
			// The process was asked politely and did not listen; a stop that has
			// been cancelled still must not leave it running.
			_ = Kill(record.PID)
			return StopResult{PID: record.PID, WasRunning: true, Forced: true}, fmt.Errorf(
				"childrun: terminate %d: %w", record.PID, ctx.Err())
		case <-time.After(stopPoll):
		}
	}
	if err := Kill(record.PID); err != nil && !errors.Is(err, ErrProcessGone) {
		return StopResult{PID: record.PID, WasRunning: true}, err
	}
	waitGone(ctx, record.PID, goneGrace)
	return StopResult{PID: record.PID, WasRunning: true, Stopped: true, Forced: true}, nil
}

// sameIdentity compares a recorded process start time with a freshly read one.
func sameIdentity(current, recorded int64) bool {
	if current <= 0 || recorded <= 0 {
		return false
	}
	delta := current - recorded
	if delta < 0 {
		delta = -delta
	}
	return delta <= int64(identityTolerance)
}

// waitGone waits, bounded, for a killed process to disappear.
func waitGone(ctx context.Context, pid int, limit time.Duration) {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if !Alive(pid) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(stopPoll):
		}
	}
}
