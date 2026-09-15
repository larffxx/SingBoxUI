package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/larffxx/singboxui/internal/platform/childrun"
)

// A core that outlived the window is state the runtime screen has to show and
// the runtime API has to be able to end (ADR 012). The fixture is a real
// process this test owns, recorded exactly as a launch records one, so the stop
// can be observed rather than assumed.

// startFixture starts a process that stays alive for the duration of the test.
func startFixture(t *testing.T) *exec.Cmd {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "ping -n 30 127.0.0.1 >NUL")
	} else {
		cmd = exec.Command("/bin/sleep", "30")
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("cannot start the fixture process: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	return cmd
}

// recordFixture writes the run record of a process, the way a launch does.
func recordFixture(t *testing.T, h *harness, revisionID string, pid int) string {
	t.Helper()
	runDir := filepath.Join(h.paths.RuntimeDir, revisionID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	start, err := childrun.StartTimeNano(pid)
	if err != nil {
		t.Fatalf("StartTimeNano: %v", err)
	}
	pidPath := filepath.Join(runDir, "sing-box.pid")
	if err := childrun.WritePIDFile(pidPath, childrun.PIDFile{
		PID:        pid,
		StartTime:  start,
		BinaryPath: h.paths.ManagedBinaryPath("1.14.0", "sing-box"),
		RecordedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("WritePIDFile: %v", err)
	}
	return pidPath
}

func TestRuntimeStatusReportsAForeignProcess(t *testing.T) {
	h := newHarness(t)
	cmd := startFixture(t)
	pidPath := recordFixture(t, h, "rev-foreign", cmd.Process.Pid)

	payload := h.app.RuntimeAPI.GetRuntimeStatus()
	if payload.Error != nil {
		t.Fatalf("GetRuntimeStatus reported %v", payload.Error)
	}
	if len(payload.ForeignProcesses) != 1 {
		t.Fatalf("foreignProcesses = %+v, want the recorded process", payload.ForeignProcesses)
	}
	got := payload.ForeignProcesses[0]
	if got.PID != cmd.Process.Pid || got.RevisionID != "rev-foreign" || got.PIDPath != pidPath {
		t.Errorf("foreignProcesses[0] = %+v, want the fixture of rev-foreign", got)
	}

	// Stopping it goes through the narrow privileged boundary and is reported in
	// the payload the UI renders next.
	stopped := h.app.RuntimeAPI.StopForeignProcesses()
	if stopped.Error != nil {
		t.Fatalf("StopForeignProcesses reported %v", stopped.Error)
	}
	paths := h.plat.priv.stoppedPaths()
	if len(paths) != 1 || paths[0] != pidPath {
		t.Fatalf("the privilege runner was asked to stop %v, want %s", paths, pidPath)
	}
	if len(stopped.ForeignProcesses) != 0 {
		t.Errorf("foreignProcesses = %+v after the stop, want none", stopped.ForeignProcesses)
	}
	if childrun.Alive(cmd.Process.Pid) {
		t.Errorf("process %d is still running after the stop", cmd.Process.Pid)
	}
}

func TestRuntimeStatusReportsAProcessWithoutARecord(t *testing.T) {
	h := newHarness(t)
	// A core started by hand has no run record, so it is named in the error of a
	// refused start but cannot be stopped: the payload must not pretend there is
	// something to stop.
	payload := h.app.RuntimeAPI.GetRuntimeStatus()
	if len(payload.ForeignProcesses) != 0 {
		t.Fatalf("foreignProcesses = %+v, want none without a record", payload.ForeignProcesses)
	}
	if stopped := h.app.RuntimeAPI.StopForeignProcesses(); stopped.Error != nil {
		t.Fatalf("StopForeignProcesses with nothing to stop reported %v", stopped.Error)
	}
}
