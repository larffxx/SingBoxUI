//go:build darwin

package darwin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/larffxx/singboxui/internal/domain/applications"
)

// The listing is what the routing screen offers, and every entry turns into a
// rule the managed sing-box must accept, so the parts that can be wrong without
// a Mac in the loop — path resolution, the generated expression, the sort order,
// the cache — are covered here. The bundles are ordinary directories whose
// Info.plist is JSON, which is a plist format plutil reads as well, so the real
// reader is exercised rather than a stand-in.

// regexpFor compiles an expression the way sing-box does, so a broken
// expression fails here instead of at `sing-box check` time.
func regexpFor(t *testing.T, expression string) *regexp.Regexp {
	t.Helper()
	compiled, err := regexp.Compile(expression)
	if err != nil {
		t.Fatalf("the generated expression %q does not compile: %v", expression, err)
	}
	return compiled
}

// testClock is a manual clock: the cache of the listing is part of what the
// routing screen relies on, so it is covered without sleeping.
type testClock struct {
	at time.Time
}

func newClock() *testClock { return &testClock{at: time.Unix(1_700_000_000, 0)} }

func (c *testClock) now() time.Time { return c.at }

func (c *testClock) advance(d time.Duration) { c.at = c.at.Add(d) }

// fakeBundle writes a bundle whose Info.plist describes it, and returns its path.
func fakeBundle(t *testing.T, root, name string, info map[string]string) string {
	t.Helper()
	bundle := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(bundle, "Contents"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle, plistPath), raw, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return bundle
}

