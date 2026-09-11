-- Initial schema: profiles, immutable configuration revisions, typed settings,
-- the managed sing-box binary record and small application state.
--
-- Runtime process state is deliberately absent: it is reconciled from the real
-- process, never persisted as authoritative truth.

CREATE TABLE profiles (
    id                 TEXT PRIMARY KEY,
    name               TEXT NOT NULL,
    description        TEXT NOT NULL DEFAULT '',
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    active_revision_id TEXT,
    last_used_at       TEXT
);

CREATE UNIQUE INDEX profiles_name_unique ON profiles (name COLLATE NOCASE);

CREATE TABLE config_revisions (
    id                             TEXT PRIMARY KEY,
    profile_id                     TEXT NOT NULL REFERENCES profiles (id) ON DELETE CASCADE,
    parent_revision_id             TEXT,
    created_at                     TEXT NOT NULL,
    source                         TEXT NOT NULL,
    config_json                    TEXT NOT NULL,
    structural_validation_status   TEXT NOT NULL DEFAULT 'unknown',
    singbox_validation_status      TEXT NOT NULL DEFAULT 'unknown',
    singbox_version                TEXT NOT NULL DEFAULT '',
    comment                        TEXT NOT NULL DEFAULT ''
);

CREATE INDEX config_revisions_profile_idx ON config_revisions (profile_id, created_at DESC);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE managed_binary (
    id               INTEGER PRIMARY KEY CHECK (id = 1),
    version          TEXT NOT NULL DEFAULT '',
    path             TEXT NOT NULL DEFAULT '',
    asset_name       TEXT NOT NULL DEFAULT '',
    sha256           TEXT NOT NULL DEFAULT '',
    installed_at     TEXT,
    previous_path    TEXT NOT NULL DEFAULT '',
    previous_version TEXT NOT NULL DEFAULT ''
);

CREATE TABLE application_state (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
