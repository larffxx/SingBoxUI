package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/profile"
	"github.com/larffxx/singboxui/internal/domain/settings"
)

// openTestStore opens a real SQLite database inside the test's temporary
// directory. Nothing is mocked: the schema, the pragmas and the SQL all run.
func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open(%s) = %v, want nil", path, err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() = %v", err)
		}
	})
	return store, path
}

func requireCode(t *testing.T, err error, want apperr.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %s", want)
	}
	if got := apperr.CodeOf(err); got != want {
		t.Fatalf("error code = %s, want %s (err = %v)", got, want, err)
	}
}

func requireErr(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want a failure")
	}
}

func newProfile(id, name string, at time.Time) profile.Profile {
	return profile.Profile{
		ID:        id,
		Name:      name,
		CreatedAt: at,
		UpdatedAt: at,
	}
}

func newRevision(id, profileID, config string, at time.Time) profile.Revision {
	return profile.Revision{
		ID:         id,
		ProfileID:  profileID,
		CreatedAt:  at,
		Source:     profile.SourceManual,
		ConfigJSON: config,
	}
}

func TestOpenCreatesSchemaAndStaysIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("first Open() = %v, want nil", err)
	}

	tables := []string{"profiles", "config_revisions", "settings", "managed_binary", "application_state", "schema_migrations"}
	for _, name := range tables {
		var found string
		err := store.DB().QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&found)
		if err != nil {
			t.Errorf("table %s was not created: %v", name, err)
		}
	}
	var indexes int
	if err := store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name IN ('profiles_name_unique','config_revisions_profile_idx')`).
		Scan(&indexes); err != nil {
		t.Fatalf("count indexes: %v", err)
	}
	if indexes != 2 {
		t.Errorf("indexes = %d, want the 2 declared constraints", indexes)
	}

	var applied int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if applied == 0 {
		t.Fatal("no migration was recorded")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}

	// Re-opening the same file must re-run migrate() safely: the schema exists,
	// every migration is already recorded, and nothing is applied twice.
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("second Open() = %v, want nil", err)
	}
	defer reopened.Close()

	var reapplied int
	if err := reopened.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&reapplied); err != nil {
		t.Fatalf("count migrations after reopen: %v", err)
	}
	if reapplied != applied {
		t.Errorf("migrations after reopen = %d, want %d: a migration was applied twice", reapplied, applied)
	}

	// The schema is usable after the second migrate: a write still succeeds.
	p := newProfile("p1", "Default", time.Now().UTC())
	if err := reopened.CreateProfile(ctx, p, newRevision("r1", "p1", "{}", p.CreatedAt)); err != nil {
		t.Fatalf("CreateProfile() after reopen = %v, want nil", err)
	}
}

func TestOpenReportsAnUnusableDatabasePath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// The "directory" of the database is a regular file, so MkdirAll fails.
	blocker := filepath.Join(dir, "not-a-dir")
	if err := writeFile(t, blocker, "x"); err != nil {
		t.Fatalf("seed blocker file: %v", err)
	}

	_, err := Open(context.Background(), filepath.Join(blocker, "state.db"))
	requireCode(t, err, apperr.CodeDatabaseError)
}

func TestStoreSurvivesReopen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	installedAt := time.Date(2026, 3, 4, 6, 0, 0, 0, time.UTC)

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	p := newProfile("p-1", "Work", at)
	if err := store.CreateProfile(ctx, p, newRevision("rev-1", "p-1", `{"log":{"level":"info"}}`, at)); err != nil {
		t.Fatalf("CreateProfile() = %v", err)
	}
	if err := store.SaveSettings(ctx, settings.Settings{
		AutoConnect:          true,
		BinarySource:         settings.BinaryManaged,
		ManagedStableChannel: true,
		UpdateCheckEnabled:   true,
		Theme:                settings.ThemeDark,
		LogLevel:             settings.LogWarn,
		LastProfileID:        "p-1",
	}); err != nil {
		t.Fatalf("SaveSettings() = %v", err)
	}
	if err := store.SaveManagedBinary(ctx, ManagedBinary{
		Version: "1.14.0", Path: "/data/bin/1.14.0/sing-box", AssetName: "sing-box-1.14.0-darwin-arm64.tar.gz",
		SHA256: "aabbcc", InstalledAt: &installedAt, PreviousVersion: "1.13.0",
	}); err != nil {
		t.Fatalf("SaveManagedBinary() = %v", err)
	}
	if err := store.SetState(ctx, "lastCheck", "2026-03-04T06:00:00Z"); err != nil {
		t.Fatalf("SetState() = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen = %v", err)
	}
	defer reopened.Close()

	got, err := reopened.GetProfile(ctx, "p-1")
	if err != nil {
		t.Fatalf("GetProfile() after reopen = %v", err)
	}
	if got.Name != "Work" || !got.CreatedAt.Equal(at) {
		t.Errorf("profile after reopen = %+v, want name Work and createdAt %s", got, at)
	}
	rev, err := reopened.GetRevision(ctx, "p-1", "rev-1")
	if err != nil {
		t.Fatalf("GetRevision() after reopen = %v", err)
	}
	if rev.ConfigJSON != `{"log":{"level":"info"}}` {
		t.Errorf("ConfigJSON after reopen = %q", rev.ConfigJSON)
	}
	if rev.Source != profile.SourceManual {
		t.Errorf("Source after reopen = %q, want manual", rev.Source)
	}
	if rev.StructuralValidation != profile.StatusUnknown || rev.SingBoxValidation != profile.StatusUnknown {
		t.Errorf("validation status after reopen = %q/%q, want the unknown defaults",
			rev.StructuralValidation, rev.SingBoxValidation)
	}
	values, err := reopened.LoadSettings(ctx)
	if err != nil {
		t.Fatalf("LoadSettings() after reopen = %v", err)
	}
	if values.Theme != settings.ThemeDark || values.LogLevel != settings.LogWarn || !values.AutoConnect {
		t.Errorf("settings after reopen = %+v", values)
	}
	bin, err := reopened.GetManagedBinary(ctx)
	if err != nil {
		t.Fatalf("GetManagedBinary() after reopen = %v", err)
	}
	if bin.Version != "1.14.0" || bin.SHA256 != "aabbcc" || bin.PreviousVersion != "1.13.0" {
		t.Errorf("managed binary after reopen = %+v", bin)
	}
	if bin.InstalledAt == nil || !bin.InstalledAt.Equal(installedAt) {
		t.Errorf("InstalledAt = %v, want %s", bin.InstalledAt, installedAt)
	}
	state, ok, err := reopened.GetState(ctx, "lastCheck")
	if err != nil || !ok || state != "2026-03-04T06:00:00Z" {
		t.Errorf("GetState() = %q/%v/%v after reopen", state, ok, err)
	}
}

func TestProfileCRUDAndNameUniqueness(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, _ := openTestStore(t)
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	for _, p := range []struct{ id, name string }{
		{"p-b", "zeta"},
		{"p-a", "Alpha"},
		{"p-c", "beta"},
	} {
		if err := store.CreateProfile(ctx, newProfile(p.id, p.name, at), newRevision("rev-"+p.id, p.id, "{}", at)); err != nil {
			t.Fatalf("CreateProfile(%s) = %v", p.id, err)
		}
	}

	list, err := store.ListProfiles(ctx)
	if err != nil {
		t.Fatalf("ListProfiles() = %v", err)
	}
	names := make([]string, 0, len(list))
	for _, p := range list {
		names = append(names, p.Name)
	}
	if want := []string{"Alpha", "beta", "zeta"}; !equalStrings(names, want) {
		t.Errorf("profile order = %v, want the case-insensitive name order %v", names, want)
	}

	// Name uniqueness is enforced case-insensitively by the schema.
	err = store.CreateProfile(ctx, newProfile("p-d", "alpha", at), newRevision("rev-p-d", "p-d", "{}", at))
	requireCode(t, err, apperr.CodeProfileNameConflict)

	// A failed create must not leave the profile or its revision behind.
	if _, err := store.GetProfile(ctx, "p-d"); !apperr.IsCode(err, apperr.CodeProfileNotFound) {
		t.Errorf("GetProfile(p-d) = %v, want PROFILE_NOT_FOUND", err)
	}
	if n, err := store.CountRevisions(ctx, "p-d"); err != nil || n != 0 {
		t.Errorf("CountRevisions(p-d) = %d/%v, want 0: the revision insert must roll back with the profile", n, err)
	}

	updated := newProfile("p-b", "Zeta renamed", at)
	updated.Description = "renamed by test"
	updated.UpdatedAt = at.Add(time.Hour)
	if err := store.UpdateProfile(ctx, updated); err != nil {
		t.Fatalf("UpdateProfile() = %v", err)
	}
	got, err := store.GetProfile(ctx, "p-b")
	if err != nil {
		t.Fatalf("GetProfile() = %v", err)
	}
	if got.Name != "Zeta renamed" || got.Description != "renamed by test" {
		t.Errorf("profile after update = %+v", got)
	}
	if !got.UpdatedAt.Equal(updated.UpdatedAt) {
		t.Errorf("UpdatedAt = %s, want %s", got.UpdatedAt, updated.UpdatedAt)
	}

	if err := store.UpdateProfile(ctx, newProfile("missing", "x", at)); !apperr.IsCode(err, apperr.CodeProfileNotFound) {
		t.Errorf("UpdateProfile(missing) = %v, want PROFILE_NOT_FOUND", err)
	}
	if _, err := store.GetProfile(ctx, "missing"); !apperr.IsCode(err, apperr.CodeProfileNotFound) {
		t.Errorf("GetProfile(missing) = %v, want PROFILE_NOT_FOUND", err)
	}
	if err := store.DeleteProfile(ctx, "missing"); !apperr.IsCode(err, apperr.CodeProfileNotFound) {
		t.Errorf("DeleteProfile(missing) = %v, want PROFILE_NOT_FOUND", err)
	}

	if err := store.TouchLastUsed(ctx, "p-b"); err != nil {
		t.Fatalf("TouchLastUsed() = %v", err)
	}
	got, err = store.GetProfile(ctx, "p-b")
	if err != nil {
		t.Fatalf("GetProfile() after touch = %v", err)
	}
	if got.LastUsedAt == nil {
		t.Error("LastUsedAt = nil after TouchLastUsed")
	}
	if err := store.TouchLastUsed(ctx, "missing"); !apperr.IsCode(err, apperr.CodeProfileNotFound) {
		t.Errorf("TouchLastUsed(missing) = %v, want PROFILE_NOT_FOUND", err)
	}
}

func TestCreateProfileRejectsIncompleteInput(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, _ := openTestStore(t)
	at := time.Now().UTC()

	tests := []struct {
		name     string
		prof     profile.Profile
		revision profile.Revision
	}{
		{name: "missing profile id", prof: newProfile("", "n", at), revision: newRevision("r-a", "", "{}", at)},
		{name: "missing revision id", prof: newProfile("p-a", "n", at), revision: newRevision("", "p-a", "{}", at)},
		{name: "unknown revision source", prof: newProfile("p-b", "n", at), revision: func() profile.Revision {
			r := newRevision("r-b", "p-b", "{}", at)
			r.Source = "telepathy"
			return r
		}()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			requireErr(t, store.CreateProfile(ctx, tc.prof, tc.revision))
		})
	}
}

func TestDeleteProfileCascadesItsRevisions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, _ := openTestStore(t)
	at := time.Date(2026, 2, 2, 2, 2, 2, 0, time.UTC)

	if err := store.CreateProfile(ctx, newProfile("p-1", "Doomed", at), newRevision("rev-1", "p-1", "{}", at)); err != nil {
		t.Fatalf("CreateProfile() = %v", err)
	}
	for i := 2; i <= 4; i++ {
		rev := newRevision(fmt.Sprintf("rev-%d", i), "p-1", "{}", at.Add(time.Duration(i)*time.Minute))
		if err := store.CreateRevision(ctx, rev, false); err != nil {
			t.Fatalf("CreateRevision(%d) = %v", i, err)
		}
	}
	if n, err := store.CountRevisions(ctx, "p-1"); err != nil || n != 4 {
		t.Fatalf("CountRevisions() = %d/%v, want 4", n, err)
	}

	if err := store.DeleteProfile(ctx, "p-1"); err != nil {
		t.Fatalf("DeleteProfile() = %v", err)
	}
	if n, err := store.CountRevisions(ctx, "p-1"); err != nil || n != 0 {
		t.Errorf("CountRevisions() after delete = %d/%v, want 0 (ON DELETE CASCADE)", n, err)
	}
	if _, err := store.GetRevision(ctx, "p-1", "rev-1"); !apperr.IsCode(err, apperr.CodeRevisionNotFound) {
		t.Errorf("GetRevision() after delete = %v, want REVISION_NOT_FOUND", err)
	}
}

// TestRevisionIsImmutableOnceSaved is the storage half of spec §12, §68: only
// the validation columns of a revision may change, never its configuration.
func TestRevisionIsImmutableOnceSaved(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, _ := openTestStore(t)
	at := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	original := `{"outbounds":[{"type":"direct"}]}`

	if err := store.CreateProfile(ctx, newProfile("p-1", "Main", at), newRevision("rev-1", "p-1", original, at)); err != nil {
		t.Fatalf("CreateProfile() = %v", err)
	}

	// A second revision with distinct bytes must not disturb the first.
	second := newRevision("rev-2", "p-1", `{"outbounds":[]}`, at.Add(time.Minute))
	second.ParentRevisionID = "rev-1"
	second.Source = profile.SourceImport
	if err := store.CreateRevision(ctx, second, true); err != nil {
		t.Fatalf("CreateRevision() = %v", err)
	}

	first, err := store.GetRevision(ctx, "p-1", "rev-1")
	if err != nil {
		t.Fatalf("GetRevision() = %v", err)
	}
	if first.ConfigJSON != original {
		t.Errorf("rev-1 ConfigJSON = %q, want the bytes it was saved with %q", first.ConfigJSON, original)
	}
	if first.Source != profile.SourceManual {
		t.Errorf("rev-1 Source = %q, want manual", first.Source)
	}
	if !first.CreatedAt.Equal(at) {
		t.Errorf("rev-1 CreatedAt = %s, want %s", first.CreatedAt, at)
	}

	// Recording a validation outcome touches only the validation columns.
	if err := store.SetRevisionValidation(ctx, "rev-1", profile.StatusPassed, profile.StatusPassed, "1.14.0"); err != nil {
		t.Fatalf("SetRevisionValidation() = %v", err)
	}
	after, err := store.GetRevision(ctx, "p-1", "rev-1")
	if err != nil {
		t.Fatalf("GetRevision() after validation = %v", err)
	}
	if after.ConfigJSON != first.ConfigJSON || after.Source != first.Source || !after.CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("validation changed the immutable fields: before %+v after %+v", first, after)
	}
	if after.StructuralValidation != profile.StatusPassed || after.SingBoxValidation != profile.StatusPassed {
		t.Errorf("validation status = %q/%q, want passed/passed", after.StructuralValidation, after.SingBoxValidation)
	}
	if after.SingBoxVersion != "1.14.0" {
		t.Errorf("SingBoxVersion = %q, want 1.14.0", after.SingBoxVersion)
	}

	// Re-inserting an existing revision id is a conflict, not an overwrite.
	err = store.CreateRevision(ctx, newRevision("rev-1", "p-1", `{"tampered":true}`, at), false)
	requireErr(t, err)
	again, err := store.GetRevision(ctx, "p-1", "rev-1")
	if err != nil {
		t.Fatalf("GetRevision() = %v", err)
	}
	if again.ConfigJSON != original {
		t.Errorf("rev-1 ConfigJSON = %q, want the original bytes %q", again.ConfigJSON, original)
	}
}

func TestSetRevisionValidationAndActiveRevisionErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, _ := openTestStore(t)
	at := time.Date(2026, 2, 4, 0, 0, 0, 0, time.UTC)

	if err := store.CreateProfile(ctx, newProfile("p-1", "Main", at), newRevision("rev-1", "p-1", "{}", at)); err != nil {
		t.Fatalf("CreateProfile() = %v", err)
	}

	if err := store.SetRevisionValidation(ctx, "nope", profile.StatusPassed, profile.StatusPassed, "1.14.0"); !apperr.IsCode(err, apperr.CodeRevisionNotFound) {
		t.Errorf("SetRevisionValidation(unknown) = %v, want REVISION_NOT_FOUND", err)
	}
	if err := store.SetActiveRevision(ctx, "p-1", "nope"); err != nil {
		// The column has no foreign key, so this is accepted; what matters is
		// that ActiveRevision then reports the dangling pointer as missing.
		t.Fatalf("SetActiveRevision() = %v, want nil", err)
	}
	if _, err := store.ActiveRevision(ctx, "p-1"); !apperr.IsCode(err, apperr.CodeRevisionNotFound) {
		t.Errorf("ActiveRevision() with a dangling pointer = %v, want REVISION_NOT_FOUND", err)
	}
	if err := store.SetActiveRevision(ctx, "missing", "rev-1"); !apperr.IsCode(err, apperr.CodeProfileNotFound) {
		t.Errorf("SetActiveRevision(missing profile) = %v, want PROFILE_NOT_FOUND", err)
	}

	// The profile created with an initial revision, when nothing was made
	// active, must report that it has no active revision.
	if err := store.SetActiveRevision(ctx, "p-1", ""); err != nil {
		t.Fatalf("SetActiveRevision(clear) = %v", err)
	}
	if _, err := store.ActiveRevision(ctx, "p-1"); !apperr.IsCode(err, apperr.CodeRevisionNotFound) {
		t.Errorf("ActiveRevision() with no pointer = %v, want REVISION_NOT_FOUND", err)
	}

	if err := store.SetActiveRevision(ctx, "p-1", "rev-1"); err != nil {
		t.Fatalf("SetActiveRevision(rev-1) = %v", err)
	}
	active, err := store.ActiveRevision(ctx, "p-1")
	if err != nil {
		t.Fatalf("ActiveRevision() = %v", err)
	}
	if active.ID != "rev-1" {
		t.Errorf("active revision = %q, want rev-1", active.ID)
	}
	if !active.IsActive(profile.Profile{ActiveRevisionID: "rev-1"}) {
		t.Error("IsActive() = false for the active revision")
	}
}

func TestListRevisionsIsNewestFirstAndBounded(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, _ := openTestStore(t)
	base := time.Date(2026, 2, 5, 10, 0, 0, 0, time.UTC)

	if err := store.CreateProfile(ctx, newProfile("p-1", "Main", base), newRevision("rev-1", "p-1", "{}", base)); err != nil {
		t.Fatalf("CreateProfile() = %v", err)
	}
	// Five more revisions, minutes apart, plus two that share a timestamp so
	// the documented rowid tie-break is exercised.
	for i := 2; i <= 6; i++ {
		rev := newRevision(fmt.Sprintf("rev-%d", i), "p-1", "{}", base.Add(time.Duration(i)*time.Minute))
		if err := store.CreateRevision(ctx, rev, false); err != nil {
			t.Fatalf("CreateRevision(rev-%d) = %v", i, err)
		}
	}
	tie := base.Add(10 * time.Minute)
	for _, id := range []string{"tie-a", "tie-b"} {
		if err := store.CreateRevision(ctx, newRevision(id, "p-1", "{}", tie), false); err != nil {
			t.Fatalf("CreateRevision(%s) = %v", id, err)
		}
	}

	all, err := store.ListRevisions(ctx, "p-1", 0)
	if err != nil {
		t.Fatalf("ListRevisions() = %v", err)
	}
	if len(all) != 8 {
		t.Fatalf("revisions = %d, want 8", len(all))
	}
	if all[0].ID != "tie-b" || all[1].ID != "tie-a" {
		t.Errorf("newest revisions = %q, %q; want tie-b then tie-a (rowid breaks the tie)", all[0].ID, all[1].ID)
	}
	for i := 1; i < len(all); i++ {
		if all[i].CreatedAt.After(all[i-1].CreatedAt) {
			t.Errorf("revisions are not ordered newest-first at index %d: %s before %s", i, all[i-1].CreatedAt, all[i].CreatedAt)
		}
	}
	last := all[len(all)-1]
	if last.ID != "rev-1" {
		t.Errorf("oldest revision = %q, want rev-1", last.ID)
	}

	limited, err := store.ListRevisions(ctx, "p-1", 3)
	if err != nil {
		t.Fatalf("ListRevisions(limit 3) = %v", err)
	}
	if len(limited) != 3 {
		t.Fatalf("limited revisions = %d, want 3", len(limited))
	}
	for i, rev := range limited {
		if rev.ID != all[i].ID {
			t.Errorf("limited[%d] = %q, want %q", i, rev.ID, all[i].ID)
		}
	}

	// A non-positive or absurd limit falls back to the documented default
	// rather than returning everything or nothing.
	for _, limit := range []int{-1, 0, 5000} {
		got, err := store.ListRevisions(ctx, "p-1", limit)
		if err != nil {
			t.Fatalf("ListRevisions(limit %d) = %v", limit, err)
		}
		if len(got) != 8 {
			t.Errorf("ListRevisions(limit %d) returned %d revisions, want all 8 under the default cap", limit, len(got))
		}
	}
}

func TestListRevisionsIsScopedToTheProfile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, _ := openTestStore(t)
	at := time.Date(2026, 2, 6, 0, 0, 0, 0, time.UTC)

	for i, id := range []string{"p-1", "p-2"} {
		if err := store.CreateProfile(ctx, newProfile(id, "P"+id, at), newRevision("rev-"+id, id, "{}", at)); err != nil {
			t.Fatalf("CreateProfile(%s) = %v", id, err)
		}
		if i == 1 {
			if err := store.CreateRevision(ctx, newRevision("rev-p-2-extra", "p-2", "{}", at.Add(time.Minute)), false); err != nil {
				t.Fatalf("CreateRevision() = %v", err)
			}
		}
	}

	one, err := store.ListRevisions(ctx, "p-1", 0)
	if err != nil {
		t.Fatalf("ListRevisions(p-1) = %v", err)
	}
	if len(one) != 1 || one[0].ID != "rev-p-1" {
		t.Errorf("p-1 revisions = %+v, want only rev-p-1", one)
	}
	two, err := store.ListRevisions(ctx, "p-2", 0)
	if err != nil {
		t.Fatalf("ListRevisions(p-2) = %v", err)
	}
	if len(two) != 2 {
		t.Errorf("p-2 revisions = %d, want 2", len(two))
	}
	empty, err := store.ListRevisions(ctx, "missing", 0)
	if err != nil {
		t.Fatalf("ListRevisions(missing) = %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("ListRevisions(missing) = %d revisions, want none", len(empty))
	}
}

func TestMarkRevisionsStale(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, _ := openTestStore(t)
	at := time.Date(2026, 2, 7, 0, 0, 0, 0, time.UTC)

	if err := store.CreateProfile(ctx, newProfile("p-1", "Main", at), newRevision("rev-old", "p-1", "{}", at)); err != nil {
		t.Fatalf("CreateProfile() = %v", err)
	}
	// The oldest revision was validated by an older sing-box release.
	if err := store.SetRevisionValidation(ctx, "rev-old", profile.StatusPassed, profile.StatusPassed, "1.13.0"); err != nil {
		t.Fatalf("SetRevisionValidation(rev-old) = %v", err)
	}
	for _, rev := range []struct {
		id       string
		structur profile.ValidationStatus
		version  string
		singbox  profile.ValidationStatus
	}{
		{id: "rev-new", structur: profile.StatusPassed, version: "1.14.0", singbox: profile.StatusPassed},
		{id: "rev-failed", structur: profile.StatusFailed, version: "1.13.0", singbox: profile.StatusFailed},
		{id: "rev-unvalidated", structur: profile.StatusUnknown, version: "", singbox: profile.StatusUnknown},
	} {
		r := newRevision(rev.id, "p-1", "{}", at.Add(time.Minute))
		if err := store.CreateRevision(ctx, r, false); err != nil {
			t.Fatalf("CreateRevision(%s) = %v", rev.id, err)
		}
		if err := store.SetRevisionValidation(ctx, rev.id, rev.structur, rev.singbox, rev.version); err != nil {
			t.Fatalf("SetRevisionValidation(%s) = %v", rev.id, err)
		}
	}

	affected, err := store.MarkRevisionsStale(ctx, "1.14.0")
	if err != nil {
		t.Fatalf("MarkRevisionsStale() = %v", err)
	}
	if affected != 1 {
		t.Errorf("affected = %d, want 1: only the revision passed by the older version", affected)
	}
	stale, err := store.GetRevision(ctx, "p-1", "rev-old")
	if err != nil {
		t.Fatalf("GetRevision(rev-old) = %v", err)
	}
	if stale.SingBoxValidation != profile.StatusStale {
		t.Errorf("rev-old status = %q, want stale", stale.SingBoxValidation)
	}
	if stale.ConfigJSON != "{}" {
		t.Errorf("rev-old ConfigJSON = %q, want it untouched", stale.ConfigJSON)
	}
	newest, err := store.GetRevision(ctx, "p-1", "rev-new")
	if err != nil {
		t.Fatalf("GetRevision(rev-new) = %v", err)
	}
	if newest.SingBoxValidation != profile.StatusPassed {
		t.Errorf("rev-new status = %q, want it to stay passed", newest.SingBoxValidation)
	}
	failedRev, err := store.GetRevision(ctx, "p-1", "rev-failed")
	if err != nil {
		t.Fatalf("GetRevision(rev-failed) = %v", err)
	}
	if failedRev.SingBoxValidation != profile.StatusFailed {
		t.Errorf("rev-failed status = %q, want it to stay failed", failedRev.SingBoxValidation)
	}

	// Running it again with the same installed version changes nothing.
	again, err := store.MarkRevisionsStale(ctx, "1.14.0")
	if err != nil {
		t.Fatalf("second MarkRevisionsStale() = %v", err)
	}
	if again != 0 {
		t.Errorf("second run affected = %d, want 0", again)
	}
}

// TestSettingsIgnoreUnknownDatabaseKeys covers spec §49 on the storage side: the
// settings row is a typed model, so extra keys written by another build are
// dropped on read and do not survive the next write.
func TestSettingsIgnoreUnknownDatabaseKeys(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, _ := openTestStore(t)

	raw := `{"version":1,"values":{"binarySource":"managed","theme":"dark","logLevel":"warn",` +
		`"updateCheckEnabled":true,"managedStableChannel":true,"autoConnect":true,` +
		`"javaOnlySwitch":true,"obsolete":{"nested":1},"legacyProfileId":"java-7"}}`
	if _, err := store.DB().ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?)`, settingsKey, raw); err != nil {
		t.Fatalf("seed settings row: %v", err)
	}

	got, err := store.LoadSettings(ctx)
	if err != nil {
		t.Fatalf("LoadSettings() = %v, want nil", err)
	}
	if got.Theme != settings.ThemeDark || got.LogLevel != settings.LogWarn {
		t.Errorf("typed settings = %+v, want the stored theme and log level", got)
	}
	if !got.AutoConnect || !got.UpdateCheckEnabled {
		t.Errorf("typed settings lost a known boolean: %+v", got)
	}

	// The unknown keys are gone as soon as the value is written back.
	if err := store.SaveSettings(ctx, got); err != nil {
		t.Fatalf("SaveSettings() = %v", err)
	}
	var stored string
	if err := store.DB().QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, settingsKey).Scan(&stored); err != nil {
		t.Fatalf("read stored settings: %v", err)
	}
	for _, key := range []string{"javaOnlySwitch", "obsolete", "legacyProfileId"} {
		if strings.Contains(stored, key) {
			t.Errorf("unknown key %q survived the rewrite: %s", key, stored)
		}
	}
	if !strings.Contains(stored, `"version":1`) {
		t.Errorf("stored settings lost the version envelope: %s", stored)
	}
}

