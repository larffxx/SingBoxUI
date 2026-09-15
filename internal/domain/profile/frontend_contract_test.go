package profile

import (
	"os"
	"regexp"
	"slices"
	"testing"
)

// The UI's list of revision sources. Wails types profile.Source as a plain
// string in TypeScript, so a value the UI hard-codes is invisible to the
// compiler: `source: 'editor'` typed fine, shipped, and every save failed with
// INVALID_ARGUMENT unknown revision source "editor". Comparing the two lists
// here is what makes that mistake impossible to repeat silently.
const frontendSourcesPath = "../../../frontend/src/shared/api/revisionSources.ts"

var (
	// The block holding the list, so a string elsewhere in the file (a label, a
	// comment) cannot satisfy the comparison.
	sourceBlock = regexp.MustCompile(`(?s)REVISION_SOURCES\s*=\s*\[(.*?)\]\s*as const`)
	quotedValue = regexp.MustCompile(`'([a-z-]+)'`)
)

func TestFrontendRevisionSourcesMatchTheEnum(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(frontendSourcesPath)
	if err != nil {
		t.Fatalf("cannot read the UI's revision sources (%s): %v", frontendSourcesPath, err)
	}
	match := sourceBlock.FindStringSubmatch(string(raw))
	if match == nil {
		t.Fatalf("no REVISION_SOURCES list in %s", frontendSourcesPath)
	}

	var got []string
	for _, m := range quotedValue.FindAllStringSubmatch(match[1], -1) {
		got = append(got, m[1])
	}

	want := make([]string, 0, len(allSources))
	for _, src := range AllSources() {
		want = append(want, string(src))
	}

	if !slices.Equal(got, want) {
		t.Errorf("UI sources = %v, want %v (spec §12): a source only one side knows fails every save with INVALID_ARGUMENT", got, want)
	}
}

// TestAllSourcesMirrorsValid keeps the list and the check together: a source
// added to one but not the other would be accepted by the database and refused
// by the service, or the reverse.
func TestAllSourcesMirrorsValid(t *testing.T) {
	t.Parallel()

	for _, src := range AllSources() {
		if !src.Valid() {
			t.Errorf("AllSources() lists %q but Valid() rejects it", src)
		}
	}
	if len(AllSources()) != len(allSources) {
		t.Errorf("AllSources() = %d values, want %d", len(AllSources()), len(allSources))
	}
	if got := AllSources(); &got[0] == &allSources[0] {
		t.Error("AllSources() returns the package's own slice; callers could mutate it")
	}
}
