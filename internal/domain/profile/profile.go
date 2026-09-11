// Package profile holds the profile and configuration-revision domain model.
package profile

import "time"

// Source records where a revision came from. Stored as a string in SQLite.
type Source string

const (
	SourceManual      Source = "manual"
	SourceImport      Source = "import"
	SourceShareImport Source = "share-import"
	SourceTemplate    Source = "template"
	SourceRollback    Source = "rollback"
	SourceMigration   Source = "migration"
)

// Valid reports whether the source is one of the known values.
func (s Source) Valid() bool {
	switch s {
	case SourceManual, SourceImport, SourceShareImport, SourceTemplate, SourceRollback, SourceMigration:
		return true
	}
	return false
}

// ValidationStatus describes the outcome of a validation step for a revision.
type ValidationStatus string

const (
	// StatusUnknown means validation never ran.
	StatusUnknown ValidationStatus = "unknown"
	// StatusPassed means the step succeeded.
	StatusPassed ValidationStatus = "passed"
	// StatusFailed means the step failed; the revision is retained for inspection.
	StatusFailed ValidationStatus = "failed"
	// StatusStale means the revision was validated by a different sing-box version.
	StatusStale ValidationStatus = "stale"
)

// Profile is a named sing-box configuration with a pointer to its active revision.
type Profile struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Description      string     `json:"description"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	ActiveRevisionID string     `json:"activeRevisionId"`
	LastUsedAt       *time.Time `json:"lastUsedAt,omitempty"`
}

// Revision is an immutable snapshot of a profile's configuration.
type Revision struct {
	ID                   string           `json:"id"`
	ProfileID            string           `json:"profileId"`
	ParentRevisionID     string           `json:"parentRevisionId,omitempty"`
	CreatedAt            time.Time        `json:"createdAt"`
	Source               Source           `json:"source"`
	ConfigJSON           string           `json:"configJson"`
	StructuralValidation ValidationStatus `json:"structuralValidationStatus"`
	SingBoxValidation    ValidationStatus `json:"singBoxValidationStatus"`
	SingBoxVersion       string           `json:"singBoxVersion"`
	Comment              string           `json:"comment,omitempty"`
}

// IsActive reports whether this revision is the profile's active one.
func (r Revision) IsActive(p Profile) bool { return p.ActiveRevisionID == r.ID }
