//go:build windows

package windows

import "github.com/larffxx/singboxui/internal/domain/applications"

// Applications is the Windows answer to the application catalog.
//
// Windows has no application directory that could be enumerated the way macOS
// enumerates bundles: programs are installed anywhere, and the registry entries
// that survive an installation describe installers rather than the process a
// user launches. Matching by name on Windows therefore stays a routing rule the
// user writes with process_name or process_path (ADR 011), and the catalog says
// so instead of guessing.
type Applications struct{}

// NewApplications returns the Windows catalog, which reports that it cannot
// list anything.
func NewApplications() *Applications { return &Applications{} }

// Supported reports that Windows cannot list installed applications.
func (a *Applications) Supported() bool { return false }

// UnsupportedReason tells the user what to do instead.
func (a *Applications) UnsupportedReason() string {
	return "Windows не предоставляет список установленных программ: укажите process_name или process_path в правиле маршрутизации"
}

// List returns nothing: ask Supported first.
func (a *Applications) List() ([]applications.Application, error) { return nil, nil }
