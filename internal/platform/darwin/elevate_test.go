//go:build darwin

package darwin

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/privilege"
)

// interpreter stands in for /usr/bin/osascript: it writes the arguments it was
// started with to a file and then runs body, so a test can see exactly what the
// elevator told it to do without a password dialog appearing.
func interpreter(t *testing.T, body string) (path string, args string) {
	t.Helper()
	dir := t.TempDir()
	args = filepath.Join(dir, "argv.txt")
	path = filepath.Join(dir, "interpreter")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + args + "'\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) = %v", path, err)
	}
	return path, args
}

// waitForExited waits for a session to end, and fails the test if it does not.
func waitForExited(t *testing.T, session interface{ Exited() bool }) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !session.Exited() {
		if time.Now().After(deadline) {
			t.Fatal("the elevated session did not end")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPosixQuoteMakesOneShellWordOfAnything(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", "''"},
		{"plain", "'plain'"},
		{"a b", "'a b'"},
		{"it's", `'it'\''s'`},
		{"$(rm -rf /)", "'$(rm -rf /)'"},
		{"`whoami`", "'`whoami`'"},
		{"a;b|c&d", "'a;b|c&d'"},
		{"*", "'*'"},
		{"line\nbreak", "'line\nbreak'"},
	} {
		if got := PosixQuote(tc.in); got != tc.want {
			t.Errorf("PosixQuote(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestLaunchScriptIsTheOnePlaceAnArgvBecomesAShellCommand(t *testing.T) {
	// Arguments that a shell would otherwise interpret: a space, a quote, a
	// command substitution, a glob, an empty word.
	marker := filepath.Join(t.TempDir(), "escaped")
	argv := []string{
		"/opt/SingBoxUI/singboxui-priv",
		"start",
		"--data-dir",
		"/tmp/SingBox UI/data",
		"--config",
		"/tmp/it's/a.json",
		"$(touch " + marker + ")",
		"*",
		"",
	}
	script := LaunchScript(argv)
	for _, arg := range argv {
		if !strings.Contains(script, PosixQuote(arg)) {
			t.Errorf("LaunchScript(%q) = %s, want every element quoted", argv, script)
		}
	}

	// Run the script through a real /bin/sh and read back what it received: this
	// is what osascript does with it. set -- takes the words as they are parsed,
	// so a word that was not quoted properly is expanded here.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	probe := "set -- " + script + "; printf '%s\\n' \"$@\""
	out, err := exec.CommandContext(ctx, "/bin/sh", "-c", probe).Output()
	if err != nil {
		t.Fatalf("/bin/sh -c %q = %v", probe, err)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(got) != len(argv) {
		t.Fatalf("the shell received %d arguments (%q), want %d", len(got), got, len(argv))
	}
	for i := range argv {
		if got[i] != argv[i] {
			t.Errorf("argument %d reached the shell as %q, want %q", i, got[i], argv[i])
		}
	}
	// Nothing the arguments name was executed: the command substitution stayed an
	// argument (spec §24, spec §83).
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("the command substitution in the arguments was executed: %s exists", marker)
	}
}

func TestAppleScriptAsksForAdministratorRights(t *testing.T) {
	got := AppleScript(`say "hi" \ bye`)
	want := `do shell script "say \"hi\" \\ bye" with administrator privileges`
	if got != want {
		t.Errorf("AppleScript() = %s, want %s", got, want)
	}
	if !strings.HasSuffix(got, "with administrator privileges") {
		t.Errorf("AppleScript() = %s, want it to end in the request for rights", got)
	}
}

func TestElevateRunsTheHelperWithExactlyTheArgumentsGiven(t *testing.T) {
	interpreterPath, argsPath := interpreter(t, "exit 0")
	elevator := NewElevatorWith(interpreterPath)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	argv := []string{
		"/opt/SingBoxUI/singboxui-priv",
		"start",
		"--data-dir",
		"/tmp/SingBox UI/data",
		"--work-dir",
		"/tmp/SingBox UI/work",
		"--reason",
		"TUN mode needs administrator rights",
	}
	session, err := elevator.Elevate(ctx, argv)
	if err != nil {
		t.Fatalf("Elevate() = %v", err)
	}
	t.Cleanup(session.Release)
	if session.PID() <= 0 {
		t.Errorf("PID() = %d, want the process that was started", session.PID())
	}
	waitForExited(t, session)
	if status := session.Status(); status != nil {
		t.Errorf("Status() = %v, want nil for an interpreter that exited zero", status)
	}

	written, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) = %v", argsPath, err)
	}
	got := strings.Split(strings.TrimSuffix(string(written), "\n"), "\n")
	if len(got) != 2 {
		t.Fatalf("the interpreter was started with %d arguments (%q), want 2", len(got), got)
	}
	if got[0] != "-e" {
		t.Errorf("the first argument of the interpreter is %q, want \"-e\"", got[0])
	}
	if want := AppleScript(LaunchScript(argv)); got[1] != want {
		t.Errorf("the script handed to the interpreter is\n%s\nwant\n%s", got[1], want)
	}
	// The helper is named by the caller, as an argument, and never by a string
	// that was concatenated somewhere else (spec §24, §83).
	if !strings.Contains(got[1], "'/opt/SingBoxUI/singboxui-priv'") {
		t.Errorf("the script does not name the helper as one word: %s", got[1])
	}
}

func TestElevateRefusesAnInvocationItCannotQuoteSafely(t *testing.T) {
	interpreterPath, argsPath := interpreter(t, "exit 0")
	elevator := NewElevatorWith(interpreterPath)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, tc := range []struct {
		name string
		argv []string
	}{
		{"nothing", nil},
		{"a helper path that is not absolute", []string{"singboxui-priv", "start"}},
		{"an argument with a newline", []string{"/opt/SingBoxUI/singboxui-priv", "start\nrm -rf /"}},
		{"an argument with a control character", []string{"/opt/SingBoxUI/singboxui-priv", "start\x00"}},
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
			if _, err := os.Stat(argsPath); err == nil {
				t.Error("the interpreter was started for a refused invocation")
				_ = os.Remove(argsPath)
			}
		})
	}
}

