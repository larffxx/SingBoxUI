// Package platform is the operating-system boundary: where application data
// lives, what the machine can run, and how the application starts with the
// session.
//
// The application layer depends on this interface only; darwin and windows
// provide the implementations.
package platform

import (
	"errors"
	"path/filepath"

	"github.com/larffxx/singboxui/internal/privilege"
)

// Info describes the machine the application runs on.
type Info struct {
	// OS is "darwin", "windows" or "linux" (linux is unsupported, spec §1).
	OS string
	// Arch is the GOARCH-style architecture: amd64 or arm64.
	Arch string
	// Supported is false on platforms the application does not target.
	Supported bool
	// UnsupportedReason explains why, when Supported is false.
	UnsupportedReason string
}

// Paths is the complete on-disk layout of the application.
//
// Nothing here is derived from the working directory: a packaged macOS .app has
// no meaningful cwd (see docs/architecture/migration-plan.md §2).
type Paths struct {
	// DataDir holds everything the application owns.
	DataDir string
	// DBPath is the SQLite metadata database.
	DBPath string
	// LogPath is the application log file.
	LogPath string
	// ConfigDir holds materialised sing-box configurations.
	ConfigDir string
	// ActiveConfigPath is the configuration the managed sing-box reads.
	ActiveConfigPath string
	// LastGoodConfigPath is the last configuration that started successfully.
	LastGoodConfigPath string
	// RuntimeDir holds per-launch scratch files (pid, status, sing-box logs).
	RuntimeDir string
	// BinDir holds the managed sing-box releases, one directory per version.
	BinDir string
	// TempDir is for downloads before they are verified and installed.
	TempDir string
}

// ManagedBinaryPath returns the install location of a managed sing-box version.
func (p Paths) ManagedBinaryPath(version, executable string) string {
	return filepath.Join(p.BinDir, version, executable)
}

// Autostart is the session-autostart boundary (spec §50). The frontend only ever
// sees "supported" and "enabled"; the OS mechanism stays behind this interface.
type Autostart interface {
	// Supported reports whether this platform can register an autostart entry.
	Supported() bool
	// Enabled reports whether the entry currently exists.
	Enabled() (bool, error)
	// Enable registers the entry so `execPath` starts with the user session.
	Enable(execPath string) error
	// Disable removes the entry; a missing entry is not an error.
	Disable() error
	// LegacyEntry describes an autostart entry left behind by the Java
	// prototype, if one is present (migration plan §1).
	LegacyEntry() string
	// RemoveLegacy deletes that legacy entry. It is only called after the user
	// confirms the replacement.
	RemoveLegacy() error
}

// Platform is the whole OS abstraction handed to the application layer.
type Platform interface {
	// Info describes the current machine.
	Info() Info
	// Paths returns the directory layout, creating nothing.
	Paths() Paths
	// EnsureDirs creates the layout with the correct permissions.
	EnsureDirs() error
	// Autostart returns the session-autostart adapter.
	Autostart() Autostart
	// PrivilegeRunner returns the platform's narrow privileged-launch adapter.
	// It is a separate concern from the rest of the platform surface because
	// only the runtime supervisor may use it (spec §27).
	PrivilegeRunner() privilege.Runner
}

// ErrUnsupportedPlatform is returned when the application runs somewhere it does
// not support.
var ErrUnsupportedPlatform = errors.New("platform: unsupported operating system")
