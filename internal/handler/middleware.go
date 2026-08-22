package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// limitBodySize wraps the request body so reads past limit fail with
// *http.MaxBytesError instead of allocating without bound. Applied
// router-wide so endpoints added later inherit the protection.
func limitBodySize(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

// timeout puts a deadline on the request context.
//
// The http.Server timeouts cannot do this: ReadTimeout and WriteTimeout
// govern moving bytes over the socket, and neither interrupts a handler
// blocked on a slow dependency. This is the deadline a database query
// inherits once it is handed c.Request.Context().
//
// It deliberately does NOT write a timeout response of its own. Racing
// the handler for the ResponseWriter is a data race, and there is no safe
// way to stop a handler that ignores its context. The contract is the
// other way round: handlers pass c.Request.Context() to anything that
// blocks, and turn the resulting context.DeadlineExceeded into a response.
func timeout(d time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()

		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
