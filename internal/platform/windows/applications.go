//go:build windows

package windows

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/applications"
)

// opApplications is this file's operation as it appears in typed errors.
const opApplications = "platform.windows.Applications.List"

const (
	// listingTTL bounds how long a listing is reused. Opening the routing screen
	// twice in a row must not walk the Start Menu and every process again, and a
	// program installed in between shows up after this window (ADR 011).
	listingTTL = 2 * time.Minute
	// executableSuffix is the only kind of file a routing rule can select:
	// sing-box matches a running process, and only an image can be one.
	executableSuffix = ".exe"
	// installerImage is what an advertised shortcut resolves to. Installers
	// create such shortcuts to run an MSI the user never selects as a program,
	// so the entry is dropped instead of being offered as "Windows Installer".
	installerImage = "msiexec.exe"
)

// catalogSources is where a listing comes from.
//
// The three functions are the whole operating-system surface of this file, so a
// test describes a machine — the programs it runs, the shortcuts it offers, the
// names its files carry — instead of inspecting the machine it runs on.
type catalogSources struct {
	// running returns the executable of every program that shows a window.
	running func() ([]string, error)
	// installed returns the target of every shortcut the Start Menu offers.
	installed func() ([]string, error)
	// describe returns the name a program calls itself in its version resource,
	// or an empty string when the file has none.
	describe func(path string) string
}

// Applications is the Windows catalog: the programs this machine runs and the
// programs its Start Menu offers (ADR 013).
//
// Windows has no application directory that could be enumerated the way macOS
// enumerates bundles — programs are installed anywhere, and the registry entries
// that survive an installation describe installers rather than the process a
// user launches. What it does have is the shell: a Start Menu shortcut is the
// program the user starts, and a running process that shows a window is the
// program that is running. Both answer with the executable path sing-box will see.
type Applications struct {
	sources catalogSources
	// windowsDir is the Windows directory. What lives there is the operating
	// system, its shells, its services and its installers; none of them is a
	// program a routing rule selects.
	windowsDir string
	// excluded are paths that are never offered: this application's own data
	// directory and its own executables. Routing the tunnel's own core, or the
	// window that manages it, is never what the screen is for.
	excluded []string

	ttl time.Duration
	now func() time.Time

	mu     sync.Mutex
	loaded bool
	at     time.Time
	items  []applications.Application
}

// NewApplications returns the catalog of this machine: its running windowed
// programs and the programs its Start Menu offers.
//
// dataDir is the application data directory, which is excluded from the listing
// together with this application's executables.
func NewApplications(dataDir string) *Applications {
	return &Applications{
		sources: catalogSources{
			running:   windowedProgramPaths,
			installed: startMenuShortcutTargets,
			describe:  fileDescription,
		},
		windowsDir: windowsDirectory(),
		excluded:   excludedPrefixes(dataDir),
		ttl:        listingTTL,
		now:        time.Now,
	}
}

// Supported reports that Windows can list its programs.
func (a *Applications) Supported() bool { return true }

// UnsupportedReason is empty: this platform supports the listing.
func (a *Applications) UnsupportedReason() string { return "" }

// List returns the programs of this machine, sorted by name. The result is
// cached for listingTTL; a listing that fails is not cached, so a transient
// failure does not hide the programs for the next two minutes.
func (a *Applications) List() ([]applications.Application, error) {
	const op = opApplications
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.loaded && a.now().Sub(a.at) < a.ttl {
		return a.items, nil
	}
	items, err := a.collect()
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, op, "the programs of this machine cannot be listed", err)
	}
	a.loaded, a.at, a.items = true, a.now(), items
	return a.items, nil
}

// collect turns both sources into the listing.
//
// A source that fails is reported only when nothing at all could be listed: a
// machine whose Start Menu cannot be read still shows the programs it is running.
func (a *Applications) collect() ([]applications.Application, error) {
	var (
		firstErr error
		items    []applications.Application
		seen     = make(map[string]struct{})
	)
	for _, source := range []func() ([]string, error){a.sources.running, a.sources.installed} {
		paths, err := source()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, path := range paths {
			entry, ok := a.entryFor(path)
			if !ok {
				continue
			}
			// The condition is the identity of a row: two paths that produce the
			// same rule are one program to the screen (Chrome's renderers, the
			// 32- and 64-bit launcher of one game, the versioned directory of an
			// application that just updated).
			key := strings.ToLower(entry.MatchValue)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			items = append(items, entry)
		}
	}
	if firstErr != nil && len(items) == 0 {
		return nil, firstErr
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := strings.ToLower(items[i].Name), strings.ToLower(items[j].Name)
		if left != right {
			return left < right
		}
		return strings.ToLower(items[i].Path) < strings.ToLower(items[j].Path)
	})
	if items == nil {
		items = []applications.Application{}
	}
	return items, nil
}

// entryFor describes one candidate path, or reports that it is not a program the
// screen offers: a shortcut may point at a document, a folder or a program that
// has been uninstalled, and a running process may be part of the operating
// system or of this application itself.
func (a *Applications) entryFor(path string) (applications.Application, bool) {
	// The shell returns shortcut targets in the form they were created with,
	// which can carry the %VAR% form of a path.
	resolved := filepath.Clean(expandEnvironment(strings.TrimSpace(path)))
	if resolved == "." || !filepath.IsAbs(resolved) {
		return applications.Application{}, false
	}
	if !strings.EqualFold(filepath.Ext(resolved), executableSuffix) {
		return applications.Application{}, false
	}
	if a.isExcluded(resolved) {
		return applications.Application{}, false
	}
	info, err := os.Stat(resolved)
	if err != nil || info.IsDir() {
		return applications.Application{}, false
	}
	name := strings.TrimSpace(a.sources.describe(resolved))
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(resolved), filepath.Ext(resolved))
	}
	return applications.Application{
		Name:       name,
		Path:       resolved,
		Executable: filepath.Base(resolved),
		// A Windows program is a file whose directory moves with every update
		// while its name stays what it was, so the condition that selects it is
		// its file name (ADR 013).
		MatchKey:   applications.MatchProcessName,
		MatchValue: filepath.Base(resolved),
	}, true
}

// isExcluded reports whether a path belongs to the operating system or to this
// application.
func (a *Applications) isExcluded(path string) bool {
	if base := strings.ToLower(filepath.Base(path)); base == installerImage {
		return true
	}
	if a.windowsDir != "" && withinDirectory(path, a.windowsDir) {
		return true
	}
	for _, prefix := range a.excluded {
		if withinDirectory(path, prefix) || strings.EqualFold(path, prefix) {
			return true
		}
	}
	return false
}

// withinDirectory reports whether path is the directory dir or lives inside it.
// Windows paths are compared case-insensitively, and a directory is a prefix of
// its own contents only when the separator is included.
func withinDirectory(path, dir string) bool {
	if dir == "" {
		return false
	}
	if strings.EqualFold(path, dir) {
		return true
	}
	prefix := dir
	if !strings.HasSuffix(prefix, string(os.PathSeparator)) {
		prefix += string(os.PathSeparator)
	}
	return len(path) > len(prefix) && strings.EqualFold(path[:len(prefix)], prefix)
}
