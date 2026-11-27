package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestHandleJoin(t *testing.T) {
	h := testHandler(t)
	ballotID, _ := newBallot(t, h)
	path := map[string]string{"id": ballotID}

	if rec := do(t, h.HandleJoin, "POST", req{path: path, form: url.Values{"name": {"   "}}}); rec.Code != http.StatusBadRequest {
		t.Errorf("blank name: got %d, want 400", rec.Code)
	}

	setOpen(t, h, ballotID, false)
	if rec := do(t, h.HandleJoin, "POST", req{path: path, form: url.Values{"name": {"Bob"}}}); rec.Code != http.StatusForbidden {
		t.Errorf("closed ballot: got %d, want 403", rec.Code)
	}
	setOpen(t, h, ballotID, true)

	rec := do(t, h.HandleJoin, "POST", req{path: path, form: url.Values{"name": {" Bob "}}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/ballot/"+ballotID {
		t.Fatalf("join: got %d to %q, want 303 to /ballot/%s", rec.Code, rec.Header().Get("Location"), ballotID)
	}
	bob, ok, err := h.participantBySecret(t.Context(), ballotID, participantCookieFrom(t, rec).Value)
	if err != nil || !ok || bob.DisplayName != "Bob" || bob.IsCreator {
		t.Fatalf("joined participant: %+v ok=%v err=%v", bob, ok, err)
	}

	// Joining again as an existing participant just returns to the ballot.
	rec = do(t, h.HandleJoin, "POST", req{as: &bob, path: path, form: url.Values{"name": {"Bob again"}}})
	if rec.Code != http.StatusSeeOther {
		t.Errorf("rejoin: got %d, want 303", rec.Code)
	}
	if n := countRows(t, h, "SELECT COUNT(*) FROM participants WHERE ballot_id = ?", ballotID); n != 2 {
		t.Errorf("got %d participants, want 2", n)
	}
}

// API calls never make anyone a participant.
func TestViewerCannotAct(t *testing.T) {
	h := testHandler(t)
	ballotID, creator := newBallot(t, h)
	item := insertItem(t, h.db, ballotID, "Item", creator.ID)
	path := map[string]string{"id": ballotID}

	calls := map[string]*httptest.ResponseRecorder{
		"add item": do(t, h.HandleAddItem, "POST", req{path: path, form: url.Values{"name": {"X"}}}),
		"rank":     do(t, h.HandleSaveRankings, "POST", req{path: path, json: map[string]any{"order": []int64{item}}}),
		"delete": do(t, h.HandleDeleteItem, "POST", req{
			path: map[string]string{"id": ballotID, "itemID": "1"},
		}),
		"rename": do(t, h.HandleSetName, "POST", req{path: path, form: url.Values{"name": {"X"}}}),
	}
	for name, rec := range calls {
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: got %d, want 401", name, rec.Code)
		}
	}
	if n := countRows(t, h, "SELECT COUNT(*) FROM participants WHERE ballot_id = ?", ballotID); n != 1 {
		t.Errorf("API calls created participants: got %d, want 1", n)
	}
}
