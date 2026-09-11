// Package sqlite is the metadata store: profiles, immutable revisions, settings,
// the managed binary record and small application state.
//
// It contains no business rules — transaction boundaries requested by the
// application layer are honoured here, nothing more.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver: single binary on macOS/Windows

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/storage/migrations"
)

// Store is the SQLite-backed metadata store.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path and applies migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabaseError, "storage.Open", "cannot create database directory", err)
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabaseError, "storage.Open", "cannot open database", err)
	}
	// SQLite serialises writers; a single connection keeps error handling simple
	// and deterministic for a single-user desktop application.
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)

	store := &Store{db: db}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, apperr.Wrap(apperr.CodeDatabaseError, "storage.Open", "cannot reach database", err)
	}
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

// Close releases the database handle.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// DB exposes the raw handle for tests.
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) migrate(ctx context.Context) error {
	const op = "storage.migrate"
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return apperr.Wrap(apperr.CodeDatabaseError, op, "cannot create migration table", err)
	}

	applied := map[int]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return apperr.Wrap(apperr.CodeDatabaseError, op, "cannot read applied migrations", err)
	}
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			rows.Close()
			return apperr.Wrap(apperr.CodeDatabaseError, op, "cannot read applied migrations", err)
		}
		applied[version] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return apperr.Wrap(apperr.CodeDatabaseError, op, "cannot read applied migrations", err)
	}

	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, op, "cannot read embedded migrations", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		version, err := migrationVersion(name)
		if err != nil {
			return apperr.Wrap(apperr.CodeInternal, op, "invalid migration filename", err)
		}
		if applied[version] {
			continue
		}
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			return apperr.Wrap(apperr.CodeInternal, op, "cannot read migration", err)
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return apperr.Wrap(apperr.CodeDatabaseError, op, "cannot begin migration transaction", err)
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			_ = tx.Rollback()
			return apperr.Wrap(apperr.CodeDatabaseError, op, fmt.Sprintf("migration %s failed", name), err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
			version, name, formatTime(time.Now())); err != nil {
			_ = tx.Rollback()
			return apperr.Wrap(apperr.CodeDatabaseError, op, "cannot record migration", err)
		}
		if err := tx.Commit(); err != nil {
			return apperr.Wrap(apperr.CodeDatabaseError, op, "cannot commit migration", err)
		}
	}
	return nil
}

func migrationVersion(name string) (int, error) {
	idx := strings.Index(name, "_")
	if idx <= 0 {
		return 0, fmt.Errorf("migration %q must start with a numeric version", name)
	}
	var version int
	if _, err := fmt.Sscanf(name[:idx], "%d", &version); err != nil {
		return 0, fmt.Errorf("migration %q must start with a numeric version", name)
	}
	return version, nil
}

// withTx runs fn inside a transaction, rolling back on error or panic.
func (s *Store) withTx(ctx context.Context, op string, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return apperr.Wrap(apperr.CodeDatabaseError, op, "cannot begin transaction", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return apperr.Wrap(apperr.CodeDatabaseError, op, "cannot commit transaction", err)
	}
	return nil
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return t
}

func nullableTime(value string) *time.Time {
	if value == "" {
		return nil
	}
	t := parseTime(value)
	if t.IsZero() {
		return nil
	}
	return &t
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
