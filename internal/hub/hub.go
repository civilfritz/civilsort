package hub

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Manager tracks all active ballot hubs.
type Manager struct {
	mu      sync.Mutex
	ballots map[string]*BallotHub
}

// NewManager creates a new Manager.
func NewManager() *Manager {
	return &Manager{
		ballots: make(map[string]*BallotHub),
	}
}

// GetOrCreate returns the existing hub for a ballot, or creates a new one.
func (m *Manager) GetOrCreate(ballotID string) *BallotHub {
	m.mu.Lock()
	defer m.mu.Unlock()

	if hub, ok := m.ballots[ballotID]; ok {
		return hub
	}

	hub := &BallotHub{
		ballotID:   ballotID,
		manager:    m,
		clients:    make(map[*Client]struct{}),
		broadcast:  make(chan []byte, 10),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		done:       make(chan struct{}),
	}

	go hub.run()
	m.ballots[ballotID] = hub
	return hub
}

// Broadcast sends a message to all clients on a ballot.
func (m *Manager) Broadcast(ballotID string, data []byte) {
	m.mu.Lock()
	hub, ok := m.ballots[ballotID]
	m.mu.Unlock()

	if ok {
		select {
		case hub.broadcast <- data:
		case <-time.After(100 * time.Millisecond):
			log.Printf("Broadcast to ballot %s timed out", ballotID)
		}
	}
}

// remove removes a ballot hub from the manager.
func (m *Manager) remove(ballotID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.ballots, ballotID)
}

// CloseAll closes all ballot hubs.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, hub := range m.ballots {
		close(hub.done)
	}
}

// BallotHub manages WebSocket clients for a single ballot.
type BallotHub struct {
	ballotID   string
	manager    *Manager
	clients    map[*Client]struct{}
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	done       chan struct{}
}

// run is the main loop for the ballot hub.
func (hub *BallotHub) run() {
	for {
		select {
		case client := <-hub.register:
			hub.clients[client] = struct{}{}
			log.Printf("Client registered to ballot %s (%d total)", hub.ballotID, len(hub.clients))

		case client := <-hub.unregister:
			if _, ok := hub.clients[client]; ok {
				delete(hub.clients, client)
				close(client.send)
				log.Printf("Client unregistered from ballot %s (%d remaining)", hub.ballotID, len(hub.clients))
			}

			// If no clients left, shut down the hub
			if len(hub.clients) == 0 {
				hub.manager.remove(hub.ballotID)
				log.Printf("Hub for ballot %s shut down (no clients)", hub.ballotID)
				return
			}

		case message := <-hub.broadcast:
			for client := range hub.clients {
				select {
				case client.send <- message:
				default:
					// Client's send buffer is full, disconnect it
					delete(hub.clients, client)
					close(client.send)
					log.Printf("Client disconnected from ballot %s (slow consumer)", hub.ballotID)
				}
			}

		case <-hub.done:
			// Close all clients
			for client := range hub.clients {
				close(client.send)
			}
			log.Printf("Hub for ballot %s shut down (manager closed)", hub.ballotID)
			return
		}
	}
}

// Register adds a client to the hub.
func (hub *BallotHub) Register(client *Client) {
	hub.register <- client
}

// Unregister removes a client from the hub.
func (hub *BallotHub) Unregister(client *Client) {
	hub.unregister <- client
}

// Client wraps a single WebSocket connection.
type Client struct {
	conn *websocket.Conn
	send chan []byte
	hub  *BallotHub
}

// NewClient creates a new Client.
func NewClient(conn *websocket.Conn, hub *BallotHub) *Client {
	return &Client{
		conn: conn,
		send: make(chan []byte, 16),
		hub:  hub,
	}
}

// WritePump writes messages from the send channel to the WebSocket.
func (c *Client) WritePump(ctx context.Context) {
	defer func() {
		c.conn.Close(websocket.StatusNormalClosure, "")
	}()

	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				// Channel closed
				return
			}

			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := c.conn.Write(writeCtx, websocket.MessageText, message)
			cancel()

			if err != nil {
				log.Printf("Write error: %v", err)
				return
			}

		case <-ctx.Done():
			return
		}
	}
}

// ReadPump reads messages from the WebSocket (primarily to detect disconnects).
func (c *Client) ReadPump(ctx context.Context) {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close(websocket.StatusNormalClosure, "")
	}()

	for {
		_, _, err := c.conn.Read(ctx)
		if err != nil {
			// Connection closed or error
			return
		}
		// We don't expect messages from clients, just read to detect disconnect
	}
}

// Send sends a message to the client.
func (c *Client) Send(data []byte) {
	select {
	case c.send <- data:
	default:
		// Buffer full, will be disconnected by hub
	}
}
