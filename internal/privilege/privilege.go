// Package privilege defines the narrow privileged-execution boundary.
//
// The SingBoxUI process itself is never elevated (spec §27). When a profile needs
// TUN, the runtime supervisor asks a Runner for one specific operation: launch
// *this* sing-box binary with *this* configuration file. There is no generic
// command execution, no shell, and no credential retention (spec §24, §83).
package privilege

import (
	"context"
	"errors"
	"io"
)

// Request is a narrow, fully validated launch request. Every path must be
// absolute and inside the application data directory; implementations re-validate
// them and refuse anything else.
type Request struct {
	// BinaryPath is the sing-box executable to run.
	BinaryPath string `json:"binaryPath"`
	// ConfigPath is the configuration file sing-box must read.
	ConfigPath string `json:"configPath"`
	// WorkDir is the working directory for the child process.
	WorkDir string `json:"workDir"`
	// LogPath receives merged stdout+stderr so the UI can stream logs.
	LogPath string `json:"logPath"`
	// PIDPath receives the child's PID so it can always be stopped again.
	PIDPath string `json:"pidPath"`
	// StatusPath receives the child's exit status once it terminates.
	StatusPath string `json:"statusPath"`
	// Elevate requests administrator privileges for this launch only.
	Elevate bool `json:"elevate"`
	// Reason is shown to the user in the elevation prompt, e.g. "TUN device required".
	Reason string `json:"reason"`
}

// Validate performs the checks every implementation may rely on.
func (r Request) Validate() error {
	for name, value := range map[string]string{
		"binaryPath": r.BinaryPath,
		"configPath": r.ConfigPath,
		"logPath":    r.LogPath,
		"pidPath":    r.PIDPath,
		"statusPath": r.StatusPath,
	} {
		if value == "" {
			return errors.New("privilege: " + name + " is required")
		}
	}
	return nil
}

// Process is a running sing-box instance under supervision.
//
// It hides whether the process is privileged: the supervisor only ever stops,
// waits for and reads logs from it.
type Process interface {
	// PID returns the operating-system process id, or 0 while unknown.
	PID() int
	// Elevated reports whether the process runs with administrator privileges.
	Elevated() bool
	// Logs returns the merged stdout+stderr stream. The reader stays valid until
	// Wait returns and must be closed by the caller.
	Logs() io.ReadCloser
	// Wait blocks until the process exits and returns its exit code. A negative
	// code means the process was terminated by a signal.
	Wait() (int, error)
	// Terminate asks the process to stop gracefully (SIGTERM / CTRL_BREAK).
	Terminate() error
	// Kill terminates the process tree without waiting for cooperation.
	Kill() error
	// Exited reports whether the process has already terminated.
	Exited() bool
}

// Runner launches sing-box, escalating only when Request.Elevate is set.
type Runner interface {
	// Start launches the requested process. Implementations must not block until
	// the child exits, so the caller can confirm health while it runs.
	Start(ctx context.Context, req Request) (Process, error)
	// Stop terminates the process recorded in the pid file, escalating only when
	// this process is not allowed to signal it (ADR 012).
	//
	// It exists because a sing-box can outlive the application that launched it:
	// nothing holds a handle to such a process, and one that runs as
	// administrator cannot be signalled by the unprivileged application at all,
	// so the same narrow helper stops it. The recorded start time is verified
	// before any signal is sent, and stopping a process that has already exited
	// is a successful no-op.
	Stop(ctx context.Context, pidPath string, force bool) error
	// Supported reports whether privileged launches are possible on this system,
	// and why not when they are not.
	Supported() (bool, string)
}

// ErrCancelled is returned when the user dismissed an elevation prompt. The
// supervisor maps it to the PRIVILEGE_DENIED error code (spec §28).
var ErrCancelled = errors.New("privilege: elevation cancelled by the user")

// ErrUnsupported is returned when the platform cannot escalate at all.
var ErrUnsupported = errors.New("privilege: privileged launch is not supported on this platform")
