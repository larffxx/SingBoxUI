package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/profile"
)

const profileColumns = `id, name, description, created_at, updated_at, active_revision_id, last_used_at`

// CreateProfile inserts a profile together with its initial revision in one
// transaction, so metadata is never partially written (spec §64).
func (s *Store) CreateProfile(ctx context.Context, p profile.Profile, initial profile.Revision) error {
	const op = "storage.CreateProfile"
	if p.ID == "" || initial.ID == "" {
		return apperr.New(apperr.CodeInvalidArgument, op, "profile and revision ids are required")
	}
	initial.ProfileID = p.ID
	return s.withTx(ctx, op, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO profiles (`+profileColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			p.ID, p.Name, p.Description, formatTime(p.CreatedAt), formatTime(p.UpdatedAt),
			nullString(p.ActiveRevisionID), nullableTimeString(p.LastUsedAt)); err != nil {
			return mapProfileError(err, op)
		}
		return insertRevision(ctx, tx, initial)
	})
}

func nullableTimeString(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

// ListProfiles returns all profiles ordered by name.
func (s *Store) ListProfiles(ctx context.Context) ([]profile.Profile, error) {
	const op = "storage.ListProfiles"
	rows, err := s.db.QueryContext(ctx, `SELECT `+profileColumns+` FROM profiles ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabaseError, op, "cannot list profiles", err)
	}
	defer rows.Close()
	var out []profile.Profile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, apperr.Wrap(apperr.CodeDatabaseError, op, "cannot read profile", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabaseError, op, "cannot list profiles", err)
	}
	return out, nil
}

// GetProfile returns one profile.
func (s *Store) GetProfile(ctx context.Context, id string) (profile.Profile, error) {
	const op = "storage.GetProfile"
	row := s.db.QueryRowContext(ctx, `SELECT `+profileColumns+` FROM profiles WHERE id = ?`, id)
	p, err := scanProfile(row)
	if err != nil {
		if isNoRows(err) {
			return profile.Profile{}, apperr.Newf(apperr.CodeProfileNotFound, op, "profile %s does not exist", id)
		}
		return profile.Profile{}, apperr.Wrap(apperr.CodeDatabaseError, op, "cannot read profile", err)
	}
	return p, nil
}

// UpdateProfile writes name/description/updatedAt.
func (s *Store) UpdateProfile(ctx context.Context, p profile.Profile) error {
	const op = "storage.UpdateProfile"
	res, err := s.db.ExecContext(ctx,
		`UPDATE profiles SET name = ?, description = ?, updated_at = ? WHERE id = ?`,
		p.Name, p.Description, formatTime(p.UpdatedAt), p.ID)
	if err != nil {
		return mapProfileError(err, op)
	}
	return requireRows(res, op, p.ID)
}

// DeleteProfile removes a profile and, by cascade, its revisions.
func (s *Store) DeleteProfile(ctx context.Context, id string) error {
	const op = "storage.DeleteProfile"
	res, err := s.db.ExecContext(ctx, `DELETE FROM profiles WHERE id = ?`, id)
	if err != nil {
		return apperr.Wrap(apperr.CodeDatabaseError, op, "cannot delete profile", err)
	}
	return requireRows(res, op, id)
}

// SetActiveRevision points a profile at a revision.
func (s *Store) SetActiveRevision(ctx context.Context, profileID, revisionID string) error {
	const op = "storage.SetActiveRevision"
	res, err := s.db.ExecContext(ctx,
		`UPDATE profiles SET active_revision_id = ?, updated_at = ? WHERE id = ?`,
		nullString(revisionID), formatTime(nowUTC()), profileID)
	if err != nil {
		return apperr.Wrap(apperr.CodeDatabaseError, op, "cannot set active revision", err)
	}
	return requireRows(res, op, profileID)
}

// TouchLastUsed records that a profile was started.
func (s *Store) TouchLastUsed(ctx context.Context, profileID string) error {
	const op = "storage.TouchLastUsed"
	res, err := s.db.ExecContext(ctx,
		`UPDATE profiles SET last_used_at = ? WHERE id = ?`, formatTime(nowUTC()), profileID)
	if err != nil {
		return apperr.Wrap(apperr.CodeDatabaseError, op, "cannot update last-used time", err)
	}
	return requireRows(res, op, profileID)
}

func scanProfile(scanner interface{ Scan(...any) error }) (profile.Profile, error) {
	var (
		p          profile.Profile
		createdAt  string
		updatedAt  string
		activeRev  sql.NullString
		lastUsedAt sql.NullString
	)
	if err := scanner.Scan(&p.ID, &p.Name, &p.Description, &createdAt, &updatedAt, &activeRev, &lastUsedAt); err != nil {
		return profile.Profile{}, err
	}
	p.CreatedAt = parseTime(createdAt)
	p.UpdatedAt = parseTime(updatedAt)
	p.ActiveRevisionID = activeRev.String
	p.LastUsedAt = nullableTime(lastUsedAt.String)
	return p, nil
}

func mapProfileError(err error, op string) error {
	// SQLite reports constraint violations as TEXT errors; name uniqueness is the
	// only expected one here.
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return apperr.Wrap(apperr.CodeProfileNameConflict, op, "a profile with this name already exists", err)
	}
	return apperr.Wrap(apperr.CodeDatabaseError, op, "database error", err)
}

func requireRows(res sql.Result, op, id string) error {
	affected, err := res.RowsAffected()
	if err != nil {
		return apperr.Wrap(apperr.CodeDatabaseError, op, "cannot read affected rows", err)
	}
	if affected == 0 {
		return apperr.Newf(apperr.CodeProfileNotFound, op, "profile %s does not exist", id)
	}
	return nil
}

func nowUTC() time.Time { return time.Now().UTC() }
