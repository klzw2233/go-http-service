package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"go-http-service/internal/auth"
	"go-http-service/internal/model"
	"go-http-service/internal/repository"
)

// ErrInvalidCredentials is returned for every failed login.
//
// One error for "no such user" and "wrong password" is the whole point.
// Distinguishing them turns the endpoint into a username oracle: an
// attacker submits candidate names with a junk password, learns which
// accounts exist, and then spends its guesses only on those.
//
// The same error covers a missing, expired, or already-revoked refresh
// token. Refresh is also a credential check.
var ErrInvalidCredentials = errors.New("invalid username or password")

// ErrRefreshTokenReused means a refresh token was presented after it had
// already been rotated. Every session for that user has been revoked.
// Handlers still map this to the same 401 as ErrInvalidCredentials; the
// distinct value exists so the access log can record that a theft was
// assumed.
var ErrRefreshTokenReused = errors.New("refresh token reused")

// userFinder is the slice of the repository this service reads.
type userFinder interface {
	FindByUsername(ctx context.Context, username string) (*model.User, error)
	FindByID(ctx context.Context, id int64) (*model.User, error)
}

// tokenIssuer mints access tokens.
type tokenIssuer interface {
	IssueAccess(userID int64) (string, time.Time, error)
}

// refreshStore persists refresh-token hashes.
type refreshStore interface {
	Store(ctx context.Context, userID int64, hash string, expiresAt time.Time) error
	TryRotate(ctx context.Context, oldHash, newHash string, newExpiry, now time.Time) (userID int64, err error)
	RevokeByHash(ctx context.Context, hash string, now time.Time) error
}

// LoginResult carries everything a successful login or refresh produces.
type LoginResult struct {
	User         *model.User
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// AuthService authenticates users and issues tokens.
type AuthService struct {
	users      userFinder
	tokens     tokenIssuer
	refresh    refreshStore
	refreshTTL time.Duration
	now        func() time.Time

	// dummyHash is compared against when no user matched, so a miss
	// costs the same as a wrong password. See Login.
	dummyHash []byte
}

// AuthOption customises an AuthService.
type AuthOption func(*authOptions)

type authOptions struct {
	cost int
	now  func() time.Time
}

// WithAuthBcryptCost sets the cost of the timing-equalising comparison.
// It must match the cost real hashes were created with, or the two
// paths take measurably different times again.
func WithAuthBcryptCost(cost int) AuthOption {
	return func(o *authOptions) { o.cost = cost }
}

// WithAuthClock replaces the time source, so tests can expire a refresh
// token without sleeping.
func WithAuthClock(now func() time.Time) AuthOption {
	return func(o *authOptions) { o.now = now }
}

// NewAuthService builds the service.
//
// The dummy hash is generated here at the configured cost rather than
// being a hardcoded constant. A constant baked at one cost would verify
// in a different time than real hashes whenever the cost differed,
// which is exactly the gap this defence exists to erase - and tests
// lower the cost deliberately.
func NewAuthService(users userFinder, tokens tokenIssuer, refresh refreshStore, refreshTTL time.Duration, opts ...AuthOption) (*AuthService, error) {
	if refreshTTL <= 0 {
		return nil, fmt.Errorf("refresh token TTL must be positive, got %s", refreshTTL)
	}

	options := authOptions{
		cost: bcrypt.DefaultCost,
		now:  func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(&options)
	}

	dummy, err := bcrypt.GenerateFromPassword(
		[]byte("timing-equalisation-placeholder"), options.cost)
	if err != nil {
		return nil, fmt.Errorf("prepare timing placeholder: %w", err)
	}

	return &AuthService{
		users:      users,
		tokens:     tokens,
		refresh:    refresh,
		refreshTTL: refreshTTL,
		now:        options.now,
		dummyHash:  dummy,
	}, nil
}

// Login verifies credentials and issues an access/refresh pair.
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

	return s.issuePair(ctx, user)
}

// Refresh exchanges a still-valid refresh token for a new pair.
//
// The old token is revoked as part of the exchange. Presenting it a
// second time is treated as theft: every refresh token for that user is
// revoked and the caller gets ErrRefreshTokenReused.
func (s *AuthService) Refresh(ctx context.Context, raw string) (*LoginResult, error) {
	newRaw, newHash, err := auth.NewRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}

	now := s.now()
	userID, err := s.refresh.TryRotate(ctx, auth.HashRefresh(raw), newHash, now.Add(s.refreshTTL), now)
	if err != nil {
		return nil, translateRefreshError(err)
	}

	token, expiresAt, err := s.tokens.IssueAccess(userID)
	if err != nil {
		return nil, fmt.Errorf("issue access token: %w", err)
	}

	return &LoginResult{
		AccessToken:  token,
		RefreshToken: newRaw,
		ExpiresAt:    expiresAt,
	}, nil
}

// Logout revokes one refresh token. Missing, expired, and already
// revoked tokens all look the same: ErrInvalidCredentials. A second
// logout is not treated as replay; that would log a user out of every
// device because they double-submitted.
func (s *AuthService) Logout(ctx context.Context, raw string) error {
	err := s.refresh.RevokeByHash(ctx, auth.HashRefresh(raw), s.now())
	if err != nil {
		return translateRefreshError(err)
	}
	return nil
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

func (s *AuthService) issuePair(ctx context.Context, user *model.User) (*LoginResult, error) {
	raw, hash, err := auth.NewRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}

	if err := s.refresh.Store(ctx, user.ID, hash, s.now().Add(s.refreshTTL)); err != nil {
		return nil, fmt.Errorf("store refresh token: %w", err)
	}

	token, expiresAt, err := s.tokens.IssueAccess(user.ID)
	if err != nil {
		return nil, fmt.Errorf("issue access token: %w", err)
	}

	return &LoginResult{
		User:         user,
		AccessToken:  token,
		RefreshToken: raw,
		ExpiresAt:    expiresAt,
	}, nil
}

func translateRefreshError(err error) error {
	switch {
	case errors.Is(err, repository.ErrRefreshTokenNotFound):
		return ErrInvalidCredentials
	case errors.Is(err, repository.ErrRefreshTokenReused):
		return ErrRefreshTokenReused
	default:
		return fmt.Errorf("refresh token: %w", err)
	}
}
