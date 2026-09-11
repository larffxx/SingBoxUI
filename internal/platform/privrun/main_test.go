package privrun

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The runner starts a real process on its direct path, so the tests give it one:
// this test binary is copied into the data directory as sing-box and run again
// with SINGBOXUI_TEST_CHILD set, where it plays sing-box. Nothing is interpreted
// and no shell is involved (spec §24, §83).
const (
	testChildEnv  = "SINGBOXUI_TEST_CHILD"
	testChildMode = "SINGBOXUI_TEST_CHILD_MODE"
	// modeServe waits for a termination signal and exits successfully.
	modeServe = "serve"
)

// helperPath is the name the launching side would run. The tests drive the
// runner directly, so it only has to be an absolute path.
const helperPath = "/opt/SingBoxUI/singboxui-priv"

// TestMain lets this test binary act as the stand-in sing-box.
func TestMain(m *testing.M) {
	if os.Getenv(testChildEnv) == "1" {
		os.Exit(testChildMain())
	}
	os.Exit(m.Run())
}

// testChildMain is the whole stand-in sing-box. It announces itself only once its
// signal handling is in place, so a test that stops it right after the
// announcement cannot race the handler.
func testChildMain() int {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	wd, err := os.Getwd()
	if err != nil {
		wd = "unknown"
	}
	fmt.Fprintln(os.Stdout, "fake sing-box: serving in "+wd)
	if config := configArg(os.Args[1:]); config != "" {
		fmt.Fprintln(os.Stdout, "fake sing-box: configuration "+config)
	}
	select {
	case <-signals:
		fmt.Fprintln(os.Stdout, "fake sing-box: stopping on request")
		return 0
	case <-time.After(60 * time.Second):
		return 9
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

// asTestChild makes the next child of this test run the stand-in sing-box.
func asTestChild(t *testing.T) {
	t.Helper()
	t.Setenv(testChildEnv, "1")
	t.Setenv(testChildMode, modeServe)
}

// layout is a data directory with every file a request must point at, laid out
// the way internal-contracts.md §2 describes it.
type layout struct {
	dataDir string
	binary  string
	config  string
	workDir string
	log     string
	pid     string
	status  string
}

// request is the port request of this layout, elevated or not.
func (l layout) request(elevate bool) privilegeRequest {
	return privilegeRequest{
		BinaryPath: l.binary,
		ConfigPath: l.config,
		WorkDir:    l.workDir,
		LogPath:    l.log,
		PIDPath:    l.pid,
		StatusPath: l.status,
		Elevate:    elevate,
		Reason:     "TUN mode requires administrator rights",
	}
}

// newLayout builds the data directory with a runnable sing-box in it.
func newLayout(t *testing.T) layout {
	t.Helper()
	dataDir := filepath.Join(t.TempDir(), "SingBoxUI")
	l := layout{
		dataDir: dataDir,
		binary:  filepath.Join(dataDir, "bin", "sing-box", "1.12.0", executableName(runtime.GOOS)),
		config:  filepath.Join(dataDir, "config", "active.json"),
		workDir: filepath.Join(dataDir, "run"),
		log:     filepath.Join(dataDir, "run", "singbox.log"),
		pid:     filepath.Join(dataDir, "run", "singbox.pid"),
		status:  filepath.Join(dataDir, "run", "singbox.status"),
	}
	fakeBinaryAt(t, l.binary)
	writeFile(t, l.config, `{"log":{"level":"info"}}`)
	for _, dir := range []string{filepath.Dir(l.config), l.workDir} {
		if err := os.MkdirAll(dir, dirMode); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	return l
}

// fakeBinaryAt copies this test binary to path, so the runner starts the
// stand-in sing-box from the name the helper protocol insists on.
func fakeBinaryAt(t *testing.T, path string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate the test binary: %v", err)
	}
	content, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("read the test binary: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// writeFile writes a file the fixtures need and returns its path.
func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
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
// running, in the given working directory when one is known.
func waitUntilServing(t *testing.T, logPath string) {
	t.Helper()
	waitFor(t, 30*time.Second, "the stand-in sing-box to announce itself in "+logPath, func() bool {
		content, err := os.ReadFile(logPath)
		return err == nil && strings.Contains(string(content), "fake sing-box: serving")
	})
}
