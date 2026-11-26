package protocol

import (
	"encoding/json"
	"testing"
)

func TestMessagesEncode(t *testing.T) {
	tests := []struct {
		name string
		msg  any
		want string
	}{
		{
			name: "results",
			msg:  NewResultsMessage(nil, 0),
			want: `{"type":"results","results":[],"participantCount":0}`,
		},
		{
			name: "items_changed",
			msg:  NewItemsChangedMessage(nil, nil, 2),
			want: `{"type":"items_changed","results":[],"items":[],"participantCount":2}`,
		},
		{
			name: "state_changed",
			msg:  NewStateChangedMessage(true),
			want: `{"type":"state_changed","isOpen":true}`,
		},
		{
			name: "participants_changed",
			msg: NewParticipantsChangedMessage(
				[]ResultEntry{{Rank: 1, Name: "A"}},
				[]ItemEntry{{ID: 1, Name: "A", AddedByPublicID: "p", AddedByName: "U"}},
				[]ParticipantEntry{{DisplayName: "U", PublicID: "p"}},
			),
			want: `{"type":"participants_changed","results":[{"rank":1,"name":"A"}],"items":[{"id":1,"name":"A","addedByPublicId":"p","addedByName":"U"}],"participants":[{"displayName":"U","publicId":"p"}],"participantCount":1}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.msg)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}
