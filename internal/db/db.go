package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	"github.com/civilfritz/civilsort/internal/util"
	_ "modernc.org/sqlite"
)

// busyTimeoutMS is how long a write waits for a lock before failing.
const busyTimeoutMS = 5000

// Open opens the SQLite database and applies pragmas.
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

	// Run migrations
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

// migrate creates all tables if they don't exist.
func migrate(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id         TEXT PRIMARY KEY,
		created_at DATETIME NOT NULL DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS ballots (
		id         TEXT PRIMARY KEY,
		title      TEXT NOT NULL DEFAULT '',
		is_open    INTEGER NOT NULL DEFAULT 1,
		created_by TEXT NOT NULL REFERENCES users(id),
		created_at DATETIME NOT NULL DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS items (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		ballot_id  TEXT NOT NULL REFERENCES ballots(id) ON DELETE CASCADE,
		name       TEXT NOT NULL,
		added_by   TEXT NOT NULL REFERENCES users(id),
		created_at DATETIME NOT NULL DEFAULT (datetime('now')),
		UNIQUE(ballot_id, name)
	);

	CREATE TABLE IF NOT EXISTS rankings (
		ballot_id TEXT    NOT NULL REFERENCES ballots(id) ON DELETE CASCADE,
		user_id   TEXT    NOT NULL REFERENCES users(id),
		item_id   INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
		position  INTEGER NOT NULL,
		PRIMARY KEY (ballot_id, user_id, item_id)
	);
	`

	_, err := db.Exec(schema)
	if err != nil {
		return err
	}

	// Additive migrations for existing databases
	migrations := []string{
		`ALTER TABLE ballots ADD COLUMN is_open INTEGER NOT NULL DEFAULT 1`,
		`CREATE TABLE IF NOT EXISTS ballot_participants (
			ballot_id  TEXT NOT NULL REFERENCES ballots(id) ON DELETE CASCADE,
			user_id    TEXT NOT NULL REFERENCES users(id),
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			PRIMARY KEY (ballot_id, user_id)
		)`,
		`ALTER TABLE users ADD COLUMN display_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE ballot_participants ADD COLUMN participant_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE ballot_participants ADD COLUMN display_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE ballot_participants ADD COLUMN public_id TEXT NOT NULL DEFAULT ''`,
	}

	for _, m := range migrations {
		_, err := db.Exec(m)
		// Ignore "duplicate column" or "table already exists" errors
		if err != nil && !strings.Contains(err.Error(), "duplicate column") &&
			!strings.Contains(err.Error(), "already exists") {
			return err
		}
	}

	// Backfill IDs for existing rows BEFORE creating the unique indexes.
	// participant_id is the participant's private re-join link; public_id is
	// what other participants see.
	for _, column := range []string{"participant_id", "public_id"} {
		if err := backfillIDs(db, column); err != nil {
			return err
		}
	}

	indexes := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_bp_participant ON ballot_participants(ballot_id, participant_id) WHERE participant_id != ''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_bp_public ON ballot_participants(ballot_id, public_id) WHERE public_id != ''`,
	}
	for _, idx := range indexes {
		if _, err := db.Exec(idx); err != nil {
			return err
		}
	}

	// Remove rankings orphaned while foreign keys were only enabled on one
	// pooled connection.
	if _, err := db.Exec(`DELETE FROM rankings WHERE item_id NOT IN (SELECT id FROM items)`); err != nil {
		return err
	}

	return nil
}

// backfillIDs generates random IDs for ballot_participants rows where column
// is empty. column must be a trusted identifier.
func backfillIDs(db *sql.DB, column string) error {
	rows, err := db.Query(`SELECT ballot_id, user_id FROM ballot_participants WHERE ` + column + ` = ''`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type participant struct {
		ballotID string
		userID   string
	}

	var participants []participant
	for rows.Next() {
		var p participant
		if err := rows.Scan(&p.ballotID, &p.userID); err != nil {
			return err
		}
		participants = append(participants, p)
	}

	if err := rows.Err(); err != nil {
		return err
	}

	stmt, err := db.Prepare(`UPDATE ballot_participants SET ` + column + ` = ? WHERE ballot_id = ? AND user_id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, p := range participants {
		if _, err := stmt.Exec(util.GenerateShortID(), p.ballotID, p.userID); err != nil {
			return err
		}
	}

	return nil
}
