// Package events defines the Wails event names and the emitter port used by the
// application layer to push state to the frontend.
//
// The application layer never imports the Wails runtime: it depends on Emitter,
// which the composition root satisfies with a Wails-backed implementation
// (spec §46, §47: events instead of polling).
package events

// Event names. They are part of the frontend contract and must stay stable.
const (
	// RuntimeStatus carries a runtime.Status snapshot on every state change.
	RuntimeStatus = "runtime:status"
	// RuntimeLog carries a batch of log records (never one event per line).
	RuntimeLog = "runtime:log"
	// TrafficSnapshot carries a traffic.Snapshot while the runtime is RUNNING.
	TrafficSnapshot = "traffic:snapshot"
	// BinaryProgress carries a binary.Progress during install/update.
	BinaryProgress = "binary:progress"
	// ConfigApplyProgress carries a config.ApplyProgress per apply stage.
	ConfigApplyProgress = "config:apply"
	// AppNotice carries a Notice that needs a persistent surface in the UI.
	AppNotice = "app:notice"
	// ProfilesChanged fires when the profile list changes outside a bound call
	// (for example a first-launch import).
	ProfilesChanged = "profiles:changed"
)

// Notice is a user-visible message that must not be hidden in the logs: startup
// failures, auto-connect failures and downgraded functionality (spec §51).
type Notice struct {
	Level     string   `json:"level"` // info | warning | error
	Title     string   `json:"title"`
	Message   string   `json:"message"`
	Code      string   `json:"code,omitempty"`
	Operation string   `json:"operation,omitempty"`
	Details   []string `json:"details,omitempty"`
	// Persistent notices stay visible until dismissed; transient ones may fade.
	Persistent bool   `json:"persistent"`
	OccurredAt string `json:"occurredAt"`
}

// Emitter publishes an event to the frontend.
type Emitter interface {
	Emit(name string, payload any)
}

// EmitterFunc adapts a function to the Emitter interface.
type EmitterFunc func(name string, payload any)

// Emit implements Emitter.
func (f EmitterFunc) Emit(name string, payload any) { f(name, payload) }

// Noop is an Emitter for tests and headless paths.
type Noop struct{}

// Emit implements Emitter and discards the payload.
func (Noop) Emit(string, any) {}
