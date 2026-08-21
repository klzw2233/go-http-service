package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// maxBodyBytes caps how large a request body may be. Without a cap,
// ShouldBindJSON reads the whole body into memory before decoding it, so
// a single large upload can exhaust the process.
const maxBodyBytes = 1 << 20 // 1 MiB

// limitBodySize wraps the request body so reads past limit fail with
// *http.MaxBytesError instead of allocating without bound. Applied
// router-wide so endpoints added later inherit the protection.
func limitBodySize(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}
