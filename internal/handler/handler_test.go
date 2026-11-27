package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/civilfritz/civilsort/internal/db"
	"github.com/civilfritz/civilsort/internal/hub"
	"github.com/civilfritz/civilsort/internal/util"
)

// testHandler creates a fully wired Handler with in-memory database for testing
func testHandler(t *testing.T) *Handler {
	t.Helper()

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Minimal templates for testing
	tmplText := `
{{define "home.html"}}<html></html>{{end}}
{{define "ballot.html"}}<html></html>{{end}}
{{define "about.html"}}<html></html>{{end}}
{{define "stats.html"}}<html></html>{{end}}
{{define "closed.html"}}<html></html>{{end}}
`
	tmpls := template.Must(template.New("").Parse(tmplText))

	hubManager := hub.NewManager()

	t.Cleanup(func() {
		database.Close()
		hubManager.CloseAll()
	})

	return New(database, tmpls, hubManager)
}

// member is a test participant and the browser identity that owns it.
type member struct {
	userID string
	participant
}

// newBallot creates an open ballot and its creator.
func newBallot(t *testing.T, h *Handler) (string, member) {
	t.Helper()
	ballotID := generateBallotID()
	if _, err := h.db.Exec("INSERT INTO ballots (id, title) VALUES (?, 'Test')", ballotID); err != nil {
		t.Fatalf("creating ballot: %v", err)
	}
	return ballotID, addMember(t, h, ballotID, true)
}

// join adds a new non-creator participant to ballotID.
func join(t *testing.T, h *Handler, ballotID string) member {
	t.Helper()
	return addMember(t, h, ballotID, false)
}

func addMember(t *testing.T, h *Handler, ballotID string, isCreator bool) member {
	t.Helper()
	userID := util.GenerateUUID()
	p, _, err := h.addParticipant(context.Background(), ballotID, userID, isCreator)
	if err != nil {
		t.Fatalf("addParticipant: %v", err)
	}
	return member{userID: userID, participant: p}
}

func setOpen(t *testing.T, h *Handler, ballotID string, open bool) {
	t.Helper()
	if _, err := h.db.Exec("UPDATE ballots SET is_open = ? WHERE id = ?", open, ballotID); err != nil {
		t.Fatal(err)
	}
}

// insertItem inserts an item and returns its ID, failing the test on error.
func insertItem(t *testing.T, db *sql.DB, ballotID, name, addedBy string) int64 {
	t.Helper()
	var id int64
	err := db.QueryRow("INSERT INTO items (ballot_id, name, added_by) VALUES (?, ?, ?) RETURNING id",
		ballotID, name, addedBy).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to insert item %q: %v", name, err)
	}
	return id
}

func countRows(t *testing.T, h *Handler, query string, args ...any) int {
	t.Helper()
	var n int
	if err := h.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// req describes a test request. as is the requesting member; nil means a
// browser with no identity on any ballot.
type req struct {
	as   *member
	path map[string]string
	form url.Values
	json any
}

// do builds the request described by o, passes it to handle, and returns
// the response.
func do(t *testing.T, handle http.HandlerFunc, method string, o req) *httptest.ResponseRecorder {
	t.Helper()

	var body io.Reader
	contentType := ""
	switch {
	case o.form != nil:
		body = strings.NewReader(o.form.Encode())
		contentType = "application/x-www-form-urlencoded"
	case o.json != nil:
		data, err := json.Marshal(o.json)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(data)
		contentType = "application/json"
	}

	r := httptest.NewRequest(method, "/", body)
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	for k, v := range o.path {
		r.SetPathValue(k, v)
	}

	userID, fresh := util.GenerateUUID(), true
	if o.as != nil {
		userID, fresh = o.as.userID, false
	}
	ctx := context.WithValue(r.Context(), userIDKey, userID)
	ctx = context.WithValue(ctx, freshCookieKey, fresh)

	rec := httptest.NewRecorder()
	handle(rec, r.WithContext(ctx))
	return rec
}

func TestRegisterRoutes(t *testing.T) {
	h := testHandler(t)
	mux := http.NewServeMux()

	// Should not panic
	h.RegisterRoutes(mux)
}
