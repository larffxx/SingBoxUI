// Package runtime models the lifecycle of the managed sing-box process.
package runtime

import "time"

// State is the lifecycle state of the runtime supervisor.
type State string

const (
	StateStopped  State = "STOPPED"
	StateStarting State = "STARTING"
	StateRunning  State = "RUNNING"
	StateStopping State = "STOPPING"
	StateFailed   State = "FAILED"
)

// Valid reports whether the state is known.
func (s State) Valid() bool {
	switch s {
	case StateStopped, StateStarting, StateRunning, StateStopping, StateFailed:
		return true
	}
	return false
}

// Busy reports whether a lifecycle transition is in flight.
func (s State) Busy() bool { return s == StateStarting || s == StateStopping }

// Status is the typed snapshot exposed to the frontend (spec §22).
type Status struct {
	State            State      `json:"state"`
	PID              int        `json:"pid"`
	StartedAt        *time.Time `json:"startedAt,omitempty"`
	UptimeSeconds    int64      `json:"uptimeSeconds"`
	ActiveProfileID  string     `json:"activeProfileId"`
	ActiveRevisionID string     `json:"activeRevisionId"`
	BinaryVersion    string     `json:"binaryVersion"`
	ConfigPath       string     `json:"configPath"`
	Elevated         bool       `json:"elevated"`
	LastExitCode     *int       `json:"lastExitCode,omitempty"`
	LastError        string     `json:"lastError,omitempty"`
	LastErrorCode    string     `json:"lastErrorCode,omitempty"`
}

// Stopped returns the canonical stopped snapshot.
func Stopped() Status { return Status{State: StateStopped} }
