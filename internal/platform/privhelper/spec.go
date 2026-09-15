// Package privhelper implements the protocol of the narrow privileged helper,
// cmd/singboxui-priv, and the validation both sides of the boundary share.
//
// The helper performs exactly two operations — start and stop of one validated
// sing-box process — and accepts no other operation and no free-form arguments
// (docs/architecture/internal-contracts.md §3). The unprivileged side builds the
// same Spec and validates it before asking the operating system for elevation, so
// a request that reaches the helper has already been checked twice.
//
// Nothing here builds a command line: processes are always started from an
// argument array (spec §24, §83).
package privhelper

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/privilege"
)

// OpStart and OpStop are the only operations the helper knows.
const (
	OpStart = "start"
	OpStop  = "stop"
)

// Flag names of the helper's command line.
const (
	FlagDataDir = "data-dir"
	FlagBinary  = "binary"
	FlagConfig  = "config"
	FlagWorkDir = "work-dir"
	FlagLog     = "log"
	FlagPID     = "pid"
	FlagStatus  = "status"
	FlagReason  = "reason"
	FlagForce   = "force"
)

// opValidate names this package's operation in typed errors.
const opValidate = "privhelper.validate"

// maxReasonLength bounds the human-readable elevation reason.
const maxReasonLength = 200

// Spec is a launch request tied to the application data directory. The port type
// privilege.Request deliberately has no data directory (it belongs to the
// platform), so the runner adds it before validating.
type Spec struct {
	// DataDir is the application data directory every other path must be inside.
	DataDir string
	// BinaryPath is the sing-box executable to run.
	BinaryPath string
	// ConfigPath is the configuration file sing-box reads.
	ConfigPath string
	// WorkDir is the child working directory; empty means the config's directory.
	WorkDir string
	// LogPath receives merged stdout+stderr.
	LogPath string
	// PIDPath receives the pid + start time record.
	PIDPath string
	// StatusPath receives the exit code record.
	StatusPath string
	// Reason explains the request; it is shown to the user where the OS allows.
	Reason string
}

// SpecFromRequest ties a port request to the application data directory.
func SpecFromRequest(req privilege.Request, dataDir string) Spec {
	return Spec{
		DataDir:    dataDir,
		BinaryPath: req.BinaryPath,
		ConfigPath: req.ConfigPath,
		WorkDir:    req.WorkDir,
		LogPath:    req.LogPath,
		PIDPath:    req.PIDPath,
		StatusPath: req.StatusPath,
		Reason:     req.Reason,
	}
}

// ChildWorkDir is the directory the child runs in: the request's working
// directory, or the directory of the configuration file.
func (s Spec) ChildWorkDir() string {
	if s.WorkDir != "" {
		return s.WorkDir
	}
	return filepath.Dir(s.ConfigPath)
}

// StartArgv builds the helper invocation for this request. The result is an
// argument array used directly with the OS elevation mechanism.
func (s Spec) StartArgv(helperPath string) []string {
	argv := []string{
		helperPath, OpStart,
		"--" + FlagDataDir, s.DataDir,
		"--" + FlagBinary, s.BinaryPath,
		"--" + FlagConfig, s.ConfigPath,
		"--" + FlagLog, s.LogPath,
		"--" + FlagPID, s.PIDPath,
		"--" + FlagStatus, s.StatusPath,
	}
	if s.WorkDir != "" {
		argv = append(argv, "--"+FlagWorkDir, s.WorkDir)
	}
	if s.Reason != "" {
		argv = append(argv, "--"+FlagReason, s.Reason)
	}
	return argv
}

// StopArgv builds the helper invocation that stops the process recorded in
// pidPath.
func StopArgv(helperPath, dataDir, pidPath string, force bool) []string {
	argv := []string{
		helperPath, OpStop,
		"--" + FlagDataDir, dataDir,
		"--" + FlagPID, pidPath,
	}
	if force {
		argv = append(argv, "--"+FlagForce)
	}
	return argv
}

