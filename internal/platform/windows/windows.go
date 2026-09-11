//go:build windows

// Package windows implements the Windows side of internal/platform: the data
// directory, the registry login item, and the ShellExecuteExW(runas) elevation of
// the narrow privileged helper (target-state.md §7).
//
// The package deliberately does not import internal/platform. The parent has to
// import this package to dispatch to it by build tag, so an import back would be
// an import cycle: the parent therefore assembles platform.Platform out of New's
// result, and this package exposes the Windows mechanisms only. Autostart does
// satisfy the platform.Autostart port as it stands, because that port is declared
// entirely in terms of strings, booleans and errors.
package windows

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/platform/privrun"
	"golang.org/x/sys/windows"
)

// Operations of this package as they appear in typed errors.
const (
	opDataDir   = "platform.windows.DataDir"
	opHelper    = "platform.windows.HelperPath"
	opAutostart = "platform.windows.Autostart"
	opElevate   = "platform.windows.Elevate"
)

// Names owned by the Windows implementation.
const (
	// AppName is the user-visible application name and the directory under
	// %LOCALAPPDATA%.
	AppName = "SingBoxUI"
	// HelperExecutable is the narrow privileged helper, which ships next to the
	// main binary (internal-contracts.md §3).
	HelperExecutable = "singboxui-priv.exe"
)

// DataDir is the application data directory, %LOCALAPPDATA%\SingBoxUI
// (internal-contracts.md §2), resolved through the known-folder API rather than
// from the environment (migration plan §2).
func DataDir() (string, error) {
	base, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_DEFAULT)
	if err != nil || base == "" {
		// A service account or a locked-down profile may not expose the known
		// folder; the environment variable is the documented fallback of the
		// platform itself.
		base = strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	}
	if base == "" {
		return "", apperr.Wrap(apperr.CodeInternal, opDataDir, "the local application data directory cannot be determined", err)
	}
	return filepath.Join(base, AppName), nil
}

// HelperPath is the absolute path of the privileged helper: it is installed next
// to the application executable (internal-contracts.md §3).
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

// System is the Windows implementation of the platform mechanisms.
type System struct {
	// DataDir is the application data directory.
	DataDir string
	// Autostart is the registry login item.
	Autostart *Autostart
	// PrivilegeRunner launches sing-box with administrator rights, or directly
	// when the caller asks for no elevation.
	PrivilegeRunner *privrun.Runner
}

// New resolves the application data directory and builds the Windows mechanisms.
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
