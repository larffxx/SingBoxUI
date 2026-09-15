package childrun

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The tests need a real process to supervise. Instead of a shell script, which
// only exists on one family of operating systems, they copy this test binary
// somewhere and run it again with SINGBOXUI_TEST_CHILD set: the same binary then
// plays sing-box. Nothing is built, nothing is interpreted, and the child is
// started from an argument array exactly as production starts it (spec §24).
const (
	testChildEnv  = "SINGBOXUI_TEST_CHILD"
	testChildMode = "SINGBOXUI_TEST_CHILD_MODE"
)

// Profiles the stand-in sing-box understands.
const (
	// modeServe waits for a termination signal and exits successfully.
	modeServe = "serve"
	// modeFail exits immediately with a non-zero code.
	modeFail = "fail"
	// modeStubborn ignores termination signals, for the escalation paths.
	modeStubborn = "stubborn"
)

// TestMain lets this test binary act as the stand-in sing-box.
func TestMain(m *testing.M) {
	if os.Getenv(testChildEnv) == "1" {
		os.Exit(testChildMain())
	}
	os.Exit(m.Run())
}

// testChildMain is the whole stand-in sing-box.
//
// Signal handling is installed before the process announces itself, because the
// tests stop it as soon as they see the announcement: a stop that arrives before
// the handling is in place would end a process the test expects to survive.
func testChildMain() int {
	mode := os.Getenv(testChildMode)
	switch mode {
	case modeFail:
		fmt.Fprintln(os.Stdout, "fake sing-box: starting in "+mode+" mode")
		fmt.Fprintln(os.Stderr, "fake sing-box: refusing to run")
		return 3
	case modeStubborn:
		ignoreTermination()
		announce(mode, "ignoring "+terminationSignal().String())
		time.Sleep(30 * time.Second)
		return 9
	default:
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, terminationSignal())
		announce(mode, "serving")
		select {
		case <-signals:
			fmt.Fprintln(os.Stdout, "fake sing-box: stopping on request")
			return 0
		case <-time.After(30 * time.Second):
			return 9
		}
	}
}

// announce reports that the stand-in sing-box is running, with its signal
// handling already in place.
func announce(mode, state string) {
	fmt.Fprintln(os.Stdout, "fake sing-box: "+mode+", "+state)
	if config := configArg(os.Args[1:]); config != "" {
		fmt.Fprintln(os.Stdout, "fake sing-box: configuration "+config)
	}
}

// configArg finds the configuration file in the argument array of the child.
func configArg(args []string) string {
	for i, arg := range args {
		if (arg == "-c" || arg == "--config") && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// asTestChild makes the next child of this test run the stand-in sing-box in the
// given mode.
func asTestChild(t *testing.T, mode string) {
	t.Helper()
	t.Setenv(testChildEnv, "1")
	t.Setenv(testChildMode, mode)
}

// fakeBinaryFor copies this test binary into dir under the given name and returns
// the path: a real executable that starts as the stand-in sing-box.
func fakeBinaryFor(t *testing.T, dir, name string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate the test binary: %v", err)
	}
	content, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("read the test binary: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// executableName is the name of a sing-box binary on this system.
func executableName() string {
	if runtime.GOOS == "windows" {
		return "sing-box.exe"
	}
	return "sing-box"
}

// fixture is a configuration whose files all exist and whose paths are absolute,
// the smallest thing Config.Validate accepts.
func fixture(t *testing.T) Config {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "active.json")
	if err := os.WriteFile(configPath, []byte(`{"mode":"sleeper"}`), 0o600); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}
	return Config{
		BinaryPath: fakeBinaryFor(t, filepath.Join(dir, "bin"), executableName()),
		ConfigPath: configPath,
		WorkDir:    dir,
		LogPath:    filepath.Join(dir, "run", "singbox.log"),
		PIDPath:    filepath.Join(dir, "run", "singbox.pid"),
		StatusPath: filepath.Join(dir, "run", "singbox.status"),
	}
}

// waitFor polls until check succeeds, or fails the test after limit.
func waitFor(t *testing.T, limit time.Duration, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", limit, what)
}

// waitUntilServing waits until the stand-in sing-box has announced that it is
// running, which it does only after its signal handling is in place.
func waitUntilServing(t *testing.T, logPath string) {
	t.Helper()
	waitFor(t, 20*time.Second, "the stand-in sing-box to announce itself in "+logPath, func() bool {
		content, err := os.ReadFile(logPath)
		return err == nil && strings.Contains(string(content), "fake sing-box:")
	})
}

// exitError is the *exec.ExitError of running the stand-in sing-box in the
// failing profile.
func exitError(t *testing.T) error {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate the test binary: %v", err)
	}
	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(), testChildEnv+"=1", testChildMode+"="+modeFail)
	cmd.Stdout = nil
	cmd.Stderr = nil
	err = cmd.Run()
	if err == nil {
		t.Fatal("the failing profile of the stand-in sing-box did not fail")
	}
	return err
}

// configArg finds the configuration file in the argument array of the child.
