package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go-http-service/internal/model"
)

// forbidAnonymous rejects a request that carries no Bearer token with
// 403. Used on POST /api/users so closed registration is a permission
// decision, not a prompt to sign in. A presented token is left for
// requireAuth, so a bad credential is still 401.
func (a *API) forbidAnonymous() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := bearerToken(c.GetHeader(authHeader)); !ok {
			a.respondForbidden(c)
			return
		}
		c.Next()
	}
}

// requireAuthor rejects a signed-in User who is not the configured
// Author. It must sit behind requireAuth. Comparison is case-insensitive
// to match the unique index on lower(username).
func (a *API) requireAuthor() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := UserIDFrom(c.Request.Context())
		if !ok {
			a.respondUnauthorized(c)
			return
		}

		user, err := a.auth.UserByID(c.Request.Context(), userID)
		if err != nil {
			a.respondAuthError(c, err, "resolving author failed")
			return
		}

		if !strings.EqualFold(user.Username, a.cfg.AuthorUsername) {
			a.respondForbidden(c)
			return
		}

		c.Next()
	}
}

// respondForbidden answers a signed-in caller who is not allowed to do
// this. No WWW-Authenticate: the credentials were usable.
func (a *API) respondForbidden(c *gin.Context) {
	a.respondError(c, http.StatusForbidden, model.ErrCodeForbidden,
		"you are not allowed to perform this action")
}
