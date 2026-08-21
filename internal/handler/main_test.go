package handler

import (
	"encoding/json"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// testRouter is shared by every test. gin.Engine is safe for concurrent
// ServeHTTP, and SetupRouter holds no per-request state.
var testRouter *gin.Engine

// TestMain replaces the gin.SetMode call that used to open every single
// test function, and silences the request and panic logs so a failing
// assertion is not buried in noise.
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	gin.DefaultWriter = io.Discard
	gin.DefaultErrorWriter = io.Discard
	log.SetOutput(io.Discard)

	testRouter = SetupRouter()

	os.Exit(m.Run())
}

// fixedTime is the instant handlers report while withFixedTime is active.
var fixedTime = time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)

// withFixedTime pins the handler clock so timestamps can be asserted
// exactly, and restores it when the test ends.
//
// now is package-level state, so a test that calls this must not call
// t.Parallel itself. Its subtests may, since the cleanup registered here
// does not run until they have all finished.
func withFixedTime(t *testing.T) {
	t.Helper()

	original := now
	now = func() time.Time { return fixedTime }
	t.Cleanup(func() { now = original })
}

// request describes one HTTP call against testRouter.
type request struct {
	method string
	path   string
	body   string
	// contentType overrides the default. Use noContentType to send none.
	contentType string
}

// noContentType tells do to omit the Content-Type header entirely.
const noContentType = "-"

// do performs the request and returns the recorded response.
func (r request) do(t *testing.T) *httptest.ResponseRecorder {
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

	w := httptest.NewRecorder()
	testRouter.ServeHTTP(w, req)
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
