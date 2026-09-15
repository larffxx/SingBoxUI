package privrun

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/larffxx/singboxui/internal/platform/childrun"
)

// executableName is the sing-box file name of a platform. The helper protocol
// insists on it (internal-contracts.md §3), so the fixtures have to use it too.
func executableName(goos string) string {
	if goos == "windows" {
		return "sing-box.exe"
	}
	return "sing-box"
}

// fakeElevator stands in for osascript (macOS) or ShellExecuteEx (Windows): it
// records every invocation it is asked to run instead of asking a real user for
// administrator rights, which a test cannot do.
type fakeElevator struct {
	helperPath string
	helperErr  error

	mu      sync.Mutex
	argvs   [][]string
	handler func(argv []string) (Elevated, error)
}

// HelperPath reports the helper the application would run.
func (f *fakeElevator) HelperPath() (string, error) {
	if f.helperErr != nil {
		return "", f.helperErr
	}
	return f.helperPath, nil
}

// Elevate records the invocation and hands it to the test's handler.
func (f *fakeElevator) Elevate(_ context.Context, argv []string) (Elevated, error) {
	f.mu.Lock()
	f.argvs = append(f.argvs, append([]string(nil), argv...))
	handler := f.handler
	f.mu.Unlock()
	if handler == nil {
		return nil, errors.New("fake elevator: no handler installed")
	}
	return handler(argv)
}

// calls returns a copy of every invocation the elevator was asked to run.
func (f *fakeElevator) calls() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]string, len(f.argvs))
	copy(out, f.argvs)
	return out
}

// callCount reports how often elevation was asked for.
func (f *fakeElevator) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.argvs)
}

// fakeElevated is a running elevated program the test drives by hand: it decides
// when the elevated helper has exited and why.
type fakeElevated struct {
	mu       sync.Mutex
	pid      int
	exited   bool
	status   error
	releases int
}

func (e *fakeElevated) PID() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.pid
}

func (e *fakeElevated) Exited() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.exited
}

func (e *fakeElevated) Status() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status
}

func (e *fakeElevated) Release() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.releases++
}

// releaseCount reports how often the session was released.
func (e *fakeElevated) releaseCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.releases
}

// end makes the elevated program report that it has exited.
func (e *fakeElevated) end(status error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.exited = true
	e.status = status
}

// helperSession returns an elevator that behaves like a helper which starts
// sing-box and records it with the given pid.
func helperSession(t *testing.T, l layout, pid int) *fakeElevator {
	t.Helper()
	session := &fakeElevated{pid: os.Getpid()}
	return &fakeElevator{
		helperPath: helperPath,
		handler: func([]string) (Elevated, error) {
			if err := childrun.WritePIDFile(l.pid, childrun.PIDFile{
				PID:        pid,
				StartTime:  time.Now().Unix(),
				BinaryPath: l.binary,
				RecordedAt: time.Now(),
			}); err != nil {
				t.Errorf("record the privileged process: %v", err)
			}
			return session, nil
		},
	}
}

// newRunner returns the runner of a layout, with the given elevator.
func newRunner(t *testing.T, dataDir string, elevator Elevator) *Runner {
	t.Helper()
	runner, err := New(dataDir, elevator)
	if err != nil {
		t.Fatalf("New(%q): %v", dataDir, err)
	}
	return runner
}

// newTestContext returns a context that outlives the test whatever the test does,
// and is cancelled before the test ends.
func newTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}
