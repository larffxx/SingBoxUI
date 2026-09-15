// Package faketest is the fake sing-box executable used by the test suite
// (spec §67).
//
// It exists so every code path that talks to a real sing-box — version probing,
// `check`, supervised runs that are terminated, crash detection and foreign
// process detection — is exercised against a real process with real exit codes
// and real signals, without a network, without root and without risking the
// user's actual proxy. cmd/fakesingbox wraps Main, and tests build that command
// on demand into a temporary directory, usually naming the result "sing-box" so
// the adapter sees exactly what it would see in production.
package faketest

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/larffxx/singboxui/internal/atomicfile"
)

// Scenario names. A test sets one of these through the -scenario flag or the
// FAKESINGBOX_SCENARIO environment variable; constants exist so no test has to
// spell a scenario correctly twice.
const (
	// ScenarioVersion prints the current stable banner.
	ScenarioVersion = "version"
	// ScenarioVersionOld prints an older stable version.
	ScenarioVersionOld = "version-old"
	// ScenarioVersionBeta prints a pre-release version.
	ScenarioVersionBeta = "version-beta"
	// ScenarioVersionFail prints junk and exits non-zero, like a file that is
	// executable but is not sing-box.
	ScenarioVersionFail = "version-fail"
	// ScenarioCheckOK accepts any configuration.
	ScenarioCheckOK = "check-ok"
	// ScenarioCheckFail rejects the configuration with a validator error line.
	ScenarioCheckFail = "check-fail"
	// ScenarioRun logs, writes its PID and exits 0 on SIGTERM.
	ScenarioRun = "run"
	// ScenarioRunSlow delays its first log and its PID file, so a health
	// timeout can be tested without waiting for a real binary.
	ScenarioRunSlow = "run-slow"
	// ScenarioRunCrash prints one line and exits 2.
	ScenarioRunCrash = "run-crash"
	// ScenarioRunIgnoreTerm ignores SIGTERM and must be killed.
	ScenarioRunIgnoreTerm = "run-ignore-term"
	// ScenarioRunForeign is a long-lived stand-in for a sing-box this
	// application did not start.
	ScenarioRunForeign = "run-foreign"
)

// EnvScenario selects the scenario when no flag is given, so a test can copy
// the binary under the name "sing-box" and drive it without extra arguments.
const EnvScenario = "FAKESINGBOX_SCENARIO"

// runSlowDelay is how long run-slow pretends to still be starting.
const runSlowDelay = 3 * time.Second

// heartbeatInterval keeps a running fake producing log lines, so a log tail or
// ring buffer can be tested with a process that stays alive.
const heartbeatInterval = 250 * time.Millisecond

// options is the parsed command line.
type options struct {
	scenario   string
	command    string
	pidFile    string
	statusFile string
	logFile    string
	configPath string
}

// Main runs the fake sing-box and returns the process exit code.
func Main() int {
	opts := parseArgs(os.Args[1:])
	scenario := resolveScenario(opts)

	switch {
	case scenario == ScenarioVersion:
		// The first line is exactly what the real binary prints; the rest mimics
		// the environment banner so version parsing is tested against the shape
		// it will meet in production, including the go1.26.7 that must not be
		// mistaken for the release version.
		fmt.Println("sing-box version 1.14.0")
		fmt.Println()
		fmt.Println("Environment: go1.26.7 darwin/arm64")
		fmt.Println("Tags: with_gvisor,with_quic,with_dhcp,with_wireguard")
		return 0

	case scenario == ScenarioVersionOld:
		fmt.Println("1.13.0")
		return 0

	case scenario == ScenarioVersionBeta:
		fmt.Println("1.15.0-beta.1")
		return 0

	case scenario == ScenarioVersionFail:
		// Junk, not a version: the probe must reject this rather than guess.
		fmt.Fprintln(os.Stderr, "not a sing-box binary: unexpected output")
		return 1

	case scenario == ScenarioCheckOK:
		return 0

	case scenario == ScenarioCheckFail:
		config := opts.configPath
		if config == "" {
			config = "config.json"
		}
		fmt.Fprintf(os.Stderr, "FATAL[0000] decode config at %s: json: unknown field \"inbounds\"\n", config)
		return 1

	case strings.HasPrefix(scenario, "run"):
		return runScenario(scenario, opts)

	default:
		fmt.Fprintf(os.Stderr, "fakesingbox: unknown scenario %q\n", scenario)
		return 2
	}
}

// resolveScenario applies the documented precedence: flag, then environment,
// then the command word, so an explicit scenario always wins.
func resolveScenario(opts options) string {
	if opts.scenario != "" {
		return opts.scenario
	}
	if fromEnv := strings.TrimSpace(os.Getenv(EnvScenario)); fromEnv != "" {
		return fromEnv
	}
	switch opts.command {
	case "check":
		return ScenarioCheckOK
	case "run":
		return ScenarioRun
	default:
		// No command, or "version": report a version, which is what a probe of
		// a freshly copied fixture expects.
		return ScenarioVersion
	}
}

