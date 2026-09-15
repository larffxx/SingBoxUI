// Package config implements the configuration use cases: drafts, validation,
// immutable revisions, the transactional apply flow and rollback.
//
// It never writes the active configuration except through internal/atomicfile
// (spec §15), and it never marks a revision active before the runtime confirms
// health (spec §14).
package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/larffxx/singboxui/internal/app/runtime"
	"github.com/larffxx/singboxui/internal/app/templates"
	"github.com/larffxx/singboxui/internal/atomicfile"
	"github.com/larffxx/singboxui/internal/domain/apperr"
	domainconfig "github.com/larffxx/singboxui/internal/domain/config"
	"github.com/larffxx/singboxui/internal/domain/profile"
	domruntime "github.com/larffxx/singboxui/internal/domain/runtime"
	"github.com/larffxx/singboxui/internal/events"
	"github.com/larffxx/singboxui/internal/idgen"
	"github.com/larffxx/singboxui/internal/logging"
	"github.com/larffxx/singboxui/internal/platform"
	"github.com/larffxx/singboxui/internal/singbox"
)

// Store is the persistence the config service needs.
type Store interface {
	GetProfile(ctx context.Context, id string) (profile.Profile, error)
	UpdateProfile(ctx context.Context, p profile.Profile) error
	GetRevision(ctx context.Context, profileID, revisionID string) (profile.Revision, error)
	ListRevisions(ctx context.Context, profileID string, limit int) ([]profile.Revision, error)
	ActiveRevision(ctx context.Context, profileID string) (profile.Revision, error)
	CreateRevision(ctx context.Context, rev profile.Revision, makeActive bool) error
	SetActiveRevision(ctx context.Context, profileID, revisionID string) error
	SetRevisionValidation(ctx context.Context, revisionID string, structural, singbox profile.ValidationStatus, version string) error
}

// BinaryPort resolves the sing-box executable; implemented by app/binary.
type BinaryPort interface {
	Path(ctx context.Context) (string, error)
	Version(ctx context.Context) (singbox.Version, error)
}

// RuntimeControl is the runtime supervisor seen from here; implemented by
// app/runtime. Declaring it locally keeps the two packages independent.
type RuntimeControl interface {
	Status() domruntime.Status
	Running() bool
	StartProfile(ctx context.Context, profileID string) error
	Stop(ctx context.Context) error
	Restart(ctx context.Context) error
}

// Deps are the collaborators of the config service.
type Deps struct {
	Store        Store
	Binaries     BinaryPort
	Runtime      RuntimeControl
	Paths        platform.Paths
	Emitter      events.Emitter
	Logger       *slog.Logger
	CheckTimeout time.Duration
	NewID        func() string
	Now          func() time.Time
}

// Service implements the configuration use cases.
type Service struct {
	deps Deps
}

