//go:build windows

package windows

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/platform/privhelper"
	"github.com/larffxx/singboxui/internal/platform/privrun"
	"github.com/larffxx/singboxui/internal/privilege"
	"golang.org/x/sys/windows"
)

// Constants of the elevation call. They are declared here rather than imported so
// the ABI this code depends on is visible in one place.
const (
	// seeMaskNoCloseProcess (SEE_MASK_NOCLOSEPROCESS) keeps the process handle of
	// the elevated program open, which is how its lifetime is watched.
	seeMaskNoCloseProcess = 0x00000040
	// swShowNormal is the show command for the elevated program.
	swShowNormal = 1
	// waitObject0 is the WAIT_OBJECT_0 result of WaitForSingleObject: the object
	// is signalled, that is, the process has ended.
	waitObject0 = 0x00000000
	// stillActive is STILL_ACTIVE from <winnt.h>.
	stillActive = 259
	// cancelledError is ERROR_CANCELLED, returned by ShellExecuteEx when the user
	// dismisses the elevation prompt.
	cancelledError = 1223
)

// shellExecuteInfoW mirrors SHELLEXECUTEINFOW from <shellapi.h>. Field order and
// the padding the compiler inserts after the int32 fields are the ABI of that
// structure, which is asserted by TestShellExecuteInfoLayout.
type shellExecuteInfoW struct {
	cbSize         uint32
	fMask          uint32
	hwnd           uintptr
	lpVerb         *uint16
	lpFile         *uint16
	lpParameters   *uint16
	lpDirectory    *uint16
	nShow          int32
	hInstApp       uintptr
	lpIDList       uintptr
	lpClass        *uint16
	hkeyClass      uintptr
	dwHotKey       uint32
	hIconOrMonitor uintptr
	hProcess       uintptr
}

// Elevator runs the privileged helper with administrator rights through
// ShellExecuteExW with the "runas" verb (target-state.md §7): the helper process
// itself runs elevated and supervises sing-box as its child.
type Elevator struct{}

// A compile-time check that the elevator fulfils the port privrun expects.
var _ privrun.Elevator = (*Elevator)(nil)

// NewElevator returns the runas elevator.
func NewElevator() *Elevator { return &Elevator{} }

// HelperPath returns the absolute path of the privileged helper.
func (e *Elevator) HelperPath() (string, error) { return HelperPath() }

// Elevate starts one helper invocation and returns the session that watches it.
//
// argv[0] is the helper and the remaining elements are its arguments. They are
// joined for ShellExecuteEx as an argument list, never as a shell command line
// (spec §24, §83): the helper parses its own arguments and refuses anything it does
// not recognise (internal-contracts.md §3).
func (e *Elevator) Elevate(ctx context.Context, argv []string) (privrun.Elevated, error) {
	if len(argv) == 0 {
		return nil, apperr.New(apperr.CodeInvalidArgument, opElevate, "no helper invocation was given")
	}
	if !filepath.IsAbs(argv[0]) {
		return nil, apperr.Newf(apperr.CodeInvalidArgument, opElevate, "the helper path must be absolute, got %q", argv[0])
	}
	for _, arg := range argv {
		if hasControl(arg) {
			return nil, apperr.New(apperr.CodeInvalidArgument, opElevate, "an argument of the helper invocation contains control characters")
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, apperr.Wrap(apperr.CodeAppShuttingDown, opElevate, "the elevation request was cancelled before it started", err)
	}
	handle, err := shellExecuteRunAs(argv)
	if err != nil {
		return nil, err
	}
	return &session{handle: handle}, nil
}

// shellExecuteRunAs performs the one Win32 call this package makes and returns the
// process handle of the elevated program.
func shellExecuteRunAs(argv []string) (windows.Handle, error) {
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return 0, apperr.Wrap(apperr.CodeInvalidArgument, opElevate, "the elevation verb cannot be encoded", err)
	}
	file, err := windows.UTF16PtrFromString(argv[0])
	if err != nil {
		return 0, apperr.Wrap(apperr.CodeInvalidArgument, opElevate, "the helper path cannot be encoded", err)
	}
	parameters, err := windows.UTF16PtrFromString(joinArgs(argv[1:]))
	if err != nil {
		return 0, apperr.Wrap(apperr.CodeInvalidArgument, opElevate, "the helper arguments cannot be encoded", err)
	}
	info := shellExecuteInfoW{
		cbSize:       uint32(unsafe.Sizeof(shellExecuteInfoW{})),
		fMask:        seeMaskNoCloseProcess,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: parameters,
		nShow:        swShowNormal,
	}
	// The DLL is looked up per call so this package holds no process-wide state
	// (spec §62); the loader returns the already-loaded module.
	shell32 := windows.NewLazySystemDLL("shell32.dll")
	proc := shell32.NewProc("ShellExecuteExW")
	result, _, callErr := proc.Call(uintptr(unsafe.Pointer(&info)))
	if result == 0 {
		code := errorCode(callErr)
		if code == cancelledError {
			return 0, privilege.ErrCancelled
		}
		if code != 0 {
			return 0, apperr.Wrap(apperr.CodeRuntimeStartFailed, opElevate, "Windows could not start the privileged helper (error "+strconv.Itoa(int(code))+")", callErr)
		}
		return 0, apperr.Wrap(apperr.CodeRuntimeStartFailed, opElevate, "Windows could not start the privileged helper", callErr)
	}
	if info.hProcess == 0 {
		return 0, apperr.New(apperr.CodeRuntimeStartFailed, opElevate, "Windows started the privileged helper without reporting its handle")
	}
	return windows.Handle(info.hProcess), nil
}

