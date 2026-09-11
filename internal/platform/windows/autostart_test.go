//go:build windows

package windows

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func TestDataDirIsTheLocalApplicationDataDirectory(t *testing.T) {
	dir, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir() = %v", err)
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("DataDir() = %q, want an absolute path", dir)
	}
	base, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_DEFAULT)
	if err != nil || base == "" {
		base = strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	}
	if want := filepath.Join(base, AppName); dir != want {
		t.Errorf("DataDir() = %q, want %q", dir, want)
	}
	// The directory the Java prototype used (internal-contracts.md §2, migration
	// plan §1): the application name must not change, or the data is orphaned.
	if AppName != "SingBoxUI" {
		t.Errorf("AppName = %q, want %q", AppName, "SingBoxUI")
	}
}

func TestHelperPathIsTheHelperBesideTheApplication(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("Executable() = %v", err)
	}
	// The name is the contract with the packaging step (internal-contracts.md §3).
	if HelperExecutable != "singboxui-priv.exe" {
		t.Errorf("HelperExecutable = %q, want %q", HelperExecutable, "singboxui-priv.exe")
	}
	beside := filepath.Join(filepath.Dir(executable), HelperExecutable)

	if err := os.Remove(beside); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Remove(%q) = %v", beside, err)
	}
	if _, err := HelperPath(); !apperr.IsCode(err, apperr.CodeBinaryNotFound) {
		t.Fatalf("HelperPath() without the helper = %v, want %v", err, apperr.CodeBinaryNotFound)
	}

	if err := os.WriteFile(beside, []byte("MZ"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) = %v", beside, err)
	}
	t.Cleanup(func() { _ = os.Remove(beside) })
	path, err := HelperPath()
	if err != nil {
		t.Fatalf("HelperPath() = %v", err)
	}
	if path != beside {
		t.Errorf("HelperPath() = %q, want %q", path, beside)
	}
}

func TestRunKeyIsTheCurrentUserRunKeyOfThePrototype(t *testing.T) {
	// HKCU\Software\Microsoft\Windows\CurrentVersion\Run\SingBoxUI, the value the
	// Java prototype used (migration plan §1).
	if runKeyPath != `Software\Microsoft\Windows\CurrentVersion\Run` {
		t.Errorf("runKeyPath = %q, want %q", runKeyPath, `Software\Microsoft\Windows\CurrentVersion\Run`)
	}
	if valueName != "SingBoxUI" {
		t.Errorf("valueName = %q, want %q", valueName, "SingBoxUI")
	}
	item := &Autostart{}
	if item.ValueName() != valueName {
		t.Errorf("ValueName() = %q, want %q", item.ValueName(), valueName)
	}
	if !item.Supported() {
		t.Error("Supported() = false, want true on Windows")
	}
	// The name of an instance is the one it was given, so a test never has to
	// touch the login item of the machine it runs on.
	named := &Autostart{value: "SingBoxUI.another"}
	if named.ValueName() != "SingBoxUI.another" {
		t.Errorf("ValueName() = %q, want the name the item was given", named.ValueName())
	}
}

