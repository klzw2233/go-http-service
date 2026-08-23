package handler

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

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
	// 5. Body cap: it only concerns handlers that read a body.
	r.Use(limitBodySize(api.cfg.MaxBodyBytes))
	// 6. Security headers on every response, including 404s and panics, so
	//    they are written before any handler or error path can short-circuit.
	r.Use(SecurityHeaders())
	// 7. CORS last among the global middleware. It is fail-closed: with no
	//    allowed origins configured it adds no Access-Control-* header, and
	//    a browser denies every cross-origin request. Same-origin and
	//    non-browser callers are unaffected.
	r.Use(CORS(api.cfg.CORSAllowedOrigins))

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

	// Rate limiting is per-group rather than global, because the probes
	// must not be limited at all.
	//
	// kubelet polls /api/health and /api/ready constantly and does so
	// from one address, so a shared budget would throttle them and the
	// orchestrator would start restarting healthy containers. The rate
	// limiter would have caused the outage it exists to prevent.
	probes := r.Group(apiBasePath)
	{
		probes.GET("/health", api.Health)
		probes.GET("/ready", api.Ready)
	}

	globalLimiter := newIPRateLimiter(
		rate.Limit(api.cfg.RateLimitRPS), int(api.cfg.RateLimitBurst))

	limited := r.Group(apiBasePath)
	limited.Use(api.rateLimit(globalLimiter))
	{
		limited.GET("/info", api.Info)
		limited.POST("/echo", api.Echo)
		limited.POST("/users", api.CreateUser)
	}

	// Login gets its own, far tighter bucket. It is the one endpoint
	// where an attacker gains something from sheer volume, and the
	// global budget is generous enough to be useless against guessing.
	loginLimiter := newIPRateLimiter(
		rate.Limit(api.cfg.LoginRateLimitRPM)/60, int(api.cfg.LoginRateLimitBurst))

	authRoutes := r.Group(apiBasePath + "/auth")
	{
		authRoutes.POST("/login", api.rateLimit(loginLimiter), api.Login)
		authRoutes.POST("/refresh", api.rateLimit(loginLimiter), api.Refresh)
		authRoutes.POST("/logout", api.rateLimit(loginLimiter), api.Logout)

		// Reads behind a token only need the global budget; they are
		// bounded by having to hold a valid token in the first place.
		authRoutes.GET("/me", api.rateLimit(globalLimiter), api.requireAuth(), api.Me)
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
