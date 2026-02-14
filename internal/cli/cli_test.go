package cli

import (
	"strings"
	"testing"

	"github.com/civilfritz/civilsort/internal/db"
)

func TestListBallots_Empty(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	err = listBallots(database)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestListBallots_WithData(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	// Create a user
	_, err = database.Exec("INSERT INTO users (id) VALUES (?)", "user1")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Create a ballot
	_, err = database.Exec("INSERT INTO ballots (id, title, created_by) VALUES (?, ?, ?)",
		"ballot1", "Test Ballot", "user1")
	if err != nil {
		t.Fatalf("Failed to create ballot: %v", err)
	}

	// Add a participant
	_, err = database.Exec("INSERT INTO ballot_participants (ballot_id, user_id, participant_id) VALUES (?, ?, ?)",
		"ballot1", "user1", "part1")
	if err != nil {
		t.Fatalf("Failed to add participant: %v", err)
	}

	// Add an item
	_, err = database.Exec("INSERT INTO items (ballot_id, name, added_by) VALUES (?, ?, ?)",
		"ballot1", "Item 1", "user1")
	if err != nil {
		t.Fatalf("Failed to add item: %v", err)
	}

	err = listBallots(database)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestListItems_NotFound(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	err = listItems(database, "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent ballot, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' error, got %v", err)
	}
}

func TestListItems_Empty(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	// Create a user and ballot
	_, err = database.Exec("INSERT INTO users (id) VALUES (?)", "user1")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	_, err = database.Exec("INSERT INTO ballots (id, title, created_by) VALUES (?, ?, ?)",
		"ballot1", "Test", "user1")
	if err != nil {
		t.Fatalf("Failed to create ballot: %v", err)
	}

	err = listItems(database, "ballot1")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestListItems_WithResults(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	// Create users
	_, err = database.Exec("INSERT INTO users (id) VALUES (?), (?)", "user1", "user2")
	if err != nil {
		t.Fatalf("Failed to create users: %v", err)
	}

	// Create ballot
	_, err = database.Exec("INSERT INTO ballots (id, title, created_by) VALUES (?, ?, ?)",
		"ballot1", "Test", "user1")
	if err != nil {
		t.Fatalf("Failed to create ballot: %v", err)
	}

	// Add participants
	_, err = database.Exec("INSERT INTO ballot_participants (ballot_id, user_id, participant_id, display_name) VALUES (?, ?, ?, ?), (?, ?, ?, ?)",
		"ballot1", "user1", "part1", "Alice",
		"ballot1", "user2", "part2", "Bob")
	if err != nil {
		t.Fatalf("Failed to add participants: %v", err)
	}

	// Add items
	result1, _ := database.Exec("INSERT INTO items (ballot_id, name, added_by) VALUES (?, ?, ?)",
		"ballot1", "Item A", "user1")
	item1, _ := result1.LastInsertId()

	result2, _ := database.Exec("INSERT INTO items (ballot_id, name, added_by) VALUES (?, ?, ?)",
		"ballot1", "Item B", "user2")
	item2, _ := result2.LastInsertId()

	// Add rankings (user1 prefers A, user2 prefers B)
	_, err = database.Exec("INSERT INTO rankings (ballot_id, user_id, item_id, position) VALUES (?, ?, ?, ?), (?, ?, ?, ?), (?, ?, ?, ?), (?, ?, ?, ?)",
		"ballot1", "user1", item1, 1,
		"ballot1", "user1", item2, 2,
		"ballot1", "user2", item2, 1,
		"ballot1", "user2", item1, 2)
	if err != nil {
		t.Fatalf("Failed to add rankings: %v", err)
	}

	err = listItems(database, "ballot1")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestListParticipants_NotFound(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	err = listParticipants(database, "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent ballot, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' error, got %v", err)
	}
}

func TestListParticipants_WithData(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	// Create users
	_, err = database.Exec("INSERT INTO users (id) VALUES (?), (?)", "user1", "user2")
	if err != nil {
		t.Fatalf("Failed to create users: %v", err)
	}

	_, err = database.Exec("INSERT INTO ballots (id, title, created_by) VALUES (?, ?, ?)",
		"ballot1", "Test", "user1")
	if err != nil {
		t.Fatalf("Failed to create ballot: %v", err)
	}

	// Add participants (each user can only be a participant once per ballot)
	_, err = database.Exec("INSERT INTO ballot_participants (ballot_id, user_id, participant_id, display_name) VALUES (?, ?, ?, ?), (?, ?, ?, ?)",
		"ballot1", "user1", "part1", "Alice",
		"ballot1", "user2", "part2", "")
	if err != nil {
		t.Fatalf("Failed to add participants: %v", err)
	}

	err = listParticipants(database, "ballot1")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}
