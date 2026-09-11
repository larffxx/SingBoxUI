// Package settings implements the typed-settings use cases (spec §49–§51):
// reading and updating application settings, session autostart, and the
// auto-connect decision made once at startup.
package settings

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/settings"
	"github.com/larffxx/singboxui/internal/events"
	"github.com/larffxx/singboxui/internal/platform"
)

// ProfileStore is the persistence this service needs; it is declared here so
// tests can use a fake and the composition root can pass *sqlite.Store.
type ProfileStore interface {
	LoadSettings(ctx context.Context) (settings.Settings, error)
	SaveSettings(ctx context.Context, value settings.Settings) error
}

// State is what the frontend sees on the settings screen.
type State struct {
	Values    settings.Settings `json:"values"`
	Autostart AutostartState    `json:"autostart"`
	// DataDir and LogPath are shown so the user can find their files.
	DataDir string `json:"dataDir"`
	LogPath string `json:"logPath"`
	// BinaryPath is the executable currently used for sing-box, if resolvable.
	BinaryPath string `json:"binaryPath"`
}

// AutostartState is the whole autostart surface exposed to the UI (spec §50).
type AutostartState struct {
	Supported   bool   `json:"supported"`
	Enabled     bool   `json:"enabled"`
	LegacyEntry string `json:"legacyEntry,omitempty"`
	Error       string `json:"error,omitempty"`
}

// UpdateInput carries a settings change. Nil fields are left untouched, so the
// UI can save one switch without resending the whole form.
type UpdateInput struct {
	AutoStartApplication *bool                  `json:"autoStartApplication,omitempty"`
	AutoConnect          *bool                  `json:"autoConnect,omitempty"`
	BinarySource         *settings.BinarySource `json:"binarySource,omitempty"`
	CustomBinaryPath     *string                `json:"customBinaryPath,omitempty"`
	ManagedStableChannel *bool                  `json:"managedStableChannel,omitempty"`
	UpdateCheckEnabled   *bool                  `json:"updateCheckEnabled,omitempty"`
	Theme                *settings.Theme        `json:"theme,omitempty"`
	LogLevel             *settings.LogLevel     `json:"logLevel,omitempty"`
	LastProfileID        *string                `json:"lastProfileId,omitempty"`
}

// Connector starts a profile; implemented by the runtime supervisor.
type Connector interface {
	StartProfile(ctx context.Context, profileID string) error
}

// Service is the settings use-case implementation.
type Service struct {
	store         ProfileStore
	autostart     platform.Autostart
	emitter       events.Emitter
	logger        *slog.Logger
	paths         platform.Paths
	execPath      string
	binaryPath    func(ctx context.Context) (string, error)
	profileExists func(ctx context.Context, id string) (bool, error)
}

// Deps are the collaborators of the settings service.
type Deps struct {
	Store      ProfileStore
	Autostart  platform.Autostart
	Emitter    events.Emitter
	Logger     *slog.Logger
	Paths      platform.Paths
	ExecPath   string
	BinaryPath func(ctx context.Context) (string, error)
	// ProfileExists answers whether a profile id is still valid.
	ProfileExists func(ctx context.Context, id string) (bool, error)
}

// New builds the settings service.
func New(deps Deps) *Service {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	emitter := deps.Emitter
	if emitter == nil {
		emitter = events.Noop{}
	}
	return &Service{
		store:         deps.Store,
		autostart:     deps.Autostart,
		emitter:       emitter,
		logger:        logger,
		paths:         deps.Paths,
		execPath:      deps.ExecPath,
		binaryPath:    deps.BinaryPath,
		profileExists: deps.ProfileExists,
	}
}

// Get returns the current settings plus the autostart state.
func (s *Service) Get(ctx context.Context) (State, error) {
	values, err := s.store.LoadSettings(ctx)
	if err != nil {
		return State{}, err
	}
	return s.state(ctx, values), nil
}

