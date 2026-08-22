package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go-http-service/internal/model"
)

// Info handles GET /api/info and returns service metadata.
func (a *API) Info(c *gin.Context) {
	c.JSON(http.StatusOK, model.NewInfoResponse(a.now()))
}
