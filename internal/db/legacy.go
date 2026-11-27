package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/civilfritz/civilsort/internal/util"
)

// migrateLegacy brings a database created before schema versioning to
// version 1: the schema as of the global voter_id cookie, where participants
// were (ballot_id, user_id) rows in ballot_participants. It also creates that
// schema from scratch, which tests use to build version 1 databases.
func migrateLegacy(db *sql.DB) error {
	base := `
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
	if _, err := db.Exec(base); err != nil {
		return err
	}

	// Additive migrations, applied in order; errors for changes that are
	// already present are ignored.
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
		if err != nil && !strings.Contains(err.Error(), "duplicate column") &&
			!strings.Contains(err.Error(), "already exists") {
			return err
		}
	}

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

	_, err := db.Exec("PRAGMA user_version = 1")
	return err
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

// migrateV1ToV2 rebuilds the schema around per-ballot participants:
//
//   - ballot_participants becomes participants, keyed by (ballot_id, id)
//     where id is the former public_id. The former participant_id, which
//     was the participant's secret link, becomes secret, and user_id is kept
//     as legacy_user_id so existing voter_id cookies can be upgraded.
//   - items.added_by and rankings refer to participants instead of users.
//   - ballots.created_by becomes participants.is_creator.
//   - users is dropped.
//
// It follows SQLite's procedure for schema changes that alter foreign keys:
// on one connection with foreign keys off, rename the old tables, create the
// new ones, copy, verify with foreign_key_check, drop the old tables, and
// commit.
func migrateV1ToV2(db *sql.DB) (err error) {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}
	defer func() {
		if _, onErr := conn.ExecContext(ctx, "PRAGMA foreign_keys = ON"); onErr != nil && err == nil {
			err = onErr
		}
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Older data can reference users who never got a ballot_participants
	// row (items, rankings, or ballots predating that table). Give them one
	// so every reference can be mapped.
	if err := addMissingParticipants(ctx, tx); err != nil {
		return fmt.Errorf("adding missing participants: %w", err)
	}

	for _, stmt := range []string{
		`ALTER TABLE ballots RENAME TO ballots_v1`,
		`ALTER TABLE ballot_participants RENAME TO participants_v1`,
		`ALTER TABLE items RENAME TO items_v1`,
		`ALTER TABLE rankings RENAME TO rankings_v1`,
		`ALTER TABLE users RENAME TO users_v1`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}

	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("creating schema: %w", err)
	}

	copies := []string{
		`INSERT INTO ballots (id, title, is_open, rankings_private, created_at)
		 SELECT id, title, is_open, 0, created_at FROM ballots_v1`,

		`INSERT INTO participants (ballot_id, id, secret, display_name, is_creator, legacy_user_id, created_at)
		 SELECT bp.ballot_id, bp.public_id, bp.participant_id, bp.display_name,
		        bp.user_id = b.created_by, bp.user_id, bp.created_at
		 FROM participants_v1 bp JOIN ballots_v1 b ON b.id = bp.ballot_id`,

		`INSERT INTO items (id, ballot_id, name, added_by, created_at)
		 SELECT i.id, i.ballot_id, i.name, bp.public_id, i.created_at
		 FROM items_v1 i
		 JOIN participants_v1 bp ON bp.ballot_id = i.ballot_id AND bp.user_id = i.added_by
		 WHERE i.ballot_id IN (SELECT id FROM ballots)`,

		`INSERT INTO rankings (ballot_id, participant_id, item_id, position)
		 SELECT r.ballot_id, bp.public_id, r.item_id, r.position
		 FROM rankings_v1 r
		 JOIN participants_v1 bp ON bp.ballot_id = r.ballot_id AND bp.user_id = r.user_id
		 WHERE r.item_id IN (SELECT id FROM items)`,
	}
	for _, stmt := range copies {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("copying data: %w", err)
		}
	}

	// Every item and ranking on a surviving ballot must have been copied.
	checks := map[string]string{
		"items": `SELECT (SELECT COUNT(*) FROM items_v1 WHERE ballot_id IN (SELECT id FROM ballots))
		              - (SELECT COUNT(*) FROM items)`,
		"rankings": `SELECT (SELECT COUNT(*) FROM rankings_v1 WHERE item_id IN (SELECT id FROM items))
		                 - (SELECT COUNT(*) FROM rankings)`,
	}
	for table, query := range checks {
		var missing int
		if err := tx.QueryRowContext(ctx, query).Scan(&missing); err != nil {
			return err
		}
		if missing != 0 {
			return fmt.Errorf("%d %s could not be mapped to participants", missing, table)
		}
	}

	for _, table := range []string{"rankings_v1", "items_v1", "participants_v1", "ballots_v1", "users_v1"} {
		if _, err := tx.ExecContext(ctx, "DROP TABLE "+table); err != nil {
			return fmt.Errorf("dropping %s: %w", table, err)
		}
	}

	if err := checkForeignKeys(ctx, tx); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, "PRAGMA user_version = 2"); err != nil {
		return err
	}
	return tx.Commit()
}

// addMissingParticipants inserts ballot_participants rows for every
// (ballot, user) referenced by items, rankings, or ballots.created_by that
// lacks one.
func addMissingParticipants(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT ballot_id, user_id FROM (
			SELECT ballot_id, added_by AS user_id FROM items
			UNION SELECT ballot_id, user_id FROM rankings
			UNION SELECT id, created_by FROM ballots
		) refs
		WHERE NOT EXISTS (
			SELECT 1 FROM ballot_participants bp
			WHERE bp.ballot_id = refs.ballot_id AND bp.user_id = refs.user_id
		)`)
	if err != nil {
		return err
	}

	type ref struct{ ballotID, userID string }
	var missing []ref
	for rows.Next() {
		var r ref
		if err := rows.Scan(&r.ballotID, &r.userID); err != nil {
			rows.Close()
			return err
		}
		missing = append(missing, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, r := range missing {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO ballot_participants (ballot_id, user_id, participant_id, public_id) VALUES (?, ?, ?, ?)`,
			r.ballotID, r.userID, util.GenerateShortID(), util.GenerateShortID()); err != nil {
			return err
		}
	}
	return nil
}

// checkForeignKeys returns an error describing the first foreign key
// violation, if any.
func checkForeignKeys(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()

	if rows.Next() {
		var table, parent string
		var rowid sql.NullInt64
		var fkid int
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return err
		}
		return fmt.Errorf("foreign key violation: %s row %v references missing %s", table, rowid.Int64, parent)
	}
	return rows.Err()
}
