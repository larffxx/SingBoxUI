//go:build !darwin || !cgo

package tray

// New reports that this platform has no menu bar. Windows and Linux builds — and
// any build without cgo — take this path, so the menu bar is never a build
// dependency of a platform that cannot show it (docs/architecture/
// feature-matrix.md row 26).
func New(Options) (Driver, error) { return nil, ErrUnsupported }
