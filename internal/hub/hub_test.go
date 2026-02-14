package hub

import (
	"testing"
)

func TestManager_GetOrCreate(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()

	ballotID := "test123"

	hub1 := m.GetOrCreate(ballotID)
	if hub1 == nil {
		t.Fatal("GetOrCreate returned nil")
	}

	hub2 := m.GetOrCreate(ballotID)
	if hub2 != hub1 {
		t.Error("GetOrCreate should return same hub for same ballot ID")
	}

	// Different ballot should get different hub
	hub3 := m.GetOrCreate("different")
	if hub3 == hub1 {
		t.Error("Different ballot IDs should get different hubs")
	}
}

func TestManager_Broadcast_NoHub(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()

	// Broadcasting to non-existent ballot shouldn't panic
	m.Broadcast("nonexistent", []byte("test"))
}

func TestManager_CloseAll(t *testing.T) {
	m := NewManager()

	// Create some hubs
	m.GetOrCreate("ballot1")
	m.GetOrCreate("ballot2")

	// CloseAll should not panic
	m.CloseAll()

	// CloseAll on empty manager shouldn't panic either
	m2 := NewManager()
	m2.CloseAll()
}
