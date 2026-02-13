package db

import (
	"database/sql"
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
	return err
}