func TestQuotingTheLoginItemTargetRoundTripsThroughItsProgram(t *testing.T) {
	const (
		installed = `C:\Program Files\SingBoxUI\SingBoxUI.exe`
		prototype = `"C:\Program Files\Java\jre\bin\javaw.exe" -jar C:\SingBoxUI\SingBoxUI.jar`
	)
	quoted := quote(installed)
	if quoted != `"C:\Program Files\SingBoxUI\SingBoxUI.exe"` {
		t.Errorf("quote(%q) = %s, want the path in quotes", installed, quoted)
	}
	for _, tc := range []struct{ name, value, want string }{
		{"the application itself", quoted, installed},
		{"the prototype, which also passes arguments", prototype, `C:\Program Files\Java\jre\bin\javaw.exe`},
		{"a value without quotes", `C:\SingBoxUI\SingBoxUI.exe --tray`, `C:\SingBoxUI\SingBoxUI.exe`},
		{"a value with an unquoted space", `C:\Program Files\SingBoxUI.exe`, `C:\Program`},
		{"a value that is only quotes", `""`, ""},
		{"an unterminated quote", `"C:\SingBoxUI`, `C:\SingBoxUI`},
		{"nothing", "", ""},
		{"only whitespace", "  \t ", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := targetProgram(tc.value); got != tc.want {
				t.Errorf("targetProgram(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
	// A program that is written into the Run value reads back as the program it
	// is: this is what tells our own entry from the prototype's.
	if got := targetProgram(quote(installed)); !sameExecutable(got, installed) {
		t.Errorf("targetProgram(quote(%q)) = %q, want the same executable", installed, got)
	}
	if !sameExecutable(installed, `C:\Program Files\SingBoxUI\singboxui.EXE`) {
		t.Error("sameExecutable() = false, want Windows paths to compare case-insensitively")
	}
}

func TestEnableRefusesAProgramItCannotWriteIntoTheRegistry(t *testing.T) {
	item := &Autostart{value: "SingBoxUI.refused"}
	for _, tc := range []struct {
		name       string
		executable string
	}{
		{"nothing", ""},
		{"a relative path", filepath.Join("SingBoxUI", "SingBoxUI.exe")},
		{"a path that is not clean", `C:\SingBoxUI\..\SingBoxUI\SingBoxUI.exe`},
		{"a path with a newline", "C:\\SingBoxUI\\SingBoxUI.exe\nanother command"},
		{"a path with a control character", "C:\\SingBoxUI\x00\\SingBoxUI.exe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := item.Enable(tc.executable)
			if err == nil {
				t.Fatalf("Enable(%q) = nil, want an error", tc.executable)
			}
			if !apperr.IsCode(err, apperr.CodeInvalidArgument) {
				t.Errorf("Enable(%q) = %v, want %v", tc.executable, err, apperr.CodeInvalidArgument)
			}
			// A refused request must not have written anything.
			if enabled, err := item.Enabled(); err != nil || enabled {
				t.Errorf("Enabled() = %v, %v after a refused Enable, want false, nil", enabled, err)
			}
		})
	}
}

func TestLegacyEntryDescribesTheValueLeftByThePrototype(t *testing.T) {
	const installed = `C:\Program Files\SingBoxUI\SingBoxUI.exe`
	// A value name of this test's own, so the login item the machine really uses
	// is never read or written. It is removed again in any case.
	name := "SingBoxUI.test." + strconv.Itoa(os.Getpid())
	item := &Autostart{executable: installed, value: name}
	writeValue(t, name, `"C:\Program Files\Java\jre\bin\javaw.exe" -jar C:\SingBoxUI\SingBoxUI.jar`)
	t.Cleanup(func() { _ = item.Disable() })

	entry := item.LegacyEntry()
	if entry == "" {
		t.Fatal("LegacyEntry() = \"\", want the javaw value to be reported")
	}
	for _, want := range []string{"HKCU\\" + runKeyPath, name, "javaw.exe"} {
		if !strings.Contains(entry, want) {
			t.Errorf("LegacyEntry() = %q, want it to mention %q", entry, want)
		}
	}
	if err := item.RemoveLegacy(); err != nil {
		t.Fatalf("RemoveLegacy() = %v", err)
	}
	if enabled, err := item.Enabled(); err != nil || enabled {
		t.Errorf("Enabled() after RemoveLegacy() = %v, %v, want false, nil", enabled, err)
	}
	// With nothing left, removing again does nothing and is not an error.
	if err := item.RemoveLegacy(); err != nil {
		t.Errorf("RemoveLegacy() with no legacy value = %v", err)
	}
}

func TestLoginItemLifecycle(t *testing.T) {
	const installed = `C:\Program Files\SingBoxUI\SingBoxUI.exe`
	// A value name of this test's own, so the login item of the machine is left
	// exactly as it was found.
	name := "SingBoxUI.test." + strconv.Itoa(os.Getpid())
	item := &Autostart{executable: installed, value: name}
	t.Cleanup(func() { _ = item.Disable() })

	if enabled, err := item.Enabled(); err != nil || enabled {
		t.Fatalf("Enabled() = %v, %v, want false, nil before the value is written", enabled, err)
	}
	if entry := item.LegacyEntry(); entry != "" {
		t.Errorf("LegacyEntry() = %q, want empty without a value", entry)
	}

	if err := item.Enable(installed); err != nil {
		t.Fatalf("Enable(%q) = %v", installed, err)
	}
	enabled, err := item.Enabled()
	if err != nil || !enabled {
		t.Fatalf("Enabled() = %v, %v, want true, nil", enabled, err)
	}
	if value, ok, err := item.current(); err != nil || !ok || value != quote(installed) {
		t.Errorf("the login item is %q, %v, %v, want %q", value, ok, err, quote(installed))
	}
	// The value this application wrote is its own entry, not a leftover.
	if entry := item.LegacyEntry(); entry != "" {
		t.Errorf("LegacyEntry() = %q, want empty for this application's own value", entry)
	}
	// Removing "the legacy entry" must never delete the value in use.
	if err := item.RemoveLegacy(); err != nil {
		t.Fatalf("RemoveLegacy() = %v", err)
	}
	if enabled, err := item.Enabled(); err != nil || !enabled {
		t.Errorf("Enabled() after RemoveLegacy() = %v, %v, want true, nil", enabled, err)
	}

	if err := item.Disable(); err != nil {
		t.Fatalf("Disable() = %v", err)
	}
	if enabled, err := item.Enabled(); err != nil || enabled {
		t.Errorf("Enabled() after Disable() = %v, %v, want false, nil", enabled, err)
	}
	// Disabling twice is not an error: the value is simply not there.
	if err := item.Disable(); err != nil {
		t.Errorf("Disable() on a missing login item = %v", err)
	}
}

// writeValue writes a login item value directly, the way the Java prototype left
// one behind.
func writeValue(t *testing.T, name, value string) {
	t.Helper()
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		t.Fatalf("CreateKey(%q) = %v", runKeyPath, err)
	}
	defer key.Close()
	if err := key.SetStringValue(name, value); err != nil {
		t.Fatalf("SetStringValue(%q) = %v", name, err)
	}
}
