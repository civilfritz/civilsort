package handler

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"
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

func TestHandleSaveRankings_RejectsInvalidOrder(t *testing.T) {
	h := testHandler(t)
	ballotID, creator := newBallot(t, h)
	item := insertItem(t, h.db, ballotID, "Mine", creator.ID)

	otherBallot, otherCreator := newBallot(t, h)
	foreign := insertItem(t, h.db, otherBallot, "Theirs", otherCreator.ID)

	save := func(order []int64) int {
		return do(t, h.HandleSaveRankings, "POST", req{
			as:   &creator,
			path: map[string]string{"id": ballotID},
			json: map[string]any{"order": order},
		}).Code
	}

	tests := map[string][]int64{
		"duplicate":    {item, item},
		"other ballot": {item, foreign},
		"nonexistent":  {item + foreign + 100},
	}
	for name, order := range tests {
		t.Run(name, func(t *testing.T) {
			if code := save(order); code != http.StatusBadRequest {
				t.Errorf("got %d, want 400", code)
			}
		})
	}

	if code := save([]int64{item}); code != http.StatusNoContent {
		t.Errorf("valid order: got %d, want 204", code)
	}
}

func TestHandleAddItem_WhitespaceAndLength(t *testing.T) {
	h := testHandler(t)
	ballotID, creator := newBallot(t, h)
	add := func(name string) int {
		return do(t, h.HandleAddItem, "POST", req{
			as:   &creator,
			path: map[string]string{"id": ballotID},
			form: url.Values{"name": {name}},
		}).Code
	}

	if code := add("   "); code != http.StatusBadRequest {
		t.Errorf("whitespace-only name: got %d, want 400", code)
	}

	long := strings.Repeat("é", maxNameLength+10)
	if code := add("  " + long); code != http.StatusNoContent {
		t.Fatalf("long name: got %d, want 204", code)
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
	ballotID, creator := newBallot(t, h)

	// 49 ASCII bytes then a 2-byte character: byte truncation at 50 would split it.
	name := strings.Repeat("a", 49) + "éxyz"
	rec := do(t, h.HandleSetName, "POST", req{
		as:   &creator,
		path: map[string]string{"id": ballotID},
		form: url.Values{"name": {" " + name + " "}},
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("got %d, want 204", rec.Code)
	}

	var got string
	if err := h.db.QueryRow("SELECT display_name FROM participants WHERE ballot_id = ? AND id = ?",
		ballotID, creator.ID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if want := strings.Repeat("a", 49) + "é"; got != want {
		t.Errorf("display name = %q, want %q", got, want)
	}
}
