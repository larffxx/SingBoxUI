package singbox

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
)

// CheckResult is the outcome of `sing-box check`.
//
// It is a value, not an error, because "the configuration is invalid" is a
// normal answer the UI must render — with the validator's own message — while a
// Go error is reserved for the cases where the check could not be performed at
// all (spec §30). Version is the binary that produced the answer, so a revision
// can be recorded against the exact validator that accepted it.
type CheckResult struct {
	// OK is true only when sing-box accepted the configuration.
	OK bool `json:"ok"`
	// Version is the parsed version of the binary that ran the check.
	Version Version `json:"-"`
	// VersionRaw is that version as text, for the JSON carried to the frontend.
	VersionRaw string `json:"version"`
	// Output is the redacted, bounded stdout+stderr of the check.
	Output string `json:"output"`
	// Errors holds the validator's own error lines, already redacted.
	Errors []string `json:"errors"`
}

// maxCheckErrors bounds how many validator error lines are surfaced; a
// malformed configuration can report thousands and the UI cannot use them.
const maxCheckErrors = 20

// Check runs `<binary> check -c <config>`.
//
// A non-zero exit is not a Go error: it returns CheckResult{OK:false} together
// with an *apperr.Error carrying CONFIG_CHECK_FAILED, so a caller can log and
// decorate the failure while still showing the validator's own message
// (spec §30). The timeout is a parameter because the apply pipeline needs a
// short deadline while an explicit "validate" action can afford longer.
func Check(ctx context.Context, binaryPath, configPath string, timeout time.Duration) (CheckResult, error) {
	const op = "singbox.Check"
	result := CheckResult{}
	if strings.TrimSpace(binaryPath) == "" || strings.TrimSpace(configPath) == "" {
		return result, apperr.New(apperr.CodeInvalidArgument, op,
			"both a sing-box executable and a configuration file are required")
	}
	if !IsExecutable(binaryPath) {
		return result, apperr.Newf(apperr.CodeBinaryNotFound, op,
			"no executable file at %s", binaryPath)
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, binaryPath, "check", "-c", configPath)
	output := &boundedBuffer{}
	cmd.Stdout, cmd.Stderr = output, output
	cmd.Stdin = nil

	err := cmd.Run()
	result.Output = redactOutput(output.String())
	result.Errors = checkErrorLines(result.Output)
	if version, parseErr := ParseVersion(result.Output); parseErr == nil {
		result.Version = version
		result.VersionRaw = version.String()
	}

	switch {
	case ctx.Err() != nil:
		return result, apperr.WithDetails(apperr.Wrap(apperr.CodeConfigCheckFailed, op,
			"the configuration check did not finish in time", ctx.Err()), result.Errors...)
	case err != nil && !isExitError(err):
		return result, apperr.Wrap(apperr.CodeBinaryNotFound, op,
			"cannot run "+binaryPath, err)
	case err == nil:
		result.OK = true
		return result, nil
	default:
		// The check ran and rejected the configuration: report the validator's
		// verdict as data plus a typed error for the log.
		return result, apperr.WithDetails(apperr.Wrap(apperr.CodeConfigCheckFailed, op,
			"sing-box rejected the configuration", err), result.Errors...)
	}
}

// checkErrorLines extracts the validator's own error lines.
//
// sing-box logs with a level prefix ("FATAL[0000] …", "ERROR[0000] …"), so the
// level markers are the only stable signal; anything else in the output is
// context, not a verdict. The result is bounded because a single broken rule
// list can produce one line per rule.
func checkErrorLines(output string) []string {
	var out []string
	for _, line := range strings.Split(output, "\n") {
		if !isErrorLine(line) {
			continue
		}
		out = append(out, line)
		if len(out) == maxCheckErrors {
			break
		}
	}
	return out
}

func isErrorLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	upper := strings.ToUpper(trimmed)
	for _, level := range []string{"FATAL", "ERROR"} {
		if !strings.HasPrefix(upper, level) {
			continue
		}
		// Require a separator so a message that merely starts with "ERROR" in a
		// word does not get promoted to a verdict.
		rest := trimmed[len(level):]
		if rest == "" || strings.HasPrefix(rest, "[") || strings.HasPrefix(rest, ":") || strings.HasPrefix(rest, " ") {
			return true
		}
	}
	return false
}
