package handler

import (
	"log"
	"net/http"

	"github.com/civilfritz/civilsort/internal/model"
	"github.com/civilfritz/civilsort/internal/protocol"
	"github.com/civilfritz/civilsort/internal/util"
	"github.com/coder/websocket"
)

// requireCreator is requireParticipant for creator-only actions: a
// participant who isn't the creator gets 403.
func (h *Handler) requireCreator(w http.ResponseWriter, r *http.Request, ballotID string) (ballot *model.Ballot, current participant, ok bool) {
	ballot, current, ok = h.requireParticipant(w, r, ballotID)
	if !ok {
		return nil, participant{}, false
	}
	if !current.IsCreator {
		http.Error(w, "Only the ballot's creator can do that", http.StatusForbidden)
		return nil, participant{}, false
	}
	return ballot, current, true
}

// targetParticipant loads the active participant named by the URL's
// participantID, writing 404 if there is none.
func (h *Handler) targetParticipant(w http.ResponseWriter, r *http.Request, ballotID string) (participant, bool) {
	p, ok, err := h.queryParticipant(r.Context(),
		`SELECT `+participantColumns+` FROM participants
		 WHERE ballot_id = ? AND id = ? AND removed_at IS NULL`,
		ballotID, r.PathValue("participantID"))
	if err != nil {
		log.Printf("Error loading participant: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return participant{}, false
	}
	if !ok {
		http.NotFound(w, r)
		return participant{}, false
	}
	return p, true
}

// HandleSetTitle renames the ballot.
func (h *Handler) HandleSetTitle(w http.ResponseWriter, r *http.Request) {
	ballotID := r.PathValue("id")
	if _, _, ok := h.requireCreator(w, r, ballotID); !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}
	title := cleanText(r.FormValue("title"), maxNameLength)

	if _, err := h.db.ExecContext(r.Context(), "UPDATE ballots SET title = ? WHERE id = ?", title, ballotID); err != nil {
		log.Printf("Error setting title: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	h.broadcastState(r.Context(), ballotID)
	w.WriteHeader(http.StatusNoContent)
}

// HandleSetVisibility makes rankings private (form private=1) or visible
// (private=0). Private rankings can't be made visible again once someone
// other than the creator has ranked, since they ranked expecting privacy.
func (h *Handler) HandleSetVisibility(w http.ResponseWriter, r *http.Request) {
	ballotID := r.PathValue("id")
	ballot, _, ok := h.requireCreator(w, r, ballotID)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}
	private := r.FormValue("private") == "1"

	if ballot.RankingsPrivate && !private {
		var othersRanked bool
		err := h.db.QueryRowContext(r.Context(),
			`SELECT EXISTS(
			   SELECT 1 FROM rankings r
			   JOIN participants p ON p.ballot_id = r.ballot_id AND p.id = r.participant_id
			   WHERE r.ballot_id = ? AND NOT p.is_creator)`,
			ballotID).Scan(&othersRanked)
		if err != nil {
			log.Printf("Error checking rankings: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		if othersRanked {
			http.Error(w, "Rankings must stay private; participants have already ranked expecting privacy", http.StatusConflict)
			return
		}
	}

	if _, err := h.db.ExecContext(r.Context(),
		"UPDATE ballots SET rankings_private = ? WHERE id = ?", private, ballotID); err != nil {
		log.Printf("Error setting visibility: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	h.broadcastState(r.Context(), ballotID)
	w.WriteHeader(http.StatusNoContent)
}

// HandleParticipantLink returns a participant's join link so the creator can
// send it to someone who lost theirs.
func (h *Handler) HandleParticipantLink(w http.ResponseWriter, r *http.Request) {
	ballotID := r.PathValue("id")
	if _, _, ok := h.requireCreator(w, r, ballotID); !ok {
		return
	}
	target, ok := h.targetParticipant(w, r, ballotID)
	if !ok {
		return
	}
	writeJSON(w, protocol.LinkResponse{Link: joinLink(ballotID, target)})
}

// HandleResetParticipant gives a participant a new secret, so their old link
// and cookies stop working, and returns the new link.
func (h *Handler) HandleResetParticipant(w http.ResponseWriter, r *http.Request) {
	ballotID := r.PathValue("id")
	_, current, ok := h.requireCreator(w, r, ballotID)
	if !ok {
		return
	}
	target, ok := h.targetParticipant(w, r, ballotID)
	if !ok {
		return
	}

	target.Secret = util.GenerateSecret()
	if _, err := h.db.ExecContext(r.Context(),
		"UPDATE participants SET secret = ? WHERE ballot_id = ? AND id = ?",
		target.Secret, ballotID, target.ID); err != nil {
		log.Printf("Error resetting participant link: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if target.ID == current.ID {
		// The creator reset their own link: keep this browser signed in.
		setParticipantCookie(w, r, ballotID, target.Secret)
	} else {
		h.hub.Disconnect(ballotID, target.ID, websocket.StatusCode(protocol.CloseNotParticipant))
	}
	writeJSON(w, protocol.LinkResponse{Link: joinLink(ballotID, target)})
}

// HandleRemoveParticipant removes a participant from the ballot. The form
// fields rankings=1 and items=1 also delete their rankings and the items
// they added; otherwise those stay, unattributed, and their rankings still
// count.
func (h *Handler) HandleRemoveParticipant(w http.ResponseWriter, r *http.Request) {
	ballotID := r.PathValue("id")
	_, current, ok := h.requireCreator(w, r, ballotID)
	if !ok {
		return
	}
	target, ok := h.targetParticipant(w, r, ballotID)
	if !ok {
		return
	}
	if target.ID == current.ID {
		http.Error(w, "The creator can't be removed", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Printf("Error starting transaction: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var stmts []string
	if r.FormValue("items") == "1" {
		stmts = append(stmts, "DELETE FROM items WHERE ballot_id = ? AND added_by = ?")
	}
	if r.FormValue("rankings") == "1" {
		stmts = append(stmts, "DELETE FROM rankings WHERE ballot_id = ? AND participant_id = ?")
	}
	stmts = append(stmts, "UPDATE participants SET secret = NULL, removed_at = datetime('now') WHERE ballot_id = ? AND id = ?")
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(r.Context(), stmt, ballotID, target.ID); err != nil {
			log.Printf("Error removing participant: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		log.Printf("Error committing removal: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	h.hub.Disconnect(ballotID, target.ID, websocket.StatusCode(protocol.CloseNotParticipant))
	h.broadcastParticipantsChanged(r.Context(), ballotID)
	w.WriteHeader(http.StatusNoContent)
}

// HandleDeleteBallot deletes the ballot and everything on it.
func (h *Handler) HandleDeleteBallot(w http.ResponseWriter, r *http.Request) {
	ballotID := r.PathValue("id")
	if _, _, ok := h.requireCreator(w, r, ballotID); !ok {
		return
	}

	if _, err := h.db.ExecContext(r.Context(), "DELETE FROM ballots WHERE id = ?", ballotID); err != nil {
		log.Printf("Error deleting ballot: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Queued messages are still delivered before each socket closes.
	h.send(ballotID, protocol.NewBallotDeletedMessage())
	h.hub.CloseBallot(ballotID, websocket.StatusCode(protocol.CloseBallotDeleted))
	w.WriteHeader(http.StatusNoContent)
}
