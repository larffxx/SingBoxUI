// Package profiles tests the profile use cases against a real SQLite store: the
// name rules, the active-profile guard, revision ordering and the export/import
// round trip are all properties of the store and the service together, so they
// are asserted end to end instead of against a mock.
package profiles

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/larffxx/singboxui/internal/app/templates"
	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/profile"
	"github.com/larffxx/singboxui/internal/domain/settings"
	"github.com/larffxx/singboxui/internal/events"
	"github.com/larffxx/singboxui/internal/storage/sqlite"
)

// ------------------------------------------------------------------ fixtures

type recorder struct {
	mu    sync.Mutex
	names []string
}

func (r *recorder) Emit(name string, _ any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.names = append(r.names, name)
}

func (r *recorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.names...)
}

func (r *recorder) count(name string) int {
	total := 0
	for _, got := range r.all() {
		if got == name {
			total++
		}
	}
	return total
}

// fakeGuard is the runtime port: the service must ask it before deleting.
type fakeGuard struct {
	mu      sync.Mutex
	running bool
	active  string
}

func (g *fakeGuard) ActiveProfileID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.active
}

func (g *fakeGuard) Running() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.running
}

func (g *fakeGuard) set(running bool, active string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.running = running
	g.active = active
}

type harness struct {
	svc   *Service
	store *sqlite.Store
	guard *fakeGuard
	em    *recorder

	mu    sync.Mutex
	seq   int
	clock time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.Open(context.Background(), filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	h := &harness{
		store: store,
		guard: &fakeGuard{},
		em:    &recorder{},
		clock: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
	}
	h.svc = New(store, h.guard, h.em, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{
		NewID: func() string {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.seq++
			return fmt.Sprintf("id-%02d", h.seq)
		},
		Now: func() time.Time {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.clock = h.clock.Add(time.Second)
			return h.clock
		},
	})
	return h
}

func (h *harness) create(t *testing.T, name string) profile.Profile {
	t.Helper()
	p, err := h.svc.Create(context.Background(), CreateInput{Name: name})
	if err != nil {
		t.Fatalf("Create(%q) = %v", name, err)
	}
	return p
}

func (h *harness) settings(t *testing.T) settings.Settings {
	t.Helper()
	value, err := h.store.LoadSettings(context.Background())
	if err != nil {
		t.Fatalf("LoadSettings() = %v", err)
	}
	return value
}

func (h *harness) revisions(t *testing.T, id string, limit int) []profile.Revision {
	t.Helper()
	revs, err := h.svc.Revisions(context.Background(), id, limit)
	if err != nil {
		t.Fatalf("Revisions(%q, %d) = %v", id, limit, err)
	}
	return revs
}

func wantCode(t *testing.T, err error, want apperr.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %s", want)
	}
	if got := apperr.CodeOf(err); got != want {
		t.Fatalf("error code = %s (%v), want %s", got, err, want)
	}
}

// isJSONObject is a small, dependency-free structural check used where the test
// only needs to know that a document survived a round trip.
func isJSONObject(s string) bool {
	trimmed := strings.TrimSpace(s)
	return strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")
}

// --------------------------------------------------------------------- tests

