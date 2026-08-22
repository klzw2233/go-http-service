package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/gin-gonic/gin"
)

const (
	// requestIDHeader carries the correlation ID in and out.
	requestIDHeader = "X-Request-Id"

	// maxRequestIDLen bounds an ID supplied by the caller.
	maxRequestIDLen = 64
)

// contextKey is an unexported type so keys from this package cannot
// collide with keys any other package puts on the same context.
type contextKey struct{ name string }

var requestIDKey = &contextKey{"request-id"}

// requestID gives every request a correlation ID and publishes it on the
// request context, the gin context, and the response header.
//
// The context copy is the one that matters going forward: a query is
// handed c.Request.Context(), so the ID reaches slow-query logs without
// being threaded through every function signature.
func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := sanitiseRequestID(c.GetHeader(requestIDHeader))
		if id == "" {
			id = newRequestID()
		}

		c.Set(requestIDKey.name, id)
		c.Request = c.Request.WithContext(
			context.WithValue(c.Request.Context(), requestIDKey, id))
		c.Header(requestIDHeader, id)

		c.Next()
	}
}

// sanitiseRequestID returns the caller-supplied ID when it is safe to
// reuse, or "" to mean "generate a fresh one".
//
// The inbound value is echoed into every log line for the request, so
// accepting it unchecked would let a caller inject newlines, control
// characters, or counterfeit JSON into the logs. Restricting it to a
// short run of URL-safe characters keeps cross-service tracing working
// without that risk.
func sanitiseRequestID(raw string) string {
	if raw == "" || len(raw) > maxRequestIDLen {
		return ""
	}

	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_':
		default:
			return ""
		}
	}
	return raw
}

// newRequestID returns 16 random bytes rendered as hex. crypto/rand is
// used rather than a third-party UUID package: the only requirement is
// that IDs do not collide, and this costs no dependency.
func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Documented never to happen on supported platforms. Degrading to
		// a constant is still better than failing the request.
		return "norandom"
	}
	return hex.EncodeToString(b[:])
}

// requestIDOf reads the ID from the gin context, falling back to the
// request context for handlers reached without the middleware.
func requestIDOf(c *gin.Context) string {
	if v, ok := c.Get(requestIDKey.name); ok {
		if id, ok := v.(string); ok {
			return id
		}
	}
	return RequestIDFrom(c.Request.Context())
}

// RequestIDFrom returns the correlation ID carried by ctx, or "" when
// there is none. Exported so the future repository layer can tag its own
// logs with the request that triggered a query.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}
