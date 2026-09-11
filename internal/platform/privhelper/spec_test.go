package privhelper

import (
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/larffxx/singboxui/internal/privilege"
)

// TestExecutableNamePerGOOS covers the private copy of the sing-box executable
// name: the helper may not import internal/singbox, so the name is asserted here.
func TestExecutableNamePerGOOS(t *testing.T) {
	for goos, want := range map[string]string{
		"darwin":  "sing-box",
		"windows": "sing-box.exe",
		"linux":   "sing-box",
	} {
		if got := executableName(goos); got != want {
			t.Errorf("executableName(%q) = %q, want %q", goos, got, want)
		}
	}
}

// TestSpecFromRequestCarriesEveryField ties a port request to a data directory.
func TestSpecFromRequestCarriesEveryField(t *testing.T) {
	req := privilege.Request{
		BinaryPath: "/d/bin/sing-box",
		ConfigPath: "/d/config/active.json",
		WorkDir:    "/d/run",
		LogPath:    "/d/run/singbox.log",
		PIDPath:    "/d/run/singbox.pid",
		StatusPath: "/d/run/singbox.status",
		Reason:     "SingBoxUI needs to start its tunnel",
	}
	got := SpecFromRequest(req, "/d")
	want := Spec{
		DataDir:    "/d",
		BinaryPath: req.BinaryPath,
		ConfigPath: req.ConfigPath,
		WorkDir:    req.WorkDir,
		LogPath:    req.LogPath,
		PIDPath:    req.PIDPath,
		StatusPath: req.StatusPath,
		Reason:     req.Reason,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SpecFromRequest() = %+v, want %+v", got, want)
	}
}

// TestChildWorkDirFallsBackToTheConfigurationDirectory keeps a request without an
// explicit working directory from running in the helper's own directory.
func TestChildWorkDirFallsBackToTheConfigurationDirectory(t *testing.T) {
	spec := Spec{ConfigPath: "/d/config/active.json"}
	if got, want := spec.ChildWorkDir(), "/d/config"; got != want {
		t.Errorf("ChildWorkDir() = %q, want %q", got, want)
	}
	spec.WorkDir = "/d/run"
	if got, want := spec.ChildWorkDir(), "/d/run"; got != want {
		t.Errorf("ChildWorkDir() = %q, want %q", got, want)
	}
}

// TestValidateAcceptsTheLayout is the positive case: the documented data
// directory passes every check.
func TestValidateAcceptsTheLayout(t *testing.T) {
	l := newLayout(t)
	for _, spec := range []Spec{
		l.specOf(""),
		l.specOf("SingBoxUI needs to start its tunnel"),
	} {
		if err := spec.Validate(); err != nil {
			t.Errorf("Validate(%+v) = %v, want nil", spec, err)
		}
	}
}

