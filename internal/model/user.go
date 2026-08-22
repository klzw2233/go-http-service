package model

import "time"

// User limits. Username is deliberately narrow: alphanumeric only, so
// there is no ambiguity between visually similar names and nothing that
// needs escaping when it appears in a URL or a log line.
const (
	MinUsernameLen = 3
	MaxUsernameLen = 32

	// MinPasswordLen is the floor. There is no rune-based maximum here on
	// purpose: bcrypt truncates at 72 BYTES, and a binding tag counts
	// runes, so the real limit is enforced in the service layer. See
	// MaxPasswordBytes.
	MinPasswordLen = 8

	// MaxPasswordBytes is bcrypt's hard limit. Anything longer is
	// silently ignored by the algorithm, which would mean two different
	// passwords sharing their first 72 bytes both unlock the account.
	MaxPasswordBytes = 72

	// MaxEmailLen is the practical maximum length of an address.
	MaxEmailLen = 254
)

// User is a registered account as stored in the database.
//
// PasswordHash carries `json:"-"` so it cannot be serialised by accident,
// but do not rely on that alone: hand a UserResponse to the client
// instead. A struct tag is easy to lose in a refactor, and losing this
// one puts password hashes on the wire.
type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CreateUserRequest is the body of POST /api/users.
//
// The password has no max here; see MaxPasswordBytes for why it cannot
// be expressed as a binding tag.
type CreateUserRequest struct {
	Username string `json:"username" binding:"required,min=3,max=32,alphanum"`
	Email    string `json:"email"    binding:"required,email,max=254"`
	Password string `json:"password" binding:"required,min=8"`
}

// UserResponse is what a client receives. It exists so that leaking a
// password hash requires adding a field on purpose, rather than merely
// forgetting a struct tag.
type UserResponse struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

// NewUserResponse projects a User onto its public fields.
func NewUserResponse(u User) UserResponse {
	return UserResponse{
		ID:        u.ID,
		Username:  u.Username,
		Email:     u.Email,
		CreatedAt: u.CreatedAt,
	}
}
