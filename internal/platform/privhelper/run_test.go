package privhelper

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/larffxx/singboxui/internal/platform/childrun"
)

// TestRunIsTheOnlyEntryPointAndItRefusesEverythingElse pins the shape of the
// command: an operation must be named, and an unknown one is refused before
// anything is attempted (internal-contracts.md §3).
func TestRunIsTheOnlyEntryPointAndItRefusesEverythingElse(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"no arguments", nil, ExitUsage},
		{"empty arguments", []string{}, ExitUsage},
		{"unknown operation", []string{"install"}, ExitUsage},
		{"operation only", []string{OpStart}, ExitUsage},
		{"stop without arguments", []string{OpStop}, ExitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := Run(context.Background(), tc.args, &stdout, &stderr); got != tc.want {
				t.Errorf("Run(%q) = %d, want %d", tc.args, got, tc.want)
			}
			if stdout.Len()+stderr.Len() == 0 {
				t.Error("Run() reported nothing to the caller")
			}
		})
	}
}

// TestRunHelpPrintsTheUsage covers the only read-only invocation.
func TestRunHelpPrintsTheUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if got := Run(context.Background(), []string{"help"}, &stdout, &stderr); got != ExitUsage {
		t.Errorf("Run(help) = %d, want %d", got, ExitUsage)
	}
	if !strings.Contains(stdout.String(), "usage:") {
		t.Errorf("Run(help) wrote %q to stdout, want the usage text", stdout.String())
	}
}

// TestRunStartRefusesARequestOutsideTheDataDirectory is the security boundary: a
// refusal is not a failure, so the caller can tell "I will not do this" from
// "I tried and could not".
func TestRunStartRefusesARequestOutsideTheDataDirectory(t *testing.T) {
	l := newLayout(t)
	spec := l.specOf("")
	spec.BinaryPath = filepath.Join(t.TempDir(), "sing-box")
	argv := spec.StartArgv(helperPath)

	var stdout, stderr bytes.Buffer
	if got := Run(context.Background(), argv[1:], &stdout, &stderr); got != ExitUsage {
		t.Errorf("Run(%q) = %d, want %d", argv[1:], got, ExitUsage)
	}
	if !strings.Contains(stderr.String(), "outside the application data directory") {
		t.Errorf("stderr = %q, want the refusal reason", stderr.String())
	}
	if _, err := os.Stat(l.pid); err == nil {
		t.Error("a refused request wrote a pid file")
	}
}

// TestRunStartSupervisesTheChild is the end-to-end case: the helper starts the
// stand-in sing-box, records it, and stops it again through its second operation.
func TestRunStartSupervisesTheChild(t *testing.T) {
	l := newLayout(t)
	asTestChild(t, modeServe)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)

	// A record from an earlier run must not be readable as the current one.
	if err := childrun.WriteStatus(l.status, childrun.Status{PID: 999999, ExitCode: 42}); err != nil {
		t.Fatalf("seed the status record: %v", err)
	}

	var stdout, stderr bytes.Buffer
	started := make(chan int, 1)
	go func() { started <- Run(ctx, l.startArgv("")[1:], &stdout, &stderr) }()

	// The pid file is the readiness signal of the contract (§3).
	var record childrun.PIDFile
	waitFor(t, 60*time.Second, "the pid file of the supervised process", func() bool {
		got, err := childrun.ReadPIDFile(l.pid)
		if err != nil {
			return false
		}
		record = got
		return record.Valid()
	})
	if !childrun.Alive(record.PID) {
		t.Fatalf("the supervised process %d is not running", record.PID)
	}
	if _, err := childrun.ReadStatus(l.status); err == nil {
		t.Error("the status record of the previous run survived the start")
	}
	waitUntilServing(t, l.log)

	var stopOut bytes.Buffer
	if got := Run(ctx, l.stopArgv(false)[1:], &stopOut, io.Discard); got != ExitOK {
		t.Fatalf("stop returned %d, want %d (output: %s)", got, ExitOK, stopOut.String())
	}
	select {
	case got := <-started:
		if got != ExitOK {
			t.Fatalf("start returned %d, want %d (stderr: %s)", got, ExitOK, stderr.String())
		}
	case <-time.After(60 * time.Second):
		t.Fatal("the start operation did not return after the process was stopped")
	}
	if !strings.Contains(stdout.String(), "started sing-box") {
		t.Errorf("stdout = %q, want the start announcement", stdout.String())
	}
	status, err := childrun.ReadStatus(l.status)
	if err != nil {
		t.Fatalf("read the status record: %v", err)
	}
	if status.PID != record.PID {
		t.Errorf("the status record belongs to pid %d, want %d", status.PID, record.PID)
	}
	if status.EndedAt.IsZero() {
		t.Error("the status record has no end time")
	}
}

