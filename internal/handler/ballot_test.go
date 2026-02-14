package handler

import (
	"github.com/civilfritz/civilsort/internal/util"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHandleCreateBallot(t *testing.T) {
	h := testHandler(t)

	form := url.Values{}
	form.Set("title", "Test Ballot")
	req, rec, _ := authedRequest(t, h, "POST", "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	h.HandleCreateBallot(rec, req)

	// Should redirect
	if rec.Code != http.StatusSeeOther {
		t.Errorf("Expected status 303, got %d", rec.Code)
	}

	location := rec.Header().Get("Location")
	if !strings.HasPrefix(location, "/ballot/") {
		t.Errorf("Expected redirect to /ballot/..., got %s", location)
	}
}

func TestHandleBallot_NotFound(t *testing.T) {
	h := testHandler(t)
	req, rec, _ := authedRequest(t, h, "GET", "/ballot/nonexistent", nil)
	req.SetPathValue("id", "nonexistent")

	h.HandleBallot(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", rec.Code)
	}
}

func TestHandleBallot_NewParticipant(t *testing.T) {
	h := testHandler(t)

	// Create creator user first
	creatorID := util.GenerateUUID()
	_, err := h.db.Exec("INSERT INTO users (id) VALUES (?)", creatorID)
	if err != nil {
		t.Fatalf("Failed to create creator user: %v", err)
	}

	// Create a ballot
	ballotID := generateBallotID()
	_, err = h.db.Exec("INSERT INTO ballots (id, title, is_open, created_by) VALUES (?, ?, 1, ?)",
		ballotID, "Test", creatorID)
	if err != nil {
		t.Fatalf("Failed to create ballot: %v", err)
	}

	req, rec, userID := authedRequest(t, h, "GET", "/ballot/"+ballotID, nil)
	req.SetPathValue("id", ballotID)

	h.HandleBallot(rec, req)

	// Should redirect to participant-specific URL
	if rec.Code != http.StatusSeeOther {
		t.Errorf("Expected status 303, got %d", rec.Code)
	}

	// User should be added as participant
	var count int
	err = h.db.QueryRow("SELECT COUNT(*) FROM ballot_participants WHERE ballot_id = ? AND user_id = ?",
		ballotID, userID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query participants: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected user to be added as participant")
	}
}

func TestHandleBallot_ClosedBallot_NonParticipant(t *testing.T) {
	h := testHandler(t)

	// Create creator user first
	creatorID := util.GenerateUUID()
	_, err := h.db.Exec("INSERT INTO users (id) VALUES (?)", creatorID)
	if err != nil {
		t.Fatalf("Failed to create creator user: %v", err)
	}

	// Create a closed ballot
	ballotID := generateBallotID()
	_, err = h.db.Exec("INSERT INTO ballots (id, title, is_open, created_by) VALUES (?, ?, 0, ?)",
		ballotID, "Test", creatorID)
	if err != nil {
		t.Fatalf("Failed to create ballot: %v", err)
	}

	req, rec, _ := authedRequest(t, h, "GET", "/ballot/"+ballotID, nil)
	req.SetPathValue("id", ballotID)

	h.HandleBallot(rec, req)

	// Should return 403
	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", rec.Code)
	}
}

func TestHandleAddItem(t *testing.T) {
	h := testHandler(t)

	// Create ballot and make user a participant
	req, rec, userID := authedRequest(t, h, "POST", "/ballot/test/items", nil)

	ballotID := generateBallotID()
	_, err := h.db.Exec("INSERT INTO ballots (id, title, created_by) VALUES (?, ?, ?)",
		ballotID, "Test", userID)
	if err != nil {
		t.Fatalf("Failed to create ballot: %v", err)
	}
	_, err = h.db.Exec("INSERT INTO ballot_participants (ballot_id, user_id, participant_id) VALUES (?, ?, ?)",
		ballotID, userID, generateParticipantID())
	if err != nil {
		t.Fatalf("Failed to add participant: %v", err)
	}

	form := url.Values{}
	form.Set("name", "Test Item")
	req, rec, _ = authedRequest(t, h, "POST", "/ballot/"+ballotID+"/items", strings.NewReader(form.Encode()))
	req.SetPathValue("id", ballotID)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	h.HandleAddItem(rec, req)

	// Should succeed
	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d", rec.Code)
	}

	// Item should exist in database
	var count int
	err = h.db.QueryRow("SELECT COUNT(*) FROM items WHERE ballot_id = ? AND name = ?",
		ballotID, "Test Item").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query item: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 item, got %d", count)
	}
}

