package db

import (
	"database/sql"
	"strings"

	"github.com/civilfritz/civilsort/internal/util"
	_ "modernc.org/sqlite"
)

// Open opens the SQLite database and applies pragmas.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}

	// Enable foreign key constraints
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, err
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
	}

	for _, m := range migrations {
		_, err := db.Exec(m)
		// Ignore "duplicate column" or "table already exists" errors
		if err != nil && !strings.Contains(err.Error(), "duplicate column") &&
			!strings.Contains(err.Error(), "already exists") {
			return err
		}
	}

	// Backfill participant_id for existing rows BEFORE creating the unique index
	if err := backfillParticipantIDs(db); err != nil {
		return err
	}

	// Create index for participant_id lookups (after backfill ensures no empty values)
	_, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_bp_participant ON ballot_participants(ballot_id, participant_id) WHERE participant_id != ''`)
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}

	return nil
}

// backfillParticipantIDs generates participant IDs for existing ballot_participants rows
func backfillParticipantIDs(db *sql.DB) error {
	rows, err := db.Query(`SELECT ballot_id, user_id FROM ballot_participants WHERE participant_id = ''`)
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

	// Update each row with a generated participant_id
	stmt, err := db.Prepare(`UPDATE ballot_participants SET participant_id = ? WHERE ballot_id = ? AND user_id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, p := range participants {
		participantID := util.GenerateShortID()
		if _, err := stmt.Exec(participantID, p.ballotID, p.userID); err != nil {
			return err
		}
	}

	return nil
}
