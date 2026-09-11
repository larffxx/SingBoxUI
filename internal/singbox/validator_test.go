package singbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/logging"
	"github.com/larffxx/singboxui/internal/singbox/faketest"
)

func TestCheck(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config.json")
	if err := os.WriteFile(config, []byte(`{"log":{"level":"info"}}`), 0o600); err != nil {
		t.Fatalf("cannot write the config: %v", err)
	}
	plain := filepath.Join(dir, "sing-box.txt")
	if err := os.WriteFile(plain, []byte("not a binary"), 0o600); err != nil {
		t.Fatalf("cannot write %s: %v", plain, err)
	}

	tests := []struct {
		name         string
		scenario     string
		binary       string
		config       string
		wantOK       bool
		wantCode     apperr.Code
		wantErrors   int
		wantOutputIn string
	}{
		{
			name:     "accepted configuration",
			scenario: faketest.ScenarioCheckOK,
			config:   config,
			wantOK:   true,
		},
		{
			name:         "rejected configuration",
			scenario:     faketest.ScenarioCheckFail,
			config:       config,
			wantCode:     apperr.CodeConfigCheckFailed,
			wantErrors:   1,
			wantOutputIn: "FATAL[0000]",
		},
		{
			name:         "rejected configuration quotes the file",
			scenario:     faketest.ScenarioCheckFail,
			config:       config,
			wantCode:     apperr.CodeConfigCheckFailed,
			wantErrors:   1,
			wantOutputIn: config,
		},
		{
			name:     "executable that is not sing-box",
			scenario: faketest.ScenarioVersionFail,
			config:   config,
			wantCode: apperr.CodeConfigCheckFailed,
		},
		{
			name:     "missing executable",
			scenario: faketest.ScenarioCheckOK,
			binary:   filepath.Join(dir, "absent"),
			config:   config,
			wantCode: apperr.CodeBinaryNotFound,
		},
		{
			name:     "non-executable file",
			scenario: faketest.ScenarioCheckOK,
			binary:   plain,
			config:   config,
			wantCode: apperr.CodeBinaryNotFound,
		},
		{
			name:     "no configuration",
			scenario: faketest.ScenarioCheckOK,
			config:   "  ",
			wantCode: apperr.CodeInvalidArgument,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(faketest.EnvScenario, test.scenario)
			binary := test.binary
			if binary == "" {
				binary = copyFakeBinary(t, t.TempDir(), "sing-box")
			}

			result, err := Check(context.Background(), binary, test.config, 10*time.Second)
			switch {
			case test.wantCode == "":
				if err != nil {
					t.Fatalf("Check() failed: %v", err)
				}
			case err == nil:
				t.Fatalf("Check() = %+v, want an error with code %s", result, test.wantCode)
			default:
				if code := apperr.CodeOf(err); code != test.wantCode {
					t.Errorf("error code = %s, want %s (%v)", code, test.wantCode, err)
				}
				if result.OK {
					t.Error("result.OK is true for a failed check")
				}
			}
			if result.OK != test.wantOK {
				t.Errorf("OK = %v, want %v", result.OK, test.wantOK)
			}
			if len(result.Errors) != test.wantErrors {
				t.Errorf("errors = %v, want %d line(s)", result.Errors, test.wantErrors)
			}
			if test.wantOutputIn != "" && !strings.Contains(result.Output, test.wantOutputIn) {
				t.Errorf("output %q does not contain %q", result.Output, test.wantOutputIn)
			}
		})
	}
}

