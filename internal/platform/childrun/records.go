package childrun

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/larffxx/singboxui/internal/atomicfile"
)

// File modes for the bookkeeping files.
//
// The privileged helper writes pid, status and log files as root, while the
// unprivileged application has to read them back (internal-contracts.md §3), so
// they must be world readable. They always live in the application runtime
// directory, which is created 0700 and owned by the user, so they stay private in
// practice.
const (
	recordFileMode os.FileMode = 0o644
	logFileMode    os.FileMode = 0o644
	runtimeDirMode os.FileMode = 0o700
)

// errInvalidPIDFile is returned when a pid file exists but does not carry the
// pid and start time that make the process identifiable.
var errInvalidPIDFile = errors.New("childrun: pid file has no usable pid and start time")

// PIDFile is the on-disk record of a managed sing-box process.
//
// It carries the process start time next to the pid, so a recycled pid can never
// be mistaken for the process the application started: Terminate and Kill verify
// identity before signalling (internal-contracts.md §3).
type PIDFile struct {
	// PID is the operating-system process id.
	PID int `json:"pid"`
	// StartTime is the process start instant as unix nanoseconds, taken from the
	// operating system (see StartTimeNano).
	StartTime int64 `json:"startTime"`
	// BinaryPath is the executable the record was written for; informational.
	BinaryPath string `json:"binaryPath,omitempty"`
	// RecordedAt is when the record was written.
	RecordedAt time.Time `json:"recordedAt"`
}

// Valid reports whether the record can be used to identify a process.
func (p PIDFile) Valid() bool { return p.PID > 0 && p.StartTime > 0 }

// Matches reports whether the process running under this record's pid is the one
// the record describes.
//
// A pid is recycled by the kernel long before a run directory is cleaned up, so
// "the pid exists" is not the same question as "our process is running"
// (ADR 012): the start time must match as well, and a process that has exited but
// whose parent has not reaped it yet is already gone. This is the identity check
// a stop performs before it signals anything, in the one place both the stop and
// the reader of a record can use it.
func (p PIDFile) Matches() bool {
	if !p.Valid() || !Alive(p.PID) {
		return false
	}
	start, err := StartTimeNano(p.PID)
	if err != nil {
		// The process is gone, so nothing can match it.
		return false
	}
	return sameIdentity(start, p.StartTime)
}

// WritePIDFile writes the record atomically, so a concurrently starting reader
// never observes a partial file.
func WritePIDFile(path string, p PIDFile) error {
	if !p.Valid() {
		return fmt.Errorf("childrun: refusing to write a pid file without pid and start time to %s", path)
	}
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("encode pid file %s: %w", path, err)
	}
	if err := atomicfile.Write(path, data, recordFileMode); err != nil {
		return fmt.Errorf("write pid file %s: %w", path, err)
	}
	return nil
}

// ReadPIDFile reads and validates a pid file. A missing file returns an error
// wrapping fs.ErrNotExist.
func ReadPIDFile(path string) (PIDFile, error) {
	var p PIDFile
	data, err := os.ReadFile(path)
	if err != nil {
		return p, fmt.Errorf("read pid file %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return p, fmt.Errorf("parse pid file %s: %w", path, err)
	}
	if !p.Valid() {
		return p, fmt.Errorf("%s: %w", path, errInvalidPIDFile)
	}
	return p, nil
}

// Status is the exit record the supervisor of a managed process writes when the
// process ends (internal-contracts.md §3).
type Status struct {
	// PID is the process the record belongs to.
	PID int `json:"pid"`
	// ExitCode is the child exit code; negative when the process was terminated
	// by a signal.
	ExitCode int `json:"exitCode"`
	// StartedAt and EndedAt bracket the process lifetime.
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
	// Error carries a human-readable reason when the process could not be waited
	// for cleanly.
	Error string `json:"error,omitempty"`
}

// WriteStatus writes the exit record atomically. A failure to record the status
// is reported, but the caller has already observed the exit.
func WriteStatus(path string, s Status) error {
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("encode status file %s: %w", path, err)
	}
	if err := atomicfile.Write(path, data, recordFileMode); err != nil {
		return fmt.Errorf("write status file %s: %w", path, err)
	}
	return nil
}

// ReadStatus reads an exit record. A missing file returns an error wrapping
// fs.ErrNotExist, which is the normal state while the process still runs.
func ReadStatus(path string) (Status, error) {
	var s Status
	data, err := os.ReadFile(path)
	if err != nil {
		return s, fmt.Errorf("read status file %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("parse status file %s: %w", path, err)
	}
	return s, nil
}
