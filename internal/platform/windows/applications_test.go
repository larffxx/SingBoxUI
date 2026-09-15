//go:build windows

package windows

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/larffxx/singboxui/internal/domain/applications"
)

// The listing is what the routing screen offers, and every entry turns into a
// rule the managed sing-box must accept, so the parts that can be wrong without
// a user watching — the condition a program is selected by, the deduplication,
// the exclusions, the sort order and the cache — are covered here. The three
// sources are substituted, so the suite describes a machine instead of
// inspecting the one it runs on (the machine itself is covered by
// TestApplicationsListsThisMachine in applications_machine_test.go).

// fakeMachine builds a catalog whose sources answer with what a test decides.
func fakeMachine(t *testing.T, sources catalogSources) (*Applications, *int) {
	t.Helper()
	calls := 0
	running, installed := sources.running, sources.installed
	sources.running = func() ([]string, error) {
		calls++
		return running()
	}
	sources.installed = func() ([]string, error) {
		calls++
		return installed()
	}
	if sources.describe == nil {
		sources.describe = func(string) string { return "" }
	}
	catalog := &Applications{
		sources:    sources,
		windowsDir: `C:\Windows`,
		ttl:        listingTTL,
		now:        time.Now,
	}
	return catalog, &calls
}

// program writes an executable-shaped file and returns its path. The catalog
// only looks at the name and the suffix, so nothing has to be a real image.
func program(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("MZ"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func listOrFail(t *testing.T, catalog *Applications) []applications.Application {
	t.Helper()
	items, err := catalog.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return items
}

func names(items []applications.Application) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Name)
	}
	return out
}

func TestListSelectsAProgramByItsFileName(t *testing.T) {
	dir := t.TempDir()
	chrome := program(t, filepath.Join(dir, `Google\Chrome\Application`), "chrome.exe")
	steam := program(t, filepath.Join(dir, "Steam"), "steam.exe")
	catalog, _ := fakeMachine(t, catalogSources{
		running:   func() ([]string, error) { return []string{chrome, steam}, nil },
		installed: func() ([]string, error) { return nil, nil },
		describe: func(path string) string {
			if strings.EqualFold(filepath.Base(path), "chrome.exe") {
				return "Google Chrome"
			}
			return ""
		},
	})

	items := listOrFail(t, catalog)

	if len(items) != 2 {
		t.Fatalf("listing = %+v, want two programs", items)
	}
	// Sorted by name, and the name is what the file calls itself when it says so.
	if got := names(items); got[0] != "Google Chrome" || got[1] != "steam" {
		t.Errorf("names = %v, want the description first and the file name second", got)
	}
	for _, item := range items {
		if item.MatchKey != applications.MatchProcessName {
			t.Errorf("%s: MatchKey = %q, want %q", item.Name, item.MatchKey, applications.MatchProcessName)
		}
		if !strings.EqualFold(item.MatchValue, filepath.Base(item.Path)) {
			t.Errorf("%s: MatchValue = %q, want the file name of %q", item.Name, item.MatchValue, item.Path)
		}
		if item.BundleID != "" {
			t.Errorf("%s: BundleID = %q, want empty on Windows", item.Name, item.BundleID)
		}
	}
}

func TestListCollapsesPathsThatProduceTheSameRule(t *testing.T) {
	dir := t.TempDir()
	// One program, many processes and two architectures of one launcher: they all
	// produce the same process_name condition, so they are one row.
	first := program(t, filepath.Join(dir, "Discord", "app-1.0.9180"), "Discord.exe")
	second := program(t, filepath.Join(dir, "Discord", "app-1.0.9257"), "Discord.exe")
	win32 := program(t, filepath.Join(dir, "Epic", "Win32"), "EpicGamesLauncher.exe")
	win64 := program(t, filepath.Join(dir, "Epic", "Win64"), "EpicGamesLauncher.exe")
	catalog, _ := fakeMachine(t, catalogSources{
		running:   func() ([]string, error) { return []string{first, second, win64}, nil },
		installed: func() ([]string, error) { return []string{win32}, nil },
	})

	items := listOrFail(t, catalog)

	if len(items) != 2 {
		t.Fatalf("listing = %+v, want one row per condition", names(items))
	}
	for _, item := range items {
		if item.MatchValue != "Discord.exe" && item.MatchValue != "EpicGamesLauncher.exe" {
			t.Errorf("unexpected condition %q", item.MatchValue)
		}
	}
}

