package singbox

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/settings"
	"github.com/larffxx/singboxui/internal/platform"
	"github.com/larffxx/singboxui/internal/singbox/faketest"
)

// copyFakeBinary puts the fixture where the adapter expects an installed
// executable and hands back the path, so every probe test exercises the real
// process path.
func copyFakeBinary(t *testing.T, dir, name string) string {
	t.Helper()
	raw, err := os.ReadFile(fakeBinPath)
	if err != nil {
		t.Fatalf("cannot read the fake sing-box: %v", err)
	}
	// The name carries the platform's program extension when it has none: Windows
	// starts a program file only, so a copy called "sing-box" cannot be run there
	// at all, whatever the adapter thinks of it.
	if runtime.GOOS == "windows" && filepath.Ext(name) == "" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, raw, 0o755); err != nil {
		t.Fatalf("cannot write %s: %v", path, err)
	}
	return path
}

func TestProbeAgainstFakeBinary(t *testing.T) {
	tests := []struct {
		name       string
		scenario   string
		want       Version
		wantString string
		wantCode   apperr.Code
	}{
		{
			name:       "stable banner",
			scenario:   faketest.ScenarioVersion,
			want:       Version{Major: 1, Minor: 14, Patch: 0, Raw: "1.14.0"},
			wantString: "1.14.0",
		},
		{
			name:       "older stable",
			scenario:   faketest.ScenarioVersionOld,
			want:       Version{Major: 1, Minor: 13, Patch: 0, Raw: "1.13.0"},
			wantString: "1.13.0",
		},
		{
			name:       "prerelease",
			scenario:   faketest.ScenarioVersionBeta,
			want:       Version{Major: 1, Minor: 15, Patch: 0, Pre: "beta.1", Raw: "1.15.0-beta.1"},
			wantString: "1.15.0-beta.1",
		},
		{
			name:     "executable that is not sing-box",
			scenario: faketest.ScenarioVersionFail,
			wantCode: apperr.CodeBinarySourceInvalid,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// The scenario travels through the environment, which is the
			// documented way to drive the fixture through a code path that
			// builds the command line itself.
			t.Setenv(faketest.EnvScenario, test.scenario)
			path := copyFakeBinary(t, t.TempDir(), "sing-box")

			got, err := Probe(context.Background(), path)
			if test.wantCode != "" {
				if err == nil {
					t.Fatalf("Probe() = %+v, want an error", got)
				}
				if code := apperr.CodeOf(err); code != test.wantCode {
					t.Errorf("error code = %s, want %s (%v)", code, test.wantCode, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Probe() failed: %v", err)
			}
			if got != test.want {
				t.Errorf("Probe() = %+v, want %+v", got, test.want)
			}
			if got.String() != test.wantString {
				t.Errorf("version string = %q, want %q", got.String(), test.wantString)
			}
		})
	}
}

func TestProbeRejectsUnusablePaths(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "sing-box")
	if err := os.WriteFile(plain, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatalf("cannot write %s: %v", plain, err)
	}
	tests := []struct {
		name     string
		path     string
		wantCode apperr.Code
	}{
		{name: "missing file", path: filepath.Join(dir, "absent"), wantCode: apperr.CodeBinaryNotFound},
		{name: "not executable", path: plain, wantCode: apperr.CodeBinaryNotFound},
		{name: "directory", path: dir, wantCode: apperr.CodeBinaryNotFound},
		{name: "empty path", path: "", wantCode: apperr.CodeBinaryNotFound},
		{name: "blank path", path: "   ", wantCode: apperr.CodeBinaryNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Probe(context.Background(), test.path)
			if err == nil {
				t.Fatalf("Probe(%q) = %+v, want an error", test.path, got)
			}
			if code := apperr.CodeOf(err); code != test.wantCode {
				t.Errorf("error code = %s, want %s (%v)", code, test.wantCode, err)
			}
			if !got.Empty() {
				t.Errorf("version = %+v, want the zero Version", got)
			}
		})
	}
}

func TestProbeDoesNotRunTheShell(t *testing.T) {
	// A path with shell metacharacters must be executed as one argument array
	// entry, never interpolated into a command line (spec §24).
	t.Setenv(faketest.EnvScenario, faketest.ScenarioVersion)
	dir := t.TempDir()
	path := copyFakeBinary(t, dir, "sing-box; echo pwned")
	got, err := Probe(context.Background(), path)
	if err != nil {
		t.Fatalf("Probe(%q) failed: %v", path, err)
	}
	if got.String() != "1.14.0" {
		t.Errorf("version = %q, want 1.14.0", got.String())
	}
}

func TestIsExecutable(t *testing.T) {
	dir := t.TempDir()
	executable := writeExecutable(t, filepath.Join(dir, "exec"), "#!/bin/sh\n")
	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, []byte("data"), 0o600); err != nil {
		t.Fatalf("cannot write %s: %v", plain, err)
	}
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "executable file", path: executable, want: true},
		// Windows records no execute bit: there, any regular file is reported as
		// potentially executable and the probe is what refuses it, so the contrast
		// belongs to the platforms that record a permission bit.
		{name: "plain file", path: plain, want: runtime.GOOS == "windows"},
		{name: "directory", path: dir, want: false},
		{name: "missing", path: filepath.Join(dir, "absent"), want: false},
		{name: "empty", path: "", want: false},
		{name: "blank", path: " ", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsExecutable(test.path); got != test.want {
				t.Errorf("IsExecutable(%q) = %v, want %v", test.path, got, test.want)
			}
		})
	}
}

