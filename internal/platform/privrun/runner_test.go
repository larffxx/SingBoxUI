package privrun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/platform/childrun"
	"github.com/larffxx/singboxui/internal/platform/privhelper"
	"github.com/larffxx/singboxui/internal/privilege"
)

// The operations of the helper command line, as privhelper spells them.
const (
	opStartWord = "start"
	opStopWord  = "stop"
)

func TestNewRefusesADataDirectoryOrElevationItCannotUse(t *testing.T) {
	elevator := &fakeElevator{helperPath: helperPath}
	dataDir := t.TempDir()
	for name, build := range map[string]func() (*Runner, error){
		"empty data directory":    func() (*Runner, error) { return New("", elevator) },
		"relative data directory": func() (*Runner, error) { return New("data", elevator) },
		"no elevator":             func() (*Runner, error) { return New(dataDir, nil) },
	} {
		t.Run(name, func(t *testing.T) {
			runner, err := build()
			if err == nil {
				t.Fatalf("New() accepted %s and returned %#v", name, runner)
			}
			if !apperr.IsCode(err, apperr.CodeInvalidArgument) {
				t.Errorf("New() = %v, want an INVALID_ARGUMENT error", err)
			}
		})
	}
	t.Run("absolute data directory and an elevator", func(t *testing.T) {
		runner, err := New(dataDir, elevator)
		if err != nil {
			t.Fatalf("New(%q): %v", dataDir, err)
		}
		if runner == nil {
			t.Fatal("New() returned no runner and no error")
		}
	})
}

func TestSupportedFollowsWhetherTheHelperIsInstalled(t *testing.T) {
	t.Run("helper installed", func(t *testing.T) {
		runner := newRunner(t, t.TempDir(), &fakeElevator{helperPath: helperPath})
		supported, reason := runner.Supported()
		if !supported || reason != "" {
			t.Errorf("Supported() = (%v, %q), want (true, \"\")", supported, reason)
		}
	})
	t.Run("helper missing", func(t *testing.T) {
		// The message the user should see is the reason the mechanism reports.
		missing := apperr.New(apperr.CodeRuntimeStartFailed, "platform.darwin.HelperPath",
			"the privileged helper is not installed beside the application")
		runner := newRunner(t, t.TempDir(), &fakeElevator{helperErr: missing})
		supported, reason := runner.Supported()
		if supported {
			t.Error("Supported() reported a privileged launch is possible without a helper")
		}
		if reason != apperr.MessageOf(missing) {
			t.Errorf("Supported() reason = %q, want %q", reason, apperr.MessageOf(missing))
		}
	})
}

