// Package migrations tests the embedded schema files. The SQL itself is applied
// by the storage layer (see internal/storage/sqlite), so what is asserted here
// are the properties the migration runner relies on: an ordered, contiguous set
// of forward-only files whose content really is embedded in the binary.
package migrations

import (
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// requiredSchema lists what the storage layer queries. A missing table or index
// here means the binary would fail at runtime, not at build time.
var requiredSchema = []string{
	"CREATE TABLE profiles",
	"CREATE UNIQUE INDEX profiles_name_unique",
	"CREATE TABLE config_revisions",
	"CREATE INDEX config_revisions_profile_idx",
	"CREATE TABLE settings",
	"CREATE TABLE managed_binary",
	"CREATE TABLE application_state",
}

func embeddedNames(t *testing.T) []string {
	t.Helper()
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		t.Fatalf("reading the embedded migrations: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			t.Errorf("%s is a directory, want migration files only", entry.Name())
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

func TestEmbeddedMigrationsAreOrderedAndContiguous(t *testing.T) {
	names := embeddedNames(t)
	if len(names) == 0 {
		t.Fatal("no migration files are embedded")
	}
	for i, name := range names {
		if !strings.HasSuffix(name, ".sql") {
			t.Errorf("migration %q does not end in .sql", name)
		}
		prefix, _, ok := strings.Cut(name, "_")
		if !ok {
			t.Fatalf("migration %q has no NNNN_name form", name)
		}
		version, err := strconv.Atoi(prefix)
		if err != nil {
			t.Fatalf("migration %q prefix %q is not a number: %v", name, prefix, err)
		}
		if want := i + 1; version != want {
			t.Errorf("migration %d has version %d, want %d (versions must be contiguous)", i, version, want)
		}
	}
}

func TestEmbeddedMigrationsAreReadableAndNonEmpty(t *testing.T) {
	for _, name := range embeddedNames(t) {
		content, err := FS.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if len(strings.TrimSpace(string(content))) == 0 {
			t.Errorf("%s is empty", name)
		}
	}
	if _, err := FS.ReadFile("does-not-exist.sql"); err == nil {
		t.Error("reading a missing migration succeeded, want an error")
	}
}

func TestEmbeddedSchemaCreatesEverythingTheStoreUses(t *testing.T) {
	var joined strings.Builder
	for _, name := range embeddedNames(t) {
		content, err := FS.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		joined.Write(content)
		joined.WriteString("\n")
	}
	sql := joined.String()
	for _, fragment := range requiredSchema {
		if !strings.Contains(sql, fragment) {
			t.Errorf("schema does not contain %q", fragment)
		}
	}
}

func TestMigrationsAreForwardOnly(t *testing.T) {
	// A migration that deletes data cannot be replayed safely, and the store's
	// tests rely on applying the whole set to an existing database.
	destructive := []string{"DROP TABLE", "DELETE FROM", "TRUNCATE", "ALTER TABLE profiles RENAME"}
	for _, name := range embeddedNames(t) {
		content, err := FS.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		upper := strings.ToUpper(string(content))
		for _, statement := range destructive {
			if strings.Contains(upper, statement) {
				t.Errorf("%s contains %q, want forward-only migrations", name, statement)
			}
		}
	}
}
