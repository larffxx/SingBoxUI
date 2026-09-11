package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/larffxx/singboxui/internal/domain/apperr"
)

// ManagedBinary is the record of the sing-box release SingBoxUI installed.
//
// It is metadata only: the authoritative check of the executable on disk is a
// version probe performed by internal/singbox. The previous binary is retained
// so a failed verification can be rolled back (spec §18).
type ManagedBinary struct {
	Version         string     `json:"version"`
	Path            string     `json:"path"`
	AssetName       string     `json:"assetName"`
	SHA256          string     `json:"sha256"`
	InstalledAt     *time.Time `json:"installedAt,omitempty"`
	PreviousPath    string     `json:"previousPath,omitempty"`
	PreviousVersion string     `json:"previousVersion,omitempty"`
}

// Empty reports whether nothing has been installed yet.
func (m ManagedBinary) Empty() bool { return m.Path == "" && m.Version == "" }

// GetManagedBinary returns the recorded managed binary, or a zero value.
func (s *Store) GetManagedBinary(ctx context.Context) (ManagedBinary, error) {
	const op = "storage.GetManagedBinary"
	var (
		out         ManagedBinary
		installedAt sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT version, path, asset_name, sha256, installed_at, previous_path, previous_version
		 FROM managed_binary WHERE id = 1`).
		Scan(&out.Version, &out.Path, &out.AssetName, &out.SHA256, &installedAt, &out.PreviousPath, &out.PreviousVersion)
	if err != nil {
		if isNoRows(err) {
			return ManagedBinary{}, nil
		}
		return ManagedBinary{}, apperr.Wrap(apperr.CodeDatabaseError, op, "cannot read managed binary record", err)
	}
	out.InstalledAt = nullableTime(installedAt.String)
	return out, nil
}

// SaveManagedBinary upserts the single managed-binary row.
func (s *Store) SaveManagedBinary(ctx context.Context, value ManagedBinary) error {
	const op = "storage.SaveManagedBinary"
	var installedAt any
	if value.InstalledAt != nil {
		installedAt = formatTime(*value.InstalledAt)
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO managed_binary (id, version, path, asset_name, sha256, installed_at, previous_path, previous_version)
		 VALUES (1, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET
		   version = excluded.version, path = excluded.path, asset_name = excluded.asset_name,
		   sha256 = excluded.sha256, installed_at = excluded.installed_at,
		   previous_path = excluded.previous_path, previous_version = excluded.previous_version`,
		value.Version, value.Path, value.AssetName, value.SHA256, installedAt, value.PreviousPath, value.PreviousVersion); err != nil {
		return apperr.Wrap(apperr.CodeDatabaseError, op, "cannot write managed binary record", err)
	}
	return nil
}
