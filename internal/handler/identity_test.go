package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/civilfritz/civilsort/internal/protocol"
)

// newParticipant creates a user, joins them to ballotID via addParticipant, and
// returns their user ID, participant ID, and public ID.
func newParticipant(t *testing.T, h *Handler, ballotID string) (userID, participantID, publicID string) {
	t.Helper()
	_, _, userID = authedRequest(t, h, "GET", "/", nil)
	participantID, _, err := h.addParticipant(context.Background(), ballotID, userID)
	if err != nil {
		t.Fatalf("addParticipant: %v", err)
	}
	if err := h.db.QueryRow("SELECT public_id FROM ballot_participants WHERE ballot_id = ? AND user_id = ?",
		ballotID, userID).Scan(&publicID); err != nil {
		t.Fatalf("fetching public_id: %v", err)
	}
	if publicID == "" || publicID == participantID {
		t.Fatalf("public ID %q must be set and differ from participant ID %q", publicID, participantID)
	}
	return userID, participantID, publicID
}

func newBallot(t *testing.T, h *Handler) string {
	t.Helper()
	_, _, creator := authedRequest(t, h, "GET", "/", nil)
	ballotID := generateBallotID()
	if _, err := h.db.Exec("INSERT INTO ballots (id, title, created_by) VALUES (?, ?, ?)",
		ballotID, "Test", creator); err != nil {
		t.Fatalf("creating ballot: %v", err)
	}
	return ballotID
}

func TestBroadcastsHideSecrets(t *testing.T) {
	h := testHandler(t)
	ballotID := newBallot(t, h)
	userID, participantID, publicID := newParticipant(t, h, ballotID)
	if _, err := h.db.Exec("UPDATE ballot_participants SET display_name = 'A' WHERE user_id = ?", userID); err != nil {
		t.Fatal(err)
	}
	insertItem(t, h.db, ballotID, "Item", userID)

	s, err := h.snapshot(context.Background(), ballotID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(protocol.NewParticipantsChangedMessage(s.results, s.items, s.participants))
	if err != nil {
		t.Fatal(err)
	}
	payload := string(data)

	for name, secret := range map[string]string{"user ID": userID, "participant ID": participantID} {
		if strings.Contains(payload, secret) {
			t.Errorf("broadcast contains %s %q: %s", name, secret, payload)
		}
	}
	if !strings.Contains(payload, publicID) {
		t.Errorf("broadcast should identify participant by public ID %q: %s", publicID, payload)
	}
}

func TestHandleGetRankings_Access(t *testing.T) {
	h := testHandler(t)
	ballotID := newBallot(t, h)
	viewerID, _, _ := newParticipant(t, h, ballotID)
	_, targetParticipantID, targetPublicID := newParticipant(t, h, ballotID)

	get := func(userID, id string) int {
		req, rec, _ := authedRequest(t, h, "GET", "/", nil)
		req = req.WithContext(context.WithValue(req.Context(), userIDKey, userID))
		req.SetPathValue("id", ballotID)
		req.SetPathValue("publicID", id)
		h.HandleGetRankings(rec, req)
		return rec.Code
	}

	if code := get(viewerID, targetPublicID); code != http.StatusOK {
		t.Errorf("participant viewing by public ID: got %d, want 200", code)
	}
	if code := get(viewerID, targetParticipantID); code != http.StatusNotFound {
		t.Errorf("lookup by private participant ID: got %d, want 404", code)
	}

	_, _, outsider := authedRequest(t, h, "GET", "/", nil)
	if code := get(outsider, targetPublicID); code != http.StatusForbidden {
		t.Errorf("non-participant: got %d, want 403", code)
	}
}
