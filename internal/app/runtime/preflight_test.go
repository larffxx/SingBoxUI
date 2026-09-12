package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	domruntime "github.com/larffxx/singboxui/internal/domain/runtime"
	"github.com/larffxx/singboxui/internal/singbox/faketest"
)

// This suite covers the preflight gate described in spec §38: the managed
// binary judges the materialised file before anything is launched, and a
// configuration it refuses is reported with the decoder's own message instead
// of becoming a profile that quietly refuses to come up.

// wantCode asserts the error carries the expected code and returns the typed
// error, so a test can inspect the message and the details that go with it.
func wantCode(t *testing.T, err error, want apperr.Code) *apperr.Error {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %s", want)
	}
	typed, ok := apperr.As(err)
	if !ok {
		t.Fatalf("error %v is not a typed application error", err)
	}
	if typed.Code != want {
		t.Fatalf("code = %s, want %s (message: %s)", typed.Code, want, typed.Message)
	}
	return typed
}

// TestStartProfileRefusesAConfigurationSingBoxRejects asserts that a rejected
// configuration never reaches a process, and that the refusal is readable.
func TestStartProfileRefusesAConfigurationSingBoxRejects(t *testing.T) {
	t.Setenv(faketest.EnvScenario, faketest.ScenarioCheckFail)
	h := newHarness(t, defaultOptions())

	err := h.sup.StartProfile(context.Background(), "p-1")
	typed := wantCode(t, err, apperr.CodeConfigCheckFailed)

	if got := h.launcher.recorded(); len(got) != 0 {
		t.Fatalf("a process was launched for a configuration sing-box rejected: %+v", got)
	}
	if h.sup.Running() {
		t.Error("Running() = true although nothing was started")
	}
	if h.sawState(domruntime.StateRunning) {
		t.Errorf("RUNNING was emitted for a refused configuration: %v", h.states())
	}

	// The reason the user needs is the decoder's line, both in the error the
	// caller renders and in the status the UI shows afterwards.
	if !strings.Contains(typed.Message, "rejected") {
		t.Errorf("message = %q, want it to say the configuration was refused", typed.Message)
	}
	details := strings.Join(typed.Details, "\n")
	if !strings.Contains(details, "FATAL") || !strings.Contains(details, "unknown field") {
		t.Errorf("details = %q, want the sing-box decoder output", details)
	}

	status := h.sup.Status()
	if status.State != domruntime.StateFailed {
		t.Errorf("state = %s, want FAILED", status.State)
	}
	if status.LastErrorCode != string(apperr.CodeConfigCheckFailed) {
		t.Errorf("LastErrorCode = %q, want %q", status.LastErrorCode, apperr.CodeConfigCheckFailed)
	}
	if !strings.Contains(status.LastError, "unknown field") {
		t.Errorf("LastError = %q, want the decoder reason, not just a failure notice", status.LastError)
	}
	if status.PID != 0 {
		t.Errorf("pid = %d, want 0", status.PID)
	}
}

// TestStartProfileStillStartsAValidConfiguration guards the gate itself: with a
// configuration the binary accepts, the process runs as before.
func TestStartProfileStillStartsAValidConfiguration(t *testing.T) {
	h := newHarness(t, defaultOptions())

	if err := h.sup.StartProfile(context.Background(), "p-1"); err != nil {
		t.Fatalf("StartProfile() = %v, want the valid configuration to start", err)
	}
	if !h.sup.Running() {
		t.Fatal("Running() = false after a successful start")
	}
}

// TestTailFileLinesReadsTheNewestLines covers the fallback used when a process
// dies before its output reached the live buffer.
func TestTailFileLinesReadsTheNewestLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sing-box.log")

	if got := tailFileLines(path, 3); got != nil {
		t.Errorf("missing file = %v, want nothing", got)
	}
	if got := tailFileLines("", 3); got != nil {
		t.Errorf("empty path = %v, want nothing", got)
	}

	if err := os.WriteFile(path, []byte("one\n\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatalf("writing the log: %v", err)
	}
	got := tailFileLines(path, 2)
	if len(got) != 2 || got[0] != "two" || got[1] != "three" {
		t.Errorf("tail = %v, want [two three] oldest first", got)
	}
	if all := tailFileLines(path, 10); len(all) != 3 || all[0] != "one" {
		t.Errorf("whole file = %v, want three lines", all)
	}

	// A file longer than the read window keeps its newest lines and drops the
	// fragment the window starts in the middle of.
	long := strings.Repeat("filler filler filler\n", 4000) + "last line\n"
	if err := os.WriteFile(path, []byte(long), 0o600); err != nil {
		t.Fatalf("writing the long log: %v", err)
	}
	got = tailFileLines(path, 1)
	if len(got) != 1 || got[0] != "last line" {
		t.Errorf("tail of a long file = %v, want [last line]", got)
	}
}

// TestExitErrorFallsBackToTheLogFile asserts that a process which dies during
// the startup grace still explains itself: the live buffer is read first, and
// the launch log on disk supplies what the buffer missed.
func TestExitErrorFallsBackToTheLogFile(t *testing.T) {
	h := newHarness(t, defaultOptions())

	dir := h.paths.RuntimeDir
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("creating the runtime directory: %v", err)
	}
	path := filepath.Join(dir, "sing-box.log")
	line := `FATAL[0000] decode config: dns.servers[0]: legacy DNS server formats are deprecated in sing-box 1.12.0 and removed in sing-box 1.14.0`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("writing the launch log: %v", err)
	}
	h.sup.setLogPath(path)

	err := h.sup.exitError("runtime.StartProfile", exitResult{code: 1})
	details := strings.Join(apperr.DetailsOf(err), "\n")
	if !strings.Contains(details, "exitCode=1") {
		t.Errorf("details = %q, want the exit status", details)
	}
	if !strings.Contains(details, "legacy DNS server formats") {
		t.Errorf("details = %q, want the launch log line", details)
	}
}
