// Package repository is the data access layer: it turns Go values into
// SQL and back, and knows nothing about HTTP or business rules.
//
// Dependency direction is Handler -> Service -> Repository. Nothing here
// may import service or handler.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"go-http-service/internal/model"
)

// Errors this layer reports in terms a caller can act on. The service
// layer translates them into its own errors so that handlers never need
// to import this package.
var (
	ErrUsernameTaken = errors.New("username already taken")
	ErrEmailTaken    = errors.New("email already taken")
)

// pgUniqueViolation is PostgreSQL's SQLSTATE for a unique constraint
// breach. See https://www.postgresql.org/docs/current/errcodes-appendix.html
const pgUniqueViolation = "23505"

// Index names from 0001_create_users.sql. Renaming an index there
// without changing these turns a precise 409 into a generic 500.
const (
	usernameIndex = "users_username_key"
	emailIndex    = "users_email_key"
)

// UserRepository reads and writes the users table.
type UserRepository struct {
	pool *pgxpool.Pool
}

// NewUserRepository wires the repository to a pool.
func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

// Create inserts a user and fills in the database-generated fields.
//
// It does NOT check whether the username is free first. That check would
// be a time-of-check-to-time-of-use race: two concurrent registrations
// both find the name available, both insert, and one gets a 500 from a
// constraint the code claimed to have handled. Inserting and translating
// the unique violation is both simpler and actually correct, because the
// database evaluates the constraint atomically with the write.
//
// ctx is passed straight through, so a request deadline reaches the
// query. See CLAUDE.md 4.6.
func (r *UserRepository) Create(ctx context.Context, u *model.User) error {
	const query = `
		INSERT INTO users (username, email, password_hash)
		VALUES ($1, $2, $3)
		RETURNING id, created_at, updated_at`

	err := r.pool.QueryRow(ctx, query, u.Username, u.Email, u.PasswordHash).
		Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return translateCreateError(err)
	}
	return nil
}

// translateCreateError maps a driver error onto this layer's vocabulary.
func translateCreateError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgUniqueViolation {
		return fmt.Errorf("insert user: %w", err)
	}

	// ConstraintName tells us which index rejected the row, which is the
	// difference between "pick another username" and "you already have an
	// account".
	switch pgErr.ConstraintName {
	case usernameIndex:
		return ErrUsernameTaken
	case emailIndex:
		return ErrEmailTaken
	default:
		return fmt.Errorf("insert user: unexpected unique violation on %q: %w",
			pgErr.ConstraintName, err)
	}
}
