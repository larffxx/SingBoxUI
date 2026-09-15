package desktop

// The desktop tests drive the real configuration, binary and runtime services, so
// they need a stand-in sing-box they can point those services at. It is the same
// fixture the other packages use (cmd/fakesingbox) built once for the package,
// rather than a shell script: a script cannot be started on Windows, where only a
// program file runs, and the tests that need a working "custom binary" are as
// meaningful there as anywhere else.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/larffxx/singboxui/internal/singbox"
)

// fakeSingboxPath is the stand-in sing-box built for this package.
var fakeSingboxPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fakesingbox-desktop-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot create the fake binary directory: %v\n", err)
		os.Exit(1)
	}
	root, err := moduleRootFromTest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot locate the module root: %v\n", err)
		os.Exit(1)
	}
	path := filepath.Join(dir, singbox.ExecutableName(runtime.GOOS))
	build := exec.Command("go", "build", "-o", path, "./cmd/fakesingbox")
	build.Dir = root
	if out, buildErr := build.CombinedOutput(); buildErr != nil {
		fmt.Fprintf(os.Stderr, "cannot build the fake sing-box: %v\n%s\n", buildErr, out)
		os.Exit(1)
	}
	fakeSingboxPath = path
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// moduleRootFromTest walks up from the test directory to the directory holding
// go.mod, which is where `go build ./cmd/fakesingbox` has to run.
func moduleRootFromTest() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// placeFakeSingbox puts the stand-in into dir under the given base name, with the
// extension the platform needs, and returns its path. A base name of "impostor"
// is a program that exists but does not report a sing-box version.
func placeFakeSingbox(t *testing.T, dir, name string) string {
	t.Helper()
	if fakeSingboxPath == "" {
		t.Fatal("the fake sing-box was not built")
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	destination := filepath.Join(dir, name)
	content, err := os.ReadFile(fakeSingboxPath)
	if err != nil {
		t.Fatalf("read the fake sing-box: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	if err := os.WriteFile(destination, content, 0o755); err != nil {
		t.Fatalf("write %s: %v", destination, err)
	}
	return destination
}
