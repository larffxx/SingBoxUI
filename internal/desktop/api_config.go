package desktop

import (
	"os"

	"github.com/larffxx/singboxui/internal/app/config"
	"github.com/larffxx/singboxui/internal/app/profiles"
	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/profile"
)

// ConfigAPI is the configuration facade bound to the frontend (spec §31): the
// draft, validation, revisions, apply/rollback and import of an existing
// sing-box configuration.
type ConfigAPI struct{ app *App }

// DraftPayload carries the draft source of a profile.
type DraftPayload struct {
	Draft config.Draft  `json:"draft"`
	Error *apperr.Error `json:"error,omitempty"`
}

// ValidatePayload carries the full validation result of a candidate
// configuration (structural plus `sing-box check`, spec §37).
type ValidatePayload struct {
	Result config.ValidateResult `json:"result"`
	Error  *apperr.Error         `json:"error,omitempty"`
}

// RevisionPayload carries one revision.
type RevisionPayload struct {
	Revision config.RevisionView `json:"revision"`
	Error    *apperr.Error       `json:"error,omitempty"`
}

// RevisionListPayload carries revisions with their active marker.
type RevisionListPayload struct {
	Revisions []config.RevisionView `json:"revisions"`
	Error     *apperr.Error         `json:"error,omitempty"`
}

// ApplyPayload carries the outcome of a transactional apply (spec §14).
type ApplyPayload struct {
	Result config.ApplyResult `json:"result"`
	Error  *apperr.Error      `json:"error,omitempty"`
}

// DiffPayload carries a JSON diff between two configurations (spec §56).
type DiffPayload struct {
	Diff  config.Diff   `json:"diff"`
	Error *apperr.Error `json:"error,omitempty"`
}

// ActiveConfigPayload carries the materialised active configuration.
type ActiveConfigPayload struct {
	Path       string        `json:"path"`
	ProfileID  string        `json:"profileId"`
	ConfigJSON string        `json:"configJson"`
	Error      *apperr.Error `json:"error,omitempty"`
}

// LegacyPayload reports a configuration left behind by the Java prototype
// (spec §65). Detection never modifies or deletes that file.
type LegacyPayload struct {
	Candidate config.LegacyCandidate `json:"candidate"`
	Error     *apperr.Error          `json:"error,omitempty"`
}

// GetDraft returns the draft source of the active revision of a profile.
func (a *ConfigAPI) GetDraft(profileID string) DraftPayload {
	d, err := a.app.config.Draft(a.app.callCtx(), profileID)
	if err != nil {
		return DraftPayload{Error: fail("ConfigAPI.GetDraft", err)}
	}
	return DraftPayload{Draft: d}
}

// ValidateConfig validates a candidate without persisting anything (spec §68:
// validation never saves data).
func (a *ConfigAPI) ValidateConfig(in config.ValidateInput) ValidatePayload {
	res, err := a.app.config.Validate(a.app.callCtx(), in)
	if err != nil {
		return ValidatePayload{Result: res, Error: fail("ConfigAPI.ValidateConfig", err)}
	}
	return ValidatePayload{Result: res}
}

// SaveRevision stores a new immutable revision (spec §12).
func (a *ConfigAPI) SaveRevision(in config.SaveInput) RevisionPayload {
	rev, err := a.app.config.SaveRevision(a.app.callCtx(), in)
	if err != nil {
		return RevisionPayload{Error: fail("ConfigAPI.SaveRevision", err)}
	}
	return RevisionPayload{Revision: rev}
}

// ApplyRevision applies a revision transactionally and restarts the runtime if
// it is running (spec §14).
func (a *ConfigAPI) ApplyRevision(in config.ApplyInput) ApplyPayload {
	res, err := a.app.config.Apply(a.app.callCtx(), in)
	if err != nil {
		return ApplyPayload{Result: res, Error: fail("ConfigAPI.ApplyRevision", err)}
	}
	return ApplyPayload{Result: res}
}

// RollbackToRevision creates a new revision from historical content. It does
// not apply it: the new revision is returned inactive and the caller applies it
// in a separate step, so history itself is never mutated (spec §55).
func (a *ConfigAPI) RollbackToRevision(in config.ApplyInput) RevisionPayload {
	rev, err := a.app.config.Rollback(a.app.callCtx(), in)
	if err != nil {
		return RevisionPayload{Error: fail("ConfigAPI.RollbackToRevision", err)}
	}
	return RevisionPayload{Revision: rev}
}

// ApplyTemplate applies a starter template to an existing profile as a new
// revision (spec §44).
func (a *ConfigAPI) ApplyTemplate(profileID, templateID string) RevisionPayload {
	rev, err := a.app.config.ApplyTemplate(a.app.callCtx(), profileID, templateID)
	if err != nil {
		return RevisionPayload{Error: fail("ConfigAPI.ApplyTemplate", err)}
	}
	return RevisionPayload{Revision: rev}
}

