//go:build windows

package singbox

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/larffxx/singboxui/internal/platform/console"
)

// foreignPIDs lists the sing-box processes of this machine that are running a
// configuration.
//
// `tasklist` is part of every supported Windows install, so the enumeration needs
// no dependency and no elevation; its CSV output is the only dependable format.
// It answers with image names only, and the argument vector that tells a running
// core from a `sing-box check` is therefore read per candidate through
// NtQueryInformationProcess (processCommandLine), which works for the processes
// this application could not otherwise inspect: the core it starts itself runs as
// administrator, and reading its command line needs nothing more than the limited
// query right that Windows grants to every user for every process.
func foreignPIDs(ctx context.Context) ([]int, error) {
	name := ExecutableName("windows")

	cmd := exec.CommandContext(ctx, "tasklist", "/FI", "IMAGENAME eq "+name, "/FO", "CSV", "/NH")
	// tasklist is a console program and this runs on every start attempt.
	console.Windowless(cmd)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.Stdin = nil

	if err := cmd.Run(); err != nil && !isExitError(err) {
		return nil, fmt.Errorf("singbox: tasklist: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	candidates, err := parseTasklistCSV(stdout.String())
	if err != nil {
		return nil, err
	}
	out := make([]int, 0, len(candidates))
	for _, pid := range candidates {
		if runningConfiguration(pid) {
			out = append(out, pid)
		}
	}
	return out, nil
}

// parseTasklistCSV reads the pids out of a tasklist CSV answer. "INFO: No tasks
// are running which match the specified criteria." is the normal empty answer.
func parseTasklistCSV(output string) ([]int, error) {
	raw := strings.TrimSpace(output)
	if raw == "" || strings.HasPrefix(strings.ToUpper(raw), "INFO:") {
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
		if err != nil || pid <= 0 {
			continue
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

// runningConfiguration reports whether a process was launched to run a
// configuration. A process whose command line cannot be read counts as one: it is
// then reported to the user, which is the harmless direction of the two.
func runningConfiguration(pid int) bool {
	line, err := processCommandLine(pid)
	if err != nil {
		return true
	}
	return runningConfigurationArgs(strings.Fields(line))
}

// processCommandLineInformation is ProcessCommandLineInformation from
// PROCESSINFOCLASS (winternl.h), available since Windows 8.1.
const processCommandLineInformation = 60

// ntQueryInformationProcess is the native call behind a long list of Windows
// process facts. The information class used here has no Win32 wrapper, and the
// Win32 alternatives cannot read the command line of a process at a higher
// integrity level (`Get-CimInstance Win32_Process` answers with an empty
// CommandLine for exactly those, while this call answers with the full line).
var ntQueryInformationProcess = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtQueryInformationProcess")

// unicodeString mirrors UNICODE_STRING (winternl.h). The structure is 16 bytes on
// the 64-bit targets this application builds for.
type unicodeString struct {
	Length        uint16
	MaximumLength uint16
	_             uint32
	Buffer        *uint16
}

// processCommandLine reads the command line a process was started with.
//
// The call is two steps: the first asks for the size of the answer, the second
// fills a buffer of that size. The answer is a UNICODE_STRING whose Buffer points
// into the same buffer, so nothing is allocated by the kernel and nothing has to
// be freed.
func processCommandLine(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("singbox: invalid pid %d", pid)
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", fmt.Errorf("singbox: open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(handle)

	var needed uint32
	// A zero-length buffer is refused with STATUS_INFO_LENGTH_MISMATCH, and that
	// answer is the protocol, not a failure.
	status, _, _ := ntQueryInformationProcess.Call(
		uintptr(handle), processCommandLineInformation, 0, 0, uintptr(unsafe.Pointer(&needed)))
	if needed == 0 {
		return "", fmt.Errorf("singbox: command line of %d: status=0x%08x without a length", pid, uint32(status))
	}
	buffer := make([]byte, needed)
	status, _, _ = ntQueryInformationProcess.Call(
		uintptr(handle), processCommandLineInformation,
		uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), uintptr(unsafe.Pointer(&needed)))
	if status != 0 {
		return "", fmt.Errorf("singbox: command line of %d: status=0x%08x", pid, uint32(status))
	}
	line := (*unicodeString)(unsafe.Pointer(&buffer[0]))
	if line.Buffer == nil || line.Length == 0 {
		return "", nil
	}
	// The length comes from the kernel, so it is bounded by the buffer that was
	// asked for before it is used to read the string.
	length := int(line.Length) / 2
	if maximum := len(buffer) / 2; length > maximum {
		length = maximum
	}
	return windows.UTF16ToString(unsafe.Slice(line.Buffer, length)), nil
}
