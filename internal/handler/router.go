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
func SetupRouter(api *API) *gin.Engine {
	configureValidator()

	r := gin.New()

	r.Use(requestID())
	r.Use(requestLogger(api.log))
	r.Use(gin.CustomRecovery(api.handlePanic))
	r.Use(timeout(api.cfg.RequestTimeout))
	r.Use(limitBodySize(api.cfg.MaxBodyBytes))
	r.Use(SecurityHeaders())
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
