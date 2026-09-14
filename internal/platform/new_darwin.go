//go:build darwin

package platform

import "github.com/larffxx/singboxui/internal/platform/darwin"

// A compile-time check that the macOS login item fulfils the platform port.
var _ Autostart = (*darwin.Autostart)(nil)

// A compile-time check that the macOS application catalog fulfils the platform
// port: its methods are declared entirely in terms of the domain value type, so
// no adapter is needed here.
var _ ApplicationCatalog = (*darwin.Applications)(nil)

// New returns the platform of this macOS machine.
//
// internal-contracts.md §2 asks the OS packages to expose
// `New() (platform.Platform, error)`. Spelled out literally that is an import
// cycle - this package has to import them to dispatch to them, so they cannot
// import it - so darwin.New returns its own *System and the file that can see
// both directions turns it into the Platform port. The dispatch, the port and
// everything the application layer uses are exactly as documented.
func New() (Platform, error) {
	mechanisms, err := darwin.New()
	if err != nil {
		return nil, err
	}
	return newSystem(mechanisms.DataDir, mechanisms.Autostart, mechanisms.Applications, mechanisms.PrivilegeRunner), nil
}
