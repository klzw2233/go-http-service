// Package config centralises every environment-driven setting.
//
// These settings used to be scattered: main.go read PORT, router.go read
// TRUSTED_PROXIES, the five HTTP timeouts were consts in main.go, and the
// body limit was a const in middleware.go. Adding a database would have
// continued that pattern with DATABASE_URL and pool settings.
//
// Load reads and validates everything once at startup, so a bad value
// fails the process immediately with a clear message rather than showing
// up later as odd behaviour.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Supported values for LOG_FORMAT.
const (
	FormatJSON = "json"
	FormatText = "text"
)

// Defaults applied when the corresponding variable is unset or empty.
const (
	DefaultPort = "8080"

	DefaultReadHeaderTimeout = 5 * time.Second
	DefaultReadTimeout       = 10 * time.Second
	DefaultWriteTimeout      = 10 * time.Second
	DefaultIdleTimeout       = 60 * time.Second
	DefaultShutdownTimeout   = 15 * time.Second
	DefaultRequestTimeout    = 8 * time.Second

	DefaultMaxBodyBytes int64 = 1 << 20 // 1 MiB

	// DefaultDBMaxConns fixes the pool size. pgx defaults to the greater
	// of 4 and the CPU count, which silently changes the load a database
	// sees when the service moves to a different machine.
	DefaultDBMaxConns int64 = 10

	// DefaultDBConnectTimeout bounds establishing a connection, and is
	// reused as the readiness probe's timeout.
	DefaultDBConnectTimeout = 5 * time.Second

	DefaultLogLevel  = slog.LevelInfo
	DefaultLogFormat = FormatJSON
)

// Config holds every setting the service reads from the environment.
type Config struct {
	// Port is the TCP port to listen on. Env: PORT.
	Port string

	// TrustedProxies lists proxy IPs or CIDRs whose forwarding headers may
	// be believed. Empty means trust nobody, so ClientIP is the direct peer
	// address and a caller cannot forge its own IP. Env: TRUSTED_PROXIES.
	TrustedProxies []string

	// The four http.Server timeouts. Env: READ_HEADER_TIMEOUT,
	// READ_TIMEOUT, WRITE_TIMEOUT, IDLE_TIMEOUT.
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration

	// ShutdownTimeout bounds how long in-flight requests get to finish
	// after a shutdown signal. Env: SHUTDOWN_TIMEOUT.
	ShutdownTimeout time.Duration

	// RequestTimeout bounds one request's handler work. The http.Server
	// timeouts cannot do this: they govern reading the request and writing
	// the response, not how long a handler spends inside a slow query.
	// Env: REQUEST_TIMEOUT.
	RequestTimeout time.Duration

	// MaxBodyBytes caps the request body. Env: MAX_BODY_BYTES.
	MaxBodyBytes int64

	// DatabaseURL is the PostgreSQL connection string. Env: DATABASE_URL.
	//
	// Required: the service exposes endpoints that read and write the
	// database, so starting without one would only produce 500s. It was
	// optional while no endpoint needed persistence.
	//
	// Never log this value directly; see LogValue and redactDSN.
	DatabaseURL string

	// DBMaxConns is the connection pool ceiling. Env: DB_MAX_CONNS.
	DBMaxConns int64

	// DBConnectTimeout bounds establishing a connection, and doubles as
	// the readiness probe timeout. Env: DB_CONNECT_TIMEOUT.
	DBConnectTimeout time.Duration

	// LogLevel is the minimum level to emit. Env: LOG_LEVEL.
	LogLevel slog.Level

	// LogFormat selects the slog handler. Env: LOG_FORMAT.
	LogFormat string
}

// Load reads the configuration from the environment.
//
// Every problem found is reported, not just the first, so a misconfigured
// deployment can be fixed in one pass instead of one restart per typo.
func Load() (*Config, error) {
	var errs []error

	// track collects a parse error and yields the default so later
	// cross-field checks still run against usable values.
	track := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}

	cfg := &Config{
		Port:           envString("PORT", DefaultPort),
		TrustedProxies: envList("TRUSTED_PROXIES"),
		LogFormat:      strings.ToLower(envString("LOG_FORMAT", DefaultLogFormat)),
		DatabaseURL:    envString("DATABASE_URL", ""),
	}

	var err error

	cfg.ReadHeaderTimeout, err = envDuration("READ_HEADER_TIMEOUT", DefaultReadHeaderTimeout)
	track(err)
	cfg.ReadTimeout, err = envDuration("READ_TIMEOUT", DefaultReadTimeout)
	track(err)
	cfg.WriteTimeout, err = envDuration("WRITE_TIMEOUT", DefaultWriteTimeout)
	track(err)
	cfg.IdleTimeout, err = envDuration("IDLE_TIMEOUT", DefaultIdleTimeout)
	track(err)
	cfg.ShutdownTimeout, err = envDuration("SHUTDOWN_TIMEOUT", DefaultShutdownTimeout)
	track(err)
	cfg.RequestTimeout, err = envDuration("REQUEST_TIMEOUT", DefaultRequestTimeout)
	track(err)

	cfg.MaxBodyBytes, err = envInt64("MAX_BODY_BYTES", DefaultMaxBodyBytes)
	track(err)

	cfg.DBMaxConns, err = envInt64("DB_MAX_CONNS", DefaultDBMaxConns)
	track(err)
	cfg.DBConnectTimeout, err = envDuration("DB_CONNECT_TIMEOUT", DefaultDBConnectTimeout)
	track(err)

	cfg.LogLevel, err = envLogLevel("LOG_LEVEL", DefaultLogLevel)
	track(err)

	errs = append(errs, cfg.validate()...)

	if joined := errors.Join(errs...); joined != nil {
		return nil, joined
	}
	return cfg, nil
}

