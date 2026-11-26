// Package protocol defines the JSON messages exchanged between the server and
// the browser. TypeScript definitions are generated from this file; run
// `go generate ./internal/protocol` after changing it.
package protocol

//go:generate go tool tygo generate

// MessageType discriminates WebSocket messages sent to the browser.
type MessageType string

const (
	TypeResults             MessageType = "results"
	TypeItemsChanged        MessageType = "items_changed"
	TypeStateChanged        MessageType = "state_changed"
	TypeParticipantsChanged MessageType = "participants_changed"
)

// ResultEntry is one item in the computed Schulze ranking.
type ResultEntry struct {
	Rank int    `json:"rank"`
	Name string `json:"name"`
}

// ItemEntry is a ballot item for client-side rendering.
type ItemEntry struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	AddedBy     string `json:"addedBy"`
	AddedByName string `json:"addedByName"`
}

// ParticipantEntry is a ballot participant for client-side rendering.
type ParticipantEntry struct {
	DisplayName   string `json:"displayName"`
	ParticipantID string `json:"participantId"`
}

// ResultsMessage is sent when rankings change.
type ResultsMessage struct {
	Type             MessageType   `json:"type" tstype:"\"results\""`
	Results          []ResultEntry `json:"results"`
	ParticipantCount int           `json:"participantCount"`
}

// ItemsChangedMessage is sent when items are added or deleted.
type ItemsChangedMessage struct {
	Type             MessageType   `json:"type" tstype:"\"items_changed\""`
	Results          []ResultEntry `json:"results"`
	Items            []ItemEntry   `json:"items"`
	ParticipantCount int           `json:"participantCount"`
}

// StateChangedMessage is sent when the ballot is opened or closed.
type StateChangedMessage struct {
	Type   MessageType `json:"type" tstype:"\"state_changed\""`
	IsOpen bool        `json:"isOpen"`
}

// ParticipantsChangedMessage is sent when participants join or rename.
type ParticipantsChangedMessage struct {
	Type             MessageType        `json:"type" tstype:"\"participants_changed\""`
	Results          []ResultEntry      `json:"results"`
	Items            []ItemEntry        `json:"items"`
	Participants     []ParticipantEntry `json:"participants"`
	ParticipantCount int                `json:"participantCount"`
}

// SaveRankingsRequest is the body of POST /ballot/{id}/rankings.
type SaveRankingsRequest struct {
	Order []int64 `json:"order"`
}

// ToggleResponse is the body returned by POST /ballot/{id}/toggle.
type ToggleResponse struct {
	IsOpen bool `json:"isOpen"`
}

// RankedItem is one entry in the body returned by
// GET /ballot/{id}/rankings/{participantID}.
type RankedItem struct {
	Name string `json:"name"`
}

func NewResultsMessage(results []ResultEntry, participantCount int) ResultsMessage {
	return ResultsMessage{
		Type:             TypeResults,
		Results:          nonNil(results),
		ParticipantCount: participantCount,
	}
}

func NewItemsChangedMessage(results []ResultEntry, items []ItemEntry, participantCount int) ItemsChangedMessage {
	return ItemsChangedMessage{
		Type:             TypeItemsChanged,
		Results:          nonNil(results),
		Items:            nonNil(items),
		ParticipantCount: participantCount,
	}
}

func NewStateChangedMessage(isOpen bool) StateChangedMessage {
	return StateChangedMessage{Type: TypeStateChanged, IsOpen: isOpen}
}

func NewParticipantsChangedMessage(results []ResultEntry, items []ItemEntry, participants []ParticipantEntry) ParticipantsChangedMessage {
	return ParticipantsChangedMessage{
		Type:             TypeParticipantsChanged,
		Results:          nonNil(results),
		Items:            nonNil(items),
		Participants:     nonNil(participants),
		ParticipantCount: len(participants),
	}
}

// nonNil ensures slices encode as [] rather than null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
