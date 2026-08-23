package model

import "time"

// RefreshToken is a persisted refresh credential.
//
// The raw token never appears here. TokenHash is SHA-256 hex of the
// value sent to the client; a row stolen from the database cannot be
// presented back as a credential.
type RefreshToken struct {
	ID        int64
	UserID    int64
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}
