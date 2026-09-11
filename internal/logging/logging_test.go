package logging

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestParseLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  slog.Level
	}{
		{name: "debug", input: "debug", want: slog.LevelDebug},
		{name: "info", input: "info", want: slog.LevelInfo},
		{name: "warn", input: "warn", want: slog.LevelWarn},
		{name: "error", input: "error", want: slog.LevelError},
		{name: "empty defaults to info", input: "", want: slog.LevelInfo},
		{name: "unknown defaults to info", input: "verbose", want: slog.LevelInfo},
		{name: "case sensitive, so uppercased defaults to info", input: "DEBUG", want: slog.LevelInfo},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := parseLevel(tc.input); got != tc.want {
				t.Errorf("parseLevel(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestSetupStderrOnly(t *testing.T) {
	t.Parallel()

	logger, closer, err := Setup(Options{})
	if err != nil {
		t.Fatalf("Setup() = %v, want nil", err)
	}
	if logger == nil {
		t.Fatal("Setup() returned a nil logger")
	}
	if closer == nil {
		t.Fatal("Setup() returned a nil closer")
	}
	ctx := context.Background()
	if !logger.Enabled(ctx, slog.LevelInfo) {
		t.Error("default level should enable Info")
	}
	if logger.Enabled(ctx, slog.LevelDebug) {
		t.Error("default level should not enable Debug")
	}
	if err := closer.Close(); err != nil {
		t.Errorf("closer.Close() = %v, want nil", err)
	}
}

func TestSetupRespectsLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		level     string
		wantDebug bool
		wantInfo  bool
	}{
		{level: "debug", wantDebug: true, wantInfo: true},
		{level: "info", wantDebug: false, wantInfo: true},
		{level: "warn", wantDebug: false, wantInfo: false},
		{level: "error", wantDebug: false, wantInfo: false},
	}
	for _, tc := range tests {
		t.Run(tc.level, func(t *testing.T) {
			t.Parallel()
			logger, closer, err := Setup(Options{Level: tc.level, Path: filepath.Join(t.TempDir(), "app.log")})
			if err != nil {
				t.Fatalf("Setup() = %v, want nil", err)
			}
			defer closer.Close()
			ctx := context.Background()
			if got := logger.Enabled(ctx, slog.LevelDebug); got != tc.wantDebug {
				t.Errorf("Debug enabled = %v, want %v", got, tc.wantDebug)
			}
			if got := logger.Enabled(ctx, slog.LevelInfo); got != tc.wantInfo {
				t.Errorf("Info enabled = %v, want %v", got, tc.wantInfo)
			}
		})
	}
}

func TestSetupWritesStructuredJSONToFile(t *testing.T) {
	t.Parallel()

	// The parent directory does not exist yet: Setup must create it.
	path := filepath.Join(t.TempDir(), "logs", "app.log")
	logger, closer, err := Setup(Options{Level: "debug", Path: path})
	if err != nil {
		t.Fatalf("Setup() = %v, want nil", err)
	}
	logger.Debug("starting", "component", "runtime")
	logger.Info("ready", "pid", 42)
	if err := closer.Close(); err != nil {
		t.Fatalf("closer.Close() = %v, want nil", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("logged %d lines, want 2: %q", len(lines), data)
	}
	wantLevels := []string{"DEBUG", "INFO"}
	for i, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("line %d is not JSON (%v): %q", i, err, line)
		}
		if record["level"] != wantLevels[i] {
			t.Errorf("line %d level = %v, want %v", i, record["level"], wantLevels[i])
		}
		if _, ok := record["msg"]; !ok {
			t.Errorf("line %d is missing the msg key: %q", i, line)
		}
	}
	if !strings.Contains(string(data), `"component":"runtime"`) {
		t.Errorf("log output lost the structured attribute: %q", data)
	}
}

func TestSetupLevelFiltersRecords(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.log")
	logger, closer, err := Setup(Options{Level: "error", Path: path})
	if err != nil {
		t.Fatalf("Setup() = %v, want nil", err)
	}
	logger.Debug("debug-suppressed")
	logger.Info("info-suppressed")
	logger.Warn("warn-suppressed")
	logger.Error("error-kept")
	if err := closer.Close(); err != nil {
		t.Fatalf("closer.Close() = %v, want nil", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if strings.Contains(string(data), "suppressed") {
		t.Errorf("records below the level leaked into the file: %q", data)
	}
	if !strings.Contains(string(data), "error-kept") {
		t.Errorf("the error record is missing from the file: %q", data)
	}
}

func TestSetupErrors(t *testing.T) {
	t.Parallel()

	t.Run("parent is a file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		file := filepath.Join(dir, "not-a-dir")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatalf("seed file: %v", err)
		}
		logger, closer, err := Setup(Options{Path: filepath.Join(file, "app.log")})
		if err == nil {
			t.Fatal("Setup() = nil, want an error when the log directory cannot be created")
		}
		if logger != nil || closer != nil {
			t.Errorf("Setup() = (%v, %v), want nil logger and closer on error", logger, closer)
		}
		if !strings.Contains(err.Error(), "create log dir") {
			t.Errorf("error %q should name the failing step", err)
		}
	})

	t.Run("path is a directory", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		logger, closer, err := Setup(Options{Path: dir})
		if err == nil {
			t.Fatal("Setup() = nil, want an error when the log path is a directory")
		}
		if logger != nil || closer != nil {
			t.Errorf("Setup() = (%v, %v), want nil logger and closer on error", logger, closer)
		}
		if !strings.Contains(err.Error(), "open log file") {
			t.Errorf("error %q should name the failing step", err)
		}
	})
}

