package handler

import (
	"context"
	"log"
	"net/http"

	"github.com/civilfritz/civilsort/internal/util"
)

const cookieName = "voter_id"

type contextKey string

const userIDKey contextKey = "userID"
const freshCookieKey contextKey = "freshCookie"

// UserMiddleware ensures every request has a user ID.
// If the voter_id cookie doesn't exist, it creates a new user and sets the cookie.
func (h *Handler) UserMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var userID string
		var fresh bool

		cookie, err := r.Cookie(cookieName)
		if err != nil || cookie.Value == "" {
			// Generate new user
			userID = util.GenerateUUID()
			fresh = true

			// Insert into users table
			_, err := h.db.ExecContext(r.Context(), "INSERT OR IGNORE INTO users (id) VALUES (?)", userID)
			if err != nil {
				log.Printf("Error creating user: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			setCookie(w, userID)
		} else {
			userID = cookie.Value

			// Verify user exists in database
			var exists bool
			err := h.db.QueryRowContext(r.Context(), "SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)", userID).Scan(&exists)
			if err != nil || !exists {
				// Cookie exists but user doesn't - treat as new user
				userID = util.GenerateUUID()
				fresh = true
				h.db.ExecContext(r.Context(), "INSERT OR IGNORE INTO users (id) VALUES (?)", userID)
				setCookie(w, userID)
			} else {
				fresh = false
			}
		}

		ctx := context.WithValue(r.Context(), userIDKey, userID)
		ctx = context.WithValue(ctx, freshCookieKey, fresh)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserID extracts the user ID from the request context.
func UserID(ctx context.Context) string {
	if id, ok := ctx.Value(userIDKey).(string); ok {
		return id
	}
	return ""
}

// IsFreshCookie returns true if this request resulted in creating a new cookie/user.
func IsFreshCookie(ctx context.Context) bool {
	if fresh, ok := ctx.Value(freshCookieKey).(bool); ok {
		return fresh
	}
	return false
}

// setCookie sets the voter_id cookie with standard parameters.
func setCookie(w http.ResponseWriter, userID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    userID,
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60, // 1 year
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// generateBallotID generates a short random ID for URLs.
func generateBallotID() string {
	return util.GenerateShortID()
}

// generateParticipantID generates a short random ID for participant URLs.
func generateParticipantID() string {
	return util.GenerateShortID()
}