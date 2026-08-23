package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"go-http-service/internal/model"
)

// Errors specific to refresh tokens. The service layer translates them
// so handlers never import this package for anything but construction.
var (
	ErrRefreshTokenNotFound = errors.New("refresh token not found")
	// ErrRefreshTokenReused means a token that had already been rotated
	// was presented again. The family of tokens for that user has been
	// revoked as a result; see TryRotate.
	ErrRefreshTokenReused = errors.New("refresh token reused")
)

// RefreshTokenRepository reads and writes the refresh_tokens table.
type RefreshTokenRepository struct {
	pool *pgxpool.Pool
}

// NewRefreshTokenRepository wires the repository to a pool.
func NewRefreshTokenRepository(pool *pgxpool.Pool) *RefreshTokenRepository {
	return &RefreshTokenRepository{pool: pool}
}

// Store inserts a new refresh token hash. The raw token never reaches
// this layer.
func (r *RefreshTokenRepository) Store(ctx context.Context, userID int64, hash string, expiresAt time.Time) error {
	const query = `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)`

	if _, err := r.pool.Exec(ctx, query, userID, hash, expiresAt); err != nil {
		return fmt.Errorf("insert refresh token: %w", err)
	}
	return nil
}

// TryRotate atomically replaces an active refresh token with a new hash.
//
// The row is locked for the duration of the transaction so two concurrent
// refreshes cannot both succeed. Outcomes:
//
//   - missing or expired: ErrRefreshTokenNotFound, nothing else changes
//   - already revoked: every unrevoked token for that user is revoked and
//     ErrRefreshTokenReused is returned. A second use means the token
//     leaked; killing the whole family is the conservative response.
//   - active: the old row is revoked, the new hash is inserted, user id
//     is returned
func (r *RefreshTokenRepository) TryRotate(ctx context.Context, oldHash, newHash string, newExpiry, now time.Time) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin rotate: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var tok model.RefreshToken
	err = tx.QueryRow(ctx, `
		SELECT id, user_id, token_hash, expires_at, revoked_at, created_at
		FROM refresh_tokens
		WHERE token_hash = $1
		FOR UPDATE`, oldHash).Scan(
		&tok.ID, &tok.UserID, &tok.TokenHash, &tok.ExpiresAt, &tok.RevokedAt, &tok.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrRefreshTokenNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("lock refresh token: %w", err)
	}

	if tok.RevokedAt != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE refresh_tokens
			SET revoked_at = $2
			WHERE user_id = $1 AND revoked_at IS NULL`, tok.UserID, now); err != nil {
			return 0, fmt.Errorf("revoke token family: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return 0, fmt.Errorf("commit family revoke: %w", err)
		}
		return 0, ErrRefreshTokenReused
	}

	if !tok.ExpiresAt.After(now) {
		return 0, ErrRefreshTokenNotFound
	}

	if _, err := tx.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = $2 WHERE id = $1`, tok.ID, now); err != nil {
		return 0, fmt.Errorf("revoke used token: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)`, tok.UserID, newHash, newExpiry); err != nil {
		return 0, fmt.Errorf("insert rotated token: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit rotate: %w", err)
	}
	return tok.UserID, nil
}

// RevokeByHash marks an active, unexpired token revoked.
//
// Missing, expired, and already-revoked hashes all return
// ErrRefreshTokenNotFound: logout must not distinguish them, and it
// must not treat a second presentation as a replay. Replay detection
// belongs on the refresh path, where a reused token is actually being
// used to mint a new session.
func (r *RefreshTokenRepository) RevokeByHash(ctx context.Context, hash string, now time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = $2
		WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > $2`,
		hash, now)
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRefreshTokenNotFound
	}
	return nil
}