func TestCreateSanitisesTheNameAndStoresTheInitialRevision(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	p, err := h.svc.Create(ctx, CreateInput{Name: "  Work VPN \t"})
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}
	if p.Name != "Work VPN" {
		t.Errorf("Name = %q, want the trimmed %q", p.Name, "Work VPN")
	}
	if p.ID == "" || p.ActiveRevisionID == "" {
		t.Errorf("profile = %+v, want an id and an active revision", p)
	}
	if !p.CreatedAt.Equal(p.UpdatedAt) {
		t.Errorf("created %v, updated %v: want the same instant", p.CreatedAt, p.UpdatedAt)
	}

	revs := h.revisions(t, p.ID, 0)
	if len(revs) != 1 {
		t.Fatalf("revisions = %d, want 1", len(revs))
	}
	rev := revs[0]
	if rev.ID != p.ActiveRevisionID {
		t.Errorf("active revision = %q, first revision = %q", p.ActiveRevisionID, rev.ID)
	}
	if rev.ProfileID != p.ID || rev.ParentRevisionID != "" {
		t.Errorf("revision = %+v, want it owned by the profile with no parent", rev)
	}
	if rev.Source != profile.SourceTemplate {
		t.Errorf("source = %q, want the empty template", rev.Source)
	}
	if rev.StructuralValidation != profile.StatusPassed {
		t.Errorf("structural status = %q, want passed", rev.StructuralValidation)
	}
	if rev.SingBoxValidation != profile.StatusUnknown {
		t.Errorf("sing-box status = %q, want unknown (no binary ran)", rev.SingBoxValidation)
	}
	if !isJSONObject(rev.ConfigJSON) {
		t.Errorf("config = %q, want a JSON object", rev.ConfigJSON)
	}

	// The store round-trips the profile unchanged.
	stored, err := h.svc.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if stored != p {
		t.Errorf("stored = %+v, want %+v", stored, p)
	}
	if h.em.count(events.ProfilesChanged) != 1 {
		t.Errorf("ProfilesChanged events = %d, want 1", h.em.count(events.ProfilesChanged))
	}
}

func TestCreateRejectsInvalidNames(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"whitespace only", "   \t "},
		{"embedded newline", "Work\nVPN"},
		{"embedded carriage return", "Work\rVPN"},
		{"81 runes", strings.Repeat("ж", 81)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			_, err := h.svc.Create(context.Background(), CreateInput{Name: tc.input})
			wantCode(t, err, apperr.CodeInvalidArgument)

			list, listErr := h.svc.List(context.Background())
			if listErr != nil {
				t.Fatalf("List() = %v", listErr)
			}
			if len(list) != 0 {
				t.Errorf("profiles = %d, want none stored for a rejected name", len(list))
			}
		})
	}
}

func TestCreateAcceptsAnEightyRuneName(t *testing.T) {
	h := newHarness(t)
	name := strings.Repeat("ж", 80)
	p, err := h.svc.Create(context.Background(), CreateInput{Name: name})
	if err != nil {
		t.Fatalf("Create(80 runes) = %v", err)
	}
	// 80 runes of Cyrillic are 160 bytes: the limit is counted in runes.
	if p.Name != name {
		t.Errorf("Name = %q, want %q", p.Name, name)
	}
}

func TestCreateRejectsAnUnparsableConfiguration(t *testing.T) {
	h := newHarness(t)
	before := h.em.count(events.ProfilesChanged)

	_, err := h.svc.Create(context.Background(), CreateInput{
		Name:       "Broken",
		ConfigJSON: `{"outbounds": [`,
	})
	wantCode(t, err, apperr.CodeConfigInvalid)
	if details := apperr.DetailsOf(err); len(details) == 0 {
		t.Errorf("error %v carries no detail lines, want the failing JSON reported", err)
	}
	list, listErr := h.svc.List(context.Background())
	if listErr != nil {
		t.Fatalf("List() = %v", listErr)
	}
	if len(list) != 0 {
		t.Errorf("profiles = %v, want none stored for an invalid config", list)
	}
	if got := h.em.count(events.ProfilesChanged); got != before {
		t.Errorf("ProfilesChanged events = %d, want %d", got, before)
	}
}

func TestCreateRejectsADuplicateNameRegardlessOfCase(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	first := h.create(t, "Work VPN")

	_, err := h.svc.Create(ctx, CreateInput{Name: "  work vpn  "})
	wantCode(t, err, apperr.CodeProfileNameConflict)

	list, listErr := h.svc.List(ctx)
	if listErr != nil {
		t.Fatalf("List() = %v", listErr)
	}
	if len(list) != 1 || list[0].ID != first.ID {
		t.Errorf("profiles = %+v, want only the first profile", list)
	}
}

func TestGetReportsAMissingProfile(t *testing.T) {
	h := newHarness(t)
	_, err := h.svc.Get(context.Background(), "does-not-exist")
	wantCode(t, err, apperr.CodeProfileNotFound)
}

