//go:build darwin

package darwin

import (
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/larffxx/singboxui/internal/domain/apperr"
)

// newLoginItem returns a login item whose plist lives in a temporary directory:
// the tests must never write into the real ~/Library/LaunchAgents of whoever runs
// them. executable stands for this application, the way NewAutostart resolves it.
func newLoginItem(t *testing.T, executable string) *Autostart {
	t.Helper()
	dir := t.TempDir()
	return &Autostart{
		executable: executable,
		plistPath:  filepath.Join(dir, "LaunchAgents", AgentLabel+".plist"),
	}
}

func TestDataDirIsTheApplicationSupportDirectory(t *testing.T) {
	dir, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir() = %v", err)
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("DataDir() = %q, want an absolute path", dir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() = %v", err)
	}
	want := filepath.Join(home, "Library", "Application Support", AppName)
	if dir != want {
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
	if HelperExecutable != "singboxui-priv" {
		t.Errorf("HelperExecutable = %q, want %q", HelperExecutable, "singboxui-priv")
	}
	beside := filepath.Join(filepath.Dir(executable), HelperExecutable)

	// Nothing is installed next to a test binary, so the first thing to check is
	// that a missing helper is reported as such instead of a path that would only
	// fail when it is launched.
	if err := os.Remove(beside); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Remove(%q) = %v", beside, err)
	}
	if _, err := HelperPath(); !apperr.IsCode(err, apperr.CodeBinaryNotFound) {
		t.Fatalf("HelperPath() without the helper = %v, want %v", err, apperr.CodeBinaryNotFound)
	}

	if err := os.WriteFile(beside, []byte("#!/bin/sh\n"), 0o755); err != nil {
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
	if base := filepath.Base(path); base != HelperExecutable {
		t.Errorf("HelperPath() = %q, want the file name %q", path, HelperExecutable)
	}

	// A directory called singboxui-priv is not the helper either.
	if err := os.Remove(beside); err != nil {
		t.Fatalf("Remove(%q) = %v", beside, err)
	}
	if err := os.Mkdir(beside, 0o755); err != nil {
		t.Fatalf("Mkdir(%q) = %v", beside, err)
	}
	if _, err := HelperPath(); !apperr.IsCode(err, apperr.CodeBinaryNotFound) {
		t.Errorf("HelperPath() on a directory = %v, want %v", err, apperr.CodeBinaryNotFound)
	}
}

func TestNewAutostartPointsAtTheLaunchAgentOfThePrototype(t *testing.T) {
	item, err := NewAutostart()
	if err != nil {
		t.Fatalf("NewAutostart() = %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() = %v", err)
	}
	want := filepath.Join(home, "Library", "LaunchAgents", "com.larffxx.singboxui.plist")
	if item.Path() != want {
		t.Errorf("Path() = %q, want %q", item.Path(), want)
	}
	// The label and the file name are what the Java entry used (migration plan
	// §1): a different one would leave the user with two login items.
	if AgentLabel != "com.larffxx.singboxui" {
		t.Errorf("AgentLabel = %q, want %q", AgentLabel, "com.larffxx.singboxui")
	}
	if item.Label() != AgentLabel {
		t.Errorf("Label() = %q, want %q", item.Label(), AgentLabel)
	}
	if !item.Supported() {
		t.Error("Supported() = false, want true on macOS")
	}
}

func TestEnableWritesALoginItemThatStartsTheApplication(t *testing.T) {
	const application = "/Applications/SingBoxUI.app/Contents/MacOS/SingBoxUI"
	item := newLoginItem(t, application)

	if enabled, err := item.Enabled(); err != nil || enabled {
		t.Fatalf("Enabled() = %v, %v, want false, nil", enabled, err)
	}
	if entry := item.LegacyEntry(); entry != "" {
		t.Errorf("LegacyEntry() = %q, want empty without a login item", entry)
	}

	if err := item.Enable(application); err != nil {
		t.Fatalf("Enable() = %v", err)
	}
	enabled, err := item.Enabled()
	if err != nil || !enabled {
		t.Fatalf("Enabled() = %v, %v, want true, nil", enabled, err)
	}

	body, err := os.ReadFile(item.Path())
	if err != nil {
		t.Fatalf("ReadFile(%q) = %v", item.Path(), err)
	}
	if target, ok := programArgument(string(body)); !ok || target != application {
		t.Errorf("the login item starts %q (read %v), want %q", target, ok, application)
	}
	for _, want := range []string{
		"<key>Label</key>",
		AgentLabel,
		"<key>ProgramArguments</key>",
		"<key>RunAtLoad</key>",
		"<true/>",
		"<key>LimitLoadToSessionType</key>",
		"<string>Aqua</string>",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the login item does not contain %s", want)
		}
	}
	if strings.Contains(string(body), "KeepAlive</key>\n\t<true/>") {
		t.Error("the login item asks launchd to keep a killed application alive")
	}

	// An entry this application wrote is not a leftover of the prototype.
	if entry := item.LegacyEntry(); entry != "" {
		t.Errorf("LegacyEntry() = %q, want empty for this application's own entry", entry)
	}
	// Removing "the legacy entry" must never delete the entry in use.
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
	// Disabling twice is not an error: the entry is simply not there.
	if err := item.Disable(); err != nil {
		t.Errorf("Disable() on a missing login item = %v", err)
	}
}

