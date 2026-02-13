package handler

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"log"
	"math/big"
	"net/http"
)

const cookieName = "voter_id"

type contextKey string

const userIDKey contextKey = "userID"

// UserMiddleware ensures every request has a user ID.
// If the voter_id cookie doesn't exist, it creates a new user and sets the cookie.
func (h *Handler) UserMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var userID string

		cookie, err := r.Cookie(cookieName)
		if err != nil || cookie.Value == "" {
			// Generate new user
			userID = generateUUID()

			// Insert into users table
			_, err := h.db.ExecContext(r.Context(), "INSERT OR IGNORE INTO users (id) VALUES (?)", userID)
			if err != nil {
				log.Printf("Error creating user: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			http.SetCookie(w, &http.Cookie{
				Name:     cookieName,
				Value:    userID,
				Path:     "/",
				MaxAge:   365 * 24 * 60 * 60, // 1 year
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
		} else {
			userID = cookie.Value

			// Verify user exists in database
			var exists bool
			err := h.db.QueryRowContext(r.Context(), "SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)", userID).Scan(&exists)
			if err != nil || !exists {
				// Cookie exists but user doesn't - treat as new user
				userID = generateUUID()
				h.db.ExecContext(r.Context(), "INSERT OR IGNORE INTO users (id) VALUES (?)", userID)
				http.SetCookie(w, &http.Cookie{
					Name:     cookieName,
					Value:    userID,
					Path:     "/",
					MaxAge:   365 * 24 * 60 * 60,
					HttpOnly: true,
					SameSite: http.SameSiteLaxMode,
				})
			}
		}

		ctx := context.WithValue(r.Context(), userIDKey, userID)
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

// generateUUID generates a UUID v4.
func generateUUID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 2
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// generateBallotID generates a short random ID for URLs.
func generateBallotID() string {
	const charset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	b := make([]byte, 8)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			panic(err)
		}
		b[i] = charset[n.Int64()]
	}
	return string(b)
}

// getBallot retrieves a ballot by ID.
func (h *Handler) getBallot(ctx context.Context, ballotID string) (*sql.Row, error) {
	return h.db.QueryRowContext(ctx,
		"SELECT id, title, created_by, created_at FROM ballots WHERE id = ?",
		ballotID), nil
}
