// Package profiles implements the profile use cases: create, rename, duplicate,
// import, export, delete and switch the active profile (spec §11).
//
// A profile owns immutable configuration revisions. Every function that changes
// a profile's configuration creates a revision instead of editing one.
package profiles

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/larffxx/singboxui/internal/app/templates"
	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/config"
	"github.com/larffxx/singboxui/internal/domain/profile"
	"github.com/larffxx/singboxui/internal/domain/settings"
	"github.com/larffxx/singboxui/internal/events"
	"github.com/larffxx/singboxui/internal/idgen"
)

// Store is the persistence this service needs.
type Store interface {
	ListProfiles(ctx context.Context) ([]profile.Profile, error)
	GetProfile(ctx context.Context, id string) (profile.Profile, error)
	CreateProfile(ctx context.Context, p profile.Profile, initial profile.Revision) error
	UpdateProfile(ctx context.Context, p profile.Profile) error
	DeleteProfile(ctx context.Context, id string) error
	CreateRevision(ctx context.Context, rev profile.Revision, makeActive bool) error
	ListRevisions(ctx context.Context, profileID string, limit int) ([]profile.Revision, error)
	CountRevisions(ctx context.Context, profileID string) (int, error)
	LoadSettings(ctx context.Context) (settings.Settings, error)
	SaveSettings(ctx context.Context, value settings.Settings) error
}

// RuntimeGuard tells the service which profile the managed runtime is using, so
// the active profile cannot be deleted out from under a running VPN (spec §11).
type RuntimeGuard interface {
	ActiveProfileID() string
	Running() bool
}

// Options are the injectable seams (ids and time) used by tests.
type Options struct {
	NewID func() string
	Now   func() time.Time
}

// Service is the profile use-case implementation.
type Service struct {
	store   Store
	runtime RuntimeGuard
	emitter events.Emitter
	logger  *slog.Logger
	opts    Options
}

// New builds the service. runtime may be nil before the supervisor exists; the
// delete guard then behaves as "nothing is running".
func New(store Store, runtime RuntimeGuard, emitter events.Emitter, logger *slog.Logger, opts Options) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if emitter == nil {
		emitter = events.Noop{}
	}
	return &Service{store: store, runtime: runtime, emitter: emitter, logger: logger, opts: opts}
}

// CreateInput describes a new profile.
type CreateInput struct {
	Name        string
	Description string
	// TemplateID selects the initial configuration; empty means an empty config.
	TemplateID string
	// ConfigJSON overrides the template with an explicit configuration (import).
	ConfigJSON string
	// Source records where the configuration came from; defaults to manual.
	Source profile.Source
}

// List returns every profile.
func (s *Service) List(ctx context.Context) ([]profile.Profile, error) {
	return s.store.ListProfiles(ctx)
}

// Get returns one profile.
func (s *Service) Get(ctx context.Context, id string) (profile.Profile, error) {
	return s.store.GetProfile(ctx, id)
}