func TestEnableEscapesThePathIntoWellFormedXML(t *testing.T) {
	// A path that would end the XML text if it were written as it is.
	application := filepath.Join(t.TempDir(), `Sing & <Box> "UI"`)
	item := newLoginItem(t, application)

	if err := item.Enable(application); err != nil {
		t.Fatalf("Enable(%q) = %v", application, err)
	}
	body, err := os.ReadFile(item.Path())
	if err != nil {
		t.Fatalf("ReadFile(%q) = %v", item.Path(), err)
	}
	var document struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal(body, &document); err != nil {
		t.Fatalf("the login item is not well-formed XML: %v\n%s", err, body)
	}
	// The escaped text has to read back as the path that was written.
	if target, ok := programArgument(string(body)); !ok || target != application {
		t.Errorf("the login item starts %q (read %v), want %q", target, ok, application)
	}
	if entry := item.LegacyEntry(); entry != "" {
		t.Errorf("LegacyEntry() = %q, want empty after Enable", entry)
	}
}

func TestEnableRefusesAProgramItCannotWriteIntoTheLoginItem(t *testing.T) {
	for _, tc := range []struct {
		name       string
		executable string
	}{
		{"nothing", ""},
		{"a relative path", filepath.Join("Applications", "SingBoxUI")},
		{"a path that is not clean", "/Applications/../Applications/SingBoxUI"},
		{"a path with a newline", "/Applications/SingBoxUI\nanother command"},
		{"a path with a control character", "/Applications/Sing\x00BoxUI"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := newLoginItem(t, tc.executable)
			err := item.Enable(tc.executable)
			if err == nil {
				t.Fatalf("Enable(%q) = nil, want an error", tc.executable)
			}
			if !apperr.IsCode(err, apperr.CodeInvalidArgument) {
				t.Errorf("Enable(%q) = %v, want %v", tc.executable, err, apperr.CodeInvalidArgument)
			}
			// A refused request must not leave a login item behind.
			if _, err := os.Stat(item.Path()); err == nil {
				t.Errorf("Enable(%q) wrote %q anyway", tc.executable, item.Path())
			}
		})
	}
}

