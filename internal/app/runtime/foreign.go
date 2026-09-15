/**
 * A sing-box the application does not own (ADR 012).
 *
 * The failure this file answers: a core that outlives the window. Its parent is
 * gone, so nothing holds a handle to stop it, and the next start dies with a bind
 * error from inside the child — while the log tail shown to the user mixes lines
 * the surviving process is still writing into the same run directory.
 *
 * Two things are therefore needed, and each is built on what already exists:
 * the process is *found* through the record every launch writes (pid + start
 * time, internal-contracts.md §3), and it is *stopped* through the same narrow
 * helper that stops a supervised one — the privileged helper has had a verified
 * stop since the beginning, it simply had no caller for a process this instance
 * did not start.
 */
package runtime

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/platform/childrun"
)

// ForeignProcess is a running sing-box that this supervisor does not own.
type ForeignProcess struct {
	// PID is the recorded process id.
	PID int `json:"pid"`
	// RevisionID is the run directory the record lives in: the revision that
	// process was launched for.
	RevisionID string `json:"revisionId"`
	// PIDPath is the record a stop must be asked for. It is the only handle the
	// privileged helper accepts, which is why a process without a record cannot
	// be stopped from here.
	PIDPath string `json:"pidPath"`
	// BinaryPath is the sing-box the record names.
	BinaryPath string `json:"binaryPath"`
	// StartedAt is when the process was launched, from its record.
	StartedAt time.Time `json:"startedAt"`
}

// ForeignProcesses returns the recorded sing-box processes this supervisor does
// not own, sorted by pid.
//
// Nothing is signalled and no process is spawned: the answer comes from the run
// directory of every revision this data directory has ever run. A record whose
// process has exited is not reported — a stopped profile must not look like a
// running one.
func (s *Supervisor) ForeignProcesses() []ForeignProcess {
	own := s.ownPID()
	entries, err := os.ReadDir(s.deps.Paths.RuntimeDir)
	if err != nil {
		// No run directory means nothing was ever launched from here.
		return nil
	}
	out := make([]ForeignProcess, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		revisionID := entry.Name()
		pidPath := filepath.Join(s.deps.Paths.RuntimeDir, revisionID, runPIDName)
		record, err := childrun.ReadPIDFile(pidPath)
		if err != nil {
			// Unreadable, foreign-owned (an elevated launch records as root) or
			// describing a process that no longer exists: nothing to report.
			continue
		}
		if record.PID == own || !record.Matches() {
			continue
		}
		out = append(out, ForeignProcess{
			PID:        record.PID,
			RevisionID: revisionID,
			PIDPath:    pidPath,
			BinaryPath: record.BinaryPath,
			StartedAt:  record.RecordedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}

// StopForeign stops every recorded sing-box this supervisor does not own and
// reports how many were stopped. A process that cannot be stopped does not stop
// the others: the error names the first failure and the count says what worked.
func (s *Supervisor) StopForeign(ctx context.Context) (int, error) {
	const op = "runtime.StopForeign"
	// The same lock as start and stop: a foreign core is state the supervisor
	// must not race with.
	s.opMu.Lock()
	defer s.opMu.Unlock()

	foreign := s.ForeignProcesses()
	if len(foreign) == 0 {
		return 0, nil
	}
	stopped := 0
	var firstErr error
	for _, process := range foreign {
		if err := s.deps.Privilege.Stop(ctx, process.PIDPath, false); err != nil {
			if firstErr == nil {
				firstErr = apperr.Wrap(apperr.CodeRuntimeStopFailed, op,
					"the sing-box that outlived the application could not be stopped", err)
			}
			continue
		}
		stopped++
		s.deps.Logger.Info("stopped a sing-box the application did not own",
			"operation", op, "pid", process.PID, "revisionId", process.RevisionID)
	}
	if firstErr != nil {
		return stopped, firstErr
	}
	return stopped, nil
}

// ownPID is the process this supervisor started, or zero.
func (s *Supervisor) ownPID() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.proc == nil {
		return 0
	}
	return s.proc.PID()
}

// refuseForeignLocked refuses a start while a sing-box that this supervisor does
// not own is running a configuration (ADR 012).
//
// Two sing-boxes cannot both hold the TUN device, the Clash API port and the
// routing table. The second one dies with a bind error from inside the child,
// reported through a log file the first one is still writing to, so the message
// the user sees explains nothing — and the profile they asked for simply does not
// start. Naming the process is the answer: the runtime screen can stop the ones
// that left a record, and a process launched by hand is named so it can be
// stopped wherever it was started.
//
// The enumeration is a diagnostic, never a permission: a machine that cannot be
// searched (a missing pgrep, a denied ps) starts normally (spec §26).
func (s *Supervisor) refuseForeignLocked(ctx context.Context, op string) error {
	recorded := s.ForeignProcesses()
	if len(recorded) > 0 {
		pids := make([]string, 0, len(recorded))
		for _, process := range recorded {
			pids = append(pids, strconv.Itoa(process.PID))
		}
		return apperr.WithDetails(
			apperr.Newf(apperr.CodeRuntimeAlreadyRunning, op,
				"sing-box is already running outside the application (pid %s): stop it and start again", pids[0]),
			"pids="+strings.Join(pids, ","),
			"revisions="+strings.Join(revisionIDs(recorded), ","),
			"the Runtime screen stops a process that has a record")
	}

	running, err := s.deps.Foreign(ctx)
	if err != nil {
		s.deps.Logger.Warn("could not look for sing-box processes outside the application",
			"operation", op, "error", err)
		return nil
	}
	own := s.ownPID()
	unrecorded := make([]string, 0, len(running))
	for _, pid := range running {
		if pid != own {
			unrecorded = append(unrecorded, strconv.Itoa(pid))
		}
	}
	if len(unrecorded) == 0 {
		return nil
	}
	// A process without a record cannot be stopped by the application: the
	// privileged helper only stops what a pid file describes.
	return apperr.WithDetails(
		apperr.Newf(apperr.CodeRuntimeAlreadyRunning, op,
			"sing-box (pid %s) is running a configuration that SingBoxUI did not start: stop it and start again", unrecorded[0]),
		"pids="+strings.Join(unrecorded, ","),
		"it was started outside the application, so it must be stopped there")
}

// revisionIDs lists the revisions of the reported processes.
func revisionIDs(processes []ForeignProcess) []string {
	out := make([]string, 0, len(processes))
	for _, process := range processes {
		out = append(out, process.RevisionID)
	}
	return out
}
