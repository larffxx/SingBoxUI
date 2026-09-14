// Package applications describes a program the user can route by name
// (ADR 011, ADR 013).
//
// The description is deliberately flat and made of strings only: the platform
// packages produce it, the platform port carries it to the application layer,
// and the frontend writes a routing rule out of it without deriving anything
// itself.
package applications

// The routing-rule conditions a catalog can declare. sing-box matches
// process_name against the file name of the process and process_path_regex
// against its full path, so which one identifies a program is a property of the
// operating system, not of the rule: macOS selects a bundle by path, Windows
// selects a file name that survives an update.
const (
	// MatchProcessName selects every process whose executable has this file
	// name, wherever it runs from. The match is case-insensitive on Windows.
	MatchProcessName = "process_name"
	// MatchProcessPathRegex selects every process whose executable path matches
	// this expression.
	MatchProcessPathRegex = "process_path_regex"
)

// Application is one program a routing rule can select, together with the
// condition that selects it.
type Application struct {
	// Name is the name the user knows the program by.
	Name string `json:"name"`
	// BundleID is the reverse-DNS identifier of the bundle, when it declares
	// one. It is shown for disambiguation and never matched on: sing-box has no
	// bundle match on macOS.
	BundleID string `json:"bundleId"`
	// Path is the resolved path of the program: the bundle on macOS, the
	// executable on Windows. Resolved matters: sing-box reports the real path
	// of a process, so a rule written against /tmp/... never matches the
	// /private/tmp/... it sees.
	Path string `json:"path"`
	// Executable is the main executable of the program: what a process_name
	// condition matches, and what the listing shows when it has no better name.
	Executable string `json:"executable"`
	// MatchKey is the field of a route rule this program is selected by, and
	// MatchValue the value for it. The pair is ready to use: the frontend puts
	// it into a rule as it stands. It comes from the operating system because
	// only the OS knows what stays true about a program — on macOS an
	// application is a directory tree, on Windows it is a file whose directory
	// moves with every update.
	MatchKey string `json:"matchKey"`
	// MatchValue is the value for MatchKey.
	MatchValue string `json:"matchValue"`
}
