package singbox

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/larffxx/singboxui/internal/singbox/faketest"
)

// fakeBinPath is the fake sing-box that every process-level test runs.
//
// The tests must never touch a real sing-box — the user may well be running
// their own — so the fixture is built once here and reused, always under the
// name "sing-box" so the adapter and the process detection see exactly what they
// see in production.
var fakeBinPath string

func TestMain(m *testing.M) {
	code, err := runWithFakeBinary(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "singbox: test setup failed:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func runWithFakeBinary(m *testing.M) (int, error) {
	root, err := moduleRoot()
	if err != nil {
		return 0, err
	}
	goTool, err := goBinary()
	if err != nil {
		return 0, err
	}
	dir, err := os.MkdirTemp("", "fakesingbox-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(dir)

	// The fixture carries the platform's executable name: Windows only starts a
	// file whose extension is in PATHEXT, so a fixture called "sing-box" cannot
	// be run there at all.
	path := filepath.Join(dir, ExecutableName(runtime.GOOS))
	build := exec.Command(goTool, "build", "-o", path, "./cmd/fakesingbox")
	build.Dir = root
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		return 0, fmt.Errorf("building cmd/fakesingbox: %w", err)
	}
	fakeBinPath = path
	return m.Run(), nil
}

// moduleRoot walks up from the test directory to the directory holding go.mod,
// which is where `go build ./cmd/fakesingbox` has to run.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("cannot find the module root: no go.mod above the test directory")
		}
		dir = parent
	}
}

// goBinary finds the toolchain even when PATH was trimmed for the test run.
func goBinary() (string, error) {
	if path, err := exec.LookPath("go"); err == nil {
		return path, nil
	}
	// PATH-free fallbacks. runtime.GOROOT is deliberately avoided: it reports
	// the root the test binary was built with, which is meaningless once the
	// binary has been copied elsewhere (deprecated in Go 1.24).
	home, _ := os.UserHomeDir()
	for _, candidate := range []string{
		"/usr/local/go/bin/go",
		filepath.Join(home, "go", "bin", "go"),
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", errors.New("cannot find the go tool to build the fake sing-box")
}

// safeBuffer is a concurrent-safe capture target: the fixture keeps writing
// while a test inspects what it has printed so far, and a plain bytes.Buffer
// would race under -race.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// fakeProcess drives one running fixture and guarantees it does not outlive its
// test.
type fakeProcess struct {
	cmd      *exec.Cmd
	stdout   *safeBuffer
	stderr   *safeBuffer
	finished chan struct{}
	waitErr  error
}

func startFake(t *testing.T, args ...string) *fakeProcess {
	t.Helper()
	cmd := exec.Command(fakeBinPath, args...)
	process := &fakeProcess{
		cmd:      cmd,
		stdout:   &safeBuffer{},
		stderr:   &safeBuffer{},
		finished: make(chan struct{}),
	}
	cmd.Stdout, cmd.Stderr = process.stdout, process.stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("cannot start the fake sing-box: %v", err)
	}
	go func() {
		process.waitErr = cmd.Wait()
		close(process.finished)
	}()
	t.Cleanup(process.stop)
	return process
}

// stop kills the fixture if the test left it running, so a failing test cannot
// leak a process into the rest of the suite.
func (p *fakeProcess) stop() {
	if p.cmd.Process == nil {
		return
	}
	select {
	case <-p.finished:
		return
	default:
	}
	_ = p.cmd.Process.Kill()
	select {
	case <-p.finished:
	case <-time.After(5 * time.Second):
	}
}

// wait returns the exit code once the process has finished.
func (p *fakeProcess) wait(t *testing.T, timeout time.Duration) int {
	t.Helper()
	select {
	case <-p.finished:
	case <-time.After(timeout):
		t.Fatalf("the fake sing-box did not exit within %s", timeout)
	}
	if p.cmd.ProcessState == nil {
		t.Fatal("the fake sing-box has no process state")
	}
	return p.cmd.ProcessState.ExitCode()
}

// alive reports whether the process still exists, which is how the SIGKILL-only
// scenario is verified.
func (p *fakeProcess) alive() bool {
	if p.cmd.Process == nil {
		return false
	}
	return p.cmd.Process.Signal(syscall.Signal(0)) == nil
}

func (p *fakeProcess) terminate(t *testing.T) {
	t.Helper()
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("cannot send SIGTERM to the fake sing-box: %v", err)
	}
}

// runFake runs the fixture to completion and returns what it printed.
func runFake(t *testing.T, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(fakeBinPath, args...)
	cmd.Env = append(os.Environ(), env...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	switch {
	case err == nil:
		code = 0
	case cmd.ProcessState == nil:
		t.Fatalf("cannot run the fake sing-box: %v", err)
	default:
		code = cmd.ProcessState.ExitCode()
	}
	return out.String(), errOut.String(), code
}

// requirePosixSignals skips a test on a platform whose os.Process.Signal cannot
// deliver SIGTERM, so the suite stays runnable everywhere the code compiles.
func requirePosixSignals(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM semantics are POSIX-only; the Windows launch path is covered by the helper tests")
	}
}