func TestWithLoggerAndFromContext(t *testing.T) {
	t.Parallel()

	base := context.Background()
	if got := FromContext(base); got != slog.Default() {
		t.Error("FromContext(empty) should return the default logger")
	}

	custom := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := WithLogger(base, custom)
	if got := FromContext(ctx); got != custom {
		t.Error("FromContext should return the logger stored by WithLogger")
	}

	// A typed nil logger is treated exactly like an absent one.
	nilCtx := WithLogger(base, nil)
	if got := FromContext(nilCtx); got != slog.Default() {
		t.Error("FromContext should fall back to the default when the stored logger is nil")
	}
}

func TestRotatingWriterRotatesAndKeepsBackups(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.log")
	w, err := newRotatingWriter(path, 10, 2)
	if err != nil {
		t.Fatalf("newRotatingWriter() = %v, want nil", err)
	}
	defer w.Close()

	write := func(s string) {
		t.Helper()
		n, err := w.Write([]byte(s))
		if err != nil {
			t.Fatalf("Write(%q) = %v, want nil", s, err)
		}
		if n != len(s) {
			t.Fatalf("Write(%q) = %d bytes, want %d", s, n, len(s))
		}
	}

	write("aaaaaaaaaa") // exactly at the cap: no rotation yet
	write("b")          // overflow: rotate, current becomes app.log.1
	write("c")          // "bc" stays below the cap
	write("ddddddddd")  // overflow again: .1 -> .2, current -> .1

	tests := []struct {
		file string
		want string
	}{
		{file: path, want: "ddddddddd"},
		{file: path + ".1", want: "bc"},
		{file: path + ".2", want: "aaaaaaaaaa"},
	}
	for _, tc := range tests {
		got, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatalf("read %s: %v", tc.file, err)
		}
		if string(got) != tc.want {
			t.Errorf("%s = %q, want %q", tc.file, got, tc.want)
		}
	}
}

func TestRotatingWriterCountsPreexistingSize(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatalf("seed log file: %v", err)
	}
	w, err := newRotatingWriter(path, 10, 3)
	if err != nil {
		t.Fatalf("newRotatingWriter() = %v, want nil", err)
	}
	defer w.Close()

	// The file is already at the cap, so a single byte must trigger a rotation.
	if _, err := w.Write([]byte("x")); err != nil {
		t.Fatalf("Write() = %v, want nil", err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read current log: %v", err)
	}
	if string(current) != "x" {
		t.Errorf("current log = %q, want %q", current, "x")
	}
	backup, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("read backup log: %v", err)
	}
	if string(backup) != "0123456789" {
		t.Errorf("backup log = %q, want the pre-existing content", backup)
	}
}

