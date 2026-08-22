package handler

import (
	"context"
	"log/slog"
	"time"

	"go-http-service/internal/config"
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

	readyChecks []readyCheck
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