// validate checks values that parsing alone cannot rule out.
func (c *Config) validate() []error {
	var errs []error

	if err := validatePort(c.Port); err != nil {
		errs = append(errs, err)
	}

	for _, p := range c.TrustedProxies {
		if net.ParseIP(p) != nil {
			continue
		}
		if _, _, err := net.ParseCIDR(p); err == nil {
			continue
		}
		errs = append(errs, fmt.Errorf("TRUSTED_PROXIES: %q is neither an IP nor a CIDR", p))
	}

	if c.MaxBodyBytes <= 0 {
		errs = append(errs, fmt.Errorf("MAX_BODY_BYTES must be positive, got %d", c.MaxBodyBytes))
	}

	if c.LogFormat != FormatJSON && c.LogFormat != FormatText {
		errs = append(errs, fmt.Errorf("LOG_FORMAT must be %q or %q, got %q",
			FormatJSON, FormatText, c.LogFormat))
	}

	// A handler deadline longer than the write deadline can never fire
	// usefully: the server gives up on the response first.
	if c.RequestTimeout >= c.WriteTimeout {
		errs = append(errs, fmt.Errorf(
			"REQUEST_TIMEOUT (%s) must be shorter than WRITE_TIMEOUT (%s), "+
				"otherwise the write deadline always wins and the handler deadline never fires",
			c.RequestTimeout, c.WriteTimeout))
	}

	// Pool settings only matter when a database is configured, so an
	// unused DB_MAX_CONNS=0 is not worth failing a deployment over.
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New(
			"DATABASE_URL is required; the service cannot serve its endpoints without a database"))
	} else if c.DBMaxConns <= 0 {
		errs = append(errs, fmt.Errorf("DB_MAX_CONNS must be positive, got %d", c.DBMaxConns))
	}

	return errs
}

// Addr returns the listen address for http.Server.
func (c *Config) Addr() string { return ":" + c.Port }

// LogValue renders the config for structured logging.
//
// It exists so startup can log the effective settings. Durations are
// rendered as strings ("5s") rather than with slog.Duration, which a JSON
// handler emits as a nanosecond integer. This value is a config dump read
// by a human deciding whether the deployment picked up the right
// settings, and 5000000000 does not answer that question at a glance.
//
// DatabaseURL goes through redactDSN: it carries a password, and this
// record is written on every start and kept by whatever collects the
// logs.
func (c *Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("port", c.Port),
		slog.Any("trusted_proxies", c.TrustedProxies),
		slog.String("read_header_timeout", c.ReadHeaderTimeout.String()),
		slog.String("read_timeout", c.ReadTimeout.String()),
		slog.String("write_timeout", c.WriteTimeout.String()),
		slog.String("idle_timeout", c.IdleTimeout.String()),
		slog.String("shutdown_timeout", c.ShutdownTimeout.String()),
		slog.String("request_timeout", c.RequestTimeout.String()),
		slog.Int64("max_body_bytes", c.MaxBodyBytes),
		slog.String("database_url", redactDSN(c.DatabaseURL)),
		slog.Int64("db_max_conns", c.DBMaxConns),
		slog.String("db_connect_timeout", c.DBConnectTimeout.String()),
		slog.String("log_level", c.LogLevel.String()),
		slog.String("log_format", c.LogFormat),
	)
}

// Placeholders used in place of a connection string.
const (
	dsnUnset = "(unset)"
	// dsnOpaque is used when the DSN cannot be parsed as a URL, which is
	// the case for the keyword/value form pgx also accepts
	// ("host=... password=..."). Reporting that it is set is the most we
	// can say without risking the credential.
	dsnOpaque = "(set)"
)

// redactDSN renders a connection string safe to log.
//
// url.Redacted replaces the password with "xxxxx" while keeping the host,
// port and database name, which is what makes the log useful for
// confirming a deployment connected where it was meant to.
//
// Anything that does not parse as a URL falls back to a fixed
// placeholder. It never falls back to returning the input: a DSN that
// failed to parse is exactly the case where a stray credential would be
// printed.
func redactDSN(dsn string) string {
	if dsn == "" {
		return dsnUnset
	}

	u, err := url.Parse(dsn)
	if err != nil || u.Scheme == "" {
		return dsnOpaque
	}
	return u.Redacted()
}

func validatePort(p string) error {
	n, err := strconv.Atoi(p)
	if err != nil {
		return fmt.Errorf("PORT must be a number, got %q", p)
	}
	if n < 1 || n > 65535 {
		return fmt.Errorf("PORT must be between 1 and 65535, got %d", n)
	}
	return nil
}

// envString returns the value of key, or def when unset or empty.
// Empty is treated as unset so `PORT=` in a compose file is not a
// silent misconfiguration.
func envString(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// envList splits key on commas, dropping blanks. Returns nil when unset.
func envList(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// envDuration parses key as a Go duration such as "5s" or "1m30s".
func envDuration(key string, def time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def, nil
	}

	d, err := time.ParseDuration(raw)
	if err != nil {
		return def, fmt.Errorf("%s must be a duration such as \"5s\", got %q", key, raw)
	}
	if d <= 0 {
		return def, fmt.Errorf("%s must be positive, got %s", key, d)
	}
	return d, nil
}

// envInt64 parses key as a base-10 integer.
func envInt64(key string, def int64) (int64, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def, nil
	}

	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return def, fmt.Errorf("%s must be an integer, got %q", key, raw)
	}
	return n, nil
}

// envLogLevel parses key as debug, info, warn or error.
func envLogLevel(key string, def slog.Level) (slog.Level, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def, nil
	}

	switch strings.ToLower(raw) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return def, fmt.Errorf("%s must be debug, info, warn or error, got %q", key, raw)
	}
}