func TestHandleAddItem_Empty(t *testing.T) {
	h := testHandler(t)

	req, rec, userID := authedRequest(t, h, "POST", "/ballot/test/items", strings.NewReader("name="))

	ballotID := generateBallotID()
	_, err := h.db.Exec("INSERT INTO ballots (id, title, created_by) VALUES (?, ?, ?)",
		ballotID, "Test", userID)
	if err != nil {
		t.Fatalf("Failed to create ballot: %v", err)
	}
	_, err = h.db.Exec("INSERT INTO ballot_participants (ballot_id, user_id, participant_id) VALUES (?, ?, ?)",
		ballotID, userID, generateParticipantID())
	if err != nil {
		t.Fatalf("Failed to add participant: %v", err)
	}

	req.SetPathValue("id", ballotID)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	h.HandleAddItem(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

func TestHandleSaveRankings(t *testing.T) {
	h := testHandler(t)

	// Create user and ballot setup
	_, _, userID := authedRequest(t, h, "GET", "/", nil)

	ballotID := generateBallotID()
	_, err := h.db.Exec("INSERT INTO ballots (id, title, created_by) VALUES (?, ?, ?)",
		ballotID, "Test", userID)
	if err != nil {
		t.Fatalf("Failed to create ballot: %v", err)
	}
	_, err = h.db.Exec("INSERT INTO ballot_participants (ballot_id, user_id, participant_id) VALUES (?, ?, ?)",
		ballotID, userID, generateParticipantID())
	if err != nil {
		t.Fatalf("Failed to add participant: %v", err)
	}

	// Add items
	result1, _ := h.db.Exec("INSERT INTO items (ballot_id, name, added_by) VALUES (?, ?, ?)",
		ballotID, "Item 1", userID)
	item1, _ := result1.LastInsertId()
	result2, _ := h.db.Exec("INSERT INTO items (ballot_id, name, added_by) VALUES (?, ?, ?)",
		ballotID, "Item 2", userID)
	item2, _ := result2.LastInsertId()

	// Save rankings
	payload := map[string]interface{}{
		"order": []int64{item1, item2},
	}
	body, _ := json.Marshal(payload)

	// Create request for the SAME user
	req := httptest.NewRequest("POST", "/ballot/"+ballotID+"/rankings", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, userID))
	req.SetPathValue("id", ballotID)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandleSaveRankings(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d", rec.Code)
	}

	// Rankings should be in database
	var count int
	err = h.db.QueryRow("SELECT COUNT(*) FROM rankings WHERE ballot_id = ? AND user_id = ?",
		ballotID, userID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query rankings: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected 2 rankings, got %d", count)
	}
}

func TestHandleSaveRankings_InvalidJSON(t *testing.T) {
	h := testHandler(t)

	req, rec, userID := authedRequest(t, h, "POST", "/ballot/test/rankings", strings.NewReader("invalid json"))

	ballotID := generateBallotID()
	_, err := h.db.Exec("INSERT INTO ballots (id, title, created_by) VALUES (?, ?, ?)",
		ballotID, "Test", userID)
	if err != nil {
		t.Fatalf("Failed to create ballot: %v", err)
	}
	_, err = h.db.Exec("INSERT INTO ballot_participants (ballot_id, user_id, participant_id) VALUES (?, ?, ?)",
		ballotID, userID, generateParticipantID())
	if err != nil {
		t.Fatalf("Failed to add participant: %v", err)
	}

	req.SetPathValue("id", ballotID)

	h.HandleSaveRankings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

func TestHandleToggleOpen(t *testing.T) {
	h := testHandler(t)

	req, rec, userID := authedRequest(t, h, "POST", "/ballot/test/toggle", nil)

	ballotID := generateBallotID()
	_, err := h.db.Exec("INSERT INTO ballots (id, title, is_open, created_by) VALUES (?, ?, 1, ?)",
		ballotID, "Test", userID)
	if err != nil {
		t.Fatalf("Failed to create ballot: %v", err)
	}
	_, err = h.db.Exec("INSERT INTO ballot_participants (ballot_id, user_id, participant_id) VALUES (?, ?, ?)",
		ballotID, userID, generateParticipantID())
	if err != nil {
		t.Fatalf("Failed to add participant: %v", err)
	}

	req.SetPathValue("id", ballotID)

	h.HandleToggleOpen(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	// Should return JSON
	var response map[string]bool
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to parse JSON response: %v", err)
	}

	// Should have toggled to closed
	if response["isOpen"] != false {
		t.Error("Expected isOpen to be false")
	}
}

func TestHandleToggleOpen_NotParticipant(t *testing.T) {
	h := testHandler(t)

	req, rec, _ := authedRequest(t, h, "POST", "/ballot/test/toggle", nil)

	differentUserID := util.GenerateUUID()
	_, err := h.db.Exec("INSERT INTO users (id) VALUES (?)", differentUserID)
	if err != nil {
		t.Fatalf("Failed to create different user: %v", err)
	}

	ballotID := generateBallotID()
	_, err = h.db.Exec("INSERT INTO ballots (id, title, is_open, created_by) VALUES (?, ?, 1, ?)",
		ballotID, "Test", differentUserID)
	if err != nil {
		t.Fatalf("Failed to create ballot: %v", err)
	}

	req.SetPathValue("id", ballotID)

	h.HandleToggleOpen(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", rec.Code)
	}
}

func TestHandleSetName(t *testing.T) {
	h := testHandler(t)

	req, rec, userID := authedRequest(t, h, "POST", "/ballot/test/name", strings.NewReader("name=TestUser"))

	ballotID := generateBallotID()
	_, err := h.db.Exec("INSERT INTO ballots (id, title, created_by) VALUES (?, ?, ?)",
		ballotID, "Test", userID)
	if err != nil {
		t.Fatalf("Failed to create ballot: %v", err)
	}
	_, err = h.db.Exec("INSERT INTO ballot_participants (ballot_id, user_id, participant_id) VALUES (?, ?, ?)",
		ballotID, userID, generateParticipantID())
	if err != nil {
		t.Fatalf("Failed to add participant: %v", err)
	}

	req.SetPathValue("id", ballotID)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	h.HandleSetName(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d", rec.Code)
	}

	// Name should be in database
	var displayName string
	err = h.db.QueryRow("SELECT display_name FROM ballot_participants WHERE ballot_id = ? AND user_id = ?",
		ballotID, userID).Scan(&displayName)
	if err != nil {
		t.Fatalf("Failed to query display name: %v", err)
	}
	if displayName != "TestUser" {
		t.Errorf("Expected display name 'TestUser', got %s", displayName)
	}
}

func TestHandleSetName_NotParticipant(t *testing.T) {
	h := testHandler(t)

	req, rec, _ := authedRequest(t, h, "POST", "/ballot/test/name", strings.NewReader("name=TestUser"))

	differentUserID := util.GenerateUUID()
	_, err := h.db.Exec("INSERT INTO users (id) VALUES (?)", differentUserID)
	if err != nil {
		t.Fatalf("Failed to create different user: %v", err)
	}

	ballotID := generateBallotID()
	_, err = h.db.Exec("INSERT INTO ballots (id, title, created_by) VALUES (?, ?, ?)",
		ballotID, "Test", differentUserID)
	if err != nil {
		t.Fatalf("Failed to create ballot: %v", err)
	}

	req.SetPathValue("id", ballotID)

	h.HandleSetName(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", rec.Code)
	}
}
