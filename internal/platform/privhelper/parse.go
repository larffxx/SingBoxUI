package privhelper

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// Exit codes of the helper. The launching side branches on these, so they are
// part of the protocol.
const (
	// ExitOK means the operation succeeded.
	ExitOK = 0
	// ExitFailure means the operation was rejected by the environment or failed
	// at run time.
	ExitFailure = 1
	// ExitUsage means the invocation was refused before anything was attempted:
	// unknown operation, unknown flag or a request that failed validation.
	ExitUsage = 2
	// ExitCancelled means the local elevation prompt was dismissed before the
	// helper could start; only the macOS launcher produces it.
	ExitCancelled = 3
)

// Usage is the helper's help text. It is the only way to ask the helper what it
// does: there is no capability probe and no generic mode.
func Usage() string {
	return `singboxui-priv - narrow privileged helper of SingBoxUI

usage:
  singboxui-priv start --data-dir DIR --binary PATH --config PATH --log PATH
                       --pid PATH --status PATH [--work-dir DIR] [--reason TEXT]
  singboxui-priv stop  --data-dir DIR --pid PATH [--force]
  singboxui-priv help

The helper starts or stops one sing-box process. Every path must be absolute and
inside --data-dir, and --binary must be the sing-box executable inside it.
There is no other operation and no free-form argument.

exit codes: 0 success, 1 failure, 2 refused invocation, 3 elevation dismissed
`
}

// ParseStartArgs parses one start invocation.
//
// It is deliberately strict: only the documented flags are accepted, positional
// arguments are refused, and validation happens in the caller before anything is
// run, so the helper cannot be talked into executing something else
// (internal-contracts.md §3).
func ParseStartArgs(args []string) (Spec, error) {
	var spec Spec
	fs := newFlagSet(OpStart)
	fs.StringVar(&spec.DataDir, FlagDataDir, "", "application data directory")
	fs.StringVar(&spec.BinaryPath, FlagBinary, "", "sing-box executable")
	fs.StringVar(&spec.ConfigPath, FlagConfig, "", "sing-box configuration")
	fs.StringVar(&spec.WorkDir, FlagWorkDir, "", "child working directory")
	fs.StringVar(&spec.LogPath, FlagLog, "", "log file")
	fs.StringVar(&spec.PIDPath, FlagPID, "", "pid file")
	fs.StringVar(&spec.StatusPath, FlagStatus, "", "exit status file")
	fs.StringVar(&spec.Reason, FlagReason, "", "elevation reason")
	if err := parse(fs, OpStart, args); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

// ParseStopArgs parses one stop invocation.
func ParseStopArgs(args []string) (StopRequest, error) {
	var req StopRequest
	fs := newFlagSet(OpStop)
	fs.StringVar(&req.DataDir, FlagDataDir, "", "application data directory")
	fs.StringVar(&req.PIDPath, FlagPID, "", "pid file")
	fs.BoolVar(&req.Force, FlagForce, false, "kill without a graceful stop")
	if err := parse(fs, OpStop, args); err != nil {
		return StopRequest{}, err
	}
	return req, nil
}

// newFlagSet builds an operation's flag set. Errors are collected by the caller;
// the set never writes to the process output itself.
func newFlagSet(operation string) *flag.FlagSet {
	fs := flag.NewFlagSet(operation, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

// parse runs a flag set and turns its failure into a single sentence.
func parse(fs *flag.FlagSet, operation string, args []string) error {
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("privhelper: %s: %w", operation, err)
	}
	if rest := fs.Args(); len(rest) > 0 {
		return fmt.Errorf("privhelper: %s accepts no positional arguments, got %q",
			operation, strings.Join(rest, " "))
	}
	return nil
}

// IsHelp reports whether an invocation asks for the usage text.
func IsHelp(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "help", "-h", "--help", "-help", "usage":
		return len(args) == 1
	default:
		return false
	}
}
