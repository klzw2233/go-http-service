package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestHealthHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := SetupRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	resp := httptest.NewRecorder()

	r.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), `"status":"ok"`)
}

func TestInfoHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := SetupRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	resp := httptest.NewRecorder()

	r.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), `"name":"go-http-service"`)
	assert.Contains(t, resp.Body.String(), `"version":"0.2.0"`)
}

func TestEchoHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := SetupRouter()

	body := `{"message":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/echo", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), `"message":"hello"`)
	assert.Contains(t, resp.Body.String(), `"echoed_at"`)
}

func TestEchoHandler_BadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := SetupRouter()

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/echo", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Contains(t, resp.Body.String(), "error")
}
