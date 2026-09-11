package privrun

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/platform/childrun"
	"github.com/larffxx/singboxui/internal/platform/privhelper"
	"github.com/larffxx/singboxui/internal/privilege"
)

// This file names everything this package borrows, so the rest of the package
// reads as one flow.

// specType is the validated launch request of the helper protocol: the port
// request plus the application data directory.
type specType = privhelper.Spec

// The port types, under short names.
type (
	privilegeRequest = privilege.Request
	privilegeProcess = privilege.Process
)

// dirMode keeps the runtime directory private to its owner.
const dirMode = 0o700

// Codes of the errors this package raises, under short names.
const (
	apperrCodeRuntimeStartFailed = apperr.CodeRuntimeStartFailed
	apperrCodeRuntimeStopFailed  = apperr.CodeRuntimeStopFailed
)

// isAbs reports whether a path is absolute; a request path always must be.
func isAbs(path string) bool { return filepath.IsAbs(path) }

// invalid builds the typed error of a request this package refuses.
func invalid(format string, args ...any) error {
	return apperr.Newf(apperr.CodeInvalidArgument, opStart, format, args...)
}

// newSpec ties a port request to the application data directory.
func newSpec(req privilegeRequest, dataDir string) specType {
	return privhelper.SpecFromRequest(req, dataDir)
}

// stopRequest builds a stop request.
func stopRequest(dataDir, pidPath string, force bool) privhelper.StopRequest {
	return privhelper.StopRequest{DataDir: dataDir, PIDPath: pidPath, Force: force}
}

// stopArgv builds the elevated stop invocation.
func stopArgv(helperPath, dataDir, pidPath string, force bool) []string {
	return privhelper.StopArgv(helperPath, dataDir, pidPath, force)
}

// prepare makes the runtime directories usable and clears stale records, so a
// failed launch from an earlier session is never mistaken for this one.
func prepare(spec specType) error {
	if err := makeDirs(dirOf(spec.LogPath), dirOf(spec.PIDPath), dirOf(spec.StatusPath)); err != nil {
		return err
	}
	return privhelper.ClearRunFiles(spec.PIDPath, spec.StatusPath)
}

// childrunStart supervises the process with the rights this process has.
func childrunStart(ctx context.Context, spec specType) (privilegeProcess, error) {
	return childrun.Start(ctx, childrun.Config{
		BinaryPath: spec.BinaryPath,
		ConfigPath: spec.ConfigPath,
		WorkDir:    spec.ChildWorkDir(),
		LogPath:    spec.LogPath,
		PIDPath:    spec.PIDPath,
		StatusPath: spec.StatusPath,
	})
}

// childrunReadPID reads the identity record of a managed process.
func childrunReadPID(path string) (childrun.PIDFile, error) { return childrun.ReadPIDFile(path) }

// childrunReadStatus reads the exit record of a managed process.
func childrunReadStatus(path string) (childrun.Status, error) { return childrun.ReadStatus(path) }

// childrunAlivePID reports whether a recorded process is still running.
func childrunAlivePID(pid int) bool { return childrun.Alive(pid) }

// childrunTail follows the log file of a managed process.
func childrunTail(ctx context.Context, path string, exited func() bool) io.ReadCloser {
	return childrun.NewTailReader(ctx, path, exited)
}

// cancelledError reports a dismissed elevation prompt as PRIVILEGE_DENIED
// (spec §28).
func cancelledError(err error) error {
	return apperr.Wrap(apperr.CodePrivilegeDenied, opStart,
		"administrator rights are required to run sing-box with TUN support", err)
}

// wrapStart reports a launch failure with its cause.
func wrapStart(message string, err error) error {
	if err == nil {
		return apperr.New(apperr.CodeRuntimeStartFailed, opStart, message)
	}
	return apperr.Wrap(apperr.CodeRuntimeStartFailed, opStart, message, err)
}

// timeoutFailure reports a helper that never recorded the process it started.
func timeoutFailure(spec specType) error {
	return apperr.Newf(apperr.CodeRuntimeStartFailed, opStart,
		"the privileged helper did not record a process within %s", readinessTimeout)
}

// shuttingDown reports a launch cancelled by the caller, which for the runtime
// supervisor means the application is going away.
func shuttingDown(err error) error {
	return apperr.Wrap(apperr.CodeAppShuttingDown, opStart, "the launch was cancelled", err)
}

// apperrWrap is apperr.Wrap behind a short name.
func apperrWrap(code apperr.Code, operation, message string, err error) error {
	return apperr.Wrap(code, operation, message, err)
}

// apperrMessage returns the user-facing message of a typed error, or the empty
// string when the error is not typed.
func apperrMessage(err error) string {
	if err == nil {
		return ""
	}
	if _, ok := apperr.As(err); !ok {
		return ""
	}
	return apperr.MessageOf(err)
}

// dirOf is filepath.Dir, named for how it is used here.
func dirOf(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Dir(path)
}

// makeDirs creates the directories a run writes into, skipping empty paths.
func makeDirs(dirs ...string) error {
	seen := make(map[string]struct{}, len(dirs))
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		if err := os.MkdirAll(dir, dirMode); err != nil {
			return apperr.Wrap(apperr.CodeRuntimeStartFailed, opStart,
				"the runtime directory is not writable", err)
		}
	}
	return nil
}
