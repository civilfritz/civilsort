package cli

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/civilfritz/civilsort/internal/db"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func execAll(t *testing.T, database *sql.DB, stmts ...string) {
	t.Helper()
	for _, s := range stmts {
		if _, err := database.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

// seedBallot creates ballot1 with creator alice and anonymous bob.
func seedBallot(t *testing.T, database *sql.DB) {
	t.Helper()
	execAll(t, database,
		`INSERT INTO ballots (id, title) VALUES ('ballot1', 'Test Ballot')`,
		`INSERT INTO participants (ballot_id, id, secret, display_name, is_creator) VALUES
			('ballot1', 'alice', 'secretA', 'Alice', 1),
			('ballot1', 'bob',   'secretB', '',      0)`,
	)
}

func TestListBallots_Empty(t *testing.T) {
	if err := ListBallots(openDB(t)); err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestListBallots_WithData(t *testing.T) {
	database := openDB(t)
	seedBallot(t, database)
	execAll(t, database, `INSERT INTO items (ballot_id, name, added_by) VALUES ('ballot1', 'Item 1', 'alice')`)

	if err := ListBallots(database); err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestListItems_NotFound(t *testing.T) {
	err := ListItems(openDB(t), "nonexistent")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' error, got %v", err)
	}
}

func TestListItems_Empty(t *testing.T) {
	database := openDB(t)
	seedBallot(t, database)

	if err := ListItems(database, "ballot1"); err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestListItems_WithResults(t *testing.T) {
	database := openDB(t)
	seedBallot(t, database)
	execAll(t, database,
		`INSERT INTO items (id, ballot_id, name, added_by) VALUES (1, 'ballot1', 'Item A', 'alice'), (2, 'ballot1', 'Item B', 'bob')`,
		// alice prefers A, bob prefers B
		`INSERT INTO rankings (ballot_id, participant_id, item_id, position) VALUES
			('ballot1', 'alice', 1, 1), ('ballot1', 'alice', 2, 2),
			('ballot1', 'bob', 2, 1), ('ballot1', 'bob', 1, 2)`,
	)

	items, err := getItems(t.Context(), database, "ballot1")
	if err != nil {
		t.Fatal(err)
	}
	results, err := computeResults(t.Context(), database, "ballot1", items)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Rank != 1 || results[1].Rank != 1 {
		t.Errorf("Expected a two-way tie, got %+v", results)
	}

	if err := ListItems(database, "ballot1"); err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestListParticipants_NotFound(t *testing.T) {
	err := ListParticipants(openDB(t), "nonexistent")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' error, got %v", err)
	}
}

func TestListParticipants_WithData(t *testing.T) {
	database := openDB(t)
	seedBallot(t, database)
	execAll(t, database, `INSERT INTO participants (ballot_id, id, display_name, removed_at) VALUES ('ballot1', 'carol', 'Carol', datetime('now'))`)

	participants, err := getParticipants(t.Context(), database, "ballot1")
	if err != nil {
		t.Fatal(err)
	}
	if len(participants) != 3 {
		t.Fatalf("Expected 3 participants including removed, got %d", len(participants))
	}
	if !participants[0].IsCreator || !participants[2].Removed || participants[2].Secret != "" {
		t.Errorf("Unexpected participants: %+v", participants)
	}

	if err := ListParticipants(database, "ballot1"); err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}
