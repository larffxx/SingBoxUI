//go:build darwin

package darwin

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/applications"
)

// opApplications is this file's operation as it appears in typed errors.
const opApplications = "platform.darwin.Applications.List"

const (
	// listingTTL bounds how long a directory listing is reused. Opening the
	// routing screen twice in a row must not re-read every bundle on the disk,
	// and an application installed in between shows up after this window.
	listingTTL = 2 * time.Minute
	// listingWorkers bounds how many bundles are described at once. Every bundle
	// costs one plutil run, so the listing stays responsive without spawning one
	// process per installed application at the same time.
	listingWorkers = 8
	// bundleSuffix is the directory suffix of an application bundle.
	bundleSuffix = ".app"
	// plistPath is where a bundle keeps its description.
	plistPath = "Contents/Info.plist"
	// plutilPath converts a plist to JSON. It reads both the XML and the binary
	// format, which is what makes it the right reader here: most bundles ship
	// Info.plist as a binary plist, and a hand-written XML parser would refuse
	// them.
	plutilPath = "/usr/bin/plutil"
)

// DefaultApplicationRoots are the directories macOS installs bundles into: the
// two system locations, their Utilities folders, and the user's own
// ~/Applications. Only the first level is scanned, which is where bundles are
// installed; a bundle nested deeper keeps its rule from the application screen
// only after it is listed by hand.
func DefaultApplicationRoots() []string {
	roots := []string{
		"/Applications",
		"/Applications/Utilities",
		"/System/Applications",
		"/System/Applications/Utilities",
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots, filepath.Join(home, "Applications"))
	}
	return roots
}

// BundlePathRegex is the process_path_regex that selects every executable inside
// a bundle (ADR 011).
//
// The expression is anchored at the bundle path and every metacharacter of that
// path is quoted, so a bundle called "Xcode 16.2.app" cannot turn into a
// different match. sing-box matches the expression against the real path of the
// process, helpers included, which is why the trailing slash is required: a
// bundle path is a prefix of its own contents and never of a sibling.
func BundlePathRegex(bundlePath string) string {
	return "^" + regexp.QuoteMeta(strings.TrimSuffix(bundlePath, "/")) + "/"
}

// Applications is the macOS application catalog: the bundles installed on this
// machine, described so that a routing rule can select one by name.
type Applications struct {
	roots []string
	ttl   time.Duration
	now   func() time.Time
	// plistJSON renders an Info.plist as JSON. It is a seam for tests: the real
	// implementation runs plutil, and a test does not need macOS bundles to
	// cover the listing.
	plistJSON func(path string) ([]byte, error)

	mu     sync.Mutex
	loaded bool
	at     time.Time
	items  []applications.Application
}

// NewApplications returns the catalog of the bundles installed on this Mac.
func NewApplications() *Applications {
	return &Applications{
		roots:     DefaultApplicationRoots(),
		ttl:       listingTTL,
		now:       time.Now,
		plistJSON: plistToJSON,
	}
}

// Supported reports that macOS can list its applications.
func (a *Applications) Supported() bool { return true }

// UnsupportedReason is empty: this platform supports the listing.
func (a *Applications) UnsupportedReason() string { return "" }

// List returns the installed applications, sorted by name. The result is
// cached for listingTTL; a listing that fails is not cached, so a transient
// failure does not hide the applications for the next two minutes.
func (a *Applications) List() ([]applications.Application, error) {
	const op = opApplications
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.loaded && a.now().Sub(a.at) < a.ttl {
		return a.items, nil
	}
	items, err := listApplications(a.roots, a.plistJSON)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, op, "the installed applications cannot be listed", err)
	}
	a.loaded, a.at, a.items = true, a.now(), items
	return a.items, nil
}

// listApplications describes every bundle under the roots. A root that does not
// exist is not an error: ~/Applications is optional, and so is the Utilities
// directory on a machine that has none.
func listApplications(roots []string, plistJSON func(string) ([]byte, error)) ([]applications.Application, error) {
	var (
		mu       sync.Mutex
		items    []applications.Application
		seen     = make(map[string]struct{})
		firstErr error
		wg       sync.WaitGroup
		slots    = make(chan struct{}, listingWorkers)
	)
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			if !os.IsNotExist(err) && firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), bundleSuffix) {
				continue
			}
			path := filepath.Join(root, entry.Name())
			if !isBundleDirectory(entry, path) {
				continue
			}
			wg.Add(1)
			slots <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-slots }()
				app, ok := describeApplication(path, plistJSON)
				if !ok {
					return
				}
				mu.Lock()
				defer mu.Unlock()
				if _, dup := seen[app.Path]; dup {
					return
				}
				seen[app.Path] = struct{}{}
				items = append(items, app)
			}()
		}
	}
	wg.Wait()
	// A root that cannot be read is reported only when nothing was found: a
	// readable /Applications with an unreadable ~/Applications still lists the
	// applications the user can pick.
	if firstErr != nil && len(items) == 0 {
		return nil, firstErr
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].Path < items[j].Path
	})
	if items == nil {
		items = []applications.Application{}
	}
	return items, nil
}

// isBundleDirectory reports whether an entry of an application directory is a
// bundle. A symlink counts when it points at a directory: macOS installs
// bundles that way in some locations, and the listing resolves the link anyway,
// so a rule written from the entry matches the process sing-box sees.
func isBundleDirectory(entry os.DirEntry, path string) bool {
	if entry.IsDir() {
		return true
	}
	if entry.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// describeApplication reads one bundle. A directory that is not a readable
// bundle is skipped: the listing reports what the user can choose from, not what
// failed to parse.
func describeApplication(path string, plistJSON func(string) ([]byte, error)) (applications.Application, bool) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolved = path
	}
	raw, err := plistJSON(filepath.Join(resolved, plistPath))
	if err != nil {
		return applications.Application{}, false
	}
	var info struct {
		DisplayName string `json:"CFBundleDisplayName"`
		Name        string `json:"CFBundleName"`
		Identifier  string `json:"CFBundleIdentifier"`
		Executable  string `json:"CFBundleExecutable"`
	}
	if err := json.Unmarshal(raw, &info); err != nil {
		return applications.Application{}, false
	}
	name := strings.TrimSpace(info.DisplayName)
	if name == "" {
		name = strings.TrimSpace(info.Name)
	}
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(resolved), bundleSuffix)
	}
	return applications.Application{
		Name:             name,
		BundleID:         info.Identifier,
		Path:             resolved,
		Executable:       info.Executable,
		ProcessPathRegex: BundlePathRegex(resolved),
	}, true
}

// plistToJSON asks plutil for the plist as JSON. The argument vector is fixed
// and absolute; nothing here goes through a shell.
func plistToJSON(path string) ([]byte, error) {
	return exec.Command(plutilPath, "-convert", "json", "-o", "-", "--", path).Output()
}
