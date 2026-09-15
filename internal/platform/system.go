package platform

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/privilege"
)

// Operations of this package as they appear in typed errors.
const (
	opEnsureDirs = "platform.EnsureDirs"
)

// dirMode keeps every directory of the layout private to its owner: a
// materialised configuration may carry credentials and the runtime logs carry
// the traffic being inspected, so no other user of the machine may read them.
const dirMode = 0o700

// Layout names. They are the contract of the data directory as it is described
// in docs/architecture/migration-plan.md §1 and
// docs/architecture/current-state.md (files that survive the migration).
const (
	dbName         = "singboxui.db"
	configDirName  = "config"
	activeName     = "active.json"
	lastGoodName   = "last-good.json"
	logsDirName    = "logs"
	logFileName    = "singboxui.log"
	runtimeDirName = "run"
	binDirName     = "bin"
	binOwnerName   = "sing-box"
	tempDirName    = "tmp"
)

// hostSupport describes a GOOS/GOARCH pair. It is a pure function so the
// unsupported answer can be tested from any host, in particular from a host that
// could not build the unsupported branch itself.
func hostSupport(goos, goarch string) Info {
	info := Info{OS: goos, Arch: goarch}
	// The operating systems the application targets (spec §1). A switch, not a
	// table: the package keeps no package-level state at all (spec §62).
	switch goos {
	case "darwin", "windows":
		info.Supported = true
		return info
	}
	info.UnsupportedReason = "sing-box is managed on macOS and Windows only; " + goos + " is not supported"
	return info
}

// layout returns the complete on-disk layout under a data directory. Everything
// is derived from that one directory, never from the working directory: a
// packaged macOS .app has no meaningful cwd (migration plan §2).
func layout(dataDir string) Paths {
	configDir := filepath.Join(dataDir, configDirName)
	return Paths{
		DataDir:            dataDir,
		DBPath:             filepath.Join(dataDir, dbName),
		LogPath:            filepath.Join(dataDir, logsDirName, logFileName),
		ConfigDir:          configDir,
		ActiveConfigPath:   filepath.Join(configDir, activeName),
		LastGoodConfigPath: filepath.Join(configDir, lastGoodName),
		RuntimeDir:         filepath.Join(dataDir, runtimeDirName),
		BinDir:             filepath.Join(dataDir, binDirName, binOwnerName),
		TempDir:            filepath.Join(dataDir, tempDirName),
	}
}

// directories returns every directory the layout owns, without repetitions, in
// the order EnsureDirs creates them.
func (p Paths) directories() []string {
	candidates := []string{
		p.DataDir,
		dirOf(p.DBPath),
		dirOf(p.LogPath),
		p.ConfigDir,
		p.RuntimeDir,
		p.BinDir,
		p.TempDir,
	}
	dirs := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, dir := range candidates {
		if dir == "" {
			continue
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		dirs = append(dirs, dir)
	}
	return dirs
}

// dirOf is the directory of one of the layout's files. A file with no path has
// no directory, so an unset Paths reports nothing rather than the current
// directory.
func dirOf(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Dir(path)
}

// system implements Platform out of the mechanisms of the running operating
// system, which darwin.New or windows.New supplies. Everything that is not
// specific to an operating system - the layout, the directory creation and the
// description of the machine - lives here, so both platforms cannot drift apart.
type system struct {
	info         Info
	paths        Paths
	autostart    Autostart
	applications ApplicationCatalog
	privilege    privilege.Runner
}

// A compile-time check that the assembled platform fulfils the port the
// application layer depends on.
var _ Platform = (*system)(nil)

// newSystem assembles the platform of the running machine.
func newSystem(dataDir string, autostart Autostart, applications ApplicationCatalog, runner privilege.Runner) *system {
	return &system{
		info:         hostSupport(runtime.GOOS, runtime.GOARCH),
		paths:        layout(dataDir),
		autostart:    autostart,
		applications: applications,
		privilege:    runner,
	}
}

// Info describes the current machine.
func (s *system) Info() Info { return s.info }

// Paths returns the directory layout, creating nothing.
func (s *system) Paths() Paths { return s.paths }

// EnsureDirs creates the layout, owner-only, and is safe to call before any path
// is used. It is idempotent, creates no file, and leaves the permissions of a
// directory that already exists untouched.
func (s *system) EnsureDirs() error {
	for _, dir := range s.paths.directories() {
		if err := os.MkdirAll(dir, dirMode); err != nil {
			return apperr.Wrap(apperr.CodeInternal, opEnsureDirs,
				"the directory "+dir+" cannot be created", err)
		}
	}
	return nil
}

// Autostart returns the session-autostart adapter.
func (s *system) Autostart() Autostart { return s.autostart }

// Applications returns the catalog of the applications this machine has
// installed.
func (s *system) Applications() ApplicationCatalog { return s.applications }

// PrivilegeRunner returns the platform's narrow privileged-launch adapter.
func (s *system) PrivilegeRunner() privilege.Runner { return s.privilege }
