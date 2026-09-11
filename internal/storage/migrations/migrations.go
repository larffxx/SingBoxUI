// Package migrations embeds the SQLite schema migrations.
//
// Migrations are applied in filename order and recorded in schema_migrations;
// they must be forward-only and idempotent-by-version.
package migrations

import "embed"

// FS holds the embedded .sql migration files.
//
//go:embed *.sql
var FS embed.FS
