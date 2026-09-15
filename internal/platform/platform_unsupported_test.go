//go:build !darwin && !windows

package platform

import (
	"errors"
	"runtime"
	"testing"
)

// TestNewRefusesAnUntargetedOperatingSystem can only be compiled where the
// unsupported branch exists, which is why it carries the same build constraint.
// It is never run on the hosts this application targets.
func TestNewRefusesAnUntargetedOperatingSystem(t *testing.T) {
	platform, err := New()
	if err == nil {
		t.Fatalf("New() returned %#v on %s, want the unsupported-platform error", platform, runtime.GOOS)
	}
	if !errors.Is(err, ErrUnsupportedPlatform) {
		t.Errorf("New() = %v, want it to wrap ErrUnsupportedPlatform", err)
	}
}
