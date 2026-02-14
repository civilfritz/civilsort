package handler

import (
	"context"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
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

// authedRequest creates a test request with a user in the database and the userID in context
func authedRequest(t *testing.T, h *Handler, method, path string, body io.Reader) (*http.Request, *httptest.ResponseRecorder, string) {
	t.Helper()

	// Create a user in the database
	userID := util.GenerateUUID()
	_, err := h.db.Exec("INSERT INTO users (id) VALUES (?)", userID)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	req := httptest.NewRequest(method, path, body)
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, userID))
	req = req.WithContext(context.WithValue(req.Context(), freshCookieKey, false))

	rec := httptest.NewRecorder()

	return req, rec, userID
}

func TestRegisterRoutes(t *testing.T) {
	h := testHandler(t)
	mux := http.NewServeMux()

	// Should not panic
	h.RegisterRoutes(mux)
}