// New builds the config service.
func New(deps Deps) *Service {
	if deps.Emitter == nil {
		deps.Emitter = events.Noop{}
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.CheckTimeout <= 0 {
		deps.CheckTimeout = 20 * time.Second
	}
	if deps.NewID == nil {
		deps.NewID = idgen.New
	}
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{deps: deps}
}

// Draft is the editable working copy of a profile's configuration (spec §13).
// Drafts live in the frontend; this is the base the draft derives from.
type Draft struct {
	ProfileID        string              `json:"profileId"`
	ProfileName      string              `json:"profileName"`
	BaseRevisionID   string              `json:"baseRevisionId"`
	BaseSource       profile.Source      `json:"baseSource"`
	ConfigJSON       string              `json:"configJson"`
	SingBoxVersion   string              `json:"singBoxVersion"`
	Validation       string              `json:"validation"`
	StructuralResult domainconfig.Result `json:"structural"`
}

// ValidateInput is a validation request. Validation never persists anything
// (spec §68).
type ValidateInput struct {
	ProfileID  string `json:"profileId"`
	ConfigJSON string `json:"configJson"`
	// SkipSingBoxCheck limits validation to the structural pass.
	SkipSingBoxCheck bool `json:"skipSingBoxCheck"`
}

// ValidateResult reports both validation layers (spec §57).
type ValidateResult struct {
	Structural domainconfig.Result  `json:"structural"`
	Check      *singbox.CheckResult `json:"check,omitempty"`
	Valid      bool                 `json:"valid"`
	Version    string               `json:"version"`
}

// SaveInput creates a new revision from a validated draft.
type SaveInput struct {
	ProfileID  string         `json:"profileId"`
	ConfigJSON string         `json:"configJson"`
	Comment    string         `json:"comment"`
	Source     profile.Source `json:"source"`
	// Apply marks the new revision active right away; the runtime is restarted
	// through the same transactional path as an explicit apply.
	Apply bool `json:"apply"`
}

// ApplyResult reports the outcome of an apply.
type ApplyResult struct {
	RevisionID   string `json:"revisionId"`
	ProfileID    string `json:"profileId"`
	ActiveConfig string `json:"activeConfigPath"`
	Restarted    bool   `json:"restarted"`
	Warning      string `json:"warning,omitempty"`
}

// RevisionView is a revision plus its active marker, ready for the history UI.
type RevisionView struct {
	profile.Revision
	Active bool `json:"active"`
	// Size is the byte length of the configuration, for the history list.
	Size int `json:"size"`
}

// TemplateInfo is a profile starter (spec §44).
type TemplateInfo = templates.Template

// Draft returns the base revision of a profile.
func (s *Service) Draft(ctx context.Context, profileID string) (Draft, error) {
	p, err := s.deps.Store.GetProfile(ctx, profileID)
	if err != nil {
		return Draft{}, err
	}
	rev, err := s.deps.Store.ActiveRevision(ctx, profileID)
	if err != nil {
		return Draft{}, err
	}
	return Draft{
		ProfileID:        p.ID,
		ProfileName:      p.Name,
		BaseRevisionID:   rev.ID,
		BaseSource:       rev.Source,
		ConfigJSON:       rev.ConfigJSON,
		SingBoxVersion:   rev.SingBoxVersion,
		Validation:       string(rev.SingBoxValidation),
		StructuralResult: domainconfig.Validate([]byte(rev.ConfigJSON)),
	}, nil
}

// ListRevisions returns the history, newest first, with the active marker.
func (s *Service) ListRevisions(ctx context.Context, profileID string, limit int) ([]RevisionView, error) {
	p, err := s.deps.Store.GetProfile(ctx, profileID)
	if err != nil {
		return nil, err
	}
	revisions, err := s.deps.Store.ListRevisions(ctx, profileID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]RevisionView, 0, len(revisions))
	for _, rev := range revisions {
		out = append(out, RevisionView{Revision: rev, Active: rev.ID == p.ActiveRevisionID, Size: len(rev.ConfigJSON)})
	}
	return out, nil
}

// GetRevision returns one revision.
func (s *Service) GetRevision(ctx context.Context, profileID, revisionID string) (RevisionView, error) {
	p, err := s.deps.Store.GetProfile(ctx, profileID)
	if err != nil {
		return RevisionView{}, err
	}
	rev, err := s.deps.Store.GetRevision(ctx, profileID, revisionID)
	if err != nil {
		return RevisionView{}, err
	}
	return RevisionView{Revision: rev, Active: rev.ID == p.ActiveRevisionID, Size: len(rev.ConfigJSON)}, nil
}

// Validate runs the structural validator and, unless skipped, `sing-box check`
// (spec §37). Nothing is written to the database.
func (s *Service) Validate(ctx context.Context, in ValidateInput) (ValidateResult, error) {
	result := ValidateResult{Structural: domainconfig.Validate([]byte(in.ConfigJSON))}
	if !result.Structural.OK {
		result.Valid = false
		return result, apperr.WithDetails(
			apperr.New(apperr.CodeConfigInvalid, "config.Validate", "the configuration is not valid sing-box JSON"),
			result.Structural.Errors...)
	}
	if in.SkipSingBoxCheck {
		result.Valid = true
		return result, nil
	}

	binaryPath, version, err := s.binary(ctx)
	if err != nil {
		// No binary yet: structural validation still answered the question.
		result.Valid = true
		return result, nil
	}
	result.Version = version.String()

	tempPath, cleanup, err := s.stageCandidate(ctx, in.ConfigJSON, "validate")
	if err != nil {
		return result, err
	}
	defer cleanup()

	check, err := singbox.Check(ctx, binaryPath, tempPath, s.deps.CheckTimeout)
	result.Check = &check
	if err != nil {
		result.Valid = false
		if typed, ok := apperr.As(err); ok {
			return result, typed
		}
		return result, apperr.Wrap(apperr.CodeConfigCheckFailed, "config.Validate", "sing-box rejected the configuration", err)
	}
	result.Valid = check.OK
	return result, nil
}

