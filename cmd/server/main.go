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

	"go-http-service/internal/config"
	"go-http-service/internal/handler"
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

	// Dependencies are built here and released in reverse. The database
	// pool slots in at this point:
	//
	//	pool, err := db.Connect(ctx, cfg)
	//	if err != nil {
	//		return fmt.Errorf("database: %w", err)
	//	}
	//	defer pool.Close()
	//
	// Registering that defer HERE, above the server, is what gets the
	// teardown order right. Shutdown below is a plain call, so it finishes
	// draining in-flight requests before run returns and the deferred
	// pool.Close finally executes. Closing the pool first would cut off
	// queries that requests are still waiting on.

	api := handler.New(cfg, logger)
	// Once the pool exists, register its probe so /api/ready reports it:
	//	handler.New(cfg, logger, handler.WithReadyCheck("database", pool.Ping))

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           handler.SetupRouter(api),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}

	// Cancels ctx on SIGINT or SIGTERM. SIGTERM is what Docker and
	// Kubernetes send first when stopping a container.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
