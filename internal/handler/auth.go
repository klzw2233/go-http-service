package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go-http-service/internal/model"
	"go-http-service/internal/service"
)

const (
	authHeader = "Authorization"
	// bearerPrefix is matched case-insensitively: RFC 7235 defines the
	// scheme token as case-insensitive, and clients do send "bearer".
	bearerPrefix = "bearer "
)

var userIDKey = &contextKey{"user-id"}

// Login handles POST /api/auth/login.
func (a *API) Login(c *gin.Context) {
	var req model.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		a.respondBindError(c, err)
		return
	}

	result, err := a.auth.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		a.respondAuthError(c, err, "login failed")
		return
	}

	c.JSON(http.StatusOK, model.NewTokenPair(result.AccessToken, result.ExpiresAt))
}

// Me handles GET /api/auth/me, the first endpoint behind requireAuth.
func (a *API) Me(c *gin.Context) {
	userID, ok := UserIDFrom(c.Request.Context())
	if !ok {
		// Unreachable behind requireAuth; a defensive 401 beats a panic
		// if the route is ever registered without the middleware.
		a.respondUnauthorized(c)
		return
	}

	user, err := a.auth.UserByID(c.Request.Context(), userID)
	if err != nil {
		a.respondAuthError(c, err, "resolving authenticated user failed")
		return
	}

	c.JSON(http.StatusOK, model.NewUserResponse(*user))
}

// requireAuth rejects requests without a usable access token and
// publishes the caller's id on the request context.
func (a *API) requireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := bearerToken(c.GetHeader(authHeader))
		if !ok {
			a.respondUnauthorized(c)
			return
		}

		userID, err := a.tokens.ParseAccess(token)
		if err != nil {
			a.respondUnauthorized(c)
			return
		}

		c.Request = c.Request.WithContext(
			context.WithValue(c.Request.Context(), userIDKey, userID))

		c.Next()
	}
}

// bearerToken extracts the credential from an Authorization header.
func bearerToken(header string) (string, bool) {
	if len(header) < len(bearerPrefix) ||
		!strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
		return "", false
	}

	token := strings.TrimSpace(header[len(bearerPrefix):])
	if token == "" {
		return "", false
	}
	return token, true
}

// UserIDFrom returns the authenticated caller's id, if any. Exported so
// future handlers can identify the caller without re-parsing the header.
func UserIDFrom(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(userIDKey).(int64)
	return id, ok
}

// respondUnauthorized answers every authentication failure identically.
//
// Missing header, malformed header, wrong scheme, expired token, forged
// signature and deleted account all produce this one response. Telling
// a caller which applies tells an attacker how close they are: "expired"
// confirms the signature verified, which is far more useful news than
// "malformed".
func (a *API) respondUnauthorized(c *gin.Context) {
	// RFC 7235 requires a challenge on a 401.
	c.Header("WWW-Authenticate", `Bearer realm="api"`)

	a.respondError(c, http.StatusUnauthorized, model.ErrCodeUnauthorized,
		"authentication required")
}

// respondAuthError maps a service failure onto the HTTP contract.
func (a *API) respondAuthError(c *gin.Context, err error, logMsg string) {
	if errors.Is(err, service.ErrInvalidCredentials) {
		a.respondUnauthorized(c)
		return
	}

	a.logFor(c).Error(logMsg,
		"method", c.Request.Method,
		"path", c.Request.URL.Path,
		"error", err)

	a.respondError(c, http.StatusInternalServerError, model.ErrCodeInternal,
		"the server failed to process this request")
}
