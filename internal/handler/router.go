package handler

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"go-http-service/internal/model"
)

// apiBasePath prefixes every route in this service.
const apiBasePath = "/api"

// SetupRouter configures and returns the application router.
//
// The middleware order below is deliberate and is the part most likely to
// be broken by a careless edit; each step says why it sits where it does.
func SetupRouter(api *API) *gin.Engine {
	configureValidator()

	r := gin.New()

	// 1. Correlation ID first, so everything logged afterwards carries it.
	r.Use(requestID())
	// 2. Access log next: it measures the whole request and must observe
	//    the final status. It has to sit OUTSIDE Recovery — see the
	//    comment on requestLogger for why the usual order is wrong here.
	r.Use(requestLogger(api.log))
	// 3. Recovery inside the logger, so a panic is turned into a 500
	//    before the access log reads the status.
	r.Use(gin.CustomRecovery(api.handlePanic))
	// 4. Deadline for handler work, inherited by any query downstream.
	r.Use(timeout(api.cfg.RequestTimeout))
	// 5. Body cap last: it only concerns handlers that read a body.
	r.Use(limitBodySize(api.cfg.MaxBodyBytes))

	// gin trusts every proxy by default, so c.ClientIP() would believe any
	// X-Forwarded-For it is sent. An empty list means trust nobody.
	if err := r.SetTrustedProxies(api.cfg.TrustedProxies); err != nil {
		// Config validates these, so this is defence in depth. Leaving the
		// gin default in place would silently mean "trust everyone", so
		// fail closed instead.
		api.log.Error("invalid trusted proxies, falling back to trusting none", "error", err)
		_ = r.SetTrustedProxies(nil)
	}

	// Off by default, which makes a known path with the wrong verb return
	// 404 and send callers off to debug routing. gin also supplies the
	// RFC 7231 Allow header once this is enabled.
	r.HandleMethodNotAllowed = true

	r.NoRoute(api.handleNoRoute)
	r.NoMethod(api.handleNoMethod)

	group := r.Group(apiBasePath)
	{
		group.GET("/health", api.Health)
		group.GET("/ready", api.Ready)
		group.GET("/info", api.Info)
		group.POST("/echo", api.Echo)
	}

	return r
}

// handleNoRoute answers unmatched paths in JSON. gin's default writes
// "404 page not found" as text/plain, which breaks clients that parse
// every response as JSON.
func (a *API) handleNoRoute(c *gin.Context) {
	a.respondError(c, http.StatusNotFound, model.ErrCodeNotFound,
		"no route matches this path")
}

// handleNoMethod answers a known path reached with the wrong method. The
// response deliberately does not echo the method back, since that is
// caller-controlled; the Allow header carries what the caller needs.
func (a *API) handleNoMethod(c *gin.Context) {
	a.respondError(c, http.StatusMethodNotAllowed, model.ErrCodeMethodNotAllowed,
		"this HTTP method is not allowed on this path")
}

// handlePanic keeps the JSON error contract intact when a handler panics.
// gin's stock Recovery aborts with a bare 500 and no body, which would be
// the one response a client could not parse. The panic value and stack go
// to the log; the client is told nothing it cannot act on.
func (a *API) handlePanic(c *gin.Context, recovered any) {
	a.logFor(c).Error("handler panicked",
		"method", c.Request.Method,
		"path", c.Request.URL.Path,
		"panic", recovered,
		"stack", string(debug.Stack()))

	a.respondError(c, http.StatusInternalServerError, model.ErrCodeInternal,
		"the server failed to process this request")
}
