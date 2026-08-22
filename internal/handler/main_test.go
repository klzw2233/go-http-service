package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"go-http-service/internal/config"
)

// fixedTime is the instant the test API reports. With the clock injected
// per API value, tests no longer save and restore a package-level var.
var fixedTime = time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

// testRouter is shared by most tests. gin.Engine is safe for concurrent
// ServeHTTP and the API holds no per-request state.
var testRouter *gin.Engine

// TestMain replaces the gin.SetMode call that used to open every test
// function, and silences gin's own writers so a failed assertion is not
// buried in noise.
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	gin.DefaultWriter = io.Discard
	gin.DefaultErrorWriter = io.Discard

	testRouter = SetupRouter(newTestAPI())

	os.Exit(m.Run())
}

// testConfig returns the settings tests run against. RequestTimeout is
// generous so it never fires by accident; the timeout test sets its own.
func testConfig() *config.Config {
	return &config.Config{
		Port:              "8080",
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ShutdownTimeout:   15 * time.Second,
		RequestTimeout:    8 * time.Second,
		MaxBodyBytes:      1 << 20,
		LogLevel:          slog.LevelDebug,
		LogFormat:         config.FormatJSON,
	}
}

// discardLogger keeps test output readable.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// newTestAPI builds an API with the clock pinned to fixedTime.
func newTestAPI(opts ...Option) *API {
	base := []Option{WithClock(func() time.Time { return fixedTime })}
	return New(testConfig(), discardLogger(), append(base, opts...)...)
}

// newTestAPIWith builds an API from a customised config, for tests that
// need a specific timeout or body limit.
func newTestAPIWith(cfg *config.Config, log *slog.Logger, opts ...Option) *API {
	base := []Option{WithClock(func() time.Time { return fixedTime })}
	return New(cfg, log, append(base, opts...)...)
}

// lockedBuffer is a bytes.Buffer safe for concurrent writes. The logging
// middleware emits from the request goroutine, so an unsynchronised
// buffer trips the race detector.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// records parses every emitted line as a JSON log record.
func (b *lockedBuffer) records(t *testing.T) []map[string]any {
	t.Helper()

	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec), "日志不是合法 JSON: %s", line)
		out = append(out, rec)
	}
	return out
}

// captureLogs returns a logger writing JSON records into the buffer.
func captureLogs() (*slog.Logger, *lockedBuffer) {
	buf := &lockedBuffer{}
	handler := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(handler), buf
}

// noContentType tells do to omit the Content-Type header entirely.
const noContentType = "-"

// request describes one HTTP call.
type request struct {
	method string
	path   string
	body   string
	// contentType overrides the default. Use noContentType to send none.
	contentType string
	// headers are applied after contentType.
	headers map[string]string
	// remoteAddr overrides the default test peer address.
	remoteAddr string
}

// do performs the request against the shared testRouter.
func (r request) do(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	return r.doOn(t, testRouter)
}

// doOn performs the request against a specific engine, for tests that
// need their own API configuration.
func (r request) doOn(t *testing.T, engine *gin.Engine) *httptest.ResponseRecorder {
	t.Helper()

	var body io.Reader
	if r.body != "" {
		body = strings.NewReader(r.body)
	}

	req := httptest.NewRequest(r.method, r.path, body)
	switch r.contentType {
	case noContentType:
		// send nothing
	case "":
		req.Header.Set("Content-Type", "application/json")
	default:
		req.Header.Set("Content-Type", r.contentType)
	}
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	if r.remoteAddr != "" {
		req.RemoteAddr = r.remoteAddr
	}

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

// decodeBody unmarshals the response body into dst, reporting the raw
// body on failure so a shape mismatch is diagnosable.
func decodeBody(t *testing.T, w *httptest.ResponseRecorder, dst any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), dst),
		"response body was: %s", w.Body.String())
}

// jsonContentType is what gin sets on every c.JSON response.
const jsonContentType = "application/json; charset=utf-8"

// requireJSONResponse asserts the status and content type, then decodes.
func requireJSONResponse(t *testing.T, w *httptest.ResponseRecorder, wantStatus int, dst any) {
	t.Helper()
	require.Equal(t, wantStatus, w.Code, "body: %s", w.Body.String())
	require.Equal(t, jsonContentType, w.Header().Get("Content-Type"))
	decodeBody(t, w, dst)
}
