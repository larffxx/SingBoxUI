package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	domainconfig "github.com/larffxx/singboxui/internal/domain/config"
)

// LegacyCandidate describes a sing-box configuration left behind by the Java
// prototype (spec §65). The original file is only ever read.
type LegacyCandidate struct {
	// Found reports whether any legacy configuration was detected.
	Found bool `json:"found"`
	// ConfigPath is the legacy configuration file.
	ConfigPath string `json:"configPath"`
	// SettingsPath is the prototype's ui-settings.json, when present.
	SettingsPath string `json:"settingsPath,omitempty"`
	// BinaryPath is a sing-box executable the prototype downloaded, when present.
	BinaryPath string `json:"binaryPath,omitempty"`
	// AutoConnect mirrors the prototype's autoConnect setting, when known.
	AutoConnect *bool `json:"autoConnect,omitempty"`
	// Valid reports whether the file is parseable sing-box JSON.
	Valid bool `json:"valid"`
	// Size and Modified describe the file, for the import prompt.
	Size     int       `json:"size"`
	Modified time.Time `json:"modified"`
	// Warnings lists non-fatal problems found while inspecting the candidate.
	Warnings []string `json:"warnings,omitempty"`
}

// legacySettings is the shape of the prototype's ui-settings.json.
type legacySettings struct {
	AutoConnect *bool `json:"autoConnect"`
}

// DetectLegacy looks for the prototype's files in the locations it used: the
// working directory first, then the fallback data directory (spec §65, §80).
// workingDir is passed in so the search stays testable and no library code
// depends on the process working directory.
func (s *Service) DetectLegacy(workingDir string) LegacyCandidate {
	for _, dir := range legacyDirs(workingDir) {
		candidate := s.inspectLegacyDir(dir)
		if candidate.Found {
			return candidate
		}
	}
	return LegacyCandidate{}
}

func (s *Service) inspectLegacyDir(dir string) LegacyCandidate {
	configPath := filepath.Join(dir, "config.json")
	info, err := os.Stat(configPath)
	if err != nil || info.IsDir() {
		return LegacyCandidate{}
	}
	candidate := LegacyCandidate{
		Found:      true,
		ConfigPath: configPath,
		Size:       int(info.Size()),
		Modified:   info.ModTime().UTC(),
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		candidate.Warnings = append(candidate.Warnings, "the configuration could not be read: "+err.Error())
		return candidate
	}
	structural := domainconfig.Validate(raw)
	candidate.Valid = structural.OK
	candidate.Warnings = append(candidate.Warnings, structural.Errors...)
	if !structural.OK {
		candidate.Warnings = append(candidate.Warnings, structural.Warnings...)
	}

	settingsPath := filepath.Join(dir, "ui-settings.json")
	if settingsRaw, err := os.ReadFile(settingsPath); err == nil {
		candidate.SettingsPath = settingsPath
		var legacy legacySettings
		if err := json.Unmarshal(settingsRaw, &legacy); err == nil {
			candidate.AutoConnect = legacy.AutoConnect
		} else {
			candidate.Warnings = append(candidate.Warnings, "the prototype settings could not be parsed")
		}
	}

	for _, name := range []string{filepath.Join("bin", "sing-box"), filepath.Join("bin", "sing-box.exe")} {
		binaryPath := filepath.Join(dir, name)
		if stat, err := os.Stat(binaryPath); err == nil && !stat.IsDir() {
			candidate.BinaryPath = binaryPath
			break
		}
	}
	return candidate
}

// legacyDirs returns the directories the prototype used, in priority order.
func legacyDirs(workingDir string) []string {
	dirs := make([]string, 0, 2)
	if workingDir != "" {
		dirs = append(dirs, workingDir)
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".singboxui"))
	}
	return dirs
}

// LegacyConfigJSON reads a detected legacy configuration for import.
func (s *Service) LegacyConfigJSON(candidate LegacyCandidate) (string, error) {
	if !candidate.Found {
		return "", errors.New("config: no legacy configuration was detected")
	}
	raw, err := os.ReadFile(candidate.ConfigPath)
	if err != nil {
		return "", errors.New("config: the legacy configuration could not be read: " + err.Error())
	}
	pretty, err := s.normalize(string(raw))
	if err != nil {
		return "", err
	}
	return pretty, nil
}
