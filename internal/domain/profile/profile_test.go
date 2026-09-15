package profile

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSourceValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  Source
		want bool
	}{
		{name: "manual", src: SourceManual, want: true},
		{name: "import", src: SourceImport, want: true},
		{name: "share-import", src: SourceShareImport, want: true},
		{name: "template", src: SourceTemplate, want: true},
		{name: "rollback", src: SourceRollback, want: true},
		{name: "migration", src: SourceMigration, want: true},
		{name: "empty", src: "", want: false},
		{name: "wrong case", src: "Manual", want: false},
		{name: "unknown", src: "generated", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.src.Valid(); got != tc.want {
				t.Errorf("Source(%q).Valid() = %v, want %v", tc.src, got, tc.want)
			}
		})
	}
}

// TestSourceValuesAreStable pins the persisted strings: they are stored in
// SQLite, so a rename silently corrupts every existing revision row.
func TestSourceValuesAreStable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		got  Source
		want string
	}{
		{SourceManual, "manual"},
		{SourceImport, "import"},
		{SourceShareImport, "share-import"},
		{SourceTemplate, "template"},
		{SourceRollback, "rollback"},
		{SourceMigration, "migration"},
	}
	for _, tc := range tests {
		if string(tc.got) != tc.want {
			t.Errorf("Source = %q, want %q", tc.got, tc.want)
		}
	}
}

func TestValidationStatusValuesAreStable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		got  ValidationStatus
		want string
	}{
		{StatusUnknown, "unknown"},
		{StatusPassed, "passed"},
		{StatusFailed, "failed"},
		{StatusStale, "stale"},
	}
	for _, tc := range tests {
		if string(tc.got) != tc.want {
			t.Errorf("ValidationStatus = %q, want %q", tc.got, tc.want)
		}
	}
}

func TestRevisionIsActive(t *testing.T) {
	t.Parallel()

	rev := Revision{ID: "rev-2"}
	tests := []struct {
		name string
		p    Profile
		want bool
	}{
		{name: "active", p: Profile{ActiveRevisionID: "rev-2"}, want: true},
		{name: "different revision", p: Profile{ActiveRevisionID: "rev-1"}, want: false},
		{name: "no active revision", p: Profile{}, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := rev.IsActive(tc.p); got != tc.want {
				t.Errorf("IsActive(%+v) = %v, want %v", tc.p, got, tc.want)
			}
		})
	}
}

func TestRevisionJSONContract(t *testing.T) {
	t.Parallel()

	rev := Revision{
		ID:                   "rev-1",
		ProfileID:            "prof-1",
		ParentRevisionID:     "rev-0",
		CreatedAt:            time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
		Source:               SourceManual,
		ConfigJSON:           `{"log":{"level":"info"}}`,
		StructuralValidation: StatusPassed,
		SingBoxValidation:    StatusStale,
		SingBoxVersion:       "1.11.0",
		Comment:              "first",
	}

	raw, err := json.Marshal(rev)
	if err != nil {
		t.Fatalf("Marshal() = %v, want nil", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode marshalled revision: %v", err)
	}
	for _, key := range []string{
		"id", "profileId", "parentRevisionId", "createdAt", "source", "configJson",
		"structuralValidationStatus", "singBoxValidationStatus", "singBoxVersion", "comment",
	} {
		if _, ok := m[key]; !ok {
			t.Errorf("marshalled revision is missing key %q: %s", key, raw)
		}
	}
	if m["source"] != "manual" {
		t.Errorf("source = %v, want manual", m["source"])
	}
	if m["structuralValidationStatus"] != "passed" || m["singBoxValidationStatus"] != "stale" {
		t.Errorf("validation statuses = %v/%v, want passed/stale",
			m["structuralValidationStatus"], m["singBoxValidationStatus"])
	}

	var out Revision
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode revision: %v", err)
	}
	if out.ID != rev.ID || out.ProfileID != rev.ProfileID || out.ParentRevisionID != rev.ParentRevisionID {
		t.Errorf("identifiers = %+v, want %+v", out, rev)
	}
	if out.Source != rev.Source || out.ConfigJSON != rev.ConfigJSON || out.Comment != rev.Comment {
		t.Errorf("fields = %+v, want %+v", out, rev)
	}
	if !out.CreatedAt.Equal(rev.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", out.CreatedAt, rev.CreatedAt)
	}
}

// TestRevisionJSONOmitsEmptyOptionalFields pins the omitempty contract; the
// frontend distinguishes "no parent/comment" from an empty string.
func TestRevisionJSONOmitsEmptyOptionalFields(t *testing.T) {
	t.Parallel()

	rev := Revision{ID: "r", ProfileID: "p", Source: SourceImport, ConfigJSON: "{}"}
	raw, err := json.Marshal(rev)
	if err != nil {
		t.Fatalf("Marshal() = %v, want nil", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode revision: %v", err)
	}
	for _, key := range []string{"parentRevisionId", "comment"} {
		if _, ok := m[key]; ok {
			t.Errorf("key %q should be omitted when empty: %s", key, raw)
		}
	}
	// Non-omitempty fields must still be present.
	for _, key := range []string{"id", "profileId", "source", "configJson", "structuralValidationStatus"} {
		if _, ok := m[key]; !ok {
			t.Errorf("key %q must always be present: %s", key, raw)
		}
	}
}

func TestProfileJSONOmitsNilLastUsedAt(t *testing.T) {
	t.Parallel()

	p := Profile{ID: "p", Name: "n"}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal() = %v, want nil", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode profile: %v", err)
	}
	if _, ok := m["lastUsedAt"]; ok {
		t.Errorf("lastUsedAt should be omitted when nil: %s", raw)
	}

	used := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)
	p.LastUsedAt = &used
	raw, err = json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal() = %v, want nil", err)
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode profile: %v", err)
	}
	if _, ok := m["lastUsedAt"]; !ok {
		t.Errorf("lastUsedAt should be present once set: %s", raw)
	}
}
