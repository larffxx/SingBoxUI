//go:build windows

package singbox

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/larffxx/singboxui/internal/singbox/faketest"
)

// TestProcessCommandLineReadsTheFixture pins the call that lets the Windows
// enumeration filter candidates the way the POSIX one does. It has to read the
// command line of an arbitrary process, including the core this application
// starts as administrator, so the call is asked for real here.
func TestProcessCommandLineReadsTheFixture(t *testing.T) {
	dir := t.TempDir()
	process := startFake(t, "-scenario", faketest.ScenarioRunForeign, "-pid-file", filepath.Join(dir, "pid"), "check")

	line, err := processCommandLine(process.cmd.Process.Pid)
	if err != nil {
		t.Fatalf("processCommandLine(%d) failed: %v", process.cmd.Process.Pid, err)
	}
	if !strings.Contains(line, "check") || !strings.Contains(line, faketest.ScenarioRunForeign) {
		t.Errorf("processCommandLine() = %q, want the fixture's own command line", line)
	}
	// The whole point of reading the line: a `check` is not a running core.
	if runningConfiguration(process.cmd.Process.Pid) {
		t.Errorf("runningConfiguration(%d) = true for a check, want false", process.cmd.Process.Pid)
	}
}

func TestProcessCommandLineOfAProcessThatIsGone(t *testing.T) {
	// A pid that cannot exist: OpenProcess refuses it before anything is read.
	if _, err := processCommandLine(0x7ffffff0); err == nil {
		t.Fatal("processCommandLine() succeeded for a pid that cannot exist, want an error")
	}
	if _, err := processCommandLine(0); err == nil {
		t.Fatal("processCommandLine(0) succeeded, want an error")
	}
}

func TestRunningConfigurationCountsAProcessItCannotRead(t *testing.T) {
	// A process the application may not inspect is reported, not assumed
	// harmless: the harmless direction of the two (see process.go).
	if !runningConfiguration(0x7ffffff0) {
		t.Error("runningConfiguration() = false for a process that cannot be read, want true")
	}
}

func TestParseTasklistCSV(t *testing.T) {
	const answer = "\"sing-box.exe\",\"4242\",\"Console\",\"1\",\"12,345 K\"\r\n" +
		"\"sing-box.exe\",\"7\",\"Services\",\"0\",\"1,024 K\"\r\n"
	pids, err := parseTasklistCSV(answer)
	if err != nil {
		t.Fatalf("parseTasklistCSV() failed: %v", err)
	}
	if len(pids) != 2 || pids[0] != 4242 || pids[1] != 7 {
		t.Errorf("parseTasklistCSV() = %v, want [4242 7]", pids)
	}

	// The empty answer of tasklist is not a failure.
	pids, err = parseTasklistCSV("INFO: No tasks are running which match the specified criteria.\r\n")
	if err != nil || len(pids) != 0 {
		t.Errorf("parseTasklistCSV(no tasks) = %v, %v, want no processes and no error", pids, err)
	}
	if pids, err = parseTasklistCSV(""); err != nil || len(pids) != 0 {
		t.Errorf("parseTasklistCSV(empty) = %v, %v, want no processes and no error", pids, err)
	}
}
