package sqlite

import (
	"context"
	"database/sql"

	"github.com/larffxx/singboxui/internal/domain/apperr"
	"github.com/larffxx/singboxui/internal/domain/profile"
)

const revisionColumns = `id, profile_id, parent_revision_id, created_at, source, config_json,
	structural_validation_status, singbox_validation_status, singbox_version, comment`

// insertRevision writes an immutable revision row. It runs on the caller's
// transaction so a profile and its first revision are created atomically.
func insertRevision(ctx context.Context, tx *sql.Tx, rev profile.Revision) error {
	const op = "storage.insertRevision"
	if rev.ID == "" || rev.ProfileID == "" {
		return apperr.New(apperr.CodeInvalidArgument, op, "revision id and profile id are required")
	}
	if !rev.Source.Valid() {
		return apperr.Newf(apperr.CodeInvalidArgument, op, "unknown revision source %q", rev.Source)
	}
	if rev.StructuralValidation == "" {
		rev.StructuralValidation = profile.StatusUnknown
	}
	if rev.SingBoxValidation == "" {
		rev.SingBoxValidation = profile.StatusUnknown
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO config_revisions (`+revisionColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rev.ID, rev.ProfileID, nullString(rev.ParentRevisionID), formatTime(rev.CreatedAt), string(rev.Source),
		rev.ConfigJSON, string(rev.StructuralValidation), string(rev.SingBoxValidation), rev.SingBoxVersion, rev.Comment); err != nil {
		return apperr.Wrap(apperr.CodeDatabaseError, op, "cannot insert revision", err)
	}
	return nil
}

// CreateRevision appends a revision and, when makeActive is set, moves the
// profile's active pointer in the same transaction.
func (s *Store) CreateRevision(ctx context.Context, rev profile.Revision, makeActive bool) error {
	const op = "storage.CreateRevision"
	return s.withTx(ctx, op, func(tx *sql.Tx) error {
		if err := insertRevision(ctx, tx, rev); err != nil {
			return err
		}
		if !makeActive {
			return nil
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE profiles SET active_revision_id = ?, updated_at = ? WHERE id = ?`,
			rev.ID, formatTime(nowUTC()), rev.ProfileID)
		if err != nil {
			return apperr.Wrap(apperr.CodeDatabaseError, op, "cannot mark revision active", err)
		}
		return requireRows(res, op, rev.ProfileID)
	})
}

// GetRevision returns one revision of one profile.
func (s *Store) GetRevision(ctx context.Context, profileID, revisionID string) (profile.Revision, error) {
	const op = "storage.GetRevision"
	row := s.db.QueryRowContext(ctx,
		`SELECT `+revisionColumns+` FROM config_revisions WHERE id = ? AND profile_id = ?`, revisionID, profileID)
	rev, err := scanRevision(row)
	if err != nil {
		if isNoRows(err) {
			return profile.Revision{}, apperr.Newf(apperr.CodeRevisionNotFound, op, "revision %s does not exist", revisionID)
		}
		return profile.Revision{}, apperr.Wrap(apperr.CodeDatabaseError, op, "cannot read revision", err)
	}
	return rev, nil
}

// ListRevisions returns a profile's revisions, newest first.
func (s *Store) ListRevisions(ctx context.Context, profileID string, limit int) ([]profile.Revision, error) {
	const op = "storage.ListRevisions"
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+revisionColumns+` FROM config_revisions WHERE profile_id = ? ORDER BY created_at DESC, rowid DESC LIMIT ?`,
		profileID, limit)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabaseError, op, "cannot list revisions", err)
	}
	defer rows.Close()
	out := make([]profile.Revision, 0, 16)
	for rows.Next() {
		rev, err := scanRevision(rows)
		if err != nil {
			return nil, apperr.Wrap(apperr.CodeDatabaseError, op, "cannot read revision", err)
		}
		out = append(out, rev)
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.Wrap(apperr.CodeDatabaseError, op, "cannot list revisions", err)
	}
	return out, nil
}

// ActiveRevision returns the revision a profile currently points at.
func (s *Store) ActiveRevision(ctx context.Context, profileID string) (profile.Revision, error) {
	const op = "storage.ActiveRevision"
	p, err := s.GetProfile(ctx, profileID)
	if err != nil {
		return profile.Revision{}, err
	}
	if p.ActiveRevisionID == "" {
		return profile.Revision{}, apperr.Newf(apperr.CodeRevisionNotFound, op, "profile %s has no active revision", profileID)
	}
	return s.GetRevision(ctx, profileID, p.ActiveRevisionID)
}

// SetRevisionValidation records validation metadata. The configuration bytes of
// a revision are never rewritten (spec §12); only the validation outcome and the
// sing-box version that produced it are updated.
func (s *Store) SetRevisionValidation(ctx context.Context, revisionID string, structural, singbox profile.ValidationStatus, version string) error {
	const op = "storage.SetRevisionValidation"
	res, err := s.db.ExecContext(ctx,
		`UPDATE config_revisions SET structural_validation_status = ?, singbox_validation_status = ?, singbox_version = ? WHERE id = ?`,
		string(structural), string(singbox), version, revisionID)
	if err != nil {
		return apperr.Wrap(apperr.CodeDatabaseError, op, "cannot record validation status", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return apperr.Wrap(apperr.CodeDatabaseError, op, "cannot read affected rows", err)
	}
	if affected == 0 {
		return apperr.Newf(apperr.CodeRevisionNotFound, op, "revision %s does not exist", revisionID)
	}
	return nil
}

// MarkRevisionsStale flags every revision that was validated by a different
// sing-box version than the one now installed (spec §38).
func (s *Store) MarkRevisionsStale(ctx context.Context, version string) (int64, error) {
	const op = "storage.MarkRevisionsStale"
	res, err := s.db.ExecContext(ctx,
		`UPDATE config_revisions SET singbox_validation_status = ?
		 WHERE singbox_validation_status = ? AND singbox_version <> ? AND singbox_version <> ''`,
		string(profile.StatusStale), string(profile.StatusPassed), version)
	if err != nil {
		return 0, apperr.Wrap(apperr.CodeDatabaseError, op, "cannot mark revisions stale", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, apperr.Wrap(apperr.CodeDatabaseError, op, "cannot read affected rows", err)
	}
	return affected, nil
}

// CountRevisions returns how many revisions a profile owns; used by the delete
// constraint checks and by tests.
func (s *Store) CountRevisions(ctx context.Context, profileID string) (int, error) {
	const op = "storage.CountRevisions"
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM config_revisions WHERE profile_id = ?`, profileID).Scan(&n); err != nil {
		return 0, apperr.Wrap(apperr.CodeDatabaseError, op, "cannot count revisions", err)
	}
	return n, nil
}

func scanRevision(scanner interface{ Scan(...any) error }) (profile.Revision, error) {
	var (
		rev            profile.Revision
		createdAt      string
		source         string
		structural     string
		singbox        string
		parentRevision sql.NullString
	)
	if err := scanner.Scan(&rev.ID, &rev.ProfileID, &parentRevision, &createdAt, &source, &rev.ConfigJSON,
		&structural, &singbox, &rev.SingBoxVersion, &rev.Comment); err != nil {
		return profile.Revision{}, err
	}
	rev.ParentRevisionID = parentRevision.String
	rev.CreatedAt = parseTime(createdAt)
	rev.Source = profile.Source(source)
	rev.StructuralValidation = profile.ValidationStatus(structural)
	rev.SingBoxValidation = profile.ValidationStatus(singbox)
	return rev, nil
}
