//go:build !darwin && !windows

package platform

// New refuses to hand out a platform on an operating system the application does
// not target (spec §1): the machine description that says so is available
// through hostSupport, but there is no half-built platform to go with it.
func New() (Platform, error) {
	return nil, ErrUnsupportedPlatform
}
