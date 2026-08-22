package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go-http-service/internal/model"
)

// Health handles GET /api/health, the liveness probe.
//
// It reports only that the process is up and serving, and deliberately
// checks no dependencies. A liveness probe that fails while the database
// is briefly unreachable makes the orchestrator restart a process that
// was working fine, turning a transient dependency blip into an outage.
// Dependency state belongs in Ready.
func (a *API) Health(c *gin.Context) {
	c.JSON(http.StatusOK, model.NewHealthResponse(a.now()))
}