func TestListReturnsEveryProfile(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	first := h.create(t, "Alpha")
	second := h.create(t, "Beta")

	list, err := h.svc.List(ctx)
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List() = %d profiles, want 2", len(list))
	}
	seen := map[string]bool{}
	for _, p := range list {
		seen[p.ID] = true
	}
	if !seen[first.ID] || !seen[second.ID] {
		t.Errorf("List() returned %+v, want both profiles", list)
	}
}

func TestRenameUpdatesMetadataOnly(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	p := h.create(t, "Old name")
	revsBefore := h.revisions(t, p.ID, 0)

	renamed, err := h.svc.Rename(ctx, p.ID, "  New name  ", "  for the office  ")
	if err != nil {
		t.Fatalf("Rename() = %v", err)
	}
	if renamed.Name != "New name" || renamed.Description != "for the office" {
		t.Errorf("renamed = %+v, want the trimmed values", renamed)
	}
	if renamed.ID != p.ID || renamed.ActiveRevisionID != p.ActiveRevisionID {
		t.Errorf("rename changed identity: %+v", renamed)
	}
	if !renamed.UpdatedAt.After(p.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want it after %v", renamed.UpdatedAt, p.UpdatedAt)
	}

	// Renaming must not touch the configuration history.
	revsAfter := h.revisions(t, p.ID, 0)
	if len(revsAfter) != len(revsBefore) || revsAfter[0] != revsBefore[0] {
		t.Errorf("revisions changed by rename: %+v -> %+v", revsBefore, revsAfter)
	}
	if got := h.em.count(events.ProfilesChanged); got != 2 {
		t.Errorf("ProfilesChanged events = %d, want 2 (create + rename)", got)
	}
}

func TestRenameRejectsAnEmptyOrTakenName(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	first := h.create(t, "First")
	h.create(t, "Second")

	_, err := h.svc.Rename(ctx, first.ID, "   ", "")
	wantCode(t, err, apperr.CodeInvalidArgument)

	_, err = h.svc.Rename(ctx, first.ID, "second", "")
	wantCode(t, err, apperr.CodeProfileNameConflict)

	stored, getErr := h.svc.Get(ctx, first.ID)
	if getErr != nil {
		t.Fatalf("Get() = %v", getErr)
	}
	if stored.Name != "First" {
		t.Errorf("Name = %q, want the rejected rename to leave it alone", stored.Name)
	}

	_, err = h.svc.Rename(ctx, "missing", "Whatever", "")
	wantCode(t, err, apperr.CodeProfileNotFound)
}

func TestDuplicateCopiesTheActiveConfiguration(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	source := h.create(t, "Source")
	revs := h.revisions(t, source.ID, 1)
	if len(revs) != 1 {
		t.Fatalf("revisions = %d, want 1", len(revs))
	}

	copied, err := h.svc.Duplicate(ctx, source.ID, "")
	if err != nil {
		t.Fatalf("Duplicate() = %v", err)
	}
	if copied.Name != "Source copy" {
		t.Errorf("Name = %q, want the derived default", copied.Name)
	}
	if copied.ID == source.ID {
		t.Error("the copy reused the source id")
	}
	copiedRevs := h.revisions(t, copied.ID, 0)
	if len(copiedRevs) != 1 {
		t.Fatalf("copy revisions = %d, want 1", len(copiedRevs))
	}
	// Create normalises the body (trailing whitespace is trimmed), so the copy
	// carries the same document, not necessarily the same bytes.
	if copiedRevs[0].ConfigJSON != strings.TrimSpace(revs[0].ConfigJSON) {
		t.Errorf("copy config = %q, want %q", copiedRevs[0].ConfigJSON, strings.TrimSpace(revs[0].ConfigJSON))
	}
	if copiedRevs[0].ID == revs[0].ID {
		t.Error("the copy reused the source revision id")
	}
	if copiedRevs[0].Source != profile.SourceImport {
		t.Errorf("copy source = %q, want import", copiedRevs[0].Source)
	}
	if copied.Description != source.Description {
		t.Errorf("copy description = %q, want %q", copied.Description, source.Description)
	}

	// An explicit name wins, and the default name is taken once copied.
	explicit, err := h.svc.Duplicate(ctx, source.ID, "Source copy")
	wantCode(t, err, apperr.CodeProfileNameConflict)
	if explicit.ID != "" {
		t.Errorf("failed duplicate returned %+v, want the zero profile", explicit)
	}
	named, err := h.svc.Duplicate(ctx, source.ID, "Source (2)")
	if err != nil {
		t.Fatalf("Duplicate(explicit) = %v", err)
	}
	if named.Name != "Source (2)" {
		t.Errorf("Name = %q, want the explicit name", named.Name)
	}
}

