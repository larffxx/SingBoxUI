package events

import (
	"encoding/json"
	"testing"
)

// TestEventNamesAreStable pins the frontend contract: these strings are matched
// by listeners in the UI, so a rename is a breaking change.
func TestEventNamesAreStable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "runtime status", got: RuntimeStatus, want: "runtime:status"},
		{name: "runtime log", got: RuntimeLog, want: "runtime:log"},
		{name: "traffic snapshot", got: TrafficSnapshot, want: "traffic:snapshot"},
		{name: "binary progress", got: BinaryProgress, want: "binary:progress"},
		{name: "config apply progress", got: ConfigApplyProgress, want: "config:apply"},
		{name: "app notice", got: AppNotice, want: "app:notice"},
		{name: "profiles changed", got: ProfilesChanged, want: "profiles:changed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.got != tc.want {
				t.Errorf("event name = %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestEmitterFuncForwardsNameAndPayload(t *testing.T) {
	t.Parallel()

	var _ Emitter = EmitterFunc(func(string, any) {})

	var gotName string
	var gotPayload any
	var emitter Emitter = EmitterFunc(func(name string, payload any) {
		gotName = name
		gotPayload = payload
	})

	payload := map[string]any{"state": "RUNNING"}
	emitter.Emit(RuntimeStatus, payload)
	if gotName != RuntimeStatus {
		t.Errorf("name = %q, want %q", gotName, RuntimeStatus)
	}
	if m, ok := gotPayload.(map[string]any); !ok || m["state"] != "RUNNING" {
		t.Errorf("payload = %#v, want the forwarded map", gotPayload)
	}
}

func TestNoopEmitterDiscards(t *testing.T) {
	t.Parallel()

	var _ Emitter = Noop{}

	// The headless path must accept any name and payload without panicking.
	Noop{}.Emit(RuntimeLog, nil)
	Noop{}.Emit("", struct{}{})
}

func TestNoticeJSONContract(t *testing.T) {
	t.Parallel()

	full := Notice{
		Level:      "error",
		Title:      "Startup failed",
		Message:    "the managed binary is missing",
		Code:       "BINARY_NOT_FOUND",
		Operation:  "startup",
		Details:    []string{"checked /opt/sing-box"},
		Persistent: true,
		OccurredAt: "2026-01-02T03:04:05Z",
	}
	raw, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("Marshal() = %v, want nil", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode notice: %v", err)
	}
	for _, key := range []string{"level", "title", "message", "code", "operation", "details", "persistent", "occurredAt"} {
		if _, ok := m[key]; !ok {
			t.Errorf("notice is missing key %q: %s", key, raw)
		}
	}
	if m["persistent"] != true {
		t.Errorf("persistent = %v, want true", m["persistent"])
	}
	details, ok := m["details"].([]any)
	if !ok || len(details) != 1 || details[0] != "checked /opt/sing-box" {
		t.Errorf("details = %#v, want the single detail", m["details"])
	}
}

func TestNoticeJSONOmitsEmptyOptionalFields(t *testing.T) {
	t.Parallel()

	minimal := Notice{Level: "info", Title: "hi", Message: "body", OccurredAt: "2026-01-02T03:04:05Z"}
	raw, err := json.Marshal(minimal)
	if err != nil {
		t.Fatalf("Marshal() = %v, want nil", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode notice: %v", err)
	}
	for _, key := range []string{"code", "operation", "details"} {
		if _, ok := m[key]; ok {
			t.Errorf("key %q should be omitted when empty: %s", key, raw)
		}
	}
	// Persistent and OccurredAt carry no omitempty and are always present.
	for _, key := range []string{"persistent", "occurredAt"} {
		if _, ok := m[key]; !ok {
			t.Errorf("key %q must always be present: %s", key, raw)
		}
	}
}
