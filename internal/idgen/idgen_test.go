package idgen

import (
	"regexp"
	"testing"
)

// uuidV4 matches the RFC 4122 version-4 layout idgen promises to produce.
var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewShape(t *testing.T) {
	t.Parallel()

	got := New()
	if len(got) != 36 {
		t.Fatalf("len(New()) = %d, want 36: %q", len(got), got)
	}
	for _, i := range []int{8, 13, 18, 23} {
		if got[i] != '-' {
			t.Errorf("New() = %q, want a hyphen at index %d", got, i)
		}
	}
	if !uuidV4.MatchString(got) {
		t.Errorf("New() = %q, want a version-4 UUID", got)
	}
}

func TestNewVersionAndVariantBits(t *testing.T) {
	t.Parallel()

	for i := 0; i < 100; i++ {
		got := New()
		if got[14] != '4' {
			t.Fatalf("New() = %q, want the version nibble to be 4", got)
		}
		switch got[19] {
		case '8', '9', 'a', 'b':
		default:
			t.Fatalf("New() = %q, want an RFC 4122 variant nibble (8, 9, a or b)", got)
		}
	}
}

func TestNewIsUnique(t *testing.T) {
	t.Parallel()

	const n = 5000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := New()
		if _, dup := seen[id]; dup {
			t.Fatalf("New() produced a duplicate id %q after %d calls", id, i)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != n {
		t.Fatalf("got %d distinct ids, want %d", len(seen), n)
	}
}

func TestNewIsLowercaseHexOnly(t *testing.T) {
	t.Parallel()

	const hex = "0123456789abcdef-"
	for i := 0; i < 100; i++ {
		id := New()
		for j := 0; j < len(id); j++ {
			if !containsByte(hex, id[j]) {
				t.Fatalf("New() = %q, unexpected byte %q at index %d", id, id[j], j)
			}
		}
	}
}

func containsByte(set string, b byte) bool {
	for i := 0; i < len(set); i++ {
		if set[i] == b {
			return true
		}
	}
	return false
}
