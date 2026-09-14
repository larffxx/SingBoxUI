// Package childrun supervises a sing-box process that is launched directly, and
// keeps the bookkeeping files that make such a process observable and stoppable
// from outside the process that started it:
//
//   - PIDPath    pid + process start time, written before Start returns;
//   - StatusPath exit code, written when the process ends;
//   - LogPath    merged stdout+stderr, tailed through a cancellation-aware reader.
//
// Both the unprivileged application and the privileged helper
// (cmd/singboxui-priv, internal-contracts.md §3) supervise their child through
// this package, so a privileged launch behaves exactly like a local one.
//
// Nothing in this package shells out through a shell: children are started from
// an argument array (spec §24).
package childrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
)

// operation names used in typed errors.
const (
	opStart = "childrun.Start"
	opWait  = "childrun.Wait"
)

// killGrace is how long a child gets to react to a graceful termination when its
// context is cancelled, before it is killed.
const killGrace = 3 * time.Second

// Config describes the process to supervise.
type Config struct {
	// BinaryPath is the executable to run; it is always started from an
	// argument array, never through a shell.
	BinaryPath string
	// ConfigPath is the sing-box configuration file.
	ConfigPath string
	// WorkDir is the child working directory; empty means the current one.
	WorkDir string
	// LogPath receives merged stdout+stderr.
	LogPath string
	// PIDPath receives the pid + start time record.
	PIDPath string
	// StatusPath receives the exit code record.
	StatusPath string
	// Args overrides the command line; empty means "run -c <ConfigPath>".
	Args []string
}

// Validate checks the minimum needed to start a child.
func (c Config) Validate() error {
	for name, value := range map[string]string{
		"binaryPath": c.BinaryPath,
		"configPath": c.ConfigPath,
		"logPath":    c.LogPath,
		"pidPath":    c.PIDPath,
		"statusPath": c.StatusPath,
	} {
		if value == "" {
			return fmt.Errorf("childrun: %s is required", name)
		}
		if !filepath.IsAbs(value) {
			return fmt.Errorf("childrun: %s must be absolute, got %q", name, value)
		}
	}
	return nil
}

// CommandArgs returns the argument array used to run sing-box.
func (c Config) CommandArgs() []string {
	if len(c.Args) > 0 {
		return append([]string(nil), c.Args...)
	}
	return []string{"run", "-c", c.ConfigPath}
}

// Child is a supervised sing-box process. It satisfies privilege.Process, so the
// runtime supervisor treats local and privileged launches identically.
type Child struct {
	cfg       Config
	cmd       *exec.Cmd
	pid       int
	startedAt time.Time

	once    sync.Once
	done    chan struct{}
	code    int
	waitErr error
	ctx     context.Context
}

// Start launches the process and records PIDPath before returning, so the caller
// can always stop what it started (internal-contracts.md §3). It does not wait
// for the child: the caller confirms health while it runs.
func Start(ctx context.Context, cfg Config) (*Child, error) {
	if err := cfg.Validate(); err != nil {
		return nil, apperr.Wrap(apperr.CodeInvalidArgument, opStart, err.Error(), err)
	}
	for _, dir := range []string{filepath.Dir(cfg.LogPath), filepath.Dir(cfg.PIDPath), filepath.Dir(cfg.StatusPath)} {
		if err := os.MkdirAll(dir, runtimeDirMode); err != nil {
			return nil, apperr.Wrap(apperr.CodeRuntimeStartFailed, opStart, "create runtime directory "+dir, err)
		}
	}
	logFile, err := os.OpenFile(cfg.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, logFileMode)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeRuntimeStartFailed, opStart, "open log file "+cfg.LogPath, err)
	}

	cmd := exec.Command(cfg.BinaryPath, cfg.CommandArgs()...)
	if cfg.WorkDir != "" {
		cmd.Dir = cfg.WorkDir
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	configureChild(cmd)

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, apperr.Wrap(apperr.CodeRuntimeStartFailed, opStart, "start "+cfg.BinaryPath, err)
	}
	// The child owns its handles now.
	_ = logFile.Close()

	child := &Child{
		cfg:       cfg,
		cmd:       cmd,
		pid:       cmd.Process.Pid,
		startedAt: time.Now().UTC(),
		done:      make(chan struct{}),
		ctx:       ctx,
	}
	start, err := StartTimeNano(child.pid)
	if err != nil {
		// Without a start time the process could never be verified again, so it is
		// stopped right away rather than left unmanaged.
		_ = Kill(child.pid)
		_, _ = child.Wait()
		return nil, apperr.Wrap(apperr.CodeRuntimeStartFailed, opStart, "record process identity", err)
	}
	record := PIDFile{
		PID:        child.pid,
		StartTime:  start,
		BinaryPath: cfg.BinaryPath,
		RecordedAt: child.startedAt,
	}
	if err := WritePIDFile(cfg.PIDPath, record); err != nil {
		_ = Kill(child.pid)
		_, _ = child.Wait()
		return nil, apperr.Wrap(apperr.CodeRuntimeStartFailed, opStart, err.Error(), err)
	}
	go child.watchContext()
	return child, nil
}

