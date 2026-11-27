package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/civilfritz/civilsort/internal/protocol"
)

func TestCreatorOnlyActions(t *testing.T) {
	h := testHandler(t)
	ballotID, _ := newBallot(t, h)
	member := join(t, h, ballotID)
	other := join(t, h, ballotID)

	path := map[string]string{"id": ballotID}
	targetPath := map[string]string{"id": ballotID, "participantID": other.ID}
	actions := map[string]struct {
		handle http.HandlerFunc
		method string
		path   map[string]string
	}{
		"toggle":     {h.HandleToggleOpen, "POST", path},
		"title":      {h.HandleSetTitle, "POST", path},
		"visibility": {h.HandleSetVisibility, "POST", path},
		"delete":     {h.HandleDeleteBallot, "POST", path},
		"link":       {h.HandleParticipantLink, "GET", targetPath},
		"reset":      {h.HandleResetParticipant, "POST", targetPath},
		"remove":     {h.HandleRemoveParticipant, "POST", targetPath},
	}
	for name, a := range actions {
		t.Run(name, func(t *testing.T) {
			if rec := do(t, a.handle, a.method, req{as: &member, path: a.path}); rec.Code != http.StatusForbidden {
				t.Errorf("participant: got %d, want 403", rec.Code)
			}
			if rec := do(t, a.handle, a.method, req{path: a.path}); rec.Code != http.StatusUnauthorized {
				t.Errorf("viewer: got %d, want 401", rec.Code)
			}
		})
	}
	if n := countRows(t, h, "SELECT COUNT(*) FROM participants WHERE ballot_id = ? AND removed_at IS NULL", ballotID); n != 3 {
		t.Errorf("refused actions changed participants: %d active, want 3", n)
	}
}

func TestDeleteItemRule(t *testing.T) {
	h := testHandler(t)
	ballotID, creator := newBallot(t, h)
	alice := join(t, h, ballotID)
	bob := join(t, h, ballotID)

	unranked := insertItem(t, h.db, ballotID, "Unranked", alice.ID)
	selfRanked := insertItem(t, h.db, ballotID, "Self ranked", alice.ID)
	othersRanked := insertItem(t, h.db, ballotID, "Others ranked", alice.ID)
	bobs := insertItem(t, h.db, ballotID, "Bob's", bob.ID)
	for _, r := range []struct {
		who  string
		item int64
	}{{alice.ID, selfRanked}, {bob.ID, othersRanked}, {bob.ID, bobs}} {
		if _, err := h.db.Exec("INSERT INTO rankings (ballot_id, participant_id, item_id, position) VALUES (?, ?, ?, 1)",
			ballotID, r.who, r.item); err != nil {
			t.Fatal(err)
		}
	}

	del := func(as participant, item int64) int {
		return do(t, h.HandleDeleteItem, "POST", req{
			as:   &as,
			path: map[string]string{"id": ballotID, "itemID": fmt.Sprint(item)},
		}).Code
	}

	if code := del(alice, othersRanked); code != http.StatusForbidden {
		t.Errorf("own item ranked by someone else: got %d, want 403", code)
	}
	if code := del(alice, bobs); code != http.StatusForbidden {
		t.Errorf("someone else's item: got %d, want 403", code)
	}
	if code := del(alice, unranked); code != http.StatusNoContent {
		t.Errorf("own unranked item: got %d, want 204", code)
	}
	if code := del(alice, selfRanked); code != http.StatusNoContent {
		t.Errorf("own item ranked only by self: got %d, want 204", code)
	}
	if code := del(creator, othersRanked); code != http.StatusNoContent {
		t.Errorf("creator deleting a ranked item: got %d, want 204", code)
	}
}

func TestHandleSetTitle(t *testing.T) {
	h := testHandler(t)
	ballotID, creator := newBallot(t, h)

	rec := do(t, h.HandleSetTitle, "POST", req{as: &creator, path: map[string]string{"id": ballotID}, form: url.Values{"title": {"  New title "}}})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("got %d, want 204", rec.Code)
	}
	var title string
	if err := h.db.QueryRow("SELECT title FROM ballots WHERE id = ?", ballotID).Scan(&title); err != nil || title != "New title" {
		t.Errorf("title = %q (%v), want %q", title, err, "New title")
	}
}

