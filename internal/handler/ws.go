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

	// Check ballot access
	ballot := h.checkBallotAccess(w, r, ballotID)
	if ballot == nil {
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

	client := hubpkg.NewClient(conn, ballotID, h.hub)
	h.hub.Register(client)

	// Send current results immediately
	if snap, err := h.snapshot(r.Context(), ballotID); err != nil {
		log.Printf("Error building initial results: %v", err)
	} else if data, err := json.Marshal(protocol.NewResultsMessage(snap.results, len(snap.participants))); err == nil {
		client.Send(data)
	}

	// Start write pump in a goroutine
	go client.WritePump(r.Context())

	// Start read pump (blocks until disconnect)
	client.ReadPump(r.Context())
}
