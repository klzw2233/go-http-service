package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go-http-service/internal/model"
)

// Echo handles POST /api/echo and returns the message back.
func (a *API) Echo(c *gin.Context) {
	var req model.EchoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		a.respondBindError(c, err)
		return
	}

	c.JSON(http.StatusOK, model.NewEchoResponse(req.Message, a.now()))
}
