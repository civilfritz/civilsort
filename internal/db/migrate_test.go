package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// openV1 creates a version 1 database (the pre-participant-table schema
// plus legacy migrations) at a temp path, without migrating it further.
func openV1(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "v1.db")
	raw, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacy(raw); err != nil {
		t.Fatal(err)
	}
	return raw, path
}

func execAll(t *testing.T, db *sql.DB, stmts ...string) {
	t.Helper()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

func TestMigrateV1ToV2(t *testing.T) {
	raw, path := openV1(t)

	// alice created ballot b1 and is on b2 too; bob is anonymous on b1;
	// carol created b2 but (as in very old data) has no participant row;
	// dave added an item on b1 without a participant row.
	execAll(t, raw,
		`INSERT INTO users (id) VALUES ('alice'), ('bob'), ('carol'), ('dave')`,
		`INSERT INTO ballots (id, title, created_by) VALUES ('b1', 'One', 'alice'), ('b2', 'Two', 'carol')`,
		`INSERT INTO ballot_participants (ballot_id, user_id, participant_id, public_id, display_name) VALUES
			('b1', 'alice', 'secretA1', 'pubA1', 'Alice'),
			('b1', 'bob',   'secretB1', 'pubB1', ''),
			('b2', 'alice', 'secretA2', 'pubA2', 'Alice')`,
		`INSERT INTO items (id, ballot_id, name, added_by) VALUES
			(1, 'b1', 'Pizza', 'alice'), (2, 'b1', 'Tacos', 'bob'),
			(3, 'b1', 'Sushi', 'dave'),  (4, 'b2', 'Red', 'alice')`,
		`INSERT INTO rankings (ballot_id, user_id, item_id, position) VALUES
			('b1', 'alice', 1, 1), ('b1', 'alice', 2, 2),
			('b1', 'bob', 2, 1), ('b2', 'alice', 4, 1)`,
	)
	raw.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("user_version = %d (%v), want %d", version, err, schemaVersion)
	}

	type participant struct {
		id, secret, name, legacy string
		creator                  bool
	}
	got := map[string]participant{} // key: ballot/legacy user
	rows, err := db.Query(`SELECT ballot_id, id, COALESCE(secret, ''), display_name, is_creator, legacy_user_id FROM participants`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var ballot string
		var p participant
		if err := rows.Scan(&ballot, &p.id, &p.secret, &p.name, &p.creator, &p.legacy); err != nil {
			t.Fatal(err)
		}
		got[ballot+"/"+p.legacy] = p
	}
	rows.Close()

	want := map[string]participant{
		"b1/alice": {id: "pubA1", secret: "secretA1", name: "Alice", legacy: "alice", creator: true},
		"b1/bob":   {id: "pubB1", secret: "secretB1", name: "", legacy: "bob"},
		"b2/alice": {id: "pubA2", secret: "secretA2", name: "Alice", legacy: "alice"},
	}
	for key, w := range want {
		if g := got[key]; g != w {
			t.Errorf("%s: got %+v, want %+v", key, g, w)
		}
	}
	for _, key := range []string{"b2/carol", "b1/dave"} {
		g, ok := got[key]
		if !ok || g.id == "" || g.secret == "" {
			t.Errorf("%s: missing participant should have been created, got %+v", key, g)
		}
	}
	if !got["b2/carol"].creator {
		t.Error("carol should be b2's creator")
	}
	if len(got) != 5 {
		t.Errorf("got %d participants, want 5", len(got))
	}

	var addedBy string
	if err := db.QueryRow(`SELECT added_by FROM items WHERE id = 2`).Scan(&addedBy); err != nil || addedBy != "pubB1" {
		t.Errorf("item 2 added_by = %q (%v), want pubB1", addedBy, err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rankings WHERE ballot_id = 'b1' AND participant_id = 'pubA1'`).Scan(&n); err != nil || n != 2 {
		t.Errorf("alice's b1 rankings = %d (%v), want 2", n, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('users', 'ballot_participants') OR name LIKE '%_v1'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("old tables remain: %d (%v)", n, err)
	}

	// New items continue the AUTOINCREMENT sequence.
	var nextID int64
	if err := db.QueryRow(`INSERT INTO items (ballot_id, name, added_by) VALUES ('b1', 'New', 'pubA1') RETURNING id`).Scan(&nextID); err != nil {
		t.Fatal(err)
	}
	if nextID <= 4 {
		t.Errorf("new item id = %d, want > 4", nextID)
	}

	// Opening again is a no-op.
	db.Close()
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	db2.Close()
}

func TestDeletingBallotCascades(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	execAll(t, db,
		`INSERT INTO ballots (id) VALUES ('b')`,
		`INSERT INTO participants (ballot_id, id, secret, display_name, is_creator) VALUES ('b', 'p', 's', 'P', 1)`,
		`INSERT INTO items (id, ballot_id, name, added_by) VALUES (1, 'b', 'A', 'p')`,
		`INSERT INTO rankings (ballot_id, participant_id, item_id, position) VALUES ('b', 'p', 1, 1)`,
		`DELETE FROM ballots WHERE id = 'b'`,
	)
	for _, table := range []string{"participants", "items", "rankings"} {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s: %d rows left (%v), want 0", table, n, err)
		}
	}
}
