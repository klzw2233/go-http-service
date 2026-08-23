package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// corsEngine builds a minimal engine with only the CORS middleware, so a test
// can exercise CORS behaviour without the rate limiter or timeout getting in
// the way.
func corsEngine(allowed []string) *gin.Engine {
	r := gin.New()
	r.Use(CORS(allowed))
	r.GET("/api/health", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return r
}

// corsRequest builds a request with the given Origin, mirroring what a browser
// sends. An empty origin simulates a same-origin or non-browser call.
func corsRequest(t *testing.T, engine *gin.Engine, origin string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

// TestCORS_NoOriginsConfiguredIsDenied is the fail-closed property: with no
// allowed origins, a cross-origin request gets no Access-Control-Allow-Origin,
// which the browser treats as a denial.
func TestCORS_NoOriginsConfiguredIsDenied(t *testing.T) {
	t.Parallel()

	w := corsRequest(t, corsEngine(nil), "https://app.example.com")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"),
		"未配置 origin 时不应返回 ACAO，浏览器应拒绝跨域")
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
}

// TestCORS_NoOriginHeaderIsPassthrough confirms that a same-origin or
// non-browser request (no Origin header) is unaffected: CORS simply does not
// apply.
func TestCORS_NoOriginHeaderIsPassthrough(t *testing.T) {
	t.Parallel()

	w := corsRequest(t, corsEngine([]string{"https://app.example.com"}), "")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"),
		"无 Origin 头时 CORS 不生效")
}

// TestCORS_AllowedOriginEchoedAndCredentialsEnabled covers the happy path: an
// exact match is echoed back (not a wildcard), and credentials are enabled so
// credentialed requests keep working.
func TestCORS_AllowedOriginEchoedAndCredentialsEnabled(t *testing.T) {
	t.Parallel()

	const origin = "https://app.example.com"
	w := corsRequest(t, corsEngine([]string{origin}), origin)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, origin, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
	assert.Contains(t, w.Header().Get("Vary"), "Origin")
}

// TestCORS_UnlistedOriginDenied pins the substring-matching property: an
// origin that shares a suffix with an allowed one must not be authorised, so
// "https://evilapp.example.com" does not piggyback on
// "https://app.example.com".
func TestCORS_UnlistedOriginDenied(t *testing.T) {
	t.Parallel()

	w := corsRequest(t,
		corsEngine([]string{"https://app.example.com"}),
		"https://evilapp.example.com")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"),
		"子串相似不等于允许")
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
}

// TestCORS_WildcardDoesNotEnableCredentials confirms the spec rule the code
// relies on: a "*" allowed origin is echoed as the wildcard and never carries
// Allow-Credentials, since a browser forbids the combination.
func TestCORS_WildcardDoesNotEnableCredentials(t *testing.T) {
	t.Parallel()

	w := corsRequest(t, corsEngine([]string{"*"}), "https://anywhere.example.com")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"),
		"通配符不得同时启用凭据")
}

// TestCORS_PreflightShortCircuitsWith204 checks that an OPTIONS preflight for
// an allowed origin returns 204 with the preflight headers and never reaches
// the handler.
func TestCORS_PreflightShortCircuitsWith204(t *testing.T) {
	t.Parallel()

	const origin = "https://app.example.com"
	engine := corsEngine([]string{origin})

	req := httptest.NewRequest(http.MethodOptions, "/api/health", nil)
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, Authorization")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, origin, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Methods"), http.MethodPost)
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Headers"), "Authorization")
	assert.NotEmpty(t, w.Header().Get("Access-Control-Max-Age"))
}

// TestCORS_PreflightForUnlistedOriginIsDenied ensures a preflight from an
// unlisted origin gets no ACAO and still short-circuits, so a browser cannot
// discover allowed methods for a disallowed origin.
func TestCORS_PreflightForUnlistedOriginIsDenied(t *testing.T) {
	t.Parallel()

	engine := corsEngine([]string{"https://app.example.com"})

	req := httptest.NewRequest(http.MethodOptions, "/api/health", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Methods"))
}

// TestCORS_AllowedHeadersDoesNotEchoRequest pins the fixed-allowlist decision:
// a client requesting an exotic header must not have it approved, since an
// echo-back policy approves whatever the client asks for.
func TestCORS_AllowedHeadersDoesNotEchoRequest(t *testing.T) {
	t.Parallel()

	const origin = "https://app.example.com"
	engine := corsEngine([]string{origin})

	req := httptest.NewRequest(http.MethodOptions, "/api/health", nil)
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "X-Exotic, X-Also-Banned")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
	got := w.Header().Get("Access-Control-Allow-Headers")
	assert.NotContains(t, got, "X-Exotic", "不得回显请求头")
	assert.Contains(t, got, "Authorization")
}

// TestCORS_ExposeHeadersCarriesRequestID confirms the X-Request-Id header is
// exposed so a browser-based caller can read the correlation ID back from a
// cross-origin response.
func TestCORS_ExposeHeadersCarriesRequestID(t *testing.T) {
	t.Parallel()

	const origin = "https://app.example.com"
	w := corsRequest(t, corsEngine([]string{origin}), origin)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Access-Control-Expose-Headers"), "X-Request-Id")
}
