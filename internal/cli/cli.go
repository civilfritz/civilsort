package cli

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/civilfritz/civilsort/internal/model"
	"github.com/civilfritz/civilsort/internal/schulze"
)

// ballotRow represents a ballot for the list command.
type ballotRow struct {
	ID           string
	CreatedAt    time.Time
	Title        string
	Participants int
	Items        int
}

// resultEntry represents one item in the final ranking with its creator.
type resultEntry struct {
	Rank        int
	Name        string
	AddedByName string
	AddedBy     string // participant ID
}

// participantRow represents a participant with all identifiers.
type participantRow struct {
	ID          string
	Secret      string // empty once removed
	DisplayName string
	IsCreator   bool
	Removed     bool
}

// ListBallots prints all ballots with participant and item counts.
func ListBallots(db *sql.DB) error {
	ctx := context.Background()
	rows, err := db.QueryContext(ctx, `
		SELECT b.id, b.created_at, b.title,
			(SELECT COUNT(*) FROM participants p WHERE p.ballot_id = b.id AND p.removed_at IS NULL),
			(SELECT COUNT(*) FROM items i WHERE i.ballot_id = b.id)
		FROM ballots b
		ORDER BY b.created_at DESC
	`)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var ballots []ballotRow
	for rows.Next() {
		var b ballotRow
		if err := rows.Scan(&b.ID, &b.CreatedAt, &b.Title, &b.Participants, &b.Items); err != nil {
			return fmt.Errorf("scan failed: %w", err)
		}
		ballots = append(ballots, b)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows error: %w", err)
	}

	return printBallotsTable(ballots)
}

func printBallotsTable(ballots []ballotRow) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tCREATED\tTITLE\tPARTICIPANTS\tITEMS")

	for _, b := range ballots {
		title := b.Title
		if title == "" {
			title = "(untitled)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\n",
			b.ID,
			b.CreatedAt.Format("2006-01-02 15:04"),
			title,
			b.Participants,
			b.Items,
		)
	}

	return w.Flush()
}

// ListItems prints items for a ballot in Schulze result order.
func ListItems(db *sql.DB, ballotID string) error {
	ctx := context.Background()

	// Verify ballot exists
	if err := ballotExists(ctx, db, ballotID); err != nil {
		return err
	}

	// Fetch items
	items, err := getItems(ctx, db, ballotID)
	if err != nil {
		return fmt.Errorf("failed to get items: %w", err)
	}

	// Compute Schulze results
	results, err := computeResults(ctx, db, ballotID, items)
	if err != nil {
		return fmt.Errorf("failed to compute results: %w", err)
	}

	return printItemsTable(results)
}

func printItemsTable(results []resultEntry) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "RANK\tTITLE\tADDED BY")

	for _, r := range results {
		addedBy := r.AddedByName
		if addedBy == "" {
			// Show participant ID for anonymous users
			addedBy = fmt.Sprintf("(anonymous: %s)", r.AddedBy)
		}
		fmt.Fprintf(w, "%d\t%s\t%s\n", r.Rank, r.Name, addedBy)
	}

	return w.Flush()
}

// ListParticipants prints participants for a ballot.
func ListParticipants(db *sql.DB, ballotID string) error {
	ctx := context.Background()

	// Verify ballot exists
	if err := ballotExists(ctx, db, ballotID); err != nil {
		return err
	}

	// Fetch participants
	participants, err := getParticipants(ctx, db, ballotID)
	if err != nil {
		return fmt.Errorf("failed to get participants: %w", err)
	}

	return printParticipantsTable(participants)
}

func printParticipantsTable(participants []participantRow) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PARTICIPANT ID\tSECRET\tDISPLAY NAME\tROLE")

	for _, p := range participants {
		displayName := p.DisplayName
		if displayName == "" {
			displayName = "(anonymous)"
		}
		role := "participant"
		switch {
		case p.Removed:
			role = "removed"
		case p.IsCreator:
			role = "creator"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.ID, p.Secret, displayName, role)
	}

	return w.Flush()
}

// ballotExists checks if a ballot exists, returns error if not.
func ballotExists(ctx context.Context, db *sql.DB, ballotID string) error {
	var exists bool
	err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM ballots WHERE id = ?)", ballotID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}
	if !exists {
		return fmt.Errorf("ballot not found: %s", ballotID)
	}
	return nil
}

// getItems retrieves all items for a ballot with display names.
func getItems(ctx context.Context, db *sql.DB, ballotID string) ([]model.Item, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT i.id, i.ballot_id, i.name, i.added_by, i.created_at, COALESCE(p.display_name, '')
		FROM items i
		LEFT JOIN participants p ON p.ballot_id = i.ballot_id AND p.id = i.added_by
		WHERE i.ballot_id = ?
		ORDER BY i.id
	`, ballotID)
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

// getParticipants retrieves all participants for a ballot, including removed ones.
func getParticipants(ctx context.Context, db *sql.DB, ballotID string) ([]participantRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, COALESCE(secret, ''), display_name, is_creator, removed_at IS NOT NULL
		FROM participants
		WHERE ballot_id = ?
		ORDER BY created_at, rowid
	`, ballotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var participants []participantRow
	for rows.Next() {
		var p participantRow
		if err := rows.Scan(&p.ID, &p.Secret, &p.DisplayName, &p.IsCreator, &p.Removed); err != nil {
			return nil, err
		}
		participants = append(participants, p)
	}
	return participants, rows.Err()
}

// computeResults computes the Schulze ranking for a ballot.
func computeResults(ctx context.Context, db *sql.DB, ballotID string, items []model.Item) ([]resultEntry, error) {
	if len(items) == 0 {
		return []resultEntry{}, nil
	}

	// Build item index mapping
	idToIndex := make(map[int64]int)
	indexToItem := make(map[int]model.Item)
	for i, item := range items {
		idToIndex[item.ID] = i
		indexToItem[i] = item
	}

	// Get all rankings for this ballot
	rows, err := db.QueryContext(ctx,
		"SELECT participant_id, item_id, position FROM rankings WHERE ballot_id = ?",
		ballotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Build preferences by participant
	prefsByParticipant := make(map[string]schulze.Preference)
	for rows.Next() {
		var participantID string
		var itemID int64
		var position int
		if err := rows.Scan(&participantID, &itemID, &position); err != nil {
			return nil, err
		}

		if _, ok := prefsByParticipant[participantID]; !ok {
			prefsByParticipant[participantID] = make(schulze.Preference)
		}
		if idx, ok := idToIndex[itemID]; ok {
			prefsByParticipant[participantID][idx] = position
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Convert to slice of preferences
	prefs := make([]schulze.Preference, 0, len(prefsByParticipant))
	for _, p := range prefsByParticipant {
		prefs = append(prefs, p)
	}

	// Compute Schulze results
	results := schulze.Compute(len(items), prefs)

	// Convert to resultEntry
	entries := make([]resultEntry, len(results))
	for i, r := range results {
		item := indexToItem[r.CandidateIndex]
		entries[i] = resultEntry{
			Rank:        r.Rank,
			Name:        item.Name,
			AddedByName: item.AddedByName,
			AddedBy:     item.AddedBy,
		}
	}

	return entries, nil
}