func TestRotatingWriterConcurrentWritesAreSerialised(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.log")
	// A cap far above the total keeps rotation out of the picture so the total
	// byte count is deterministic.
	w, err := newRotatingWriter(path, 1<<30, 3)
	if err != nil {
		t.Fatalf("newRotatingWriter() = %v, want nil", err)
	}

	const (
		goroutines = 8
		perWorker  = 50
		chunk      = "hello\n"
	)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				if _, err := w.Write([]byte(chunk)); err != nil {
					t.Errorf("concurrent Write() = %v, want nil", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if err := w.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	want := goroutines * perWorker * len(chunk)
	if len(data) != want {
		t.Errorf("log holds %d bytes, want %d (lost or interleaved writes)", len(data), want)
	}
	if strings.Count(string(data), chunk) != goroutines*perWorker {
		t.Errorf("log holds %d intact chunks, want %d", strings.Count(string(data), chunk), goroutines*perWorker)
	}
}

func TestRedactJSON(t *testing.T) {
	t.Parallel()

	t.Run("nested secrets are replaced, public keys kept", func(t *testing.T) {
		t.Parallel()
		in := `{"password":"s3cret","nested":{"uuid":"abc-123","public_key":"pk-123"},` +
			`"servers":[{"token":"tok"}],"server_port":443}`
		out := RedactJSON([]byte(in))
		var got map[string]any
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("redacted output is not JSON (%v): %s", err, out)
		}
		if got["password"] != Redacted {
			t.Errorf("password = %v, want %q", got["password"], Redacted)
		}
		nested, ok := got["nested"].(map[string]any)
		if !ok {
			t.Fatalf("nested = %#v, want an object", got["nested"])
		}
		if nested["uuid"] != Redacted {
			t.Errorf("nested.uuid = %v, want %q", nested["uuid"], Redacted)
		}
		if nested["public_key"] != "pk-123" {
			t.Errorf("nested.public_key = %v, want it left readable", nested["public_key"])
		}
		servers, ok := got["servers"].([]any)
		if !ok || len(servers) != 1 {
			t.Fatalf("servers = %#v, want one entry", got["servers"])
		}
		if entry := servers[0].(map[string]any); entry["token"] != Redacted {
			t.Errorf("servers[0].token = %v, want %q", entry["token"], Redacted)
		}
		if got["server_port"] != float64(443) {
			t.Errorf("server_port = %v, want 443 untouched", got["server_port"])
		}
	})

	t.Run("key matching is case-insensitive", func(t *testing.T) {
		t.Parallel()
		out := RedactJSON([]byte(`{"PASSWORD":"x","Token":"y"}`))
		if !strings.Contains(string(out), `"PASSWORD":"`+Redacted+`"`) {
			t.Errorf("PASSWORD was not redacted: %s", out)
		}
		if !strings.Contains(string(out), `"Token":"`+Redacted+`"`) {
			t.Errorf("Token was not redacted: %s", out)
		}
	})

	t.Run("non-string secret values are still replaced", func(t *testing.T) {
		t.Parallel()
		out := RedactJSON([]byte(`{"password":123}`))
		if got := string(out); got != `{"password":"`+Redacted+`"}` {
			t.Errorf("output = %s, want the numeric secret replaced by %q", got, Redacted)
		}
	})

	t.Run("array of objects is walked", func(t *testing.T) {
		t.Parallel()
		out := RedactJSON([]byte(`[{"token":"t"},{"name":"ok"}]`))
		if !strings.Contains(string(out), Redacted) {
			t.Errorf("array element was not redacted: %s", out)
		}
		if !strings.Contains(string(out), `"name":"ok"`) {
			t.Errorf("non-secret value was disturbed: %s", out)
		}
	})

	t.Run("invalid JSON falls back to textual redaction", func(t *testing.T) {
		t.Parallel()
		out := RedactJSON([]byte("not-json vless://user:pass@host:443\nsecond line"))
		if !strings.Contains(string(out), "vless://"+Redacted) {
			t.Errorf("share link was not redacted: %s", out)
		}
		if strings.Contains(string(out), "\n") {
			t.Errorf("newlines should be collapsed by the textual fallback: %q", out)
		}
	})

	t.Run("empty input stays empty", func(t *testing.T) {
		t.Parallel()
		if got := string(RedactJSON(nil)); got != "" {
			t.Errorf("RedactJSON(nil) = %q, want empty", got)
		}
	})

	// Characterisation: inside valid JSON only credential keys are rewritten;
	// a bare share URL held in an ordinary string leaf is left as-is. Pinned so
	// a future tightening of the policy shows up as a test change.
	t.Run("share link in an ordinary string leaf is not rewritten", func(t *testing.T) {
		t.Parallel()
		out := string(RedactJSON([]byte(`["vless://user:pass@host:443"]`)))
		if out != `["vless://user:pass@host:443"]` {
			t.Errorf("output = %s, want the string leaf untouched by the JSON path", out)
		}
	})
}

func TestRedactString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain text unchanged", in: "nothing sensitive here", want: "nothing sensitive here"},
		{name: "newline collapsed", in: "line1\nline2", want: "line1 line2"},
		{name: "vless link", in: "visit vless://a:b@h:443 now", want: "visit vless://" + Redacted + " now"},
		{name: "scheme case is preserved", in: "VLESS://A:B@H:1", want: "VLESS://" + Redacted},
		{name: "ss and ssr links", in: "ssr://abc ss://def", want: "ssr://" + Redacted + " ss://" + Redacted},
		{name: "multiple links", in: "trojan://a tuic://b", want: "trojan://" + Redacted + " tuic://" + Redacted},
		{name: "hysteria2 link", in: "hy2 and hysteria2://x", want: "hy2 and hysteria2://" + Redacted},
		{name: "empty", in: "", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := RedactString(tc.in); got != tc.want {
				t.Errorf("RedactString(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRedactValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{name: "secret key", key: "password", value: "plain", want: Redacted},
		{name: "secret key case-insensitive", key: "PASSWORD", value: "x", want: Redacted},
		{name: "uuid is secret", key: "uuid", value: "1234", want: Redacted},
		{name: "public key is not secret", key: "public_key", value: "pk", want: "pk"},
		{name: "ordinary key with a share link", key: "server", value: "vless://a@b:1", want: "vless://" + Redacted},
		{name: "ordinary key with plain text", key: "name", value: "proxy", want: "proxy"},
		{name: "empty value", key: "name", value: "", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := RedactValue(tc.key, tc.value); got != tc.want {
				t.Errorf("RedactValue(%q, %q) = %q, want %q", tc.key, tc.value, got, tc.want)
			}
		})
	}
}
