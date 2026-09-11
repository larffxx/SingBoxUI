package config

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/profile"
	"github.com/larffxx/singboxui/internal/platform"
	"github.com/larffxx/singboxui/internal/storage/sqlite"
)

// This file drives the whole config pipeline against the real SQLite store and
// the real atomic writer: the memStore tests cover the decision logic, these
// cover the promise that a revision never changes once it is on disk and that a
// failed apply leaves the previous file byte-for-byte as it was (spec §12, §14).

type sqliteHarness struct {
	svc   *Service
	store *sqlite.Store
	rt    *fakeRuntime
	em    *recorder
	paths platform.Paths
	db    string
	now   time.Time
	seq   int
}

func (h *sqliteHarness) clock() time.Time {
	h.now = h.now.Add(time.Second)
	return h.now
}

func (h *sqliteHarness) ids() string {
	h.seq++
	return "rev-" + string(rune('a'+h.seq-1))
}

func newSQLiteHarness(t *testing.T) *sqliteHarness {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	paths := testPaths(root)

	store, err := sqlite.Open(ctx, paths.DBPath)
	if err != nil {
		t.Fatalf("sqlite.Open(%s) = %v, want nil", paths.DBPath, err)
	}
	t.Cleanup(func() { _ = store.Close() })

	h := &sqliteHarness{
		store: store,
		rt:    &fakeRuntime{},
		em:    &recorder{},
		paths: paths,
		db:    paths.DBPath,
		now:   time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC),
	}
	h.svc = New(Deps{
		Store:        store,
		Binaries:     &fakeBinaries{path: fakeSingBoxPath, version: mustVersion(t, "1.14.0")},
		Runtime:      h.rt,
		Paths:        paths,
		Emitter:      h.em,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		CheckTimeout: 20 * time.Second,
		NewID:        h.ids,
		Now:          h.clock,
	})

	// An imported profile with one recorded revision, as the migration leaves it.
	p := profile.Profile{
		ID: "p-1", Name: "Work", CreatedAt: h.clock(), UpdatedAt: h.clock(),
		ActiveRevisionID: "rev-imported",
	}
	initial := profile.Revision{
		ID: "rev-imported", ProfileID: p.ID, CreatedAt: h.clock(),
		Source: profile.SourceImport, ConfigJSON: prettyConfig(t, configA),
		StructuralValidation: profile.StatusPassed,
	}
	if err := store.CreateProfile(ctx, p, initial); err != nil {
		t.Fatalf("CreateProfile() = %v, want nil", err)
	}
	return h
}

