package main

import (
	"net/http"
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

// setupRouter configures and returns the application router.
func setupRouter() *gin.Engine {
	r := gin.Default()

	r.GET("/api/health", healthHandler)

	return r
}

func main() {
	port := "8080"

	r := setupRouter()
	r.Run(":" + port)
}