func TestElevateEndsWithTheContext(t *testing.T) {
	interpreterPath, _ := interpreter(t, "sleep 30")
	elevator := NewElevatorWith(interpreterPath)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	session, err := elevator.Elevate(ctx, []string{"/opt/SingBoxUI/singboxui-priv", "start"})
	if err != nil {
		t.Fatalf("Elevate() = %v", err)
	}
	t.Cleanup(session.Release)
	waitForExited(t, session)
	status := session.Status()
	if status == nil {
		t.Fatal("Status() = nil, want the cancellation to be reported")
	}
	// A cancelled context is not the user dismissing the dialog.
	if errors.Is(status, privilege.ErrCancelled) {
		t.Errorf("Status() = %v, want an execution failure rather than a dismissal", status)
	}
	if !strings.Contains(status.Error(), "osascript") {
		t.Errorf("Status() = %v, want it to name what failed", status)
	}
}

func TestStatusReportsThatTheUserDismissedTheDialog(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want error
	}{
		{"the numeric code of a dismissed dialog", `printf '%s\n' "` + cancelCode + `" >&2; exit 1`, privilege.ErrCancelled},
		{"a localised message", `printf '%s\n' "User canceled." >&2; exit 1`, privilege.ErrCancelled},
		{"a message that is not a dismissal", `printf '%s\n' "The operation is not permitted." >&2; exit 1`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			interpreterPath, _ := interpreter(t, tc.body)
			elevator := NewElevatorWith(interpreterPath)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			session, err := elevator.Elevate(ctx, []string{"/opt/SingBoxUI/singboxui-priv", "start"})
			if err != nil {
				t.Fatalf("Elevate() = %v", err)
			}
			t.Cleanup(session.Release)
			waitForExited(t, session)
			status := session.Status()
			if tc.want != nil {
				if !errors.Is(status, tc.want) {
					t.Fatalf("Status() = %v, want %v", status, tc.want)
				}
				return
			}
			if status == nil {
				t.Fatal("Status() = nil, want the failure of the interpreter")
			}
			if errors.Is(status, privilege.ErrCancelled) {
				t.Errorf("Status() = %v, want a failure that is not a dismissed dialog", status)
			}
			if !strings.Contains(status.Error(), "not permitted") {
				t.Errorf("Status() = %v, want it to carry what osascript said", status)
			}
		})
	}
}

func TestReleasedSessionDoesNotReportAKillAsAFailure(t *testing.T) {
	interpreterPath, _ := interpreter(t, "sleep 30")
	elevator := NewElevatorWith(interpreterPath)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	session, err := elevator.Elevate(ctx, []string{"/opt/SingBoxUI/singboxui-priv", "start"})
	if err != nil {
		t.Fatalf("Elevate() = %v", err)
	}
	session.Release()
	if !session.Exited() {
		t.Fatal("Release() did not end the session")
	}
	// The session was ended by this application, so its death is not an
	// escalation failure to report to the user.
	if status := session.Status(); status != nil {
		t.Errorf("Status() = %v, want nil after Release", status)
	}
}
