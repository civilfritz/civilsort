package hub

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func newTestClient(m *Manager, ballotID string) *Client {
	return NewClient(nil, ballotID, "", m)
}

// drained reports whether c's send channel is closed, after reading any
// queued messages.
func drained(c *Client) bool {
	for {
		select {
		case _, ok := <-c.send:
			if !ok {
				return true
			}
		default:
			return false
		}
	}
}

func TestBroadcast_OnlyReachesBallot(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()

	a := newTestClient(m, "a")
	b := newTestClient(m, "b")
	m.Register(a)
	m.Register(b)

	m.Broadcast("a", []byte("hello"))

	if got := len(a.send); got != 1 {
		t.Errorf("ballot a client: %d messages, want 1", got)
	}
	if got := len(b.send); got != 0 {
		t.Errorf("ballot b client: %d messages, want 0", got)
	}
}

func TestBroadcast_NoClients(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	m.Broadcast("nonexistent", []byte("test"))
}

func TestUnregister_RemovesEmptyBallot(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()

	c := newTestClient(m, "a")
	m.Register(c)
	m.Unregister(c)
	m.Unregister(c) // idempotent

	if _, ok := m.ballots["a"]; ok {
		t.Error("ballot entry should be removed when its last client leaves")
	}
	if !drained(c) {
		t.Error("send channel should be closed after Unregister")
	}
	c.Send([]byte("late")) // must not panic on the closed channel
}

func TestBroadcast_DropsSlowConsumer(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()

	slow := newTestClient(m, "a")
	m.Register(slow)
	for range sendBuffer + 1 {
		m.Broadcast("a", []byte("x"))
	}

	if _, ok := m.ballots["a"][slow]; ok {
		t.Error("slow consumer should be removed")
	}
	if !drained(slow) {
		t.Error("slow consumer's send channel should be closed")
	}
	m.Unregister(slow) // ReadPump still calls this; must not double-close
}

func TestCloseAll(t *testing.T) {
	m := NewManager()
	c := newTestClient(m, "a")
	m.Register(c)

	m.CloseAll()
	if !drained(c) {
		t.Error("CloseAll should close registered clients")
	}

	late := newTestClient(m, "a")
	m.Register(late)
	if !drained(late) {
		t.Error("Register after CloseAll should close the client")
	}
	m.Unregister(late)
}

// Clients joining and leaving a ballot while it is broadcast to must never
// block or panic. Run with -race.
func TestConcurrentRegisterUnregisterBroadcast(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()

	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for i := range 50 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ballotID := fmt.Sprintf("b%d", i%3)
				for range 100 {
					c := newTestClient(m, ballotID)
					m.Register(c)
					m.Broadcast(ballotID, []byte("x"))
					c.Send([]byte("y"))
					m.Unregister(c)
				}
			}()
		}
		wg.Wait()
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("deadlock: concurrent register/unregister did not finish")
	}

	if len(m.ballots) != 0 {
		t.Errorf("%d ballots left registered, want 0", len(m.ballots))
	}
}

func TestDisconnect_OnlyThatParticipant(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()

	alice1 := NewClient(nil, "a", "alice", m)
	alice2 := NewClient(nil, "a", "alice", m)
	bob := NewClient(nil, "a", "bob", m)
	viewer := newTestClient(m, "a")
	elsewhere := NewClient(nil, "b", "alice", m)
	for _, c := range []*Client{alice1, alice2, bob, viewer, elsewhere} {
		m.Register(c)
	}

	m.Disconnect("a", "alice", 4001)

	for name, c := range map[string]*Client{"alice1": alice1, "alice2": alice2} {
		if !drained(c) || c.closeCode != 4001 {
			t.Errorf("%s: closed=%v code=%d, want closed with 4001", name, drained(c), c.closeCode)
		}
	}
	for name, c := range map[string]*Client{"bob": bob, "viewer": viewer, "same ID on another ballot": elsewhere} {
		if drained(c) {
			t.Errorf("%s should stay connected", name)
		}
	}
}

func TestCloseBallot(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()

	a := NewClient(nil, "a", "alice", m)
	viewer := newTestClient(m, "a")
	other := newTestClient(m, "b")
	for _, c := range []*Client{a, viewer, other} {
		m.Register(c)
	}

	m.CloseBallot("a", 4002)

	if !drained(a) || !drained(viewer) || a.closeCode != 4002 || viewer.closeCode != 4002 {
		t.Error("every socket on the ballot should close with 4002")
	}
	if drained(other) {
		t.Error("other ballots should be unaffected")
	}
	if _, ok := m.ballots["a"]; ok {
		t.Error("closed ballot should be removed")
	}
}
