package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"go-http-service/internal/model"
	"go-http-service/internal/service"
)

// CreateUser handles POST /api/users and registers an account.
//
// Public registration is closed. Callers without a Bearer token get 403,
// not 401: this is a permission decision, not a prompt to sign in.
// A signed-in Author may still create a User (operator convenience after
// the first account exists). Anyone else is 403.
func (a *API) CreateUser(c *gin.Context) {
	var req model.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		a.respondBindError(c, err)
		return
	}

	// c.Request.Context() rather than context.Background(): this is the
	// deadline REQUEST_TIMEOUT installed, and it has to reach the query
	// or a slow database pins the goroutine. See CLAUDE.md 4.6.
	user, err := a.users.Register(c.Request.Context(), service.RegisterInput{
		Username: req.Username,
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		a.respondRegisterError(c, err)
		return
	}

	// NewUserResponse, not the User itself: the model carries a password
	// hash, and projecting it explicitly means leaking one would take a
	// deliberate edit rather than a forgotten struct tag.
	c.JSON(http.StatusCreated, model.NewUserResponse(*user))
}

// respondRegisterError maps service errors onto the HTTP contract.
func (a *API) respondRegisterError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrUsernameTaken):
		a.respondError(c, http.StatusConflict, model.ErrCodeConflict,
			"that username is already taken")

	case errors.Is(err, service.ErrEmailTaken):
		a.respondError(c, http.StatusConflict, model.ErrCodeConflict,
			"that email is already registered")

	case errors.Is(err, service.ErrPasswordTooLong):
		// Reported in bytes because that is the real constraint, and a
		// caller measuring characters would not understand a rejection
		// of what looks to them like a short password.
		c.AbortWithStatusJSON(http.StatusBadRequest, model.ErrorResponse{
			Code:    model.ErrCodeValidationFailed,
			Message: "one or more fields failed validation",
			Fields: []model.FieldError{{
				Field:  "password",
				Reason: "must be at most 72 bytes",
			}},
		})

	default:
		// Anything unrecognised is a server problem. The cause goes to
		// the log; the client is told nothing it could act on and
		// nothing that describes the internals.
		a.logFor(c).Error("user registration failed",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"error", err)

		a.respondError(c, http.StatusInternalServerError, model.ErrCodeInternal,
			"the server failed to process this request")
	}
}
