package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-http-service/internal/model"
)

const secretDraftBody = "# secret draft body that must not leak into HTML shells"

func TestAuthorLogin_IsHTMLShellWithoutCookie(t *testing.T) {
	t.Parallel()

	w := request{method: http.MethodGet, path: "/author/login"}.
		doOn(t, routerWithPosts(t, &stubPostService{}))

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
	assert.Empty(t, w.Header().Get("Set-Cookie"))
	body := w.Body.String()
	assert.Contains(t, body, `id="login-form"`)
	assert.Contains(t, body, `/author/app.js`)
	assert.NotContains(t, body, secretDraftBody)
}

func TestAuthorPosts_ShellDoesNotEmbedBodies(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{listPosts: func(ctx context.Context) ([]model.Post, error) {
		return []model.Post{{
			ID: 1, Slug: "hello", Title: "Hello", Body: secretDraftBody,
			Published: false, CreatedAt: fixedTime, UpdatedAt: fixedTime,
		}}, nil
	}}
	w := request{method: http.MethodGet, path: "/author/posts"}.
		doOn(t, routerWithPosts(t, stub))

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `id="rows"`)
	assert.NotContains(t, body, secretDraftBody)
	assert.NotContains(t, body, "hello")
}

func TestAuthorEdit_ShellDoesNotEmbedBody(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{getPost: func(ctx context.Context, slug string) (*model.Post, error) {
		return &model.Post{
			ID: 1, Slug: slug, Title: "Hello", Body: secretDraftBody,
			Published: false, CreatedAt: fixedTime, UpdatedAt: fixedTime,
		}, nil
	}}
	w := request{method: http.MethodGet, path: "/author/posts/hello"}.
		doOn(t, routerWithPosts(t, stub))

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `id="edit-form"`)
	assert.Contains(t, body, `data-mode="edit"`)
	assert.NotContains(t, body, secretDraftBody)
}

func TestAuthorNew_Shell(t *testing.T) {
	t.Parallel()

	w := request{method: http.MethodGet, path: "/author/new"}.
		doOn(t, routerWithPosts(t, &stubPostService{}))

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `data-mode="new"`)
}

func TestSiteCSS_Served(t *testing.T) {
	t.Parallel()

	w := request{method: http.MethodGet, path: "/site.css"}.
		doOn(t, routerWithPosts(t, &stubPostService{}))

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/css")
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	body := w.Body.String()
	assert.Contains(t, body, "prefers-color-scheme")
	assert.Contains(t, body, "--bg")
	assert.NotContains(t, body, secretDraftBody)
}

func TestHome_LinksStylesheet(t *testing.T) {
	t.Parallel()

	w := request{method: http.MethodGet, path: "/"}.
		doOn(t, routerWithPosts(t, &stubPostService{}))

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `href="/site.css"`)
	assert.Contains(t, body, `name="viewport"`)
}

func TestAuthorJS_ContainsBearerUsage(t *testing.T) {
	t.Parallel()

	w := request{method: http.MethodGet, path: "/author/app.js"}.
		doOn(t, routerWithPosts(t, &stubPostService{}))

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "javascript")
	assert.Contains(t, w.Body.String(), "Authorization")
	assert.Contains(t, w.Body.String(), "sessionStorage")
	assert.Contains(t, w.Body.String(), "afterLoginPath")
	assert.NotContains(t, w.Body.String(), secretDraftBody)
}

func TestHome_FooterLinkToAuthorLogin(t *testing.T) {
	t.Parallel()

	w := request{method: http.MethodGet, path: "/"}.
		doOn(t, routerWithPosts(t, &stubPostService{}))

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `href="/author/posts"`)
	assert.Contains(t, body, "<h1>"+model.SiteName+"</h1>")
}

func TestPreviewPost_AuthorOK(t *testing.T) {
	t.Parallel()

	w := request{
		method:  http.MethodPost,
		path:    "/api/posts/preview",
		body:    `{"body":"hello <script>alert(1)</script>"}`,
		headers: authorHeaders(),
	}.doOn(t, routerWithPosts(t, &stubPostService{}))

	var got model.PreviewPostResponse
	requireJSONResponse(t, w, http.StatusOK, &got)
	assert.NotContains(t, got.HTML, "<script>")
}

func TestPreviewPost_AnonymousIs401(t *testing.T) {
	t.Parallel()

	w := request{
		method: http.MethodPost,
		path:   "/api/posts/preview",
		body:   `{"body":"# Hello"}`,
	}.doOn(t, routerWithPosts(t, &stubPostService{}))

	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusUnauthorized, &got)
	assert.Equal(t, model.ErrCodeUnauthorized, got.Code)
}
