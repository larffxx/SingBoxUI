package childrun

import (
	"bufio"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfigValidateRequiresAbsolutePaths(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{"empty", Config{}},
		{"relative binary", Config{BinaryPath: "sing-box", ConfigPath: "/c.json", LogPath: "/l", PIDPath: "/p", StatusPath: "/s"}},
		{"relative log", Config{BinaryPath: "/b", ConfigPath: "/c.json", LogPath: "l", PIDPath: "/p", StatusPath: "/s"}},
		{"no status path", Config{BinaryPath: "/b", ConfigPath: "/c.json", LogPath: "/l", PIDPath: "/p"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.cfg.Validate(); err == nil {
				t.Fatalf("Validate(%+v) accepted an unusable configuration", test.cfg)
			}
		})
	}
	if err := fixture(t).Validate(); err != nil {
		t.Fatalf("Validate of a complete configuration: %v", err)
	}
}

func TestCommandArgsDefaultsToRunWithTheConfiguration(t *testing.T) {
	cfg := Config{ConfigPath: filepath.Join(t.TempDir(), "active.json")}
	got := cfg.CommandArgs()
	want := []string{"run", "-c", cfg.ConfigPath}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("CommandArgs() = %q, want %q", got, want)
	}
	// The override is copied, so a caller cannot change the child's command line
	// through the slice it handed in.
	cfg.Args = []string{"check", "-c", "other.json"}
	got = cfg.CommandArgs()
	cfg.Args[0] = "mutated"
	if got[0] != "check" {
		t.Fatalf("CommandArgs() shares its argument array with the caller: %q", got)
	}
}

func TestStartRunsTheConfiguredProgram(t *testing.T) {
	asTestChild(t, modeServe)
	cfg := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	child, err := Start(ctx, cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = child.Kill() })

	if child.PID() <= 0 {
		t.Fatalf("PID() = %d, want the pid of the started process", child.PID())
	}
	if child.Elevated() {
		t.Error("a locally started process must not report itself as elevated")
	}
	if child.Exited() {
		t.Error("Exited() is true right after Start")
	}

	// The pid file is the record the supervisor stops from, so Start must have
	// written it before it returned (internal-contracts.md §3).
	record, err := ReadPIDFile(cfg.PIDPath)
	if err != nil {
		t.Fatalf("the pid file was not written by Start: %v", err)
	}
	if record.PID != child.PID() {
		t.Errorf("the pid file records pid %d, want %d", record.PID, child.PID())
	}
	if !record.Valid() {
		t.Errorf("the pid file is not usable: %+v", record)
	}

	waitUntilServing(t, cfg.LogPath)

	logs := child.Logs()
	defer logs.Close()
	line, err := bufio.NewReader(logs).ReadString('\n')
	if err != nil {
		t.Fatalf("reading the child log stream: %v", err)
	}
	if !strings.Contains(line, "fake sing-box") {
		t.Errorf("log stream starts with %q", line)
	}

	if err := child.Terminate(); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	code, err := child.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if code < 0 {
		t.Errorf("Wait returned %d after a graceful stop", code)
	}
	if !child.Exited() {
		t.Error("Exited() is false after Wait returned")
	}
	status, err := ReadStatus(cfg.StatusPath)
	if err != nil {
		t.Fatalf("Wait did not record the exit status: %v", err)
	}
	if status.PID != child.PID() || status.ExitCode != code {
		t.Errorf("status %+v does not describe the child (pid %d, code %d)", status, child.PID(), code)
	}
}

func TestStartReportsANonZeroExitCode(t *testing.T) {
	asTestChild(t, modeFail)
	cfg := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	child, err := Start(ctx, cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	code, err := child.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if code != 3 {
		t.Errorf("Wait = %d, want the exit code 3 of the stand-in sing-box", code)
	}
	status, err := ReadStatus(cfg.StatusPath)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if status.ExitCode != 3 {
		t.Errorf("the recorded exit code is %d, want 3", status.ExitCode)
	}
	if status.EndedAt.IsZero() {
		t.Error("the recorded status has no end time")
	}
}

func TestStartRefusesAConfigurationThatCannotBeUsed(t *testing.T) {
	_, err := Start(context.Background(), Config{BinaryPath: "sing-box"})
	if err == nil {
		t.Fatal("Start accepted an unusable configuration")
	}
}

func TestStartReportsAMissingExecutable(t *testing.T) {
	cfg := fixture(t)
	cfg.BinaryPath = filepath.Join(filepath.Dir(cfg.BinaryPath), "not-installed")
	_, err := Start(context.Background(), cfg)
	if err == nil {
		t.Fatal("Start reported success for an executable that does not exist")
	}
	if !strings.Contains(err.Error(), "not-installed") {
		t.Errorf("the error does not name the missing executable: %v", err)
	}
}

func TestPIDFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "singbox.pid")
	want := PIDFile{PID: os.Getpid(), StartTime: time.Now().UnixNano(), BinaryPath: "/bin/sing-box", RecordedAt: time.Now()}
	if err := WritePIDFile(path, want); err != nil {
		t.Fatalf("WritePIDFile: %v", err)
	}
	got, err := ReadPIDFile(path)
	if err != nil {
		t.Fatalf("ReadPIDFile: %v", err)
	}
	if got.PID != want.PID || got.StartTime != want.StartTime || got.BinaryPath != want.BinaryPath {
		t.Errorf("ReadPIDFile = %+v, want %+v", got, want)
	}
	if !got.Valid() {
		t.Error("the record read back is not usable")
	}
	if (PIDFile{}).Valid() {
		t.Error("an empty pid record reports itself as usable")
	}
}