func TestLegacyEntryDescribesTheLoginItemLeftByThePrototype(t *testing.T) {
	const application = "/Applications/SingBoxUI.app/Contents/MacOS/SingBoxUI"

	t.Run("the java entry is reported and removed on request", func(t *testing.T) {
		item := newLoginItem(t, application)
		if err := os.MkdirAll(filepath.Dir(item.Path()), 0o755); err != nil {
			t.Fatalf("MkdirAll() = %v", err)
		}
		prototype := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.larffxx.singboxui</string>
	<key>ProgramArguments</key>
	<array>
		<string>/usr/bin/java</string>
		<string>-jar</string>
		<string>/opt/SingBoxUI/SingBoxUI.jar</string>
	</array>
</dict>
</plist>
`
		if err := os.WriteFile(item.Path(), []byte(prototype), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) = %v", item.Path(), err)
		}
		entry := item.LegacyEntry()
		if entry == "" {
			t.Fatal("LegacyEntry() = \"\", want the java entry to be reported")
		}
		for _, want := range []string{item.Path(), "/usr/bin/java"} {
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
		if entry := item.LegacyEntry(); entry != "" {
			t.Errorf("LegacyEntry() = %q, want empty once the file is gone", entry)
		}
		// With nothing left, removing again does nothing and is not an error.
		if err := item.RemoveLegacy(); err != nil {
			t.Errorf("RemoveLegacy() with no legacy entry = %v", err)
		}
	})

	t.Run("an entry that starts this application is not legacy", func(t *testing.T) {
		item := newLoginItem(t, "/usr/bin/java")
		if err := item.Enable("/usr/bin/java"); err != nil {
			t.Fatalf("Enable() = %v", err)
		}
		// The entry names the same program this application resolved for
		// itself, so it is its own entry and not a leftover.
		if entry := item.LegacyEntry(); entry != "" {
			t.Errorf("LegacyEntry() = %q, want empty for the entry of this very program", entry)
		}
		if err := item.RemoveLegacy(); err != nil {
			t.Fatalf("RemoveLegacy() = %v", err)
		}
		if enabled, err := item.Enabled(); err != nil || !enabled {
			t.Errorf("Enabled() = %v, %v, want the entry of this program to survive", enabled, err)
		}
	})

	t.Run("the same program through a symlink is not legacy", func(t *testing.T) {
		real := filepath.Join(t.TempDir(), "SingBoxUI")
		if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatalf("WriteFile(%q) = %v", real, err)
		}
		link := filepath.Join(t.TempDir(), "SingBoxUI.app")
		if err := os.Symlink(real, link); err != nil {
			t.Fatalf("Symlink(%q, %q) = %v", real, link, err)
		}
		// The login item names the executable, this process was started through
		// the link: still the same program.
		item := newLoginItem(t, link)
		if err := item.Enable(real); err != nil {
			t.Fatalf("Enable(%q) = %v", real, err)
		}
		if entry := item.LegacyEntry(); entry != "" {
			t.Errorf("LegacyEntry() = %q, want empty for the same program through a link", entry)
		}
	})

	t.Run("a document without a program argument is legacy", func(t *testing.T) {
		item := newLoginItem(t, application)
		if err := os.MkdirAll(filepath.Dir(item.Path()), 0o755); err != nil {
			t.Fatalf("MkdirAll() = %v", err)
		}
		if err := os.WriteFile(item.Path(), []byte("<plist><dict></dict></plist>"), 0o644); err != nil {
			t.Fatalf("WriteFile() = %v", err)
		}
		// Reporting an unreadable entry is always the safe side: ignoring it
		// would leave a second login item in place.
		if entry := item.LegacyEntry(); entry == "" {
			t.Error("LegacyEntry() = \"\", want an unrecognised entry to be reported")
		}
	})

	t.Run("an entry that is not a file is not an entry", func(t *testing.T) {
		item := newLoginItem(t, application)
		if err := os.MkdirAll(item.Path(), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) = %v", item.Path(), err)
		}
		if entry := item.LegacyEntry(); entry != "" {
			t.Errorf("LegacyEntry() = %q, want empty when the entry cannot be read", entry)
		}
	})
}

func TestProgramArgumentReadsTheProgramOfTheArray(t *testing.T) {
	for _, tc := range []struct {
		name     string
		document string
		want     string
		ok       bool
	}{
		{"no program arguments", "<plist><dict><key>Label</key></dict></plist>", "", false},
		{"the key without a string", "<key>ProgramArguments</key><array></array>", "", false},
		{
			"the first string of the array",
			"<key>ProgramArguments</key><array><string>/usr/bin/java</string><string>-jar</string></array>",
			"/usr/bin/java",
			true,
		},
		{
			"a string surrounded by whitespace",
			"<key>ProgramArguments</key><array><string>\n\t/usr/bin/java\n</string></array>",
			"/usr/bin/java",
			true,
		},
		{"a string that is never closed", "<key>ProgramArguments</key><array><string>", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := programArgument(tc.document)
			if ok != tc.ok || got != tc.want {
				t.Errorf("programArgument(%q) = %q, %v, want %q, %v", tc.document, got, ok, tc.want, tc.ok)
			}
		})
	}
}