// TestValidateRefusesRequestsOutsideTheContract is the negative case: the helper
// is the privileged side of a security boundary, so every documented refusal is
// asserted here rather than trusted (internal-contracts.md §3).
func TestValidateRefusesRequestsOutsideTheContract(t *testing.T) {
	l := newLayout(t)

	for _, tc := range []struct {
		name string
		// mutate changes the good request into the refused one.
		mutate func(*Spec)
		// want is a fragment of the refusal message.
		want string
		// skipWindows marks a case the Windows checks do not cover.
		skipWindows bool
	}{
		{
			name:   "relative data directory",
			mutate: func(s *Spec) { s.DataDir = "SingBoxUI" },
			want:   "absolute",
		},
		{
			name:   "binary outside the data directory",
			mutate: func(s *Spec) { s.BinaryPath = "/tmp/sing-box" },
			want:   "outside the application data directory",
		},
		{
			name: "binary with the wrong name",
			mutate: func(s *Spec) {
				s.BinaryPath = writeFile(t, l.dataDir+"/bin/other", "x")
			},
			want: "must be the sing-box executable",
		},
		{
			name: "binary that is not executable",
			mutate: func(s *Spec) {
				// A sing-box under the right name, but without the executable bit.
				s.BinaryPath = writeFile(t, l.dataDir+"/bin/1.11.0/sing-box", "x")
				if err := os.Chmod(s.BinaryPath, 0o600); err != nil {
					t.Fatalf("chmod %s: %v", s.BinaryPath, err)
				}
			},
			want:        "not executable",
			skipWindows: true,
		},
		{
			name:   "work directory that does not exist",
			mutate: func(s *Spec) { s.WorkDir = l.dataDir + "/absent" },
			want:   "--work-dir is not usable",
		},
		{
			name: "work directory that is a file",
			mutate: func(s *Spec) {
				s.WorkDir = writeFile(t, l.dataDir+"/run/not-a-directory", "x")
			},
			want: "--work-dir is not a directory",
		},
		{
			name: "configuration that is not json",
			mutate: func(s *Spec) {
				s.ConfigPath = writeFile(t, l.dataDir+"/config/active.yaml", "log: {}")
			},
			want: "must be a .json file",
		},
		{
			name:   "configuration outside the data directory",
			mutate: func(s *Spec) { s.ConfigPath = "/tmp/active.json" },
			want:   "outside the application data directory",
		},
		{
			name:   "path that is not in its shortest form",
			mutate: func(s *Spec) { s.LogPath = l.dataDir + "/run/../run/singbox.log" },
			want:   "must not contain",
		},
		{
			name:   "path written into a directory that does not exist",
			mutate: func(s *Spec) { s.LogPath = l.dataDir + "/logs/singbox.log" },
			want:   "no such file or directory",
		},
		{
			name:   "log path that is a directory",
			mutate: func(s *Spec) { s.StatusPath = l.workDir },
			want:   "is a directory",
		},
		{
			name:   "control character in a path",
			mutate: func(s *Spec) { s.PIDPath = l.dataDir + "/run/singbox\n.pid" },
			want:   "control character",
		},
		{
			name:   "control character in the reason",
			mutate: func(s *Spec) { s.Reason = "start\nsing-box" },
			want:   "control character",
		},
		{
			name:   "reason longer than the limit",
			mutate: func(s *Spec) { s.Reason = strings.Repeat("a", maxReasonLength+1) },
			want:   "longer than",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skipWindows && runtime.GOOS == "windows" {
				t.Skip("the executable bit is not part of the Windows checks")
			}
			spec := l.specOf("")
			tc.mutate(&spec)
			err := spec.Validate()
			if err == nil {
				t.Fatalf("Validate(%+v) = nil, want a refusal mentioning %q", spec, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Validate() = %q, want a refusal mentioning %q", err, tc.want)
			}
		})
	}
}

