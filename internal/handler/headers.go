package handler

import "github.com/gin-gonic/gin"

// SecurityHeaders adds a baseline set of response headers that harden every
// response, including errors and 404s.
//
// It is registered after the body-size limit but before route handlers so
// the headers are written up front. Writing them before c.Next() matters:
// the timeout middleware does not write its own response (it only sets a
// deadline), but a handler that aborts with a 500 or a 404 still flows
// through the headers already on the ResponseWriter, so even failure
// responses carry the protections.
//
// These are defence-in-depth hints the browser enforces; none of them change
// the response body. A value of "0" disables a legacy mechanism rather than
// enabling it (X-XSS-Protection is set to 0 deliberately below).
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()

		// Prevent MIME-type sniffing. Without this a browser may treat an
		// uploaded text file as HTML or script and execute it.
		h.Set("X-Content-Type-Options", "nosniff")

		// Stop the page from being framed by any origin, which is the
		// baseline against clickjacking. A same-origin allowlist would
		// need a reason this API does not have.
		h.Set("X-Frame-Options", "DENY")

		// Disable the legacy IE/old-Edge reflected-XSS auditor. Modern
		// browsers have removed it, and in the versions that still have it
		// the auditor itself introduced bypasses. "0" turns it off.
		h.Set("X-XSS-Protection", "0")

		// Control how much of the referring URL a destination receives.
		// strict-origin-when-cross-origin keeps full origin on same-origin
		// navigations but sends only the origin, stripped, across origins.
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Tell the browser to only load this response over HTTPS, and to
		// remember that for 30 days. The API itself never redirects to
		// plain HTTP, but this protects a user who mistyped the scheme from
		// silently downgrading. opt-in for subdomains; if a subdomain needs
		// plain HTTP it is responsible for not declaring HSTS.
		h.Set("Strict-Transport-Security", "max-age=2592000; includeSubDomains")

		// Restrict the powerful APIs a document may use. Nothing the API
		// returns needs camera, microphone, geolocation or payment, so deny
		// all of them by default.
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")

		// Keep the response out of shared caches and the browser's back
		// forward cache. API responses are per-user and frequently carry
		// credentials, so they must never be served from a cache to a
		// different request.
		h.Set("Cache-Control", "no-store")

		c.Next()
	}
}
