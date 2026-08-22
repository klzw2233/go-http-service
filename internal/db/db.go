// Package db owns the PostgreSQL connection pool.
//
// It deliberately exposes nothing but a pool and a health probe. Query
// code belongs in the repository layer, which will be built on top of
// this once there is a table to query.
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"go-http-service/internal/config"
)

// ErrInvalidDSN means DATABASE_URL could not be parsed.
//
// The underlying parse error is deliberately dropped rather than
// wrapped: pgx includes the connection string it failed on, and that
// string carries the password. This error is returned to main, which
// logs it, so wrapping would write the credential to disk on every
// failed start. The variable name of the setting is enough to act on.
var ErrInvalidDSN = errors.New("DATABASE_URL is not a valid connection string")

// Connect builds the pool and proves it can reach the database.
//
// pgxpool.NewWithConfig is lazy: it validates the settings but opens no
// connection, so on its own it cannot tell you whether the database is
// reachable. The explicit Ping is what makes a failure surface at
// startup instead of on the first request.
func Connect(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, ErrInvalidDSN
	}

	poolCfg.MaxConns = int32(cfg.DBMaxConns)
	poolCfg.ConnConfig.ConnectTimeout = cfg.DBConnectTimeout

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, cfg.DBConnectTimeout)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}

// HealthCheck returns a probe suitable for handler.WithReadyCheck.
//
// The timeout is applied on top of whatever deadline the request already
// carries. context.WithTimeout keeps the earlier of the two, so the probe
// stays bounded by REQUEST_TIMEOUT while a wedged database cannot spend
// the whole request budget either.
func HealthCheck(pool *pgxpool.Pool, timeout time.Duration) func(context.Context) error {
	return func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		return pool.Ping(ctx)
	}
}
