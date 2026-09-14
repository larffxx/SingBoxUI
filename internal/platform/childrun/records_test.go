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

// startSleep starts a process that stays alive for the test.
func startSleep(t *testing.T) (int, time.Time) {
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
	start, err := StartTimeNano(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("StartTimeNano: %v", err)
	}
	return cmd.Process.Pid, time.Unix(0, start)
}

func TestPIDFileMatchesTheProcessItDescribes(t *testing.T) {
	pid, startedAt := startSleep(t)
	start, err := StartTimeNano(pid)
	if err != nil {
		t.Fatalf("StartTimeNano: %v", err)
	}
	record := PIDFile{PID: pid, StartTime: start, RecordedAt: startedAt}

	if !record.Matches() {
		t.Error("Matches() = false for the process the record describes")
	}
}

func TestPIDFileDoesNotMatchARecycledPID(t *testing.T) {
	pid, _ := startSleep(t)
	// A record written for another process that once had this pid.
	record := PIDFile{PID: pid, StartTime: time.Now().Add(-24 * time.Hour).UnixNano()}

	if record.Matches() {
		t.Error("Matches() = true for a pid that was recycled")
	}
}

func TestPIDFileDoesNotMatchAProcessThatIsGone(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("cannot start the fixture process: %v", err)
	}
	pid := cmd.Process.Pid
	start, err := StartTimeNano(pid)
	if err != nil {
		t.Fatalf("StartTimeNano: %v", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("cannot kill the fixture process: %v", err)
	}
	_, _ = cmd.Process.Wait()

	if (PIDFile{PID: pid, StartTime: start}).Matches() {
		t.Error("Matches() = true for a process that exited")
	}
	if (PIDFile{}).Matches() {
		t.Error("Matches() = true for an empty record")
	}
}

func TestPIDFileDoesNotMatchAProcessThatHasNotBeenReaped(t *testing.T) {
	// A process that exited but whose parent has not waited for it is not
	// running: right after a stop, the record still describes the pid the kernel
	// has not yet released, and calling that a running core would refuse the next
	// start.
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("cannot start the fixture process: %v", err)
	}
	pid := cmd.Process.Pid
	start, err := StartTimeNano(pid)
	if err != nil {
		t.Fatalf("StartTimeNano: %v", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("cannot kill the fixture process: %v", err)
	}
	// The signal has to be delivered before the process stops being one: wait
	// for it to reach the state a parent has not reaped yet.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !Alive(pid) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if Alive(pid) {
		t.Skip("the fixture did not reach the unreaped state in time")
	}
	if (PIDFile{PID: pid, StartTime: start}).Matches() {
		t.Error("Matches() = true for a process that exited and was not reaped")
	}
	_, _ = cmd.Process.Wait()
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
