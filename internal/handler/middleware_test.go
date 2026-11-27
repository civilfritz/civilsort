package handler

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/civilfritz/civilsort/internal/util"
)

func TestUserMiddleware_NewUser(t *testing.T) {
	h := testHandler(t)

	// Create a request with no cookie
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	// Wrap a simple handler that checks the context
	var gotUserID string
	var gotFresh bool
	handler := h.UserMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserID = UserID(r.Context())
		gotFresh = IsFreshCookie(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)

	// Should have created a user
	if gotUserID == "" {
		t.Error("UserID should be set")
	}

	// Should be marked as fresh
	if !gotFresh {
		t.Error("IsFreshCookie should return true for new user")
	}

	// Should have set a cookie
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("Should have set a cookie")
	}
	if cookies[0].Name != cookieName {
		t.Errorf("Expected cookie name %s, got %s", cookieName, cookies[0].Name)
	}

	// Visiting must not write to the database.
	if n := countRows(t, h, "SELECT COUNT(*) FROM participants"); n != 0 {
		t.Errorf("Expected no participants, got %d", n)
	}
}

// A visitor who has a cookie but hasn't joined anything keeps their ID and
// is still fresh, so they can adopt a participant link.
func TestUserMiddleware_CookieWithoutUserRow(t *testing.T) {
	h := testHandler(t)

	userID := util.GenerateUUID()
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: userID})
	rec := httptest.NewRecorder()

	var gotUserID string
	var gotFresh bool
	handler := h.UserMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserID = UserID(r.Context())
		gotFresh = IsFreshCookie(r.Context())
	}))
	handler.ServeHTTP(rec, req)

	if gotUserID != userID {
		t.Errorf("Expected cookie user ID %s to be kept, got %s", userID, gotUserID)
	}
	if !gotFresh {
		t.Error("IsFreshCookie should be true until the user joins a ballot")
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Error("Should not reissue a well-formed cookie")
	}
}

func TestUserMiddleware_ExistingUser(t *testing.T) {
	h := testHandler(t)

	// A user who has joined a ballot
	ballotID, _ := newBallot(t, h)
	userID := join(t, h, ballotID).userID

	// Create a request with a valid cookie
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: userID})
	rec := httptest.NewRecorder()

	var gotUserID string
	var gotFresh bool
	handler := h.UserMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserID = UserID(r.Context())
		gotFresh = IsFreshCookie(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)

	// Should use the existing user ID
	if gotUserID != userID {
		t.Errorf("Expected userID %s, got %s", userID, gotUserID)
	}

	// Should NOT be marked as fresh
	if gotFresh {
		t.Error("IsFreshCookie should return false for existing user")
	}

}

func TestUserMiddleware_InvalidCookie(t *testing.T) {
	h := testHandler(t)

	// Create a request with a malformed cookie
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "nonexistent-user-id"})
	rec := httptest.NewRecorder()

	var gotUserID string
	var gotFresh bool
	handler := h.UserMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserID = UserID(r.Context())
		gotFresh = IsFreshCookie(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)

	// Should have created a new user
	if gotUserID == "" {
		t.Error("UserID should be set")
	}
	if gotUserID == "nonexistent-user-id" {
		t.Error("Should have issued a new user ID, not used the malformed one")
	}

	// Should be marked as fresh
	if !gotFresh {
		t.Error("IsFreshCookie should return true for invalid cookie")
	}

	// Should have set a new cookie
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("Should have set a new cookie")
	}
}

// Creating a ballot makes the requester its creator participant.
func TestHandleCreateBallot_CreatesCreator(t *testing.T) {
	h := testHandler(t)

	userID := util.GenerateUUID()
	req := httptest.NewRequest("POST", "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, userID))
	rec := httptest.NewRecorder()

	h.HandleCreateBallot(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("Expected 303, got %d: %s", rec.Code, rec.Body)
	}
	if n := countRows(t, h, "SELECT COUNT(*) FROM participants WHERE legacy_user_id = ? AND is_creator", userID); n != 1 {
		t.Errorf("Expected the requester to be the creator, got %d rows", n)
	}
}

func TestSetCookie_Secure(t *testing.T) {
	tests := map[string]struct {
		configure func(*http.Request)
		want      bool
	}{
		"plain http":   {func(*http.Request) {}, false},
		"behind proxy": {func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") }, true},
		"direct tls":   {func(r *http.Request) { r.TLS = &tls.ConnectionState{} }, true},
		"proxy, http":  {func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "http") }, false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			tt.configure(req)
			rec := httptest.NewRecorder()
			setCookie(rec, req, "id")
			if got := rec.Result().Cookies()[0].Secure; got != tt.want {
				t.Errorf("Secure = %v, want %v", got, tt.want)
			}
		})
	}
}