func TestListDropsWhatIsNotAProgramTheUserCouldRoute(t *testing.T) {
	dir := t.TempDir()
	real := program(t, filepath.Join(dir, "Apps"), "real.exe")
	document := filepath.Join(dir, "Apps", "readme.txt")
	if err := os.WriteFile(document, []byte("not a program"), 0o644); err != nil {
		t.Fatalf("write %s: %v", document, err)
	}
	missing := filepath.Join(dir, "Apps", "uninstalled.exe")
	system := program(t, filepath.Join(dir, "WindowsDir"), "svchost.exe")
	installer := program(t, filepath.Join(dir, "Cache"), "msiexec.exe")
	ownData := program(t, filepath.Join(dir, "SingBoxUI", "bin", "sing-box", "1.14.0"), "sing-box.exe")
	catalog, _ := fakeMachine(t, catalogSources{
		running: func() ([]string, error) { return []string{real, document, missing, system}, nil },
		installed: func() ([]string, error) {
			// A shortcut to the installer of a program, to a program of this
			// application's own tree, and entries that hold no path at all.
			return []string{installer, ownData, `%NOPE%`, ""}, nil
		},
	})
	catalog.windowsDir = filepath.Join(dir, "WindowsDir")
	catalog.excluded = []string{filepath.Join(dir, "SingBoxUI")}

	items := listOrFail(t, catalog)

	if len(items) != 1 || !strings.EqualFold(items[0].Path, real) {
		t.Fatalf("listing = %+v, want only %s", names(items), real)
	}
}

func TestListDropsTheWindowsDirectory(t *testing.T) {
	catalog, _ := fakeMachine(t, catalogSources{
		running:   func() ([]string, error) { return []string{`C:\Windows\explorer.exe`}, nil },
		installed: func() ([]string, error) { return nil, nil },
	})
	if items := listOrFail(t, catalog); len(items) != 0 {
		t.Errorf("listing = %+v, want nothing from the Windows directory", names(items))
	}
}

func TestListExpandsTheEnvironmentInShortcutTargets(t *testing.T) {
	dir := t.TempDir()
	program(t, dir, "tool.exe")
	t.Setenv("SINGBOXUI_TEST_TARGET", dir)
	catalog, _ := fakeMachine(t, catalogSources{
		running:   func() ([]string, error) { return nil, nil },
		installed: func() ([]string, error) { return []string{`%SINGBOXUI_TEST_TARGET%\tool.exe`}, nil },
	})

	items := listOrFail(t, catalog)

	if len(items) != 1 || !strings.EqualFold(items[0].Path, filepath.Join(dir, "tool.exe")) {
		t.Fatalf("listing = %+v, want the expanded target", names(items))
	}
}

func TestListReportsAFailingSourceOnlyWhenNothingWasFound(t *testing.T) {
	broken := errors.New("the start menu cannot be read")
	dir := t.TempDir()
	running := program(t, dir, "running.exe")

	catalog, _ := fakeMachine(t, catalogSources{
		running:   func() ([]string, error) { return []string{running}, nil },
		installed: func() ([]string, error) { return nil, broken },
	})
	if items := listOrFail(t, catalog); len(items) != 1 {
		t.Errorf("listing = %+v, want the program that was found", names(items))
	}

	catalog, _ = fakeMachine(t, catalogSources{
		running:   func() ([]string, error) { return nil, broken },
		installed: func() ([]string, error) { return nil, broken },
	})
	if _, err := catalog.List(); err == nil {
		t.Error("List reported success although no source answered")
	}
}

func TestListIsCachedForItsTTL(t *testing.T) {
	dir := t.TempDir()
	running := program(t, dir, "cached.exe")
	catalog, calls := fakeMachine(t, catalogSources{
		running:   func() ([]string, error) { return []string{running}, nil },
		installed: func() ([]string, error) { return nil, nil },
	})
	clock := time.Unix(1_700_000_000, 0)
	catalog.now = func() time.Time { return clock }

	listOrFail(t, catalog)
	listOrFail(t, catalog)
	if *calls != 2 {
		t.Errorf("the sources were asked %d times for two listings, want 2 (one per source)", *calls)
	}

	clock = clock.Add(listingTTL + time.Second)
	listOrFail(t, catalog)
	if *calls != 4 {
		t.Errorf("the sources were asked %d times after the TTL, want 4", *calls)
	}
}

func TestApplicationsSupportsTheListingOnWindows(t *testing.T) {
	catalog := NewApplications(t.TempDir())
	if !catalog.Supported() {
		t.Error("Supported = false on a platform that lists its programs")
	}
	if catalog.UnsupportedReason() != "" {
		t.Errorf("UnsupportedReason = %q, want empty", catalog.UnsupportedReason())
	}
}

func TestWithinDirectoryKnowsAPrefixFromASibling(t *testing.T) {
	for _, tc := range []struct {
		path string
		dir  string
		want bool
	}{
		{`C:\Windows\System32\svchost.exe`, `C:\Windows`, true},
		{`c:\windows\explorer.exe`, `C:\Windows`, true},
		{`C:\Windows`, `C:\Windows`, true},
		{`C:\WindowsApps\app.exe`, `C:\Windows`, false},
		{`C:\Users\someone\app.exe`, ``, false},
	} {
		if got := withinDirectory(tc.path, tc.dir); got != tc.want {
			t.Errorf("withinDirectory(%q, %q) = %v, want %v", tc.path, tc.dir, got, tc.want)
		}
	}
}
