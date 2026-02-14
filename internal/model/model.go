package model

import "time"

// User represents a participant identified by a browser cookie.
type User struct {
	ID          string
	DisplayName string
	CreatedAt   time.Time
}

// Ballot represents a voting session.
type Ballot struct {
	ID        string
	Title     string
	IsOpen    bool
	CreatedBy string
	CreatedAt time.Time
}

// Item represents a candidate option on a ballot.
type Item struct {
	ID          int64
	BallotID    string
	Name        string
	AddedBy     string
	AddedByName string
	CreatedAt   time.Time
}

// Ranking represents one user's ranking of one item.
type Ranking struct {
	BallotID string
	UserID   string
	ItemID   int64
	Position int // 1 = most preferred
}

// Participant represents a ballot participant with their display name and ID.
type Participant struct {
	DisplayName   string
	ParticipantID string
}
