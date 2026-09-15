package sqlite

import (
	"context"
	"database/sql"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/settings"
)

// settingsKey is the single row holding the settings envelope.
const settingsKey = "runtime"

// LoadSettings returns the persisted settings, or defaults when none are stored.
func (s *Store) LoadSettings(ctx context.Context) (settings.Settings, error) {
	const op = "storage.LoadSettings"
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, settingsKey).Scan(&raw)
	if err != nil {
		if isNoRows(err) {
			return settings.Default(), nil
		}
		return settings.Settings{}, apperr.Wrap(apperr.CodeDatabaseError, op, "cannot read settings", err)
	}
	loaded, err := settings.Unmarshal(raw)
	if err != nil {
		// A corrupted blob must not brick the application: fall back to defaults
		// and let the caller decide whether to surface it.
		return settings.Default(), apperr.Wrap(apperr.CodeDatabaseError, op, "stored settings are unreadable", err)
	}
	return loaded, nil
}

// SaveSettings validates and persists settings.
func (s *Store) SaveSettings(ctx context.Context, value settings.Settings) error {
	const op = "storage.SaveSettings"
	value = value.WithDefaults()
	if err := value.Validate(); err != nil {
		return apperr.Wrap(apperr.CodeInvalidArgument, op, "invalid settings", err)
	}
	raw, err := settings.Marshal(value)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, op, "cannot encode settings", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
		settingsKey, raw); err != nil {
		return apperr.Wrap(apperr.CodeDatabaseError, op, "cannot write settings", err)
	}
	return nil
}

// GetState reads a value from the small application-state table.
func (s *Store) GetState(ctx context.Context, key string) (string, bool, error) {
	const op = "storage.GetState"
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM application_state WHERE key = ?`, key).Scan(&value)
	if err != nil {
		if isNoRows(err) {
			return "", false, nil
		}
		return "", false, apperr.Wrap(apperr.CodeDatabaseError, op, "cannot read application state", err)
	}
	return value, true, nil
}

// SetState writes a value into the application-state table.
func (s *Store) SetState(ctx context.Context, key, value string) error {
	const op = "storage.SetState"
	if key == "" {
		return apperr.New(apperr.CodeInvalidArgument, op, "state key is required")
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO application_state (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
		key, value); err != nil {
		return apperr.Wrap(apperr.CodeDatabaseError, op, "cannot write application state", err)
	}
	return nil
}

// DeleteState removes a state key; missing keys are not an error.
func (s *Store) DeleteState(ctx context.Context, key string) error {
	const op = "storage.DeleteState"
	if _, err := s.db.ExecContext(ctx, `DELETE FROM application_state WHERE key = ?`, key); err != nil {
		return apperr.Wrap(apperr.CodeDatabaseError, op, "cannot delete application state", err)
	}
	return nil
}

var _ = sql.ErrNoRows
