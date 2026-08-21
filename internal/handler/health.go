package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go-http-service/internal/model"
)

// HealthHandler handles GET /api/health and returns a simple JSON status.
func HealthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, model.NewHealthResponse(now()))
}
