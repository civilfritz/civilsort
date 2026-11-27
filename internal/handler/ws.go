package handler

import (
	"encoding/json"
	"log"
	"net/http"

	hubpkg "github.com/civilfritz/civilsort/internal/hub"
	"github.com/civilfritz/civilsort/internal/protocol"
	"github.com/coder/websocket"
)

// HandleWebSocket upgrades to WebSocket for live results.
func (h *Handler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	ballotID := r.PathValue("id")

	// Viewers and participants alike get live updates. A participant's
	// socket is tagged so it can be closed if they're removed.
	if h.loadBallot(w, r, ballotID) == nil {
		return
	}
	current, _, err := h.currentParticipant(w, r, ballotID)
	if err != nil {
		log.Printf("Error checking participant: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Upgrade to WebSocket
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	client := hubpkg.NewClient(conn, ballotID, current.ID, h.hub)
	h.hub.Register(client)

	// Send current results immediately
	if snap, err := h.snapshot(r.Context(), ballotID); err != nil {
		log.Printf("Error building initial results: %v", err)
	} else if data, err := json.Marshal(protocol.NewResultsMessage(snap.results, snap.items, len(snap.participants))); err == nil {
		client.Send(data)
	}

	// Start write pump in a goroutine
	go client.WritePump(r.Context())

	// Start read pump (blocks until disconnect)
	client.ReadPump(r.Context())
}