func TestReadPIDFileOfAMissingFileIsNotExist(t *testing.T) {
	_, err := ReadPIDFile(filepath.Join(t.TempDir(), "absent.pid"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadPIDFile of a missing file = %v, want fs.ErrNotExist", err)
	}
	_, err = ReadStatus(filepath.Join(t.TempDir(), "absent.status"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadStatus of a missing file = %v, want fs.ErrNotExist", err)
	}
}

func TestReadPIDFileRefusesGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "singbox.pid")
	if err := os.WriteFile(path, []byte("not a record"), 0o600); err != nil {
		t.Fatalf("write the pid file: %v", err)
	}
	if _, err := ReadPIDFile(path); err == nil {
		t.Fatal("ReadPIDFile accepted a file that is not a pid record")
	}
}

func TestStatusRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "singbox.status")
	want := Status{PID: 4321, ExitCode: 7, StartedAt: time.Now(), EndedAt: time.Now(), Error: "boom"}
	if err := WriteStatus(path, want); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}
	got, err := ReadStatus(path)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if got.PID != want.PID || got.ExitCode != want.ExitCode || got.Error != want.Error {
		t.Errorf("ReadStatus = %+v, want %+v", got, want)
	}
}

func TestAliveAndStartTimeDescribeRealProcesses(t *testing.T) {
	if !Alive(os.Getpid()) {
		t.Error("Alive reports the running test process as gone")
	}
	if Alive(0) {
		t.Error("Alive reports pid 0 as running")
	}
	start, err := StartTimeNano(os.Getpid())
	if err != nil {
		t.Fatalf("StartTimeNano: %v", err)
	}
	if start <= 0 {
		t.Errorf("StartTimeNano = %d, want a positive start time", start)
	}
	if _, err := StartTimeNano(0); err == nil {
		t.Error("StartTimeNano accepted pid 0")
	}
}

func TestStopVerifiedIsANoOpWithoutAPIDFile(t *testing.T) {
	result, err := StopVerified(context.Background(), filepath.Join(t.TempDir(), "absent.pid"), false, time.Second)
	if err != nil {
		t.Fatalf("StopVerified: %v", err)
	}
	if result.Stopped || result.WasRunning || result.Forced {
		t.Errorf("StopVerified without a pid file = %+v, want an empty result", result)
	}
}

func TestStopVerifiedRefusesAReusedPID(t *testing.T) {
	// A pid file whose start time does not describe the running process must never
	// be signalled: the pid has been reused (internal-contracts.md §3).
	path := filepath.Join(t.TempDir(), "singbox.pid")
	stale := PIDFile{PID: os.Getpid(), StartTime: time.Now().Add(-time.Hour).UnixNano()}
	if err := WritePIDFile(path, stale); err != nil {
		t.Fatalf("WritePIDFile: %v", err)
	}
	result, err := StopVerified(context.Background(), path, false, 500*time.Millisecond)
	if !errors.Is(err, ErrIdentityMismatch) {
		t.Fatalf("StopVerified = %v, want ErrIdentityMismatch", err)
	}
	if result.Stopped {
		t.Error("StopVerified reports a stop it must not have performed")
	}
	if !Alive(os.Getpid()) {
		t.Fatal("StopVerified signalled the process the stale pid file pointed at")
	}
}

func TestStopVerifiedStopsTheRecordedProcess(t *testing.T) {
	asTestChild(t, modeServe)
	cfg := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	child, err := Start(ctx, cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = child.Kill() })

	result, err := StopVerified(ctx, cfg.PIDPath, false, 10*time.Second)
	if err != nil {
		t.Fatalf("StopVerified: %v", err)
	}
	if !result.WasRunning || !result.Stopped {
		t.Errorf("StopVerified = %+v, want a stopped process that was running", result)
	}
	if result.Forced {
		t.Error("a process answering the termination request was killed")
	}
	if _, err := child.Wait(); err != nil {
		t.Fatalf("Wait after a verified stop: %v", err)
	}
	// The process is gone from the kernel's point of view.
	if Alive(result.PID) {
		t.Error("the process is still running after a verified stop")
	}
}

