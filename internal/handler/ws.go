package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/coder/websocket"
	hubpkg "github.com/civilfritz/civilsort/internal/hub"
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

	// Get or create ballot hub
	ballotHub := h.hub.GetOrCreate(ballotID)

	// Create client
	client := hubpkg.NewClient(conn, ballotHub)
	ballotHub.Register(client)

	// Send current results immediately
	items, err := h.getItems(r.Context(), ballotID)
	if err == nil {
		results, err := h.computeResults(r.Context(), ballotID, items)
		if err == nil {
			msg := BroadcastMessage{
				Type:    "results",
				Results: results,
			}
			data, err := json.Marshal(msg)
			if err == nil {
				client.Send(data)
			}
		}
	}

	// Start write pump in a goroutine
	go client.WritePump(r.Context())

	// Start read pump (blocks until disconnect)
	client.ReadPump(r.Context())
}