// SaveRevision persists a validated draft as a new immutable revision
// (spec §12). The database is never updated in place.
func (s *Service) SaveRevision(ctx context.Context, in SaveInput) (RevisionView, error) {
	const op = "config.SaveRevision"
	source := in.Source
	if source == "" {
		source = profile.SourceManual
	}
	if !source.Valid() {
		return RevisionView{}, apperr.Newf(apperr.CodeInvalidArgument, op, "unknown revision source %q", source)
	}
	p, err := s.deps.Store.GetProfile(ctx, in.ProfileID)
	if err != nil {
		return RevisionView{}, err
	}
	pretty, err := s.normalize(in.ConfigJSON)
	if err != nil {
		return RevisionView{}, err
	}

	rev := profile.Revision{
		ID:                   s.deps.NewID(),
		ProfileID:            p.ID,
		ParentRevisionID:     p.ActiveRevisionID,
		CreatedAt:            s.deps.Now(),
		Source:               source,
		ConfigJSON:           pretty,
		StructuralValidation: profile.StatusPassed,
		SingBoxValidation:    profile.StatusUnknown,
		Comment:              strings.TrimSpace(in.Comment),
	}

	// Record the sing-box verdict even when it fails: the revision is kept for
	// inspection (spec §14.4) but never applied.
	checkErrors := []string{}
	if binaryPath, version, binErr := s.binary(ctx); binErr == nil {
		rev.SingBoxVersion = version.String()
		if tempPath, cleanup, stageErr := s.stageCandidate(ctx, pretty, "save"); stageErr == nil {
			check, checkErr := singbox.Check(ctx, binaryPath, tempPath, s.deps.CheckTimeout)
			cleanup()
			if checkErr == nil && check.OK {
				rev.SingBoxValidation = profile.StatusPassed
			} else {
				rev.SingBoxValidation = profile.StatusFailed
				checkErrors = append(checkErrors, check.Errors...)
				if checkErr != nil && len(check.Errors) == 0 {
					checkErrors = append(checkErrors, checkErr.Error())
				}
			}
		}
	} else {
		s.deps.Logger.Warn("saving a revision without a sing-box binary", "operation", op, "error", binErr)
	}

	// A revision the validator refused is never made active, even when the caller
	// asked to apply it: activating it would leave the profile pointing at a
	// configuration that cannot start, and the next launch would fail for reasons
	// the revision list no longer shows. The revision is still stored so the
	// editor can show what was refused (spec §14.4, §37).
	activate := in.Apply && rev.SingBoxValidation != profile.StatusFailed
	if err := s.deps.Store.CreateRevision(ctx, rev, activate); err != nil {
		return RevisionView{}, err
	}
	s.deps.Logger.Info("revision saved", "operation", op,
		"profileId", p.ID, "revisionId", rev.ID, "source", string(rev.Source),
		"check", string(rev.SingBoxValidation), "applied", activate)

	view := RevisionView{Revision: rev, Active: activate, Size: len(rev.ConfigJSON)}
	if in.Apply && rev.SingBoxValidation == profile.StatusFailed {
		rejected := apperr.New(apperr.CodeConfigCheckFailed, op,
			"sing-box rejected the configuration, so it was saved but not applied")
		if len(checkErrors) > 0 {
			rejected.Message = "sing-box rejected the configuration, so it was saved but not applied: " +
				strings.TrimSpace(checkErrors[0])
		}
		return view, apperr.WithDetails(rejected, checkErrors...)
	}
	if activate {
		if _, err := s.Apply(ctx, ApplyInput{ProfileID: p.ID, RevisionID: rev.ID}); err != nil {
			return view, err
		}
		view.Active = true
	}
	return view, nil
}

// ApplyInput requests the transactional apply of a revision (spec §14).
type ApplyInput struct {
	ProfileID  string `json:"profileId"`
	RevisionID string `json:"revisionId"`
}

