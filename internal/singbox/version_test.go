package singbox

import (
	"testing"

	"github.com/larffxx/singboxui/internal/domain/apperr"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		want       Version
		wantString string
		wantStable bool
		wantEmpty  bool
		wantErr    bool
	}{
		{
			name:       "bare version",
			input:      "1.14.0",
			want:       Version{Major: 1, Minor: 14, Patch: 0, Raw: "1.14.0"},
			wantString: "1.14.0",
			wantStable: true,
		},
		{
			name:  "real banner",
			input: "sing-box version 1.14.0\n\nEnvironment: go1.26.7 darwin/arm64\nTags: with_gvisor",
			// The Go toolchain version in the same output must not win.
			want:       Version{Major: 1, Minor: 14, Patch: 0, Raw: "1.14.0"},
			wantString: "1.14.0",
			wantStable: true,
		},
		{
			name:       "prerelease",
			input:      "1.13.0-beta.4",
			want:       Version{Major: 1, Minor: 13, Patch: 0, Pre: "beta.4", Raw: "1.13.0-beta.4"},
			wantString: "1.13.0-beta.4",
		},
		{
			name:       "release candidate in a banner",
			input:      "sing-box version 1.15.0-rc.2",
			want:       Version{Major: 1, Minor: 15, Patch: 0, Pre: "rc.2", Raw: "1.15.0-rc.2"},
			wantString: "1.15.0-rc.2",
		},
		{
			name:       "release tag with a leading v",
			input:      "v1.14.0",
			want:       Version{Major: 1, Minor: 14, Patch: 0, Raw: "1.14.0"},
			wantString: "1.14.0",
			wantStable: true,
		},
		{
			name:       "build metadata is ignored",
			input:      "1.14.0+meta.7",
			want:       Version{Major: 1, Minor: 14, Patch: 0, Raw: "1.14.0"},
			wantString: "1.14.0",
			wantStable: true,
		},
		{
			name:       "surrounding whitespace",
			input:      "  sing-box version 1.9.3  ",
			want:       Version{Major: 1, Minor: 9, Patch: 3, Raw: "1.9.3"},
			wantString: "1.9.3",
			wantStable: true,
		},
		{
			// The distinguishing case for Empty: a parsed 0.0.0 is a version.
			name:       "zero version is parsed, not unknown",
			input:      "0.0.0",
			want:       Version{Raw: "0.0.0"},
			wantString: "0.0.0",
			wantStable: true,
		},
		{
			name:    "only the go toolchain version is present",
			input:   "Environment: go1.26.7 darwin/arm64",
			wantErr: true,
		},
		{
			name:    "junk",
			input:   "not a sing-box binary",
			wantErr: true,
		},
		{
			name:    "empty",
			input:   "",
			wantErr: true,
		},
		{
			name:    "two components only",
			input:   "1.14",
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseVersion(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatalf("ParseVersion(%q) = %+v, want an error", test.input, got)
				}
				if code := apperr.CodeOf(err); code != apperr.CodeBinarySourceInvalid {
					t.Errorf("error code = %s, want %s", code, apperr.CodeBinarySourceInvalid)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseVersion(%q) failed: %v", test.input, err)
			}
			if got != test.want {
				t.Errorf("ParseVersion(%q) = %+v, want %+v", test.input, got, test.want)
			}
			if got.String() != test.wantString {
				t.Errorf("String() = %q, want %q", got.String(), test.wantString)
			}
			if got.Stable() != test.wantStable {
				t.Errorf("Stable() = %v, want %v", got.Stable(), test.wantStable)
			}
			if got.Empty() != test.wantEmpty {
				t.Errorf("Empty() = %v, want %v", got.Empty(), test.wantEmpty)
			}
		})
	}
}

func TestVersionEmptyAndString(t *testing.T) {
	tests := []struct {
		name       string
		version    Version
		wantEmpty  bool
		wantString string
	}{
		{name: "zero value", version: Version{}, wantEmpty: true, wantString: ""},
		{name: "parsed", version: Version{Major: 1, Minor: 14, Raw: "1.14.0"}, wantString: "1.14.0"},
		{name: "prerelease only", version: Version{Pre: "beta.1"}, wantString: "0.0.0-beta.1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.version.Empty(); got != test.wantEmpty {
				t.Errorf("Empty() = %v, want %v", got, test.wantEmpty)
			}
			if got := test.version.String(); got != test.wantString {
				t.Errorf("String() = %q, want %q", got, test.wantString)
			}
		})
	}
}

func TestVersionCompare(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{name: "equal", a: "1.14.0", b: "1.14.0", want: 0},
		{name: "newer patch", a: "1.14.1", b: "1.14.0", want: 1},
		{name: "newer minor beats newer patch", a: "1.15.0", b: "1.14.99", want: 1},
		{name: "newer major", a: "2.0.0", b: "1.99.99", want: 1},
		// The case string comparison gets wrong: "1.13.21" > "1.14.0" as text.
		{name: "backport on an older line is older", a: "1.13.21", b: "1.14.0", want: -1},
		{name: "stable outranks its own prerelease", a: "1.14.0", b: "1.14.0-rc.1", want: 1},
		{name: "prerelease of a newer version still wins", a: "1.15.0-beta.1", b: "1.14.0", want: 1},
		{name: "prerelease ordering", a: "1.15.0-beta.1", b: "1.15.0-beta.2", want: -1},
		{name: "numeric identifier before alphanumeric", a: "1.15.0-1", b: "1.15.0-beta", want: -1},
		{name: "rc after beta", a: "1.15.0-rc.1", b: "1.15.0-beta.9", want: 1},
		{name: "more identifiers is greater", a: "1.15.0-alpha.1.1", b: "1.15.0-alpha.1", want: 1},
		{name: "unknown version sorts first", a: "1.14.0", b: "", want: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			left, err := ParseVersion(test.a)
			if err != nil {
				t.Fatalf("ParseVersion(%q) failed: %v", test.a, err)
			}
			right := Version{}
			if test.b != "" {
				if right, err = ParseVersion(test.b); err != nil {
					t.Fatalf("ParseVersion(%q) failed: %v", test.b, err)
				}
			}
			if got := left.Compare(right); got != test.want {
				t.Errorf("%s.Compare(%s) = %d, want %d", test.a, test.b, got, test.want)
			}
			// Compare must be antisymmetric, which is what makes it usable as
			// an update-decision primitive.
			if got := right.Compare(left); got != -test.want {
				t.Errorf("%s.Compare(%s) = %d, want %d", test.b, test.a, got, -test.want)
			}
		})
	}
}
