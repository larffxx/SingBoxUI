// Package settings holds the application's typed settings.
//
// Settings are explicit fields, never an untyped JSON map (spec §49): adding a
// field is a code change, and unknown keys read from disk are dropped instead of
// silently round-tripping through the UI.
package settings

import (
	"encoding/json"
	"fmt"
	"strings"
)

// BinarySource selects where the sing-box executable comes from (spec §20).
type BinarySource string

const (
	// BinaryManaged uses the binary downloaded and verified by SingBoxUI.
	BinaryManaged BinarySource = "managed"
	// BinaryCustom uses an executable the user selected explicitly.
	BinaryCustom BinarySource = "custom"
)

// Valid reports whether the source is known.
func (s BinarySource) Valid() bool { return s == BinaryManaged || s == BinaryCustom }

// Theme is the interface colour scheme.
type Theme string

const (
	ThemeSystem Theme = "system"
	ThemeLight  Theme = "light"
	ThemeDark   Theme = "dark"
)

// Valid reports whether the theme is known.
func (t Theme) Valid() bool { return t == ThemeSystem || t == ThemeLight || t == ThemeDark }

// LogLevel is the application log verbosity.
type LogLevel string

const (
	LogDebug LogLevel = "debug"
	LogInfo  LogLevel = "info"
	LogWarn  LogLevel = "warn"
	LogError LogLevel = "error"
)

// Valid reports whether the level is known.
func (l LogLevel) Valid() bool {
	return l == LogDebug || l == LogInfo || l == LogWarn || l == LogError
}

// Settings is the typed application settings model.
type Settings struct {
	AutoStartApplication bool         `json:"autoStartApplication"`
	AutoConnect          bool         `json:"autoConnect"`
	BinarySource         BinarySource `json:"binarySource"`
	CustomBinaryPath     string       `json:"customBinaryPath"`
	ManagedStableChannel bool         `json:"managedStableChannel"`
	UpdateCheckEnabled   bool         `json:"updateCheckEnabled"`
	Theme                Theme        `json:"theme"`
	LogLevel             LogLevel     `json:"logLevel"`
	LastProfileID        string       `json:"lastProfileId"`
}

// Default returns the settings used on a fresh installation.
func Default() Settings {
	return Settings{
		BinarySource:         BinaryManaged,
		ManagedStableChannel: true,
		UpdateCheckEnabled:   true,
		Theme:                ThemeSystem,
		LogLevel:             LogInfo,
	}
}

// ErrInvalid describes a validation failure of a single field.
type ErrInvalid struct {
	Field   string
	Message string
}

func (e *ErrInvalid) Error() string { return fmt.Sprintf("%s: %s", e.Field, e.Message) }

// Validate checks every field. Unknown enum values are rejected rather than
// coerced, so a corrupted database cannot silently change behaviour.
func (s Settings) Validate() error {
	if !s.BinarySource.Valid() {
		return &ErrInvalid{Field: "binarySource", Message: "must be managed or custom"}
	}
	if s.BinarySource == BinaryCustom && strings.TrimSpace(s.CustomBinaryPath) == "" {
		return &ErrInvalid{Field: "customBinaryPath", Message: "is required when the binary source is custom"}
	}
	if !s.Theme.Valid() {
		return &ErrInvalid{Field: "theme", Message: "must be system, light or dark"}
	}
	if !s.LogLevel.Valid() {
		return &ErrInvalid{Field: "logLevel", Message: "must be debug, info, warn or error"}
	}
	return nil
}

// WithDefaults fills empty enum fields from Default, so a row written by an
// older build still loads.
func (s Settings) WithDefaults() Settings {
	def := Default()
	if s.BinarySource == "" {
		s.BinarySource = def.BinarySource
	}
	if s.Theme == "" {
		s.Theme = def.Theme
	}
	if s.LogLevel == "" {
		s.LogLevel = def.LogLevel
	}
	return s
}

// SettingsVersion is the schema version of the persisted settings blob.
const SettingsVersion = 1

// stored is the on-disk envelope: a version plus the typed payload, so future
// migrations can detect the old shape.
type stored struct {
	Version int      `json:"version"`
	Values  Settings `json:"values"`
}

// Marshal encodes the settings for the settings table.
func Marshal(s Settings) (string, error) {
	out, err := json.Marshal(stored{Version: SettingsVersion, Values: s})
	if err != nil {
		return "", fmt.Errorf("encode settings: %w", err)
	}
	return string(out), nil
}

// Unmarshal decodes the settings blob, tolerating an empty value.
func Unmarshal(raw string) (Settings, error) {
	if strings.TrimSpace(raw) == "" {
		return Default(), nil
	}
	var env stored
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return Settings{}, fmt.Errorf("decode settings: %w", err)
	}
	return env.Values.WithDefaults(), nil
}
