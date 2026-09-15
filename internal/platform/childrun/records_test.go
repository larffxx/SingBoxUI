package childrun

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// A record is only evidence of a running process when the start time matches:
// the kernel recycles a pid long before a run directory is cleaned up, and
// ADR 012 makes the difference between "the pid is alive" and "our process is
// running" the difference between a correct refusal and a wrong one.

// startFixtureProcess starts a process that stays alive for the duration of the
// test, on whichever operating system the suite runs.
func startFixtureProcess(t *testing.T) *exec.Cmd {
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

// fixtureRecord describes a live fixture process the way a launch records one.
func fixtureRecord(t *testing.T, cmd *exec.Cmd) PIDFile {
	t.Helper()
	start, err := StartTimeNano(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("StartTimeNano: %v", err)
	}
	return PIDFile{PID: cmd.Process.Pid, StartTime: start, RecordedAt: time.Now().UTC()}
}

func TestPIDFileMatchesTheProcessItDescribes(t *testing.T) {
	cmd := startFixtureProcess(t)

	if !fixtureRecord(t, cmd).Matches() {
		t.Error("Matches() = false for the process the record describes")
	}
}

func TestPIDFileDoesNotMatchARecycledPID(t *testing.T) {
	cmd := startFixtureProcess(t)
	record := fixtureRecord(t, cmd)
	// The same pid, one day earlier: the process the pid belonged to is gone.
	record.StartTime = time.Now().Add(-24 * time.Hour).UnixNano()

	if record.Matches() {
		t.Error("Matches() = true for a pid that was recycled")
	}
}

func TestPIDFileDoesNotMatchAProcessThatIsGone(t *testing.T) {
	cmd := startFixtureProcess(t)
	record := fixtureRecord(t, cmd)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("cannot kill the fixture process: %v", err)
	}
	_, _ = cmd.Process.Wait()

	if record.Matches() {
		t.Error("Matches() = true for a process that exited")
	}
	if (PIDFile{}).Matches() {
		t.Error("Matches() = true for an empty record")
	}
}

func TestPIDFileDoesNotMatchAProcessThatHasNotBeenReaped(t *testing.T) {
	// A process that exited but whose parent has not waited for it is not
	// running: right after a stop, the record still describes a pid the kernel
	// has not released, and calling that a running core would refuse the next
	// start.
	cmd := startFixtureProcess(t)
	record := fixtureRecord(t, cmd)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("cannot kill the fixture process: %v", err)
	}
	// The signal has to be delivered before the process stops being one: wait
	// for it to reach the unreaped state.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && Alive(record.PID) {
		time.Sleep(10 * time.Millisecond)
	}
	if Alive(record.PID) {
		t.Skip("the fixture did not reach the unreaped state in time")
	}
	if record.Matches() {
		t.Error("Matches() = true for a process that exited and was not reaped")
	}
}

func TestReadPIDFileRefusesAnIncompleteRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sing-box.pid")
	if err := os.WriteFile(path, []byte(`{"pid":4242}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := ReadPIDFile(path); err == nil {
		t.Error("ReadPIDFile accepted a record without a start time")
	}
}
