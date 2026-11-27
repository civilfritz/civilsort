package handler

import (
	"net/http"
	"testing"
)

func TestHandleStats_Empty(t *testing.T) {
	h := testHandler(t)
	if rec := do(t, h.HandleStats, "GET", req{}); rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestHandleStats_WithData(t *testing.T) {
	h := testHandler(t)
	ballotID, creator := newBallot(t, h)
	insertItem(t, h.db, ballotID, "Item 1", creator.ID)

	if rec := do(t, h.HandleStats, "GET", req{}); rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}