func TestCheckReportsTheVersionItSaw(t *testing.T) {
	// sing-box prints its banner for some subcommands; when the probe text is
	// there, Check keeps it rather than discarding it.
	t.Setenv(faketest.EnvScenario, faketest.ScenarioVersion)
	binary := copyFakeBinary(t, t.TempDir(), "sing-box")
	dir := t.TempDir()

	result, err := Check(context.Background(), binary, filepath.Join(dir, "config.json"), 10*time.Second)
	if err != nil {
		t.Fatalf("Check() failed: %v", err)
	}
	if !result.OK {
		t.Fatal("OK = false, want the scenario to be accepted")
	}
	if result.Version.String() != "1.14.0" || result.VersionRaw != "1.14.0" {
		t.Errorf("version = %q (%q), want 1.14.0", result.Version.String(), result.VersionRaw)
	}
}

func TestCheckTimesOut(t *testing.T) {
	// run-slow prints nothing and does nothing for three seconds: a check that
	// is given 300ms must give up and say so (spec §30).
	t.Setenv(faketest.EnvScenario, faketest.ScenarioRunSlow)
	binary := copyFakeBinary(t, t.TempDir(), "sing-box")

	start := time.Now()
	result, err := Check(context.Background(), binary, filepath.Join(t.TempDir(), "config.json"), 300*time.Millisecond)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("check took %s, want it to stop at the 300ms deadline", elapsed)
	}
	if err == nil {
		t.Fatalf("Check() = %+v, want a timeout error", result)
	}
	if code := apperr.CodeOf(err); code != apperr.CodeConfigCheckFailed {
		t.Errorf("error code = %s, want %s (%v)", code, apperr.CodeConfigCheckFailed, err)
	}
	if result.OK {
		t.Error("OK = true for a timed out check")
	}
	if !strings.Contains(err.Error(), "in time") {
		t.Errorf("error %q does not mention the deadline", err)
	}
}

func TestCheckHonoursAnAlreadyCancelledContext(t *testing.T) {
	t.Setenv(faketest.EnvScenario, faketest.ScenarioRunSlow)
	binary := copyFakeBinary(t, t.TempDir(), "sing-box")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Check(ctx, binary, filepath.Join(t.TempDir(), "config.json"), 10*time.Second)
	if err == nil {
		t.Fatal("Check() ran with a cancelled context")
	}
	if code := apperr.CodeOf(err); code != apperr.CodeConfigCheckFailed {
		t.Errorf("error code = %s, want %s", code, apperr.CodeConfigCheckFailed)
	}
}

func TestCheckRedactsShareLinks(t *testing.T) {
	// Whatever the validator echoes reaches a log line and the UI, so a share
	// link in its output must be gone by then (spec §33).
	t.Setenv(faketest.EnvScenario, faketest.ScenarioCheckFail)
	binary := copyFakeBinary(t, t.TempDir(), "sing-box")
	secret := "trojan://secret-password@example.com:443"

	result, err := Check(context.Background(), binary, secret, 10*time.Second)
	if err == nil {
		t.Fatal("Check() accepted a configuration the fixture rejects")
	}
	if strings.Contains(result.Output, "secret-password") {
		t.Errorf("output leaks the credential: %q", result.Output)
	}
	if !strings.Contains(result.Output, "trojan://"+logging.Redacted) {
		t.Errorf("output %q does not carry the redaction marker", result.Output)
	}
	for _, line := range result.Errors {
		if strings.Contains(line, "secret-password") {
			t.Errorf("error line leaks the credential: %q", line)
		}
	}
}

