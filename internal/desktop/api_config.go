package desktop

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/larffxx/singboxui/internal/app/config"
	"github.com/larffxx/singboxui/internal/app/profiles"
	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/profile"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// maxImportBytes caps a file the import is willing to read: a sing-box
// configuration is kilobytes, and no user action may load an arbitrary amount
// of disk into memory.
const maxImportBytes = 8 << 20

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

// ImportConfigFileRequest names the file to import and the profile to create.
// `Name` is optional: the file name is used when it is empty.
type ImportConfigFileRequest struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// PickConfigFilePayload carries the path chosen in the native picker. A
// dismissed dialog is `Canceled`, not an error.
type PickConfigFilePayload struct {
	Path     string        `json:"path"`
	Canceled bool          `json:"canceled"`
	Error    *apperr.Error `json:"error,omitempty"`
}

// PickConfigFile opens the native file picker for a sing-box configuration
// (spec §11). It returns an empty, canceled payload when the user dismissed the
// dialog: closing a picker is not a failure.
func (a *ConfigAPI) PickConfigFile() PickConfigFilePayload {
	const op = "ConfigAPI.PickConfigFile"
	if err := a.app.guard(op); err != nil {
		return PickConfigFilePayload{Error: err}
	}
	path, err := a.pickFile(op, wruntime.OpenDialogOptions{
		Title: "Выберите конфигурацию sing-box",
		Filters: []wruntime.FileFilter{
			{DisplayName: "Конфигурация sing-box (*.json)", Pattern: "*.json"},
			{DisplayName: "Все файлы", Pattern: "*"},
		},
	})
	if err != nil {
		return PickConfigFilePayload{Error: err}
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return PickConfigFilePayload{Canceled: true}
	}
	return PickConfigFilePayload{Path: path}
}

// pickFile opens the native file dialog through the injected picker. The Wails
// dialog panics when its context has no dialog handler attached, so a panic
// becomes an error the user can read instead of a dead window.
func (a *ConfigAPI) pickFile(op string, options wruntime.OpenDialogOptions) (path string, err *apperr.Error) {
	pick := a.app.deps.OpenFileDialog
	if pick == nil {
		return "", apperr.New(apperr.CodeInternal, op, "the file picker is not available in this build")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			path = ""
			err = apperr.Newf(apperr.CodeInternal, op, "the file picker failed: %v", recovered)
		}
	}()
	chosen, pickErr := pick(a.app.runtimeCtx(), options)
	if pickErr != nil {
		return "", apperr.From(op, apperr.CodeInternal, pickErr)
	}
	return chosen, nil
}

// ImportConfigFile creates a profile from a sing-box configuration on disk. The
// file is only ever read: importing never rewrites or deletes what the user
// pointed at (spec §11, §65, §66).
func (a *ConfigAPI) ImportConfigFile(req ImportConfigFileRequest) ProfilePayload {
	const op = "ConfigAPI.ImportConfigFile"
	if err := a.app.guard(op); err != nil {
		return ProfilePayload{Error: err}
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		return ProfilePayload{Error: apperr.New(apperr.CodeInvalidArgument, op, "a configuration file path is required")}
	}
	info, err := os.Stat(path)
	if err != nil {
		return ProfilePayload{Error: apperr.From(op, apperr.CodeNotFound, err)}
	}
	if info.IsDir() {
		return ProfilePayload{Error: apperr.Newf(apperr.CodeInvalidArgument, op, "%s is a directory, not a configuration file", path)}
	}
	if info.Size() > maxImportBytes {
		return ProfilePayload{Error: apperr.Newf(apperr.CodeInvalidArgument, op, "the configuration file is larger than %d bytes", maxImportBytes)}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ProfilePayload{Error: apperr.From(op, apperr.CodeNotFound, err)}
	}
	configJSON := decodeConfigFile(raw)
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = importProfileName(path)
	}
	p, err := a.app.profiles.Import(a.app.callCtx(), profiles.ImportInput{
		Name:        name,
		Description: "Imported from " + path,
		ConfigJSON:  configJSON,
		Source:      profile.SourceImport,
	})
	if err != nil {
		return ProfilePayload{Error: fail(op, err)}
	}
	return ProfilePayload{Profile: p}
}

// decodeConfigFile turns the bytes of a configuration file into the JSON text
// we store. encoding/json accepts neither a UTF-8 BOM nor UTF-16, and Windows
// still produces both (Notepad, PowerShell redirection, some editors), so a file
// the user can read must not fail as "invalid JSON".
func decodeConfigFile(raw []byte) string {
	switch {
	case bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}):
		// UTF-8 BOM: the JSON itself is already UTF-8.
		return string(raw[3:])
	case bytes.HasPrefix(raw, []byte{0xFF, 0xFE}):
		return decodeUTF16(raw[2:], binary.LittleEndian)
	case bytes.HasPrefix(raw, []byte{0xFE, 0xFF}):
		return decodeUTF16(raw[2:], binary.BigEndian)
	}
	return string(raw)
}

// decodeUTF16 decodes BOM-less UTF-16 text in the given byte order.
func decodeUTF16(raw []byte, order binary.ByteOrder) string {
	units := make([]uint16, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		units = append(units, order.Uint16(raw[i:i+2]))
	}
	return string(utf16.Decode(units))
}

// importProfileName derives a profile name from a configuration file path:
// `/etc/sing-box/config.json` becomes `config`.
func importProfileName(path string) string {
	base := filepath.Base(path)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	base = strings.TrimSpace(base)
	if base == "" || base == "." {
		return "Imported configuration"
	}
	return base
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
