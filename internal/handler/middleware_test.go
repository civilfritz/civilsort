package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
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

	// User should exist in database
	var count int
	err := h.db.QueryRow("SELECT COUNT(*) FROM users WHERE id = ?", gotUserID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query database: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 user in database, got %d", count)
	}
}

func TestUserMiddleware_ExistingUser(t *testing.T) {
	h := testHandler(t)

	// Create a user in the database
	userID := generateUUID()
	_, err := h.db.Exec("INSERT INTO users (id) VALUES (?)", userID)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

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

	// Should not create a duplicate user
	var count int
	err = h.db.QueryRow("SELECT COUNT(*) FROM users WHERE id = ?", userID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query database: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 user in database, got %d", count)
	}
}

func TestUserMiddleware_InvalidCookie(t *testing.T) {
	h := testHandler(t)

	// Create a request with a cookie that references a non-existent user
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
		t.Error("Should have created a new user ID, not used the invalid one")
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

func TestUserID_FromContext(t *testing.T) {
	req, _, userID := authedRequest(t, testHandler(t), "GET", "/", nil)

	gotUserID := UserID(req.Context())
	if gotUserID != userID {
		t.Errorf("Expected userID %s, got %s", userID, gotUserID)
	}
}

func TestIsFreshCookie_FromContext(t *testing.T) {
	req, _, _ := authedRequest(t, testHandler(t), "GET", "/", nil)

	// authedRequest sets fresh=false
	if IsFreshCookie(req.Context()) {
		t.Error("Expected IsFreshCookie to return false")
	}
}
