package platform

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/applications"
	"github.com/larffxx/singboxui/internal/privilege"
)

// stubAutostart is a login item that records what the platform asked of it.
type stubAutostart struct {
	enabled bool
	legacy  string
}

var _ Autostart = (*stubAutostart)(nil)

func (s *stubAutostart) Supported() bool { return true }

func (s *stubAutostart) Enabled() (bool, error) { return s.enabled, nil }

func (s *stubAutostart) Enable(string) error {
	s.enabled = true
	return nil
}

func (s *stubAutostart) Disable() error {
	s.enabled = false
	return nil
}

func (s *stubAutostart) LegacyEntry() string { return s.legacy }

func (s *stubAutostart) RemoveLegacy() error {
	s.legacy = ""
	return nil
}

// stubApplications is an application catalog that lists one application: this
// package only hands it on, so the platform port is what is under test here.
type stubApplications struct{}

var _ ApplicationCatalog = (*stubApplications)(nil)

func (s *stubApplications) Supported() bool { return true }

func (s *stubApplications) UnsupportedReason() string { return "" }

func (s *stubApplications) List() ([]applications.Application, error) {
	return []applications.Application{{Name: "Test", Path: "/Applications/Test.app"}}, nil
}

// stubRunner is a privilege runner that launches nothing: this package only hands
// it on, so the platform port is what is under test here.
type stubRunner struct{}

var _ privilege.Runner = stubRunner{}

func (stubRunner) Start(context.Context, privilege.Request) (privilege.Process, error) {
	return nil, privilege.ErrUnsupported
}

func (stubRunner) Supported() (bool, string) { return false, "the test runner does not elevate" }