func TestDuplicateOfAMissingProfileFails(t *testing.T) {
	h := newHarness(t)
	_, err := h.svc.Duplicate(context.Background(), "missing", "Copy")
	wantCode(t, err, apperr.CodeProfileNotFound)
}

func TestDeleteIsBlockedWhileThatProfileRuns(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	running := h.create(t, "Running")
	idle := h.create(t, "Idle")
	h.guard.set(true, running.ID)

	err := h.svc.Delete(ctx, running.ID)
	wantCode(t, err, apperr.CodeProfileDeleteBlocked)
	if _, getErr := h.svc.Get(ctx, running.ID); getErr != nil {
		t.Errorf("the blocked profile disappeared: %v", getErr)
	}

	// Another profile may be deleted while the VPN runs on a different one.
	if err := h.svc.Delete(ctx, idle.ID); err != nil {
		t.Errorf("Delete(idle) = %v, want it to succeed", err)
	}

	// Stopping the runtime releases the profile.
	h.guard.set(false, running.ID)
	if err := h.svc.Delete(ctx, running.ID); err != nil {
		t.Errorf("Delete(after stop) = %v, want it to succeed", err)
	}
	if _, getErr := h.svc.Get(ctx, running.ID); !apperr.IsCode(getErr, apperr.CodeProfileNotFound) {
		t.Errorf("Get(deleted) = %v, want PROFILE_NOT_FOUND", getErr)
	}
}

func TestDeleteRemovesTheRevisionsAndClearsTheActivePointer(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	p := h.create(t, "Doomed")
	if err := h.svc.SetActive(ctx, p.ID); err != nil {
		t.Fatalf("SetActive() = %v", err)
	}
	if got := h.settings(t).LastProfileID; got != p.ID {
		t.Fatalf("LastProfileID = %q, want %q", got, p.ID)
	}

	if err := h.svc.Delete(ctx, p.ID); err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	if got := h.settings(t).LastProfileID; got != "" {
		t.Errorf("LastProfileID = %q, want it cleared so the pointer cannot dangle", got)
	}
	count, err := h.store.CountRevisions(ctx, p.ID)
	if err != nil {
		t.Fatalf("CountRevisions() = %v", err)
	}
	if count != 0 {
		t.Errorf("revisions left behind = %d, want 0 (cascade)", count)
	}
}

func TestDeleteOfAMissingProfileReportsNotFound(t *testing.T) {
	h := newHarness(t)
	wantCode(t, h.svc.Delete(context.Background(), "missing"), apperr.CodeProfileNotFound)
}

func TestSetActivePersistsTheLastProfile(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	first := h.create(t, "First")
	second := h.create(t, "Second")

	if got := h.settings(t).LastProfileID; got != "" {
		t.Errorf("LastProfileID = %q, want empty before any activation", got)
	}
	if err := h.svc.SetActive(ctx, first.ID); err != nil {
		t.Fatalf("SetActive() = %v", err)
	}
	if got := h.settings(t).LastProfileID; got != first.ID {
		t.Errorf("LastProfileID = %q, want %q", got, first.ID)
	}
	// Activation replaces the previous choice: only one profile is active.
	if err := h.svc.SetActive(ctx, second.ID); err != nil {
		t.Fatalf("SetActive() = %v", err)
	}
	if got := h.settings(t).LastProfileID; got != second.ID {
		t.Errorf("LastProfileID = %q, want %q", got, second.ID)
	}
	if got := h.em.count(events.ProfilesChanged); got != 4 {
		t.Errorf("ProfilesChanged events = %d, want 4 (2 creates + 2 activations)", got)
	}

	wantCode(t, h.svc.SetActive(ctx, "missing"), apperr.CodeProfileNotFound)
	if got := h.settings(t).LastProfileID; got != second.ID {
		t.Errorf("LastProfileID = %q, want it unchanged after the rejected call", got)
	}
}

