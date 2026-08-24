package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"go-http-service/internal/model"
	"go-http-service/internal/service"
)

// CreatePost handles POST /api/posts.
//
// Creating a Post always yields a Draft: the request has no `published`
// field, and the service forces it false. There is no create-and-Publish
// shortcut. The 201 response echoes the Post with its Draft state.
func (a *API) CreatePost(c *gin.Context) {
	var req model.CreatePostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		a.respondBindError(c, err)
		return
	}

	// c.Request.Context() carries the REQUEST_TIMEOUT deadline; it has to
	// reach the query or a slow one pins the goroutine. See CLAUDE.md 4.6.
	post, err := a.posts.CreatePost(c.Request.Context(), service.CreatePostInput{
		Title: req.Title,
		Slug:  req.Slug,
		Body:  req.Body,
	})
	if err != nil {
		a.respondPostError(c, err, "create post failed")
		return
	}

	c.JSON(http.StatusCreated, model.NewPostResponse(*post))
}

// ListPosts handles GET /api/posts, the Author's view including Drafts.
// Unauthenticated callers never reach this handler: requireAuth answers
// 401 (with WWW-Authenticate) before it runs, so there is no public JSON
// feed of Posts.
func (a *API) ListPosts(c *gin.Context) {
	posts, err := a.posts.ListPosts(c.Request.Context())
	if err != nil {
		a.respondPostError(c, err, "list posts failed")
		return
	}

	out := make([]model.PostResponse, 0, len(posts))
	for _, p := range posts {
		out = append(out, model.NewPostResponse(p))
	}
	c.JSON(http.StatusOK, out)
}

// GetPost handles GET /api/posts/:slug. A Draft is returned to the Author
// here; the public HTML read (issue #4) will filter Drafts to a 404. A
// missing slug is 404 with the existing NOT_FOUND code.
func (a *API) GetPost(c *gin.Context) {
	post, err := a.posts.GetPost(c.Request.Context(), c.Param("slug"))
	if err != nil {
		a.respondPostError(c, err, "get post failed")
		return
	}

	c.JSON(http.StatusOK, model.NewPostResponse(*post))
}

// UpdatePost handles PATCH /api/posts/:slug.
//
// Title and/or Body may be sent; absent fields keep their value (the
// service reads the current row). The slug is immutable: a body slug that
// disagrees with the path is a validation error. Last save wins.
func (a *API) UpdatePost(c *gin.Context) {
	var req model.UpdatePostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		a.respondBindError(c, err)
		return
	}

	post, err := a.posts.UpdatePost(c.Request.Context(), service.UpdatePostInput{
		Title:           req.Title,
		Body:            req.Body,
		SlugFromRequest: req.Slug,
		SlugFromPath:    c.Param("slug"),
	})
	if err != nil {
		a.respondPostError(c, err, "update post failed")
		return
	}

	c.JSON(http.StatusOK, model.NewPostResponse(*post))
}

// respondPostError maps a Post service error onto the HTTP contract.
//
// The raw error is logged but never returned: a database driver message
// may carry hostnames or constraint internals, and the wording drifts with
// dependency versions. Clients see stable codes only. See CLAUDE.md 4.4.
func (a *API) respondPostError(c *gin.Context, err error, logMsg string) {
	switch {
	case errors.Is(err, service.ErrSlugTaken):
		a.respondError(c, http.StatusConflict, model.ErrCodeConflict,
			"that slug is already taken")

	case errors.Is(err, service.ErrPostNotFound):
		a.respondError(c, http.StatusNotFound, model.ErrCodeNotFound,
			"no post with that slug")

	case errors.Is(err, service.ErrSlugInvalid):
		c.AbortWithStatusJSON(http.StatusBadRequest, model.ErrorResponse{
			Code:    model.ErrCodeValidationFailed,
			Message: "one or more fields failed validation",
			Fields: []model.FieldError{{
				Field:  "slug",
				Reason: "must match [a-z0-9]+(?:-[a-z0-9]+)* and cannot be changed",
			}},
		})

	case errors.Is(err, service.ErrTitleTooLong):
		c.AbortWithStatusJSON(http.StatusBadRequest, model.ErrorResponse{
			Code:    model.ErrCodeValidationFailed,
			Message: "one or more fields failed validation",
			Fields: []model.FieldError{{
				Field:  "title",
				Reason: "must be at most 120 characters",
			}},
		})

	case errors.Is(err, service.ErrBodyTooLarge):
		c.AbortWithStatusJSON(http.StatusBadRequest, model.ErrorResponse{
			Code:    model.ErrCodeValidationFailed,
			Message: "one or more fields failed validation",
			Fields: []model.FieldError{{
				Field:  "body",
				Reason: "must be at most 65536 bytes",
			}},
		})

	default:
		a.logFor(c).Error(logMsg,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"error", err)
		a.respondError(c, http.StatusInternalServerError, model.ErrCodeInternal,
			"the server failed to process this request")
	}
}
