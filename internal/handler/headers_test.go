package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// headersEngine builds a minimal engine with only the SecurityHeaders
// middleware and handlers that exercise the success and error paths, so the
// assertions cover every response shape the service produces.
func headersEngine() *gin.Engine {
	r := gin.New()
	r.Use(SecurityHeaders())
	r.GET("/ok", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/boom", func(c *gin.Context) {
		c.String(http.StatusInternalServerError, "boom")
	})
	return r
}

// TestSecurityHeaders_PresentOnSuccess confirms the full baseline set is
// attached to a normal 200 response.
func TestSecurityHeaders_PresentOnSuccess(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	w := httptest.NewRecorder()
	headersEngine().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	h := w.Header()

	assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", h.Get("X-Frame-Options"))
	assert.Equal(t, "0", h.Get("X-XSS-Protection"),
		"旧版 XSS 审计器应被关闭，值为 0")
	assert.Equal(t, "strict-origin-when-cross-origin", h.Get("Referrer-Policy"))
	assert.Contains(t, h.Get("Strict-Transport-Security"), "max-age=")
	assert.Contains(t, h.Get("Strict-Transport-Security"), "includeSubDomains")
	assert.Contains(t, h.Get("Permissions-Policy"), "camera=()")
	assert.Contains(t, h.Get("Permissions-Policy"), "microphone=()")
	assert.Contains(t, h.Get("Permissions-Policy"), "payment=()")
	assert.Equal(t, "no-store", h.Get("Cache-Control"))
}

// TestSecurityHeaders_PresentOnErrors confirms the protections are not lost on
// an error response, which is the whole point of writing them before c.Next().
func TestSecurityHeaders_PresentOnErrors(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	w := httptest.NewRecorder()
	headersEngine().ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	h := w.Header()

	assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", h.Get("X-Frame-Options"))
	assert.Equal(t, "no-store", h.Get("Cache-Control"))
}

// TestSecurityHeaders_AppliedViaRouter confirms the middleware is actually
// wired into SetupRouter, so a header forgotten in wiring would fail here
// rather than only in a standalone test.
func TestSecurityHeaders_AppliedViaRouter(t *testing.T) {
	t.Parallel()

	w := request{method: http.MethodGet, path: "/api/health"}.do(t)

	require.Equal(t, http.StatusOK, w.Code)
	h := w.Header()

	assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", h.Get("X-Frame-Options"))
	assert.Equal(t, "no-store", h.Get("Cache-Control"))
}

// TestSecurityHeaders_XSSProtectionIsZero pins the deliberate choice to
// disable the legacy auditor: a non-zero value would enable a mechanism that
// modern browsers have removed and that older versions used as an attack
// vector.
func TestSecurityHeaders_XSSProtectionIsZero(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	w := httptest.NewRecorder()
	headersEngine().ServeHTTP(w, req)

	assert.Equal(t, "0", w.Header().Get("X-XSS-Protection"),
		"X-XSS-Protection 必须为 0（关闭旧审计器），而非 1; mode=block")
}

// TestSecurityHeaders_NoStorePreventsCaching confirms API responses carry a
// no-store directive, since they are per-user and frequently carry credentials
// that must never be served from a shared cache.
func TestSecurityHeaders_NoStorePreventsCaching(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	w := httptest.NewRecorder()
	headersEngine().ServeHTTP(w, req)

	cc := w.Header().Get("Cache-Control")
	assert.Equal(t, "no-store", cc, "API 响应不得被缓存")
	assert.NotContains(t, cc, "public")
}
