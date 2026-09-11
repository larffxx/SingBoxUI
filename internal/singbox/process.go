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

// Foreign reports the PIDs of sing-box processes that are running but were not
// started by this application — a manual CLI run, or a child left behind by the
// legacy Java UI.
//
// It deliberately reports every sing-box process it can see, including any this
// application started: only the runtime supervisor knows its own PIDs, and a
// port package must not keep that kind of process bookkeeping. The supervisor
// subtracts what it owns and uses the remainder to warn the user (spec §26,
// §27) instead of letting two sing-box instances fight over TUN devices,
// ports and the routing table.
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

// foreignPIDsPgrep asks pgrep for processes whose executable is named name.
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
	return parsePIDLines(stdout.String()), nil
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
