//go:build !windows

package singbox

import (
	"context"
	"testing"
)

// TestForeignIgnoresProcessesWithAnotherName pins the pgrep path: exit code 1
// means "nothing matched", which is an empty result and not a failure. The
// Windows enumeration answers through tasklist and is covered by its own test
// file.
func TestForeignIgnoresProcessesWithAnotherName(t *testing.T) {
	pids, err := pgrepPIDs(context.Background(), "singbox-definitely-not-running-4242")
	if err != nil {
		t.Fatalf("pgrepPIDs() failed: %v", err)
	}
	if len(pids) != 0 {
		t.Errorf("pgrepPIDs() = %v, want no processes", pids)
	}
}