func TestStopVerifiedEscalatesToAKill(t *testing.T) {
	asTestChild(t, modeStubborn)
	cfg := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	child, err := Start(ctx, cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = child.Kill() })
	waitUntilServing(t, cfg.LogPath)

	result, err := StopVerified(ctx, cfg.PIDPath, false, 250*time.Millisecond)
	if err != nil {
		t.Fatalf("StopVerified: %v", err)
	}
	if !result.Stopped {
		t.Errorf("StopVerified = %+v, want the process stopped", result)
	}
	// Windows has no SIGTERM: the graceful attempt is taskkill without /F, which
	// stops the stand-in outright, so an escalation to a kill is not observable
	// there — the process is gone either way, which is what the caller needs.
	if runtime.GOOS != "windows" && !result.Forced {
		t.Errorf("StopVerified = %+v, want a process that had to be killed", result)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("fixture: %v", err)
	}
}

func TestStopVerifiedKillsOnRequest(t *testing.T) {
	asTestChild(t, modeStubborn)
	cfg := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	child, err := Start(ctx, cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = child.Kill() })
	waitUntilServing(t, cfg.LogPath)

	result, err := StopVerified(ctx, cfg.PIDPath, true, time.Second)
	if err != nil {
		t.Fatalf("StopVerified(force): %v", err)
	}
	if !result.Forced || !result.Stopped {
		t.Errorf("StopVerified(force) = %+v, want a forced stop", result)
	}
}

func TestStopVerifiedStopsWhenTheContextEnds(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("there is no SIGTERM to ignore on Windows: taskkill stops the stand-in at once, so the escalation this case asserts does not exist")
	}
	asTestChild(t, modeStubborn)
	cfg := fixture(t)
	setup, cancelSetup := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelSetup()

	child, err := Start(setup, cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = child.Kill() })
	waitUntilServing(t, cfg.LogPath)

	// A stop whose context ends must still not leave the process behind.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	result, err := StopVerified(ctx, cfg.PIDPath, false, 30*time.Second)
	if err == nil {
		t.Fatal("StopVerified reported success although its context ended")
	}
	if !result.Forced {
		t.Errorf("StopVerified = %+v, want the process to have been killed", result)
	}
}

func TestTailReaderFollowsAGrowingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "singbox.log")
	if err := os.WriteFile(path, []byte("first line\n"), 0o600); err != nil {
		t.Fatalf("write the log: %v", err)
	}
	var finished atomic.Bool
	reader := NewTailReader(context.Background(), path, finished.Load)
	defer reader.Close()
	lines := bufio.NewReader(reader)

	first, err := lines.ReadString('\n')
	if err != nil {
		t.Fatalf("read the first line: %v", err)
	}
	if first != "first line\n" {
		t.Errorf("first line = %q", first)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open the log for appending: %v", err)
	}
	if _, err := file.WriteString("second line\n"); err != nil {
		t.Fatalf("append to the log: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close the log: %v", err)
	}
	second, err := lines.ReadString('\n')
	if err != nil {
		t.Fatalf("read the appended line: %v", err)
	}
	if second != "second line\n" {
		t.Errorf("appended line = %q", second)
	}

	finished.Store(true)
	if _, err := lines.ReadString('\n'); !errors.Is(err, io.EOF) {
		t.Fatalf("reading a finished log = %v, want io.EOF", err)
	}
}

func TestTailReaderWaitsForAFileThatDoesNotExistYet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "later.log")
	var finished atomic.Bool
	reader := NewTailReader(context.Background(), path, finished.Load)
	done := make(chan string, 1)
	go func() {
		buffer := make([]byte, 64)
		n, _ := reader.Read(buffer)
		done <- string(buffer[:n])
	}()
	select {
	case <-done:
		t.Fatal("the reader reported data before the writer created the file")
	case <-time.After(100 * time.Millisecond):
	}
	if err := os.WriteFile(path, []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write the log: %v", err)
	}
	select {
	case got := <-done:
		if got != "hello\n" {
			t.Errorf("the reader returned %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the reader did not pick up the file that appeared")
	}
	reader.Close()
}

func TestTailReaderStopsWhenClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "singbox.log")
	if err := os.WriteFile(path, []byte("data\n"), 0o600); err != nil {
		t.Fatalf("write the log: %v", err)
	}
	reader := NewTailReader(context.Background(), path, func() bool { return false })
	done := make(chan error, 1)
	go func() {
		buffer := make([]byte, 64)
		for {
			if _, err := reader.Read(buffer); err != nil {
				done <- err
				return
			}
		}
	}()
	time.Sleep(100 * time.Millisecond)
	if err := reader.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("a closed reader returned %v, want io.EOF", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("closing the reader did not end the blocked read")
	}
	if err := reader.Close(); err != nil {
		t.Errorf("Close is not idempotent: %v", err)
	}
}

func TestIsExitErrorSeparatesAFailedProgramFromAFailureToRunOne(t *testing.T) {
	if IsExitError(errors.New("cannot start")) {
		t.Error("IsExitError accepted a plain error")
	}
	if IsExitError(nil) {
		t.Error("IsExitError accepted nil")
	}
	if err := exitError(t); !IsExitError(err) {
		t.Fatalf("IsExitError(%v) = false, want true for a program that ran and failed", err)
	}
}
