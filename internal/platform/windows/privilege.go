//go:build windows

package windows

import (
	"github.com/larffxx/singboxui/internal/platform/privrun"
)

// NewPrivilegeRunner returns the privilege runner of this platform: it asks
// Windows for administrator rights through ShellExecuteExW when a request sets
// Elevate, and supervises sing-box directly when it does not (spec §27: the
// application itself is never elevated).
func NewPrivilegeRunner(dataDir string) (*privrun.Runner, error) {
	runner, err := privrun.New(dataDir, NewElevator())
	if err != nil {
		return nil, err
	}
	return runner, nil
}
