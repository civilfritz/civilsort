package handler

import (
	"net/http"
	"testing"
)

func TestHandleStats_Empty(t *testing.T) {
	h := testHandler(t)
	req, rec, _ := authedRequest(t, h, "GET", "/stats", nil)

	h.HandleStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestHandleStats_WithData(t *testing.T) {
	h := testHandler(t)

	// Create some test data
	userID := generateUUID()
	_, err := h.db.Exec("INSERT INTO users (id) VALUES (?)", userID)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	ballotID := generateBallotID()
	_, err = h.db.Exec("INSERT INTO ballots (id, title, created_by) VALUES (?, ?, ?)",
		ballotID, "Test Ballot", userID)
	if err != nil {
		t.Fatalf("Failed to create ballot: %v", err)
	}

	// Add a participant
	_, err = h.db.Exec("INSERT INTO ballot_participants (ballot_id, user_id, participant_id) VALUES (?, ?, ?)",
		ballotID, userID, generateParticipantID())
	if err != nil {
		t.Fatalf("Failed to add participant: %v", err)
	}

	// Add some items
	_, err = h.db.Exec("INSERT INTO items (ballot_id, name, added_by) VALUES (?, ?, ?)",
		ballotID, "Item 1", userID)
	if err != nil {
		t.Fatalf("Failed to add item: %v", err)
	}

	req, rec, _ := authedRequest(t, h, "GET", "/stats", nil)

	h.HandleStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}
