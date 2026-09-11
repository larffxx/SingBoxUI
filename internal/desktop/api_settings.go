package desktop

import (
	"os"

	configsvc "github.com/larffxx/singboxui/internal/app/config"
	"github.com/larffxx/singboxui/internal/app/settings"
	"github.com/larffxx/singboxui/internal/app/share"
	"github.com/larffxx/singboxui/internal/domain/apperr"
)

// SettingsAPI is the settings facade bound to the frontend (spec §31, §49–§51).
type SettingsAPI struct{ app *App }

// SettingsPayload carries the typed settings plus the autostart state.
type SettingsPayload struct {
	State settings.State `json:"state"`
	Error *apperr.Error  `json:"error,omitempty"`
}

// Environment is static information about this installation: it changes only
// with the version, the machine or the directory layout, so the UI can cache it.
type Environment struct {
	Version            string                     `json:"version"`
	OS                 string                     `json:"os"`
	Arch               string                     `json:"arch"`
	Supported          bool                       `json:"supported"`
	UnsupportedReason  string                     `json:"unsupportedReason,omitempty"`
	ExecPath           string                     `json:"execPath"`
	DataDir            string                     `json:"dataDir"`
	ConfigDir          string                     `json:"configDir"`
	ActiveConfigPath   string                     `json:"activeConfigPath"`
	LastGoodConfigPath string                     `json:"lastGoodConfigPath"`
	LogPath            string                     `json:"logPath"`
	BinDir             string                     `json:"binDir"`
	RuntimeDir         string                     `json:"runtimeDir"`
	ShareSchemes       []string                   `json:"shareSchemes"`
	Migration          *configsvc.LegacyCandidate `json:"migration,omitempty"`
}

// EnvironmentPayload carries the installation description.
type EnvironmentPayload struct {
	Environment Environment   `json:"environment"`
	Error       *apperr.Error `json:"error,omitempty"`
}

// GetSettings returns the typed settings and the autostart state.
func (a *SettingsAPI) GetSettings() SettingsPayload {
	st, err := a.app.settings.Get(a.app.callCtx())
	if err != nil {
		return SettingsPayload{State: st, Error: fail("SettingsAPI.GetSettings", err)}
	}
	return SettingsPayload{State: st}
}

// UpdateSettings applies a partial settings change (spec §49).
func (a *SettingsAPI) UpdateSettings(in settings.UpdateInput) SettingsPayload {
	st, err := a.app.settings.Update(a.app.callCtx(), in)
	if err != nil {
		return SettingsPayload{State: st, Error: fail("SettingsAPI.UpdateSettings", err)}
	}
	return SettingsPayload{State: st}
}

// SetAutostart enables or disables starting with the user session (spec §50).
func (a *SettingsAPI) SetAutostart(enabled bool) SettingsPayload {
	st, err := a.app.settings.SetAutostart(a.app.callCtx(), enabled)
	if err != nil {
		return SettingsPayload{State: st, Error: fail("SettingsAPI.SetAutostart", err)}
	}
	return SettingsPayload{State: st}
}

// RemoveLegacyAutostart deletes the autostart entry left by the Java prototype.
func (a *SettingsAPI) RemoveLegacyAutostart() SettingsPayload {
	st, err := a.app.settings.RemoveLegacyAutostart(a.app.callCtx())
	if err != nil {
		return SettingsPayload{State: st, Error: fail("SettingsAPI.RemoveLegacyAutostart", err)}
	}
	return SettingsPayload{State: st}
}

// GetEnvironment describes this installation and the detected legacy files.
func (a *SettingsAPI) GetEnvironment() EnvironmentPayload {
	info := a.app.plat.Info()
	paths := a.app.plat.Paths()
	env := Environment{
		Version:            a.app.deps.Version,
		OS:                 info.OS,
		Arch:               info.Arch,
		Supported:          info.Supported,
		UnsupportedReason:  info.UnsupportedReason,
		ExecPath:           a.app.deps.ExecPath,
		DataDir:            paths.DataDir,
		ConfigDir:          paths.ConfigDir,
		ActiveConfigPath:   paths.ActiveConfigPath,
		LastGoodConfigPath: paths.LastGoodConfigPath,
		LogPath:            paths.LogPath,
		BinDir:             paths.BinDir,
		RuntimeDir:         paths.RuntimeDir,
		ShareSchemes:       share.Supported(),
	}
	wd, err := os.Getwd()
	if err != nil {
		wd = ""
	}
	if candidate := a.app.config.DetectLegacy(wd); candidate.Found {
		env.Migration = &candidate
	}
	return EnvironmentPayload{Environment: env}
}
