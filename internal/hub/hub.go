package hub

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// sendBuffer is how many messages a client may fall behind before it is
// disconnected as a slow consumer.
const sendBuffer = 16

// Manager tracks WebSocket clients per ballot and broadcasts to them.
//
// All client-set changes happen under one mutex, and a client's send channel
// is closed only by whoever removes it from the set, so each channel is closed
// exactly once and never sent to after closing.
type Manager struct {
	mu      sync.Mutex
	ballots map[string]map[*Client]struct{}
	closed  bool
}

// NewManager creates a new Manager.
func NewManager() *Manager {
	return &Manager{
		ballots: make(map[string]map[*Client]struct{}),
	}
}

// Register adds a client to its ballot. After CloseAll, the client is closed
// immediately instead.
func (m *Manager) Register(c *Client) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		close(c.send)
		return
	}

	clients, ok := m.ballots[c.ballotID]
	if !ok {
		clients = make(map[*Client]struct{})
		m.ballots[c.ballotID] = clients
	}
	clients[c] = struct{}{}
	log.Printf("Client registered to ballot %s (%d total)", c.ballotID, len(clients))
}

// Unregister removes a client from its ballot. It is safe to call more than once.
func (m *Manager) Unregister(c *Client) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.removeLocked(c) {
		log.Printf("Client unregistered from ballot %s (%d remaining)", c.ballotID, len(m.ballots[c.ballotID]))
	}
}

// Broadcast sends a message to all clients on a ballot, disconnecting any
// client whose send buffer is full.
func (m *Manager) Broadcast(ballotID string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for c := range m.ballots[ballotID] {
		m.deliverLocked(c, data)
	}
}

// Disconnect closes every socket a participant has open on a ballot with the
// given close code.
func (m *Manager) Disconnect(ballotID, participantID string, code websocket.StatusCode) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for c := range m.ballots[ballotID] {
		if c.participantID != "" && c.participantID == participantID {
			c.closeCode = code
			m.removeLocked(c)
		}
	}
}

// CloseBallot closes every socket on a ballot with the given close code.
func (m *Manager) CloseBallot(ballotID string, code websocket.StatusCode) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for c := range m.ballots[ballotID] {
		c.closeCode = code
		m.removeLocked(c)
	}
}

// CloseAll disconnects every client and rejects future registrations.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true
	for _, clients := range m.ballots {
		for c := range clients {
			close(c.send)
		}
	}
	clear(m.ballots)
}

// deliverLocked queues data for c if c is still registered. m.mu must be held.
func (m *Manager) deliverLocked(c *Client, data []byte) {
	if _, ok := m.ballots[c.ballotID][c]; !ok {
		return
	}
	select {
	case c.send <- data:
	default:
		m.removeLocked(c)
		log.Printf("Client disconnected from ballot %s (slow consumer)", c.ballotID)
	}
}

// removeLocked removes c and closes its send channel, reporting whether c was
// registered. m.mu must be held.
func (m *Manager) removeLocked(c *Client) bool {
	clients := m.ballots[c.ballotID]
	if _, ok := clients[c]; !ok {
		return false
	}
	delete(clients, c)
	close(c.send)
	if len(clients) == 0 {
		delete(m.ballots, c.ballotID)
	}
	return true
}

// Client wraps a single WebSocket connection.
type Client struct {
	conn          *websocket.Conn
	ballotID      string
	participantID string // empty for viewers
	send          chan []byte
	manager       *Manager

	// closeCode is the status WritePump closes the connection with. It is
	// set under the manager's lock before send is closed, so WritePump
	// reads it after observing the closed channel.
	closeCode websocket.StatusCode
}

// NewClient creates a Client for a ballot; participantID is empty for a
// viewer. Call Manager.Register to start receiving broadcasts.
func NewClient(conn *websocket.Conn, ballotID, participantID string, manager *Manager) *Client {
	return &Client{
		conn:          conn,
		ballotID:      ballotID,
		participantID: participantID,
		send:          make(chan []byte, sendBuffer),
		manager:       manager,
		closeCode:     websocket.StatusNormalClosure,
	}
}

// Send queues a message for this client only.
func (c *Client) Send(data []byte) {
	c.manager.mu.Lock()
	defer c.manager.mu.Unlock()
	c.manager.deliverLocked(c, data)
}

// WritePump writes queued messages to the WebSocket until the send channel is
// closed or ctx is done.
func (c *Client) WritePump(ctx context.Context) {
	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				c.conn.Close(c.closeCode, "")
				return
			}

			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := c.conn.Write(writeCtx, websocket.MessageText, message)
			cancel()

			if err != nil {
				log.Printf("Write error: %v", err)
				c.conn.Close(websocket.StatusInternalError, "")
				return
			}

		case <-ctx.Done():
			c.conn.Close(websocket.StatusNormalClosure, "")
			return
		}
	}
}

// ReadPump reads from the WebSocket to detect disconnects, then unregisters
// the client. Clients aren't expected to send anything.
func (c *Client) ReadPump(ctx context.Context) {
	defer func() {
		c.manager.Unregister(c)
		c.conn.Close(websocket.StatusNormalClosure, "")
	}()

	for {
		if _, _, err := c.conn.Read(ctx); err != nil {
			return
		}
	}
}
