package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-http-service/internal/model"
	"go-http-service/internal/service"
)

func TestHome_EmptyState(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{listPublishedPosts: func(ctx context.Context) ([]model.Post, error) {
		return nil, nil
	}}
	w := request{method: http.MethodGet, path: "/"}.doOn(t, routerWithPosts(t, stub))

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	assert.Contains(t, w.Body.String(), model.SiteName)
	assert.Contains(t, w.Body.String(), "No posts yet.")
}

func TestHome_ListsPublishedOnly(t *testing.T) {
	t.Parallel()

	when := fixedTime
	stub := &stubPostService{listPublishedPosts: func(ctx context.Context) ([]model.Post, error) {
		return []model.Post{{
			ID: 1, Slug: "hello", Title: "Hello", Body: "secret draft body",
			Published: true, PublishedAt: &when, CreatedAt: fixedTime, UpdatedAt: fixedTime,
		}}, nil
	}}
	w := request{method: http.MethodGet, path: "/"}.doOn(t, routerWithPosts(t, stub))

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "Hello")
	assert.Contains(t, body, `/posts/hello`)
	assert.NotContains(t, body, "secret draft body")
}

func TestPostPage_RendersPublished(t *testing.T) {
	t.Parallel()

	when := fixedTime
	stub := &stubPostService{getPublishedPost: func(ctx context.Context, slug string) (*model.Post, error) {
		return &model.Post{
			ID: 1, Slug: slug, Title: "Hello", Body: "# Hello\n\n[ok](https://example.com)",
			Published: true, PublishedAt: &when, CreatedAt: fixedTime, UpdatedAt: fixedTime,
		}, nil
	}}
	w := request{method: http.MethodGet, path: "/posts/hello"}.doOn(t, routerWithPosts(t, stub))

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "Hello · "+model.SiteName)
	assert.Contains(t, body, "<h1>Hello</h1>")
	assert.Contains(t, body, `href="https://example.com"`)
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func TestPostPage_DraftAndUnknownShareHTML404(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{getPublishedPost: func(ctx context.Context, slug string) (*model.Post, error) {
		return nil, service.ErrPostNotFound
	}}
	draft := request{method: http.MethodGet, path: "/posts/a-draft"}.doOn(t, routerWithPosts(t, stub))
	unknown := request{method: http.MethodGet, path: "/posts/nope"}.doOn(t, routerWithPosts(t, stub))

	require.Equal(t, http.StatusNotFound, draft.Code)
	require.Equal(t, http.StatusNotFound, unknown.Code)
	assert.Equal(t, draft.Body.String(), unknown.Body.String())
	assert.Contains(t, draft.Body.String(), "Not found")
	assert.NotContains(t, draft.Body.String(), `"code":"NOT_FOUND"`)
}

func TestPublishPost_JSON(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{}
	w := request{
		method: http.MethodPost, path: "/api/posts/hello/publish",
		headers: authorHeaders(),
	}.doOn(t, routerWithPosts(t, stub))

	var got model.PostResponse
	requireJSONResponse(t, w, http.StatusOK, &got)
	assert.False(t, got.Draft)
	assert.Equal(t, "hello", got.Slug)
}

func TestUnpublishPost_JSON(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{}
	w := request{
		method: http.MethodPost, path: "/api/posts/hello/unpublish",
		headers: authorHeaders(),
	}.doOn(t, routerWithPosts(t, stub))

	var got model.PostResponse
	requireJSONResponse(t, w, http.StatusOK, &got)
	assert.True(t, got.Draft)
}

func TestPublishPost_AnonymousIs401(t *testing.T) {
	t.Parallel()

	w := request{method: http.MethodPost, path: "/api/posts/hello/publish"}.
		doOn(t, routerWithPosts(t, &stubPostService{}))

	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusUnauthorized, &got)
	assert.Equal(t, model.ErrCodeUnauthorized, got.Code)
}
