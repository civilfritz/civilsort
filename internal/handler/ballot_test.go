package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestHandleCreateBallot(t *testing.T) {
	h := testHandler(t)

	rec := do(t, h.HandleCreateBallot, "POST", req{form: url.Values{"title": {"Test Ballot"}}})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("Expected status 303, got %d", rec.Code)
	}
	location := rec.Header().Get("Location")
	if !strings.HasPrefix(location, "/ballot/") {
		t.Fatalf("Expected redirect to /ballot/..., got %s", location)
	}

	ballotID := strings.Split(location, "/")[2]
	if n := countRows(t, h, "SELECT COUNT(*) FROM participants WHERE ballot_id = ? AND is_creator", ballotID); n != 1 {
		t.Errorf("Expected 1 creator, got %d", n)
	}
}

func TestHandleBallot_NotFound(t *testing.T) {
	h := testHandler(t)
	rec := do(t, h.HandleBallot, "GET", req{path: map[string]string{"id": "nonexistent"}})
	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", rec.Code)
	}
}

func TestHandleBallot_NewParticipant(t *testing.T) {
	h := testHandler(t)
	ballotID, _ := newBallot(t, h)

	rec := do(t, h.HandleBallot, "GET", req{path: map[string]string{"id": ballotID}})

	if rec.Code != http.StatusSeeOther {
		t.Errorf("Expected status 303, got %d", rec.Code)
	}
	if n := countRows(t, h, "SELECT COUNT(*) FROM participants WHERE ballot_id = ?", ballotID); n != 2 {
		t.Errorf("Expected visitor to be added as participant, got %d participants", n)
	}
}

func TestHandleBallot_ClosedBallot_NonParticipant(t *testing.T) {
	h := testHandler(t)
	ballotID, _ := newBallot(t, h)
	setOpen(t, h, ballotID, false)

	rec := do(t, h.HandleBallot, "GET", req{path: map[string]string{"id": ballotID}})

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", rec.Code)
	}
}

func TestHandleAddItem(t *testing.T) {
	h := testHandler(t)
	ballotID, creator := newBallot(t, h)

	rec := do(t, h.HandleAddItem, "POST", req{
		as:   &creator,
		path: map[string]string{"id": ballotID},
		form: url.Values{"name": {"Test Item"}},
	})

	if rec.Code != http.StatusNoContent {
		t.Fatalf("Expected status 204, got %d", rec.Code)
	}
	if n := countRows(t, h, "SELECT COUNT(*) FROM items WHERE ballot_id = ? AND name = ? AND added_by = ?",
		ballotID, "Test Item", creator.ID); n != 1 {
		t.Errorf("Expected item attributed to creator, got %d", n)
	}
}

func TestHandleAddItem_Empty(t *testing.T) {
	h := testHandler(t)
	ballotID, creator := newBallot(t, h)

	rec := do(t, h.HandleAddItem, "POST", req{
		as:   &creator,
		path: map[string]string{"id": ballotID},
		form: url.Values{"name": {""}},
	})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

func TestHandleSaveRankings(t *testing.T) {
	h := testHandler(t)
	ballotID, creator := newBallot(t, h)
	item1 := insertItem(t, h.db, ballotID, "Item 1", creator.ID)
	item2 := insertItem(t, h.db, ballotID, "Item 2", creator.ID)

	rec := do(t, h.HandleSaveRankings, "POST", req{
		as:   &creator,
		path: map[string]string{"id": ballotID},
		json: map[string]any{"order": []int64{item2, item1}},
	})

	if rec.Code != http.StatusNoContent {
		t.Fatalf("Expected status 204, got %d: %s", rec.Code, rec.Body)
	}
	rankings, err := h.getRankings(t.Context(), ballotID, creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rankings) != 2 || rankings[0].ItemID != item2 || rankings[1].ItemID != item1 {
		t.Errorf("Unexpected rankings: %+v", rankings)
	}
}

func TestHandleSaveRankings_InvalidJSON(t *testing.T) {
	h := testHandler(t)
	ballotID, creator := newBallot(t, h)

	rec := do(t, h.HandleSaveRankings, "POST", req{
		as:   &creator,
		path: map[string]string{"id": ballotID},
		form: url.Values{"not": {"json"}},
	})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

func TestHandleToggleOpen(t *testing.T) {
	h := testHandler(t)
	ballotID, creator := newBallot(t, h)

	rec := do(t, h.HandleToggleOpen, "POST", req{as: &creator, path: map[string]string{"id": ballotID}})

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}
	var response map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse JSON response: %v", err)
	}
	if response["isOpen"] {
		t.Error("Expected isOpen to be false")
	}
}

func TestHandleToggleOpen_NotParticipant(t *testing.T) {
	h := testHandler(t)
	ballotID, _ := newBallot(t, h)

	rec := do(t, h.HandleToggleOpen, "POST", req{path: map[string]string{"id": ballotID}})

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", rec.Code)
	}
}

func TestHandleSetName(t *testing.T) {
	h := testHandler(t)
	ballotID, creator := newBallot(t, h)

	rec := do(t, h.HandleSetName, "POST", req{
		as:   &creator,
		path: map[string]string{"id": ballotID},
		form: url.Values{"name": {"TestUser"}},
	})

	if rec.Code != http.StatusNoContent {
		t.Fatalf("Expected status 204, got %d", rec.Code)
	}
	var displayName string
	if err := h.db.QueryRow("SELECT display_name FROM participants WHERE ballot_id = ? AND id = ?",
		ballotID, creator.ID).Scan(&displayName); err != nil {
		t.Fatal(err)
	}
	if displayName != "TestUser" {
		t.Errorf("Expected display name 'TestUser', got %s", displayName)
	}
}

func TestHandleSetName_NotParticipant(t *testing.T) {
	h := testHandler(t)
	ballotID, _ := newBallot(t, h)

	rec := do(t, h.HandleSetName, "POST", req{
		path: map[string]string{"id": ballotID},
		form: url.Values{"name": {"TestUser"}},
	})

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", rec.Code)
	}
}
