package auth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testSecret is exactly at the minimum length, so the length rule and
// the happy path are exercised by the same value.
const testSecret = "0123456789abcdef0123456789abcdef"

var testNow = time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

// fixedClock returns a clock pinned at testNow plus an offset, so tests
// can move past an expiry without sleeping.
func fixedClock(offset time.Duration) func() time.Time {
	return func() time.Time { return testNow.Add(offset) }
}

func newTestIssuer(t *testing.T, opts ...Option) *TokenIssuer {
	t.Helper()

	all := append([]Option{WithClock(fixedClock(0))}, opts...)
	issuer, err := NewTokenIssuer(testSecret, 15*time.Minute, all...)
	require.NoError(t, err)

	return issuer
}

func TestNewTokenIssuer_RejectsWeakSecret(t *testing.T) {
	t.Parallel()

	// HMAC is only as strong as its key: a short one can be recovered
	// offline from any captured token, after which an attacker mints
	// tokens for any user they like.
	_, err := NewTokenIssuer(strings.Repeat("a", MinSecretLen-1), time.Minute)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least 32 bytes")
}

func TestNewTokenIssuer_RejectsNonPositiveTTL(t *testing.T) {
	t.Parallel()

	_, err := NewTokenIssuer(testSecret, 0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be positive")
}

func TestIssueAndParse_RoundTrip(t *testing.T) {
	t.Parallel()

	issuer := newTestIssuer(t)

	token, expiresAt, err := issuer.IssueAccess(42)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	assert.True(t, expiresAt.Equal(testNow.Add(15*time.Minute)))

	got, err := issuer.ParseAccess(token)
	require.NoError(t, err)
	assert.Equal(t, int64(42), got)
}

// TestClaimsCarryNoPersonalData is a reminder that a JWT payload is
// base64, not ciphertext. Anything put in the claims is readable by
// whoever holds the token.
func TestClaimsCarryNoPersonalData(t *testing.T) {
	t.Parallel()

	token, _, err := newTestIssuer(t).IssueAccess(42)
	require.NoError(t, err)

	parts := strings.Split(token, ".")
	require.Len(t, parts, 3, "JWT 应有三段")

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err, "payload 只是 base64，任何人都能解开")

	var claims map[string]any
	require.NoError(t, json.Unmarshal(payload, &claims))

	keys := make([]string, 0, len(claims))
	for k := range claims {
		keys = append(keys, k)
	}
	assert.ElementsMatch(t, []string{"sub", "iss", "iat", "exp"}, keys,
		"claims 里只应有 user id 和标准字段，不该出现用户名或邮箱")
}

