package model

import "time"

// User represents a participant identified by a browser cookie.
type User struct {
	ID        string
	CreatedAt time.Time
}

// Ballot represents a voting session.
type Ballot struct {
	ID        string
	Title     string
	CreatedBy string
	CreatedAt time.Time
}

// Item represents a candidate option on a ballot.
type Item struct {
	ID        int64
	BallotID  string
	Name      string
	AddedBy   string
	CreatedAt time.Time
}

// Ranking represents one user's ranking of one item.
type Ranking struct {
	BallotID string
	UserID   string
	ItemID   int64
	Position int // 1 = most preferred
}
