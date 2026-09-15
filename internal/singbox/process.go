package singbox

import (
	"context"
	"os"
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
// out: refusing to start because a check is running would be wrong. Each platform
// answers that question its own way (process_posix.go, process_windows.go), and
// both read the argument vector of the candidates, so the filter is the same one
// on every machine.
//
// It deliberately reports every running sing-box process it can see, including
// any this application started: only the runtime supervisor knows its own PIDs,
// and a port package must not keep that kind of process bookkeeping. The
// supervisor subtracts what it owns and uses the remainder to warn the user
// (spec §26, §27) instead of letting two sing-box instances fight over TUN
// devices, ports and the routing table.
func Foreign(ctx context.Context) ([]int, error) {
	const op = "singbox.Foreign"
	pids, err := foreignPIDs(ctx)
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

// runningConfigurationArgs reports whether an argument vector belongs to a
// process that was launched to run a configuration, which is what the command
// line says: `sing-box run -c …`.
//
// A command line that cannot be read is treated as running a configuration: a
// process the application cannot inspect is one it must not assume to be
// harmless, and reporting it too much only produces a warning.
func runningConfigurationArgs(fields []string) bool { return hasRunArgument(fields) }

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