// TestRunStartReportsTheChildExitCodeAndStillSucceeds keeps a sing-box that exits
// non-zero from being read as a helper failure: supervising the process was the
// job, and its outcome is reported in the status record.
func TestRunStartReportsTheChildExitCodeAndStillSucceeds(t *testing.T) {
	l := newLayout(t)
	asTestChild(t, modeFail)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	var stdout, stderr bytes.Buffer
	if got := Run(ctx, l.startArgv("")[1:], &stdout, &stderr); got != ExitOK {
		t.Fatalf("Run() = %d, want %d (stderr: %s)", got, ExitOK, stderr.String())
	}
	status, err := childrun.ReadStatus(l.status)
	if err != nil {
		t.Fatalf("read the status record: %v", err)
	}
	if status.ExitCode != 3 {
		t.Errorf("the status record reports exit code %d, want 3", status.ExitCode)
	}
	if !strings.Contains(stdout.String(), "exited with code 3") {
		t.Errorf("stdout = %q, want the child exit code", stdout.String())
	}
}

// TestRunStopWithoutARecordIsANoOp keeps the helper from failing when there is
// nothing to stop.
func TestRunStopWithoutARecordIsANoOp(t *testing.T) {
	l := newLayout(t)
	var stdout, stderr bytes.Buffer
	if got := Run(context.Background(), l.stopArgv(false)[1:], &stdout, &stderr); got != ExitOK {
		t.Errorf("Run(stop) = %d, want %d (stderr: %s)", got, ExitOK, stderr.String())
	}
	if !strings.Contains(stdout.String(), "not running") {
		t.Errorf("stdout = %q, want the no-op announcement", stdout.String())
	}
}

// TestRunStopRefusesAReusedPID is the reason the pid file carries a start time: a
// pid that now belongs to another process must never be signalled. The stand-in
// target is this test process itself.
func TestRunStopRefusesAReusedPID(t *testing.T) {
	l := newLayout(t)
	if err := childrun.WritePIDFile(l.pid, childrun.PIDFile{PID: os.Getpid(), StartTime: 1}); err != nil {
		t.Fatalf("write the pid file: %v", err)
	}
	var stdout, stderr bytes.Buffer
	if got := Run(context.Background(), l.stopArgv(true)[1:], &stdout, &stderr); got == ExitOK {
		t.Fatalf("Run(stop) = %d, want a refusal (stdout: %s)", got, stdout.String())
	}
	if !strings.Contains(stderr.String(), "does not describe") {
		t.Errorf("stderr = %q, want the identity mismatch", stderr.String())
	}
}

// TestRunStartFailsWhenTheChildCannotBeStarted keeps a run-time failure distinct
// from a refusal: the request was valid, so the code is ExitFailure.
func TestRunStartFailsWhenTheChildCannotBeStarted(t *testing.T) {
	l := newLayout(t)
	// A valid sing-box that cannot run: the binary is a directory-free file with
	// the executable bit but no valid program image.
	if err := os.WriteFile(l.binary, []byte("not a program"), 0o755); err != nil {
		t.Fatalf("replace the stand-in binary: %v", err)
	}
	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	if got := Run(ctx, l.startArgv("")[1:], &stdout, &stderr); got != ExitFailure {
		t.Errorf("Run(start) = %d, want %d (stderr: %s)", got, ExitFailure, stderr.String())
	}
}
