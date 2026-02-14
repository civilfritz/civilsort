package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"github.com/civilfritz/civilsort/internal/model"
	"github.com/civilfritz/civilsort/internal/schulze"
)

// HandleHome shows the landing page with the create ballot form.
func (h *Handler) HandleHome(w http.ResponseWriter, r *http.Request) {
	if err := h.templates.ExecuteTemplate(w, "home.html", nil); err != nil {
		log.Printf("Error rendering home: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HandleCreateBallot creates a new ballot and redirects to it.
func (h *Handler) HandleCreateBallot(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	title := r.FormValue("title")
	userID := UserID(r.Context())
	ballotID := generateBallotID()

	_, err := h.db.ExecContext(r.Context(),
		"INSERT INTO ballots (id, title, created_by) VALUES (?, ?, ?)",
		ballotID, title, userID)
	if err != nil {
		log.Printf("Error creating ballot: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Creator is automatically the first participant
	participantID, _, err := h.addParticipant(r.Context(), ballotID, userID)
	if err != nil {
		log.Printf("Error adding creator as participant: %v", err)
		// Non-fatal: redirect to base URL and they'll be added when they visit
		http.Redirect(w, r, "/ballot/"+ballotID, http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/ballot/"+ballotID+"/"+participantID, http.StatusSeeOther)
}

// HandleBallot shows the ballot page with items, ranking, and results.
// Handles both /ballot/{id} and /ballot/{id}/{participantID} routes.
func (h *Handler) HandleBallot(w http.ResponseWriter, r *http.Request) {
	ballotID := r.PathValue("id")
	participantID := r.PathValue("participantID") // empty for base URL
	userID := UserID(r.Context())

	// 1. Load ballot
	var ballot model.Ballot
	var isOpenInt int
	err := h.db.QueryRowContext(r.Context(),
		"SELECT id, title, is_open, created_by, created_at FROM ballots WHERE id = ?",
		ballotID).Scan(&ballot.ID, &ballot.Title, &isOpenInt, &ballot.CreatedBy, &ballot.CreatedAt)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	} else if err != nil {
		log.Printf("Error fetching ballot: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	ballot.IsOpen = isOpenInt == 1

	// 2. If participant URL with fresh cookie: adopt the identity
	if participantID != "" && IsFreshCookie(r.Context()) {
		var ownerID string
		err := h.db.QueryRowContext(r.Context(),
			"SELECT user_id FROM ballot_participants WHERE ballot_id = ? AND participant_id = ?",
			ballotID, participantID).Scan(&ownerID)
		if err == nil {
			// Found the owner - set cookie and redirect to same URL
			setCookie(w, ownerID)
			http.Redirect(w, r, r.URL.Path, http.StatusSeeOther)
			return
		}
		// Not found: fall through to normal flow
	}

	// 3. Check if current user is a participant
	var currentParticipantID string
	err = h.db.QueryRowContext(r.Context(),
		"SELECT participant_id FROM ballot_participants WHERE ballot_id = ? AND user_id = ?",
		ballotID, userID).Scan(&currentParticipantID)
	isParticipant := (err == nil)

	if isParticipant {
		// User is a participant
		// If URL doesn't match their participant ID, redirect
		if participantID == "" || participantID != currentParticipantID {
			http.Redirect(w, r, "/ballot/"+ballotID+"/"+currentParticipantID, http.StatusSeeOther)
			return
		}
		// URLs match - proceed to render
	} else {
		// User is NOT a participant
		if ballot.IsOpen {
			// Add participant and redirect
			newParticipantID, isNew, err := h.addParticipant(r.Context(), ballotID, userID)
			if err != nil {
				log.Printf("Error adding participant: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}
			// Broadcast to other participants that a new participant joined
			if isNew {
				h.broadcastParticipantsChanged(r.Context(), ballotID)
			}
			http.Redirect(w, r, "/ballot/"+ballotID+"/"+newParticipantID, http.StatusSeeOther)
			return
		} else {
			// Ballot is closed
			w.WriteHeader(http.StatusForbidden)
			h.templates.ExecuteTemplate(w, "closed.html", nil)
			return
		}
	}

	// 4. Render ballot
	// Get all items with display names
	items, err := h.getItems(r.Context(), ballotID)
	if err != nil {
		log.Printf("Error fetching items: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get current user's ranking
	rankings, err := h.getRankings(r.Context(), ballotID, userID)
	if err != nil {
		log.Printf("Error fetching rankings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Split items into ranked and unranked
	rankedItems := make([]model.Item, 0)
	unrankedItems := make([]model.Item, 0)

	rankMap := make(map[int64]int)
	for _, r := range rankings {
		rankMap[r.ItemID] = r.Position
	}

	for _, item := range items {
		if _, ranked := rankMap[item.ID]; ranked {
			rankedItems = append(rankedItems, item)
		} else {
			unrankedItems = append(unrankedItems, item)
		}
	}

	// Sort ranked items by position
	for i := 0; i < len(rankedItems); i++ {
		for j := i + 1; j < len(rankedItems); j++ {
			if rankMap[rankedItems[i].ID] > rankMap[rankedItems[j].ID] {
				rankedItems[i], rankedItems[j] = rankedItems[j], rankedItems[i]
			}
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

	// Get current user's display name for this ballot
	var displayName string
	h.db.QueryRowContext(r.Context(),
		"SELECT display_name FROM ballot_participants WHERE ballot_id = ? AND user_id = ?",
		ballotID, userID).Scan(&displayName)

	// Count anonymous participants
	anonymousCount := 0
	for _, p := range participants {
		if p.DisplayName == "" {
			anonymousCount++
		}
	}

	data := map[string]interface{}{
		"Ballot":           ballot,
		"RankedItems":      rankedItems,
		"UnrankedItems":    unrankedItems,
		"CurrentUser":      userID,
		"Results":          results,
		"IsOpen":           ballot.IsOpen,
		"ParticipantCount": len(participants),
		"ParticipantID":    currentParticipantID,
		"Participants":     participants,
		"DisplayName":      displayName,
		"AnonymousCount":   anonymousCount,
	}

	if err := h.templates.ExecuteTemplate(w, "ballot.html", data); err != nil {
		log.Printf("Error rendering ballot: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HandleAddItem adds a new item to the ballot.
func (h *Handler) HandleAddItem(w http.ResponseWriter, r *http.Request) {
	ballotID := r.PathValue("id")

	ballot := h.checkBallotAccess(w, r, ballotID)
	if ballot == nil {
		return
	}

	userID := UserID(r.Context())

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	itemName := r.FormValue("name")
	if itemName == "" {
		http.Error(w, "Item name cannot be empty", http.StatusBadRequest)
		return
	}

	_, err := h.db.ExecContext(r.Context(),
		"INSERT INTO items (ballot_id, name, added_by) VALUES (?, ?, ?)",
		ballotID, itemName, userID)
	if err != nil {
		log.Printf("Error adding item: %v", err)
		http.Error(w, "Could not add item (duplicate name?)", http.StatusBadRequest)
		return
	}

	// Broadcast that items changed
	h.broadcast(r.Context(), ballotID, "items_changed")

	w.WriteHeader(http.StatusNoContent)
}

// HandleDeleteItem deletes an item (only if the current user added it).
func (h *Handler) HandleDeleteItem(w http.ResponseWriter, r *http.Request) {
	ballotID := r.PathValue("id")

	ballot := h.checkBallotAccess(w, r, ballotID)
	if ballot == nil {
		return
	}

	itemID := r.PathValue("itemID")
	userID := UserID(r.Context())

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

	if addedBy != userID {
		http.Error(w, "You can only delete items you added", http.StatusForbidden)
		return
	}

	_, err = h.db.ExecContext(r.Context(), "DELETE FROM items WHERE id = ?", itemID)
	if err != nil {
		log.Printf("Error deleting item: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Broadcast that items changed
	h.broadcast(r.Context(), ballotID, "items_changed")

	w.WriteHeader(http.StatusNoContent)
}

// HandleSaveRankings saves the user's ranking (JSON API).
func (h *Handler) HandleSaveRankings(w http.ResponseWriter, r *http.Request) {
	ballotID := r.PathValue("id")

	ballot := h.checkBallotAccess(w, r, ballotID)
	if ballot == nil {
		return
	}

	userID := UserID(r.Context())

	// Parse JSON body
	var req struct {
		Order []int64 `json:"order"`
	}
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

	// Delete existing rankings for this user on this ballot
	_, err = tx.ExecContext(r.Context(),
		"DELETE FROM rankings WHERE ballot_id = ? AND user_id = ?",
		ballotID, userID)
	if err != nil {
		log.Printf("Error deleting old rankings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Insert new rankings
	stmt, err := tx.PrepareContext(r.Context(),
		"INSERT INTO rankings (ballot_id, user_id, item_id, position) VALUES (?, ?, ?, ?)")
	if err != nil {
		log.Printf("Error preparing statement: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	for i, itemID := range req.Order {
		_, err := stmt.ExecContext(r.Context(), ballotID, userID, itemID, i+1)
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
	h.broadcast(r.Context(), ballotID, "results")

	w.WriteHeader(http.StatusNoContent)
}

// HandleToggleOpen toggles the ballot's open/closed state.
func (h *Handler) HandleToggleOpen(w http.ResponseWriter, r *http.Request) {
	ballotID := r.PathValue("id")
	userID := UserID(r.Context())

	// Only authorized participants can toggle
	isParticipant, err := h.isParticipant(r.Context(), ballotID, userID)
	if err != nil {
		log.Printf("Error checking participant for toggle: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if !isParticipant {
		http.Error(w, "This ballot is currently closed.", http.StatusForbidden)
		return
	}

	// Toggle: flip is_open between 0 and 1
	var newIsOpenInt int
	err = h.db.QueryRowContext(r.Context(),
		"UPDATE ballots SET is_open = NOT is_open WHERE id = ? RETURNING is_open",
		ballotID).Scan(&newIsOpenInt)
	if err != nil {
		log.Printf("Error toggling ballot state: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	newIsOpen := newIsOpenInt == 1

	// Broadcast state change via WebSocket
	h.broadcastStateChange(ballotID, newIsOpen)

	// Return JSON with the new state
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"isOpen": newIsOpen,
	})
}

// HandleSetName sets the display name for the current user.
func (h *Handler) HandleSetName(w http.ResponseWriter, r *http.Request) {
	ballotID := r.PathValue("id")
	userID := UserID(r.Context())

	// Verify user is a participant
	isParticipant, err := h.isParticipant(r.Context(), ballotID, userID)
	if err != nil || !isParticipant {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	displayName := r.FormValue("name")
	// Trim and limit length
	if len(displayName) > 50 {
		displayName = displayName[:50]
	}

	_, err = h.db.ExecContext(r.Context(),
		"UPDATE ballot_participants SET display_name = ? WHERE ballot_id = ? AND user_id = ?",
		displayName, ballotID, userID)
	if err != nil {
		log.Printf("Error updating display name: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Broadcast participants_changed message
	h.broadcastParticipantsChanged(r.Context(), ballotID)

	w.WriteHeader(http.StatusNoContent)
}

// broadcastStateChange broadcasts a ballot open/closed state change to all WebSocket clients.
func (h *Handler) broadcastStateChange(ballotID string, isOpen bool) {
	msg := map[string]interface{}{
		"type":   "state_changed",
		"isOpen": isOpen,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Error marshaling state change for broadcast: %v", err)
		return
	}

	h.hub.Broadcast(ballotID, data)
}

// isParticipant checks if a user is an authorized participant of a ballot.
func (h *Handler) isParticipant(ctx context.Context, ballotID, userID string) (bool, error) {
	var exists bool
	err := h.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM ballot_participants WHERE ballot_id = ? AND user_id = ?)",
		ballotID, userID).Scan(&exists)
	return exists, err
}

// addParticipant records a user as an authorized participant and returns their participant ID.
func (h *Handler) addParticipant(ctx context.Context, ballotID, userID string) (string, bool, error) {
	participantID := generateParticipantID()
	result, err := h.db.ExecContext(ctx,
		"INSERT OR IGNORE INTO ballot_participants (ballot_id, user_id, participant_id) VALUES (?, ?, ?)",
		ballotID, userID, participantID)
	if err != nil {
		return "", false, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return "", false, err
	}

	isNew := rowsAffected > 0

	// If the insert was ignored (already exists), fetch the existing participant_id
	if !isNew {
		err = h.db.QueryRowContext(ctx,
			"SELECT participant_id FROM ballot_participants WHERE ballot_id = ? AND user_id = ?",
			ballotID, userID).Scan(&participantID)
		if err != nil {
			return "", false, err
		}
	}

	return participantID, isNew, nil
}

// checkBallotAccess verifies the user can access the ballot.
// Returns the ballot if access is granted, or writes an HTTP error and returns nil.
// If the ballot is open and the user is new, they are automatically added as a participant.
func (h *Handler) checkBallotAccess(w http.ResponseWriter, r *http.Request, ballotID string) *model.Ballot {
	userID := UserID(r.Context())

	var ballot model.Ballot
	var isOpenInt int
	err := h.db.QueryRowContext(r.Context(),
		"SELECT id, title, is_open, created_by, created_at FROM ballots WHERE id = ?",
		ballotID).Scan(&ballot.ID, &ballot.Title, &isOpenInt, &ballot.CreatedBy, &ballot.CreatedAt)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return nil
	} else if err != nil {
		log.Printf("Error fetching ballot: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return nil
	}
	ballot.IsOpen = isOpenInt == 1

	isParticipant, err := h.isParticipant(r.Context(), ballotID, userID)
	if err != nil {
		log.Printf("Error checking participant: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return nil
	}

	if ballot.IsOpen {
		// Open ballot: auto-add new visitors as participants
		if !isParticipant {
			if _, isNew, err := h.addParticipant(r.Context(), ballotID, userID); err != nil {
				log.Printf("Error adding participant: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return nil
			} else if isNew {
				// Broadcast to other participants that a new participant joined
				h.broadcastParticipantsChanged(r.Context(), ballotID)
			}
		}
		return &ballot
	}

	// Closed ballot: only existing participants allowed
	if !isParticipant {
		http.Error(w, "This ballot is currently closed.", http.StatusForbidden)
		return nil
	}
	return &ballot
}

// getItems retrieves all items for a ballot with display names.
func (h *Handler) getItems(ctx context.Context, ballotID string) ([]model.Item, error) {
	rows, err := h.db.QueryContext(ctx,
		`SELECT i.id, i.ballot_id, i.name, i.added_by, i.created_at, COALESCE(bp.display_name, '')
		 FROM items i
		 LEFT JOIN ballot_participants bp ON i.ballot_id = bp.ballot_id AND i.added_by = bp.user_id
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
func (h *Handler) getParticipants(ctx context.Context, ballotID string) ([]model.Participant, error) {
	rows, err := h.db.QueryContext(ctx,
		`SELECT participant_id, display_name
		 FROM ballot_participants
		 WHERE ballot_id = ?
		 ORDER BY created_at`,
		ballotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var participants []model.Participant
	for rows.Next() {
		var p model.Participant
		if err := rows.Scan(&p.ParticipantID, &p.DisplayName); err != nil {
			return nil, err
		}
		participants = append(participants, p)
	}
	return participants, rows.Err()
}

// getRankings retrieves all rankings for a user on a ballot.
func (h *Handler) getRankings(ctx context.Context, ballotID, userID string) ([]model.Ranking, error) {
	rows, err := h.db.QueryContext(ctx,
		"SELECT ballot_id, user_id, item_id, position FROM rankings WHERE ballot_id = ? AND user_id = ?",
		ballotID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rankings []model.Ranking
	for rows.Next() {
		var r model.Ranking
		if err := rows.Scan(&r.BallotID, &r.UserID, &r.ItemID, &r.Position); err != nil {
			return nil, err
		}
		rankings = append(rankings, r)
	}
	return rankings, rows.Err()
}

// ResultEntry represents one item in the final ranking.
type ResultEntry struct {
	Rank int    `json:"rank"`
	Name string `json:"name"`
}

// ItemEntry represents an item for client-side rendering.
type ItemEntry struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	AddedBy     string `json:"addedBy"`
	AddedByName string `json:"addedByName"`
}

// ParticipantEntry represents a participant for client-side rendering.
type ParticipantEntry struct {
	DisplayName   string `json:"displayName"`
	ParticipantID string `json:"participantId"`
}

// BroadcastMessage wraps the WebSocket payload with a message type.
type BroadcastMessage struct {
	Type             string             `json:"type"`
	Results          []ResultEntry      `json:"results"`
	Items            []ItemEntry        `json:"items,omitempty"`
	Participants     []ParticipantEntry `json:"participants,omitempty"`
	ParticipantCount int                `json:"participantCount,omitempty"`
}

// computeResults computes the Schulze ranking for a ballot.
func (h *Handler) computeResults(ctx context.Context, ballotID string, items []model.Item) ([]ResultEntry, error) {
	if len(items) == 0 {
		return []ResultEntry{}, nil
	}

	// Build item index mapping
	idToIndex := make(map[int64]int)
	indexToItem := make(map[int]model.Item)
	for i, item := range items {
		idToIndex[item.ID] = i
		indexToItem[i] = item
	}

	// Get all rankings for this ballot (all users)
	rows, err := h.db.QueryContext(ctx,
		"SELECT user_id, item_id, position FROM rankings WHERE ballot_id = ?",
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

	// Convert to ResultEntry
	entries := make([]ResultEntry, len(results))
	for i, r := range results {
		entries[i] = ResultEntry{
			Rank: r.Rank,
			Name: indexToItem[r.CandidateIndex].Name,
		}
	}

	return entries, nil
}

// broadcast recomputes and broadcasts a typed message to all WebSocket clients.
// messageType should be "results" (ranking changed) or "items_changed" (items added/deleted).
func (h *Handler) broadcast(ctx context.Context, ballotID, messageType string) {
	items, err := h.getItems(ctx, ballotID)
	if err != nil {
		log.Printf("Error getting items for broadcast: %v", err)
		return
	}

	results, err := h.computeResults(ctx, ballotID, items)
	if err != nil {
		log.Printf("Error computing results for broadcast: %v", err)
		return
	}

	participants, err := h.getParticipants(ctx, ballotID)
	if err != nil {
		log.Printf("Error getting participants for broadcast: %v", err)
		return
	}

	msg := BroadcastMessage{
		Type:             messageType,
		Results:          results,
		ParticipantCount: len(participants),
	}

	// Include item list for items_changed messages
	if messageType == "items_changed" {
		itemEntries := make([]ItemEntry, len(items))
		for i, item := range items {
			itemEntries[i] = ItemEntry{
				ID:          item.ID,
				Name:        item.Name,
				AddedBy:     item.AddedBy,
				AddedByName: item.AddedByName,
			}
		}
		msg.Items = itemEntries
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Error marshaling message for broadcast: %v", err)
		return
	}

	h.hub.Broadcast(ballotID, data)
}

// broadcastParticipantsChanged broadcasts a participants_changed message with updated participant list and items.
func (h *Handler) broadcastParticipantsChanged(ctx context.Context, ballotID string) {
	items, err := h.getItems(ctx, ballotID)
	if err != nil {
		log.Printf("Error getting items for broadcast: %v", err)
		return
	}

	results, err := h.computeResults(ctx, ballotID, items)
	if err != nil {
		log.Printf("Error computing results for broadcast: %v", err)
		return
	}

	participants, err := h.getParticipants(ctx, ballotID)
	if err != nil {
		log.Printf("Error getting participants for broadcast: %v", err)
		return
	}

	itemEntries := make([]ItemEntry, len(items))
	for i, item := range items {
		itemEntries[i] = ItemEntry{
			ID:          item.ID,
			Name:        item.Name,
			AddedBy:     item.AddedBy,
			AddedByName: item.AddedByName,
		}
	}

	participantEntries := make([]ParticipantEntry, len(participants))
	for i, p := range participants {
		participantEntries[i] = ParticipantEntry{
			DisplayName:   p.DisplayName,
			ParticipantID: p.ParticipantID,
		}
	}

	msg := BroadcastMessage{
		Type:             "participants_changed",
		Results:          results,
		Items:            itemEntries,
		Participants:     participantEntries,
		ParticipantCount: len(participants),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Error marshaling participants_changed for broadcast: %v", err)
		return
	}

	h.hub.Broadcast(ballotID, data)
}