// PID returns the operating-system process id.
func (c *Child) PID() int { return c.pid }

// Elevated reports whether the process runs with administrator privileges; a
// locally started child never does.
func (c *Child) Elevated() bool { return false }

// Logs returns the merged stdout+stderr stream. The reader stays valid until Wait
// returns and must be closed by the caller.
func (c *Child) Logs() io.ReadCloser {
	return NewTailReader(c.ctx, c.cfg.LogPath, c.Exited)
}

// Wait blocks until the process exits, records StatusPath and returns its exit
// code. A negative code means the process was terminated by a signal.
func (c *Child) Wait() (int, error) {
	c.once.Do(c.collect)
	return c.code, c.waitErr
}

// collect reaps the child exactly once and writes the exit record.
func (c *Child) collect() {
	err := c.cmd.Wait()
	c.code, c.waitErr = exitStatus(err)
	close(c.done)
	status := Status{
		PID:       c.pid,
		ExitCode:  c.code,
		StartedAt: c.startedAt,
		EndedAt:   time.Now().UTC(),
	}
	// Only a real failure to run the child is an abnormal end. A non-zero exit
	// code is the program's own outcome and is already in ExitCode: recording
	// the raw wait error here made every ordinary non-zero exit — a refused
	// configuration, or a stop — read as a failure of the privileged helper
	// ("the privileged process ended abnormally: exit status 1") instead of
	// "sing-box exited with code 1" next to the reason from its log.
	if c.waitErr != nil {
		status.Error = c.waitErr.Error()
	}
	if werr := WriteStatus(c.cfg.StatusPath, status); werr != nil {
		if c.waitErr == nil {
			c.waitErr = apperr.Wrap(apperr.CodeInternal, opWait, werr.Error(), werr)
		}
	}
}

// Terminate asks the child to stop gracefully (SIGTERM / CTRL_BREAK).
func (c *Child) Terminate() error {
	if c.Exited() {
		return nil
	}
	return terminateOwn(c.pid)
}

// Kill terminates the child without waiting for cooperation.
func (c *Child) Kill() error {
	if c.Exited() {
		return nil
	}
	return killOwn(c.pid)
}

// Exited reports whether the process has already terminated.
func (c *Child) Exited() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// watchContext is the safety net behind explicit stops: when the context that
// started the child is cancelled the child is terminated, and killed if it does
// not react (spec §62: every goroutine is bound to a context). It ends as soon as
// the process exits.
func (c *Child) watchContext() {
	select {
	case <-c.done:
		return
	case <-c.ctx.Done():
	}
	_ = c.Terminate()
	timer := time.NewTimer(killGrace)
	defer timer.Stop()
	select {
	case <-c.done:
	case <-timer.C:
		_ = c.Kill()
	}
}

// StatusPath returns the exit-record path of the child.
func (c *Child) StatusPath() string { return c.cfg.StatusPath }

// PIDPath returns the identity-record path of the child.
func (c *Child) PIDPath() string { return c.cfg.PIDPath }

// LogPath returns the log path of the child.
func (c *Child) LogPath() string { return c.cfg.LogPath }

// IsExitError reports whether err is a non-zero child exit (which is not an
// error for the caller: the code carries the information).
func IsExitError(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}
