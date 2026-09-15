//go:build windows

package windows

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"golang.org/x/sys/windows/registry"
)

// Registry location of the login item: the per-user Run key, which the shell
// executes for every interactive logon (internal-contracts.md §2).
const (
	runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	valueName  = AppName
)

// Autostart is the Windows login item: the SingBoxUI value of
// HKCU\Software\Microsoft\Windows\CurrentVersion\Run.
//
// The value is written under the same name the Java prototype used and points at
// the native application instead of `javaw -jar`, so a leftover value is detected
// by comparing its program with this executable (migration plan §1).
type Autostart struct {
	// executable is the path of the running application, used to tell a value this
	// application wrote from one left behind by the prototype. It is empty when
	// os.Executable failed, in which case any existing value counts as legacy.
	executable string
	// value is the registry value name; empty means the one documented above. It
	// is a field, rather than only a constant, so that a test can use a name of
	// its own and never touch the login item of the machine it runs on.
	value string
}

// NewAutostart returns the login item of this application.
func NewAutostart() (*Autostart, error) {
	self, err := os.Executable()
	if err != nil {
		self = ""
	}
	return &Autostart{executable: self}, nil
}

// ValueName is the registry value this login item lives in. The platform port
// does not expose it; the migration and the tests need to know which value is
// meant.
func (a *Autostart) ValueName() string { return a.name() }

// name is the registry value name in force for this instance.
func (a *Autostart) name() string {
	if a.value != "" {
		return a.value
	}
	return valueName
}

// Supported reports whether the platform can install a login item. The per-user
// Run key exists for every interactive user.
func (a *Autostart) Supported() bool { return true }

// Enabled reports whether the login item is installed.
func (a *Autostart) Enabled() (bool, error) {
	_, ok, err := a.current()
	return ok, err
}

// Enable writes the login item so that execPath starts with the next logon,
// replacing whatever value is there. The command line is quoted, because the Run
// key is parsed by the shell and the path may contain spaces.
func (a *Autostart) Enable(execPath string) error {
	if err := validateExecutable(execPath); err != nil {
		return err
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, opAutostart, "the login item registry key cannot be opened for writing", err)
	}
	defer key.Close()
	if err := key.SetStringValue(a.name(), quote(execPath)); err != nil {
		return apperr.Wrap(apperr.CodeInternal, opAutostart, "the login item cannot be written", err)
	}
	return nil
}

// Disable removes the login item, leaving the rest of the Run key alone.
func (a *Autostart) Disable() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, opAutostart, "the login item registry key cannot be opened", err)
	}
	defer key.Close()
	if err := key.DeleteValue(a.name()); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return apperr.Wrap(apperr.CodeInternal, opAutostart, "the login item cannot be removed", err)
	}
	return nil
}

// LegacyEntry describes the login item the Java prototype left behind, if one is
// present, and returns "" when the installed value is this application's own
// (migration plan §1). The description is shown in Settings and can be acted upon
// through RemoveLegacy; nothing here deletes anything by itself.
func (a *Autostart) LegacyEntry() string {
	value, ok, err := a.current()
	if err != nil || !ok {
		return ""
	}
	program := targetProgram(value)
	if a.executable != "" && program != "" && sameExecutable(program, a.executable) {
		return ""
	}
	return "the registry value HKCU\\" + runKeyPath + "\\" + a.name() + " starts " + value
}

// RemoveLegacy deletes the login item left behind by the prototype, and does
// nothing when there is no legacy entry. It is the explicit user action behind
// "old Java entry found - replace?".
func (a *Autostart) RemoveLegacy() error {
	if a.LegacyEntry() == "" {
		return nil
	}
	return a.Disable()
}

// current reads the installed login item value.
func (a *Autostart) current() (string, bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, apperr.Wrap(apperr.CodeInternal, opAutostart, "the login item registry key cannot be read", err)
	}
	defer key.Close()
	value, _, err := key.GetStringValue(a.name())
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, apperr.Wrap(apperr.CodeInternal, opAutostart, "the login item cannot be read", err)
	}
	return value, true, nil
}

// quote wraps a command line in the quotes the shell needs for a path with
// spaces. It is not a shell parser: an argument list is never built here.
func quote(execPath string) string {
	return `"` + execPath + `"`
}

// targetProgram extracts the program of a Run value: the quoted first word when
// the value is quoted, the first word otherwise.
func targetProgram(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, `"`) {
		if end := strings.Index(value[1:], `"`); end >= 0 {
			return value[1 : 1+end]
		}
		return strings.Trim(value, `"`)
	}
	if space := strings.IndexAny(value, " \t"); space >= 0 {
		return value[:space]
	}
	return value
}

// validateExecutable refuses a path that cannot be an application executable, so
// that a malformed request never reaches the registry.
func validateExecutable(path string) error {
	if !filepath.IsAbs(path) {
		return apperr.Newf(apperr.CodeInvalidArgument, opAutostart, "the autostart program must be an absolute path, got %q", path)
	}
	if path != filepath.Clean(path) {
		return apperr.Newf(apperr.CodeInvalidArgument, opAutostart, "the autostart program must be a clean path, got %q", path)
	}
	if hasControl(path) {
		return apperr.New(apperr.CodeInvalidArgument, opAutostart, "the autostart program contains control characters")
	}
	return nil
}

// sameExecutable compares two program paths, case-insensitively because Windows
// paths are not case-sensitive.
func sameExecutable(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// hasControl reports whether s contains a control character, which no path of this
// application may contain.
func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