// TestParseAccess_RejectsExpired uses a clock rather than a sleep, so
// the test is instant and cannot flake on a loaded machine.
func TestParseAccess_RejectsExpired(t *testing.T) {
	t.Parallel()

	issuer := newTestIssuer(t)

	token, _, err := issuer.IssueAccess(42)
	require.NoError(t, err)

	// Same secret, same issuer, only later.
	future := newTestIssuer(t, WithClock(fixedClock(16*time.Minute)))

	_, err = future.ParseAccess(token)

	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestParseAccess_AcceptsJustBeforeExpiry(t *testing.T) {
	t.Parallel()

	token, _, err := newTestIssuer(t).IssueAccess(42)
	require.NoError(t, err)

	almost := newTestIssuer(t, WithClock(fixedClock(14*time.Minute)))

	got, err := almost.ParseAccess(token)
	require.NoError(t, err)
	assert.Equal(t, int64(42), got)
}

// TestParseAccess_RejectsForeignSignature guards against verifying only
// that a token decodes. A token signed with a different key must not be
// accepted no matter how well-formed it looks.
func TestParseAccess_RejectsForeignSignature(t *testing.T) {
	t.Parallel()

	other, err := NewTokenIssuer("ffffffffffffffffffffffffffffffff", 15*time.Minute,
		WithClock(fixedClock(0)))
	require.NoError(t, err)

	token, _, err := other.IssueAccess(42)
	require.NoError(t, err)

	_, err = newTestIssuer(t).ParseAccess(token)

	require.ErrorIs(t, err, ErrInvalidToken)
}

// TestParseAccess_RejectsAlgNone covers the classic JWT attack: strip
// the signature and set alg to none. The token header is
// attacker-controlled, so it must never get to choose how the token is
// verified - which is what WithValidMethods enforces.
func TestParseAccess_RejectsAlgNone(t *testing.T) {
	t.Parallel()

	claims := jwt.RegisteredClaims{
		Subject:   "42",
		Issuer:    tokenIssuer,
		IssuedAt:  jwt.NewNumericDate(testNow),
		ExpiresAt: jwt.NewNumericDate(testNow.Add(time.Hour)),
	}

	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = newTestIssuer(t).ParseAccess(unsigned)

	require.ErrorIs(t, err, ErrInvalidToken, "alg=none 的 token 必须被拒绝")
}

// TestParseAccess_RejectsWrongIssuer stops a token minted by another
// service that happens to share the key from being accepted here.
func TestParseAccess_RejectsWrongIssuer(t *testing.T) {
	t.Parallel()

	claims := jwt.RegisteredClaims{
		Subject:   "42",
		Issuer:    "some-other-service",
		IssuedAt:  jwt.NewNumericDate(testNow),
		ExpiresAt: jwt.NewNumericDate(testNow.Add(time.Hour)),
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).
		SignedString([]byte(testSecret))
	require.NoError(t, err)

	_, err = newTestIssuer(t).ParseAccess(token)

	require.ErrorIs(t, err, ErrInvalidToken)
}

// TestParseAccess_RejectsMissingExpiry stops a token that never expires
// from being honoured, which WithExpirationRequired enforces.
func TestParseAccess_RejectsMissingExpiry(t *testing.T) {
	t.Parallel()

	claims := jwt.RegisteredClaims{
		Subject:  "42",
		Issuer:   tokenIssuer,
		IssuedAt: jwt.NewNumericDate(testNow),
		// 故意不设 ExpiresAt
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).
		SignedString([]byte(testSecret))
	require.NoError(t, err)

	_, err = newTestIssuer(t).ParseAccess(token)

	require.ErrorIs(t, err, ErrInvalidToken, "没有过期时间的 token 必须被拒绝")
}

func TestParseAccess_RejectsMalformed(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, token string }{
		{"空字符串", ""},
		{"不是 JWT", "hello"},
		{"段数不对", "a.b"},
		{"payload 不是 base64", "eyJhbGciOiJIUzI1NiJ9.!!!.sig"},
		{"签名被截断", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiI0MiJ9."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := newTestIssuer(t).ParseAccess(tt.token)

			require.ErrorIs(t, err, ErrInvalidToken)
		})
	}
}

// TestParseAccess_RejectsNonNumericSubject covers a token that is
// otherwise valid but whose subject cannot be a user id.
func TestParseAccess_RejectsNonNumericSubject(t *testing.T) {
	t.Parallel()

	for _, subject := range []string{"jimmy", "", "0", "-1"} {
		claims := jwt.RegisteredClaims{
			Subject:   subject,
			Issuer:    tokenIssuer,
			IssuedAt:  jwt.NewNumericDate(testNow),
			ExpiresAt: jwt.NewNumericDate(testNow.Add(time.Hour)),
		}

		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).
			SignedString([]byte(testSecret))
		require.NoError(t, err)

		_, err = newTestIssuer(t).ParseAccess(token)

		require.ErrorIs(t, err, ErrInvalidToken, "subject=%q 应被拒绝", subject)
	}
}

// TestErrInvalidToken_IsUniform is the reason every failure path returns
// the same error. "expired" tells an attacker the signature checked out,
// which is a different and much more useful answer than "malformed".
func TestErrInvalidToken_IsUniform(t *testing.T) {
	t.Parallel()

	issuer := newTestIssuer(t)

	expired, _, err := issuer.IssueAccess(42)
	require.NoError(t, err)

	future := newTestIssuer(t, WithClock(fixedClock(time.Hour)))

	_, expiredErr := future.ParseAccess(expired)
	_, garbageErr := issuer.ParseAccess("not-a-token")

	assert.Equal(t, expiredErr.Error(), garbageErr.Error(),
		"过期与畸形必须返回完全相同的错误")
}