func TestLoadSettingsFallsBackToDefaults(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("no row yet", func(t *testing.T) {
		t.Parallel()
		store, _ := openTestStore(t)
		got, err := store.LoadSettings(ctx)
		if err != nil {
			t.Fatalf("LoadSettings() = %v, want nil", err)
		}
		if got != settings.Default() {
			t.Errorf("LoadSettings() = %+v, want Default() %+v", got, settings.Default())
		}
	})

	t.Run("corrupted blob", func(t *testing.T) {
		t.Parallel()
		store, _ := openTestStore(t)
		if _, err := store.DB().ExecContext(ctx,
			`INSERT INTO settings (key, value) VALUES (?, ?)`, settingsKey, `{"version":1,"values":`); err != nil {
			t.Fatalf("seed corrupted settings: %v", err)
		}
		got, err := store.LoadSettings(ctx)
		if err == nil {
			t.Fatal("LoadSettings() = nil, want an error for an unreadable blob")
		}
		requireCode(t, err, apperr.CodeDatabaseError)
		if got != settings.Default() {
			t.Errorf("LoadSettings() = %+v, want Default() alongside the error so the app still starts", got)
		}
	})
}

func TestSaveSettingsValidatesAndUpserts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, _ := openTestStore(t)

	invalid := settings.Settings{BinarySource: "system", Theme: settings.ThemeSystem, LogLevel: settings.LogInfo}
	requireCode(t, store.SaveSettings(ctx, invalid), apperr.CodeInvalidArgument)

	if err := store.SaveSettings(ctx, settings.Settings{
		BinarySource: settings.BinaryManaged, Theme: settings.ThemeDark, LogLevel: settings.LogInfo,
	}); err != nil {
		t.Fatalf("SaveSettings() = %v", err)
	}
	if err := store.SaveSettings(ctx, settings.Settings{
		BinarySource: settings.BinaryManaged, Theme: settings.ThemeLight, LogLevel: settings.LogError,
	}); err != nil {
		t.Fatalf("second SaveSettings() = %v", err)
	}
	var rows int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM settings`).Scan(&rows); err != nil {
		t.Fatalf("count settings rows: %v", err)
	}
	if rows != 1 {
		t.Errorf("settings rows = %d, want 1: saving must upsert, not append", rows)
	}
	got, err := store.LoadSettings(ctx)
	if err != nil {
		t.Fatalf("LoadSettings() = %v", err)
	}
	if got.Theme != settings.ThemeLight || got.LogLevel != settings.LogError {
		t.Errorf("settings after upsert = %+v", got)
	}
}

func TestManagedBinaryRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, _ := openTestStore(t)

	empty, err := store.GetManagedBinary(ctx)
	if err != nil {
		t.Fatalf("GetManagedBinary() = %v, want nil for an empty table", err)
	}
	if !empty.Empty() {
		t.Errorf("GetManagedBinary() = %+v, want the empty record", empty)
	}

	installedAt := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	if err := store.SaveManagedBinary(ctx, ManagedBinary{
		Version: "1.14.0", Path: "/data/bin/1.14.0/sing-box",
		AssetName: "sing-box-1.14.0-darwin-arm64.tar.gz", SHA256: "deadbeef",
		InstalledAt: &installedAt, PreviousPath: "/data/bin/1.13.0/sing-box", PreviousVersion: "1.13.0",
	}); err != nil {
		t.Fatalf("SaveManagedBinary() = %v", err)
	}

	got, err := store.GetManagedBinary(ctx)
	if err != nil {
		t.Fatalf("GetManagedBinary() = %v", err)
	}
	if got.Empty() {
		t.Fatal("record reports empty after being saved")
	}
	if got.Version != "1.14.0" || got.Path != "/data/bin/1.14.0/sing-box" || got.SHA256 != "deadbeef" {
		t.Errorf("record = %+v", got)
	}
	if got.PreviousPath != "/data/bin/1.13.0/sing-box" || got.PreviousVersion != "1.13.0" {
		t.Errorf("rollback target was not preserved: %+v", got)
	}
	if got.InstalledAt == nil || !got.InstalledAt.Equal(installedAt) {
		t.Errorf("InstalledAt = %v, want %s", got.InstalledAt, installedAt)
	}

	// The second install replaces the single row.
	if err := store.SaveManagedBinary(ctx, ManagedBinary{
		Version: "1.15.0", Path: "/data/bin/1.15.0/sing-box", AssetName: "a", SHA256: "cafe",
		PreviousPath: "/data/bin/1.14.0/sing-box", PreviousVersion: "1.14.0",
	}); err != nil {
		t.Fatalf("second SaveManagedBinary() = %v", err)
	}
	var rows int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM managed_binary`).Scan(&rows); err != nil {
		t.Fatalf("count managed_binary rows: %v", err)
	}
	if rows != 1 {
		t.Errorf("managed_binary rows = %d, want 1", rows)
	}
	got, err = store.GetManagedBinary(ctx)
	if err != nil {
		t.Fatalf("GetManagedBinary() = %v", err)
	}
	if got.Version != "1.15.0" || got.PreviousVersion != "1.14.0" {
		t.Errorf("record after replace = %+v", got)
	}
	if got.InstalledAt != nil {
		t.Errorf("InstalledAt = %v, want nil when the caller omits it", got.InstalledAt)
	}
}

