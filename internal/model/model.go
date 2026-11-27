package model

import "time"

// Ballot represents a voting session.
type Ballot struct {
	ID              string
	Title           string
	IsOpen          bool // accepting new participants
	RankingsPrivate bool
	CreatedAt       time.Time
}

// Item represents a candidate option on a ballot.
type Item struct {
	ID          int64
	BallotID    string
	Name        string
	AddedBy     string // participant ID
	AddedByName string
	CreatedAt   time.Time

	// RankedByOthers reports whether anyone other than the adder has ranked
	// the item; once they have, only the creator can delete it.
	RankedByOthers bool
}

// Ranking represents one participant's ranking of one item.
type Ranking struct {
	BallotID      string
	ParticipantID string
	ItemID        int64
	Position      int // 1 = most preferred
}

// Participant is a member of one ballot, as other participants see them.
type Participant struct {
	ID          string
	DisplayName string
}