// Apply runs the full apply lifecycle and marks the revision active only after
// the runtime confirms health.
func (s *Service) Apply(ctx context.Context, in ApplyInput) (ApplyResult, error) {
	const op = "config.Apply"
	p, err := s.deps.Store.GetProfile(ctx, in.ProfileID)
	if err != nil {
		return ApplyResult{}, err
	}
	rev, err := s.deps.Store.GetRevision(ctx, p.ID, in.RevisionID)
	if err != nil {
		return ApplyResult{}, err
	}
	previous, previousErr := s.deps.Store.ActiveRevision(ctx, p.ID)

	s.progress("validate", "validating the configuration", 0)
	pretty, err := s.normalize(rev.ConfigJSON)
	if err != nil {
		return ApplyResult{}, err
	}
	structural := domainconfig.Validate([]byte(pretty))
	if !structural.OK {
		return ApplyResult{}, apperr.WithDetails(
			apperr.New(apperr.CodeConfigInvalid, op, "the configuration is not valid sing-box JSON"),
			structural.Errors...)
	}

	binaryPath, version, err := s.binary(ctx)
	if err != nil {
		return ApplyResult{}, err
	}

	s.progress("check", "running sing-box check", 25)
	tempPath, cleanup, err := s.stageCandidate(ctx, pretty, "apply")
	if err != nil {
		return ApplyResult{}, err
	}
	check, checkErr := singbox.Check(ctx, binaryPath, tempPath, s.deps.CheckTimeout)
	cleanup()
	if checkErr != nil || !check.OK {
		_ = s.deps.Store.SetRevisionValidation(ctx, rev.ID, profile.StatusPassed, profile.StatusFailed, version.String())
		return ApplyResult{}, apperr.WithDetails(
			apperr.Wrap(apperr.CodeConfigCheckFailed, op, "sing-box rejected the configuration", checkErr),
			check.Errors...)
	}

	s.progress("materialize", "writing the active configuration", 55)
	if err := s.writeActive(ctx, []byte(pretty)); err != nil {
		return ApplyResult{}, err
	}

	restarted := false
	if s.deps.Runtime.Running() {
		s.progress("restart", "restarting sing-box", 75)
		if err := s.deps.Runtime.Restart(ctx); err != nil {
			s.progress("rollback", "restoring the previous configuration", 90)
			rolledBack := s.rollbackFilesystem(ctx, p.ID, previous)
			detail := "the previous configuration was restored"
			if !rolledBack {
				detail = "the previous configuration could not be restored automatically"
			}
			_ = s.deps.Store.SetRevisionValidation(ctx, rev.ID, profile.StatusPassed, profile.StatusFailed, version.String())
			return ApplyResult{RevisionID: rev.ID, ProfileID: p.ID, Warning: detail}, apperr.WithDetails(
				apperr.Wrap(apperr.CodeConfigApplyFailed, op, "sing-box could not start with the new configuration; "+detail, err),
				apperr.MessageOf(err))
		}
		restarted = true
	}

	s.progress("commit", "committing the revision", 95)
	if err := s.deps.Store.SetActiveRevision(ctx, p.ID, rev.ID); err != nil {
		return ApplyResult{}, err
	}
	if err := s.deps.Store.SetRevisionValidation(ctx, rev.ID, profile.StatusPassed, profile.StatusPassed, version.String()); err != nil {
		s.deps.Logger.Warn("recording the validation result failed", "operation", op, "error", err)
	}
	p.ActiveRevisionID = rev.ID
	p.UpdatedAt = s.deps.Now()
	if err := s.deps.Store.UpdateProfile(ctx, p); err != nil {
		return ApplyResult{}, err
	}
	if previousErr == nil && previous.ID != rev.ID {
		s.keepLastGood(ctx)
	}
	s.progress("done", "configuration applied", 100)
	s.deps.Logger.Info("configuration applied", "operation", op,
		"profileId", p.ID, "revisionId", rev.ID, "restarted", restarted)
	return ApplyResult{RevisionID: rev.ID, ProfileID: p.ID, ActiveConfig: s.deps.Paths.ActiveConfigPath, Restarted: restarted}, nil
}

