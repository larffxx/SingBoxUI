package privhelper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/platform/childrun"
)

// Operations of this package as they appear in typed errors.
const (
	opStart = "privhelper.start"
	opStop  = "privhelper.stop"
)

// stopGrace bounds the graceful wait of an elevated stop.
const stopGrace = 5 * time.Second

// Run is the helper's entry point: it performs exactly one operation and returns
// the process exit status (see ExitOK and friends).
//
// The context is the helper's lifetime: when it is cancelled the supervised
// child is terminated and the helper returns.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if IsHelp(args) {
		fmt.Fprint(stdout, Usage())
		return ExitUsage
	}
	if len(args) == 0 {
		fmt.Fprint(stderr, "singboxui-priv: no operation given\n\n")
		fmt.Fprint(stderr, Usage())
		return ExitUsage
	}
	switch args[0] {
	case OpStart:
		return runStart(ctx, args[1:], stdout, stderr)
	case OpStop:
		return runStop(ctx, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "singboxui-priv: refusing unknown operation %q\n\n", args[0])
		fmt.Fprint(stderr, Usage())
		return ExitUsage
	}
}

// runStart validates the request, clears the records of any previous run and
// supervises sing-box until it exits. The helper's lifetime is the child's
// lifetime, which is what makes the launcher's session handle meaningful.
func runStart(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	spec, err := ParseStartArgs(args)
	if err != nil {
		return refuse(stderr, err)
	}
	if err := spec.Validate(); err != nil {
		return refuse(stderr, err)
	}
	if err := prepareRun(spec); err != nil {
		return fail(stderr, err)
	}
	child, err := childrun.Start(ctx, childrun.Config{
		BinaryPath: spec.BinaryPath,
		ConfigPath: spec.ConfigPath,
		WorkDir:    spec.ChildWorkDir(),
		LogPath:    spec.LogPath,
		PIDPath:    spec.PIDPath,
		StatusPath: spec.StatusPath,
	})
	if err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintf(stdout, "singboxui-priv: started sing-box (pid %d), log %s\n", child.PID(), spec.LogPath)
	code, err := child.Wait()
	switch {
	case err == nil, childrun.IsExitError(err):
		// sing-box ran to completion and its exit code is recorded in --status.
		// A non-zero code is the child's outcome, not the helper's: supervising
		// it was the whole job, so the helper reports its own success and the
		// application reads the code from the record.
		fmt.Fprintf(stdout, "singboxui-priv: sing-box exited with code %d\n", code)
		return ExitOK
	default:
		return fail(stderr, err)
	}
}

// runStop stops the process recorded in the pid file. Stopping a process that
// has already exited is not an error.
func runStop(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	req, err := ParseStopArgs(args)
	if err != nil {
		return refuse(stderr, err)
	}
	if err := req.Validate(); err != nil {
		return refuse(stderr, err)
	}
	result, err := childrun.StopVerified(ctx, req.PIDPath, req.Force, stopGrace)
	if err != nil {
		return fail(stderr, err)
	}
	switch {
	case result.Stopped && result.Forced:
		fmt.Fprintf(stdout, "singboxui-priv: killed pid %d\n", result.PID)
	case result.Stopped:
		fmt.Fprintf(stdout, "singboxui-priv: stopped pid %d\n", result.PID)
	default:
		fmt.Fprintf(stdout, "singboxui-priv: pid %d is not running\n", result.PID)
	}
	return ExitOK
}

// prepareRun clears stale records and makes sure the directories the child
// writes into exist, with permissions that keep other users out.
func prepareRun(spec Spec) error {
	for _, dir := range []string{spec.DataDir, filepathDir(spec.LogPath), filepathDir(spec.PIDPath), filepathDir(spec.StatusPath)} {
		if err := os.MkdirAll(dir, dirMode); err != nil {
			return apperr.Wrap(apperr.CodeInternal, opStart, "the application data directory is not writable", err)
		}
	}
	if err := ClearRunFiles(spec.PIDPath, spec.StatusPath); err != nil {
		return err
	}
	return nil
}

// refuse reports an invocation the helper will not perform. Every reason a
// request is refused is an invalid argument, so the exit code is the same for
// all of them: the caller must not read anything into the difference.
func refuse(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "singboxui-priv: %s\n", err.Error())
	return ExitUsage
}

// fail reports a run-time failure.
func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "singboxui-priv: %s\n", err.Error())
	if errors.Is(err, childrun.ErrNotPermitted) {
		// Running as administrator and still refused: the records must be
		// pointing at another user's process.
		return ExitUsage
	}
	return ExitFailure
}