func TestHandleRemoveParticipant(t *testing.T) {
	for _, tt := range []struct{ rankings, items bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		t.Run(fmt.Sprintf("rankings=%v,items=%v", tt.rankings, tt.items), func(t *testing.T) {
			h := testHandler(t)
			ballotID, creator := newBallot(t, h)
			target := join(t, h, ballotID)
			theirs := insertItem(t, h.db, ballotID, "Theirs", target.ID)
			mine := insertItem(t, h.db, ballotID, "Mine", creator.ID)
			for _, r := range []struct {
				who  string
				item int64
			}{{target.ID, mine}, {creator.ID, theirs}} {
				if _, err := h.db.Exec("INSERT INTO rankings (ballot_id, participant_id, item_id, position) VALUES (?, ?, ?, 1)",
					ballotID, r.who, r.item); err != nil {
					t.Fatal(err)
				}
			}

			form := url.Values{}
			if tt.rankings {
				form.Set("rankings", "1")
			}
			if tt.items {
				form.Set("items", "1")
			}
			rec := do(t, h.HandleRemoveParticipant, "POST", req{
				as:   &creator,
				path: map[string]string{"id": ballotID, "participantID": target.ID},
				form: form,
			})
			if rec.Code != http.StatusNoContent {
				t.Fatalf("got %d, want 204: %s", rec.Code, rec.Body)
			}

			if _, ok, _ := h.participantBySecret(t.Context(), ballotID, target.Secret); ok {
				t.Error("removed participant's secret still works")
			}
			participants, err := h.getParticipants(t.Context(), ballotID)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range participants {
				if p.ID == target.ID {
					t.Error("removed participant still listed")
				}
			}

			wantRankings := 1
			if !tt.rankings {
				wantRankings = 2
			}
			if tt.items {
				wantRankings-- // the creator's ranking of the deleted item goes too
			}
			if n := countRows(t, h, "SELECT COUNT(*) FROM rankings WHERE ballot_id = ?", ballotID); n != wantRankings {
				t.Errorf("rankings left: %d, want %d", n, wantRankings)
			}
			wantItems := 2
			if tt.items {
				wantItems = 1
			}
			if n := countRows(t, h, "SELECT COUNT(*) FROM items WHERE ballot_id = ?", ballotID); n != wantItems {
				t.Errorf("items left: %d, want %d", n, wantItems)
			}
			if !tt.items {
				items, err := h.getItems(t.Context(), ballotID)
				if err != nil {
					t.Fatal(err)
				}
				for _, item := range items {
					if item.ID == theirs && item.AddedByName != "" {
						t.Errorf("kept item still attributed to %q", item.AddedByName)
					}
				}
			}
		})
	}

	t.Run("not self", func(t *testing.T) {
		h := testHandler(t)
		ballotID, creator := newBallot(t, h)
		rec := do(t, h.HandleRemoveParticipant, "POST", req{as: &creator, path: map[string]string{"id": ballotID, "participantID": creator.ID}})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("creator removing self: got %d, want 400", rec.Code)
		}
	})
}

func TestParticipantLinks(t *testing.T) {
	h := testHandler(t)
	ballotID, creator := newBallot(t, h)
	member := join(t, h, ballotID)
	path := map[string]string{"id": ballotID, "participantID": member.ID}

	link := func(rec *httptest.ResponseRecorder) string {
		t.Helper()
		var resp protocol.LinkResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decoding link: %v (%s)", err, rec.Body)
		}
		return resp.Link
	}

	got := link(do(t, h.HandleParticipantLink, "GET", req{as: &creator, path: path}))
	if got != joinLink(ballotID, member) {
		t.Errorf("link = %q, want %q", got, joinLink(ballotID, member))
	}

	reset := link(do(t, h.HandleResetParticipant, "POST", req{as: &creator, path: path}))
	newSecret := strings.TrimPrefix(reset, "/ballot/"+ballotID+"/join/")
	if newSecret == member.Secret || newSecret == reset {
		t.Fatalf("reset link %q should carry a new secret", reset)
	}
	if _, ok, _ := h.participantBySecret(t.Context(), ballotID, member.Secret); ok {
		t.Error("old secret still works after reset")
	}
	if p, ok, _ := h.participantBySecret(t.Context(), ballotID, newSecret); !ok || p.ID != member.ID {
		t.Error("new secret should identify the same participant")
	}

	// Resetting your own link keeps this browser signed in.
	rec := do(t, h.HandleResetParticipant, "POST", req{as: &creator, path: map[string]string{"id": ballotID, "participantID": creator.ID}})
	if c := participantCookieFrom(t, rec); c.Value == creator.Secret || !strings.HasSuffix(link(rec), c.Value) {
		t.Errorf("self reset should set the new secret as the cookie, got %q", c.Value)
	}
}

