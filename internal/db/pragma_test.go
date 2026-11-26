package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// Pragmas such as foreign_keys are per connection, so every pooled
// connection must have them, not just the first.
func TestOpen_PragmasOnEveryConnection(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	ctx := context.Background()
	conns := make([]*sql.Conn, 3)
	for i := range conns {
		conn, err := database.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		conns[i] = conn
	}

	for i, conn := range conns {
		for pragma, want := range map[string]int{"foreign_keys": 1, "busy_timeout": busyTimeoutMS} {
			var got int
			if err := conn.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("connection %d: %s = %d, want %d", i, pragma, got, want)
			}
		}
	}
}

func TestOpen_DeletingItemCascadesToRankings(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	// Hold a connection so later statements run on a different one.
	ctx := context.Background()
	held, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	for _, stmt := range []string{
		`INSERT INTO users (id) VALUES ('u')`,
		`INSERT INTO ballots (id, created_by) VALUES ('b', 'u')`,
		`INSERT INTO items (id, ballot_id, name, added_by) VALUES (1, 'b', 'A', 'u')`,
		`INSERT INTO rankings (ballot_id, user_id, item_id, position) VALUES ('b', 'u', 1, 1)`,
		`DELETE FROM items WHERE id = 1`,
	} {
		if _, err := database.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	var n int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM rankings`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("rankings after deleting item: got %d, want 0", n)
	}
}
