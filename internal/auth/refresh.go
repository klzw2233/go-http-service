package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// refreshTokenBytes is the entropy of a refresh token. 32 bytes is the
// output size of SHA-256 and far beyond anything brute-forceable, which
// is why the stored form can be a fast hash rather than bcrypt.
const refreshTokenBytes = 32

// NewRefreshToken returns a raw token for the client and the hash that
// belongs in the database.
//
// The two must never be confused. The raw value is the credential; the
// hash is what a stolen database row contains, and hashing it again will
// not recover the raw token.
func NewRefreshToken() (raw, hash string, err error) {
	var b [refreshTokenBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}

	raw = base64.RawURLEncoding.EncodeToString(b[:])
	return raw, HashRefresh(raw), nil
}

// HashRefresh is the stored form of a refresh token: SHA-256, hex.
//
// bcrypt would be the wrong tool. A refresh token is 32 random bytes, so
// there is no low-entropy secret to stretch, and every refresh would pay
// the password-hashing cost for no gain. SHA-256 is one-way enough for
// an input the attacker cannot guess.
func HashRefresh(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
