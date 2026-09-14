//go:build windows

package platform

import "github.com/larffxx/singboxui/internal/platform/windows"

// A compile-time check that the Windows login item fulfils the platform port.
var _ Autostart = (*windows.Autostart)(nil)

// A compile-time check that the Windows application catalog fulfils the platform
// port: the programs this machine runs and the programs its Start Menu offers
// (ADR 013).
var _ ApplicationCatalog = (*windows.Applications)(nil)

// New returns the platform of this Windows machine.
//
// See new_darwin.go for why the OS packages return their own *System and the
// conversion into Platform happens here: an OS package that imported this one
// while this one imports it to dispatch would be an import cycle.
func New() (Platform, error) {
	mechanisms, err := windows.New()
	if err != nil {
		return nil, err
	}
	return newSystem(mechanisms.DataDir, mechanisms.Autostart, mechanisms.Applications, mechanisms.PrivilegeRunner), nil
}
