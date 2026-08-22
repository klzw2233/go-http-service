package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"go-http-service/internal/model"
)

// fakeClock drives the limiter without sleeping, so the eviction and
// refill tests finish instantly and never flake on a loaded machine.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestIPRateLimiter_AllowsBurstThenRefuses(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	l := newIPRateLimiter(rate.Limit(1), 3)
	l.now = clock.Now

	for i := range 3 {
		assert.True(t, l.allow("1.2.3.4"), "突发额度内的第 %d 个请求应放行", i+1)
	}
	assert.False(t, l.allow("1.2.3.4"), "超过突发额度后应拒绝")
}

func TestIPRateLimiter_RefillsOverTime(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	l := newIPRateLimiter(rate.Limit(1), 1) // 每秒 1 个
	l.now = clock.Now

	require.True(t, l.allow("1.2.3.4"))
	require.False(t, l.allow("1.2.3.4"))

	clock.advance(time.Second)

	assert.True(t, l.allow("1.2.3.4"), "一秒后应补充一个令牌")
}

// TestIPRateLimiter_IsolatesAddresses is the point of keying by IP: one
// abusive client must not lock everyone else out.
func TestIPRateLimiter_IsolatesAddresses(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	l := newIPRateLimiter(rate.Limit(1), 1)
	l.now = clock.Now

	require.True(t, l.allow("1.1.1.1"))
	require.False(t, l.allow("1.1.1.1"), "第一个地址已用尽")

	assert.True(t, l.allow("2.2.2.2"), "另一个地址应有自己的额度")
}

// TestIPRateLimiter_EvictsIdleBuckets covers the reason eviction exists.
// The map is keyed by a value the caller controls, so without sweeping,
// rotating source addresses grows it until the process dies - the rate
// limiter becomes the denial of service.
func TestIPRateLimiter_EvictsIdleBuckets(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	l := newIPRateLimiter(rate.Limit(100), 100)
	l.now = clock.Now

	for i := range 500 {
		l.allow(fmt.Sprintf("10.0.%d.%d", i/256, i%256))
	}
	require.Equal(t, 500, l.size(), "每个地址应各有一个桶")

	// Nobody comes back, and one straggler arrives after the TTL.
	clock.advance(bucketIdleTTL + time.Second)
	l.allow("192.0.2.1")

	assert.Equal(t, 1, l.size(),
		"闲置的桶应被清扫，只剩刚活动过的那个，实际剩 %d 个", l.size())
}

func TestIPRateLimiter_KeepsActiveBuckets(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	l := newIPRateLimiter(rate.Limit(100), 100)
	l.now = clock.Now

	l.allow("1.1.1.1")
	l.allow("2.2.2.2")

	// 1.1.1.1 keeps coming back; 2.2.2.2 goes quiet.
	for range 5 {
		clock.advance(bucketIdleTTL / 2)
		l.allow("1.1.1.1")
	}

	assert.Equal(t, 1, l.size(), "活跃的桶不应被清掉，闲置的应被清掉")
	assert.True(t, l.allow("1.1.1.1"))
}

func TestIPRateLimiter_RetryAfter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		limit rate.Limit
		want  time.Duration
	}{
		{"每秒 20 个", rate.Limit(20), time.Second}, // 不足一秒，向上取整
		{"每秒 1 个", rate.Limit(1), time.Second},
		{"每分钟 5 个", rate.Limit(5) / 60, 12 * time.Second},
		{"零值兜底", rate.Limit(0), time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := newIPRateLimiter(tt.limit, 1).retryAfter()

			assert.Equal(t, tt.want, got)
			assert.GreaterOrEqual(t, got, time.Second, "Retry-After 至少一秒")
		})
	}
}

// TestRateLimit_Returns429WithRetryAfter covers the middleware's HTTP
// contract, including the header RFC 6585 asks for.
func TestRateLimit_Returns429WithRetryAfter(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.RateLimitRPS = 1
	cfg.RateLimitBurst = 1

	r := SetupRouter(newTestAPIWith(cfg, discardLogger()))

	// The first request consumes the only token.
	first := request{method: http.MethodGet, path: "/api/info", remoteAddr: "203.0.113.1:1"}.doOn(t, r)
	require.Equal(t, http.StatusOK, first.Code)

	second := request{method: http.MethodGet, path: "/api/info", remoteAddr: "203.0.113.1:1"}.doOn(t, r)

	var got model.ErrorResponse
	requireJSONResponse(t, second, http.StatusTooManyRequests, &got)

	assert.Equal(t, model.ErrCodeRateLimited, got.Code)

	retry := second.Header().Get("Retry-After")
	require.NotEmpty(t, retry, "429 必须带 Retry-After，否则客户端只能盲目重试")

	seconds, err := strconv.Atoi(retry)
	require.NoError(t, err, "Retry-After 必须是整秒数")
	assert.GreaterOrEqual(t, seconds, 1)
}

// TestRateLimit_ProbesAreExempt is the one that would have caused an
// outage. kubelet polls the probes constantly from a single address; a
// shared budget would throttle them and the orchestrator would restart
// containers that were perfectly healthy.
func TestRateLimit_ProbesAreExempt(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.RateLimitRPS = 1
	cfg.RateLimitBurst = 1

	r := SetupRouter(newTestAPIWith(cfg, discardLogger()))

	for _, path := range []string{"/api/health", "/api/ready"} {
		t.Run(path, func(t *testing.T) {
			for i := range 100 {
				w := request{method: http.MethodGet, path: path, remoteAddr: "203.0.113.9:1"}.doOn(t, r)
				require.Equal(t, http.StatusOK, w.Code,
					"探针不该被限流，第 %d 次请求返回了 %d", i+1, w.Code)
			}
		})
	}
}

func TestRateLimit_DoesNotAffectOtherClients(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.RateLimitRPS = 1
	cfg.RateLimitBurst = 1

	r := SetupRouter(newTestAPIWith(cfg, discardLogger()))

	require.Equal(t, http.StatusOK,
		request{method: http.MethodGet, path: "/api/info", remoteAddr: "198.51.100.1:1"}.doOn(t, r).Code)
	require.Equal(t, http.StatusTooManyRequests,
		request{method: http.MethodGet, path: "/api/info", remoteAddr: "198.51.100.1:1"}.doOn(t, r).Code)

	assert.Equal(t, http.StatusOK,
		request{method: http.MethodGet, path: "/api/info", remoteAddr: "198.51.100.2:1"}.doOn(t, r).Code,
		"一个客户端超限不应影响其他客户端")
}

// TestRateLimit_UsesTrustedProxyClientIP ties the limiter to the
// TRUSTED_PROXIES work: if a caller could forge X-Forwarded-For, it
// would get a fresh bucket per request and the limit would be no limit.
func TestRateLimit_UsesTrustedProxyClientIP(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.RateLimitRPS = 1
	cfg.RateLimitBurst = 1
	// No trusted proxies, so forwarding headers must be ignored.

	r := SetupRouter(newTestAPIWith(cfg, discardLogger()))

	send := func(forwarded string) int {
		return request{
			method:     http.MethodGet,
			path:       "/api/info",
			remoteAddr: "203.0.113.50:1",
			headers:    map[string]string{"X-Forwarded-For": forwarded},
		}.doOn(t, r).Code
	}

	require.Equal(t, http.StatusOK, send("1.1.1.1"))

	assert.Equal(t, http.StatusTooManyRequests, send("2.2.2.2"),
		"伪造的 X-Forwarded-For 不该换来一个新桶")
}
