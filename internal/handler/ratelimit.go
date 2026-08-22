package handler

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"go-http-service/internal/model"
)

// bucketIdleTTL is how long an unused bucket survives before being swept.
//
// Not a configuration knob: it trades memory for the accuracy of a
// limiter nobody is currently hitting, and there is no deployment where
// a different value would matter.
const bucketIdleTTL = 10 * time.Minute

// ipRateLimiter keeps one token bucket per client IP.
//
// The map needs eviction, and that is not a nicety. Keyed by an
// attacker-controlled value, an unbounded map turns the rate limiter
// itself into a memory-exhaustion vector: rotate source addresses and
// the process grows until it dies. Sweeping idle buckets bounds it to
// the number of addresses actually active in the last bucketIdleTTL.
type ipRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket

	limit rate.Limit
	burst int

	lastSweep time.Time
	now       func() time.Time
}

type bucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// newIPRateLimiter builds a limiter allowing limit requests per second
// with the given burst.
func newIPRateLimiter(limit rate.Limit, burst int) *ipRateLimiter {
	return &ipRateLimiter{
		buckets: make(map[string]*bucket),
		limit:   limit,
		burst:   burst,
		now:     time.Now,
	}
}

// allow reports whether this address may proceed, and consumes a token
// when it may.
func (l *ipRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweepLocked(now)

	b, ok := l.buckets[ip]
	if !ok {
		b = &bucket{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.buckets[ip] = b
	}
	b.lastSeen = now

	// AllowN with an explicit time rather than Allow, so an injected
	// clock drives the buckets too and the tests need no sleeping.
	return b.limiter.AllowN(now, 1)
}

// sweepLocked drops buckets nobody has used recently. Called on the
// request path rather than from a background goroutine: the router has
// no lifecycle to hang one off, and a goroutine would then need its own
// shutdown plumbing for no benefit.
func (l *ipRateLimiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < bucketIdleTTL {
		return
	}
	l.lastSweep = now

	for ip, b := range l.buckets {
		if now.Sub(b.lastSeen) >= bucketIdleTTL {
			delete(l.buckets, ip)
		}
	}
}

// size reports how many buckets are held. Used by tests to prove
// eviction happens.
func (l *ipRateLimiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// retryAfter is how long it takes to accrue one token, rounded up to a
// whole second because that is the granularity of the header.
func (l *ipRateLimiter) retryAfter() time.Duration {
	if l.limit <= 0 {
		return time.Second
	}

	seconds := math.Ceil(1 / float64(l.limit))
	if seconds < 1 {
		seconds = 1
	}
	return time.Duration(seconds) * time.Second
}

// rateLimit rejects requests from an address that has exhausted its
// bucket.
//
// The address comes from c.ClientIP(), whose correctness rests on
// TRUSTED_PROXIES being set honestly. With gin's default of trusting
// every proxy, a caller could forge X-Forwarded-For and get a fresh
// bucket per request, which is no limit at all.
func (a *API) rateLimit(l *ipRateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if l.allow(c.ClientIP()) {
			c.Next()
			return
		}

		retry := l.retryAfter()

		// RFC 6585 pairs 429 with Retry-After so a well-behaved client
		// can back off instead of hammering.
		c.Header("Retry-After", strconv.Itoa(int(retry.Seconds())))

		a.logFor(c).Warn("rate limit exceeded",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"client_ip", c.ClientIP())

		a.respondError(c, http.StatusTooManyRequests, model.ErrCodeRateLimited,
			"too many requests, please slow down")
	}
}
