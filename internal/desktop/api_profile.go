package desktop

import (
	"github.com/larffxx/singboxui/internal/app/config"
	"github.com/larffxx/singboxui/internal/app/profiles"
	"github.com/larffxx/singboxui/internal/app/templates"
	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/profile"
)

// ProfileAPI is the profile facade bound to the frontend (spec §31). Every
// method calls one application use case; none of them contain business logic
// (spec §5).
type ProfileAPI struct{ app *App }

// CreateProfileRequest is the frontend shape for creating or importing a
// profile. TemplateID selects a starter; ConfigJSON overrides it with explicit
// content (import, paste).
type CreateProfileRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	TemplateID  string `json:"templateId"`
	ConfigJSON  string `json:"configJson"`
}

// ProfilesPayload carries the full profile list, which is what the UI needs
// after every mutation.
type ProfilesPayload struct {
	Profiles []profile.Profile `json:"profiles"`
	ActiveID string            `json:"activeId"`
	Running  bool              `json:"running"`
	Error    *apperr.Error     `json:"error,omitempty"`
}

// ProfilePayload carries one profile.
type ProfilePayload struct {
	Profile profile.Profile `json:"profile"`
	Error   *apperr.Error   `json:"error,omitempty"`
}

// RevisionsPayload carries the revision history of one profile.
type RevisionsPayload struct {
	ProfileID string                `json:"profileId"`
	Revisions []config.RevisionView `json:"revisions"`
	Error     *apperr.Error         `json:"error,omitempty"`
}

// TemplatesPayload carries the available profile starters (spec §44).
type TemplatesPayload struct {
	Templates []templates.Template `json:"templates"`
	Error     *apperr.Error        `json:"error,omitempty"`
}

// CreateProfileFromShareLinksRequest is the frontend shape for creating a profile out of pasted
// share links (spec §39).
type CreateProfileFromShareLinksRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Links is the pasted text: one link per line, comments and blank lines allowed.
	Links string `json:"links"`
	// Base selects one of ShareLinkBasesPayload.Bases; empty means the first one.
	Base string `json:"base"`
}

// ShareLinkProfilePayload carries the profile that was created from a paste, plus what the
// dialog has to show about it: how many links went in, which protocol led, the name the backend
// derived from them and the lines or parameters it could not use.
type ShareLinkProfilePayload struct {
	Profile       profile.Profile `json:"profile"`
	Kind          string          `json:"kind"`
	Count         int             `json:"count"`
	GeneratedName string          `json:"generatedName"`
	Warnings      []string        `json:"warnings,omitempty"`
	Error         *apperr.Error   `json:"error,omitempty"`
}

// ShareLinkBasesPayload carries the shapes a generated configuration can take. The backend
// declares them so the dialog offers exactly what it can generate (ADR 013).
type ShareLinkBasesPayload struct {
	Bases []profiles.ShareLinkBase `json:"bases"`
	Error *apperr.Error            `json:"error,omitempty"`
}

// ListShareLinkBases returns the shapes the create dialog offers for a share link.
func (a *ProfileAPI) ListShareLinkBases() ShareLinkBasesPayload {
	return ShareLinkBasesPayload{Bases: profiles.ShareLinkBases()}
}

// CreateProfileFromShareLinks creates a profile whose first revision is generated from pasted
// share links (spec §39): the links become the profile's proxies and the chosen base decides
// the inbounds, DNS and routing around them.
func (a *ProfileAPI) CreateProfileFromShareLinks(req CreateProfileFromShareLinksRequest) ShareLinkProfilePayload {
	p, generated, err := a.app.profiles.CreateFromShareLinks(a.app.callCtx(), profiles.ShareLinkInput{
		Name:        req.Name,
		Description: req.Description,
		Links:       req.Links,
		Base:        req.Base,
	})
	if err != nil {
		return ShareLinkProfilePayload{Error: fail("ProfileAPI.CreateProfileFromShareLinks", err)}
	}
	return ShareLinkProfilePayload{
		Profile:       p,
		Kind:          generated.Kind,
		Count:         generated.Count,
		GeneratedName: generated.Name,
		Warnings:      generated.Warnings,
	}
}

// ExportPayload carries an exported configuration.
type ExportPayload struct {
	Export *profiles.ExportResult `json:"export,omitempty"`
	Error  *apperr.Error          `json:"error,omitempty"`
}

const opProfileList = "ProfileAPI.ListProfiles"

// ListProfiles returns every profile plus the active one and the runtime flag.
func (a *ProfileAPI) ListProfiles() ProfilesPayload {
	ctx := a.app.callCtx()
	items, err := a.app.profiles.List(ctx)
	if err != nil {
		return ProfilesPayload{Error: fail(opProfileList, err)}
	}
	return ProfilesPayload{
		Profiles: items,
		ActiveID: a.app.runtime.ActiveProfileID(),
		Running:  a.app.runtime.Running(),
	}
}

// GetProfile returns one profile.
func (a *ProfileAPI) GetProfile(id string) ProfilePayload {
	p, err := a.app.profiles.Get(a.app.callCtx(), id)
	if err != nil {
		return ProfilePayload{Error: fail("ProfileAPI.GetProfile", err)}
	}
	return ProfilePayload{Profile: p}
}

