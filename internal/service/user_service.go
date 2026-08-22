// Package service holds business rules. It sits between the HTTP layer
// and the database, and knows about neither gin nor SQL.
//
// Dependency direction is Handler -> Service -> Repository. Nothing here
// may import handler.
package service

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"go-http-service/internal/model"
	"go-http-service/internal/repository"
)

// Errors callers can act on.
//
// These deliberately mirror the repository's errors instead of
// re-exporting them. Handlers match on these, so handlers never import
// repository, and the dependency direction stays one-way even for error
// values.
var (
	ErrUsernameTaken   = errors.New("username already taken")
	ErrEmailTaken      = errors.New("email already taken")
	ErrPasswordTooLong = errors.New("password exceeds the maximum length")
)

// userStore is the slice of the repository this service needs. Declaring
// it here rather than depending on the concrete type keeps the service
// testable without a database.
type userStore interface {
	Create(ctx context.Context, u *model.User) error
}

// UserService implements account operations.
type UserService struct {
	users userStore
	cost  int
}

// Option customises a UserService.
type Option func(*UserService)

// WithBcryptCost overrides the hashing cost.
//
// Tests use bcrypt.MinCost: the default cost takes roughly 60ms per hash
// by design, which is the correct trade-off in production and an
// unacceptable one when a test suite hashes dozens of times under the
// race detector.
func WithBcryptCost(cost int) Option {
	return func(s *UserService) { s.cost = cost }
}

// NewUserService builds a service over the given store.
func NewUserService(users userStore, opts ...Option) *UserService {
	s := &UserService{users: users, cost: bcrypt.DefaultCost}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// RegisterInput carries the values needed to create an account.
type RegisterInput struct {
	Username string
	Email    string
	Password string
}

// Register hashes the password and stores a new account.
//
// The returned User still carries PasswordHash; project it through
// model.NewUserResponse before it reaches a client.
func (s *UserService) Register(ctx context.Context, in RegisterInput) (*model.User, error) {
	// Length is checked in BYTES, not runes.
	//
	// bcrypt uses only the first 72 bytes of a password and discards the
	// rest without complaining. A binding tag cannot express this: gin's
	// validator measures strings with utf8.RuneCountInString, so a
	// 72-character CJK password passes `max=72` while actually being 216
	// bytes. Left unchecked, every password sharing its first 72 bytes
	// would open the same account.
	if len(in.Password) > model.MaxPasswordBytes {
		return nil, ErrPasswordTooLong
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), s.cost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &model.User{
		Username:     in.Username,
		Email:        in.Email,
		PasswordHash: string(hash),
	}

	if err := s.users.Create(ctx, user); err != nil {
		return nil, translateCreateError(err)
	}

	return user, nil
}

// translateCreateError lifts repository errors into this layer's own.
func translateCreateError(err error) error {
	switch {
	case errors.Is(err, repository.ErrUsernameTaken):
		return ErrUsernameTaken
	case errors.Is(err, repository.ErrEmailTaken):
		return ErrEmailTaken
	default:
		return fmt.Errorf("create user: %w", err)
	}
}
