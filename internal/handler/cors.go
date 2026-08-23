package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// corsHeaders lists the request headers a browser is allowed to send. It is
// fixed rather than reflecting Access-Control-Request-Headers verbatim: an
// echo-back policy approves whatever the client asks for, which defeats the
// point of a restrictive allowlist.
const corsHeaders = "Origin, Content-Type, Authorization, X-Request-Id"

// corsMethods lists the HTTP verbs the API actually uses. OPTIONS is implied
// by the preflight handling and omitted to avoid implying it is a real
// endpoint.
const corsMethods = "GET, POST, PUT, PATCH, DELETE"

// CORS enforces a fail-closed cross-origin policy.
//
// With no allowed origins configured, every cross-origin request is denied:
// the browser will block a web app on a different host from reading a
// response. Same-origin calls and non-browser clients (curl, server-to-server)
// are unaffected by CORS, so this never locks out the API's own clients.
//
// When origins are configured, a request's Origin is matched exactly against
// the allowlist. Matching is by string equality, never by substring, so
// "https://app.example.com" does not authorise "https://evilapp.example.com".
// A matching origin is echoed back as Access-Control-Allow-Origin rather than
// using the wildcard, so credentialed requests (cookies, Authorization) keep
// working: a browser refuses to send credentials alongside a wildcard ACAO.
func CORS(allowed []string) gin.HandlerFunc {
	allowAll := len(allowed) == 1 && allowed[0] == "*"
	allowSet := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		allowSet[o] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		// No Origin header means a same-origin request or a non-browser
		// client. CORS does not apply; nothing to add.
		if origin == "" {
			c.Next()
			return
		}

		var allowedOrigin string
		switch {
		case allowAll:
			allowedOrigin = "*"
		case allowAllOrigin(origin, allowSet):
			allowedOrigin = origin
		}

		if allowedOrigin == "" {
			// Do not set any Access-Control-* header. The browser treats a
			// missing ACAO as a denial, which is exactly what fail-closed
			// means. OPTIONS still falls through to 404/405 so a misconfigured
			// preflight is noisy rather than silently 200.
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
			c.Next()
			return
		}

		h := c.Writer.Header()
		h.Set("Access-Control-Allow-Origin", allowedOrigin)
		h.Set("Access-Control-Allow-Methods", corsMethods)
		h.Set("Access-Control-Allow-Headers", corsHeaders)
		h.Set("Access-Control-Expose-Headers", requestIDHeader)
		h.Set("Access-Control-Max-Age", "600")
		if allowedOrigin != "*" {
			// Credentials are never useful with a wildcard, and the browser
			// forbids combining the two. Only signal support when an origin
			// was actually matched.
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Add("Vary", "Origin")
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// allowAllOrigin reports whether origin is an exact, case-sensitive match for
// an entry in the allowlist. Origins are case-insensitive by spec on the
// scheme and host, but an exact comparison avoids normalisation surprises and
// matches what the browser actually sends.
func allowAllOrigin(origin string, allowSet map[string]struct{}) bool {
	_, ok := allowSet[origin]
	return ok
}
