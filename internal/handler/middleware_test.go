package handler

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hexID matches the 16-byte hex ID that newRequestID produces.
var hexID = regexp.MustCompile(`^[0-9a-f]{32}$`)

func TestRequestID_GeneratedWhenAbsent(t *testing.T) {
	t.Parallel()

	w := request{method: http.MethodGet, path: "/api/health"}.do(t)

	got := w.Header().Get(requestIDHeader)
	require.NotEmpty(t, got, "响应必须带上 %s", requestIDHeader)
	assert.Regexp(t, hexID, got)
}

func TestRequestID_UniquePerRequest(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool, 50)
	for range 50 {
		w := request{method: http.MethodGet, path: "/api/health"}.do(t)
		id := w.Header().Get(requestIDHeader)
		assert.False(t, seen[id], "ID %q 重复了", id)
		seen[id] = true
	}
}

func TestRequestID_AcceptsAndRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sent     string
		wantEcho bool
	}{
		{name: "合法的短 ID", sent: "abc-123", wantEcho: true},
		{name: "合法的下划线", sent: "trace_id_42", wantEcho: true},
		{name: "恰好 64 字符", sent: strings.Repeat("a", maxRequestIDLen), wantEcho: true},

		{name: "超过 64 字符", sent: strings.Repeat("a", maxRequestIDLen+1)},
		{name: "含空格", sent: "abc 123"},
		{name: "含换行（日志注入）", sent: "abc\ninjected"},
		{name: "含回车", sent: "abc\r\nfake"},
		{name: "含引号与花括号（伪造 JSON）", sent: `a","evil":"1`},
		{name: "含分号", sent: "abc;rm -rf"},
		{name: "含点号", sent: "abc.123"},
		{name: "非 ASCII", sent: "请求编号"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := request{
				method:  http.MethodGet,
				path:    "/api/health",
				headers: map[string]string{requestIDHeader: tt.sent},
			}.do(t)

			got := w.Header().Get(requestIDHeader)

			if tt.wantEcho {
				assert.Equal(t, tt.sent, got, "合法 ID 应原样透传以支持链路追踪")
				return
			}

			assert.NotEqual(t, tt.sent, got, "不合法的 ID 必须被替换")
			assert.Regexp(t, hexID, got, "替换后应是新生成的 ID")
		})
	}
}

// TestRequestID_ReachesRequestContext is the property that matters for
// PostgreSQL: a query is handed c.Request.Context(), so the ID must be
// retrievable from there without threading it through signatures.
func TestRequestID_ReachesRequestContext(t *testing.T) {
	t.Parallel()

	const sent = "ctx-propagation-check"

	var fromCtx string
	r := SetupRouter(newTestAPI())
	r.GET("/api/ctx", func(c *gin.Context) {
		fromCtx = RequestIDFrom(c.Request.Context())
		c.Status(http.StatusOK)
	})

	w := request{
		method:  http.MethodGet,
		path:    "/api/ctx",
		headers: map[string]string{requestIDHeader: sent},
	}.doOn(t, r)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, sent, fromCtx)
}

func TestRequestIDFrom_EmptyContext(t *testing.T) {
	t.Parallel()

	assert.Empty(t, RequestIDFrom(t.Context()))
}

// TestTimeout_HandlerSeesDeadline covers the whole point of the timeout
// middleware: the deadline a database query will inherit.
func TestTimeout_HandlerSeesDeadline(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.RequestTimeout = 250 * time.Millisecond

	type result struct {
		ok        bool
		remaining time.Duration
	}
	got := make(chan result, 1)

	api := newTestAPIWith(cfg, discardLogger())
	r := SetupRouter(api)
	r.GET("/api/deadline", func(c *gin.Context) {
		deadline, ok := c.Request.Context().Deadline()
		got <- result{ok: ok, remaining: time.Until(deadline)}
		c.Status(http.StatusOK)
	})

	w := request{method: http.MethodGet, path: "/api/deadline"}.doOn(t, r)
	require.Equal(t, http.StatusOK, w.Code)

	res := <-got
	require.True(t, res.ok, "handler 的 context 必须带 deadline")
	assert.Positive(t, res.remaining)
	assert.LessOrEqual(t, res.remaining, cfg.RequestTimeout)
	assert.Greater(t, res.remaining, cfg.RequestTimeout/2,
		"剩余时间 %s 明显小于配置的 %s，deadline 可能来自别处", res.remaining, cfg.RequestTimeout)
}

// TestTimeout_ExpiredContextCancelsWork proves a handler that respects
// its context actually gets unblocked, which is how a slow query will be
// cut short once one exists.
func TestTimeout_ExpiredContextCancelsWork(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.RequestTimeout = 80 * time.Millisecond

	api := newTestAPIWith(cfg, discardLogger())
	r := SetupRouter(api)
	r.GET("/api/slow", func(c *gin.Context) {
		select {
		case <-c.Request.Context().Done():
			// Mirrors what a handler will do with a cancelled query.
			c.Status(http.StatusServiceUnavailable)
		case <-time.After(5 * time.Second):
			c.Status(http.StatusOK)
		}
	})

	start := time.Now()
	w := request{method: http.MethodGet, path: "/api/slow"}.doOn(t, r)
	elapsed := time.Since(start)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Less(t, elapsed, time.Second, "应在超时后立即返回，实际耗时 %s", elapsed)
}

