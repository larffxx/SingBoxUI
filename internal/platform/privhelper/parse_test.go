package privhelper

import (
	"reflect"
	"strings"
	"testing"
)

// TestExitCodesArePartOfTheProtocol pins the exit codes the launching side
// branches on. Changing one breaks every caller, so they are asserted literally.
func TestExitCodesArePartOfTheProtocol(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  int
		want int
	}{
		{"success", ExitOK, 0},
		{"failure", ExitFailure, 1},
		{"usage", ExitUsage, 2},
		{"cancelled", ExitCancelled, 3},
	} {
		if tc.got != tc.want {
			t.Errorf("exit code %s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

// TestUsageNamesBothOperationsAndNoOther keeps the helper honest about its scope:
// the usage text is the only description of what it does, and the operations it
// lists are exactly the two the helper performs plus help.
func TestUsageNamesBothOperationsAndNoOther(t *testing.T) {
	usage := Usage()
	for _, fragment := range []string{
		OpStart, OpStop, "singboxui-priv",
		"no other operation", "no free-form argument",
		"exit codes",
	} {
		if !strings.Contains(usage, fragment) {
			t.Errorf("Usage() does not mention %q", fragment)
		}
	}
	operations := usageOperations(usage)
	want := []string{OpStart, OpStop, "help"}
	if !reflect.DeepEqual(operations, want) {
		t.Errorf("Usage() documents the operations %q, want %q", operations, want)
	}
}

// usageOperations extracts the operation names from the usage text, so a
// capability the helper does not have cannot creep in unnoticed.
func usageOperations(usage string) []string {
	var operations []string
	for _, line := range strings.Split(usage, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "singboxui-priv" || strings.HasPrefix(fields[1], "-") {
			continue
		}
		operations = append(operations, fields[1])
	}
	return operations
}

// TestParseStartArgsAcceptsTheDocumentedFlags is the narrow happy path.
func TestParseStartArgsAcceptsTheDocumentedFlags(t *testing.T) {
	l := newLayout(t)
	args := []string{
		"--" + FlagDataDir, l.dataDir,
		"--" + FlagBinary, l.binary,
		"--" + FlagConfig, l.config,
		"--" + FlagWorkDir, l.workDir,
		"--" + FlagLog, l.log,
		"--" + FlagPID, l.pid,
		"--" + FlagStatus, l.status,
		"--" + FlagReason, "SingBoxUI needs to start its tunnel",
	}
	spec, err := ParseStartArgs(args)
	if err != nil {
		t.Fatalf("ParseStartArgs() = %v, want nil", err)
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
	if spec.DataDir != l.dataDir || spec.BinaryPath != l.binary || spec.ConfigPath != l.config {
		t.Errorf("ParseStartArgs() = %+v, want the paths it was given", spec)
	}
	if spec.Reason != "SingBoxUI needs to start its tunnel" {
		t.Errorf("ParseStartArgs() reason = %q", spec.Reason)
	}
}

// TestParseRefusesAnythingElse pins "no free-form argument": an unknown flag, a
// positional argument or a missing operation is refused instead of guessed at.
func TestParseRefusesAnythingElse(t *testing.T) {
	l := newLayout(t)
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "unknown flag",
			args: []string{"--data-dir", l.dataDir, "--daemon"},
			want: "flag provided but not defined",
		},
		{
			name: "positional argument",
			args: []string{"--data-dir", l.dataDir, l.binary},
			want: "positional",
		},
		{
			name: "single-dash long flag is still the same flag",
			args: []string{"-data-dir", l.dataDir},
			want: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseStartArgs(tc.args)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("ParseStartArgs(%q) = %v, want nil", tc.args, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ParseStartArgs(%q) = nil, want an error mentioning %q", tc.args, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("ParseStartArgs(%q) = %q, want an error mentioning %q", tc.args, err, tc.want)
			}
		})
	}
}

// TestParseStopArgsReadsTheBooleanFlag covers --force, which is the only way to
// ask for a kill instead of a graceful stop.
func TestParseStopArgsReadsTheBooleanFlag(t *testing.T) {
	l := newLayout(t)
	base := []string{"--" + FlagDataDir, l.dataDir, "--" + FlagPID, l.pid}
	req, err := ParseStopArgs(base)
	if err != nil {
		t.Fatalf("ParseStopArgs() = %v, want nil", err)
	}
	if req.Force {
		t.Error("ParseStopArgs() without --force reported Force = true")
	}
	req, err = ParseStopArgs(append(base, "--"+FlagForce))
	if err != nil {
		t.Fatalf("ParseStopArgs(--force) = %v, want nil", err)
	}
	if !req.Force {
		t.Error("ParseStopArgs(--force) reported Force = false")
	}
	if _, err := ParseStopArgs(append(base, "extra")); err == nil {
		t.Error("ParseStopArgs() accepted a positional argument")
	}
}

// TestIsHelpRecognisesTheUsageSpellings keeps the documented spellings working.
func TestIsHelpRecognisesTheUsageSpellings(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}, {"-help"}, {"usage"}, {"HELP"}} {
		if !IsHelp(args) {
			t.Errorf("IsHelp(%q) = false, want true", args)
		}
	}
	for _, args := range [][]string{nil, {}, {"start"}, {"stop"}, {"help", "start"}, {"--data-dir", "/d"}} {
		if IsHelp(args) {
			t.Errorf("IsHelp(%q) = true, want false", args)
		}
	}
}