// Validate refuses anything the helper must not be asked to do.
//
// Every path is required to be absolute and inside the application data
// directory: the helper only ever runs the sing-box executable it was asked to
// run, reading the configuration it was given (internal-contracts.md §3). Paths
// are resolved through symbolic links, so a link pointing outside the data
// directory is refused as well.
func (s Spec) Validate() error {
	if err := s.validatePaths(); err != nil {
		return err
	}
	return s.validateFiles()
}

func (s Spec) validatePaths() error {
	if err := requireAbsolute(FlagDataDir, s.DataDir); err != nil {
		return err
	}
	for _, field := range []struct {
		name     string
		value    string
		required bool
	}{
		{FlagBinary, s.BinaryPath, true},
		{FlagConfig, s.ConfigPath, true},
		{FlagLog, s.LogPath, true},
		{FlagPID, s.PIDPath, true},
		{FlagStatus, s.StatusPath, true},
		{FlagWorkDir, s.WorkDir, false},
	} {
		if field.value == "" {
			if !field.required {
				continue
			}
			return invalid("--%s is required", field.name)
		}
		if err := requireAbsolute(field.name, field.value); err != nil {
			return err
		}
		if err := requireClean(field.name, field.value); err != nil {
			return err
		}
		if err := requirePrintable(field.name, field.value); err != nil {
			return err
		}
		if err := requireInside(s.DataDir, field.name, field.value); err != nil {
			return err
		}
	}
	if s.Reason != "" {
		if err := requirePrintable(FlagReason, s.Reason); err != nil {
			return err
		}
		if len(s.Reason) > maxReasonLength {
			return invalid("--%s is longer than %d characters", FlagReason, maxReasonLength)
		}
	}
	return nil
}

func (s Spec) validateFiles() error {
	if info, err := statExisting(s.DataDir); err != nil {
		return invalid("%s is not usable: %v", s.DataDir, err)
	} else if !info.IsDir() {
		return invalid("%s is not a directory", s.DataDir)
	}

	expected := executableName(runtime.GOOS)
	resolved, err := resolveInside(s.DataDir, s.BinaryPath)
	if err != nil {
		return invalid("--%s: %v", FlagBinary, err)
	}
	if got := filepath.Base(resolved); got != expected {
		return invalid("--%s must be the sing-box executable %q, got %q", FlagBinary, expected, got)
	}
	info, err := statExisting(s.BinaryPath)
	if err != nil {
		return invalid("--%s is not usable: %v", FlagBinary, err)
	}
	if !info.Mode().IsRegular() {
		return invalid("--%s is not a regular file", FlagBinary)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return invalid("--%s is not executable", FlagBinary)
	}

	if filepath.Ext(s.ConfigPath) != ".json" {
		return invalid("--%s must be a .json file", FlagConfig)
	}
	if _, err := resolveInside(s.DataDir, s.ConfigPath); err != nil {
		return invalid("--%s: %v", FlagConfig, err)
	}
	if info, err := statExisting(s.ConfigPath); err != nil {
		return invalid("--%s is not usable: %v", FlagConfig, err)
	} else if !info.Mode().IsRegular() {
		return invalid("--%s is not a regular file", FlagConfig)
	}

	// --work-dir is the child's working directory, so unlike the record files it
	// must be a directory and must already exist.
	if s.WorkDir != "" {
		if _, err := resolveInside(s.DataDir, s.WorkDir); err != nil {
			return invalid("--%s: %v", FlagWorkDir, err)
		}
		info, err := statExisting(s.WorkDir)
		if err != nil {
			return invalid("--%s is not usable: %v", FlagWorkDir, err)
		}
		if !info.IsDir() {
			return invalid("--%s is not a directory", FlagWorkDir)
		}
	}

	for _, field := range []struct {
		name  string
		value string
	}{
		{FlagLog, s.LogPath},
		{FlagPID, s.PIDPath},
		{FlagStatus, s.StatusPath},
	} {
		if field.value == "" {
			continue
		}
		if _, err := resolveInside(s.DataDir, field.value); err != nil {
			return invalid("--%s: %v", field.name, err)
		}
		if info, err := statExisting(field.value); err == nil && info.IsDir() {
			return invalid("--%s is a directory", field.name)
		}
		if info, err := statExisting(filepath.Dir(field.value)); err != nil {
			return invalid("the directory of --%s is not usable: %v", field.name, err)
		} else if !info.IsDir() {
			return invalid("the directory of --%s is not a directory", field.name)
		}
	}
	return nil
}

