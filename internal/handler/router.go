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

	if err := r.SetTrustedProxies(api.cfg.TrustedProxies); err != nil {
		api.log.Error("invalid trusted proxies, falling back to trusting none", "error", err)
		_ = r.SetTrustedProxies(nil)
	}

	r.HandleMethodNotAllowed = true
	r.NoRoute(api.handleNoRoute)
	r.NoMethod(api.handleNoMethod)

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
		limited.POST("/users", api.forbidAnonymous(), api.requireAuth(), api.requireAuthor(), api.CreateUser)
		// Post writes/reads are Author-only: requireAuth answers 401 (with
		// WWW-Authenticate) for anonymous callers so there is no public JSON
		// feed, and requireAuthor answers 403 for a signed-in non-Author.
		limited.POST("/posts", api.requireAuth(), api.requireAuthor(), api.CreatePost)
		limited.GET("/posts", api.requireAuth(), api.requireAuthor(), api.ListPosts)
		limited.GET("/posts/:slug", api.requireAuth(), api.requireAuthor(), api.GetPost)
		limited.PATCH("/posts/:slug", api.requireAuth(), api.requireAuthor(), api.UpdatePost)
	}

	loginLimiter := newIPRateLimiter(
		rate.Limit(api.cfg.LoginRateLimitRPM)/60, int(api.cfg.LoginRateLimitBurst))

	authRoutes := r.Group(apiBasePath + "/auth")
	{
		authRoutes.POST("/login", api.rateLimit(loginLimiter), api.Login)
		authRoutes.POST("/refresh", api.rateLimit(loginLimiter), api.Refresh)
		authRoutes.POST("/logout", api.rateLimit(loginLimiter), api.Logout)
		authRoutes.GET("/me", api.rateLimit(globalLimiter), api.requireAuth(), api.Me)
	}

	return r
}

func (a *API) handleNoRoute(c *gin.Context) {
	a.respondError(c, http.StatusNotFound, model.ErrCodeNotFound,
		"no route matches this path")
}

func (a *API) handleNoMethod(c *gin.Context) {
	a.respondError(c, http.StatusMethodNotAllowed, model.ErrCodeMethodNotAllowed,
		"this HTTP method is not allowed on this path")
}

func (a *API) handlePanic(c *gin.Context, recovered any) {
	a.logFor(c).Error("handler panicked",
		"method", c.Request.Method,
		"path", c.Request.URL.Path,
		"panic", recovered,
		"stack", string(debug.Stack()))

	a.respondError(c, http.StatusInternalServerError, model.ErrCodeInternal,
		"the server failed to process this request")
}