func TestApplicationStateCRUD(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, _ := openTestStore(t)

	if _, ok, err := store.GetState(ctx, "missing"); err != nil || ok {
		t.Errorf("GetState(missing) = ok %v, err %v; want false/nil", ok, err)
	}
	requireCode(t, store.SetState(ctx, "", "x"), apperr.CodeInvalidArgument)

	if err := store.SetState(ctx, "lastCheck", "2026-04-01T00:00:00Z"); err != nil {
		t.Fatalf("SetState() = %v", err)
	}
	value, ok, err := store.GetState(ctx, "lastCheck")
	if err != nil || !ok || value != "2026-04-01T00:00:00Z" {
		t.Errorf("GetState() = %q/%v/%v", value, ok, err)
	}
	if err := store.SetState(ctx, "lastCheck", "2026-04-02T00:00:00Z"); err != nil {
		t.Fatalf("overwrite state: %v", err)
	}
	value, _, err = store.GetState(ctx, "lastCheck")
	if err != nil || value != "2026-04-02T00:00:00Z" {
		t.Errorf("state after overwrite = %q/%v", value, err)
	}
	if err := store.DeleteState(ctx, "lastCheck"); err != nil {
		t.Fatalf("DeleteState() = %v", err)
	}
	if _, ok, _ := store.GetState(ctx, "lastCheck"); ok {
		t.Error("state still present after delete")
	}
	if err := store.DeleteState(ctx, "lastCheck"); err != nil {
		t.Errorf("DeleteState(missing) = %v, want nil", err)
	}
}