func TestStartWithoutElevationSupervisesTheChild(t *testing.T) {
	t.Run("in the requested working directory", func(t *testing.T) {
		l := newLayout(t)
		asTestChild(t)
		runner := newRunner(t, l.dataDir, &fakeElevator{helperPath: helperPath})
		ctx := newTestContext(t)

		process, err := runner.Start(ctx, l.request(false))
		if err != nil {
			t.Fatalf("Start() = %v", err)
		}
		if process.Elevated() {
			t.Error("a launch without Elevate reported a privileged process")
		}
		waitUntilServing(t, l.log)
		if process.Exited() {
			t.Error("the process is reported as exited while the stand-in sing-box is running")
		}
		waitFor(t, 10*time.Second, "the pid file to record the child", func() bool {
			record, err := childrun.ReadPIDFile(l.pid)
			return err == nil && record.PID == process.PID() && process.PID() > 0
		})
		assertLogged(t, process, "fake sing-box: serving in "+l.workDir)
		assertLogged(t, process, "fake sing-box: configuration "+l.config)

		if err := process.Terminate(); err != nil {
			t.Fatalf("Terminate() = %v", err)
		}
		// Exited reports the reaped state, so the wait is what makes the stop
		// observable; it returns as soon as the stand-in sing-box is gone.
		code, err := process.Wait()
		switch {
		case err != nil:
			t.Errorf("Wait() = (%d, %v), want the exit code of the stand-in", code, err)
		case runtime.GOOS == "windows":
			// Windows has no signals: the stop is a console event the child cannot
			// handle, so the exit code is the platform's, not 0. What the caller
			// needs is asserted below — the process is gone and no longer reported
			// as running.
		case code != 0:
			t.Errorf("Wait() = (%d, nil), want 0", code)
		}
		if !process.Exited() {
			t.Error("the process is still reported as running after it exited")
		}
		record, err := childrun.ReadStatus(l.status)
		if err != nil {
			t.Fatalf("read the exit record: %v", err)
		}
		if record.PID != process.PID() {
			t.Errorf("the exit record names pid %d, want %d", record.PID, process.PID())
		}
	})

	t.Run("in the configuration directory when no working directory is requested", func(t *testing.T) {
		l := newLayout(t)
		asTestChild(t)
		runner := newRunner(t, l.dataDir, &fakeElevator{helperPath: helperPath})

		request := l.request(false)
		request.WorkDir = ""
		process, err := runner.Start(newTestContext(t), request)
		if err != nil {
			t.Fatalf("Start() = %v", err)
		}
		t.Cleanup(func() { _ = process.Kill() })
		waitUntilServing(t, l.log)
		assertLogged(t, process, "fake sing-box: serving in "+filepath.Dir(l.config))
	})
}

func TestStartRefusesARequestBeyondTheDataDirectoryBeforeElevating(t *testing.T) {
	l := newLayout(t)
	elevator := helperSession(t, l, os.Getpid())
	runner := newRunner(t, l.dataDir, elevator)

	outside := filepath.Join(filepath.Dir(l.dataDir), "elsewhere", "sing-box")
	for name, mutate := range map[string]func(*privilege.Request){
		"binary outside the data directory": func(r *privilege.Request) { r.BinaryPath = outside },
		"configuration outside":             func(r *privilege.Request) { r.ConfigPath = outside },
		"log outside":                       func(r *privilege.Request) { r.LogPath = outside },
		"pid file outside":                  func(r *privilege.Request) { r.PIDPath = outside },
		"status file outside":               func(r *privilege.Request) { r.StatusPath = outside },
		"relative binary":                   func(r *privilege.Request) { r.BinaryPath = "bin/sing-box" },
		"binary that is not sing-box":       func(r *privilege.Request) { r.BinaryPath = filepath.Join(filepath.Dir(l.binary), "singbox") },
	} {
		t.Run(name, func(t *testing.T) {
			request := l.request(true)
			mutate(&request)
			process, err := runner.Start(newTestContext(t), request)
			if err == nil {
				_ = process.Kill()
				t.Fatalf("Start() accepted a %s", name)
			}
			if !apperr.IsCode(err, apperr.CodeInvalidArgument) {
				t.Errorf("Start() = %v, want an INVALID_ARGUMENT error", err)
			}
			if calls := elevator.callCount(); calls != 0 {
				t.Errorf("the request reached the elevation mechanism %d time(s): %v", calls, elevator.calls())
			}
			if _, statErr := os.Stat(l.pid); statErr == nil {
				t.Error("a refused request left a pid file behind")
			}
		})
	}
}