func writeExecutable(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("cannot create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("cannot write %s: %v", path, err)
	}
	return path
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() >= 0
}

// waitFor polls until cond is true, and fails the test with the description if
// it never becomes true.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, what)
}

type fakeStatus struct {
	ExitCode int `json:"exitCode"`
}

func readFakeStatus(t *testing.T, path string) fakeStatus {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read the status file: %v", err)
	}
	var status fakeStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		t.Fatalf("the status file is not valid JSON (%q): %v", string(raw), err)
	}
	return status
}

// --- scenario behaviour -----------------------------------------------------

func TestFakeVersionScenarios(t *testing.T) {
	tests := []struct {
		name       string
		scenario   string
		wantStdout string
		wantCode   int
	}{
		{name: "stable", scenario: faketest.ScenarioVersion, wantStdout: "sing-box version 1.14.0"},
		{name: "older stable", scenario: faketest.ScenarioVersionOld, wantStdout: "1.13.0"},
		{name: "prerelease", scenario: faketest.ScenarioVersionBeta, wantStdout: "1.15.0-beta.1"},
		{name: "not a sing-box binary", scenario: faketest.ScenarioVersionFail, wantCode: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, code := runFake(t, []string{faketest.EnvScenario + "=" + test.scenario}, "version")
			if code != test.wantCode {
				t.Errorf("exit code = %d, want %d (stderr %q)", code, test.wantCode, stderr)
			}
			if test.wantStdout != "" && !strings.Contains(stdout, test.wantStdout) {
				t.Errorf("stdout %q does not contain %q", stdout, test.wantStdout)
			}
			if test.wantStdout == "" && stdout != "" {
				t.Errorf("stdout = %q, want no version output", stdout)
			}
			if got := strings.TrimSpace(strings.SplitN(stdout, "\n", 2)[0]); test.wantStdout != "" && got != test.wantStdout {
				t.Errorf("first line = %q, want %q", got, test.wantStdout)
			}
		})
	}
}

func TestFakeDefaultsToTheVersionBanner(t *testing.T) {
	// No scenario anywhere: a probe of a freshly copied fixture has to answer.
	stdout, stderr, code := runFake(t, []string{faketest.EnvScenario + "="}, "version")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
	}
	if !strings.Contains(stdout, "sing-box version 1.14.0") {
		t.Errorf("stdout = %q, want the version banner", stdout)
	}
}

func TestFakeCheckScenarios(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config.json")
	if err := os.WriteFile(config, []byte("{}"), 0o600); err != nil {
		t.Fatalf("cannot write the config: %v", err)
	}
	tests := []struct {
		name           string
		env            []string
		wantCode       int
		wantOutputPart string
	}{
		{name: "accepts the configuration", env: []string{faketest.EnvScenario + "=check-ok"}, wantCode: 0},
		{
			name:           "rejects the configuration",
			env:            []string{faketest.EnvScenario + "=" + faketest.ScenarioCheckFail},
			wantCode:       1,
			wantOutputPart: "FATAL[0000]",
		},
		{
			// The command word alone selects check-ok, which is what makes the
			// fixture usable without setting the environment at all.
			name:     "command word selects the scenario",
			env:      []string{faketest.EnvScenario + "="},
			wantCode: 0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, stderr, code := runFake(t, test.env, "check", "-c", config)
			if code != test.wantCode {
				t.Errorf("exit code = %d, want %d", code, test.wantCode)
			}
			if test.wantOutputPart != "" && !strings.Contains(stderr, test.wantOutputPart) {
				t.Errorf("stderr %q does not contain %q", stderr, test.wantOutputPart)
			}
		})
	}
}

func TestFakeRunRecordsPIDLogAndStatus(t *testing.T) {
	requirePosixSignals(t)
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "sing-box.pid")
	statusFile := filepath.Join(dir, "sing-box.status.json")
	logFile := filepath.Join(dir, "sing-box.log")

	process := startFake(t,
		"-scenario", faketest.ScenarioRun,
		"-pid-file", pidFile,
		"-status-file", statusFile,
		"-log-file", logFile,
	)
	waitFor(t, 3*time.Second, "the pid file", func() bool { return fileExists(pidFile) })

	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("cannot read the pid file: %v", err)
	}
	if got := strings.TrimSpace(string(raw)); got != fmt.Sprint(process.cmd.Process.Pid) {
		t.Errorf("pid file = %q, want %d", got, process.cmd.Process.Pid)
	}
	waitFor(t, 3*time.Second, "the log file", func() bool { return fileExists(logFile) })
	waitFor(t, 3*time.Second, "the first log line", func() bool { return strings.Contains(readFileString(logFile), "sing-box started") })
	waitFor(t, 3*time.Second, "a stderr log line", func() bool { return strings.Contains(process.stderr.String(), "router: loaded") })

	process.terminate(t)
	if code := process.wait(t, 5*time.Second); code != 0 {
		t.Fatalf("exit code after SIGTERM = %d, want 0", code)
	}
	if status := readFakeStatus(t, statusFile); status.ExitCode != 0 {
		t.Errorf("status file exit code = %d, want 0", status.ExitCode)
	}
	if !strings.Contains(process.stdout.String(), "sing-box stopped") {
		t.Errorf("stdout %q does not report the stop", process.stdout.String())
	}
}