// StopRequest is a validated stop invocation.
type StopRequest struct {
	// DataDir is the application data directory the pid file lives in.
	DataDir string
	// PIDPath is the identity record of the process to stop.
	PIDPath string
	// Force skips the graceful termination.
	Force bool
}

// Validate refuses a stop request that points outside the data directory.
func (r StopRequest) Validate() error {
	if err := requireAbsolute(FlagDataDir, r.DataDir); err != nil {
		return err
	}
	if err := requireAbsolute(FlagPID, r.PIDPath); err != nil {
		return err
	}
	if err := requireClean(FlagPID, r.PIDPath); err != nil {
		return err
	}
	if err := requirePrintable(FlagPID, r.PIDPath); err != nil {
		return err
	}
	if err := requireInside(r.DataDir, FlagPID, r.PIDPath); err != nil {
		return err
	}
	if _, err := resolveInside(r.DataDir, r.PIDPath); err != nil {
		return invalid("--%s: %v", FlagPID, err)
	}
	if info, err := statExisting(r.PIDPath); err == nil && info.IsDir() {
		return invalid("--%s is a directory", FlagPID)
	}
	return nil
}

// ClearRunFiles removes the records of a previous run so a stale pid or exit
// code can never be mistaken for the current one. A missing file is fine.
func ClearRunFiles(paths ...string) error {
	for _, path := range paths {
		if path == "" {
			continue
		}
		if err := removeIfPresent(path); err != nil {
			return err
		}
	}
	return nil
}

// executableName is the sing-box executable name for a GOOS.
//
// internal/singbox owns the canonical ExecutableName (internal-contracts.md §1);
// the helper keeps a private copy on purpose, so the privileged binary depends on
// nothing but the standard library and the two port packages it needs.
func executableName(goos string) string {
	if goos == "windows" {
		return "sing-box.exe"
	}
	return "sing-box"
}

// invalid builds the typed error the frontend branches on.
func invalid(format string, args ...any) *apperr.Error {
	return apperr.Newf(apperr.CodeInvalidArgument, opValidate, format, args...)
}

// requireAbsolute refuses a relative path.
func requireAbsolute(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return invalid("--%s is required", name)
	}
	if !filepath.IsAbs(value) {
		return invalid("--%s must be an absolute path, got %q", name, value)
	}
	return nil
}

// requireClean refuses a path that is not in its shortest form, so that ".."
// cannot be used to reach outside the data directory after resolution.
func requireClean(name, value string) error {
	if cleaned := filepath.Clean(value); cleaned != value {
		return invalid("--%s must not contain %q, %q or a trailing separator: %q",
			name, "..", ".", value)
	}
	return nil
}

// requirePrintable refuses control characters, which no legitimate path needs
// and which must never reach a shell or AppleScript string.
func requirePrintable(name, value string) error {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return invalid("--%s contains a control character", name)
		}
	}
	return nil
}

// requireInside refuses a path outside the data directory.
func requireInside(dir, name, value string) error {
	rel, err := filepath.Rel(dir, value)
	if err != nil {
		return invalid("--%s is not inside the application data directory", name)
	}
	if rel == "." {
		return nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return invalid("--%s is outside the application data directory", name)
	}
	return nil
}

// resolveInside resolves the deepest existing ancestor of path and reports where
// the path really lands, so a symlinked directory cannot be used to escape the
// data directory.
func resolveInside(dir, path string) (string, error) {
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", dir, err)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", filepath.Dir(path), err)
	}
	resolved := filepath.Join(parent, filepath.Base(path))
	rel, err := filepath.Rel(resolvedDir, resolved)
	if err != nil {
		return "", fmt.Errorf("%s is outside %s", path, dir)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%s is outside %s", path, dir)
	}
	return resolved, nil
}