// Create builds a profile and its initial revision in one transaction.
func (s *Service) Create(ctx context.Context, in CreateInput) (profile.Profile, error) {
	const op = "profiles.Create"
	name, err := validateName(in.Name)
	if err != nil {
		return profile.Profile{}, err
	}

	source := in.Source
	if source == "" {
		source = profile.SourceManual
	}
	body := strings.TrimSpace(in.ConfigJSON)
	switch {
	case body != "":
		if source == profile.SourceManual {
			source = profile.SourceImport
		}
	case in.TemplateID != "":
		tpl, err := templates.Get(in.TemplateID)
		if err != nil {
			return profile.Profile{}, err
		}
		body = tpl.Config
		source = profile.SourceTemplate
	default:
		tpl := templates.MustGet("empty")
		body = tpl.Config
		source = profile.SourceTemplate
	}

	structural := config.Validate([]byte(body))
	if !structural.OK {
		return profile.Profile{}, apperr.WithDetails(
			apperr.New(apperr.CodeConfigInvalid, op, "the initial configuration is not valid JSON"), structural.Errors...)
	}

	now := s.now()
	p := profile.Profile{
		ID:          s.newID(),
		Name:        name,
		Description: strings.TrimSpace(in.Description),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	rev := profile.Revision{
		ID:                   s.newID(),
		ProfileID:            p.ID,
		CreatedAt:            now,
		Source:               source,
		ConfigJSON:           body,
		StructuralValidation: statusOf(structural.OK),
		SingBoxValidation:    profile.StatusUnknown,
	}
	p.ActiveRevisionID = rev.ID

	if err := s.store.CreateProfile(ctx, p, rev); err != nil {
		return profile.Profile{}, err
	}
	s.logger.Info("profile created", "operation", op, "profileId", p.ID, "source", string(source))
	s.emitter.Emit(events.ProfilesChanged, nil)
	return p, nil
}

// Rename updates the display metadata of a profile.
func (s *Service) Rename(ctx context.Context, id, name, description string) (profile.Profile, error) {
	clean, err := validateName(name)
	if err != nil {
		return profile.Profile{}, err
	}
	current, err := s.store.GetProfile(ctx, id)
	if err != nil {
		return profile.Profile{}, err
	}
	current.Name = clean
	current.Description = strings.TrimSpace(description)
	current.UpdatedAt = s.now()
	if err := s.store.UpdateProfile(ctx, current); err != nil {
		return profile.Profile{}, err
	}
	s.emitter.Emit(events.ProfilesChanged, nil)
	return current, nil
}

// Duplicate copies a profile's active configuration into a new profile.
func (s *Service) Duplicate(ctx context.Context, id, newName string) (profile.Profile, error) {
	source, err := s.store.GetProfile(ctx, id)
	if err != nil {
		return profile.Profile{}, err
	}
	rev, err := s.activeRevision(ctx, source)
	if err != nil {
		return profile.Profile{}, err
	}
	if strings.TrimSpace(newName) == "" {
		newName = source.Name + " copy"
	}
	return s.Create(ctx, CreateInput{
		Name:        newName,
		Description: source.Description,
		ConfigJSON:  rev.ConfigJSON,
		Source:      profile.SourceImport,
	})
}

// Delete removes a profile unless the managed runtime is using it.
func (s *Service) Delete(ctx context.Context, id string) error {
	const op = "profiles.Delete"
	p, err := s.store.GetProfile(ctx, id)
	if err != nil {
		return err
	}
	if s.runtime != nil && s.runtime.Running() && s.runtime.ActiveProfileID() == id {
		return apperr.New(apperr.CodeProfileDeleteBlocked, op,
			"this profile is currently running: stop the VPN or switch to another profile first")
	}
	if err := s.store.DeleteProfile(ctx, id); err != nil {
		return err
	}
	// The last-used pointer must not dangle.
	stored, err := s.store.LoadSettings(ctx)
	if err == nil && stored.LastProfileID == id {
		stored.LastProfileID = ""
		if saveErr := s.store.SaveSettings(ctx, stored); saveErr != nil {
			s.logger.Warn("could not clear last profile id", "operation", op, "error", saveErr)
		}
	}
	s.logger.Info("profile deleted", "operation", op, "profileId", p.ID)
	s.emitter.Emit(events.ProfilesChanged, nil)
	return nil
}

// SetActive remembers which profile the application should connect by default.
// Only one profile is active at a time (spec §11).
func (s *Service) SetActive(ctx context.Context, id string) error {
	if _, err := s.store.GetProfile(ctx, id); err != nil {
		return err
	}
	stored, err := s.store.LoadSettings(ctx)
	if err != nil {
		return err
	}
	stored.LastProfileID = id
	if err := s.store.SaveSettings(ctx, stored); err != nil {
		return err
	}
	s.emitter.Emit(events.ProfilesChanged, nil)
	return nil
}

// ExportResult is a profile rendered as a plain sing-box configuration.
type ExportResult struct {
	// FileName is the name the UI should suggest for the exported document.
	FileName string `json:"fileName"`
	// ConfigJSON is a normal, usable config.json (spec §66).
	ConfigJSON string `json:"configJson"`
	// RevisionID identifies the revision the export came from.
	RevisionID string `json:"revisionId"`
	// SingBoxVersion records which binary validated this content, if known.
	SingBoxVersion string `json:"singBoxVersion,omitempty"`
}

// Export renders the active revision of a profile as sing-box JSON.
func (s *Service) Export(ctx context.Context, id string) (ExportResult, error) {
	p, err := s.store.GetProfile(ctx, id)
	if err != nil {
		return ExportResult{}, err
	}
	rev, err := s.activeRevision(ctx, p)
	if err != nil {
		return ExportResult{}, err
	}
	pretty, err := config.Pretty([]byte(rev.ConfigJSON))
	if err != nil {
		// Stored bytes are returned unchanged rather than failing the export.
		pretty = []byte(rev.ConfigJSON)
	}
	return ExportResult{
		FileName:       exportFileName(p.Name),
		ConfigJSON:     string(pretty),
		RevisionID:     rev.ID,
		SingBoxVersion: rev.SingBoxVersion,
	}, nil
}

// ImportInput describes an imported configuration.
type ImportInput struct {
	Name        string
	Description string
	ConfigJSON  string
	Source      profile.Source
}

// Import creates a profile from an external configuration document. The source
// file is never modified by the caller (spec §65).
func (s *Service) Import(ctx context.Context, in ImportInput) (profile.Profile, error) {
	const op = "profiles.Import"
	source := in.Source
	if source == "" {
		source = profile.SourceImport
	}
	if in.Name == "" {
		in.Name = "Imported profile"
	}
	p, err := s.Create(ctx, CreateInput{
		Name:        in.Name,
		Description: in.Description,
		ConfigJSON:  in.ConfigJSON,
		Source:      source,
	})
	if err != nil {
		// Surface the underlying JSON problem with the failing lines attached.
		if apperr.IsCode(err, apperr.CodeConfigInvalid) {
			return profile.Profile{}, apperr.WithDetails(err.(*apperr.Error),
				"the imported file was not used; the source file is untouched")
		}
		return profile.Profile{}, err
	}
	s.logger.Info("profile imported", "operation", op, "profileId", p.ID)
	return p, nil
}

// CreateFromShareLinks creates a profile whose first revision is generated from pasted share
// links (spec §39). The links are turned into a configuration by GenerateFromShareLinks and the
// profile is then created exactly like an imported one, so the document is validated, the
// revision carries the share-import source, and nothing is created when the links are unusable.
//
// The name the user typed wins; a profile named by the dialog's own label is the fallback.
func (s *Service) CreateFromShareLinks(ctx context.Context, in ShareLinkInput) (profile.Profile, ShareLinkConfig, error) {
	const op = "profiles.CreateFromShareLinks"
	generated, err := GenerateFromShareLinks(in)
	if err != nil {
		return profile.Profile{}, ShareLinkConfig{}, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = generated.Name
	}
	p, err := s.Import(ctx, ImportInput{
		Name:        name,
		Description: in.Description,
		ConfigJSON:  generated.ConfigJSON,
		Source:      profile.SourceShareImport,
	})
	if err != nil {
		return profile.Profile{}, ShareLinkConfig{}, err
	}
	s.logger.Info("profile created from share links", "operation", op,
		"profileId", p.ID, "kind", generated.Kind, "links", generated.Count)
	return p, generated, nil
}

// Templates returns the built-in profile starters.
func (s *Service) Templates() []templates.Template { return templates.All() }

// ApplyTemplate replaces a profile's configuration with a template, creating a
// new revision. History is never rewritten (spec §44).
func (s *Service) ApplyTemplate(ctx context.Context, profileID, templateID string) (profile.Revision, error) {
	const op = "profiles.ApplyTemplate"
	tpl, err := templates.Get(templateID)
	if err != nil {
		return profile.Revision{}, err
	}
	p, err := s.store.GetProfile(ctx, profileID)
	if err != nil {
		return profile.Revision{}, err
	}
	parent, err := s.activeRevision(ctx, p)
	if err != nil {
		return profile.Revision{}, err
	}
	now := s.now()
	rev := profile.Revision{
		ID:                   s.newID(),
		ProfileID:            profileID,
		ParentRevisionID:     parent.ID,
		CreatedAt:            now,
		Source:               profile.SourceTemplate,
		ConfigJSON:           tpl.Config,
		StructuralValidation: statusOf(config.Validate([]byte(tpl.Config)).OK),
		SingBoxValidation:    profile.StatusUnknown,
		Comment:              "template: " + tpl.Name,
	}
	if err := s.store.CreateRevision(ctx, rev, true); err != nil {
		return profile.Revision{}, err
	}
	p.UpdatedAt = now
	if err := s.store.UpdateProfile(ctx, p); err != nil {
		s.logger.Warn("could not touch profile timestamp", "operation", op, "error", err)
	}
	s.emitter.Emit(events.ProfilesChanged, nil)
	return rev, nil
}

// Revisions lists a profile's revision history, newest first (spec §55).
func (s *Service) Revisions(ctx context.Context, profileID string, limit int) ([]profile.Revision, error) {
	if _, err := s.store.GetProfile(ctx, profileID); err != nil {
		return nil, err
	}
	return s.store.ListRevisions(ctx, profileID, limit)
}

func (s *Service) activeRevision(ctx context.Context, p profile.Profile) (profile.Revision, error) {
	const op = "profiles.activeRevision"
	revs, err := s.store.ListRevisions(ctx, p.ID, 1)
	if err != nil {
		return profile.Revision{}, err
	}
	if len(revs) == 0 {
		return profile.Revision{}, apperr.Newf(apperr.CodeRevisionNotFound, op, "profile %q has no revisions", p.Name)
	}
	return revs[0], nil
}

func validateName(name string) (string, error) {
	const op = "profiles.validateName"
	clean := strings.TrimSpace(name)
	if clean == "" {
		return "", apperr.New(apperr.CodeInvalidArgument, op, "a profile name is required")
	}
	if len([]rune(clean)) > 80 {
		return "", apperr.New(apperr.CodeInvalidArgument, op, "the profile name must be at most 80 characters")
	}
	if strings.ContainsAny(clean, "\n\r\t") {
		return "", apperr.New(apperr.CodeInvalidArgument, op, "the profile name must be a single line")
	}
	return clean, nil
}

func exportFileName(name string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		case r == ' ':
			return '-'
		default:
			return -1
		}
	}, name)
	if safe == "" {
		safe = "profile"
	}
	return safe + ".json"
}

func statusOf(ok bool) profile.ValidationStatus {
	if ok {
		return profile.StatusPassed
	}
	return profile.StatusFailed
}

// newID and now are the injectable seams.
func (s *Service) newID() string {
	if s.opts.NewID != nil {
		return s.opts.NewID()
	}
	return idgen.New()
}

func (s *Service) now() time.Time {
	if s.opts.Now != nil {
		return s.opts.Now().UTC()
	}
	return time.Now().UTC()
}
