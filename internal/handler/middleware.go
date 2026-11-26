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

// UserMiddleware ensures every request has a user ID, issuing a voter_id
// cookie when the request has no well-formed one. It doesn't write to the
// database: the users row is created by ensureUser when the user first joins
// or creates a ballot, so crawlers and one-off visits leave no trace.
//
// A request is "fresh" when its user has no row yet, i.e. hasn't joined any
// ballot; HandleBallot lets such a browser adopt a participant link.
func (h *Handler) UserMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var userID string
		if cookie, err := r.Cookie(cookieName); err == nil && util.IsUUID(cookie.Value) {
			userID = cookie.Value
		} else {
			userID = util.GenerateUUID()
			setCookie(w, userID)
		}

		var exists bool
		err := h.db.QueryRowContext(r.Context(), "SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)", userID).Scan(&exists)
		if err != nil {
			log.Printf("Error checking user: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		ctx := context.WithValue(r.Context(), userIDKey, userID)
		ctx = context.WithValue(ctx, freshCookieKey, !exists)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ensureUser creates the users row for userID if it doesn't exist yet.
func (h *Handler) ensureUser(ctx context.Context, userID string) error {
	_, err := h.db.ExecContext(ctx, "INSERT OR IGNORE INTO users (id) VALUES (?)", userID)
	return err
}

// UserID extracts the user ID from the request context.
func UserID(ctx context.Context) string {
	if id, ok := ctx.Value(userIDKey).(string); ok {
		return id
	}
	return ""
}

// IsFreshCookie reports whether the request's user hasn't joined any ballot yet.
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
