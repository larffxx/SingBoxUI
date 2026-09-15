//go:build windows

package windows

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/platform/privhelper"
	"golang.org/x/sys/windows"
)

func TestJoinArgsQuotesEveryArgument(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no arguments", nil, ""},
		{"one plain argument", []string{"start"}, "start"},
		{
			"an argument with a space",
			[]string{"--data-dir", `C:\Users\a b\.singboxui`},
			`--data-dir "C:\Users\a b\.singboxui"`,
		},
		{
			"an empty argument",
			[]string{"--reason", ""},
			`--reason ""`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := joinArgs(tc.args); got != tc.want {
				t.Errorf("joinArgs(%q) = %s, want %s", tc.args, got, tc.want)
			}
		})
	}

	// Every element of a real invocation survives as one argument: none of them is
	// dropped and none of them can be read as two (spec §24, spec §83).
	args := []string{
		"start",
		"--data-dir", `C:\Users\a b\.singboxui`,
		"--config", `C:\Users\a b\.singboxui\config\run.json`,
		"--reason", "TUN mode needs administrator rights",
	}
	line := joinArgs(args)
	for _, arg := range args {
		if !strings.Contains(line, syscall.EscapeArg(arg)) {
			t.Errorf("joinArgs(%q) = %s, want %q in it", args, line, syscall.EscapeArg(arg))
		}
	}
	if strings.Contains(line, `C:\Users\a b\.singboxui `) {
		t.Errorf("joinArgs(%q) = %s, want no unquoted path with a space", args, line)
	}
}

func TestErrorCodeReadsTheWin32CodeOfAFailedCall(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want uintptr
	}{
		{"an errno", syscall.Errno(5), 5},
		{"a wrapped errno", fmt.Errorf("ShellExecuteEx: %w", syscall.Errno(1223)), 1223},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := errorCode(tc.err); got != tc.want {
				t.Errorf("errorCode(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
	// A dismissal of the administrator prompt is the code the caller turns into
	// privilege.ErrCancelled.
	if got := errorCode(syscall.Errno(cancelledError)); got != cancelledError {
		t.Errorf("errorCode() = %d, want the cancelled code %d", got, cancelledError)
	}
	// An error that carries no code at all is not a code either: whatever the last
	// call of this thread reported is not this error's.
	if got := errorCode(errors.New("no code")); got != 0 {
		if _, isErrno := windows.GetLastError().(syscall.Errno); !isErrno {
			t.Errorf("errorCode() = %d, want 0 for an error without a code", got)
		}
	}
}

func TestStillActiveIsNotAnExitCode(t *testing.T) {
	// STILL_ACTIVE is what GetExitCodeProcess reports for a process that is still
	// running; reading it as an exit code would invent a failure.
	if stillActive != 259 {
		t.Errorf("stillActive = %d, want 259", stillActive)
	}
	if !stillActiveCode(stillActive) {
		t.Error("stillActiveCode(259) = false, want true")
	}
	for _, code := range []uint32{0, 1, 2, uint32(privhelper.ExitUsage), uint32(privhelper.ExitFailure)} {
		if stillActiveCode(code) {
			t.Errorf("stillActiveCode(%d) = true, want false", code)
		}
	}
}

func TestSessionReportsAProcessThatIsStillRunning(t *testing.T) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION, false, uint32(os.Getpid()))
	if err != nil {
		t.Fatalf("OpenProcess() = %v", err)
	}
	// The handle is closed directly: Release would end the process this test runs
	// in, which is the one thing it must not do.
	t.Cleanup(func() { _ = windows.CloseHandle(handle) })
	session := &session{handle: handle}

	if got := session.PID(); got != os.Getpid() {
		t.Errorf("PID() = %d, want %d", got, os.Getpid())
	}
	if session.Exited() {
		t.Error("Exited() = true for a process that is running")
	}
	// Nothing has ended, so there is nothing to report.
	if status := session.Status(); status != nil {
		t.Errorf("Status() = %v, want nil while the process is running", status)
	}
}

func TestElevateRefusesAnInvocationItCannotStart(t *testing.T) {
	elevator := NewElevator()
	if elevator == nil {
		t.Fatal("NewElevator() = nil")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, tc := range []struct {
		name string
		argv []string
	}{
		{"nothing", nil},
		{"a helper path that is not absolute", []string{"singboxui-priv.exe", "start"}},
		{"an argument with a newline", []string{`C:\SingBoxUI\singboxui-priv.exe`, "start\nrm -rf /"}},
		{"an argument with a control character", []string{`C:\SingBoxUI\singboxui-priv.exe`, "start\x00"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session, err := elevator.Elevate(ctx, tc.argv)
			if err == nil {
				session.Release()
				t.Fatalf("Elevate(%q) = nil, want an error", tc.argv)
			}
			if !apperr.IsCode(err, apperr.CodeInvalidArgument) {
				t.Errorf("Elevate(%q) = %v, want %v", tc.argv, err, apperr.CodeInvalidArgument)
			}
		})
	}

	// A request that arrives after the application has started shutting down never
	// reaches Windows: no administrator prompt appears for it.
	t.Run("a cancelled request", func(t *testing.T) {
		cancelled, cancelNow := context.WithCancel(context.Background())
		cancelNow()
		session, err := elevator.Elevate(cancelled, []string{`C:\SingBoxUI\singboxui-priv.exe`, "start"})
		if err == nil {
			session.Release()
			t.Fatal("Elevate() = nil, want an error for a cancelled context")
		}
		if !apperr.IsCode(err, apperr.CodeAppShuttingDown) {
			t.Errorf("Elevate() = %v, want %v", err, apperr.CodeAppShuttingDown)
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Elevate() = %v, want it to carry the cancellation", err)
		}
	})
}

func TestElevatorHelperPathIsTheHelperBesideTheApplication(t *testing.T) {
	elevator := NewElevator()
	path, err := elevator.HelperPath()
	if err == nil {
		if base := strings.ToLower(path[strings.LastIndex(path, `\`)+1:]); base != HelperExecutable {
			t.Errorf("HelperPath() = %q, want the file name %q", path, HelperExecutable)
		}
		return
	}
	// Without a helper next to the test binary the elevator has to say so, rather
	// than name a path that would only fail once Windows is asked to run it.
	if !apperr.IsCode(err, apperr.CodeBinaryNotFound) {
		t.Errorf("HelperPath() = %v, want %v when the helper is not installed", err, apperr.CodeBinaryNotFound)
	}
}
