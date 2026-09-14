// Package applications describes a program the user can route by name
// (ADR 011).
//
// The description is deliberately flat and made of strings only: the macOS
// package produces it, the platform port carries it to the application layer,
// and the frontend writes a routing rule out of it without deriving anything
// itself.
package applications

// Application is one program a routing rule can select, together with the
// condition that selects it.
type Application struct {
	// Name is the name the user knows the program by.
	Name string `json:"name"`
	// BundleID is the reverse-DNS identifier of the bundle, when it declares
	// one. It is shown for disambiguation and never matched on: sing-box has no
	// bundle match on macOS.
	BundleID string `json:"bundleId"`
	// Path is the resolved bundle path. Resolved matters: sing-box reports the
	// real path of a process, so a rule written against /tmp/... never matches
	// the /private/tmp/... it sees.
	Path string `json:"path"`
	// Executable is the main executable of the bundle; it is what a
	// process_name condition matches.
	Executable string `json:"executable"`
	// ProcessPathRegex is the ready-to-use process_path_regex value that selects
	// every executable inside the bundle, helpers included.
	ProcessPathRegex string `json:"processPathRegex"`
}