// rollbackFilesystem restores the previous active revision on disk and tries to
// bring the runtime back with it (spec §14.1–14.2).
func (s *Service) rollbackFilesystem(ctx context.Context, profileID string, previous profile.Revision) bool {
	if previous.ID == "" {
		return false
	}
	if err := s.writeActive(ctx, []byte(previous.ConfigJSON)); err != nil {
		s.deps.Logger.Error("restoring the previous configuration failed", "error", err)
		return false
	}
	if err := s.deps.Runtime.StartProfile(ctx, profileID); err != nil {
		s.deps.Logger.Error("restoring the previous runtime failed", "error", err)
		return false
	}
	return true
}

// Rollback creates a new revision from a historical one; history is immutable
// (spec §55).
func (s *Service) Rollback(ctx context.Context, in ApplyInput) (RevisionView, error) {
	const op = "config.Rollback"
	p, err := s.deps.Store.GetProfile(ctx, in.ProfileID)
	if err != nil {
		return RevisionView{}, err
	}
	old, err := s.deps.Store.GetRevision(ctx, p.ID, in.RevisionID)
	if err != nil {
		return RevisionView{}, err
	}
	newRev := profile.Revision{
		ID:                   s.deps.NewID(),
		ProfileID:            p.ID,
		ParentRevisionID:     p.ActiveRevisionID,
		CreatedAt:            s.deps.Now(),
		Source:               profile.SourceRollback,
		ConfigJSON:           old.ConfigJSON,
		StructuralValidation: old.StructuralValidation,
		SingBoxValidation:    old.SingBoxValidation,
		SingBoxVersion:       old.SingBoxVersion,
		Comment:              fmt.Sprintf("rollback to revision %s", old.ID),
	}
	if err := s.deps.Store.CreateRevision(ctx, newRev, false); err != nil {
		return RevisionView{}, err
	}
	s.deps.Logger.Info("revision rolled back", "operation", op,
		"profileId", p.ID, "from", old.ID, "revisionId", newRev.ID)
	return RevisionView{Revision: newRev, Active: false, Size: len(newRev.ConfigJSON)}, nil
}

// ApplyTemplate replaces a profile's draft content with a profile starter
// (spec §44) and returns the new revision.
func (s *Service) ApplyTemplate(ctx context.Context, profileID, templateID string) (RevisionView, error) {
	template, err := templates.Get(templateID)
	if err != nil {
		return RevisionView{}, err
	}
	return s.SaveRevision(ctx, SaveInput{
		ProfileID:  profileID,
		ConfigJSON: template.Config,
		Comment:    "template: " + template.Name,
		Source:     profile.SourceTemplate,
	})
}

// Templates lists the profile starters.
func (s *Service) Templates() []TemplateInfo { return templates.All() }

// MaterializeActive guarantees that the active configuration file contains the
// given revision and reports what the runtime needs to know about it. It is the
// ConfigPort implementation used by the runtime supervisor.
func (s *Service) MaterializeActive(ctx context.Context, profileID, revisionID string) (runtime.Materialized, error) {
	const op = "config.MaterializeActive"
	rev, err := s.deps.Store.GetRevision(ctx, profileID, revisionID)
	if err != nil {
		return runtime.Materialized{}, err
	}
	pretty, err := s.normalize(rev.ConfigJSON)
	if err != nil {
		return runtime.Materialized{}, err
	}
	if s.activeMatches([]byte(pretty)) {
		return s.materialized([]byte(pretty), rev.ID), nil
	}
	if err := s.writeActive(ctx, []byte(pretty)); err != nil {
		return runtime.Materialized{}, err
	}
	s.deps.Logger.Info("active configuration materialised", "operation", op,
		"profileId", profileID, "revisionId", revisionID, "path", s.deps.Paths.ActiveConfigPath)
	return s.materialized([]byte(pretty), rev.ID), nil
}

func (s *Service) materialized(raw []byte, revisionID string) runtime.Materialized {
	clash := ClashAPIFromConfig(raw)
	return runtime.Materialized{
		Path:              s.deps.Paths.ActiveConfigPath,
		RevisionID:        revisionID,
		RequiresPrivilege: domainconfig.HasTUN(raw),
		ClashAPI: runtime.ClashAPI{
			Enabled: clash.Enabled,
			BaseURL: clash.BaseURL,
			Secret:  clash.Secret,
		},
	}
}

// ActiveConfigPath exposes the materialised configuration location.
func (s *Service) ActiveConfigPath() string { return s.deps.Paths.ActiveConfigPath }

