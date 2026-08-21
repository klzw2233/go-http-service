package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go-http-service/internal/model"
)

// InfoHandler handles GET /api/info and returns service metadata.
func InfoHandler(c *gin.Context) {
	c.JSON(http.StatusOK, model.NewInfoResponse(now()))
}