func TestCheckErrorLines(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		want  bool
		notes string
	}{
		{name: "fatal with a level prefix", line: "FATAL[0000] decode config: unexpected EOF", want: true},
		{name: "error with a level prefix", line: "ERROR[0000] outbound 3: unknown protocol", want: true},
		{name: "fatal alone", line: "fatal", want: true},
		{name: "fatal with a colon", line: "FATAL: bad route", want: true},
		{name: "lower case is the same level", line: "error[0000] nope", want: true},
		{name: "info is not a verdict", line: "INFO[0000] sing-box started", want: false},
		{name: "debug is not a verdict", line: "DEBUG[0000] loading rules", want: false},
		{name: "a word starting with error is not a level", line: "ERRORS: none", want: false},
		{name: "empty", line: "", want: false},
		{name: "indented fatal", line: "  FATAL[0000] indented", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := checkErrorLines("preamble\n" + test.line + "\n")
			if (len(got) == 1) != test.want {
				t.Errorf("checkErrorLines(%q) = %v, want verdict %v", test.line, got, test.want)
			}
		})
	}

	t.Run("bounded", func(t *testing.T) {
		var builder strings.Builder
		for i := 0; i < maxCheckErrors*3; i++ {
			builder.WriteString("FATAL[0000] rule ")
			builder.WriteString(strings.Repeat("x", 3))
			builder.WriteString("\n")
		}
		if got := checkErrorLines(builder.String()); len(got) != maxCheckErrors {
			t.Errorf("errors = %d, want the bound of %d", len(got), maxCheckErrors)
		}
	})
}

func TestBoundedBuffer(t *testing.T) {
	tests := []struct {
		name          string
		writes        []int
		wantTruncated bool
		wantLen       int
	}{
		{name: "small output is kept whole", writes: []int{100}, wantLen: 100},
		{name: "exactly at the limit", writes: []int{maxProcessOutput}, wantLen: maxProcessOutput},
		{name: "one byte over the limit", writes: []int{maxProcessOutput + 1}, wantTruncated: true, wantLen: maxProcessOutput},
		{name: "many writes", writes: []int{maxProcessOutput / 2, maxProcessOutput / 2, 1}, wantTruncated: true, wantLen: maxProcessOutput},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			buffer := &boundedBuffer{}
			total := 0
			for _, size := range test.writes {
				payload := make([]byte, size)
				for i := range payload {
					payload[i] = 'x'
				}
				written, err := buffer.Write(payload)
				if err != nil {
					t.Fatalf("Write() failed: %v", err)
				}
				// The child must never see a short write because the adapter
				// stopped storing.
				if written != len(payload) {
					t.Fatalf("Write() = %d, want %d", written, len(payload))
				}
				total += size
			}
			out := buffer.String()
			want := maxProcessOutput
			if test.wantTruncated {
				want += len("\n[output truncated]")
				if !strings.HasSuffix(out, "[output truncated]") {
					t.Errorf("output does not end with the truncation marker")
				}
			} else {
				want = total
			}
			if len(out) != want {
				t.Errorf("stored %d bytes, want %d (of %d written)", len(out), want, total)
			}
		})
	}
}

func TestRedactOutputKeepsLineStructure(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     []string
		wantGone string
	}{
		{
			name: "share link",
			input: "FATAL[0000] failed to parse vless://uuid-secret@example.com:443\n" +
				"INFO[0000] done",
			want:     []string{"FATAL[0000] failed to parse vless://" + logging.Redacted, "INFO[0000] done"},
			wantGone: "uuid-secret",
		},
		{
			name:     "empty output",
			input:    "",
			want:     nil,
			wantGone: "",
		},
		{
			name:     "no secrets",
			input:    "ERROR[0000] dial tcp: connection refused",
			want:     []string{"ERROR[0000] dial tcp: connection refused"},
			wantGone: "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := redactOutput(test.input)
			if test.want == nil {
				if got != "" {
					t.Errorf("redactOutput() = %q, want empty", got)
				}
				return
			}
			// The line count survives: error extraction depends on it.
			if lines := strings.Split(got, "\n"); len(lines) != len(test.want) {
				t.Fatalf("redactOutput() produced %d lines, want %d (%q)", len(lines), len(test.want), got)
			}
			lines := strings.Split(got, "\n")
			for i, want := range test.want {
				if lines[i] != want {
					t.Errorf("line %d = %q, want %q", i, lines[i], want)
				}
			}
			if test.wantGone != "" && strings.Contains(got, test.wantGone) {
				t.Errorf("redactOutput() leaked %q", test.wantGone)
			}
		})
	}
}