// GetRevision returns one revision with its content.
func (a *ConfigAPI) GetRevision(profileID, revisionID string) RevisionPayload {
	rev, err := a.app.config.GetRevision(a.app.callCtx(), profileID, revisionID)
	if err != nil {
		return RevisionPayload{Error: fail("ConfigAPI.GetRevision", err)}
	}
	return RevisionPayload{Revision: rev}
}

// ListRevisions returns the history of a profile.
func (a *ConfigAPI) ListRevisions(profileID string, limit int) RevisionListPayload {
	items, err := a.app.config.ListRevisions(a.app.callCtx(), profileID, limit)
	if err != nil {
		return RevisionListPayload{Error: fail("ConfigAPI.ListRevisions", err)}
	}
	return RevisionListPayload{Revisions: items}
}

// CompareWithActive diffs the frontend draft against a stored revision.
func (a *ConfigAPI) CompareWithActive(profileID, revisionID, draftJSON string) DiffPayload {
	ctx := a.app.callCtx()
	rev, err := a.app.config.GetRevision(ctx, profileID, revisionID)
	if err != nil {
		return DiffPayload{Error: fail("ConfigAPI.CompareWithActive", err)}
	}
	diff, err := a.app.config.CompareWithActive(draftJSON, rev)
	if err != nil {
		return DiffPayload{Error: fail("ConfigAPI.CompareWithActive", err)}
	}
	return DiffPayload{Diff: diff}
}

// CompareRevisions diffs two stored revisions of the same profile.
func (a *ConfigAPI) CompareRevisions(profileID, leftRevisionID, rightRevisionID string) DiffPayload {
	ctx := a.app.callCtx()
	left, err := a.app.config.GetRevision(ctx, profileID, leftRevisionID)
	if err != nil {
		return DiffPayload{Error: fail("ConfigAPI.CompareRevisions", err)}
	}
	right, err := a.app.config.GetRevision(ctx, profileID, rightRevisionID)
	if err != nil {
		return DiffPayload{Error: fail("ConfigAPI.CompareRevisions", err)}
	}
	diff, err := a.app.config.CompareRevisions(left, right)
	if err != nil {
		return DiffPayload{Error: fail("ConfigAPI.CompareRevisions", err)}
	}
	return DiffPayload{Diff: diff}
}

// GetActiveConfig returns the configuration file sing-box is reading.
func (a *ConfigAPI) GetActiveConfig() ActiveConfigPayload {
	out := ActiveConfigPayload{
		Path:      a.app.config.ActiveConfigPath(),
		ProfileID: a.app.runtime.ActiveProfileID(),
	}
	raw, err := a.app.config.ActiveConfigJSON()
	if err != nil {
		out.Error = fail("ConfigAPI.GetActiveConfig", err)
		return out
	}
	out.ConfigJSON = raw
	return out
}

// ActiveConfigPath returns the absolute path of the active configuration.
func (a *ConfigAPI) ActiveConfigPath() string { return a.app.config.ActiveConfigPath() }

// DetectLegacyConfig looks for a configuration written by the Java prototype.
func (a *ConfigAPI) DetectLegacyConfig() LegacyPayload {
	wd, err := os.Getwd()
	if err != nil {
		wd = ""
	}
	return LegacyPayload{Candidate: a.app.config.DetectLegacy(wd)}
}

// ImportLegacyConfig imports the detected legacy configuration as a new
// profile; the original file is left untouched (spec §65).
func (a *ConfigAPI) ImportLegacyConfig(name string) ProfilePayload {
	ctx := a.app.callCtx()
	wd, err := os.Getwd()
	if err != nil {
		wd = ""
	}
	candidate := a.app.config.DetectLegacy(wd)
	if !candidate.Found {
		return ProfilePayload{Error: apperr.New(apperr.CodeNotFound, "ConfigAPI.ImportLegacyConfig", "no sing-box configuration from the previous version was found")}
	}
	raw, err := a.app.config.LegacyConfigJSON(candidate)
	if err != nil {
		return ProfilePayload{Error: fail("ConfigAPI.ImportLegacyConfig", err)}
	}
	if name == "" {
		name = "Imported configuration"
	}
	p, err := a.app.profiles.Import(ctx, profiles.ImportInput{
		Name:        name,
		Description: "Imported from " + candidate.ConfigPath,
		ConfigJSON:  raw,
		Source:      profile.SourceMigration,
	})
	if err != nil {
		return ProfilePayload{Error: fail("ConfigAPI.ImportLegacyConfig", err)}
	}
	return ProfilePayload{Profile: p}
}
