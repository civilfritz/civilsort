package db

import (
	"os"
	"testing"
)

func TestOpen_CreatesTablesSuccessfully(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	// Verify all expected tables exist
	expectedTables := []string{"users", "ballots", "items", "rankings", "ballot_participants"}
	for _, table := range expectedTables {
		var name string
		err := database.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("Table %s not found: %v", table, err)
		}
	}
}

func TestOpen_IdempotentMigrations(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	// Open once
	db1, err := Open(tmpfile.Name())
	if err != nil {
		t.Fatalf("First open failed: %v", err)
	}
	db1.Close()

	// Open again - migrations should be idempotent
	db2, err := Open(tmpfile.Name())
	if err != nil {
		t.Fatalf("Second open failed: %v", err)
	}
	defer db2.Close()
}

func TestOpen_WALMode(t *testing.T) {
	// WAL mode doesn't work with :memory: databases, so use a temp file
	tmpfile, err := os.CreateTemp("", "test-wal-*.db")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	database, err := Open(tmpfile.Name())
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	var journalMode string
	err = database.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatalf("Failed to get journal mode: %v", err)
	}

	if journalMode != "wal" {
		t.Errorf("Expected WAL mode, got %s", journalMode)
	}
}

func TestOpen_ForeignKeysEnabled(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	var fkEnabled int
	err = database.QueryRow("PRAGMA foreign_keys").Scan(&fkEnabled)
	if err != nil {
		t.Fatalf("Failed to get foreign_keys setting: %v", err)
	}

	if fkEnabled != 1 {
		t.Errorf("Expected foreign keys enabled (1), got %d", fkEnabled)
	}
}
