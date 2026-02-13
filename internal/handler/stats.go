package handler

import (
	"database/sql"
	"log"
	"net/http"
)

// Stats holds aggregate statistics for the /stats page.
type Stats struct {
	TotalBallots      int
	NewestBallot      string
	MaxEntries        int
	MaxParticipants   int
	AvgEntries        float64
	AvgParticipants   float64
}

// HandleStats shows de-identified aggregate statistics.
func (h *Handler) HandleStats(w http.ResponseWriter, r *http.Request) {
	var stats Stats
	var newestBallotStr sql.NullString

	// Single query with all aggregate stats
	query := `
	SELECT
		COUNT(*) AS total_ballots,
		MAX(DATE(created_at)) AS newest_ballot,
		COALESCE((SELECT MAX(cnt) FROM (SELECT COUNT(*) AS cnt FROM items GROUP BY ballot_id)), 0) AS max_entries,
		COALESCE((SELECT MAX(cnt) FROM (SELECT COUNT(DISTINCT user_id) AS cnt FROM rankings GROUP BY ballot_id)), 0) AS max_participants,
		COALESCE((SELECT AVG(cnt) FROM (SELECT COUNT(*) AS cnt FROM items GROUP BY ballot_id)), 0) AS avg_entries,
		COALESCE((SELECT AVG(cnt) FROM (SELECT COUNT(DISTINCT user_id) AS cnt FROM rankings GROUP BY ballot_id)), 0) AS avg_participants
	FROM ballots
	`

	err := h.db.QueryRowContext(r.Context(), query).Scan(
		&stats.TotalBallots,
		&newestBallotStr,
		&stats.MaxEntries,
		&stats.MaxParticipants,
		&stats.AvgEntries,
		&stats.AvgParticipants,
	)

	if err != nil {
		log.Printf("Error fetching stats: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Set newest ballot date
	if newestBallotStr.Valid {
		stats.NewestBallot = newestBallotStr.String
	} else {
		stats.NewestBallot = "N/A"
	}

	if err := h.templates.ExecuteTemplate(w, "stats.html", stats); err != nil {
		log.Printf("Error rendering stats: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