func TestRequestLogger_EmitsOneRecordPerRequest(t *testing.T) {
	t.Parallel()

	log, buf := captureLogs()
	api := newTestAPIWith(testConfig(), log)

	w := request{method: http.MethodGet, path: "/api/health"}.doOn(t, SetupRouter(api))
	require.Equal(t, http.StatusOK, w.Code)

	records := buf.records(t)
	require.Len(t, records, 1, "每个请求应恰好一条访问日志")

	rec := records[0]
	assert.Equal(t, "request", rec["msg"])
	assert.Equal(t, "INFO", rec["level"])
	assert.Equal(t, http.MethodGet, rec["method"])
	assert.Equal(t, "/api/health", rec["path"])
	assert.EqualValues(t, http.StatusOK, rec["status"])
	assert.NotNil(t, rec["duration_ms"])
	assert.NotEmpty(t, rec["client_ip"])
	assert.Equal(t, w.Header().Get(requestIDHeader), rec["request_id"],
		"日志里的 request_id 必须与响应头一致，否则串不起来")
}

func TestRequestLogger_LevelFollowsStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		method    string
		path      string
		body      string
		wantLevel string
	}{
		{name: "2xx 记 INFO", method: http.MethodGet, path: "/api/health", wantLevel: "INFO"},
		{name: "404 记 WARN", method: http.MethodGet, path: "/api/nope", wantLevel: "WARN"},
		{name: "405 记 WARN", method: http.MethodGet, path: "/api/echo", wantLevel: "WARN"},
		{
			name: "400 记 WARN", method: http.MethodPost, path: "/api/echo",
			body: `{}`, wantLevel: "WARN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log, buf := captureLogs()
			api := newTestAPIWith(testConfig(), log)

			request{method: tt.method, path: tt.path, body: tt.body}.
				doOn(t, SetupRouter(api))

			var access map[string]any
			for _, rec := range buf.records(t) {
				if rec["msg"] == "request" {
					access = rec
				}
			}
			require.NotNil(t, access, "没有找到访问日志")
			assert.Equal(t, tt.wantLevel, access["level"])
		})
	}
}

// TestRequestLogger_PanicLoggedAs500 is why requestLogger sits outside
// CustomRecovery. With the conventional order the panic would unwind past
// this middleware before Recovery set the status, and the access log
// would show the wrong outcome.
func TestRequestLogger_PanicLoggedAs500(t *testing.T) {
	t.Parallel()

	log, buf := captureLogs()
	api := newTestAPIWith(testConfig(), log)

	r := SetupRouter(api)
	r.GET("/api/boom", func(*gin.Context) { panic("boom") })

	w := request{method: http.MethodGet, path: "/api/boom"}.doOn(t, r)
	require.Equal(t, http.StatusInternalServerError, w.Code)

	var access map[string]any
	for _, rec := range buf.records(t) {
		if rec["msg"] == "request" {
			access = rec
		}
	}
	require.NotNil(t, access, "panic 之后仍应有访问日志")
	assert.EqualValues(t, http.StatusInternalServerError, access["status"])
	assert.Equal(t, "ERROR", access["level"])
}

// TestRequestLogger_OmitsQueryString pins the decision not to log query
// strings, which routinely carry tokens and API keys.
func TestRequestLogger_OmitsQueryString(t *testing.T) {
	t.Parallel()

	log, buf := captureLogs()
	api := newTestAPIWith(testConfig(), log)

	request{method: http.MethodGet, path: "/api/health?token=super-secret"}.
		doOn(t, SetupRouter(api))

	assert.NotContains(t, buf.String(), "super-secret")
}

func TestLevelForStatus(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "INFO", levelForStatus(http.StatusOK).String())
	assert.Equal(t, "INFO", levelForStatus(http.StatusMovedPermanently).String())
	assert.Equal(t, "WARN", levelForStatus(http.StatusBadRequest).String())
	assert.Equal(t, "WARN", levelForStatus(http.StatusNotFound).String())
	assert.Equal(t, "ERROR", levelForStatus(http.StatusInternalServerError).String())
	assert.Equal(t, "ERROR", levelForStatus(http.StatusServiceUnavailable).String())
}

// TestLimitBodySize_UsesConfiguredLimit confirms the cap now comes from
// config rather than a package constant.
func TestLimitBodySize_UsesConfiguredLimit(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.MaxBodyBytes = 64

	api := newTestAPIWith(cfg, discardLogger())
	w := request{
		method: http.MethodPost,
		path:   "/api/echo",
		body:   `{"message":"` + strings.Repeat("a", 200) + `"}`,
	}.doOn(t, SetupRouter(api))

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.Contains(t, w.Body.String(), "64 bytes")
}

// TestTrustedProxiesFromConfig confirms the router reads the list from
// config instead of its own os.Getenv call.
func TestTrustedProxiesFromConfig(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.TrustedProxies = []string{"203.0.113.9/32"}

	api := newTestAPIWith(cfg, discardLogger())
	r := SetupRouter(api)
	r.GET("/api/whoami", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })

	w := request{
		method:     http.MethodGet,
		path:       "/api/whoami",
		remoteAddr: "203.0.113.9:1234",
		headers:    map[string]string{"X-Forwarded-For": "1.2.3.4"},
	}.doOn(t, r)

	assert.Equal(t, "1.2.3.4", w.Body.String(),
		"来自可信代理的转发头应被采信")
}