// TestValidateAcceptsAMissingReason keeps the optional flags optional.
func TestValidateAcceptsAMissingReason(t *testing.T) {
	l := newLayout(t)
	spec := l.specOf("")
	spec.Reason = ""
	if err := spec.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

// TestStartArgvRoundTripsThroughTheParser is the protocol test: what the
// launching side builds is exactly what the privileged side parses.
func TestStartArgvRoundTripsThroughTheParser(t *testing.T) {
	l := newLayout(t)
	for _, reason := range []string{"", "SingBoxUI needs to start its tunnel"} {
		want := l.specOf(reason)
		argv := want.StartArgv(helperPath)
		if len(argv) == 0 || argv[0] != helperPath {
			t.Fatalf("StartArgv() = %q, want it to begin with the helper path", argv)
		}
		if argv[1] != OpStart {
			t.Fatalf("StartArgv()[1] = %q, want %q", argv[1], OpStart)
		}
		got, err := ParseStartArgs(argv[2:])
		if err != nil {
			t.Fatalf("ParseStartArgs(%q) = %v, want nil", argv[1:], err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("round trip produced %+v, want %+v", got, want)
		}
	}
}

// TestStopArgvRoundTripsThroughTheParser is the same protocol test for stopping.
func TestStopArgvRoundTripsThroughTheParser(t *testing.T) {
	l := newLayout(t)
	for _, force := range []bool{false, true} {
		want := StopRequest{DataDir: l.dataDir, PIDPath: l.pid, Force: force}
		argv := StopArgv(helperPath, l.dataDir, l.pid, force)
		if argv[1] != OpStop {
			t.Fatalf("StopArgv()[1] = %q, want %q", argv[1], OpStop)
		}
		got, err := ParseStopArgs(argv[2:])
		if err != nil {
			t.Fatalf("ParseStopArgs(%q) = %v, want nil", argv[1:], err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("round trip produced %+v, want %+v", got, want)
		}
	}
}

// TestArgvIsNotACommandString pins spec §24/§83: the elevation mechanism receives
// an argument array, so no element is ever a concatenated command line.
func TestArgvIsNotACommandString(t *testing.T) {
	l := newLayout(t)
	argv := l.startArgv("SingBoxUI needs to start its tunnel")
	for i, arg := range argv {
		if i == 0 {
			continue
		}
		if strings.ContainsAny(arg, "\n\r") {
			t.Errorf("argv[%d] = %q contains a line break", i, arg)
		}
		if strings.Count(arg, " --") > 0 {
			t.Errorf("argv[%d] = %q looks like a concatenated command line", i, arg)
		}
		if strings.TrimSpace(arg) != arg {
			t.Errorf("argv[%d] = %q has surrounding whitespace", i, arg)
		}
	}
	// Every flag is followed by its own value: an odd count would mean the value
	// was glued onto the flag.
	if len(argv[2:])%2 != 0 {
		t.Errorf("StartArgv() = %q has a flag without a value", argv)
	}
	for i := 2; i < len(argv); i += 2 {
		if !strings.HasPrefix(argv[i], "--") {
			t.Errorf("argv[%d] = %q is not a flag", i, argv[i])
		}
		if strings.HasPrefix(argv[i+1], "--") {
			t.Errorf("argv[%d] = %q is not a value", i+1, argv[i+1])
		}
	}
}

// TestClearRunFilesRemovesPreviousRecords keeps a stale pid or exit code from
// being read as the current one. A missing file is not an error.
func TestClearRunFilesRemovesPreviousRecords(t *testing.T) {
	l := newLayout(t)
	writeFile(t, l.pid, `{"pid":1,"startTime":1}`)
	writeFile(t, l.status, `{"pid":1,"exitCode":0}`)
	if err := ClearRunFiles(l.pid, l.status); err != nil {
		t.Fatalf("ClearRunFiles() = %v, want nil", err)
	}
	for _, path := range []string{l.pid, l.status} {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s still exists after ClearRunFiles", path)
		}
	}
	if err := ClearRunFiles(l.pid, ""); err != nil {
		t.Errorf("ClearRunFiles() on a missing file = %v, want nil", err)
	}
}

// TestStopRequestValidateRefusesAPathOutsideTheDataDirectory keeps the stop path
// as narrow as the start path.
func TestStopRequestValidateRefusesAPathOutsideTheDataDirectory(t *testing.T) {
	l := newLayout(t)
	if err := (StopRequest{DataDir: l.dataDir, PIDPath: l.pid}).Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
	for _, tc := range []struct {
		name string
		req  StopRequest
		want string
	}{
		{
			name: "relative data directory",
			req:  StopRequest{DataDir: "SingBoxUI", PIDPath: l.pid},
			want: "absolute",
		},
		{
			name: "pid file outside the data directory",
			req:  StopRequest{DataDir: l.dataDir, PIDPath: "/tmp/singbox.pid"},
			want: "outside the application data directory",
		},
		{
			name: "pid file that is a directory",
			req:  StopRequest{DataDir: l.dataDir, PIDPath: l.workDir},
			want: "is a directory",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if err == nil {
				t.Fatalf("Validate(%+v) = nil, want a refusal mentioning %q", tc.req, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Validate() = %q, want a refusal mentioning %q", err, tc.want)
			}
		})
	}
}

// TestValidateRefusesASymlinkedEscape keeps a symlink inside the data directory
// from reaching a path outside it.
func TestValidateRefusesASymlinkedEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating a symlink needs a privilege the test does not have on Windows")
	}
	l := newLayout(t)
	outside := t.TempDir()
	link := l.dataDir + "/escape"
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("create the symlink: %v", err)
	}
	spec := l.specOf("")
	spec.LogPath = link + "/singbox.log"
	err := spec.Validate()
	if err == nil {
		t.Fatalf("Validate(%+v) = nil, want a refusal: the log path escapes through a symlink", spec)
	}
	if !strings.Contains(err.Error(), "outside") && !strings.Contains(err.Error(), "not usable") {
		t.Errorf("Validate() = %q, want a refusal about the escape", err)
	}
}
