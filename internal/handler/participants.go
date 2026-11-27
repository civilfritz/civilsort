package handler

import (
	"context"
	"database/sql"
	"log"
	"net/http"

	"github.com/civilfritz/civilsort/internal/model"
	"github.com/civilfritz/civilsort/internal/util"
)

const (
	// participantCookie holds a participant's secret. It is scoped to
	// /ballot/{id}, so each ballot only ever receives its own.
	participantCookie = "participant"

	// legacyCookie is the global user ID cookie used before identity was per
	// ballot. It is only read, to upgrade existing browsers.
	legacyCookie = "voter_id"

	cookieMaxAge = 365 * 24 * 60 * 60 // 1 year, refreshed on each page view
)

// participant is a requester's membership in one ballot.
type participant struct {
	ID          string // safe to show to others
	Secret      string // credential; never send to other clients
	DisplayName string
	IsCreator   bool
}

const participantColumns = `id, COALESCE(secret, ''), display_name, is_creator`

// queryParticipant runs a single-row participants query selecting
// participantColumns. ok is false when no row matches.
func (h *Handler) queryParticipant(ctx context.Context, query string, args ...any) (p participant, ok bool, err error) {
	err = h.db.QueryRowContext(ctx, query, args...).Scan(&p.ID, &p.Secret, &p.DisplayName, &p.IsCreator)
	if err == sql.ErrNoRows {
		return participant{}, false, nil
	}
	if err != nil {
		return participant{}, false, err
	}
	return p, true, nil
}

// currentParticipant returns the requester's participant on ballotID from
// their participant cookie. ok is false when they aren't an active
// participant.
//
// A browser from before per-ballot identity has only a voter_id cookie; if
// it maps to a participant here, that participant's cookie is issued.
//
// TODO(v1.0): Remove the voter_id fallback (here and legacyCookie) and drop
// participants.legacy_user_id in a new schema version. It only upgrades
// browsers that joined ballots before per-ballot cookies.
func (h *Handler) currentParticipant(w http.ResponseWriter, r *http.Request, ballotID string) (participant, bool, error) {
	if c, err := r.Cookie(participantCookie); err == nil && c.Value != "" {
		p, ok, err := h.participantBySecret(r.Context(), ballotID, c.Value)
		if err != nil || ok {
			return p, ok, err
		}
	}

	c, err := r.Cookie(legacyCookie)
	if err != nil || !util.IsUUID(c.Value) {
		return participant{}, false, nil
	}
	p, ok, err := h.queryParticipant(r.Context(),
		`SELECT `+participantColumns+` FROM participants
		 WHERE ballot_id = ? AND legacy_user_id = ? AND removed_at IS NULL AND secret IS NOT NULL`,
		ballotID, c.Value)
	if err != nil || !ok {
		return participant{}, false, err
	}
	setParticipantCookie(w, r, ballotID, p.Secret)
	return p, true, nil
}

// participantBySecret finds the active participant on ballotID with secret.
func (h *Handler) participantBySecret(ctx context.Context, ballotID, secret string) (participant, bool, error) {
	return h.queryParticipant(ctx,
		`SELECT `+participantColumns+` FROM participants
		 WHERE ballot_id = ? AND secret = ? AND removed_at IS NULL`,
		ballotID, secret)
}

// addParticipant creates a new participant on ballotID.
func (h *Handler) addParticipant(ctx context.Context, ballotID, name string, isCreator bool) (participant, error) {
	p := participant{ID: util.GenerateShortID(), Secret: util.GenerateSecret(), DisplayName: name, IsCreator: isCreator}
	_, err := h.db.ExecContext(ctx,
		`INSERT INTO participants (ballot_id, id, secret, display_name, is_creator) VALUES (?, ?, ?, ?, ?)`,
		ballotID, p.ID, p.Secret, name, isCreator)
	if err != nil {
		return participant{}, err
	}
	return p, nil
}

// HandleJoinLink signs a browser in as the participant whose secret is in
// the URL, then redirects to the ballot so the secret leaves the address bar.
// It serves both /ballot/{id}/join/{secret} and the legacy
// /ballot/{id}/{legacySecret} links.
func (h *Handler) HandleJoinLink(w http.ResponseWriter, r *http.Request) {
	ballotID := r.PathValue("id")
	secret := r.PathValue("secret")
	if secret == "" {
		secret = r.PathValue("legacySecret")
	}

	_, ok, err := h.participantBySecret(r.Context(), ballotID, secret)
	if err != nil {
		log.Printf("Error resolving join link: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "Link no longer valid; ask the ballot's creator for a new one", http.StatusNotFound)
		return
	}

	setParticipantCookie(w, r, ballotID, secret)
	http.Redirect(w, r, "/ballot/"+ballotID, http.StatusSeeOther)
}

// joinLink is the path that signs another browser in as p.
func joinLink(ballotID string, p participant) string {
	return "/ballot/" + ballotID + "/join/" + p.Secret
}

// setParticipantCookie stores a participant's secret for one ballot.
func setParticipantCookie(w http.ResponseWriter, r *http.Request, ballotID, secret string) {
	http.SetCookie(w, &http.Cookie{
		Name:     participantCookie,
		Value:    secret,
		Path:     "/ballot/" + ballotID,
		MaxAge:   cookieMaxAge,
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

// HandleJoin makes a viewer a participant of an open ballot, using the name
// they entered, and redirects back to the ballot.
func (h *Handler) HandleJoin(w http.ResponseWriter, r *http.Request) {
	ballotID := r.PathValue("id")
	ballot := h.loadBallot(w, r, ballotID)
	if ballot == nil {
		return
	}

	_, isParticipant, err := h.currentParticipant(w, r, ballotID)
	if err != nil {
		log.Printf("Error checking participant: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if isParticipant {
		http.Redirect(w, r, "/ballot/"+ballotID, http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}
	name := cleanText(r.FormValue("name"), maxDisplayNameLength)
	if name == "" {
		http.Error(w, "Name required", http.StatusBadRequest)
		return
	}
	if !ballot.IsOpen {
		http.Error(w, "Closed to new participants", http.StatusForbidden)
		return
	}

	p, err := h.addParticipant(r.Context(), ballotID, name, false)
	if err != nil {
		log.Printf("Error adding participant: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	setParticipantCookie(w, r, ballotID, p.Secret)
	h.broadcastParticipantsChanged(r.Context(), ballotID)
	http.Redirect(w, r, "/ballot/"+ballotID, http.StatusSeeOther)
}

// requireParticipant loads the ballot and the requester's participant on it.
// If either is missing it writes an error (401 for a non-participant) and ok
// is false.
func (h *Handler) requireParticipant(w http.ResponseWriter, r *http.Request, ballotID string) (ballot *model.Ballot, current participant, ok bool) {
	ballot = h.loadBallot(w, r, ballotID)
	if ballot == nil {
		return nil, participant{}, false
	}

	current, isParticipant, err := h.currentParticipant(w, r, ballotID)
	if err != nil {
		log.Printf("Error checking participant: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return nil, participant{}, false
	}
	if !isParticipant {
		notParticipant(w)
		return nil, participant{}, false
	}
	return ballot, current, true
}

// notParticipant responds 401: the requester has no participant on this
// ballot (never joined, removed, or lost their cookie). The page switches to
// the viewer view on this status.
func notParticipant(w http.ResponseWriter) {
	http.Error(w, "Not a participant on this ballot", http.StatusUnauthorized)
}
