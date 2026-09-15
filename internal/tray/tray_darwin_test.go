//go:build darwin && cgo

package tray

import "testing"

// TestNewRequiresASelectionCallback: a driver that cannot report a selection
// would show a menu whose items do nothing, so building one is refused instead.
func TestNewRequiresASelectionCallback(t *testing.T) {
	t.Parallel()

	driver, err := New(Options{})
	if err == nil {
		if driver != nil {
			driver.Hide()
		}
		t.Fatal("New(Options{}) = nil error, want a refusal without an OnSelect callback")
	}
}

// TestNewWithCallbackReturnsADriver builds the driver without rendering a model:
// nothing here touches the menu bar, so the test is safe on any session.
func TestNewWithCallbackReturnsADriver(t *testing.T) {
	t.Parallel()

	driver, err := New(Options{OnSelect: func(string) {}})
	if err != nil {
		t.Fatalf("New(Options{OnSelect}) = %v, want a driver", err)
	}
	if driver == nil {
		t.Fatal("New(Options{OnSelect}) = nil driver, want a driver")
	}
}