// errorCode extracts the Win32 error of a failed system call.
func errorCode(err error) uintptr {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return uintptr(errno)
	}
	if last := windows.GetLastError(); last != nil {
		if errno, ok := last.(syscall.Errno); ok {
			return uintptr(errno)
		}
	}
	return 0
}

// joinArgs builds the argument string of ShellExecuteExW from an argument list.
// Every element is quoted with the rule the C runtime uses, so an argument that
// contains spaces stays one argument.
func joinArgs(args []string) string {
	line := ""
	for i, arg := range args {
		if i > 0 {
			line += " "
		}
		line += syscall.EscapeArg(arg)
	}
	return line
}

// session is the launcher of one privileged helper invocation: the elevated helper
// process, which lives exactly as long as the sing-box child it supervises.
type session struct {
	handle windows.Handle

	mu       sync.Mutex
	released bool
}

// PID reports the process id of the elevated helper, not of sing-box: the pid of
// the child is only ever read from the pid file (internal-contracts.md §3).
func (s *session) PID() int {
	id, err := windows.GetProcessId(s.handle)
	if err != nil {
		return 0
	}
	return int(id)
}

// Exited reports whether the elevated helper has ended.
func (s *session) Exited() bool {
	event, err := windows.WaitForSingleObject(s.handle, 0)
	return err == nil && event == waitObject0
}

// Status reports how the elevation ended: nil when the helper exited successfully,
// privilege.ErrCancelled when the prompt was dismissed (reported by Elevate), and
// an error describing the helper's exit code otherwise. It is only meaningful once
// Exited is true.
func (s *session) Status() error {
	s.mu.Lock()
	released := s.released
	s.mu.Unlock()
	if released || !s.Exited() {
		return nil
	}
	var code uint32
	if err := windows.GetExitCodeProcess(s.handle, &code); err != nil {
		return apperr.Wrap(apperr.CodeRuntimeStartFailed, opElevate, "the exit status of the privileged helper cannot be read", err)
	}
	if stillActiveCode(code) {
		// The helper has not ended after all: STILL_ACTIVE is not an exit code
		// and interpreting it as one would invent a failure.
		return nil
	}
	switch code {
	case 0:
		return nil
	case uint32(privhelper.ExitUsage):
		return apperr.New(apperr.CodeRuntimeStartFailed, opElevate, "the privileged helper refused the request as invalid (exit code "+strconv.Itoa(int(code))+")")
	case uint32(privhelper.ExitFailure):
		return apperr.New(apperr.CodeRuntimeStartFailed, opElevate, "the privileged helper reported a failure (exit code "+strconv.Itoa(int(code))+")")
	default:
		return apperr.New(apperr.CodeRuntimeStartFailed, opElevate, "the privileged helper exited with code "+strconv.Itoa(int(code)))
	}
}

// Release ends the session: a helper that is still running is asked to stop, so it
// can take its sing-box child with it, and is terminated if it does not.
func (s *session) Release() {
	s.mu.Lock()
	s.released = true
	s.mu.Unlock()
	if s.Exited() {
		_ = windows.CloseHandle(s.handle)
		return
	}
	// CTRL_BREAK reaches the helper when it shares a console with this process;
	// the helper turns it into the same cancellation as any other shutdown.
	if pid := s.PID(); pid > 0 {
		_ = windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(pid))
	}
	_ = windows.TerminateProcess(s.handle, 1)
	_ = windows.CloseHandle(s.handle)
}

// stillActiveCode reports whether an exit code means "the process is still
// running"; it is the value GetExitCodeProcess returns for a live process.
func stillActiveCode(code uint32) bool { return code == stillActive }
