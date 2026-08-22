package handler

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// requestLogger emits one structured record per request.
//
// It replaces gin.Logger(), whose fixed text format cannot be filtered by
// field and carries no correlation ID.
//
// Register it OUTSIDE CustomRecovery, not inside. Were Recovery the outer
// middleware, a handler panic would unwind through this middleware's
// c.Next() before Recovery had set the 500, and the access log would
// record the wrong status. With Recovery inside, the panic becomes a 500
// first and this sees the real outcome.
//
// The query string is deliberately not logged: it routinely carries
// tokens and API keys, and no endpoint here reads one.
func requestLogger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// Captured before c.Next because a handler may rewrite the URL.
		method := c.Request.Method
		path := c.Request.URL.Path

		// Also captured up front. By the time the deferred call runs, the
		// timeout middleware downstream has already cancelled the context
		// it derived, and handing a cancelled context to a logger is a
		// trap for any handler implementation that respects it.
		ctx := c.Request.Context()

		// Deferred as a second line of defence, so a panic raised above
		// Recovery still produces an access log line.
		defer func() {
			status := c.Writer.Status()

			attrs := []any{
				"method", method,
				"path", path,
				"status", status,
				"duration_ms", time.Since(start).Milliseconds(),
				"client_ip", c.ClientIP(),
			}
			// Size reports -1 when nothing was written.
			if n := c.Writer.Size(); n > 0 {
				attrs = append(attrs, "bytes", n)
			}
			if id := requestIDOf(c); id != "" {
				attrs = append(attrs, "request_id", id)
			}
			if len(c.Errors) > 0 {
				attrs = append(attrs, "gin_errors", c.Errors.String())
			}

			log.Log(ctx, levelForStatus(status), "request", attrs...)
		}()

		c.Next()
	}
}

// levelForStatus keeps routine traffic at info while making failures
// stand out, so a log query for warnings and above surfaces the problems
// without a status filter.
func levelForStatus(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

// logFor returns the service logger tagged with this request's ID, so a
// message emitted deep inside a handler can be tied back to its access
// log line.
func (a *API) logFor(c *gin.Context) *slog.Logger {
	if id := requestIDOf(c); id != "" {
		return a.log.With("request_id", id)
	}
	return a.log
}
