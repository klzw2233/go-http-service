package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-http-service/internal/model"
)

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()

	w := request{method: http.MethodGet, path: "/api/health"}.do(t)

	var got model.HealthResponse
	requireJSONResponse(t, w, http.StatusOK, &got)

	assert.Equal(t, model.StatusOK, got.Status)
	assert.True(t, got.Timestamp.Equal(fixedTime),
		"timestamp = %s, want %s", got.Timestamp, fixedTime)
}

// TestHealthIgnoresFailingDependencies pins the liveness contract: a
// broken dependency must not make /api/health fail, or an orchestrator
// would restart a process that is running fine.
func TestHealthIgnoresFailingDependencies(t *testing.T) {
	t.Parallel()

	api := newTestAPI(WithReadyCheck("always-broken", failingCheck))
	w := request{method: http.MethodGet, path: "/api/health"}.doOn(t, SetupRouter(api))

	var got model.HealthResponse
	requireJSONResponse(t, w, http.StatusOK, &got)
	assert.Equal(t, model.StatusOK, got.Status)
}

func TestInfoEndpoint(t *testing.T) {
	t.Parallel()

	w := request{method: http.MethodGet, path: "/api/info"}.do(t)

	var got model.InfoResponse
	requireJSONResponse(t, w, http.StatusOK, &got)

	// Asserting against the same constants the handler reads means a
	// version bump no longer needs an edit here.
	assert.Equal(t, model.Name, got.Name)
	assert.Equal(t, model.Version, got.Version)
	assert.Equal(t, runtime.Version(), got.GoVersion)
	assert.True(t, got.Timestamp.Equal(fixedTime),
		"timestamp = %s, want %s", got.Timestamp, fixedTime)
}

func TestEchoEndpoint_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		body        string
		contentType string
		want        string
	}{
		{
			name: "普通字符串",
			body: `{"message":"hello"}`,
			want: "hello",
		},
		{
			name: "中文与 emoji",
			body: `{"message":"你好 🎉"}`,
			want: "你好 🎉",
		},
		{
			name: "纯空格通过 required",
			body: `{"message":"   "}`,
			want: "   ",
			// required only rejects the empty string, so whitespace is
			// accepted. Pinned here so a future trim is a deliberate change.
		},
		{
			name: "恰好达到长度上限",
			body: fmt.Sprintf(`{"message":%q}`, strings.Repeat("a", model.MaxEchoMessageRunes)),
			want: strings.Repeat("a", model.MaxEchoMessageRunes),
		},
		{
			name: "上限按字符计而非字节",
			body: fmt.Sprintf(`{"message":%q}`, strings.Repeat("中", model.MaxEchoMessageRunes)),
			want: strings.Repeat("中", model.MaxEchoMessageRunes),
			// 4096 of these is 12288 bytes; passing confirms max counts runes.
		},
		{
			name: "未知字段被忽略",
			body: `{"message":"hi","unknown":123}`,
			want: "hi",
		},
		{
			name:        "非 JSON 的 Content-Type 也能绑定",
			body:        `{"message":"hi"}`,
			contentType: "text/plain",
			want:        "hi",
			// ShouldBindJSON ignores Content-Type. Documented, not endorsed.
		},
		{
			name:        "缺少 Content-Type 也能绑定",
			body:        `{"message":"hi"}`,
			contentType: noContentType,
			want:        "hi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := request{
				method:      http.MethodPost,
				path:        "/api/echo",
				body:        tt.body,
				contentType: tt.contentType,
			}.do(t)

			var got model.EchoResponse
			requireJSONResponse(t, w, http.StatusOK, &got)

			assert.Equal(t, tt.want, got.Message)
			assert.True(t, got.EchoedAt.Equal(fixedTime),
				"echoed_at = %s, want %s", got.EchoedAt, fixedTime)
		})
	}
}