// documentedLayoutRoot is the example data directory the layout contract names on
// this platform: the macOS one (internal-contracts.md §2) or the Windows one the
// platform adapter resolves. It is absolute on the host the suite runs on, which a
// POSIX path is not on Windows.
func documentedLayoutRoot() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(`C:\`, "Users", "someone", "AppData", "Local", "SingBoxUI")
	}
	return filepath.Join(string(filepath.Separator), "Users", "someone", "Library", "Application Support", "SingBoxUI")
}

func TestLayoutIsTheDocumentedDataDirectory(t *testing.T) {
	// internal-contracts.md §2 fixes the layout; every path is derived from the
	// data directory and nothing from the working directory.
	root := documentedLayoutRoot()
	paths := layout(root)
	configDir := filepath.Join(root, "config")

	// DataDir is the data directory itself, so it is the one path that is not
	// below the data directory; everything else is.
	if paths.DataDir != root {
		t.Errorf("DataDir = %q, want %q", paths.DataDir, root)
	}
	for name, got := range map[string]string{
		"DBPath":             paths.DBPath,
		"LogPath":            paths.LogPath,
		"ConfigDir":          paths.ConfigDir,
		"ActiveConfigPath":   paths.ActiveConfigPath,
		"LastGoodConfigPath": paths.LastGoodConfigPath,
		"RuntimeDir":         paths.RuntimeDir,
		"BinDir":             paths.BinDir,
		"TempDir":            paths.TempDir,
	} {
		if got == "" {
			t.Errorf("%s is empty", name)
		}
		if !filepath.IsAbs(got) {
			t.Errorf("%s = %q, want an absolute path", name, got)
		}
		if !strings.HasPrefix(got, root+string(filepath.Separator)) {
			t.Errorf("%s = %q, want it below the data directory %q", name, got, root)
		}
	}
	for name, want := range map[string]string{
		"DBPath":             filepath.Join(root, "singboxui.db"),
		"LogPath":            filepath.Join(root, "logs", "singboxui.log"),
		"ConfigDir":          configDir,
		"ActiveConfigPath":   filepath.Join(configDir, "active.json"),
		"LastGoodConfigPath": filepath.Join(configDir, "last-good.json"),
		"RuntimeDir":         filepath.Join(root, "run"),
		"BinDir":             filepath.Join(root, "bin", "sing-box"),
		"TempDir":            filepath.Join(root, "tmp"),
	} {
		got := map[string]string{
			"DBPath":             paths.DBPath,
			"LogPath":            paths.LogPath,
			"ConfigDir":          paths.ConfigDir,
			"ActiveConfigPath":   paths.ActiveConfigPath,
			"LastGoodConfigPath": paths.LastGoodConfigPath,
			"RuntimeDir":         paths.RuntimeDir,
			"BinDir":             paths.BinDir,
			"TempDir":            paths.TempDir,
		}[name]
		if got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestManagedBinaryPathKeepsTheVersionAndTheExecutableName(t *testing.T) {
	paths := layout(t.TempDir())
	bin := filepath.Join(paths.BinDir, "1.12.0", "sing-box")
	got := paths.ManagedBinaryPath("1.12.0", "sing-box")
	if got != bin {
		t.Errorf("ManagedBinaryPath(%q, %q) = %q, want %q", "1.12.0", "sing-box", got, bin)
	}
	if other := paths.ManagedBinaryPath("1.11.0", "sing-box"); other == got {
		t.Errorf("two versions share the install location %q, so an upgrade would overwrite one", got)
	}
	if exe := paths.ManagedBinaryPath("1.12.0", "sing-box.exe"); !strings.HasSuffix(exe, "sing-box.exe") {
		t.Errorf("ManagedBinaryPath() = %q, want the executable name kept verbatim for windows", exe)
	}
	if !strings.HasPrefix(got, paths.BinDir+string(filepath.Separator)) {
		t.Errorf("ManagedBinaryPath() = %q, want it inside BinDir %q", got, paths.BinDir)
	}
}

func TestDirectoriesCoverTheLayoutWithoutRepetition(t *testing.T) {
	paths := layout(t.TempDir())
	dirs := paths.directories()
	seen := make(map[string]bool, len(dirs))
	for _, dir := range dirs {
		if dir == "" {
			t.Fatal("directories() contains an empty entry")
		}
		if seen[dir] {
			t.Errorf("directories() repeats %q", dir)
		}
		seen[dir] = true
	}
	for name, want := range map[string]string{
		"the data directory": paths.DataDir,
		"the database's":     filepath.Dir(paths.DBPath),
		"the log's":          filepath.Dir(paths.LogPath),
		"the config's":       paths.ConfigDir,
		"the runtime's":      paths.RuntimeDir,
		"the binaries'":      paths.BinDir,
		"the temporary":      paths.TempDir,
	} {
		if !seen[want] {
			t.Errorf("directories() does not create %s directory %q", name, want)
		}
	}
	if zero := (Paths{}).directories(); len(zero) != 0 {
		t.Errorf("a zero Paths reports the directories %q, want none", zero)
	}
}

func TestHostSupportKnowsTheTargets(t *testing.T) {
	for _, tc := range []struct {
		goos, goarch string
		supported    bool
	}{
		{"darwin", "arm64", true},
		{"darwin", "amd64", true},
		{"windows", "arm64", true},
		{"windows", "amd64", true},
		{"linux", "amd64", false},
		{"freebsd", "arm64", false},
		{"", "", false},
	} {
		t.Run(tc.goos+"/"+tc.goarch, func(t *testing.T) {
			info := hostSupport(tc.goos, tc.goarch)
			if info.OS != tc.goos || info.Arch != tc.goarch {
				t.Errorf("hostSupport(%q, %q) names %q/%q", tc.goos, tc.goarch, info.OS, info.Arch)
			}
			if info.Supported != tc.supported {
				t.Fatalf("hostSupport(%q, %q).Supported = %v, want %v",
					tc.goos, tc.goarch, info.Supported, tc.supported)
			}
			if tc.supported {
				if info.UnsupportedReason != "" {
					t.Errorf("a supported platform explains why it is unsupported: %q", info.UnsupportedReason)
				}
				return
			}
			if !strings.Contains(info.UnsupportedReason, tc.goos) {
				t.Errorf("UnsupportedReason = %q, want it to name %q", info.UnsupportedReason, tc.goos)
			}
		})
	}
}

func TestSystemHandsOnTheMechanismsOfThePlatform(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "SingBoxUI")
	autostart := &stubAutostart{legacy: "the Java entry"}
	catalog := &stubApplications{}
	runner := stubRunner{}
	platform := newSystem(dir, autostart, catalog, runner)

	if got := platform.Paths(); got != layout(dir) {
		t.Errorf("Paths() = %+v, want the layout of %q", got, dir)
	}
	if got := platform.Info(); got.OS != runtime.GOOS || got.Arch != runtime.GOARCH {
		t.Errorf("Info() = %+v, want %s/%s", got, runtime.GOOS, runtime.GOARCH)
	}
	if got := platform.Autostart(); got != Autostart(autostart) {
		t.Error("Autostart() does not return the platform's login item")
	}
	if got := platform.Applications(); got != ApplicationCatalog(catalog) {
		t.Error("Applications() does not return the platform's application catalog")
	}
	if got := platform.PrivilegeRunner(); got != privilege.Runner(runner) {
		t.Error("PrivilegeRunner() does not return the platform's privileged-launch adapter")
	}
	// Nothing above may create anything: EnsureDirs is what does that.
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("building the platform touched %q: %v", dir, err)
	}
}

func TestEnsureDirsCreatesTheLayoutPrivately(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "SingBoxUI")
	platform := newSystem(dir, &stubAutostart{}, &stubApplications{}, stubRunner{})

	if err := platform.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() = %v", err)
	}
	paths := platform.Paths()
	for _, want := range paths.directories() {
		info, err := os.Stat(want)
		if err != nil {
			t.Fatalf("EnsureDirs() did not create %q: %v", want, err)
		}
		if !info.IsDir() {
			t.Errorf("%q exists but is not a directory", want)
		}
	}
	// The layout holds credentials and inspected traffic, so every directory it
	// created is private to its owner. Windows carries the same intent in an ACL
	// that a permission bit cannot express, and Go ignores the mode there.
	if runtime.GOOS != "windows" {
		for _, want := range paths.directories() {
			info, err := os.Stat(want)
			if err != nil {
				t.Fatalf("stat %q: %v", want, err)
			}
			if perm := info.Mode().Perm(); perm != dirMode {
				t.Errorf("%q has permissions %04o, want %04o", want, perm, dirMode)
			}
		}
	}
	// It creates no file, only directories.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %q: %v", dir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			t.Errorf("EnsureDirs() created the file %q", filepath.Join(dir, entry.Name()))
		}
	}
	// It is idempotent and leaves an existing directory's permissions alone.
	if err := platform.EnsureDirs(); err != nil {
		t.Fatalf("the second EnsureDirs() = %v", err)
	}
	if runtime.GOOS != "windows" {
		existing := paths.RuntimeDir
		if err := os.Chmod(existing, 0o755); err != nil {
			t.Fatalf("chmod %q: %v", existing, err)
		}
		if err := platform.EnsureDirs(); err != nil {
			t.Fatalf("EnsureDirs() after a permission change = %v", err)
		}
		info, err := os.Stat(existing)
		if err != nil {
			t.Fatalf("stat %q: %v", existing, err)
		}
		if perm := info.Mode().Perm(); perm != 0o755 {
			t.Errorf("EnsureDirs() changed the permissions of the existing directory %q to %04o", existing, perm)
		}
	}
}

func TestEnsureDirsReportsAFailureAsATypedError(t *testing.T) {
	root := t.TempDir()
	// The data directory is a file, so no layout can be created below it.
	dataDir := filepath.Join(root, "SingBoxUI")
	if err := os.WriteFile(dataDir, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("prepare the fixture: %v", err)
	}
	platform := newSystem(dataDir, &stubAutostart{}, &stubApplications{}, stubRunner{})

	err := platform.EnsureDirs()
	if err == nil {
		t.Fatal("EnsureDirs() reported success although the data directory cannot be created")
	}
	if !apperr.IsCode(err, apperr.CodeInternal) {
		t.Errorf("EnsureDirs() = %v, want an INTERNAL_ERROR error", err)
	}
	if message := apperr.MessageOf(err); !strings.Contains(message, dataDir) {
		t.Errorf("EnsureDirs() message = %q, want it to name the directory that failed", message)
	}
}

func TestUnsupportedPlatformIsASentinel(t *testing.T) {
	if ErrUnsupportedPlatform == nil {
		t.Fatal("ErrUnsupportedPlatform is nil")
	}
	if !strings.Contains(ErrUnsupportedPlatform.Error(), "unsupported") {
		t.Errorf("ErrUnsupportedPlatform = %q, want it to say what it is", ErrUnsupportedPlatform)
	}
}
