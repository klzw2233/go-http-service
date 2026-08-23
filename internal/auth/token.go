// Package auth mints and verifies credentials. It is a leaf package: it
// touches no database and no HTTP, so both the service and the handler
// layers may use it without bending the dependency direction.
package auth

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidToken covers every reason a token was not accepted.
//
// One error for missing, malformed, expired, and forged is deliberate.
// Telling a caller which applies tells an attacker how close they are:
// "expired" confirms the signature was valid, which is a very different
// piece of news from "malformed".
var ErrInvalidToken = errors.New("token is not valid")

// MinSecretLen is the shortest HS256 key this service accepts.
//
// HMAC's strength is bounded by the entropy of its key, and a short one
// can be brute-forced offline against any captured token - at which
// point an attacker can mint tokens for any user. 32 bytes matches the
// output size of SHA-256, which is what HS256 uses.
const MinSecretLen = 32

// tokenIssuer is the iss claim, so a token minted for this service is
// not silently accepted by another one sharing the key.
const tokenIssuer = "go-http-service"

// TokenIssuer signs and verifies access tokens.
type TokenIssuer struct {
	secret    []byte
	accessTTL time.Duration
	now       func() time.Time
}

// Option customises a TokenIssuer.
type Option func(*TokenIssuer)

// WithClock replaces the time source, so tests can expire a token
// without sleeping.
func WithClock(fn func() time.Time) Option {
	return func(t *TokenIssuer) { t.now = fn }
}

// NewTokenIssuer builds an issuer, rejecting a secret too short to be
// safe.
func NewTokenIssuer(secret string, accessTTL time.Duration, opts ...Option) (*TokenIssuer, error) {
	if len(secret) < MinSecretLen {
		return nil, fmt.Errorf("jwt secret must be at least %d bytes, got %d",
			MinSecretLen, len(secret))
	}
	if accessTTL <= 0 {
		return nil, fmt.Errorf("access token TTL must be positive, got %s", accessTTL)
	}

	t := &TokenIssuer{
		secret:    []byte(secret),
		accessTTL: accessTTL,
		now:       time.Now,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t, nil
}

// IssueAccess mints a token for a user and reports when it expires.
//
// The claims carry the user id and nothing else. A JWT payload is
// base64, not ciphertext: anybody holding the token can read it, so a
// username or an email address in there is simply published.
func (t *TokenIssuer) IssueAccess(userID int64) (string, time.Time, error) {
	now := t.now()
	expiresAt := now.Add(t.accessTTL)

	claims := jwt.RegisteredClaims{
		Subject:   strconv.FormatInt(userID, 10),
		Issuer:    tokenIssuer,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}

	return signed, expiresAt, nil
}

// ParseAccess verifies a token and returns the user it identifies.
func (t *TokenIssuer) ParseAccess(token string) (int64, error) {
	parsed, err := jwt.ParseWithClaims(token, &jwt.RegisteredClaims{},
		func(*jwt.Token) (any, error) { return t.secret, nil },
		// Pinning the algorithm is the defence against algorithm
		// confusion. Without it a token claiming alg:none, or an HS256
		// token forged with a public key when the server expects RS256,
		// can be accepted as valid. The header is attacker-controlled,
		// so it must never decide how the token is verified.
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(t.now),
	)
	if err != nil {
		return 0, ErrInvalidToken
	}

	claims, ok := parsed.Claims.(*jwt.RegisteredClaims)
	if !ok || !parsed.Valid {
		return 0, ErrInvalidToken
	}

	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || userID <= 0 {
		return 0, ErrInvalidToken
	}

	return userID, nil
}