// parseArgs reads the flags the fixture understands.
//
// It is hand-written because the fixture is invoked both as `fakesingbox
// -scenario run …` and as `sing-box check -c config.json`, and the order of
// flags and the command word has to stay flexible. Unknown flags are ignored
// with a warning so the fixture can stand in for the real binary in a code path
// that passes a flag it does not model.
func parseArgs(args []string) options {
	var opts options
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			if opts.command == "" {
				opts.command = arg
			}
			continue
		}
		name, inline, hasInline := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		take := func() (string, bool) {
			if hasInline {
				return inline, true
			}
			if i+1 < len(args) {
				i++
				return args[i], true
			}
			return "", false
		}
		switch name {
		case "scenario":
			opts.scenario, _ = take()
		case "pid-file":
			opts.pidFile, _ = take()
		case "status-file":
			opts.statusFile, _ = take()
		case "log-file":
			opts.logFile, _ = take()
		case "c", "config":
			opts.configPath, _ = take()
		case "h", "help", "v", "version":
			// Accepted and ignored: the command word already covers these, and
			// failing on them would break stand-in usage.
		default:
			fmt.Fprintf(os.Stderr, "fakesingbox: ignoring unknown flag %q\n", arg)
			if !hasInline {
				// A separate value would otherwise be mistaken for the command.
				i++
			}
		}
	}
	return opts
}

// runScenario implements every `run*` behaviour: log to stdout and stderr,
// record the PID, record the exit code, and react to SIGTERM.
func runScenario(scenario string, opts options) int {
	// Registering the handler is what stops SIGTERM from killing the process, so
	// it happens before any logging or delay: run-ignore-term has to survive a
	// signal that arrives while it is still starting up.
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	// The log file is an extra sink next to the streams, so a test can read what
	// the supervised process produced without inheriting its pipes.
	var logSink io.Writer
	if opts.logFile != "" {
		file, err := os.OpenFile(opts.logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fakesingbox: cannot open the log file: %v\n", err)
			return 2
		}
		defer file.Close()
		logSink = &syncWriter{w: file}
	}
	stdout, stderr := io.Writer(os.Stdout), io.Writer(os.Stderr)
	if logSink != nil {
		stdout, stderr = io.MultiWriter(os.Stdout, logSink), io.MultiWriter(os.Stderr, logSink)
	}

	if scenario == ScenarioRunSlow {
		// Nothing at all for three seconds: a supervisor that confirms readiness
		// by watching the log or the PID file must time out here.
		time.Sleep(runSlowDelay)
	}

	writePIDFile(opts.pidFile)

	if scenario == ScenarioRunCrash {
		// Exactly one line, then a non-zero exit: the supervisor must notice the
		// exit code, not a missing log line or a signal.
		fmt.Fprintln(stdout, "INFO[0000] sing-box started")
		writeStatusFile(opts.statusFile, 2)
		return 2
	}

	fmt.Fprintln(stdout, "INFO[0000] sing-box started")
	fmt.Fprintln(stderr, "DEBUG[0000] router: loaded 0 rules")

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	beat := 0
	for {
		select {
		case <-signals:
			if scenario == ScenarioRunIgnoreTerm {
				// Deliberately keep running: only SIGKILL ends this process, which
				// is what the kill path has to cope with.
				continue
			}
			fmt.Fprintln(stdout, "INFO[0000] sing-box stopped")
			writeStatusFile(opts.statusFile, 0)
			return 0
		case <-ticker.C:
			beat++
			fmt.Fprintf(stdout, "INFO[%04d] heartbeat\n", beat)
		}
	}
}

// writePIDFile records the process id so a supervisor can stop a process it no
// longer holds a handle to.
func writePIDFile(path string) {
	if path == "" {
		return
	}
	if err := atomicfile.Write(path, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "fakesingbox: cannot write the pid file: %v\n", err)
	}
}

// statusPayload mirrors what the privileged helper parses back out of
// StatusPath.
type statusPayload struct {
	ExitCode int `json:"exitCode"`
}

// writeStatusFile records the exit code in the same shape the privileged helper
// writes it, so the helper's reader can be tested without root.
func writeStatusFile(path string, code int) {
	if path == "" {
		return
	}
	encoded, err := json.Marshal(statusPayload{ExitCode: code})
	if err != nil {
		return
	}
	if err := atomicfile.Write(path, append(encoded, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "fakesingbox: cannot write the status file: %v\n", err)
	}
}

// syncWriter serialises writes to the log file, which stdout and stderr share.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}
