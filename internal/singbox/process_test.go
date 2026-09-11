package singbox

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/singbox/faketest"
)

func containsInt(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestForeignReportsTheFixtureProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process enumeration on Windows uses tasklist, which the fixture cannot exercise")
	}
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	process := startFake(t, "-scenario", faketest.ScenarioRunForeign, "-pid-file", pidFile)
	waitFor(t, 5*time.Second, "the fixture's PID file", func() bool { return fileExists(pidFile) })

	pids, err := Foreign(context.Background())
	if err != nil {
		t.Fatalf("Foreign() failed: %v", err)
	}
	// The adapter reports every sing-box process the machine has, so the test
	// asserts containment: a developer running sing-box by hand still passes.
	if !containsInt(pids, process.cmd.Process.Pid) {
		t.Errorf("Foreign() = %v, want it to include the fixture process %d", pids, process.cmd.Process.Pid)
	}
	if !sort.IntsAreSorted(pids) {
		t.Errorf("Foreign() = %v, want a sorted result", pids)
	}
	seen := make(map[int]int, len(pids))
	for _, pid := range pids {
		if pid <= 0 {
			t.Errorf("Foreign() = %v, want no non-positive PID", pids)
		}
		if pid == os.Getpid() {
			t.Errorf("Foreign() = %v, want the calling process to be excluded", pids)
		}
		seen[pid]++
		if seen[pid] > 1 {
			t.Errorf("Foreign() = %v, want each PID once", pids)
		}
	}
}

func TestForeignStopsWithTheContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process enumeration on Windows uses tasklist, which the fixture cannot exercise")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := Foreign(ctx); err == nil {
		t.Fatal("Foreign() with a cancelled context succeeded, want an error")
	} else if code := apperr.CodeOf(err); code != apperr.CodeInternal {
		t.Errorf("error code = %s, want %s (%v)", code, apperr.CodeInternal, err)
	}
}

func TestForeignIgnoresProcessesWithAnotherName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the pgrep path only exists on darwin and linux")
	}
	// pgrep exits 1 when nothing matches; that is an empty result, not a failure.
	pids, err := foreignPIDsPgrep(context.Background(), "singbox-definitely-not-running-4242")
	if err != nil {
		t.Fatalf("foreignPIDsPgrep() failed: %v", err)
	}
	if len(pids) != 0 {
		t.Errorf("foreignPIDsPgrep() = %v, want no processes", pids)
	}
}

func TestParsePIDLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []int
	}{
		{name: "one PID per line", input: "1234\n5678\n", want: []int{1234, 5678}},
		{name: "whitespace is tolerated", input: "  42  \n\t7\n", want: []int{42, 7}},
		{name: "junk lines are skipped", input: "pgrep: something\n7\nnot a pid\n", want: []int{7}},
		{name: "empty output", input: "", want: nil},
		{name: "blank lines", input: "\n\n", want: nil},
		{name: "pid zero is refused", input: "0\n", want: nil},
		{name: "negative PIDs are refused", input: "-5\n", want: nil},
		{name: "no trailing newline", input: "9", want: []int{9}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := parsePIDLines(test.input)
			if len(got) != len(test.want) {
				t.Fatalf("parsePIDLines(%q) = %v, want %v", test.input, got, test.want)
			}
			for i := range got {
				if got[i] != test.want[i] {
					t.Fatalf("parsePIDLines(%q) = %v, want %v", test.input, got, test.want)
				}
			}
		})
	}
}
