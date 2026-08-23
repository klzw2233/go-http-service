package model

import "time"

// LoginRequest is the body of POST /api/auth/login.
//
// The rules are looser than CreateUserRequest on purpose. Validation
// here would only tell an attacker which usernames could not exist,
// and rejecting a short password before checking it leaks that the
// stored one is longer. Anything that parses gets the same treatment.
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// TokenPair is what a successful login or refresh returns.
type TokenPair struct {
	AccessToken string `json:"access_token"`
	// RefreshToken is the long-lived credential used to mint a new pair.
	// It is shown once; the server stores only a hash of it.
	RefreshToken string `json:"refresh_token"`
	// TokenType is always "Bearer". Returned so a client can build the
	// Authorization header without hardcoding the scheme.
	TokenType string `json:"token_type"`
	// ExpiresAt is when the access token stops working, so a client can
	// refresh ahead of time instead of waiting for a 401.
	ExpiresAt time.Time `json:"expires_at"`
}

// RefreshRequest is the body of POST /api/auth/refresh and /api/auth/logout.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// NewTokenPair builds the response for a newly issued pair.
func NewTokenPair(accessToken, refreshToken string, expiresAt time.Time) TokenPair {
	return TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresAt:    expiresAt,
	}
}
