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
// database, so crawlers and one-off visits leave no trace.
//
// A request is "fresh" when its user hasn't joined any ballot; HandleBallot
// lets such a browser adopt a participant link.
func (h *Handler) UserMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var userID string
		if cookie, err := r.Cookie(cookieName); err == nil && util.IsUUID(cookie.Value) {
			userID = cookie.Value
		} else {
			userID = util.GenerateUUID()
			setCookie(w, r, userID)
		}

		var exists bool
		err := h.db.QueryRowContext(r.Context(), "SELECT EXISTS(SELECT 1 FROM participants WHERE legacy_user_id = ?)", userID).Scan(&exists)
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
func setCookie(w http.ResponseWriter, r *http.Request, userID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    userID,
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60, // 1 year
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// isHTTPS reports whether the client connected over HTTPS, directly or via a
// TLS-terminating proxy such as Caddy. A spoofed header only makes the
// spoofer's own cookie Secure.
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// generateBallotID generates a short random ID for URLs.
func generateBallotID() string {
	return util.GenerateShortID()
}