// brokenBundle writes a bundle whose Info.plist cannot be read at all.
func brokenBundle(t *testing.T, root, name string) {
	t.Helper()
	bundle := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(bundle, "Contents"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle, plistPath), []byte("this is not a plist"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestBundlePathRegexSelectsTheWholeBundle(t *testing.T) {
	for _, tc := range []struct {
		name       string
		path       string
		want       string
		alsoMatch  []string
		wantMissed []string
	}{
		{
			name: "the dot of .app is quoted and helpers are inside the bundle",
			path: "/Applications/Telegram.app",
			want: `^/Applications/Telegram\.app/`,
			alsoMatch: []string{
				"/Applications/Telegram.app/Contents/MacOS/Telegram",
				"/Applications/Telegram.app/Contents/Frameworks/Telegram Helper.app/Contents/MacOS/Telegram Helper",
			},
			wantMissed: []string{
				"/Applications/Telegram.app.backup/Contents/MacOS/Telegram",
				"/Applications/OtherTelegram.app/Contents/MacOS/Other",
				"/Applications/TelegramXapp/x",
			},
		},
		{
			name:      "a trailing slash is not doubled",
			path:      "/Applications/Test.app/",
			want:      `^/Applications/Test\.app/`,
			alsoMatch: []string{"/Applications/Test.app/Contents/MacOS/Test"},
		},
		{
			name:      "spaces and version numbers of a bundle name survive",
			path:      "/Applications/Xcode 16.2.app",
			want:      `^/Applications/Xcode 16\.2\.app/`,
			alsoMatch: []string{"/Applications/Xcode 16.2.app/Contents/MacOS/Xcode"},
		},
		{
			name:      "a resolved path stays literal",
			path:      "/private/var/folders/Test.app",
			want:      `^/private/var/folders/Test\.app/`,
			alsoMatch: []string{"/private/var/folders/Test.app/Contents/MacOS/Test"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := BundlePathRegex(tc.path)
			if got != tc.want {
				t.Fatalf("BundlePathRegex(%q) = %q, want %q", tc.path, got, tc.want)
			}
			re := regexpFor(t, got)
			for _, path := range tc.alsoMatch {
				if !re.MatchString(path) {
					t.Errorf("%q does not match %q", got, path)
				}
			}
			for _, path := range tc.wantMissed {
				if re.MatchString(path) {
					t.Errorf("%q unexpectedly matches %q", got, path)
				}
			}
		})
	}
}

func TestListApplicationsDescribesEveryBundle(t *testing.T) {
	root := t.TempDir()
	fakeBundle(t, root, "Telegram.app", map[string]string{
		"CFBundleDisplayName": "Telegram",
		"CFBundleIdentifier":  "ru.keepcoder.Telegram",
		"CFBundleExecutable":  "Telegram",
	})
	// A bundle without a display name falls back to CFBundleName, and one with
	// neither falls back to its directory name.
	fakeBundle(t, root, "Firefox.app", map[string]string{
		"CFBundleName":       "Firefox",
		"CFBundleIdentifier": "org.mozilla.firefox",
		"CFBundleExecutable": "firefox",
	})
	fakeBundle(t, root, "Anonymous.app", map[string]string{"CFBundleExecutable": "Anonymous"})
	// Neither of these is offered: a directory that is not a bundle, and a
	// bundle whose description cannot be read.
	if err := os.MkdirAll(filepath.Join(root, "NotABundle"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	brokenBundle(t, root, "Broken.app")

	got, err := listApplications([]string{root, filepath.Join(root, "missing")}, plistToJSON)
	if err != nil {
		t.Fatalf("listApplications: %v", err)
	}
	names := make([]string, 0, len(got))
	for _, item := range got {
		names = append(names, item.Name)
	}
	want := []string{"Anonymous", "Firefox", "Telegram"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("names = %v, want %v (sorted by name)", names, want)
	}
	first := got[0]
	if first.BundleID != "" || first.Executable != "Anonymous" {
		t.Errorf("entry without an identifier = %+v, want the executable only", first)
	}
	if !strings.HasSuffix(first.Path, "/Anonymous.app") {
		t.Errorf("Path = %q, want an absolute bundle path", first.Path)
	}
	if first.ProcessPathRegex != BundlePathRegex(first.Path) {
		t.Errorf("ProcessPathRegex = %q, want the expression of %q", first.ProcessPathRegex, first.Path)
	}
	for _, item := range got {
		if strings.Contains(item.Name, "Broken") || strings.Contains(item.Name, "NotABundle") {
			t.Errorf("%q is not a readable bundle but was listed", item.Name)
		}
	}
}

func TestListApplicationsResolvesSymlinkedBundles(t *testing.T) {
	// sing-box reports the real path of a process, so a rule written against a
	// symlinked location never matches: the listing resolves the link first.
	root := t.TempDir()
	elsewhere := t.TempDir()
	bundle := fakeBundle(t, elsewhere, "Real.app", map[string]string{
		"CFBundleName":       "Real",
		"CFBundleExecutable": "Real",
	})
	link := filepath.Join(root, "Linked.app")
	if err := os.Symlink(bundle, link); err != nil {
		t.Skipf("symlinks are unavailable here: %v", err)
	}

	got, err := listApplications([]string{root}, plistToJSON)
	if err != nil {
		t.Fatalf("listApplications: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("listed %d applications, want 1: %+v", len(got), got)
	}
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if got[0].Path != resolved {
		t.Errorf("Path = %q, want the resolved %q", got[0].Path, resolved)
	}
	if got[0].Path == link {
		t.Error("Path is the symlink, which no process path will ever match")
	}
}

func TestListApplicationsKeepsOneEntryPerBundle(t *testing.T) {
	// The same bundle can be reachable through more than one root; the screen
	// offers one row per program, not one per location.
	root := t.TempDir()
	fakeBundle(t, root, "Twice.app", map[string]string{"CFBundleName": "Twice", "CFBundleExecutable": "Twice"})

	got, err := listApplications([]string{root, root}, plistToJSON)
	if err != nil {
		t.Fatalf("listApplications: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("listed %d applications, want 1: %+v", len(got), got)
	}
}

func TestListApplicationsSortsByNameThenPath(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	for _, root := range []string{first, second} {
		fakeBundle(t, root, "Same.app", map[string]string{"CFBundleName": "Same", "CFBundleExecutable": "Same"})
	}
	got, err := listApplications([]string{first, second}, plistToJSON)
	if err != nil {
		t.Fatalf("listApplications: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("listed %d applications, want 2", len(got))
	}
	if got[0].Name != got[1].Name {
		t.Fatalf("names = %q and %q, want the same name", got[0].Name, got[1].Name)
	}
	if got[0].Path > got[1].Path {
		t.Errorf("paths %q and %q are not sorted", got[0].Path, got[1].Path)
	}
}

func TestCatalogCachesAndReloadsTheListing(t *testing.T) {
	root := t.TempDir()
	fakeBundle(t, root, "Cached.app", map[string]string{"CFBundleName": "Cached", "CFBundleExecutable": "Cached"})
	clock := newClock()
	catalog := &Applications{roots: []string{root}, ttl: listingTTL, now: clock.now, plistJSON: plistToJSON}

	first, err := catalog.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("listed %d applications, want 1", len(first))
	}
	fakeBundle(t, root, "Later.app", map[string]string{"CFBundleName": "Later", "CFBundleExecutable": "Later"})
	again, err := catalog.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(again) != 1 {
		t.Errorf("the second call re-read the disk: %d applications", len(again))
	}
	clock.advance(listingTTL)
	third, err := catalog.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(third) != 2 {
		t.Errorf("after the TTL the listing still has %d applications, want 2", len(third))
	}
	if !sort.SliceIsSorted(third, func(i, j int) bool { return third[i].Name < third[j].Name }) {
		t.Errorf("the listing is not sorted by name: %+v", third)
	}
}

func TestApplicationsSayTheyAreSupported(t *testing.T) {
	catalog := NewApplications()
	if !catalog.Supported() {
		t.Error("macOS reports that it cannot list applications")
	}
	if catalog.UnsupportedReason() != "" {
		t.Errorf("a supported catalog explains why it is unsupported: %q", catalog.UnsupportedReason())
	}
	roots := DefaultApplicationRoots()
	if len(roots) == 0 {
		t.Fatal("no application directory is scanned")
	}
	for _, root := range roots {
		if !filepath.IsAbs(root) {
			t.Errorf("application root %q is not absolute", root)
		}
	}
}

// TestApplicationsListsThisMac keeps the whole path honest on a Mac: the real
// plutil, the real bundle layout, the real /Applications.
func TestApplicationsListsThisMac(t *testing.T) {
	catalog := NewApplications()
	listed, err := catalog.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) == 0 {
		t.Fatal("no application was found on a Mac that has /Applications")
	}
	for _, item := range listed {
		if item.Name == "" || item.Path == "" || item.ProcessPathRegex == "" {
			t.Fatalf("incomplete entry: %+v", item)
		}
		if !strings.HasSuffix(item.Path, bundleSuffix) {
			t.Errorf("Path %q is not a bundle", item.Path)
		}
		if !strings.HasPrefix(item.ProcessPathRegex, "^") {
			t.Errorf("ProcessPathRegex %q is not anchored", item.ProcessPathRegex)
		}
		// The expression must match the executable path of the bundle it was
		// derived from, which is what the tested /private resolution protects.
		re := regexpFor(t, item.ProcessPathRegex)
		if item.Executable != "" && !re.MatchString(filepath.Join(item.Path, "Contents", "MacOS", item.Executable)) {
			t.Errorf("%q does not match the executable of %q", item.ProcessPathRegex, item.Path)
		}
	}
	// Every application the user can see must survive a round-trip through the
	// payload the frontend receives.
	raw, err := json.Marshal(applications.Application{Name: listed[0].Name, ProcessPathRegex: listed[0].ProcessPathRegex})
	if err != nil || !strings.Contains(string(raw), "processPathRegex") {
		t.Fatalf("the entry does not serialise: %s (%v)", raw, err)
	}
}
