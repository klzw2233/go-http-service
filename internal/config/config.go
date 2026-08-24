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

	// Generous enough that ordinary use never notices, tight enough to
	// blunt a scripted flood.
	DefaultRateLimitRPS   int64 = 20
	DefaultRateLimitBurst int64 = 40

	// Deliberately severe. A human logs in a handful of times a day; a
	// password-guessing script wants thousands.
	DefaultLoginRateLimitRPM   int64 = 5
	DefaultLoginRateLimitBurst int64 = 5

	// Short on purpose. An access token cannot be revoked once issued,
	// so its lifetime is the window an attacker gets from a stolen one.
	// Continuity comes from the refresh token instead.
	DefaultAccessTokenTTL = 15 * time.Minute

	// Long enough that a human is not bounced back to a password prompt
	// every afternoon, short enough that a stolen refresh token does
	// not last a season. Rotation (and replay detection) is what
	// actually bounds the damage of a leak.
	DefaultRefreshTokenTTL = 720 * time.Hour

	// MinJWTSecretLen mirrors auth.MinSecretLen. It is duplicated rather
	// than imported because config must not depend on other internal
	// packages; a test asserts the two stay equal.
	MinJWTSecretLen = 32

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

	// CORSAllowedOrigins lists the origins allowed to make credentialed
	// cross-origin requests. Empty means deny every cross-origin request,
	// so a browser will block a web app on a different host from calling
	// the API (same-origin calls and non-browser clients are unaffected).
	// Fail closed, like TRUSTED_PROXIES: an open default would let any
	// site read responses. Env: CORS_ALLOWED_ORIGINS.
	CORSAllowedOrigins []string

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

	// RateLimitRPS and RateLimitBurst bound requests from one client
	// address across most endpoints. The probes are exempt; see
	// SetupRouter. Env: RATE_LIMIT_RPS, RATE_LIMIT_BURST.
	RateLimitRPS   int64
	RateLimitBurst int64

	// LoginRateLimitRPM and LoginRateLimitBurst apply the much tighter
	// budget the login endpoint needs. Measured per minute rather than
	// per second because the useful values are single digits: an
	// unlimited password check is an online brute-force target.
	// Env: LOGIN_RATE_LIMIT_RPM, LOGIN_RATE_LIMIT_BURST.
	LoginRateLimitRPM   int64
	LoginRateLimitBurst int64

	// JWTSecret signs access tokens. Env: JWT_SECRET.
	//
	// Required, and at least auth.MinSecretLen bytes: HMAC is only as
	// strong as its key, and a short one can be recovered offline from
	// any captured token, after which an attacker mints tokens for
	// whoever they like.
	//
	// Never log this value; see LogValue and redactSecret.
	JWTSecret string

	// AccessTokenTTL is how long an access token stays valid.
	// Env: ACCESS_TOKEN_TTL.
	AccessTokenTTL time.Duration

	// RefreshTokenTTL is how long a refresh token stays valid.
	// Env: REFRESH_TOKEN_TTL.
	RefreshTokenTTL time.Duration

	// AuthorUsername is the User who may write Posts. Env: AUTHOR_USERNAME.
	//
	// Required. Compared case-insensitively with users.username, matching
	// the unique index on lower(username). It is a name, not a secret.
	AuthorUsername string

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
		Port:               envString("PORT", DefaultPort),
		TrustedProxies:     envList("TRUSTED_PROXIES"),
		CORSAllowedOrigins: envList("CORS_ALLOWED_ORIGINS"),
		LogFormat:          strings.ToLower(envString("LOG_FORMAT", DefaultLogFormat)),
		DatabaseURL:        envString("DATABASE_URL", ""),
		JWTSecret:          envString("JWT_SECRET", ""),
		AuthorUsername:     envString("AUTHOR_USERNAME", ""),
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

	cfg.RateLimitRPS, err = envInt64("RATE_LIMIT_RPS", DefaultRateLimitRPS)
	track(err)
	cfg.RateLimitBurst, err = envInt64("RATE_LIMIT_BURST", DefaultRateLimitBurst)
	track(err)
	cfg.LoginRateLimitRPM, err = envInt64("LOGIN_RATE_LIMIT_RPM", DefaultLoginRateLimitRPM)
	track(err)
	cfg.LoginRateLimitBurst, err = envInt64("LOGIN_RATE_LIMIT_BURST", DefaultLoginRateLimitBurst)
	track(err)

	cfg.AccessTokenTTL, err = envDuration("ACCESS_TOKEN_TTL", DefaultAccessTokenTTL)
	track(err)
	cfg.RefreshTokenTTL, err = envDuration("REFRESH_TOKEN_TTL", DefaultRefreshTokenTTL)
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

	// Origins are validated rather than trusted verbatim: a typo'd
	// CORS_ALLOWED_ORIGINS entry would otherwise silently fail to match,
	// and the operator would debug a browser showing "blocked by CORS"
	// with no hint why.
	for _, o := range c.CORSAllowedOrigins {
		if err := validateOrigin(o); err != nil {
			errs = append(errs, fmt.Errorf("CORS_ALLOWED_ORIGINS: %w", err))
		}
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

	// A zero or negative budget would lock everyone out rather than
	// merely slowing abusers down, which is never what was meant.
	for _, limit := range []struct {
		name  string
		value int64
	}{
		{"RATE_LIMIT_RPS", c.RateLimitRPS},
		{"RATE_LIMIT_BURST", c.RateLimitBurst},
		{"LOGIN_RATE_LIMIT_RPM", c.LoginRateLimitRPM},
		{"LOGIN_RATE_LIMIT_BURST", c.LoginRateLimitBurst},
	} {
		if limit.value <= 0 {
			errs = append(errs, fmt.Errorf("%s must be positive, got %d", limit.name, limit.value))
		}
	}

	switch {
	case c.JWTSecret == "":
		errs = append(errs, errors.New(
			"JWT_SECRET is required; generate one with: openssl rand -base64 48"))
	case len(c.JWTSecret) < MinJWTSecretLen:
		// Reporting the length is safe and useful; reporting the value
		// would put the signing key in the error and then in the log.
		errs = append(errs, fmt.Errorf(
			"JWT_SECRET must be at least %d bytes, got %d; a short HMAC key can be "+
				"brute-forced offline from any captured token",
			MinJWTSecretLen, len(c.JWTSecret)))
	}

	switch {
	case c.AuthorUsername == "":
		errs = append(errs, errors.New(
			"AUTHOR_USERNAME is required; it names the User who may write Posts"))
	case !validAuthorUsername(c.AuthorUsername):
		errs = append(errs, fmt.Errorf(
			"AUTHOR_USERNAME must be 3-32 alphanumeric characters, got %q",
			c.AuthorUsername))
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
		slog.Any("cors_allowed_origins", c.CORSAllowedOrigins),
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
		slog.Int64("rate_limit_rps", c.RateLimitRPS),
		slog.Int64("rate_limit_burst", c.RateLimitBurst),
		slog.Int64("login_rate_limit_rpm", c.LoginRateLimitRPM),
		slog.Int64("login_rate_limit_burst", c.LoginRateLimitBurst),
		slog.String("jwt_secret", redactSecret(c.JWTSecret)),
		slog.String("access_token_ttl", c.AccessTokenTTL.String()),
		slog.String("refresh_token_ttl", c.RefreshTokenTTL.String()),
		slog.String("author_username", c.AuthorUsername),
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

// redactSecret reports only whether a secret is configured.
//
// Unlike a connection string, a signing key has no non-sensitive part
// worth keeping: the host and database name in a DSN make the startup
// log useful, whereas any portion of an HMAC key only helps an attacker.
// Even the length is withheld, since it narrows a brute-force search.
func redactSecret(secret string) string {
	if secret == "" {
		return dsnUnset
	}
	return dsnOpaque
}

// validAuthorUsername is the same shape as a users.username: 3-32
// ASCII letters or digits. Duplicated here so config does not import model.
func validAuthorUsername(s string) bool {
	if n := len(s); n < 3 || n > 32 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			continue
		}
		if c >= 'A' && c <= 'Z' {
			continue
		}
		if c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
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

// validateOrigin checks one CORS_ALLOWED_ORIGINS entry.
//
// The wildcard "*" is accepted as the one explicit "open" option — it is
// still opt-in, never the default. Concrete origins must be an absolute URL
// with an http(s) scheme and a host, because a bare hostname or a missing
// scheme would never match the Origin the browser sends, and a silent
// non-match is the worst kind of CORS failure to debug.
func validateOrigin(o string) error {
	if o == "*" {
		return nil
	}
	u, err := url.Parse(o)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%q is not a valid origin; use a full URL such as https://app.example.com or *", o)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%q must use an http or https scheme", o)
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
