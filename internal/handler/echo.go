package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"go-http-service/internal/model"
)

// EchoHandler handles POST /api/echo and returns the message back.
func EchoHandler(c *gin.Context) {
	var req model.EchoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, model.EchoResponse{
		Message:  req.Message,
		EchoedAt: time.Now().UTC(),
	})
}
