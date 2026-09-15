package desktop

import (
	"strings"

	"github.com/larffxx/singboxui/internal/app/binary"
	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/settings"
	"github.com/larffxx/singboxui/internal/singbox"
)

// BinaryAPI is the managed-binary facade bound to the frontend (spec §31).
// Checking for an update and installing one are separate operations (spec §76):
// opening a screen never installs anything.
type BinaryAPI struct{ app *App }

// BinaryStatusPayload describes which sing-box the application would run.
type BinaryStatusPayload struct {
	Status binary.Status `json:"status"`
	Error  *apperr.Error `json:"error,omitempty"`
}

// BinaryCheckPayload reports the result of a stable-release check.
type BinaryCheckPayload struct {
	Check binary.CheckResult `json:"check"`
	Error *apperr.Error      `json:"error,omitempty"`
}

// BinaryInstallPayload reports the result of an install/update.
type BinaryInstallPayload struct {
	Install binary.InstallResult `json:"install"`
	Error   *apperr.Error        `json:"error,omitempty"`
}

// BinaryVersionPayload carries a probed sing-box version.
type BinaryVersionPayload struct {
	Version singbox.Version `json:"version"`
	Error   *apperr.Error   `json:"error,omitempty"`
}

// GetBinaryStatus returns the selected binary source and its state.
func (a *BinaryAPI) GetBinaryStatus() BinaryStatusPayload {
	st, err := a.app.binaries.Status(a.app.callCtx())
	if err != nil {
		return BinaryStatusPayload{Status: st, Error: fail("BinaryAPI.GetBinaryStatus", err)}
	}
	return BinaryStatusPayload{Status: st}
}

// CheckForUpdates queries the stable release channel. It never installs.
func (a *BinaryAPI) CheckForUpdates() BinaryCheckPayload {
	res, err := a.app.binaries.CheckForUpdate(a.app.callCtx())
	if err != nil {
		// A check that fails offline is reported through the payload so the UI
		// can keep the offline state visible (spec §77).
		return BinaryCheckPayload{Check: res, Error: fail("BinaryAPI.CheckForUpdates", err)}
	}
	return BinaryCheckPayload{Check: res}
}

// LastUpdateCheck returns the cached result of the previous check, if any.
func (a *BinaryAPI) LastUpdateCheck() BinaryCheckPayload {
	res, ok := a.app.binaries.LastCheck()
	if !ok {
		return BinaryCheckPayload{}
	}
	return BinaryCheckPayload{Check: res}
}

// InstallStableUpdate downloads, verifies and installs the available stable
// release. It requires an explicit user action (spec §18).
func (a *BinaryAPI) InstallStableUpdate() BinaryInstallPayload {
	if err := a.app.guard("BinaryAPI.InstallStableUpdate"); err != nil {
		return BinaryInstallPayload{Error: err}
	}
	res, err := a.app.binaries.InstallStableUpdate(a.app.callCtx())
	if err != nil {
		return BinaryInstallPayload{Install: res, Error: fail("BinaryAPI.InstallStableUpdate", err)}
	}
	return BinaryInstallPayload{Install: res}
}

// SetBinarySource switches between the managed release and a custom executable.
// The choice is persisted; PATH is never an implicit source (spec §20).
func (a *BinaryAPI) SetBinarySource(source string, customPath string) BinaryStatusPayload {
	parsed := settings.BinarySource(strings.TrimSpace(source))
	if !parsed.Valid() {
		return BinaryStatusPayload{Error: apperr.New(apperr.CodeBinarySourceInvalid, "BinaryAPI.SetBinarySource",
			"unknown binary source %q: expected managed or custom")}
	}
	if parsed == settings.BinaryCustom && strings.TrimSpace(customPath) == "" {
		return BinaryStatusPayload{Error: apperr.New(apperr.CodeBinarySourceInvalid, "BinaryAPI.SetBinarySource",
			"a custom binary source requires an executable path")}
	}
	st, err := a.app.binaries.SetSource(a.app.callCtx(), parsed, customPath)
	if err != nil {
		return BinaryStatusPayload{Status: st, Error: fail("BinaryAPI.SetBinarySource", err)}
	}
	return BinaryStatusPayload{Status: st}
}

// ProbeBinary reports the version of an executable the user selected, without
// persisting anything.
func (a *BinaryAPI) ProbeBinary(path string) BinaryVersionPayload {
	v, err := a.app.binaries.VersionOf(a.app.callCtx(), path)
	if err != nil {
		return BinaryVersionPayload{Error: fail("BinaryAPI.ProbeBinary", err)}
	}
	return BinaryVersionPayload{Version: v}
}
