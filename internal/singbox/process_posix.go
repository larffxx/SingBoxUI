//go:build !windows

package singbox

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/larffxx/singboxui/internal/domain/apperr"
)

// foreignPIDs asks pgrep for processes whose executable is named like sing-box,
// then keeps the ones that are running a configuration.
//
// pgrep matches on the executable's name and needs no privileges to see other
// users' processes, and `ps` reports the argument vector of each candidate, so
// the platform needs no help from this package to tell `run` from `check`.
func foreignPIDs(ctx context.Context) ([]int, error) {
	name := strings.TrimSuffix(ExecutableName(runtime.GOOS), ".exe")
	candidates, err := pgrepPIDs(ctx, name)
	if err != nil {
		return nil, err
	}
	out := make([]int, 0, len(candidates))
	for _, pid := range candidates {
		if runningConfiguration(ctx, pid) {
			out = append(out, pid)
		}
	}
	return out, nil
}

// pgrepPIDs asks pgrep for processes whose executable carries this name.
func pgrepPIDs(ctx context.Context, name string) ([]int, error) {
	cmd := exec.CommandContext(ctx, "pgrep", "-x", name)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.Stdin = nil

	err := cmd.Run()
	if err != nil {
		// pgrep exits 1 when nothing matched, which is not a failure: an empty
		// result is the normal answer on a clean machine.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.ExitCode() == 1 {
				return nil, nil
			}
			return nil, apperr.Newf(apperr.CodeInternal, "singbox.Foreign",
				"pgrep failed: %s", strings.TrimSpace(stderr.String()))
		}
		return nil, err
	}
	return parsePIDLines(stdout.String()), nil
}

// runningConfiguration reports whether a process was launched to run a
// configuration, which is what its command line says.
func runningConfiguration(ctx context.Context, pid int) bool {
	cmd := exec.CommandContext(ctx, "ps", "-o", "command=", "-p", strconv.Itoa(pid))
	var stdout strings.Builder
	cmd.Stdout = &stdout
	cmd.Stdin = nil
	if err := cmd.Run(); err != nil {
		return true
	}
	return runningConfigurationArgs(strings.Fields(stdout.String()))
}
