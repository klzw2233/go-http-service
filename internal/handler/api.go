package handler

import (
	"context"
	"log/slog"
	"time"

	"go-http-service/internal/config"
	"go-http-service/internal/model"
	"go-http-service/internal/service"
)

// API holds everything the HTTP handlers need.
//
// The handlers used to be package-level functions, which meant they could
// not reach a logger, a configurable clock, or — the reason this exists —
// a database pool. As methods on this struct, gaining a dependency is
// adding a field and wiring it once in main.
type API struct {
	cfg *config.Config
	log *slog.Logger

	// now supplies the current time. A field rather than a direct
	// time.Now call so tests can pin timestamps. It replaces the
	// package-level var that previously forced every test touching time
	// to save and restore global state.
	now func() time.Time

	// users implements account operations. Declared as an interface so
	// the handler tests can drive it without a database, and so this
	// package does not reach past the service layer.
	users userRegistrar

	// auth verifies credentials and resolves authenticated callers.
	auth authenticator

	// tokens verifies access tokens. Separate from auth because the
	// middleware only needs verification, not the database.
	tokens tokenVerifier

	// posts implements Post operations behind the Author check. Declared
	// as an interface so handler tests drive it without a database, the
	// same way users/auth are stubbed.
	posts postService

	readyChecks []readyCheck
}

// userRegistrar is the slice of the user service the handlers use.
type userRegistrar interface {
	Register(ctx context.Context, in service.RegisterInput) (*model.User, error)
}

// authenticator is the slice of the auth service the handlers use.
type authenticator interface {
	Login(ctx context.Context, username, password string) (*service.LoginResult, error)
	Refresh(ctx context.Context, refreshToken string) (*service.LoginResult, error)
	Logout(ctx context.Context, refreshToken string) error
	UserByID(ctx context.Context, id int64) (*model.User, error)
}

// tokenVerifier checks an access token and reports whose it is.
type tokenVerifier interface {
	ParseAccess(token string) (int64, error)
}

// postService is the slice of the Post service the handlers use. Each
// method mirrors a service method so the handler never imports service
// beyond its input/output types, keeping the dependency direction one-way.
type postService interface {
	CreatePost(ctx context.Context, in service.CreatePostInput) (*model.Post, error)
	GetPost(ctx context.Context, slug string) (*model.Post, error)
	ListPosts(ctx context.Context) ([]model.Post, error)
	UpdatePost(ctx context.Context, in service.UpdatePostInput) (*model.Post, error)
	PublishPost(ctx context.Context, slug string) (*model.Post, error)
	UnpublishPost(ctx context.Context, slug string) (*model.Post, error)
	ListPublishedPosts(ctx context.Context) ([]model.Post, error)
	GetPublishedPost(ctx context.Context, slug string) (*model.Post, error)
}

// readyCheck is one dependency probed by the readiness endpoint.
type readyCheck struct {
	name string
	fn   func(context.Context) error
}

// Option customises an API at construction.
type Option func(*API)

// New builds an API. cfg and log are both required.
func New(cfg *config.Config, log *slog.Logger, opts ...Option) *API {
	a := &API{
		cfg: cfg,
		log: log,
		now: func() time.Time { return time.Now().UTC() },
	}

	for _, opt := range opts {
		opt(a)
	}
	return a
}

// WithClock replaces the time source. Intended for tests.
func WithClock(fn func() time.Time) Option {
	return func(a *API) { a.now = fn }
}

// WithUserService supplies the account operations behind POST /api/users.
func WithUserService(users userRegistrar) Option {
	return func(a *API) { a.users = users }
}

// WithAuthService supplies credential verification for the auth routes.
func WithAuthService(auth authenticator) Option {
	return func(a *API) { a.auth = auth }
}

// WithTokenVerifier supplies access token verification for requireAuth.
func WithTokenVerifier(tokens tokenVerifier) Option {
	return func(a *API) { a.tokens = tokens }
}

// WithPostService supplies the Post operations behind the Author routes.
func WithPostService(posts postService) Option {
	return func(a *API) { a.posts = posts }
}

// WithReadyCheck registers a dependency probe for GET /api/ready.
//
// This is the seam the database pool plugs into once it exists:
//
//	handler.New(cfg, log, handler.WithReadyCheck("database", pool.Ping))
//
// Checks run concurrently on every readiness request, so each one should
// be cheap and must respect the context it is given.
func WithReadyCheck(name string, fn func(context.Context) error) Option {
	return func(a *API) {
		a.readyChecks = append(a.readyChecks, readyCheck{name: name, fn: fn})
	}
}