func TestRevisionsAreListedNewestFirstAndHonourTheLimit(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	p := h.create(t, "History")
	initial := h.revisions(t, p.ID, 0)[0]

	var created []profile.Revision
	for _, id := range []string{"tun-basic", "empty"} {
		rev, err := h.svc.ApplyTemplate(ctx, p.ID, id)
		if err != nil {
			t.Fatalf("ApplyTemplate(%q) = %v", id, err)
		}
		created = append(created, rev)
	}

	all := h.revisions(t, p.ID, 0)
	if len(all) != 3 {
		t.Fatalf("revisions = %d, want 3", len(all))
	}
	if all[0].ID != created[1].ID || all[1].ID != created[0].ID || all[2].ID != initial.ID {
		t.Errorf("order = %q,%q,%q; want newest first", all[0].ID, all[1].ID, all[2].ID)
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].CreatedAt.Before(all[i].CreatedAt) {
			t.Errorf("revision %d is older than %d", i-1, i)
		}
	}

	one := h.revisions(t, p.ID, 1)
	if len(one) != 1 || one[0].ID != created[1].ID {
		t.Errorf("limit 1 = %+v, want the newest revision only", one)
	}
	two := h.revisions(t, p.ID, 2)
	if len(two) != 2 || two[0].ID != created[1].ID || two[1].ID != created[0].ID {
		t.Errorf("limit 2 = %+v, want the two newest revisions", two)
	}
	// A limit larger than the history returns the history.
	if got := h.revisions(t, p.ID, 99); len(got) != 3 {
		t.Errorf("limit 99 returned %d revisions, want 3", len(got))
	}

	_, err := h.svc.Revisions(ctx, "missing", 0)
	wantCode(t, err, apperr.CodeProfileNotFound)
}

func TestApplyTemplateCreatesAChildRevisionAndKeepsTheParent(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	p := h.create(t, "Templated")
	initial := h.revisions(t, p.ID, 0)[0]

	rev, err := h.svc.ApplyTemplate(ctx, p.ID, "tun-basic")
	if err != nil {
		t.Fatalf("ApplyTemplate() = %v", err)
	}
	if rev.ParentRevisionID != initial.ID {
		t.Errorf("ParentRevisionID = %q, want %q", rev.ParentRevisionID, initial.ID)
	}
	if rev.Source != profile.SourceTemplate {
		t.Errorf("source = %q, want template", rev.Source)
	}
	if !strings.Contains(rev.Comment, "TUN basic") {
		t.Errorf("comment = %q, want it to name the template", rev.Comment)
	}
	// The parent revision is immutable history and must still be readable.
	stored, err := h.store.GetRevision(ctx, p.ID, initial.ID)
	if err != nil {
		t.Fatalf("GetRevision(parent) = %v", err)
	}
	if stored.ConfigJSON != initial.ConfigJSON {
		t.Errorf("parent config changed: %q -> %q", initial.ConfigJSON, stored.ConfigJSON)
	}

	updated, err := h.svc.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if updated.ActiveRevisionID != rev.ID {
		t.Errorf("ActiveRevisionID = %q, want the new revision %q", updated.ActiveRevisionID, rev.ID)
	}
	if !updated.UpdatedAt.After(p.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want it after %v", updated.UpdatedAt, p.UpdatedAt)
	}

	_, err = h.svc.ApplyTemplate(ctx, p.ID, "no-such-template")
	if err == nil || apperr.CodeOf(err) == "" {
		t.Fatalf("ApplyTemplate(unknown) = %v, want a typed error", err)
	}
	if got := len(h.revisions(t, p.ID, 0)); got != 2 {
		t.Errorf("revisions = %d, want the failed apply to add nothing", got)
	}
}

