package repository

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-http-service/internal/auth"
)

func persistedUser(t *testing.T) int64 {
	t.Helper()

	pool := testPool(t)
	repo := NewUserRepository(pool)

	u := newUser(t, pool, uniqueName("rft"))
	require.NoError(t, repo.Create(t.Context(), u))
	return u.ID
}

func TestStore_PersistsHashNotRaw(t *testing.T) {
	t.Parallel()

	pool := testPool(t)
	repo := NewRefreshTokenRepository(pool)
	userID := persistedUser(t)

	raw, hash, err := auth.NewRefreshToken()
	require.NoError(t, err)

	require.NoError(t, repo.Store(t.Context(), userID, hash, time.Now().Add(time.Hour)))

	var stored string
	err = pool.QueryRow(t.Context(),
		`SELECT token_hash FROM refresh_tokens WHERE user_id = $1`, userID).Scan(&stored)
	require.NoError(t, err)

	assert.Equal(t, hash, stored)
	assert.NotEqual(t, raw, stored, "库里绝不能是 token 原文")
	assert.Len(t, stored, 64)
	assert.NotContains(t, stored, "$2", "refresh token 不该用 bcrypt")
}

func TestTryRotate_ReplacesActiveToken(t *testing.T) {
	t.Parallel()

	pool := testPool(t)
	repo := NewRefreshTokenRepository(pool)
	userID := persistedUser(t)

	_, oldHash, err := auth.NewRefreshToken()
	require.NoError(t, err)
	_, newHash, err := auth.NewRefreshToken()
	require.NoError(t, err)

	now := time.Now().UTC()
	require.NoError(t, repo.Store(t.Context(), userID, oldHash, now.Add(time.Hour)))

	gotID, err := repo.TryRotate(t.Context(), oldHash, newHash, now.Add(24*time.Hour), now)
	require.NoError(t, err)
	assert.Equal(t, userID, gotID)

	var revokedAt *time.Time
	err = pool.QueryRow(t.Context(),
		`SELECT revoked_at FROM refresh_tokens WHERE token_hash = $1`, oldHash).Scan(&revokedAt)
	require.NoError(t, err)
	require.NotNil(t, revokedAt, "旧 token 应被标记撤销")

	var storedNew string
	err = pool.QueryRow(t.Context(),
		`SELECT token_hash FROM refresh_tokens WHERE token_hash = $1 AND revoked_at IS NULL`,
		newHash).Scan(&storedNew)
	require.NoError(t, err)
	assert.Equal(t, newHash, storedNew)
}

func TestTryRotate_UnknownHash(t *testing.T) {
	t.Parallel()

	repo := NewRefreshTokenRepository(testPool(t))
	_, hash, err := auth.NewRefreshToken()
	require.NoError(t, err)

	_, err = repo.TryRotate(t.Context(), "deadbeef", hash, time.Now().Add(time.Hour), time.Now())
	require.ErrorIs(t, err, ErrRefreshTokenNotFound)
}

func TestTryRotate_ExpiredIsNotFound(t *testing.T) {
	t.Parallel()

	pool := testPool(t)
	repo := NewRefreshTokenRepository(pool)
	userID := persistedUser(t)

	_, oldHash, err := auth.NewRefreshToken()
	require.NoError(t, err)
	_, newHash, err := auth.NewRefreshToken()
	require.NoError(t, err)

	now := time.Now().UTC()
	require.NoError(t, repo.Store(t.Context(), userID, oldHash, now.Add(-time.Minute)))

	_, err = repo.TryRotate(t.Context(), oldHash, newHash, now.Add(time.Hour), now)
	require.ErrorIs(t, err, ErrRefreshTokenNotFound)

	var n int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT count(*) FROM refresh_tokens WHERE token_hash = $1`, newHash).Scan(&n))
	assert.Zero(t, n, "过期 token 不应被轮转出新行")
}

// TestTryRotate_ReplayRevokesFamily is the reason refresh tokens are
// stored at all. A second use of a rotated token means it leaked; every
// session for that user is then untrusted.
func TestTryRotate_ReplayRevokesFamily(t *testing.T) {
	t.Parallel()

	pool := testPool(t)
	repo := NewRefreshTokenRepository(pool)
	userID := persistedUser(t)

	_, firstHash, err := auth.NewRefreshToken()
	require.NoError(t, err)
	_, secondHash, err := auth.NewRefreshToken()
	require.NoError(t, err)
	_, thirdHash, err := auth.NewRefreshToken()
	require.NoError(t, err)

	now := time.Now().UTC()
	require.NoError(t, repo.Store(t.Context(), userID, firstHash, now.Add(time.Hour)))

	_, err = repo.TryRotate(t.Context(), firstHash, secondHash, now.Add(time.Hour), now)
	require.NoError(t, err)

	_, err = repo.TryRotate(t.Context(), firstHash, thirdHash, now.Add(time.Hour), now)
	require.ErrorIs(t, err, ErrRefreshTokenReused)

	var active int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT count(*) FROM refresh_tokens WHERE user_id = $1 AND revoked_at IS NULL`,
		userID).Scan(&active))
	assert.Zero(t, active, "重放后该用户不应再有可用的 refresh token")
}

func TestRevokeByHash_ActiveToken(t *testing.T) {
	t.Parallel()

	pool := testPool(t)
	repo := NewRefreshTokenRepository(pool)
	userID := persistedUser(t)

	_, hash, err := auth.NewRefreshToken()
	require.NoError(t, err)

	now := time.Now().UTC()
	require.NoError(t, repo.Store(t.Context(), userID, hash, now.Add(time.Hour)))
	require.NoError(t, repo.RevokeByHash(t.Context(), hash, now))

	err = repo.RevokeByHash(t.Context(), hash, now)
	require.ErrorIs(t, err, ErrRefreshTokenNotFound, "再次登出应与未找到无法区分")
}

func TestRevokeByHash_Unknown(t *testing.T) {
	t.Parallel()

	repo := NewRefreshTokenRepository(testPool(t))
	err := repo.RevokeByHash(t.Context(), "nope", time.Now())
	require.ErrorIs(t, err, ErrRefreshTokenNotFound)
}