// ActiveConfigJSON returns the materialised configuration, if present.
func (s *Service) ActiveConfigJSON() (string, error) {
	raw, err := os.ReadFile(s.deps.Paths.ActiveConfigPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", apperr.Wrap(apperr.CodeNotFound, "config.ActiveConfigJSON", "no active configuration has been written yet", err)
		}
		return "", apperr.Wrap(apperr.CodeInternal, "config.ActiveConfigJSON", "the active configuration could not be read", err)
	}
	return string(raw), nil
}

// writeActive replaces the active configuration atomically (spec §15).
func (s *Service) writeActive(ctx context.Context, raw []byte) error {
	const op = "config.writeActive"
	if err := ensureDir(filepath.Dir(s.deps.Paths.ActiveConfigPath)); err != nil {
		return err
	}
	if err := atomicfile.Write(s.deps.Paths.ActiveConfigPath, raw, 0o600); err != nil {
		return apperr.Wrap(apperr.CodeInternal, op, "the active configuration could not be written", err)
	}
	return nil
}

// keepLastGood stores the configuration that just started successfully.
func (s *Service) keepLastGood(ctx context.Context) {
	raw, err := os.ReadFile(s.deps.Paths.ActiveConfigPath)
	if err != nil {
		return
	}
	if err := atomicfile.Write(s.deps.Paths.LastGoodConfigPath, raw, 0o600); err != nil {
		s.deps.Logger.Warn("keeping the last-known-good configuration failed", "error", err)
	}
}

func (s *Service) activeMatches(raw []byte) bool {
	current, err := os.ReadFile(s.deps.Paths.ActiveConfigPath)
	if err != nil {
		return false
	}
	return bytes.Equal(current, raw)
}

// stageCandidate writes a candidate configuration next to the active file so
// `sing-box check` sees the same filesystem and the same directory layout.
func (s *Service) stageCandidate(ctx context.Context, raw, purpose string) (string, func(), error) {
	if err := ensureDir(s.deps.Paths.ConfigDir); err != nil {
		return "", func() {}, err
	}
	path := filepath.Join(s.deps.Paths.ConfigDir, fmt.Sprintf(".candidate-%s-%s.json", purpose, s.deps.NewID()))
	if err := atomicfile.WriteFile(path, []byte(raw), 0o600); err != nil {
		return "", func() {}, apperr.Wrap(apperr.CodeInternal, "config.stageCandidate",
			"the candidate configuration could not be written", err)
	}
	cleanup := func() {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.deps.Logger.Debug("removing the candidate configuration failed", "path", path, "error", err)
		}
	}
	return path, cleanup, nil
}

// normalize checks that the text is sing-box JSON and returns it pretty-printed
// with secrets intact (the file must stay usable by sing-box itself).
func (s *Service) normalize(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", apperr.New(apperr.CodeConfigInvalid, "config.normalize", "the configuration is empty")
	}
	pretty, err := domainconfig.Pretty([]byte(raw))
	if err != nil {
		return "", apperr.Wrap(apperr.CodeConfigInvalid, "config.normalize", "the configuration is not valid JSON", err)
	}
	return string(pretty), nil
}

// binary resolves the sing-box executable and version.
func (s *Service) binary(ctx context.Context) (string, singbox.Version, error) {
	path, err := s.deps.Binaries.Path(ctx)
	if err != nil {
		return "", singbox.Version{}, err
	}
	version, err := s.deps.Binaries.Version(ctx)
	if err != nil {
		return "", singbox.Version{}, err
	}
	return path, version, nil
}

func (s *Service) progress(stage, message string, percent int) {
	s.deps.Emitter.Emit(events.ConfigApplyProgress, Progress{
		Stage:    stage,
		Message:  logging.RedactString(message),
		Percent:  percent,
		Occurred: s.deps.Now(),
	})
}

// ensureDir creates the directory that will receive a materialised file.
func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "config.ensureDir", "the configuration directory could not be created", err)
	}
	return nil
}

// Progress is one step of the apply pipeline (spec §14).
type Progress struct {
	Stage    string    `json:"stage"`
	Message  string    `json:"message"`
	Percent  int       `json:"percent"`
	Occurred time.Time `json:"occurredAt"`
}