func TestRevisionsAreImmutableThroughRealPersistence(t *testing.T) {
	ctx := context.Background()
	h := newSQLiteHarness(t)

	first, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configB, Apply: true})
	if err != nil {
		t.Fatalf("SaveRevision(apply) = %v, want nil", err)
	}
	if !first.Active {
		t.Fatal("the applied revision is not reported active")
	}
	firstPersisted, err := h.store.GetRevision(ctx, "p-1", first.ID)
	if err != nil {
		t.Fatalf("GetRevision() = %v", err)
	}
	if firstPersisted.ParentRevisionID != "rev-imported" {
		t.Errorf("parent = %q, want the active revision it derived from", firstPersisted.ParentRevisionID)
	}

	// A second save must not touch the first revision's row.
	second, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configTun, Comment: "with tun"})
	if err != nil {
		t.Fatalf("second SaveRevision() = %v, want nil", err)
	}
	reRead, err := h.store.GetRevision(ctx, "p-1", first.ID)
	if err != nil {
		t.Fatalf("GetRevision() after a second save = %v", err)
	}
	if reRead.ConfigJSON != firstPersisted.ConfigJSON {
		t.Errorf("the first revision's bytes changed:\n before %q\n after  %q", firstPersisted.ConfigJSON, reRead.ConfigJSON)
	}
	if !reRead.CreatedAt.Equal(firstPersisted.CreatedAt) || reRead.Source != firstPersisted.Source {
		t.Errorf("the first revision's metadata changed: %+v -> %+v", firstPersisted, reRead)
	}
	if second.ParentRevisionID != first.ID {
		t.Errorf("second parent = %q, want the active revision %q", second.ParentRevisionID, first.ID)
	}
	if reRead.SingBoxVersion != "1.14.0" {
		t.Errorf("recorded validator version = %q, want the version that accepted it", reRead.SingBoxVersion)
	}

	view, err := h.svc.ListRevisions(ctx, "p-1", 2)
	if err != nil {
		t.Fatalf("service ListRevisions(limit 2) = %v", err)
	}
	if len(view) != 2 || !view[1].Active || view[0].Active {
		t.Errorf("limit 2 = %d revisions with markers %v,%v; want the applied revision marked active", len(view), view[0].Active, view[1].Active)
	}

	// Reopen the database: everything the store promised must survive.
	if err := h.store.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	reopened, err := sqlite.Open(ctx, h.db)
	if err != nil {
		t.Fatalf("reopening the database = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	afterReopen, err := reopened.GetRevision(ctx, "p-1", first.ID)
	if err != nil {
		t.Fatalf("GetRevision() after reopen = %v", err)
	}
	if afterReopen.ConfigJSON != firstPersisted.ConfigJSON {
		t.Errorf("the revision changed across a reopen:\n before %q\n after  %q", firstPersisted.ConfigJSON, afterReopen.ConfigJSON)
	}
	active, err := reopened.ActiveRevision(ctx, "p-1")
	if err != nil {
		t.Fatalf("ActiveRevision() after reopen = %v", err)
	}
	if active.ID != first.ID {
		t.Errorf("active revision = %q, want %q: the save did not commit", active.ID, first.ID)
	}
	history, err := reopened.ListRevisions(ctx, "p-1", 0)
	if err != nil {
		t.Fatalf("ListRevisions() = %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("history = %d revisions, want 3 (imported, first, second)", len(history))
	}
	if history[0].ID != second.ID || history[1].ID != first.ID || history[2].ID != "rev-imported" {
		t.Errorf("history order = %q,%q,%q; want newest first", history[0].ID, history[1].ID, history[2].ID)
	}
}

func TestFailedApplyRestoresPersistedBytesOnDisk(t *testing.T) {
	ctx := context.Background()
	h := newSQLiteHarness(t)
	h.rt.running = true

	if _, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configB, Apply: true}); err != nil {
		t.Fatalf("SaveRevision(apply) = %v", err)
	}
	before, err := h.svc.ActiveConfigJSON()
	if err != nil {
		t.Fatalf("ActiveConfigJSON() = %v", err)
	}
	previous, err := h.store.ActiveRevision(ctx, "p-1")
	if err != nil {
		t.Fatalf("ActiveRevision() = %v", err)
	}
	if before != previous.ConfigJSON {
		t.Fatalf("active file = %q, want the active revision's bytes %q", before, previous.ConfigJSON)
	}

	next, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configTun})
	if err != nil {
		t.Fatalf("second SaveRevision() = %v", err)
	}
	h.rt.restartErr = errors.New("sing-box exited with code 1")

	result, err := h.svc.Apply(ctx, ApplyInput{ProfileID: "p-1", RevisionID: next.ID})
	wantCode(t, err, apperr.CodeConfigApplyFailed)
	if !strings.Contains(result.Warning, "restored") {
		t.Errorf("Warning = %q, want it to say the previous configuration was restored", result.Warning)
	}

	// The file on disk is exactly what it was, and exactly the bytes the store
	// still holds for the previous revision.
	restored, err := h.svc.ActiveConfigJSON()
	if err != nil {
		t.Fatalf("ActiveConfigJSON() after the failed apply = %v", err)
	}
	if restored != before {
		t.Errorf("the active file was not restored byte-for-byte:\n before %q\n after  %q", before, restored)
	}
	stillActive, err := h.store.ActiveRevision(ctx, "p-1")
	if err != nil {
		t.Fatalf("ActiveRevision() after the failure = %v", err)
	}
	if stillActive.ID != previous.ID {
		t.Errorf("active revision = %q, want the previous %q", stillActive.ID, previous.ID)
	}
	if stillActive.ConfigJSON != restored {
		t.Errorf("the store and the file disagree: file %q, store %q", restored, stillActive.ConfigJSON)
	}
	// The refused revision is kept, marked failed, and not active.
	failed, err := h.store.GetRevision(ctx, "p-1", next.ID)
	if err != nil {
		t.Fatalf("GetRevision(failed) = %v", err)
	}
	if failed.SingBoxValidation != profile.StatusFailed {
		t.Errorf("the refused revision's verdict = %q, want failed", failed.SingBoxValidation)
	}
	if got := h.rt.started; len(got) != 1 || got[0] != "p-1" {
		t.Errorf("runtime restarts = %v, want the previous configuration started again", got)
	}
	if left := scratchFiles(t, h.paths.ConfigDir); len(left) != 0 {
		t.Errorf("scratch files left behind: %v", left)
	}
}