func TestLocate(t *testing.T) {
	binDir := t.TempDir()
	installed := writeExecutable(t, filepath.Join(t.TempDir(), "installed", "sing-box"), "#!/bin/sh\n")
	managedInLayout := writeExecutable(t, filepath.Join(binDir, "1.14.0", ExecutableName(runtime.GOOS)), "#!/bin/sh\n")
	custom := writeExecutable(t, filepath.Join(t.TempDir(), "custom-sing-box"), "#!/bin/sh\n")
	plain := filepath.Join(t.TempDir(), "not-executable")
	if err := os.WriteFile(plain, []byte("data"), 0o600); err != nil {
		t.Fatalf("cannot write %s: %v", plain, err)
	}
	paths := platform.Paths{BinDir: binDir}

	tests := []struct {
		name     string
		managed  ManagedBinaryRef
		settings settings.Settings
		wantPath string
		wantCode apperr.Code
	}{
		{
			name:     "managed uses the recorded path",
			managed:  ManagedBinaryRef{Version: "1.14.0", Path: installed},
			settings: settings.Settings{BinarySource: settings.BinaryManaged},
			wantPath: installed,
		},
		{
			name:     "managed falls back to the version layout",
			managed:  ManagedBinaryRef{Version: "1.14.0", Path: filepath.Join(binDir, "1.14.0", "gone")},
			settings: settings.Settings{BinarySource: settings.BinaryManaged},
			wantPath: managedInLayout,
		},
		{
			name:     "managed with only a version",
			managed:  ManagedBinaryRef{Version: "1.14.0"},
			settings: settings.Settings{BinarySource: settings.BinaryManaged},
			wantPath: managedInLayout,
		},
		{
			name:     "managed with nothing installed",
			managed:  ManagedBinaryRef{},
			settings: settings.Settings{BinarySource: settings.BinaryManaged},
			wantCode: apperr.CodeBinaryNotFound,
		},
		{
			name:     "managed with a missing binary",
			managed:  ManagedBinaryRef{Version: "1.13.0"},
			settings: settings.Settings{BinarySource: settings.BinaryManaged},
			wantCode: apperr.CodeBinaryNotFound,
		},
		{
			name:     "custom absolute path",
			settings: settings.Settings{BinarySource: settings.BinaryCustom, CustomBinaryPath: custom},
			wantPath: custom,
		},
		{
			// A bare name would be resolved through PATH by exec.LookPath, which
			// is exactly the ambiguity the managed/custom split removes (spec §20).
			name:     "custom relative path is refused",
			settings: settings.Settings{BinarySource: settings.BinaryCustom, CustomBinaryPath: "sing-box"},
			wantCode: apperr.CodeBinarySourceInvalid,
		},
		{
			name:     "custom missing executable",
			settings: settings.Settings{BinarySource: settings.BinaryCustom, CustomBinaryPath: filepath.Join(t.TempDir(), "absent")},
			wantCode: apperr.CodeBinaryNotFound,
		},
		{
			name:     "custom non-executable file",
			settings: settings.Settings{BinarySource: settings.BinaryCustom, CustomBinaryPath: plain},
			wantCode: apperr.CodeBinaryNotFound,
		},
		{
			name:     "custom without a path",
			settings: settings.Settings{BinarySource: settings.BinaryCustom},
			wantCode: apperr.CodeBinarySourceInvalid,
		},
		{
			name:     "unknown source",
			settings: settings.Settings{BinarySource: settings.BinarySource("path")},
			wantCode: apperr.CodeBinarySourceInvalid,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Locate(test.managed, test.settings, paths)
			if test.wantCode != "" {
				if err == nil {
					t.Fatalf("Locate() = %q, want an error", got)
				}
				if code := apperr.CodeOf(err); code != test.wantCode {
					t.Errorf("error code = %s, want %s (%v)", code, test.wantCode, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Locate() failed: %v", err)
			}
			if got != test.wantPath {
				t.Errorf("Locate() = %q, want %q", got, test.wantPath)
			}
		})
	}
}

func TestLocateNeverResolvesThroughPath(t *testing.T) {
	// A custom binary named like a PATH entry must not be accepted as-is.
	paths := platform.Paths{BinDir: t.TempDir()}
	_, err := Locate(ManagedBinaryRef{}, settings.Settings{
		BinarySource:     settings.BinaryCustom,
		CustomBinaryPath: "sing-box",
	}, paths)
	if err == nil {
		t.Fatal("Locate() accepted a bare file name, want a refusal")
	}
	if code := apperr.CodeOf(err); code != apperr.CodeBinarySourceInvalid {
		t.Errorf("error code = %s, want %s", code, apperr.CodeBinarySourceInvalid)
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Errorf("error %q does not explain that the path must be absolute", err)
	}
}
