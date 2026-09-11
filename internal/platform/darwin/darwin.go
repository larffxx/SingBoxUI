//go:build darwin

// Package darwin implements the macOS side of internal/platform: the data
// directory, the LaunchAgent login item, and the osascript-based elevation of
// the narrow privileged helper (target-state.md §7).
//
// The package deliberately does not import internal/platform. The parent has to
// import this package to dispatch to it by build tag, so an import back would be
// an import cycle: the parent therefore assembles platform.Platform out of New's
// result, and this package exposes the macOS mechanisms only. Autostart does
// satisfy the platform.Autostart port as it stands, because that port is declared
// entirely in terms of strings, booleans and errors.
package darwin

import (
	"os"
	"path/filepath"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/platform/privrun"
)

// Operations of this package as they appear in typed errors. Each is a
// "<package>.<function>" pair, matching internal/domain/apperr's convention.
const (
	opDataDir   = "platform.darwin.DataDir"
	opHelper    = "platform.darwin.HelperPath"
	opAutostart = "platform.darwin.Autostart"
	opElevate   = "platform.darwin.Elevate"
)

// Names owned by the macOS implementation.
const (
	// AppName is the user-visible application name.
	AppName = "SingBoxUI"
	// AgentLabel is the launchd label of the login item. It is the label the Java
	// prototype used, because the entry is replaced in place rather than joined
	// by a second one (current-state.md, "replaced files").
	AgentLabel = "com.larffxx.singboxui"
	// HelperExecutable is the narrow privileged helper, which ships next to the
	// main binary (internal-contracts.md §3).
	HelperExecutable = "singboxui-priv"
)

// DataDir is the application data directory,
// ~/Library/Application Support/SingBoxUI (internal-contracts.md §2). It is
// derived from the home directory of the user, never from the working directory
// (migration plan §2).
func DataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", apperr.Wrap(apperr.CodeInternal, opDataDir, "the home directory cannot be determined", err)
	}
	if home == "" {
		return "", apperr.New(apperr.CodeInternal, opDataDir, "the home directory cannot be determined")
	}
	return filepath.Join(home, "Library", "Application Support", AppName), nil
}

// HelperPath is the absolute path of the privileged helper: it is installed next
// to the application executable, inside Contents/MacOS of the bundle
// (internal-contracts.md §3).
func HelperPath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", apperr.Wrap(apperr.CodeRuntimeStartFailed, opHelper, "the application executable cannot be located", err)
	}
	path := filepath.Join(filepath.Dir(executable), HelperExecutable)
	info, err := os.Stat(path)
	if err != nil {
		return "", apperr.Wrap(apperr.CodeBinaryNotFound, opHelper, "the privileged helper "+HelperExecutable+" is not installed next to the application", err)
	}
	if info.IsDir() {
		return "", apperr.Newf(apperr.CodeBinaryNotFound, opHelper, "%s is a directory, not the privileged helper", path)
	}
	return path, nil
}

// System is the macOS implementation of the platform mechanisms.
type System struct {
	// DataDir is the application data directory.
	DataDir string
	// Autostart is the LaunchAgent login item.
	Autostart *Autostart
	// PrivilegeRunner launches sing-box with administrator rights, or directly
	// when the caller asks for no elevation.
	PrivilegeRunner *privrun.Runner
}

// New resolves the application data directory and builds the macOS mechanisms.
// Nothing is created on disk: EnsureDirs, on the platform port, is what creates
// the layout, and the login item is written only when it is enabled.
//
// internal/platform calls this from its build-tagged New; see the package comment
// for why that direction is the only possible one.
func New() (*System, error) {
	dataDir, err := DataDir()
	if err != nil {
		return nil, err
	}
	autostart, err := NewAutostart()
	if err != nil {
		return nil, err
	}
	runner, err := NewPrivilegeRunner(dataDir)
	if err != nil {
		return nil, err
	}
	return &System{DataDir: dataDir, Autostart: autostart, PrivilegeRunner: runner}, nil
}
