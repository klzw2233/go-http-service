package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-http-service/internal/handler"
)

const (
	// defaultPort is used when the PORT environment variable is unset.
	defaultPort = "8080"

	// readHeaderTimeout bounds how long a client may take to send its
	// request headers. Without it a slow trickle of headers pins a
	// goroutine and a file descriptor forever (Slowloris). gin's Run()
	// leaves every timeout at zero, which is why we build the server here.
	readHeaderTimeout = 5 * time.Second

	// readTimeout bounds reading the whole request, headers plus body.
	readTimeout = 10 * time.Second

	// writeTimeout bounds writing the response back to the client.
	writeTimeout = 10 * time.Second

	// idleTimeout bounds how long a keep-alive connection may sit unused.
	idleTimeout = 60 * time.Second

	// shutdownTimeout bounds how long we wait for in-flight requests to
	// finish after a shutdown signal before giving up on them.
	shutdownTimeout = 15 * time.Second
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("server: %v", err)
	}
}

// run starts the HTTP server and blocks until it is asked to stop or
// fails. It returns nil only after a clean shutdown.
func run() error {
	srv := &http.Server{
		Addr:              ":" + port(),
		Handler:           handler.SetupRouter(),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	// Cancels ctx on SIGINT or SIGTERM. SIGTERM is what Docker and
	// Kubernetes send first when stopping a container.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Buffered so the goroutine never blocks if nobody is receiving.
	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", srv.Addr)
		// Shutdown makes ListenAndServe return ErrServerClosed; that is
		// the expected path, not a failure.
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		// Startup failed, typically because the port is taken. Previously
		// this error was discarded and the process exited with status 0.
		return fmt.Errorf("listen on %s: %w", srv.Addr, err)
	case <-ctx.Done():
		// Restore default signal handling so a second Ctrl-C during a slow
		// drain kills the process immediately instead of hanging.
		stop()
	}

	log.Printf("shutdown signal received, draining for up to %s", shutdownTimeout)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	log.Println("server stopped cleanly")
	return nil
}

// port returns the listen port, preferring the PORT environment variable
// so the container runtime can assign it without a rebuild.
func port() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	return defaultPort
}