func TestFakeRunSlowDelaysReadiness(t *testing.T) {
	requirePosixSignals(t)
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "sing-box.pid")

	process := startFake(t, "-scenario", faketest.ScenarioRunSlow, "-pid-file", pidFile)
	// Well inside the three second delay: a supervisor that treats "process
	// started" as "sing-box is ready" would pass here and fail in production.
	time.Sleep(500 * time.Millisecond)
	if fileExists(pidFile) {
		t.Fatal("the pid file exists before the readiness delay elapsed")
	}
	if strings.Contains(process.stdout.String(), "sing-box started") {
		t.Fatal("the fixture logged before the readiness delay elapsed")
	}
	waitFor(t, 6*time.Second, "the pid file after the readiness delay", func() bool { return fileExists(pidFile) })

	process.terminate(t)
	if code := process.wait(t, 5*time.Second); code != 0 {
		t.Fatalf("exit code after SIGTERM = %d, want 0", code)
	}
}

func TestFakeRunCrashExitsNonZero(t *testing.T) {
	dir := t.TempDir()
	statusFile := filepath.Join(dir, "sing-box.status.json")

	process := startFake(t, "-scenario", faketest.ScenarioRunCrash, "-status-file", statusFile)
	if code := process.wait(t, 5*time.Second); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if lines := strings.Count(strings.TrimSpace(process.stdout.String()), "\n") + 1; lines != 1 {
		t.Errorf("stdout has %d lines, want exactly one: %q", lines, process.stdout.String())
	}
	if status := readFakeStatus(t, statusFile); status.ExitCode != 2 {
		t.Errorf("status file exit code = %d, want 2", status.ExitCode)
	}
}

func TestFakeRunIgnoreTermSurvivesSIGTERM(t *testing.T) {
	requirePosixSignals(t)
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "sing-box.pid")

	process := startFake(t, "-scenario", faketest.ScenarioRunIgnoreTerm, "-pid-file", pidFile)
	waitFor(t, 3*time.Second, "the pid file", func() bool { return fileExists(pidFile) })

	process.terminate(t)
	time.Sleep(300 * time.Millisecond)
	if !process.alive() {
		t.Fatal("the fixture exited on SIGTERM; the kill path cannot be tested against it")
	}
	// Only SIGKILL ends this scenario, which is exactly what the supervisor has
	// to fall back to.
	if err := process.cmd.Process.Kill(); err != nil {
		t.Fatalf("cannot kill the fixture: %v", err)
	}
	if code := process.wait(t, 5*time.Second); code != -1 {
		t.Fatalf("exit code after SIGKILL = %d, want -1 (killed by a signal)", code)
	}
}

func TestFakeRunForeignStaysAliveUntilKilled(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "sing-box.pid")

	process := startFake(t, "-scenario", faketest.ScenarioRunForeign, "-pid-file", pidFile)
	waitFor(t, 3*time.Second, "the foreign process to record its pid", func() bool { return fileExists(pidFile) })
	if !process.alive() {
		t.Fatal("the foreign fixture is not running")
	}
}

func TestFakeUnknownScenarioFails(t *testing.T) {
	stdout, stderr, code := runFake(t, []string{faketest.EnvScenario + "=no-such-scenario"}, "version")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if !strings.Contains(stderr, "unknown scenario") {
		t.Errorf("stderr = %q, want an unknown-scenario message", stderr)
	}
}

func TestFakeToleratesUnknownFlags(t *testing.T) {
	// The fixture has to stand in for the real binary, which is invoked with
	// flags this fixture does not model.
	stdout, stderr, code := runFake(t, []string{faketest.EnvScenario + "=" + faketest.ScenarioVersion}, "-D", "/tmp/data", "version")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
	}
	if !strings.Contains(stdout, "sing-box version 1.14.0") {
		t.Errorf("stdout = %q, want the version banner", stdout)
	}
	if !strings.Contains(stderr, "ignoring unknown flag") {
		t.Errorf("stderr = %q, want a warning about the unknown flag", stderr)
	}
}

func readFileString(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(raw)
}
