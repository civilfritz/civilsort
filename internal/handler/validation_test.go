package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/civilfritz/civilsort/internal/protocol"
)

func TestCleanText(t *testing.T) {
	tests := []struct {
		in    string
		limit int
		want  string
	}{
		{"  hello  ", 10, "hello"},
		{"   ", 10, ""},
		{"héllo wörld", 5, "héllo"},
		{"日本語テキスト", 3, "日本語"},
		{"ab cd", 3, "ab"}, // trailing space after truncation is trimmed
		{"bad\xffbyte", 20, "bad�byte"},
	}
	for _, tt := range tests {
		got := cleanText(tt.in, tt.limit)
		if got != tt.want {
			t.Errorf("cleanText(%q, %d) = %q, want %q", tt.in, tt.limit, got, tt.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("cleanText(%q, %d) returned invalid UTF-8", tt.in, tt.limit)
		}
	}
}

func saveRankings(t *testing.T, h *Handler, ballotID, userID string, order []int64) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(protocol.SaveRankingsRequest{Order: order})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/ballot/"+ballotID+"/rankings", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, userID))
	req.SetPathValue("id", ballotID)
	rec := httptest.NewRecorder()
	h.HandleSaveRankings(rec, req)
	return rec
}

func TestHandleSaveRankings_RejectsInvalidOrder(t *testing.T) {
	h := testHandler(t)
	ballotID := newBallot(t, h)
	userID, _, _ := newParticipant(t, h, ballotID)
	item := insertItem(t, h.db, ballotID, "Mine", userID)

	otherBallot := newBallot(t, h)
	otherUser, _, _ := newParticipant(t, h, otherBallot)
	foreign := insertItem(t, h.db, otherBallot, "Theirs", otherUser)

	tests := map[string][]int64{
		"duplicate":    {item, item},
		"other ballot": {item, foreign},
		"nonexistent":  {item + foreign + 100},
	}
	for name, order := range tests {
		t.Run(name, func(t *testing.T) {
			if rec := saveRankings(t, h, ballotID, userID, order); rec.Code != http.StatusBadRequest {
				t.Errorf("got %d, want 400: %s", rec.Code, rec.Body)
			}
		})
	}

	if rec := saveRankings(t, h, ballotID, userID, []int64{item}); rec.Code != http.StatusNoContent {
		t.Errorf("valid order: got %d, want 204: %s", rec.Code, rec.Body)
	}
}

func postForm(t *testing.T, h *Handler, handle http.HandlerFunc, ballotID, userID string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, userID))
	req.SetPathValue("id", ballotID)
	rec := httptest.NewRecorder()
	handle(rec, req)
	return rec
}

func TestHandleAddItem_WhitespaceAndLength(t *testing.T) {
	h := testHandler(t)
	ballotID := newBallot(t, h)
	userID, _, _ := newParticipant(t, h, ballotID)

	if rec := postForm(t, h, h.HandleAddItem, ballotID, userID, url.Values{"name": {"   "}}); rec.Code != http.StatusBadRequest {
		t.Errorf("whitespace-only name: got %d, want 400", rec.Code)
	}

	long := strings.Repeat("é", maxNameLength+10)
	if rec := postForm(t, h, h.HandleAddItem, ballotID, userID, url.Values{"name": {"  " + long}}); rec.Code != http.StatusNoContent {
		t.Fatalf("long name: got %d, want 204: %s", rec.Code, rec.Body)
	}
	var name string
	if err := h.db.QueryRow("SELECT name FROM items WHERE ballot_id = ?", ballotID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(name); n != maxNameLength {
		t.Errorf("stored name has %d characters, want %d", n, maxNameLength)
	}
}

func TestHandleSetName_TruncatesByCharacter(t *testing.T) {
	h := testHandler(t)
	ballotID := newBallot(t, h)
	userID, _, _ := newParticipant(t, h, ballotID)

	// 49 ASCII bytes then a 2-byte character: byte truncation at 50 would split it.
	name := strings.Repeat("a", 49) + "éxyz"
	if rec := postForm(t, h, h.HandleSetName, ballotID, userID, url.Values{"name": {" " + name + " "}}); rec.Code != http.StatusNoContent {
		t.Fatalf("got %d, want 204", rec.Code)
	}

	var got string
	if err := h.db.QueryRow("SELECT display_name FROM ballot_participants WHERE user_id = ?", userID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if want := strings.Repeat("a", 49) + "é"; got != want {
		t.Errorf("display name = %q, want %q", got, want)
	}
}