func TestRollbackRoundTripsThroughTheRealStore(t *testing.T) {
	ctx := context.Background()
	h := newSQLiteHarness(t)

	first, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configB, Apply: true})
	if err != nil {
		t.Fatalf("SaveRevision(first) = %v", err)
	}
	second, err := h.svc.SaveRevision(ctx, SaveInput{ProfileID: "p-1", ConfigJSON: configTun, Apply: true})
	if err != nil {
		t.Fatalf("SaveRevision(second) = %v", err)
	}

	rolled, err := h.svc.Rollback(ctx, ApplyInput{ProfileID: "p-1", RevisionID: first.ID})
	if err != nil {
		t.Fatalf("Rollback() = %v, want nil", err)
	}
	if rolled.Source != profile.SourceRollback {
		t.Errorf("rollback source = %q, want %q", rolled.Source, profile.SourceRollback)
	}
	if rolled.ConfigJSON != first.ConfigJSON {
		t.Errorf("rollback bytes = %q, want the historical revision's %q", rolled.ConfigJSON, first.ConfigJSON)
	}
	if rolled.ParentRevisionID != second.ID {
		t.Errorf("rollback parent = %q, want the active revision %q", rolled.ParentRevisionID, second.ID)
	}
	if rolled.Active {
		t.Error("a rollback revision is reported active before it is applied")
	}

	// The rollback produced a new row; the two historical rows are untouched.
	history, err := h.store.ListRevisions(ctx, "p-1", 3)
	if err != nil {
		t.Fatalf("ListRevisions() = %v", err)
	}
	if len(history) != 3 || history[0].ID != rolled.ID {
		t.Fatalf("history = %d revisions starting with %q, want the rollback first", len(history), history[0].ID)
	}
	untouched, err := h.store.GetRevision(ctx, "p-1", first.ID)
	if err != nil {
		t.Fatalf("GetRevision(first) = %v", err)
	}
	if untouched.ConfigJSON != first.ConfigJSON || !untouched.CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("the rolled-back-from revision changed: %+v", untouched)
	}

	if _, err := h.svc.Apply(ctx, ApplyInput{ProfileID: "p-1", RevisionID: rolled.ID}); err != nil {
		t.Fatalf("Apply(rollback revision) = %v, want nil", err)
	}
	onDisk, err := h.svc.ActiveConfigJSON()
	if err != nil {
		t.Fatalf("ActiveConfigJSON() = %v", err)
	}
	if onDisk != first.ConfigJSON {
		t.Errorf("active file after applying the rollback = %q, want %q", onDisk, first.ConfigJSON)
	}
	if filepath.Dir(h.paths.ActiveConfigPath) != h.paths.ConfigDir {
		t.Fatalf("test wiring: the active file must live in the config directory")
	}
}