func TestTemplatesExposeThePrivilegeRequirement(t *testing.T) {
	h := newHarness(t)
	all := h.svc.Templates()
	if len(all) == 0 {
		t.Fatal("Templates() is empty")
	}
	ids := make([]string, 0, len(all))
	privileged := map[string]bool{}
	for _, tpl := range all {
		ids = append(ids, tpl.ID)
		privileged[tpl.ID] = tpl.RequiresPrivilege
		if !isJSONObject(tpl.Config) {
			t.Errorf("template %q carries no JSON object", tpl.ID)
		}
	}
	if !privileged["tun-basic"] {
		t.Errorf("templates = %v, want tun-basic to require administrator rights", ids)
	}
	if privileged["empty"] {
		t.Errorf("templates = %v, want the empty template to need no privilege", ids)
	}
	if _, err := templates.Get("empty"); err != nil {
		t.Errorf("templates.Get(empty) = %v", err)
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	source, err := h.svc.Create(ctx, CreateInput{
		Name:        "Office VPN",
		Description: "kept across the trip",
		ConfigJSON:  `{"log":{"level":"info"},"outbounds":[{"type":"direct","tag":"direct"}]}`,
	})
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}
	active := h.revisions(t, source.ID, 1)[0]

	exported, err := h.svc.Export(ctx, source.ID)
	if err != nil {
		t.Fatalf("Export() = %v", err)
	}
	if exported.FileName != "Office-VPN.json" {
		t.Errorf("FileName = %q, want %q", exported.FileName, "Office-VPN.json")
	}
	if exported.RevisionID != active.ID {
		t.Errorf("RevisionID = %q, want the active revision %q", exported.RevisionID, active.ID)
	}
	if !isJSONObject(exported.ConfigJSON) {
		t.Errorf("ConfigJSON = %q, want a JSON object", exported.ConfigJSON)
	}
	// Export renders the stored bytes as indented JSON, so it stays loadable.
	if !strings.Contains(exported.ConfigJSON, "\n") {
		t.Errorf("ConfigJSON = %q, want pretty-printed output", exported.ConfigJSON)
	}

	imported, err := h.svc.Import(ctx, ImportInput{
		Name:        "Imported",
		ConfigJSON:  exported.ConfigJSON,
		Description: source.Description,
	})
	if err != nil {
		t.Fatalf("Import() = %v", err)
	}
	if imported.ID == source.ID {
		t.Error("the import reused the source profile id")
	}
	importedActive := h.revisions(t, imported.ID, 1)[0]
	// Export renders a canonical document (parsed and marshalled with sorted
	// keys), and importing it stores that document verbatim.
	if importedActive.ConfigJSON != strings.TrimSpace(exported.ConfigJSON) {
		t.Errorf("imported config = %q, want %q", importedActive.ConfigJSON, exported.ConfigJSON)
	}
	if importedActive.Source != profile.SourceImport {
		t.Errorf("imported source = %q, want import", importedActive.Source)
	}
	if imported.Description != source.Description {
		t.Errorf("imported description = %q, want %q", imported.Description, source.Description)
	}

	// Exporting the import again yields the same document: the round trip is
	// lossless for a config that was already pretty-printed.
	again, err := h.svc.Export(ctx, imported.ID)
	if err != nil {
		t.Fatalf("Export(imported) = %v", err)
	}
	if again.ConfigJSON != exported.ConfigJSON {
		t.Errorf("second export = %q, want %q", again.ConfigJSON, exported.ConfigJSON)
	}
}

