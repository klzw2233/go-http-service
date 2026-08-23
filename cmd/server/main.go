package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"

	"go-http-service/internal/auth"
	"go-http-service/internal/config"
	"go-http-service/internal/db"
	"go-http-service/internal/handler"
	"go-http-service/internal/repository"
	"go-http-service/internal/service"
)

func main() {
	if err := run(); err != nil {
		// Config may have failed before a logger existed, so this goes
		// through slog's default handler (text, stderr).
		slog.Error("server stopped with an error", "error", err)
		os.Exit(1)
	}
}

// run wires the dependencies, serves, and blocks until told to stop or a
// failure occurs. It returns nil only after a clean shutdown.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	logger := newLogger(cfg, os.Stdout)
	slog.SetDefault(logger)

	// gin's debug mode prints its route table and warnings straight to
	// stdout. Those lines are not JSON, so they interleave with the
	// structured log stream and break any collector parsing it line by
	// line. Release is the right default for a service; an explicit
	// GIN_MODE still wins, and the handler tests set their own mode.
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Cancels ctx on SIGINT or SIGTERM. SIGTERM is what Docker and
	// Kubernetes send first when stopping a container.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The database is required: every dependency below hangs off the
	// pool, and the service has endpoints that cannot answer without it.
	pool, err := db.Connect(ctx, cfg)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}

	// Registered HERE, above the server, and that placement is the whole
	// point. defer runs last-in-first-out while srv.Shutdown below is an
	// ordinary call, so the real order is: drain in-flight HTTP
	// requests, return from run, then close the pool. Closing the pool
	// first would cut off queries that requests are still waiting on.
	defer func() {
		logger.Info("closing database pool")
		pool.Close()
	}()

	// Migrations run before the server accepts traffic, so no request
	// can arrive against a schema that is mid-upgrade. Concurrent
	// replicas are serialised by an advisory lock inside Migrate.
	if err := db.Migrate(ctx, pool, logger); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	logger.Info("database pool ready", "max_conns", cfg.DBMaxConns)

	// Dependencies are wired outward: repository -> service -> API.
	userRepo := repository.NewUserRepository(pool)
	users := service.NewUserService(userRepo)

	tokens, err := auth.NewTokenIssuer(cfg.JWTSecret, cfg.AccessTokenTTL)
	if err != nil {
		return fmt.Errorf("token issuer: %w", err)
	}

	authSvc, err := service.NewAuthService(userRepo, tokens)
	if err != nil {
		return fmt.Errorf("auth service: %w", err)
	}

	api := handler.New(cfg, logger,
		handler.WithUserService(users),
		handler.WithAuthService(authSvc),
		handler.WithTokenVerifier(tokens),
		handler.WithReadyCheck("database", db.HealthCheck(pool, cfg.DBConnectTimeout)),
	)

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           handler.SetupRouter(api),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}

	// Buffered so the goroutine never blocks if nobody is receiving.
	errCh := make(chan error, 1)
	go func() {
		logger.Info("server listening", "addr", srv.Addr, "config", cfg)
		// Shutdown makes ListenAndServe return ErrServerClosed; that is
		// the expected path, not a failure.
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		// Startup failed, typically because the port is taken.
		return fmt.Errorf("listen on %s: %w", srv.Addr, err)
	case <-ctx.Done():
		// Restore default signal handling so a second Ctrl-C during a slow
		// drain kills the process immediately instead of hanging.
		stop()
	}

	logger.Info("shutdown signal received, draining", "timeout", cfg.ShutdownTimeout)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	logger.Info("server stopped cleanly")
	return nil
}

// newLogger builds the structured logger described by cfg. The writer is
// a parameter so tests can capture the output.
func newLogger(cfg *config.Config, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}

	var h slog.Handler
	if cfg.LogFormat == config.FormatText {
		h = slog.NewTextHandler(w, opts)
	} else {
		h = slog.NewJSONHandler(w, opts)
	}
	return slog.New(h)
}
