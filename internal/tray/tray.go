// Package tray owns the application's presence in the system menu bar.
//
// It is deliberately small and platform-shaped: the package knows how to place a
// status item with an icon and a menu, and how to report the identifier of the
// item the user chose. It knows nothing about sing-box, profiles or the runtime —
// the desktop layer renders the application state into a Model and decides what a
// selection means (spec §5, §31).
//
// The menu bar is a macOS surface. AddTray/New report ErrUnsupported everywhere
// else, so the callers treat "this platform has no menu bar" as an ordinary,
// expected answer rather than a failure (docs/architecture/feature-matrix.md
// row 26).
package tray

import "errors"

// ErrUnsupported reports that the platform has no menu bar to put an icon into.
var ErrUnsupported = errors.New("tray: the platform has no menu bar")

// Icon names the visual state of the status item. The names are platform
// neutral: a driver maps them onto whatever the platform draws.
type Icon string

const (
	// IconIdle is shown while nothing is running.
	IconIdle Icon = "idle"
	// IconActive is shown while the managed process is running.
	IconActive Icon = "active"
	// IconBusy is shown during a lifecycle transition.
	IconBusy Icon = "busy"
	// IconFailed is shown after a start or stop failure.
	IconFailed Icon = "failed"
)

// Item is one row of the menu. An item with Separator set draws a divider and
// carries no title or identifier.
type Item struct {
	// ID is reported back through Options.OnSelect. It is the contract between
	// the menu the user sees and the use case a selection runs.
	ID string `json:"id,omitempty"`
	// Title is the visible label. Disabled items are used for plain status text.
	Title string `json:"title,omitempty"`
	// Enabled reports whether the item can be chosen.
	Enabled bool `json:"enabled"`
	// Separator draws a divider instead of a row.
	Separator bool `json:"separator,omitempty"`
}

// Model is the complete state of the status item: its icon, its tooltip and its
// menu. A driver is given a whole model on every update, never a delta.
type Model struct {
	Icon    Icon   `json:"icon"`
	Tooltip string `json:"tooltip"`
	Items   []Item `json:"items"`
}

// Options configure a driver.
type Options struct {
	// OnSelect receives the ID of the chosen menu item. It runs on its own
	// goroutine: the selection arrives on the platform's user-interface thread,
	// which use-case work must never block.
	OnSelect func(id string)
	// OnReady is called once, from the platform's user-interface thread, when the
	// status item is actually in the menu bar. An icon that never appears
	// otherwise leaves no trace in the logs at all — which is how the rewrite
	// lost one unnoticed — so the platform says so out loud.
	OnReady func()
	// OnProblem reports that the platform could not draw the model: a model it
	// refused, or an icon this version of the system does not have. It is
	// diagnostic only and does not change the render that already happened.
	OnProblem func(reason string)
}

// Driver is the menu bar of the platform.
type Driver interface {
	// Render places the status item when the platform does not have one yet and
	// replaces its icon and menu with the model. It is safe to call repeatedly
	// and must not block on user-interface work.
	Render(Model) error
	// Hide removes the status item.
	Hide()
}
