// Package singbox is the adapter over the official sing-box executable.
//
// It owns everything the application knows about the binary itself: version
// probing, `check` validation, detection of sing-box processes the application
// did not start, release resolution and archive extraction. Keeping all of it
// behind one small API means no other package has to know sing-box's command
// line, its output format or the layout of its release assets (spec §20, §21,
// §24, §67).
package singbox

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/larffxx/singboxui/internal/domain/apperr"
)

// Version is a parsed sing-box release version.
//
// It is a value type, so it can be copied, compared and stored without pointer
// aliasing, and it keeps Raw so a diagnostic can show exactly what the binary
// reported even when the canonical form differs (spec §24).
type Version struct {
	// Major, Minor and Patch are the numeric version components.
	Major, Minor, Patch int
	// Pre is the pre-release suffix ("beta.1", "rc.2"); empty for a stable release.
	Pre string
	// Raw is the version text exactly as it was reported, without a leading
	// "v" tag prefix. It is empty only for the zero Version (see Empty), which
	// is what lets a struct field distinguish "not probed yet" from "0.0.0".
	Raw string
}

// versionToken finds a semantic version inside arbitrary binary output.
//
// The word boundary before the optional "v" is essential: the real banner also
// mentions "go1.26.7", and matching the Go toolchain version instead of the
// release version would silently mis-report which binary is installed.
var versionToken = regexp.MustCompile(`(?i)\bv?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z][0-9A-Za-z.-]*))?`)

// ParseVersion extracts the version reported by a sing-box binary.
//
// It accepts the shapes the binary and the release API actually produce —
// "1.14.0", "sing-box version 1.14.0" and "1.13.0-beta.4" — because the probe
// must never trust a file name or a caller's assumption about the format (spec
// §24). Build metadata ("+…") is ignored, as semantic versioning requires. The
// error is typed so callers can react to "this is not a sing-box binary"
// without matching on message text (spec §33).
func ParseVersion(s string) (Version, error) {
	match := versionToken.FindStringSubmatch(s)
	if match == nil {
		return Version{}, apperr.Newf(apperr.CodeBinarySourceInvalid, "singbox.ParseVersion",
			"no version found in %q", strings.TrimSpace(s))
	}
	major, err := versionComponent(match[1])
	if err != nil {
		return Version{}, err
	}
	minor, err := versionComponent(match[2])
	if err != nil {
		return Version{}, err
	}
	patch, err := versionComponent(match[3])
	if err != nil {
		return Version{}, err
	}
	return Version{
		Major: major,
		Minor: minor,
		Patch: patch,
		Pre:   match[4],
		Raw:   strings.TrimPrefix(match[0], "v"),
	}, nil
}

func versionComponent(raw string) (int, error) {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, apperr.Newf(apperr.CodeBinarySourceInvalid, "singbox.ParseVersion",
			"version component %q is out of range", raw)
	}
	return value, nil
}

// String returns the canonical version, e.g. "1.14.0" or "1.13.0-beta.4".
//
// The zero Version renders as the empty string rather than "0.0.0", so a
// "version unknown" placeholder is never mistaken for a parsed version.
func (v Version) String() string {
	if v.Empty() {
		return ""
	}
	out := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		out += "-" + v.Pre
	}
	return out
}

// Stable reports whether this is a stable release, i.e. has no pre-release
// suffix.
//
// The zero Version counts as stable by this definition; callers that must not
// treat "not probed yet" as a stable release check Empty first. That keeps the
// method total, which is what a comparison helper needs.
func (v Version) Stable() bool { return v.Pre == "" }

// Empty reports whether nothing was parsed: no components and no raw text.
//
// It is the only way to express "unknown version" in a struct that has no
// pointer, and callers use it to decide whether to probe at all.
func (v Version) Empty() bool {
	return v.Major == 0 && v.Minor == 0 && v.Patch == 0 && v.Pre == "" && v.Raw == ""
}

// Compare orders two versions, returning -1, 0 or 1.
//
// It follows semantic-version precedence so "is a newer stable release
// available" cannot be decided by string comparison — "1.13.21" sorts after
// "1.14.0" as text but is older, and a pre-release must never outrank the stable
// release it precedes (spec §17).
func (v Version) Compare(o Version) int {
	if c := compareInt(v.Major, o.Major); c != 0 {
		return c
	}
	if c := compareInt(v.Minor, o.Minor); c != 0 {
		return c
	}
	if c := compareInt(v.Patch, o.Patch); c != 0 {
		return c
	}
	return comparePreRelease(v.Pre, o.Pre)
}

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// comparePreRelease implements semantic-version rule 11: a release without a
// suffix outranks one with a suffix, otherwise dot-separated identifiers are
// compared numerically when both are numeric, numerically before
// alphanumerically, and lexically otherwise.
func comparePreRelease(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	}
	left, right := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(left) && i < len(right); i++ {
		if c := compareIdentifier(left[i], right[i]); c != 0 {
			return c
		}
	}
	return compareInt(len(left), len(right))
}

func compareIdentifier(a, b string) int {
	an, aErr := strconv.Atoi(a)
	bn, bErr := strconv.Atoi(b)
	switch {
	case aErr == nil && bErr == nil:
		return compareInt(an, bn)
	case aErr == nil:
		// Numeric identifiers always have lower precedence than alphanumeric ones.
		return -1
	case bErr == nil:
		return 1
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
