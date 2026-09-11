package runtime

import (
	"encoding/json"
	"testing"
	"time"
)

func TestStateValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state State
		want  bool
	}{
		{name: "stopped", state: StateStopped, want: true},
		{name: "starting", state: StateStarting, want: true},
		{name: "running", state: StateRunning, want: true},
		{name: "stopping", state: StateStopping, want: true},
		{name: "failed", state: StateFailed, want: true},
		{name: "empty", state: "", want: false},
		{name: "wrong case", state: "running", want: false},
		{name: "unknown", state: "PAUSED", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.state.Valid(); got != tc.want {
				t.Errorf("State(%q).Valid() = %v, want %v", tc.state, got, tc.want)
			}
		})
	}
}

func TestStateBusy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state State
		want  bool
	}{
		{name: "starting is busy", state: StateStarting, want: true},
		{name: "stopping is busy", state: StateStopping, want: true},
		{name: "stopped is idle", state: StateStopped, want: false},
		{name: "running is idle", state: StateRunning, want: false},
		{name: "failed is idle", state: StateFailed, want: false},
		{name: "empty is idle", state: "", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.state.Busy(); got != tc.want {
				t.Errorf("State(%q).Busy() = %v, want %v", tc.state, got, tc.want)
			}
		})
	}
}

// TestStateValuesAreStable pins the wire format the frontend branches on.
func TestStateValuesAreStable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		got  State
		want string
	}{
		{StateStopped, "STOPPED"},
		{StateStarting, "STARTING"},
		{StateRunning, "RUNNING"},
		{StateStopping, "STOPPING"},
		{StateFailed, "FAILED"},
	}
	for _, tc := range tests {
		if string(tc.got) != tc.want {
			t.Errorf("State = %q, want %q", tc.got, tc.want)
		}
	}
}

func TestStoppedIsTheCanonicalSnapshot(t *testing.T) {
	t.Parallel()

	got := Stopped()
	if got.State != StateStopped {
		t.Errorf("State = %q, want %q", got.State, StateStopped)
	}
	if got != (Status{State: StateStopped}) {
		t.Errorf("Stopped() = %+v, want every other field zeroed", got)
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal() = %v, want nil", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if m["state"] != "STOPPED" {
		t.Errorf("state = %v, want STOPPED", m["state"])
	}
}

// TestStatusJSONOmitsEmptyOptionalFields pins which fields the frontend may
// treat as absent versus present-but-zero.
func TestStatusJSONOmitsEmptyOptionalFields(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(Status{})
	if err != nil {
		t.Fatalf("Marshal() = %v, want nil", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	for _, key := range []string{"startedAt", "lastExitCode", "lastError", "lastErrorCode"} {
		if _, ok := m[key]; ok {
			t.Errorf("key %q should be omitted when empty: %s", key, raw)
		}
	}
	for _, key := range []string{
		"state", "pid", "uptimeSeconds", "activeProfileId", "activeRevisionId",
		"binaryVersion", "configPath", "elevated",
	} {
		if _, ok := m[key]; !ok {
			t.Errorf("key %q must always be present: %s", key, raw)
		}
	}

	started := time.Date(2026, time.March, 4, 5, 6, 7, 0, time.UTC)
	exit := 137
	full := Status{
		State:           StateFailed,
		PID:             4242,
		StartedAt:       &started,
		UptimeSeconds:   30,
		LastExitCode:    &exit,
		LastError:       "crashed",
		LastErrorCode:   "RUNTIME_START_FAILED",
		ActiveProfileID: "p1",
	}
	raw, err = json.Marshal(full)
	if err != nil {
		t.Fatalf("Marshal() = %v, want nil", err)
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	for _, key := range []string{"startedAt", "lastExitCode", "lastError", "lastErrorCode"} {
		if _, ok := m[key]; !ok {
			t.Errorf("key %q should be present once set: %s", key, raw)
		}
	}
	if m["pid"] != float64(4242) || m["lastExitCode"] != float64(137) {
		t.Errorf("numeric fields = %v/%v, want 4242/137", m["pid"], m["lastExitCode"])
	}
}
