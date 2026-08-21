package handler

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"go-http-service/internal/model"
)

// apiBasePath prefixes every route in this service.
const apiBasePath = "/api"

// SetupRouter configures and returns the application router.
func SetupRouter() *gin.Engine {
	configureValidator()

	r := gin.New()
	r.Use(gin.Logger())
	r.Use(gin.CustomRecovery(handlePanic))

	// gin trusts every proxy by default, so c.ClientIP() believes whatever
	// X-Forwarded-For the caller sends. No handler reads ClientIP yet, but
	// the planned request log and rate limiter both will, and per-IP
	// limiting against a spoofable IP is no limiting at all.
	if err := r.SetTrustedProxies(trustedProxies()); err != nil {
		log.Printf("trusted proxies: %v", err)
	}

	// Off by default, which makes a known path with the wrong verb return
	// 404 and send callers off to debug routing or deployment when the
	// real problem is the method. gin also sets the Allow header for us
	// once this is on, as RFC 7231 requires.
	r.HandleMethodNotAllowed = true

	r.Use(limitBodySize(maxBodyBytes))

	r.NoRoute(handleNoRoute)
	r.NoMethod(handleNoMethod)

	api := r.Group(apiBasePath)
	{
		api.GET("/health", HealthHandler)
		api.GET("/info", InfoHandler)
		api.POST("/echo", EchoHandler)
	}

	return r
}

// handleNoRoute answers unmatched paths in JSON. gin's default writes
// "404 page not found" as text/plain, which breaks clients that parse
// every response as JSON.
func handleNoRoute(c *gin.Context) {
	respondError(c, http.StatusNotFound, model.ErrCodeNotFound,
		"no route matches this path")
}

// handleNoMethod answers a known path reached with the wrong method.
// The response deliberately does not echo the method back, since it is
// caller-controlled; the Allow header carries the useful information.
func handleNoMethod(c *gin.Context) {
	respondError(c, http.StatusMethodNotAllowed, model.ErrCodeMethodNotAllowed,
		"this HTTP method is not allowed on this path")
}

// handlePanic keeps the JSON error contract intact when a handler panics.
// gin's stock Recovery aborts with a bare 500 and no body, which would be
// the one response a client could not parse. The panic value and stack go
// to the log; the client is told nothing it cannot act on.
func handlePanic(c *gin.Context, recovered any) {
	log.Printf("panic %s %s: %v", c.Request.Method, c.Request.URL.Path, recovered)
	respondError(c, http.StatusInternalServerError, model.ErrCodeInternal,
		"the server failed to process this request")
}

// trustedProxies returns the proxy addresses to trust, read from
// TRUSTED_PROXIES as a comma-separated list of IPs or CIDRs.
//
// It returns nil when unset, meaning trust nobody and take the client IP
// from the direct peer address. That is the safe default: it is correct
// for a directly exposed service, and wrong only in a way that
// under-reports rather than lets a caller forge its own address.
func trustedProxies() []string {
	raw := os.Getenv("TRUSTED_PROXIES")
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	proxies := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			proxies = append(proxies, p)
		}
	}
	return proxies
}
