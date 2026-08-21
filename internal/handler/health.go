package handler

import (
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"

	"go-http-service/internal/model"
)

// HealthHandler handles GET /api/health and returns a simple JSON status.
func HealthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, model.NewHealthResponse())
}

// InfoHandler handles GET /api/info and returns service metadata.
func InfoHandler(c *gin.Context) {
	c.JSON(http.StatusOK, model.InfoResponse{
		Name:      "go-http-service",
		Version:   "0.2.0",
		GoVersion: runtime.Version(),
		Timestamp: time.Now().UTC(),
	})
}
