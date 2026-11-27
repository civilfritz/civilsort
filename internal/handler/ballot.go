package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/civilfritz/civilsort/internal/model"
	"github.com/civilfritz/civilsort/internal/protocol"
	"github.com/civilfritz/civilsort/internal/schulze"
	"github.com/civilfritz/civilsort/internal/util"
)

// HandleHome shows the landing page with the create ballot form.
func (h *Handler) HandleHome(w http.ResponseWriter, r *http.Request) {
	if err := h.templates.ExecuteTemplate(w, "home.html", nil); err != nil {
		log.Printf("Error rendering home: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HandleCreateBallot creates a new ballot with the requester as its
// creator and redirects to it.
func (h *Handler) HandleCreateBallot(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	title := cleanText(r.FormValue("title"), maxNameLength)
	name := cleanText(r.FormValue("name"), maxDisplayNameLength)
	if name == "" {
		http.Error(w, "Please enter your name.", http.StatusBadRequest)
		return
	}
	ballotID := generateBallotID()

	_, err := h.db.ExecContext(r.Context(),
		"INSERT INTO ballots (id, title) VALUES (?, ?)",
		ballotID, title)
	if err != nil {
		log.Printf("Error creating ballot: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	creator, err := h.addParticipant(r.Context(), ballotID, name, true)
	if err != nil {
		log.Printf("Error adding creator as participant: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	setParticipantCookie(w, r, ballotID, creator.Secret)
	http.Redirect(w, r, "/ballot/"+ballotID, http.StatusSeeOther)
}

// generateBallotID generates a short random ID for URLs.
func generateBallotID() string {
	return util.GenerateShortID()
}

// HandleBallot shows the ballot page with items, ranking, and results.
func (h *Handler) HandleBallot(w http.ResponseWriter, r *http.Request) {
	ballotID := r.PathValue("id")

	ballot := h.loadBallot(w, r, ballotID)
	if ballot == nil {
		return
	}

	current, isParticipant, err := h.currentParticipant(w, r, ballotID)
	if err != nil {
		log.Printf("Error fetching participant: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Anyone with the link can view; only participants get a cookie. Viewing
	// never writes to the database.
	role := "viewer"
	if isParticipant {
		role = "participant"
		if current.IsCreator {
			role = "creator"
		}
		// Refresh the cookie so it expires a year after the last visit.
		setParticipantCookie(w, r, ballotID, current.Secret)
	}

	// 4. Render ballot
	// Get all items with display names
	items, err := h.getItems(r.Context(), ballotID)
	if err != nil {
		log.Printf("Error fetching items: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Split the participant's items into their ranked order and the rest.
	// Viewers have no rankings, so everything is unranked.
	var rankings []model.Ranking
	if isParticipant {
		rankings, err = h.getRankings(r.Context(), ballotID, current.ID)
		if err != nil {
			log.Printf("Error fetching rankings: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}
	byID := make(map[int64]itemView, len(items))
	for _, item := range items {
		// Shown on own items and, for the creator, on all; the server also
		// refuses non-creators once someone else has ranked the item.
		byID[item.ID] = itemView{Item: item, CanDelete: current.IsCreator || (isParticipant && item.AddedBy == current.ID)}
	}
	rankedItems := make([]itemView, 0, len(rankings))
	for _, ranking := range rankings { // already in position order
		if v, ok := byID[ranking.ItemID]; ok {
			rankedItems = append(rankedItems, v)
			delete(byID, ranking.ItemID)
		}
	}
	unrankedItems := make([]itemView, 0, len(byID))
	for _, item := range items {
		if v, ok := byID[item.ID]; ok {
			unrankedItems = append(unrankedItems, v)
		}
	}

	// Compute results
	results, err := h.computeResults(r.Context(), ballotID, items)
	if err != nil {
		log.Printf("Error computing results: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get participants
	participants, err := h.getParticipants(r.Context(), ballotID)
	if err != nil {
		log.Printf("Error fetching participants: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Count anonymous participants
	anonymousCount := 0
	for _, p := range participants {
		if p.DisplayName == "" {
			anonymousCount++
		}
	}

	data := map[string]interface{}{
		"Ballot":               ballot,
		"RankedItems":          rankedItems,
		"UnrankedItems":        unrankedItems,
		"Results":              results,
		"IsOpen":               ballot.IsOpen,
		"ParticipantCount":     len(participants),
		"CurrentParticipantID": current.ID,
		"JoinLink":             joinLink(ballotID, current),
		"Role":                 role,
		"Items":                items,
		"Notice":               r.URL.Query().Get("notice") == "not-participant",
		"RankingsPrivate":      ballot.RankingsPrivate,
		"Participants":         participants,
		"DisplayName":          current.DisplayName,
		"AnonymousCount":       anonymousCount,
	}

	if err := h.templates.ExecuteTemplate(w, "ballot.html", data); err != nil {
		log.Printf("Error rendering ballot: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// itemView is an item as rendered for the current requester.
type itemView struct {
	model.Item
	CanDelete bool
}

// HandleAddItem adds a new item to the ballot.
func (h *Handler) HandleAddItem(w http.ResponseWriter, r *http.Request) {
	ballotID := r.PathValue("id")

	_, current, ok := h.requireParticipant(w, r, ballotID)
	if !ok {
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	itemName := cleanText(r.FormValue("name"), maxNameLength)
	if itemName == "" {
		http.Error(w, "Item name cannot be empty", http.StatusBadRequest)
		return
	}

	_, err := h.db.ExecContext(r.Context(),
		"INSERT INTO items (ballot_id, name, added_by) VALUES (?, ?, ?)",
		ballotID, itemName, current.ID)
	if err != nil {
		log.Printf("Error adding item: %v", err)
		http.Error(w, "Could not add item (duplicate name?)", http.StatusBadRequest)
		return
	}

	// Broadcast that items changed
	h.broadcastItemsChanged(r.Context(), ballotID)

	w.WriteHeader(http.StatusNoContent)
}

// HandleDeleteItem deletes an item.
func (h *Handler) HandleDeleteItem(w http.ResponseWriter, r *http.Request) {
	ballotID := r.PathValue("id")

	_, current, ok := h.requireParticipant(w, r, ballotID)
	if !ok {
		return
	}

	itemID := r.PathValue("itemID")

	// Check ownership
	var addedBy string
	err := h.db.QueryRowContext(r.Context(),
		"SELECT added_by FROM items WHERE id = ? AND ballot_id = ?",
		itemID, ballotID).Scan(&addedBy)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	} else if err != nil {
		log.Printf("Error checking item ownership: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// The creator can delete anything. Others can delete their own items
	// until another participant has ranked them.
	if !current.IsCreator {
		if addedBy != current.ID {
			http.Error(w, "You can only delete items you added", http.StatusForbidden)
			return
		}
		var rankedByOthers bool
		err = h.db.QueryRowContext(r.Context(),
			"SELECT EXISTS(SELECT 1 FROM rankings WHERE item_id = ? AND participant_id != ?)",
			itemID, current.ID).Scan(&rankedByOthers)
		if err != nil {
			log.Printf("Error checking item rankings: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		if rankedByOthers {
			http.Error(w, "Someone else has ranked this item. Ask the ballot's creator to delete it.", http.StatusForbidden)
			return
		}
	}

	_, err = h.db.ExecContext(r.Context(), "DELETE FROM items WHERE id = ?", itemID)
	if err != nil {
		log.Printf("Error deleting item: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Broadcast that items changed
	h.broadcastItemsChanged(r.Context(), ballotID)

	w.WriteHeader(http.StatusNoContent)
}

// HandleSaveRankings saves the user's ranking (JSON API).
func (h *Handler) HandleSaveRankings(w http.ResponseWriter, r *http.Request) {
	ballotID := r.PathValue("id")

	_, current, ok := h.requireParticipant(w, r, ballotID)
	if !ok {
		return
	}

	// Parse JSON body
	var req protocol.SaveRankingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Save rankings in a transaction
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Printf("Error starting transaction: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if msg, err := validateOrder(r.Context(), tx, ballotID, req.Order); err != nil {
		log.Printf("Error validating ranking: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	} else if msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	// Delete existing rankings for this user on this ballot
	_, err = tx.ExecContext(r.Context(),
		"DELETE FROM rankings WHERE ballot_id = ? AND participant_id = ?",
		ballotID, current.ID)
	if err != nil {
		log.Printf("Error deleting old rankings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Insert new rankings
	stmt, err := tx.PrepareContext(r.Context(),
		"INSERT INTO rankings (ballot_id, participant_id, item_id, position) VALUES (?, ?, ?, ?)")
	if err != nil {
		log.Printf("Error preparing statement: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	for i, itemID := range req.Order {
		_, err := stmt.ExecContext(r.Context(), ballotID, current.ID, itemID, i+1)
		if err != nil {
			log.Printf("Error inserting ranking: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Error committing transaction: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Broadcast results update (ranking changed, no item changes)
	h.broadcastResults(r.Context(), ballotID)

	w.WriteHeader(http.StatusNoContent)
}

// HandleToggleOpen toggles the ballot's open/closed state.
func (h *Handler) HandleToggleOpen(w http.ResponseWriter, r *http.Request) {
	ballotID := r.PathValue("id")

	if _, _, ok := h.requireCreator(w, r, ballotID); !ok {
		return
	}

	// Toggle: flip is_open between 0 and 1
	var newIsOpenInt int
	err := h.db.QueryRowContext(r.Context(),
		"UPDATE ballots SET is_open = NOT is_open WHERE id = ? RETURNING is_open",
		ballotID).Scan(&newIsOpenInt)
	if err != nil {
		log.Printf("Error toggling ballot state: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	newIsOpen := newIsOpenInt == 1

	// Broadcast state change via WebSocket
	h.broadcastState(r.Context(), ballotID)

	// Return JSON with the new state
	writeJSON(w, protocol.ToggleResponse{IsOpen: newIsOpen})
}

// HandleSetName sets the display name for the current user.
func (h *Handler) HandleSetName(w http.ResponseWriter, r *http.Request) {
	ballotID := r.PathValue("id")

	_, current, ok := h.requireParticipant(w, r, ballotID)
	if !ok {
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	displayName := cleanText(r.FormValue("name"), maxDisplayNameLength)

	_, err := h.db.ExecContext(r.Context(),
		"UPDATE participants SET display_name = ? WHERE ballot_id = ? AND id = ?",
		displayName, ballotID, current.ID)
	if err != nil {
		log.Printf("Error updating display name: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Broadcast participants_changed message
	h.broadcastParticipantsChanged(r.Context(), ballotID)

	w.WriteHeader(http.StatusNoContent)
}

// loadBallot fetches a ballot by ID. On failure it writes a 404 or 500 and
// returns nil.
func (h *Handler) loadBallot(w http.ResponseWriter, r *http.Request, ballotID string) *model.Ballot {
	var ballot model.Ballot
	err := h.db.QueryRowContext(r.Context(),
		"SELECT id, title, is_open, rankings_private, created_at FROM ballots WHERE id = ?",
		ballotID).Scan(&ballot.ID, &ballot.Title, &ballot.IsOpen, &ballot.RankingsPrivate, &ballot.CreatedAt)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return nil
	} else if err != nil {
		log.Printf("Error fetching ballot: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return nil
	}
	return &ballot
}

// getItems retrieves all items for a ballot with display names.
func (h *Handler) getItems(ctx context.Context, ballotID string) ([]model.Item, error) {
	rows, err := h.db.QueryContext(ctx,
		`SELECT i.id, i.ballot_id, i.name, i.added_by, i.created_at, COALESCE(p.display_name, '')
		 FROM items i
		 LEFT JOIN participants p
		   ON p.ballot_id = i.ballot_id AND p.id = i.added_by AND p.removed_at IS NULL
		 WHERE i.ballot_id = ?
		 ORDER BY i.id`,
		ballotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []model.Item
	for rows.Next() {
		var item model.Item
		if err := rows.Scan(&item.ID, &item.BallotID, &item.Name, &item.AddedBy, &item.CreatedAt, &item.AddedByName); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// getParticipants retrieves all participants for a ballot with their display names.
// Only includes participants who have set a name, added an item, or ranked items.
func (h *Handler) getParticipants(ctx context.Context, ballotID string) ([]model.Participant, error) {
	rows, err := h.db.QueryContext(ctx,
		`SELECT p.id, p.display_name
		 FROM participants p
		 WHERE p.ballot_id = ? AND p.removed_at IS NULL
		 AND (
		     p.display_name != ''
		     OR EXISTS (SELECT 1 FROM items WHERE ballot_id = p.ballot_id AND added_by = p.id)
		     OR EXISTS (SELECT 1 FROM rankings WHERE ballot_id = p.ballot_id AND participant_id = p.id)
		 )
		 ORDER BY p.created_at, p.rowid`,
		ballotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var participants []model.Participant
	for rows.Next() {
		var p model.Participant
		if err := rows.Scan(&p.ID, &p.DisplayName); err != nil {
			return nil, err
		}
		participants = append(participants, p)
	}
	return participants, rows.Err()
}

// getRankings retrieves a participant's rankings on a ballot, in order.
func (h *Handler) getRankings(ctx context.Context, ballotID, participantID string) ([]model.Ranking, error) {
	rows, err := h.db.QueryContext(ctx,
		`SELECT ballot_id, participant_id, item_id, position FROM rankings
		 WHERE ballot_id = ? AND participant_id = ? ORDER BY position`,
		ballotID, participantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rankings []model.Ranking
	for rows.Next() {
		var r model.Ranking
		if err := rows.Scan(&r.BallotID, &r.ParticipantID, &r.ItemID, &r.Position); err != nil {
			return nil, err
		}
		rankings = append(rankings, r)
	}
	return rankings, rows.Err()
}

// computeResults computes the Schulze ranking for a ballot.
func (h *Handler) computeResults(ctx context.Context, ballotID string, items []model.Item) ([]protocol.ResultEntry, error) {
	if len(items) == 0 {
		return []protocol.ResultEntry{}, nil
	}

	// Build item index mapping
	idToIndex := make(map[int64]int)
	indexToItem := make(map[int]model.Item)
	for i, item := range items {
		idToIndex[item.ID] = i
		indexToItem[i] = item
	}

	// Get all rankings for this ballot (all participants)
	rows, err := h.db.QueryContext(ctx,
		"SELECT participant_id, item_id, position FROM rankings WHERE ballot_id = ?",
		ballotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Build preferences by user
	prefsByUser := make(map[string]schulze.Preference)
	for rows.Next() {
		var userID string
		var itemID int64
		var position int
		if err := rows.Scan(&userID, &itemID, &position); err != nil {
			return nil, err
		}

		if _, ok := prefsByUser[userID]; !ok {
			prefsByUser[userID] = make(schulze.Preference)
		}
		if idx, ok := idToIndex[itemID]; ok {
			prefsByUser[userID][idx] = position
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Convert to slice of preferences
	prefs := make([]schulze.Preference, 0, len(prefsByUser))
	for _, p := range prefsByUser {
		prefs = append(prefs, p)
	}

	// Compute Schulze results
	results := schulze.Compute(len(items), prefs)

	// Convert to protocol.ResultEntry
	entries := make([]protocol.ResultEntry, len(results))
	for i, r := range results {
		entries[i] = protocol.ResultEntry{
			Rank: r.Rank,
			Name: indexToItem[r.CandidateIndex].Name,
		}
	}

	return entries, nil
}

// ballotSnapshot is the ballot state carried by broadcast messages.
type ballotSnapshot struct {
	items        []protocol.ItemEntry
	results      []protocol.ResultEntry
	participants []protocol.ParticipantEntry
}

// snapshot loads items, computes results, and lists participants for a ballot.
func (h *Handler) snapshot(ctx context.Context, ballotID string) (ballotSnapshot, error) {
	items, err := h.getItems(ctx, ballotID)
	if err != nil {
		return ballotSnapshot{}, fmt.Errorf("getting items: %w", err)
	}

	results, err := h.computeResults(ctx, ballotID, items)
	if err != nil {
		return ballotSnapshot{}, fmt.Errorf("computing results: %w", err)
	}

	participants, err := h.getParticipants(ctx, ballotID)
	if err != nil {
		return ballotSnapshot{}, fmt.Errorf("getting participants: %w", err)
	}

	itemEntries := make([]protocol.ItemEntry, len(items))
	for i, item := range items {
		itemEntries[i] = protocol.ItemEntry{
			ID:          item.ID,
			Name:        item.Name,
			AddedBy:     item.AddedBy,
			AddedByName: item.AddedByName,
		}
	}

	participantEntries := make([]protocol.ParticipantEntry, len(participants))
	for i, p := range participants {
		participantEntries[i] = protocol.ParticipantEntry{
			DisplayName:   p.DisplayName,
			ParticipantID: p.ID,
		}
	}

	return ballotSnapshot{
		items:        itemEntries,
		results:      results,
		participants: participantEntries,
	}, nil
}

// broadcastResults broadcasts updated results after a ranking change.
func (h *Handler) broadcastResults(ctx context.Context, ballotID string) {
	s, err := h.snapshot(ctx, ballotID)
	if err != nil {
		log.Printf("Error building results broadcast: %v", err)
		return
	}
	h.send(ballotID, protocol.NewResultsMessage(s.results, len(s.participants)))
}

// broadcastItemsChanged broadcasts the item list after an add or delete.
func (h *Handler) broadcastItemsChanged(ctx context.Context, ballotID string) {
	s, err := h.snapshot(ctx, ballotID)
	if err != nil {
		log.Printf("Error building items_changed broadcast: %v", err)
		return
	}
	h.send(ballotID, protocol.NewItemsChangedMessage(s.results, s.items, len(s.participants)))
}

// broadcastParticipantsChanged broadcasts the participant list, items, and results.
func (h *Handler) broadcastParticipantsChanged(ctx context.Context, ballotID string) {
	s, err := h.snapshot(ctx, ballotID)
	if err != nil {
		log.Printf("Error building participants_changed broadcast: %v", err)
		return
	}
	h.send(ballotID, protocol.NewParticipantsChangedMessage(s.results, s.items, s.participants))
}

// broadcastState broadcasts the ballot's current creator-controlled settings.
func (h *Handler) broadcastState(ctx context.Context, ballotID string) {
	var b model.Ballot
	err := h.db.QueryRowContext(ctx,
		"SELECT is_open, title, rankings_private FROM ballots WHERE id = ?", ballotID,
	).Scan(&b.IsOpen, &b.Title, &b.RankingsPrivate)
	if err != nil {
		log.Printf("Error loading ballot state for broadcast: %v", err)
		return
	}
	h.send(ballotID, protocol.NewStateChangedMessage(b.IsOpen, b.Title, b.RankingsPrivate))
}

// send marshals msg and broadcasts it to all WebSocket clients on a ballot.
func (h *Handler) send(ballotID string, msg any) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Error marshaling broadcast: %v", err)
		return
	}
	h.hub.Broadcast(ballotID, data)
}

// HandleGetRankings returns a participant's ranked items in order.
// Only participants of the ballot may view each other's rankings, and only
// while the ballot's rankings are visible.
func (h *Handler) HandleGetRankings(w http.ResponseWriter, r *http.Request) {
	ballotID := r.PathValue("id")
	targetID := r.PathValue("participantID")

	ballot, current, ok := h.requireParticipant(w, r, ballotID)
	if !ok {
		return
	}
	if ballot.RankingsPrivate && targetID != current.ID {
		http.Error(w, "Rankings on this ballot are private.", http.StatusForbidden)
		return
	}

	var exists bool
	err := h.db.QueryRowContext(r.Context(),
		"SELECT EXISTS(SELECT 1 FROM participants WHERE ballot_id = ? AND id = ? AND removed_at IS NULL)",
		ballotID, targetID).Scan(&exists)
	if err != nil {
		log.Printf("Error resolving participant: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if !exists {
		http.NotFound(w, r)
		return
	}

	rankings, err := h.getRankings(r.Context(), ballotID, targetID)
	if err != nil {
		log.Printf("Error fetching rankings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get all items to look up names
	items, err := h.getItems(r.Context(), ballotID)
	if err != nil {
		log.Printf("Error fetching items: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Build item ID to name map
	itemNames := make(map[int64]string)
	for _, item := range items {
		itemNames[item.ID] = item.Name
	}

	rankedItems := make([]protocol.RankedItem, len(rankings))
	for i, ranking := range rankings {
		rankedItems[i] = protocol.RankedItem{Name: itemNames[ranking.ItemID]}
	}

	writeJSON(w, rankedItems)
}

// writeJSON encodes v as the JSON response body.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("Error writing JSON response: %v", err)
	}
}

const (
	maxNameLength        = 200 // ballot titles and item names
	maxDisplayNameLength = 50
)

// cleanText trims s, replaces invalid UTF-8, and truncates it to at most limit
// characters without splitting one.
func cleanText(s string, limit int) string {
	s = strings.TrimSpace(strings.ToValidUTF8(s, "\uFFFD"))
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:limit]))
}

// validateOrder checks that order lists distinct items of ballotID. It returns
// a client-facing message when the order is invalid.
func validateOrder(ctx context.Context, tx *sql.Tx, ballotID string, order []int64) (string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id FROM items WHERE ballot_id = ?", ballotID)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	valid := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		valid[id] = true
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	seen := make(map[int64]bool, len(order))
	for _, id := range order {
		if !valid[id] {
			return fmt.Sprintf("Item %d is not on this ballot", id), nil
		}
		if seen[id] {
			return fmt.Sprintf("Item %d is ranked more than once", id), nil
		}
		seen[id] = true
	}
	return "", nil
}
