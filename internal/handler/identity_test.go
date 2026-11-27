package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/civilfritz/civilsort/internal/protocol"
)

func TestBroadcastsHideSecrets(t *testing.T) {
	h := testHandler(t)
	ballotID, creator := newBallot(t, h)
	if _, err := h.db.Exec("UPDATE participants SET display_name = 'A' WHERE id = ?", creator.ID); err != nil {
		t.Fatal(err)
	}
	insertItem(t, h.db, ballotID, "Item", creator.ID)

	s, err := h.snapshot(context.Background(), ballotID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(protocol.NewParticipantsChangedMessage(s.results, s.items, s.participants))
	if err != nil {
		t.Fatal(err)
	}
	payload := string(data)

	for name, secret := range map[string]string{"secret": creator.Secret, "voter_id": creator.userID} {
		if strings.Contains(payload, secret) {
			t.Errorf("broadcast contains %s %q: %s", name, secret, payload)
		}
	}
	if !strings.Contains(payload, creator.ID) {
		t.Errorf("broadcast should identify participant by ID %q: %s", creator.ID, payload)
	}
}

func TestHandleGetRankings_Access(t *testing.T) {
	h := testHandler(t)
	ballotID, viewer := newBallot(t, h)
	target := join(t, h, ballotID)

	get := func(as *member, id string) int {
		return do(t, h.HandleGetRankings, "GET", req{
			as:   as,
			path: map[string]string{"id": ballotID, "participantID": id},
		}).Code
	}

	if code := get(&viewer, target.ID); code != http.StatusOK {
		t.Errorf("participant viewing by ID: got %d, want 200", code)
	}
	if code := get(&viewer, target.Secret); code != http.StatusNotFound {
		t.Errorf("lookup by secret: got %d, want 404", code)
	}
	if code := get(nil, target.ID); code != http.StatusForbidden {
		t.Errorf("non-participant: got %d, want 403", code)
	}
}