func TestCloseIsIdempotentAndOperationsAfterCloseFail(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Errorf("second Close() = %v, want nil", err)
	}
	err = store.SaveSettings(ctx, settings.Default())
	requireCode(t, err, apperr.CodeDatabaseError)
}

func TestMigrationVersionParsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{name: "numeric prefix", input: "0001_init.sql", want: 1},
		{name: "multi-digit", input: "0012_add_index.sql", want: 12},
		{name: "no separator", input: "init.sql", wantErr: true},
		{name: "leading separator", input: "_init.sql", wantErr: true},
		{name: "non numeric", input: "abc_init.sql", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := migrationVersion(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("migrationVersion(%q) = %d, want an error", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("migrationVersion(%q) = %v, want nil", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("migrationVersion(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestLoadSettingsPropagatesADeadHandle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	defer store.Close()
	if err := store.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}

	_, err = store.LoadSettings(ctx)
	if !apperr.IsCode(err, apperr.CodeDatabaseError) {
		t.Errorf("LoadSettings() = %v, want DATABASE_ERROR", err)
	}
}

func TestWithTxRollsBackOnError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, _ := openTestStore(t)

	sentinel := errors.New("boom")
	err := store.withTx(ctx, "test.withTx", func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO application_state (key, value) VALUES ('k', 'v')`); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("withTx() = %v, want the sentinel error", err)
	}
	if _, ok, err := store.GetState(ctx, "k"); err != nil || ok {
		t.Errorf("the rolled-back write is still visible: ok=%v err=%v", ok, err)
	}
}

func writeFile(t *testing.T, path, content string) error {
	t.Helper()
	return os.WriteFile(path, []byte(content), 0o600)
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