func TestHandleDeleteBallot(t *testing.T) {
	h := testHandler(t)
	ballotID, creator := newBallot(t, h)
	insertItem(t, h.db, ballotID, "Item", creator.ID)

	if rec := do(t, h.HandleDeleteBallot, "POST", req{as: &creator, path: map[string]string{"id": ballotID}}); rec.Code != http.StatusNoContent {
		t.Fatalf("got %d, want 204", rec.Code)
	}
	for _, table := range []string{"ballots WHERE id", "participants WHERE ballot_id", "items WHERE ballot_id"} {
		if n := countRows(t, h, "SELECT COUNT(*) FROM "+table+" = ?", ballotID); n != 0 {
			t.Errorf("%s: %d rows left", table, n)
		}
	}
	if rec := do(t, h.HandleBallot, "GET", req{path: map[string]string{"id": ballotID}}); rec.Code != http.StatusNotFound {
		t.Errorf("deleted ballot: got %d, want 404", rec.Code)
	}
}

func TestRankingsVisibility(t *testing.T) {
	h := testHandler(t)
	ballotID, creator := newBallot(t, h)
	member := join(t, h, ballotID)
	item := insertItem(t, h.db, ballotID, "Item", creator.ID)
	path := map[string]string{"id": ballotID}

	setVisibility := func(private string) int {
		return do(t, h.HandleSetVisibility, "POST", req{as: &creator, path: path, form: url.Values{"private": {private}}}).Code
	}
	rank := func(p participant) {
		if _, err := h.db.Exec("INSERT INTO rankings (ballot_id, participant_id, item_id, position) VALUES (?, ?, ?, 1)",
			ballotID, p.ID, item); err != nil {
			t.Fatal(err)
		}
	}
	view := func(as, target participant) int {
		return do(t, h.HandleGetRankings, "GET", req{as: &as, path: map[string]string{"id": ballotID, "participantID": target.ID}}).Code
	}

	if code := view(member, creator); code != http.StatusOK {
		t.Errorf("visible rankings: got %d, want 200", code)
	}

	if code := setVisibility("1"); code != http.StatusNoContent {
		t.Fatalf("make private: got %d, want 204", code)
	}
	if code := view(member, creator); code != http.StatusForbidden {
		t.Errorf("private, someone else's: got %d, want 403", code)
	}
	if code := view(creator, member); code != http.StatusForbidden {
		t.Errorf("private, creator viewing a participant: got %d, want 403", code)
	}
	if code := view(member, member); code != http.StatusOK {
		t.Errorf("private, own rankings: got %d, want 200", code)
	}

	// Only the creator has ranked: still free to switch back.
	rank(creator)
	if code := setVisibility("0"); code != http.StatusNoContent {
		t.Errorf("visible again with only creator rankings: got %d, want 204", code)
	}
	if code := setVisibility("1"); code != http.StatusNoContent {
		t.Fatalf("make private again: got %d, want 204", code)
	}

	// Once someone else ranks while private, it stays private.
	rank(member)
	if code := setVisibility("0"); code != http.StatusConflict {
		t.Errorf("visible after others ranked privately: got %d, want 409", code)
	}
	var private bool
	if err := h.db.QueryRow("SELECT rankings_private FROM ballots WHERE id = ?", ballotID).Scan(&private); err != nil || !private {
		t.Errorf("rankings_private = %v (%v), want true", private, err)
	}
}