func TestImportReportsAnUnusableFileWithoutTouchingAnything(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	_, err := h.svc.Import(ctx, ImportInput{Name: "Bad", ConfigJSON: "not json at all"})
	wantCode(t, err, apperr.CodeConfigInvalid)
	details := apperr.DetailsOf(err)
	joined := strings.Join(details, " | ")
	if !strings.Contains(joined, "source file is untouched") {
		t.Errorf("details = %q, want the note that the imported file was left alone", joined)
	}
	list, listErr := h.svc.List(ctx)
	if listErr != nil {
		t.Fatalf("List() = %v", listErr)
	}
	if len(list) != 0 {
		t.Errorf("profiles = %+v, want none after a failed import", list)
	}
}

func TestImportOfAnEmptyDocumentFallsBackToTheEmptyTemplate(t *testing.T) {
	h := newHarness(t)
	// A whitespace-only document is not a configuration; the service treats it
	// like "no config given" and starts from the empty template instead of
	// storing a body that cannot be parsed.
	p, err := h.svc.Import(context.Background(), ImportInput{Name: "Blank", ConfigJSON: "  \n\t "})
	if err != nil {
		t.Fatalf("Import(blank) = %v", err)
	}
	rev := h.revisions(t, p.ID, 1)[0]
	if !isJSONObject(rev.ConfigJSON) {
		t.Errorf("config = %q, want the empty template", rev.ConfigJSON)
	}
	if rev.Source != profile.SourceTemplate {
		t.Errorf("source = %q, want template", rev.Source)
	}
}

func TestImportUsesADefaultName(t *testing.T) {
	h := newHarness(t)
	p, err := h.svc.Import(context.Background(), ImportInput{
		ConfigJSON: `{"outbounds":[{"type":"direct","tag":"direct"}]}`,
	})
	if err != nil {
		t.Fatalf("Import() = %v", err)
	}
	if p.Name != "Imported profile" {
		t.Errorf("Name = %q, want the documented default", p.Name)
	}
}

func TestExportFileNameStaysPortable(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		want string
	}{
		{"Home / Office", "Home--Office.json"},
		{"Ünïcode ✓", "ncode-.json"},
		{"№", "profile.json"},
	} {
		p := h.create(t, tc.name)
		exported, err := h.svc.Export(ctx, p.ID)
		if err != nil {
			t.Fatalf("Export(%q) = %v", tc.name, err)
		}
		if exported.FileName != tc.want {
			t.Errorf("FileName(%q) = %q, want %q", tc.name, exported.FileName, tc.want)
		}
		if strings.ContainsAny(exported.FileName, `/\`) {
			t.Errorf("FileName(%q) = %q, want no path separators", tc.name, exported.FileName)
		}
	}
}

func TestExportReportsAMissingProfile(t *testing.T) {
	h := newHarness(t)
	_, err := h.svc.Export(context.Background(), "missing")
	wantCode(t, err, apperr.CodeProfileNotFound)
}

func TestNilRuntimeGuardStillAllowsDeletion(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	p := h.create(t, "Headless")
	// Before the supervisor exists the service must behave as "nothing runs".
	svc := New(h.store, nil, events.Noop{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{})
	if err := svc.Delete(ctx, p.ID); err != nil {
		t.Fatalf("Delete() with a nil guard = %v", err)
	}
}

func TestCreateRejectsAnUnknownTemplate(t *testing.T) {
	h := newHarness(t)
	_, err := h.svc.Create(context.Background(), CreateInput{Name: "Nope", TemplateID: "does-not-exist"})
	if err == nil {
		t.Fatal("Create(unknown template) = nil, want an error")
	}
	if code := apperr.CodeOf(err); code == "" {
		t.Errorf("error %v carries no code", err)
	}
	if apperr.IsCode(err, apperr.CodeInvalidArgument) || errors.Is(err, context.Canceled) {
		// The template lookup is allowed to report either flavour of "not
		// found"; what matters is that nothing was stored.
		t.Logf("template error: %v", err)
	}
	list, listErr := h.svc.List(context.Background())
	if listErr != nil {
		t.Fatalf("List() = %v", listErr)
	}
	if len(list) != 0 {
		t.Errorf("profiles = %+v, want none after a failed create", list)
	}
}
