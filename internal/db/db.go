package db

import (
	"database/sql"
	"fmt"
	"net/url"

	_ "modernc.org/sqlite"
)

// busyTimeoutMS is how long a write waits for a lock before failing.
const busyTimeoutMS = 5000

// schemaVersion is stored in PRAGMA user_version.
const schemaVersion = 2

// schema is the current database schema, used for new databases and as the
// target of migrateV1ToV2.
//
// A participant belongs to one ballot and is identified by (ballot_id, id).
// The id is safe to show to others; secret is the credential held in the
// participant's cookie and join link. Removed participants keep their row
// (so their kept items and rankings still reference it) with secret NULL.
const schema = `
CREATE TABLE ballots (
	id               TEXT PRIMARY KEY,
	title            TEXT NOT NULL DEFAULT '',
	is_open          INTEGER NOT NULL DEFAULT 1,
	rankings_private INTEGER NOT NULL DEFAULT 0,
	created_at       DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE participants (
	ballot_id      TEXT NOT NULL REFERENCES ballots(id) ON DELETE CASCADE,
	id             TEXT NOT NULL,
	secret         TEXT,
	display_name   TEXT NOT NULL DEFAULT '',
	is_creator     INTEGER NOT NULL DEFAULT 0,
	removed_at     DATETIME,
	legacy_user_id TEXT, -- TODO(v1.0): drop with the voter_id fallback in handler.currentParticipant
	created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
	PRIMARY KEY (ballot_id, id),
	UNIQUE (ballot_id, secret)
);

CREATE UNIQUE INDEX idx_participants_legacy_user
	ON participants(ballot_id, legacy_user_id) WHERE legacy_user_id IS NOT NULL;

CREATE TABLE items (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	ballot_id  TEXT NOT NULL REFERENCES ballots(id) ON DELETE CASCADE,
	name       TEXT NOT NULL,
	added_by   TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT (datetime('now')),
	UNIQUE (ballot_id, name),
	FOREIGN KEY (ballot_id, added_by) REFERENCES participants(ballot_id, id)
);

CREATE TABLE rankings (
	ballot_id      TEXT    NOT NULL REFERENCES ballots(id) ON DELETE CASCADE,
	participant_id TEXT    NOT NULL,
	item_id        INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
	position       INTEGER NOT NULL,
	PRIMARY KEY (ballot_id, participant_id, item_id),
	FOREIGN KEY (ballot_id, participant_id) REFERENCES participants(ballot_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_rankings_item ON rankings(item_id);
`

// Open opens the SQLite database, applies pragmas, and migrates it to the
// current schema.
func Open(path string) (*sql.DB, error) {
	// Pragmas go in the DSN so the driver applies them to every pooled
	// connection; foreign_keys and busy_timeout are per-connection settings.
	dsn := path + "?" + url.Values{"_pragma": {
		"foreign_keys(1)",
		fmt.Sprintf("busy_timeout(%d)", busyTimeoutMS),
		"journal_mode(WAL)",
	}}.Encode()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	// Each connection to :memory: is a separate empty database.
	if path == ":memory:" {
		db.SetMaxOpenConns(1)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	return db, nil
}

// migrate brings the database to schemaVersion.
func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version >= schemaVersion {
		return nil
	}

	var hasBallots bool
	if err := db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'ballots')",
	).Scan(&hasBallots); err != nil {
		return err
	}

	if !hasBallots {
		if _, err := db.Exec(schema); err != nil {
			return err
		}
		_, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion))
		return err
	}

	// Databases from before versioning are at version 0.
	if version < 1 {
		if err := migrateLegacy(db); err != nil {
			return fmt.Errorf("legacy migrations: %w", err)
		}
	}
	return migrateV1ToV2(db)
}
