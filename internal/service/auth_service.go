package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"go-http-service/internal/model"
	"go-http-service/internal/repository"
)

// ErrInvalidCredentials is returned for every failed login.
//
// One error for "no such user" and "wrong password" is the whole point.
// Distinguishing them turns the endpoint into a username oracle: an
// attacker submits candidate names with a junk password, learns which
// accounts exist, and then spends its guesses only on those.
var ErrInvalidCredentials = errors.New("invalid username or password")

// userFinder is the slice of the repository this service reads.
type userFinder interface {
	FindByUsername(ctx context.Context, username string) (*model.User, error)
	FindByID(ctx context.Context, id int64) (*model.User, error)
}

// tokenIssuer mints access tokens.
type tokenIssuer interface {
	IssueAccess(userID int64) (string, time.Time, error)
}

// LoginResult carries everything a successful login produces.
type LoginResult struct {
	User        *model.User
	AccessToken string
	ExpiresAt   time.Time
}

// AuthService authenticates users and issues tokens.
type AuthService struct {
	users  userFinder
	tokens tokenIssuer

	// dummyHash is compared against when no user matched, so a miss
	// costs the same as a wrong password. See Login.
	dummyHash []byte
}

// AuthOption customises an AuthService.
type AuthOption func(*authOptions)

type authOptions struct {
	cost int
}

// WithAuthBcryptCost sets the cost of the timing-equalising comparison.
// It must match the cost real hashes were created with, or the two
// paths take measurably different times again.
func WithAuthBcryptCost(cost int) AuthOption {
	return func(o *authOptions) { o.cost = cost }
}

// NewAuthService builds the service.
//
// The dummy hash is generated here at the configured cost rather than
// being a hardcoded constant. A constant baked at one cost would verify
// in a different time than real hashes whenever the cost differed,
// which is exactly the gap this defence exists to erase - and tests
// lower the cost deliberately.
func NewAuthService(users userFinder, tokens tokenIssuer, opts ...AuthOption) (*AuthService, error) {
	options := authOptions{cost: bcrypt.DefaultCost}
	for _, opt := range opts {
		opt(&options)
	}

	dummy, err := bcrypt.GenerateFromPassword(
		[]byte("timing-equalisation-placeholder"), options.cost)
	if err != nil {
		return nil, fmt.Errorf("prepare timing placeholder: %w", err)
	}

	return &AuthService{users: users, tokens: tokens, dummyHash: dummy}, nil
}

// Login verifies credentials and issues an access token.
func (s *AuthService) Login(ctx context.Context, username, password string) (*LoginResult, error) {
	user, err := s.users.FindByUsername(ctx, username)
	if err != nil {
		if !errors.Is(err, repository.ErrUserNotFound) {
			return nil, fmt.Errorf("look up user: %w", err)
		}

		// Spend the same work as a real comparison before failing.
		//
		// Returning here directly would be roughly 60ms quicker than the
		// path that runs bcrypt, and that gap is measurable across a
		// handful of requests. An attacker who can time the response
		// learns which usernames exist even though both answers say the
		// same thing, so an identical error alone is not enough.
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	token, expiresAt, err := s.tokens.IssueAccess(user.ID)
	if err != nil {
		return nil, fmt.Errorf("issue access token: %w", err)
	}

	return &LoginResult{User: user, AccessToken: token, ExpiresAt: expiresAt}, nil
}

// UserByID resolves the subject of an authenticated request.
func (s *AuthService) UserByID(ctx context.Context, id int64) (*model.User, error) {
	user, err := s.users.FindByID(ctx, id)
	if errors.Is(err, repository.ErrUserNotFound) {
		// The token verified but the account is gone, deleted since it
		// was issued. That is an authentication failure, not a server
		// error: the credential no longer identifies anyone.
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("look up user: %w", err)
	}

	return user, nil
}
