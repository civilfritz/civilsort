package handler

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/civilfritz/civilsort/internal/util"
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

// currentParticipant returns the requester's participant on ballotID. ok is
// false when the requester isn't an active participant.
func (h *Handler) currentParticipant(r *http.Request, ballotID string) (participant, bool, error) {
	return h.participantByLegacyUser(r.Context(), ballotID, UserID(r.Context()))
}

// participantByLegacyUser finds the active participant on ballotID belonging
// to a voter_id user.
func (h *Handler) participantByLegacyUser(ctx context.Context, ballotID, userID string) (participant, bool, error) {
	return h.queryParticipant(ctx,
		`SELECT `+participantColumns+` FROM participants
		 WHERE ballot_id = ? AND legacy_user_id = ? AND removed_at IS NULL`,
		ballotID, userID)
}

// addParticipant makes userID a participant of ballotID, returning the
// existing participant if there is one. isNew reports whether one was created.
func (h *Handler) addParticipant(ctx context.Context, ballotID, userID string, isCreator bool) (p participant, isNew bool, err error) {
	result, err := h.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO participants (ballot_id, id, secret, is_creator, legacy_user_id)
		 VALUES (?, ?, ?, ?, ?)`,
		ballotID, util.GenerateShortID(), util.GenerateShortID(), isCreator, userID)
	if err != nil {
		return participant{}, false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return participant{}, false, err
	}

	p, ok, err := h.participantByLegacyUser(ctx, ballotID, userID)
	if err != nil {
		return participant{}, false, err
	}
	if !ok {
		return participant{}, false, sql.ErrNoRows
	}
	return p, n > 0, nil
}