// Update applies a partial change and returns the new state.
//
// Ordering is deliberate: the values are persisted first, then the autostart
// flag is handed to the operating system. When the platform refuses, the stored
// flag keeps the requested value and the error is returned alongside it, so the
// UI can show the switch the user asked for together with why it did not take
// effect (spec §50).
func (s *Service) Update(ctx context.Context, in UpdateInput) (State, error) {
	const op = "settings.Update"
	values, err := s.store.LoadSettings(ctx)
	if err != nil {
		return State{}, err
	}
	if in.AutoStartApplication != nil {
		values.AutoStartApplication = *in.AutoStartApplication
	}
	if in.AutoConnect != nil {
		values.AutoConnect = *in.AutoConnect
	}
	if in.BinarySource != nil {
		values.BinarySource = *in.BinarySource
	}
	if in.CustomBinaryPath != nil {
		values.CustomBinaryPath = strings.TrimSpace(*in.CustomBinaryPath)
	}
	if in.ManagedStableChannel != nil {
		values.ManagedStableChannel = *in.ManagedStableChannel
	}
	if in.UpdateCheckEnabled != nil {
		values.UpdateCheckEnabled = *in.UpdateCheckEnabled
	}
	if in.Theme != nil {
		values.Theme = *in.Theme
	}
	if in.LogLevel != nil {
		values.LogLevel = *in.LogLevel
	}
	if in.LastProfileID != nil {
		values.LastProfileID = strings.TrimSpace(*in.LastProfileID)
	}
	if values.LastProfileID != "" && s.profileExists != nil {
		exists, err := s.profileExists(ctx, values.LastProfileID)
		if err != nil {
			return State{}, err
		}
		if !exists {
			values.LastProfileID = ""
		}
	}
	if err := values.Validate(); err != nil {
		return State{}, apperr.Wrap(apperr.CodeInvalidArgument, op, "settings rejected", err)
	}
	if err := s.store.SaveSettings(ctx, values); err != nil {
		return State{}, err
	}

	// The autostart flag is a request to the operating system, not just a stored
	// value: apply it and report what actually happened. Saving first is
	// deliberate -- the other fields the caller sent are still valid, so they are
	// not discarded because the platform refused one of them. When the platform
	// refuses, the stored preference stays as the user set it and the caller gets
	// both the error and the state, so the UI can report "saved, but not applied"
	// instead of claiming success (spec §50).
	if in.AutoStartApplication != nil {
		if err := s.applyAutostart(*in.AutoStartApplication); err != nil {
			return s.state(ctx, values), err
		}
	}
	s.logger.Info("settings updated", "operation", op)
	return s.state(ctx, values), nil
}

// SetAutostart enables or disables session autostart.
func (s *Service) SetAutostart(ctx context.Context, enabled bool) (State, error) {
	return s.Update(ctx, UpdateInput{AutoStartApplication: &enabled})
}

// RemoveLegacyAutostart deletes the entry left behind by the Java prototype.
func (s *Service) RemoveLegacyAutostart(ctx context.Context) (State, error) {
	const op = "settings.RemoveLegacyAutostart"
	if s.autostart == nil {
		return s.Get(ctx)
	}
	if err := s.autostart.RemoveLegacy(); err != nil {
		return State{}, apperr.Wrap(apperr.CodeInternal, op, "could not remove the previous autostart entry", err)
	}
	values, err := s.store.LoadSettings(ctx)
	if err != nil {
		return State{}, err
	}
	return s.state(ctx, values), nil
}

// AutoConnectOnStartup implements spec §51: when enabled, the last active profile
// is started once, and a failure is surfaced instead of being hidden in logs.
func (s *Service) AutoConnectOnStartup(ctx context.Context, connector Connector) error {
	const op = "settings.AutoConnectOnStartup"
	values, err := s.store.LoadSettings(ctx)
	if err != nil || !values.AutoConnect {
		return err
	}
	if values.LastProfileID == "" {
		return nil
	}
	if err := connector.StartProfile(ctx, values.LastProfileID); err != nil {
		code := apperr.CodeOf(err)
		s.logger.Error("auto-connect failed", "operation", op, "profileId", values.LastProfileID, "code", string(code))
		s.emitter.Emit(events.AppNotice, events.Notice{
			Level:      "error",
			Title:      "Auto-connect failed",
			Message:    apperr.MessageOf(err),
			Code:       string(code),
			Operation:  op,
			Details:    apperr.DetailsOf(err),
			Persistent: true,
			OccurredAt: time.Now().UTC().Format(time.RFC3339),
		})
		return nil
	}
	return nil
}

func (s *Service) applyAutostart(enabled bool) error {
	const op = "settings.applyAutostart"
	if s.autostart == nil || !s.autostart.Supported() {
		if enabled {
			return apperr.New(apperr.CodeInvalidArgument, op, "autostart is not supported on this platform")
		}
		return nil
	}
	if enabled {
		if s.execPath == "" {
			return apperr.New(apperr.CodeInternal, op, "cannot determine the application path for autostart")
		}
		if err := s.autostart.Enable(s.execPath); err != nil {
			return apperr.Wrap(apperr.CodeInternal, op, "could not enable autostart", err)
		}
		return nil
	}
	if err := s.autostart.Disable(); err != nil {
		return apperr.Wrap(apperr.CodeInternal, op, "could not disable autostart", err)
	}
	return nil
}

func (s *Service) state(ctx context.Context, values settings.Settings) State {
	out := State{
		Values:  values.WithDefaults(),
		DataDir: s.paths.DataDir,
		LogPath: s.paths.LogPath,
	}
	if s.autostart != nil {
		out.Autostart.Supported = s.autostart.Supported()
		if enabled, err := s.autostart.Enabled(); err != nil {
			out.Autostart.Error = err.Error()
		} else {
			out.Autostart.Enabled = enabled
		}
		out.Autostart.LegacyEntry = s.autostart.LegacyEntry()
	}
	if s.binaryPath != nil {
		if path, err := s.binaryPath(ctx); err == nil {
			out.BinaryPath = path
		}
	}
	return out
}