func TestStartElevatedRunsTheHelperAsTheProtocolSpecifies(t *testing.T) {
	l := newLayout(t)
	const childPID = 4242
	elevator := helperSession(t, l, childPID)
	runner := newRunner(t, l.dataDir, elevator)

	request := l.request(true)
	process, err := runner.Start(newTestContext(t), request)
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	defer process.Kill()

	if !process.Elevated() {
		t.Error("Elevated() reported a launch asked for administrator rights as unprivileged")
	}
	if process.PID() != childPID {
		t.Errorf("PID() = %d, want the pid the helper recorded (%d)", process.PID(), childPID)
	}

	calls := elevator.calls()
	if len(calls) != 1 {
		t.Fatalf("the elevation mechanism was used %d time(s), want exactly one", len(calls))
	}
	argv := calls[0]
	if argv[0] != helperPath {
		t.Errorf("the elevated program is %q, want the helper %q", argv[0], helperPath)
	}
	if len(argv) < 2 || argv[1] != opStartWord {
		t.Errorf("the elevated invocation is %v, want the %q operation", argv, opStartWord)
	}
	// The launching side must hand the helper exactly what the helper would
	// validate and accept, which is what makes the boundary safe.
	want, err := privhelper.ParseStartArgs(privhelper.SpecFromRequest(request, l.dataDir).StartArgv(helperPath)[2:])
	if err != nil {
		t.Fatalf("the argument array the runner built is not a valid helper invocation: %v", err)
	}
	got, err := privhelper.ParseStartArgs(argv[2:])
	if err != nil {
		t.Fatalf("ParseStartArgs(%v): %v", argv[2:], err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the helper was asked to run\n%+v\nwant\n%+v", got, want)
	}
	if got.Reason != request.Reason {
		t.Errorf("the elevation reason is %q, want %q", got.Reason, request.Reason)
	}
}

func TestWaitReturnsTheExitCodeTheHelperRecorded(t *testing.T) {
	t.Run("a clean exit", func(t *testing.T) {
		l := newLayout(t)
		session := &fakeElevated{pid: os.Getpid()}
		elevator := &fakeElevator{helperPath: helperPath, handler: func([]string) (Elevated, error) {
			if err := childrun.WritePIDFile(l.pid, childrun.PIDFile{PID: 4243, StartTime: time.Now().Unix(), RecordedAt: time.Now()}); err != nil {
				t.Errorf("record the privileged process: %v", err)
			}
			return session, nil
		}}
		process, err := newRunner(t, l.dataDir, elevator).Start(newTestContext(t), l.request(true))
		if err != nil {
			t.Fatalf("Start() = %v", err)
		}
		if process.Exited() {
			t.Error("the process is reported as exited while its helper still runs")
		}
		if err := childrun.WriteStatus(l.status, childrun.Status{PID: 4243, ExitCode: 0}); err != nil {
			t.Fatalf("write the exit record: %v", err)
		}
		session.end(nil)
		code, err := process.Wait()
		if code != 0 || err != nil {
			t.Errorf("Wait() = (%d, %v), want (0, nil)", code, err)
		}
	})

	t.Run("a failed exit", func(t *testing.T) {
		l := newLayout(t)
		session := &fakeElevated{pid: os.Getpid()}
		elevator := &fakeElevator{helperPath: helperPath, handler: func([]string) (Elevated, error) {
			if err := childrun.WritePIDFile(l.pid, childrun.PIDFile{PID: 4244, StartTime: time.Now().Unix(), RecordedAt: time.Now()}); err != nil {
				t.Errorf("record the privileged process: %v", err)
			}
			return session, nil
		}}
		process, err := newRunner(t, l.dataDir, elevator).Start(newTestContext(t), l.request(true))
		if err != nil {
			t.Fatalf("Start() = %v", err)
		}
		if err := childrun.WriteStatus(l.status, childrun.Status{
			PID: 4244, ExitCode: 1, Error: "sing-box exited with code 1",
		}); err != nil {
			t.Fatalf("write the exit record: %v", err)
		}
		session.end(nil)
		code, err := process.Wait()
		if code != 1 {
			t.Errorf("Wait() = %d, want the recorded code 1", code)
		}
		if err == nil {
			t.Fatal("Wait() reported an abnormal exit as a success")
		}
		if !apperr.IsCode(err, apperr.CodeRuntimeStartFailed) {
			t.Errorf("Wait() = %v, want a RUNTIME_START_FAILED error", err)
		}
	})
}

func TestStartElevatedReportsADismissedPromptAsPrivilegeDenied(t *testing.T) {
	l := newLayout(t)
	elevator := &fakeElevator{helperPath: helperPath, handler: func([]string) (Elevated, error) {
		return nil, privilege.ErrCancelled
	}}
	process, err := newRunner(t, l.dataDir, elevator).Start(newTestContext(t), l.request(true))
	if err == nil {
		_ = process.Kill()
		t.Fatal("Start() reported success for a dismissed elevation prompt")
	}
	if !errors.Is(err, privilege.ErrCancelled) {
		t.Errorf("Start() = %v, want an error wrapping privilege.ErrCancelled", err)
	}
	if !apperr.IsCode(err, apperr.CodePrivilegeDenied) {
		t.Errorf("Start() = %v, want a PRIVILEGE_DENIED error", err)
	}
	if message := apperr.MessageOf(err); message == "" {
		t.Error("the dismissal has no message to show the user")
	}
}

func TestStartElevatedReportsAHelperThatEndedBeforeRecordingTheProcess(t *testing.T) {
	newElevator := func(t *testing.T, l layout) *fakeElevator {
		t.Helper()
		return &fakeElevator{helperPath: helperPath, handler: func([]string) (Elevated, error) {
			session := &fakeElevated{pid: os.Getpid(), exited: true}
			return session, nil
		}}
	}

	t.Run("without a reason recorded", func(t *testing.T) {
		l := newLayout(t)
		process, err := newRunner(t, l.dataDir, newElevator(t, l)).Start(newTestContext(t), l.request(true))
		if err == nil {
			_ = process.Kill()
			t.Fatal("Start() reported success although the helper never recorded a process")
		}
		if !apperr.IsCode(err, apperr.CodeRuntimeStartFailed) {
			t.Errorf("Start() = %v, want a RUNTIME_START_FAILED error", err)
		}
		if message := apperr.MessageOf(err); !strings.Contains(message, "could not start sing-box") {
			t.Errorf("Start() message = %q, want it to explain that sing-box could not be started", message)
		}
	})

	t.Run("with the reason the helper recorded", func(t *testing.T) {
		l := newLayout(t)
		// The reason is written by the helper, so it can only appear after the
		// launch started: a status file written before it is cleared as stale.
		elevator := &fakeElevator{helperPath: helperPath, handler: func([]string) (Elevated, error) {
			if err := childrun.WriteStatus(l.status, childrun.Status{
				ExitCode: 1, Error: "sing-box refused to start: TUN device is unavailable",
				EndedAt: time.Now().UTC(),
			}); err != nil {
				t.Errorf("write the exit record: %v", err)
			}
			return &fakeElevated{pid: os.Getpid(), exited: true}, nil
		}}
		process, err := newRunner(t, l.dataDir, elevator).Start(newTestContext(t), l.request(true))
		if err == nil {
			_ = process.Kill()
			t.Fatal("Start() reported success although the helper never recorded a process")
		}
		if message := apperr.MessageOf(err); !strings.Contains(message, "TUN device is unavailable") {
			t.Errorf("Start() message = %q, want the reason the helper recorded", message)
		}
	})

	t.Run("because the user dismissed a later prompt", func(t *testing.T) {
		l := newLayout(t)
		elevator := &fakeElevator{helperPath: helperPath, handler: func([]string) (Elevated, error) {
			return &fakeElevated{pid: os.Getpid(), exited: true, status: privilege.ErrCancelled}, nil
		}}
		process, err := newRunner(t, l.dataDir, elevator).Start(newTestContext(t), l.request(true))
		if err == nil {
			_ = process.Kill()
			t.Fatal("Start() reported success although the helper ended")
		}
		if !apperr.IsCode(err, apperr.CodePrivilegeDenied) {
			t.Errorf("Start() = %v, want a PRIVILEGE_DENIED error", err)
		}
	})

	t.Run("the session is released", func(t *testing.T) {
		l := newLayout(t)
		session := &fakeElevated{pid: os.Getpid(), exited: true}
		elevator := &fakeElevator{helperPath: helperPath, handler: func([]string) (Elevated, error) {
			return session, nil
		}}
		process, err := newRunner(t, l.dataDir, elevator).Start(newTestContext(t), l.request(true))
		if err == nil {
			_ = process.Kill()
			t.Fatal("Start() reported success although the helper ended")
		}
		if session.releaseCount() == 0 {
			t.Error("a failed privileged launch left its elevation session open")
		}
	})
}

func TestStartElevatedGivesUpWhenTheCallerGoesAway(t *testing.T) {
	l := newLayout(t)
	session := &fakeElevated{pid: os.Getpid()}
	elevator := &fakeElevator{helperPath: helperPath, handler: func([]string) (Elevated, error) {
		// A helper that starts but never records anything, as a hung one would.
		return session, nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	process, err := newRunner(t, l.dataDir, elevator).Start(ctx, l.request(true))
	if err == nil {
		_ = process.Kill()
		t.Fatal("Start() reported success although the helper never recorded a process")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Start() = %v, want an error wrapping the context's failure", err)
	}
	if !apperr.IsCode(err, apperr.CodeAppShuttingDown) {
		t.Errorf("Start() = %v, want an APP_SHUTTING_DOWN error", err)
	}
	if session.releaseCount() == 0 {
		t.Error("a cancelled privileged launch left its elevation session open")
	}
}

func TestStartElevatedRefusesAHelperThatIsNotInstalled(t *testing.T) {
	l := newLayout(t)
	elevator := &fakeElevator{helperErr: apperr.New(apperr.CodeRuntimeStartFailed,
		"platform.darwin.HelperPath", "the privileged helper is not installed beside the application")}
	process, err := newRunner(t, l.dataDir, elevator).Start(newTestContext(t), l.request(true))
	if err == nil {
		_ = process.Kill()
		t.Fatal("Start() reported success without a helper to run")
	}
	if calls := elevator.callCount(); calls != 0 {
		t.Errorf("the elevation mechanism was used %d time(s) although there is no helper", calls)
	}
}

func TestTerminateAndKillElevateAStopRequestOnlyForALiveProcess(t *testing.T) {
	t.Run("a process that is gone needs no elevation", func(t *testing.T) {
		l := newLayout(t)
		// 1 is not a live pid the test owns, but it is not the test's own pid
		// either: use a pid that cannot be running, then check that nothing ran.
		elevator := helperSession(t, l, 39393939)
		process, err := newRunner(t, l.dataDir, elevator).Start(newTestContext(t), l.request(true))
		if err != nil {
			t.Fatalf("Start() = %v", err)
		}
		before := elevator.callCount()
		if err := process.Terminate(); err != nil {
			t.Errorf("Terminate() of an already stopped process = %v, want nil", err)
		}
		if err := process.Kill(); err != nil {
			t.Errorf("Kill() of an already stopped process = %v, want nil", err)
		}
		if calls := elevator.callCount(); calls != before {
			t.Errorf("stopping a process that is not running used the elevation mechanism %d more time(s)", calls-before)
		}
	})

	t.Run("a live process is stopped through the helper", func(t *testing.T) {
		l := newLayout(t)
		// The test's own process is a live process that must not be signalled:
		// the fake elevator only records the invocation.
		startSession := &fakeElevated{pid: os.Getpid()}
		elevator := &fakeElevator{helperPath: helperPath}
		elevator.handler = func(argv []string) (Elevated, error) {
			switch operationArg(argv) {
			case opStartWord:
				if err := childrun.WritePIDFile(l.pid, childrun.PIDFile{
					PID: os.Getpid(), StartTime: time.Now().Unix(), RecordedAt: time.Now(),
				}); err != nil {
					t.Errorf("record the privileged process: %v", err)
				}
				return startSession, nil
			case opStopWord:
				return &fakeElevated{pid: os.Getpid(), exited: true}, nil
			default:
				t.Errorf("the helper was invoked as %v, want start or stop", argv)
				return nil, errors.New("unexpected invocation")
			}
		}
		process, err := newRunner(t, l.dataDir, elevator).Start(newTestContext(t), l.request(true))
		if err != nil {
			t.Fatalf("Start() = %v", err)
		}
		if err := process.Terminate(); err != nil {
			t.Fatalf("Terminate() = %v", err)
		}

		calls := elevator.calls()
		if len(calls) != 2 {
			t.Fatalf("the elevation mechanism was used %d time(s), want a start and a stop: %v", len(calls), calls)
		}
		stop := calls[1]
		if operationArg(stop) != opStopWord {
			t.Errorf("the second invocation is %v, want the %q operation", stop, opStopWord)
		}
		want, err := privhelper.ParseStopArgs(privhelper.StopArgv(helperPath, l.dataDir, l.pid, false)[2:])
		if err != nil {
			t.Fatalf("StopArgv() is not a valid invocation: %v", err)
		}
		got, err := privhelper.ParseStopArgs(stop[2:])
		if err != nil {
			t.Fatalf("ParseStopArgs(%v): %v", stop[2:], err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("the helper was asked to stop with\n%+v\nwant\n%+v", got, want)
		}
		if got.Force {
			t.Error("a graceful stop asked the helper to force the process down")
		}
	})

	t.Run("a kill asks for force", func(t *testing.T) {
		l := newLayout(t)
		startSession := &fakeElevated{pid: os.Getpid()}
		elevator := &fakeElevator{helperPath: helperPath}
		elevator.handler = func(argv []string) (Elevated, error) {
			if operationArg(argv) == opStartWord {
				if err := childrun.WritePIDFile(l.pid, childrun.PIDFile{
					PID: os.Getpid(), StartTime: time.Now().Unix(), RecordedAt: time.Now(),
				}); err != nil {
					t.Errorf("record the privileged process: %v", err)
				}
				return startSession, nil
			}
			return &fakeElevated{pid: os.Getpid(), exited: true}, nil
		}
		process, err := newRunner(t, l.dataDir, elevator).Start(newTestContext(t), l.request(true))
		if err != nil {
			t.Fatalf("Start() = %v", err)
		}
		if err := process.Kill(); err != nil {
			t.Fatalf("Kill() = %v", err)
		}
		calls := elevator.calls()
		if len(calls) != 2 {
			t.Fatalf("the elevation mechanism was used %d time(s), want a start and a kill: %v", len(calls), calls)
		}
		stop, err := privhelper.ParseStopArgs(calls[1][2:])
		if err != nil {
			t.Fatalf("ParseStopArgs(%v): %v", calls[1][2:], err)
		}
		if !stop.Force {
			t.Error("Kill() did not ask the helper to force the process down")
		}
	})
}

// operationArg returns the helper operation of an invocation, or the empty
// string when the invocation is too short to have one.
func operationArg(argv []string) string {
	if len(argv) < 2 {
		return ""
	}
	return argv[1]
}

// assertLogged reads the process's log stream once and requires it to contain
// want. The reader is closed again so a running child is not left with an open
// follower (spec §62).
func assertLogged(t *testing.T, process privilege.Process, want string) {
	t.Helper()
	logs := process.Logs()
	defer logs.Close()
	deadline := time.Now().Add(30 * time.Second)
	buf := make([]byte, 64*1024)
	seen := ""
	for time.Now().Before(deadline) {
		n, err := logs.Read(buf)
		seen += string(buf[:n])
		if strings.Contains(seen, want) {
			return
		}
		if err != nil && err.Error() != "EOF" {
			t.Fatalf("read the log stream: %v", err)
		}
	}
	t.Errorf("the log stream does not contain %q; it holds %q", want, seen)
}