func TestEchoEndpoint_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		body        string
		wantStatus  int
		wantCode    model.ErrorCode
		wantField   string
		wantReason  string
		wantNoField bool
	}{
		{
			name:       "缺少必填字段",
			body:       `{}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   model.ErrCodeValidationFailed,
			wantField:  "message",
			wantReason: "is required",
		},
		{
			name:       "空字符串不满足 required",
			body:       `{"message":""}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   model.ErrCodeValidationFailed,
			wantField:  "message",
			wantReason: "is required",
		},
		{
			name:       "超出长度上限一个字符",
			body:       fmt.Sprintf(`{"message":%q}`, strings.Repeat("a", model.MaxEchoMessageRunes+1)),
			wantStatus: http.StatusBadRequest,
			wantCode:   model.ErrCodeValidationFailed,
			wantField:  "message",
			wantReason: fmt.Sprintf("must be at most %d characters", model.MaxEchoMessageRunes),
		},
		{
			name:       "字段类型不匹配",
			body:       `{"message":123}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   model.ErrCodeValidationFailed,
			wantField:  "message",
			wantReason: "expected a string, got a number",
		},
		{
			name:        "JSON 被截断",
			body:        `{"message":`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    model.ErrCodeInvalidJSON,
			wantNoField: true,
		},
		{
			name:        "JSON 语法错误",
			body:        `{"message" "hi"}`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    model.ErrCodeInvalidJSON,
			wantNoField: true,
		},
		{
			name:        "空请求体",
			body:        ``,
			wantStatus:  http.StatusBadRequest,
			wantCode:    model.ErrCodeInvalidJSON,
			wantNoField: true,
		},
		{
			name:        "请求体超过上限",
			body:        fmt.Sprintf(`{"message":%q}`, strings.Repeat("b", 2*int(testConfig().MaxBodyBytes))),
			wantStatus:  http.StatusRequestEntityTooLarge,
			wantCode:    model.ErrCodePayloadTooLarge,
			wantNoField: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := request{method: http.MethodPost, path: "/api/echo", body: tt.body}.do(t)

			var got model.ErrorResponse
			requireJSONResponse(t, w, tt.wantStatus, &got)

			assert.Equal(t, tt.wantCode, got.Code)
			assert.NotEmpty(t, got.Message, "message should explain the failure")

			if tt.wantNoField {
				assert.Empty(t, got.Fields, "this error carries no field detail")
				return
			}

			require.Len(t, got.Fields, 1)
			assert.Equal(t, tt.wantField, got.Fields[0].Field)
			assert.Equal(t, tt.wantReason, got.Fields[0].Reason)
		})
	}
}

// TestErrorsNeverExposeGoIdentifiers guards the fix for the leak where
// validator and encoding/json put internal names on the wire, e.g.
// "Key: 'EchoRequest.Message' Error:Field validation for 'Message' ...".
func TestErrorsNeverExposeGoIdentifiers(t *testing.T) {
	t.Parallel()

	bodies := []struct{ name, body string }{
		{"缺少字段", `{}`},
		{"空字符串", `{"message":""}`},
		{"类型不匹配", `{"message":123}`},
		{"JSON 截断", `{"message":`},
		{"JSON 语法错误", `{"message" "hi"}`},
		{"空请求体", ``},
		{"超长字段", fmt.Sprintf(`{"message":%q}`, strings.Repeat("a", model.MaxEchoMessageRunes+1))},
	}

	// Substrings that must never reach a client. "Message" is
	// capitalised on purpose: the JSON field "message" is expected, the
	// Go field name is not.
	leaks := []string{"EchoRequest", "Message", "struct", "validator", "unmarshal", "Key:"}

	for _, tc := range bodies {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := request{method: http.MethodPost, path: "/api/echo", body: tc.body}.do(t)
			got := w.Body.String()

			for _, leak := range leaks {
				assert.NotContains(t, got, leak, "response leaked %q: %s", leak, got)
			}
		})
	}
}

func TestRouting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantCode   model.ErrorCode
		wantAllow  string
	}{
		{
			name:       "未知路径返回 JSON 404",
			method:     http.MethodGet,
			path:       "/api/nope",
			wantStatus: http.StatusNotFound,
			wantCode:   model.ErrCodeNotFound,
		},
		{
			name:       "unknown public path is JSON 404",
			method:     http.MethodGet,
			path:       "/no-such-public-path",
			wantStatus: http.StatusNotFound,
			wantCode:   model.ErrCodeNotFound,
		},
		{
			name:       "对 POST 路由用 GET",
			method:     http.MethodGet,
			path:       "/api/echo",
			wantStatus: http.StatusMethodNotAllowed,
			wantCode:   model.ErrCodeMethodNotAllowed,
			wantAllow:  "POST",
		},
		{
			name:       "对 GET 路由用 DELETE",
			method:     http.MethodDelete,
			path:       "/api/health",
			wantStatus: http.StatusMethodNotAllowed,
			wantCode:   model.ErrCodeMethodNotAllowed,
			wantAllow:  "GET",
		},
		{
			name:       "对 GET 路由用 POST",
			method:     http.MethodPost,
			path:       "/api/info",
			wantStatus: http.StatusMethodNotAllowed,
			wantCode:   model.ErrCodeMethodNotAllowed,
			wantAllow:  "GET",
		},
		{
			name:       "readiness 路由也遵守方法限制",
			method:     http.MethodPost,
			path:       "/api/ready",
			wantStatus: http.StatusMethodNotAllowed,
			wantCode:   model.ErrCodeMethodNotAllowed,
			wantAllow:  "GET",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := request{method: tt.method, path: tt.path}.do(t)

			var got model.ErrorResponse
			requireJSONResponse(t, w, tt.wantStatus, &got)

			assert.Equal(t, tt.wantCode, got.Code)

			// RFC 7231 requires Allow on a 405.
			assert.Equal(t, tt.wantAllow, w.Header().Get("Allow"))
		})
	}
}

// TestPanicReturnsJSON covers the custom recovery handler. gin's stock
// Recovery aborts with a bare 500 and no body, which would be the one
// response a JSON client could not parse.
func TestPanicReturnsJSON(t *testing.T) {
	t.Parallel()

	// A dedicated router so the panicking route is invisible to the
	// routing tests running in parallel.
	r := SetupRouter(newTestAPI())
	r.GET("/api/boom", func(*gin.Context) { panic("simulated handler failure") })

	req := httptest.NewRequest(http.MethodGet, "/api/boom", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Equal(t, jsonContentType, w.Header().Get("Content-Type"))

	var got model.ErrorResponse
	decodeBody(t, w, &got)

	assert.Equal(t, model.ErrCodeInternal, got.Code)
	// The panic value goes to the log, never to the client.
	assert.NotContains(t, w.Body.String(), "simulated handler failure")
}

// TestClientIPIgnoresForwardedHeader covers the SetTrustedProxies fix.
// With no trusted proxies, a caller cannot forge its own address.
func TestClientIPIgnoresForwardedHeader(t *testing.T) {
	t.Parallel()

	const realPeer = "203.0.113.9"

	r := SetupRouter(newTestAPI())
	r.GET("/api/whoami", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })

	w := request{
		method:     http.MethodGet,
		path:       "/api/whoami",
		remoteAddr: realPeer + ":1234",
		headers: map[string]string{
			"X-Forwarded-For": "1.2.3.4",
			"X-Real-IP":       "5.6.7.8",
		},
	}.doOn(t, r)

	assert.Equal(t, realPeer, w.Body.String(),
		"a spoofed forwarding header must not become the client IP")
}