// CreateProfile creates a profile with its first revision.
func (a *ProfileAPI) CreateProfile(req CreateProfileRequest) ProfilePayload {
	p, err := a.app.profiles.Create(a.app.callCtx(), profiles.CreateInput{
		Name:        req.Name,
		Description: req.Description,
		TemplateID:  req.TemplateID,
		ConfigJSON:  req.ConfigJSON,
	})
	if err != nil {
		return ProfilePayload{Error: fail("ProfileAPI.CreateProfile", err)}
	}
	return ProfilePayload{Profile: p}
}

// RenameProfile updates the name and description of a profile.
func (a *ProfileAPI) RenameProfile(id, name, description string) ProfilePayload {
	p, err := a.app.profiles.Rename(a.app.callCtx(), id, name, description)
	if err != nil {
		return ProfilePayload{Error: fail("ProfileAPI.RenameProfile", err)}
	}
	return ProfilePayload{Profile: p}
}

// DuplicateProfile copies a profile including its active revision.
func (a *ProfileAPI) DuplicateProfile(id, name string) ProfilePayload {
	p, err := a.app.profiles.Duplicate(a.app.callCtx(), id, name)
	if err != nil {
		return ProfilePayload{Error: fail("ProfileAPI.DuplicateProfile", err)}
	}
	return ProfilePayload{Profile: p}
}

// DeleteProfile deletes a profile and returns the remaining ones.
func (a *ProfileAPI) DeleteProfile(id string) ProfilesPayload {
	ctx := a.app.callCtx()
	if err := a.app.profiles.Delete(ctx, id); err != nil {
		return ProfilesPayload{Error: fail("ProfileAPI.DeleteProfile", err)}
	}
	items, err := a.app.profiles.List(ctx)
	if err != nil {
		return ProfilesPayload{Error: fail("ProfileAPI.DeleteProfile", err)}
	}
	return ProfilesPayload{
		Profiles: items,
		ActiveID: a.app.runtime.ActiveProfileID(),
		Running:  a.app.runtime.Running(),
	}
}

// SetActiveProfile marks which profile the runtime uses and remembers it for
// auto-connect (spec §51).
func (a *ProfileAPI) SetActiveProfile(id string) ProfilesPayload {
	ctx := a.app.callCtx()
	if err := a.app.profiles.SetActive(ctx, id); err != nil {
		return ProfilesPayload{Error: fail("ProfileAPI.SetActiveProfile", err)}
	}
	items, err := a.app.profiles.List(ctx)
	if err != nil {
		return ProfilesPayload{Error: fail("ProfileAPI.SetActiveProfile", err)}
	}
	return ProfilesPayload{
		Profiles: items,
		ActiveID: a.app.runtime.ActiveProfileID(),
		Running:  a.app.runtime.Running(),
	}
}

// ExportProfile returns a plain, usable config.json (spec §66).
func (a *ProfileAPI) ExportProfile(id string) ExportPayload {
	out, err := a.app.profiles.Export(a.app.callCtx(), id)
	if err != nil {
		return ExportPayload{Error: fail("ProfileAPI.ExportProfile", err)}
	}
	return ExportPayload{Export: &out}
}

// ImportProfile creates a profile from an explicit configuration.
func (a *ProfileAPI) ImportProfile(req CreateProfileRequest) ProfilePayload {
	p, err := a.app.profiles.Import(a.app.callCtx(), profiles.ImportInput{
		Name:        req.Name,
		Description: req.Description,
		ConfigJSON:  req.ConfigJSON,
	})
	if err != nil {
		return ProfilePayload{Error: fail("ProfileAPI.ImportProfile", err)}
	}
	return ProfilePayload{Profile: p}
}

// ListTemplates returns the profile starters.
func (a *ProfileAPI) ListTemplates() TemplatesPayload {
	return TemplatesPayload{Templates: a.app.profiles.Templates()}
}

// ListRevisions returns the revision history of a profile (spec §55).
func (a *ProfileAPI) ListRevisions(profileID string, limit int) RevisionsPayload {
	items, err := a.app.profiles.Revisions(a.app.callCtx(), profileID, limit)
	if err != nil {
		return RevisionsPayload{ProfileID: profileID, Error: fail("ProfileAPI.ListRevisions", err)}
	}
	views := make([]config.RevisionView, 0, len(items))
	active := ""
	if p, err := a.app.profiles.Get(a.app.callCtx(), profileID); err == nil {
		active = p.ActiveRevisionID
	}
	for _, rev := range items {
		views = append(views, config.RevisionView{Revision: rev, Active: rev.ID == active, Size: len(rev.ConfigJSON)})
	}
	return RevisionsPayload{ProfileID: profileID, Revisions: views}
}

// ApplyTemplateToProfile applies a starter template to an existing profile as a
// new revision; history is never destroyed (spec §44).
func (a *ProfileAPI) ApplyTemplateToProfile(profileID, templateID string) ProfilePayload {
	rev, err := a.app.profiles.ApplyTemplate(a.app.callCtx(), profileID, templateID)
	if err != nil {
		return ProfilePayload{Error: fail("ProfileAPI.ApplyTemplateToProfile", err)}
	}
	p, err := a.app.profiles.Get(a.app.callCtx(), profileID)
	if err != nil {
		return ProfilePayload{Profile: profile.Profile{ID: profileID, ActiveRevisionID: rev.ID}}
	}
	return ProfilePayload{Profile: p}
}
