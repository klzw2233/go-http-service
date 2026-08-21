package main

import (
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
)

// HealthResponse represents the response body of /api/health.
type HealthResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

// NewHealthResponse creates a new HealthResponse with the current UTC time.
func NewHealthResponse() HealthResponse {
	return HealthResponse{
		Status:    "ok",
		Timestamp: time.Now().UTC(),
	}
}

// healthHandler handles GET /api/health and returns a simple JSON status.
func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, NewHealthResponse())
}

// InfoResponse represents the response body of /api/info.
type InfoResponse struct {
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	GoVersion string    `json:"go_version"`
	Timestamp time.Time `json:"timestamp"`
}

// infoHandler handles GET /api/info and returns service metadata.
func infoHandler(c *gin.Context) {
	c.JSON(http.StatusOK, InfoResponse{
		Name:      "go-http-service",
		Version:   "0.2.0",
		GoVersion: runtime.Version(),
		Timestamp: time.Now().UTC(),
	})
}

// EchoRequest represents the request body of /api/echo.
type EchoRequest struct {
	Message string `json:"message" binding:"required"`
}

// EchoResponse represents the response body of /api/echo.
type EchoResponse struct {
	Message  string    `json:"message"`
	EchoedAt time.Time `json:"echoed_at"`
}

// echoHandler handles POST /api/echo and returns the message back.
func echoHandler(c *gin.Context) {
	var req EchoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, EchoResponse{
		Message:  req.Message,
		EchoedAt: time.Now().UTC(),
	})
}

// setupRouter configures and returns the application router.
func setupRouter() *gin.Engine {
	r := gin.Default()

	r.GET("/api/health", healthHandler)
	r.GET("/api/info", infoHandler)
	r.POST("/api/echo", echoHandler)

	return r
}

func main() {
	port := "8080"

	r := setupRouter()
	r.Run(":" + port)
}
