/**
 * A sing-box the application does not own (ADR 012).
 *
 * The property under test is the one the user hit: a core that outlived the
 * window must be found, must stop the next start with an explanation, and must be
 * stoppable from the runtime screen through the same narrow helper that stops a
 * supervised process.
 */
package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/platform/childrun"
)

// recordForeign writes a run record for a live process this supervisor does not
// own: the test's own process, which is alive for the whole test.
func recordForeign(t *testing.T, h *harness, revisionID string) string {
	t.Helper()
	runDir := filepath.Join(h.paths.RuntimeDir, revisionID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	start, err := childrun.StartTimeNano(os.Getpid())
	if err != nil {
		t.Fatalf("StartTimeNano: %v", err)
	}
	pidPath := filepath.Join(runDir, runPIDName)
	if err := childrun.WritePIDFile(pidPath, childrun.PIDFile{
		PID:        os.Getpid(),
		StartTime:  start,
		BinaryPath: h.paths.ManagedBinaryPath("1.14.0", "sing-box"),
		RecordedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("WritePIDFile: %v", err)
	}
	return pidPath
}

func TestForeignProcessesReportsARecordedProcessItDoesNotOwn(t *testing.T) {
	h := newHarness(t, defaultOptions())
	pidPath := recordForeign(t, h, "rev-foreign")

	foreign := h.sup.ForeignProcesses()
	if len(foreign) != 1 {
		t.Fatalf("ForeignProcesses() = %+v, want one process", foreign)
	}
	got := foreign[0]
	if got.PID != os.Getpid() || got.RevisionID != "rev-foreign" || got.PIDPath != pidPath {
		t.Errorf("ForeignProcesses()[0] = %+v, want the recorded process of rev-foreign", got)
	}
	if got.StartedAt.IsZero() {
		t.Error("StartedAt is zero, so the runtime screen cannot say when it started")
	}
}

func TestForeignProcessesIgnoresWhatItDoesNotOwnOrThatIsGone(t *testing.T) {
	h := newHarness(t, defaultOptions())
	runDir := filepath.Join(h.paths.RuntimeDir, "rev-dead")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// A record of a process that no longer exists, and a record that does not
	// identify anything: neither is a running core.
	if err := childrun.WritePIDFile(filepath.Join(runDir, runPIDName), childrun.PIDFile{
		PID:        999999,
		StartTime:  time.Now().UnixNano(),
		BinaryPath: "sing-box",
	}); err != nil {
		t.Fatalf("WritePIDFile: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(h.paths.RuntimeDir, "rev-empty"), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(h.paths.RuntimeDir, "rev-empty", runPIDName), []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if foreign := h.sup.ForeignProcesses(); len(foreign) != 0 {
		t.Fatalf("ForeignProcesses() = %+v, want nothing", foreign)
	}

	// The process this supervisor started is its own, not foreign.
	if err := h.sup.StartProfile(context.Background(), "p-1"); err != nil {
		t.Fatalf("StartProfile: %v", err)
	}
	if foreign := h.sup.ForeignProcesses(); len(foreign) != 0 {
		t.Fatalf("ForeignProcesses() = %+v, want the running profile to be its own", foreign)
	}
}

func TestForeignProcessesIgnoresARecycledPID(t *testing.T) {
	// A pid outlives the process it was given to: the kernel hands it to
	// somebody else long before the run directory is tidied up. A record whose
	// start time does not match the process running under that pid is not
	// evidence of a core that outlived the application (ADR 012).
	h := newHarness(t, defaultOptions())
	runDir := filepath.Join(h.paths.RuntimeDir, "rev-recycled")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := childrun.WritePIDFile(filepath.Join(runDir, runPIDName), childrun.PIDFile{
		PID:        os.Getpid(),
		StartTime:  time.Now().Add(-24 * time.Hour).UnixNano(),
		BinaryPath: "sing-box",
	}); err != nil {
		t.Fatalf("WritePIDFile: %v", err)
	}

	if foreign := h.sup.ForeignProcesses(); len(foreign) != 0 {
		t.Fatalf("ForeignProcesses() = %+v, want a recycled pid to be left out", foreign)
	}
	// And it must not refuse a start either.
	if err := h.sup.StartProfile(context.Background(), "p-1"); err != nil {
		t.Fatalf("StartProfile: %v, want a recycled pid not to block it", err)
	}
}

func TestStartProfileRefusesWhileAForeignProcessRuns(t *testing.T) {
	h := newHarness(t, defaultOptions())
	recordForeign(t, h, "rev-foreign")

	err := h.sup.StartProfile(context.Background(), "p-1")
	if err == nil {
		t.Fatal("StartProfile succeeded although a sing-box the application does not own is running")
	}
	if code := apperr.CodeOf(err); code != apperr.CodeRuntimeAlreadyRunning {
		t.Fatalf("error code = %s, want %s (%v)", code, apperr.CodeRuntimeAlreadyRunning, err)
	}
	if !strings.Contains(err.Error(), "outside the application") {
		t.Errorf("error = %q, want it to say the process runs outside the application", err.Error())
	}
	// Nothing may be launched: the refusal happens before the elevation prompt.
	if len(h.launcher.requests) != 0 {
		t.Errorf("the privilege runner was asked %d times, want none", len(h.launcher.requests))
	}
}

func TestStartProfileRefusesAProcessThatWasStartedElsewhere(t *testing.T) {
	h := newHarness(t, defaultOptions())
	// A process without a run record: started by hand, so the application can
	// name it but cannot stop it.
	h.sup.deps.Foreign = func(context.Context) ([]int, error) { return []int{os.Getpid() + 1}, nil }

	err := h.sup.StartProfile(context.Background(), "p-1")
	if code := apperr.CodeOf(err); code != apperr.CodeRuntimeAlreadyRunning {
		t.Fatalf("error code = %s, want %s (%v)", code, apperr.CodeRuntimeAlreadyRunning, err)
	}
	if !strings.Contains(err.Error(), "did not start") {
		t.Errorf("error = %q, want it to say the process was not started by the application", err.Error())
	}
}

func TestRefuseForeignAllowsTheProcessThisSupervisorOwns(t *testing.T) {
	h := newHarness(t, defaultOptions())
	if err := h.sup.StartProfile(context.Background(), "p-1"); err != nil {
		t.Fatalf("StartProfile: %v", err)
	}
	running := h.sup.Status().PID
	if running <= 0 {
		t.Fatalf("the supervisor reports pid %d", running)
	}
	// The core the supervisor started is running a configuration and is found by
	// the search: it must not refuse the next restart.
	h.sup.deps.Foreign = func(context.Context) ([]int, error) { return []int{running}, nil }

	if err := h.sup.refuseForeignLocked(context.Background(), "runtime.StartProfile"); err != nil {
		t.Fatalf("refuseForeignLocked() = %v, want the owned process to be ignored", err)
	}
}

func TestStartProfileSurvivesAMachineThatCannotBeSearched(t *testing.T) {
	h := newHarness(t, defaultOptions())
	h.sup.deps.Foreign = func(context.Context) ([]int, error) { return nil, errors.New("pgrep is missing") }

	if err := h.sup.StartProfile(context.Background(), "p-1"); err != nil {
		t.Fatalf("StartProfile: %v, want the search to be a diagnostic only", err)
	}
}

func TestStopForeignStopsWhatItFound(t *testing.T) {
	h := newHarness(t, defaultOptions())
	pidPath := recordForeign(t, h, "rev-foreign")

	stopped, err := h.sup.StopForeign(context.Background())
	if err != nil {
		t.Fatalf("StopForeign: %v", err)
	}
	if stopped != 1 {
		t.Fatalf("StopForeign stopped %d processes, want 1", stopped)
	}
	paths := h.launcher.stoppedPaths()
	if len(paths) != 1 || paths[0] != pidPath {
		t.Fatalf("the privilege runner was asked to stop %v, want %s", paths, pidPath)
	}
}

func TestStopForeignReportsAFailureWithoutLosingTheCount(t *testing.T) {
	h := newHarness(t, defaultOptions())
	recordForeign(t, h, "rev-foreign")
	h.launcher.stopErr = errors.New("the user dismissed the prompt")
	// A second record: a failure of one must not leave the other running.
	runDir := filepath.Join(h.paths.RuntimeDir, "rev-other")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	start, err := childrun.StartTimeNano(os.Getppid())
	if err != nil {
		t.Skipf("the parent process cannot be identified: %v", err)
	}
	if err := childrun.WritePIDFile(filepath.Join(runDir, runPIDName), childrun.PIDFile{
		PID: os.Getppid(), StartTime: start, BinaryPath: "sing-box",
	}); err != nil {
		t.Fatalf("WritePIDFile: %v", err)
	}

	stopped, err := h.sup.StopForeign(context.Background())
	if err == nil {
		t.Fatal("StopForeign reported success although the stop failed")
	}
	if code := apperr.CodeOf(err); code != apperr.CodeRuntimeStopFailed {
		t.Errorf("error code = %s, want %s (%v)", code, apperr.CodeRuntimeStopFailed, err)
	}
	if stopped != 0 {
		t.Errorf("StopForeign stopped %d processes, want 0", stopped)
	}
	if len(h.launcher.stoppedPaths()) != 2 {
		t.Errorf("the privilege runner was asked to stop %v, want both records", h.launcher.stoppedPaths())
	}
}

func TestStopForeignWithoutAnythingToStopDoesNothing(t *testing.T) {
	h := newHarness(t, defaultOptions())

	stopped, err := h.sup.StopForeign(context.Background())
	if err != nil {
		t.Fatalf("StopForeign: %v", err)
	}
	if stopped != 0 {
		t.Errorf("StopForeign stopped %d processes, want 0", stopped)
	}
	if len(h.launcher.stoppedPaths()) != 0 {
		t.Errorf("the privilege runner was asked to stop %v, want nothing", h.launcher.stoppedPaths())
	}
}
