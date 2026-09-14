package singbox

import (
	"context"
	"encoding/csv"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/larffxx/singboxui/internal/domain/apperr"
)

// Foreign reports the PIDs of sing-box processes that are running a
// configuration but were not started by this application — a manual CLI run, or
// a child left behind by an earlier session of the application itself (ADR 012).
//
// A `sing-box check` shares the executable name but neither the TUN device nor
// the ports, so processes that were not launched to run a configuration are left
// out: refusing to start because a check is running would be wrong. Windows is
// the exception — tasklist cannot show arguments, so every sing-box process is
// reported there.
//
// It deliberately reports every running sing-box process it can see, including
// any this application started: only the runtime supervisor knows its own PIDs,
// and a port package must not keep that kind of process bookkeeping. The
// supervisor subtracts what it owns and uses the remainder to warn the user
// (spec §26, §27) instead of letting two sing-box instances fight over TUN
// devices, ports and the routing table.
func Foreign(ctx context.Context) ([]int, error) {
	const op = "singbox.Foreign"
	name := strings.TrimSuffix(ExecutableName(runtime.GOOS), ".exe")

	var (
		pids []int
		err  error
	)
	switch runtime.GOOS {
	case "windows":
		pids, err = foreignPIDsWindows(ctx, name+".exe")
	default:
		// darwin and linux both ship pgrep, which matches on the executable's
		// name and needs no privileges to see other users' processes.
		pids, err = foreignPIDsPgrep(ctx, name)
	}
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, op,
			"cannot enumerate sing-box processes", err)
	}

	self := os.Getpid()
	out := make([]int, 0, len(pids))
	seen := make(map[int]bool, len(pids))
	for _, pid := range pids {
		// A process cannot be foreign to itself, and pid 0 means "every process
		// in the group" to the signal APIs, so it must never escape.
		if pid <= 0 || pid == self || seen[pid] {
			continue
		}
		seen[pid] = true
		out = append(out, pid)
	}
	sort.Ints(out)
	return out, nil
}

// foreignPIDsPgrep asks pgrep for processes whose executable is named name, then
// keeps the ones that are running a configuration.
func foreignPIDsPgrep(ctx context.Context, name string) ([]int, error) {
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
	candidates := parsePIDLines(stdout.String())
	out := make([]int, 0, len(candidates))
	for _, pid := range candidates {
		if runningConfiguration(ctx, pid) {
			out = append(out, pid)
		}
	}
	return out, nil
}

// runningConfiguration reports whether a process was launched to run a
// configuration, which is what the command line says: `sing-box run -c …`.
//
// A command line that cannot be read is treated as running a configuration: a
// process the application cannot inspect is one it must not assume to be
// harmless, and reporting it too much only produces a warning.
func runningConfiguration(ctx context.Context, pid int) bool {
	cmd := exec.CommandContext(ctx, "ps", "-o", "command=", "-p", strconv.Itoa(pid))
	var stdout strings.Builder
	cmd.Stdout = &stdout
	cmd.Stdin = nil
	if err := cmd.Run(); err != nil {
		return true
	}
	return hasRunArgument(strings.Fields(stdout.String()))
}

// hasRunArgument reports whether an argument vector contains the `run`
// subcommand. Arguments are compared whole, so a path or a flag value that
// merely contains the word cannot match.
func hasRunArgument(fields []string) bool {
	for _, field := range fields {
		if field == "run" {
			return true
		}
	}
	return false
}

// foreignPIDsWindows asks tasklist for processes whose image is name.
//
// tasklist is part of every supported Windows install, so this needs no
// dependency and no elevation; its CSV output is the only dependable format.
func foreignPIDsWindows(ctx context.Context, name string) ([]int, error) {
	cmd := exec.CommandContext(ctx, "tasklist", "/FI", "IMAGENAME eq "+name, "/FO", "CSV", "/NH")
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.Stdin = nil

	if err := cmd.Run(); err != nil && !isExitError(err) {
		return nil, err
	}
	raw := strings.TrimSpace(stdout.String())
	if raw == "" || strings.HasPrefix(strings.ToUpper(raw), "INFO:") {
		// "INFO: No tasks are running which match the specified criteria."
		return nil, nil
	}
	reader := csv.NewReader(strings.NewReader(raw))
	reader.FieldsPerRecord = -1
	var pids []int
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(record) < 2 {
			// A malformed row is not worth failing the whole check over.
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(record[1]))
		if err != nil {
			continue
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

// parsePIDLines reads one PID per line, skipping anything unparsable so a stray
// warning on stdout cannot be mistaken for a process.
func parsePIDLines(output string) []int {
	var pids []int
	for _, line := range strings.Split(output, "\n") {
		pid, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || pid <= 0 {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}
