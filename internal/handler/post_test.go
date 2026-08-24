package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-http-service/internal/model"
	"go-http-service/internal/service"
)

// stubPostService is a recording fake for the postService interface. Each
// method returns a canned result or error; tests inspect the captured
// inputs to assert the handler passed the right values through.
type stubPostService struct {
	createPost func(ctx context.Context, in service.CreatePostInput) (*model.Post, error)
	getPost    func(ctx context.Context, slug string) (*model.Post, error)
	listPosts  func(ctx context.Context) ([]model.Post, error)
	updatePost func(ctx context.Context, in service.UpdatePostInput) (*model.Post, error)

	createIn  service.CreatePostInput
	createCtx context.Context
	updateIn  service.UpdatePostInput
	updateCtx context.Context
	getSlug   string
	listCtx   context.Context
}

func (s *stubPostService) CreatePost(ctx context.Context, in service.CreatePostInput) (*model.Post, error) {
	s.createIn = in
	s.createCtx = ctx
	if s.createPost != nil {
		return s.createPost(ctx, in)
	}
	return &model.Post{
		ID: 1, Slug: in.Slug, Title: in.Title, Body: in.Body, Published: false,
		CreatedAt: fixedTime, UpdatedAt: fixedTime,
	}, nil
}

func (s *stubPostService) GetPost(ctx context.Context, slug string) (*model.Post, error) {
	s.getSlug = slug
	if s.getPost != nil {
		return s.getPost(ctx, slug)
	}
	return &model.Post{ID: 1, Slug: slug, Title: "T", Body: "B", Published: false,
		CreatedAt: fixedTime, UpdatedAt: fixedTime}, nil
}

func (s *stubPostService) ListPosts(ctx context.Context) ([]model.Post, error) {
	s.listCtx = ctx
	if s.listPosts != nil {
		return s.listPosts(ctx)
	}
	return []model.Post{{ID: 1, Slug: "a", Title: "A", Body: "B", Published: false,
		CreatedAt: fixedTime, UpdatedAt: fixedTime}}, nil
}

func (s *stubPostService) UpdatePost(ctx context.Context, in service.UpdatePostInput) (*model.Post, error) {
	s.updateIn = in
	s.updateCtx = ctx
	if s.updatePost != nil {
		return s.updatePost(ctx, in)
	}
	return &model.Post{ID: 1, Slug: in.SlugFromPath, Title: "T", Body: "B", Published: false,
		CreatedAt: fixedTime, UpdatedAt: fixedTime}, nil
}

// routerWithPosts wires the Author stubs plus a Post service, the
// configuration a successful Author write needs. Reuses #2's
// registeredUser/authorHeaders so the Author is "jimmy" matching testConfig.
func routerWithPosts(t *testing.T, posts postService) *gin.Engine {
	t.Helper()
	return SetupRouter(newTestAPI(
		WithPostService(posts),
		WithAuthService(&stubAuthenticator{user: registeredUser()}),
		WithTokenVerifier(&stubVerifier{userID: registeredUser().ID}),
	))
}

const validCreatePostBody = `{"title":"Hello","slug":"hello-world","body":"# Hello"}`

// --- CreatePost ---

func TestCreatePost_ReturnsDraft(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{}
	w := request{
		method: http.MethodPost, path: "/api/posts", body: validCreatePostBody,
		headers: authorHeaders(),
	}.doOn(t, routerWithPosts(t, stub))

	var got model.PostResponse
	requireJSONResponse(t, w, http.StatusCreated, &got)
	assert.Equal(t, int64(1), got.ID)
	assert.Equal(t, "hello-world", got.Slug)
	assert.True(t, got.Draft, "新建的 Post 必须是 Draft")
	assert.Equal(t, "hello-world", stub.createIn.Slug)
	assert.Equal(t, "# Hello", stub.createIn.Body)
}

func TestCreatePost_AnonymousIs401NotPublicFeed(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{}
	w := request{
		method: http.MethodPost, path: "/api/posts", body: validCreatePostBody,
	}.doOn(t, routerWithPosts(t, stub))

	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusUnauthorized, &got)
	assert.Equal(t, model.ErrCodeUnauthorized, got.Code)
	assert.Equal(t, `Bearer realm="api"`, w.Header().Get("WWW-Authenticate"))
	assert.Nil(t, stub.createCtx, "未认证不应到达 service")
}

func TestCreatePost_BadTokenIs401(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{}
	w := request{
		method: http.MethodPost, path: "/api/posts", body: validCreatePostBody,
		headers: authorHeaders(),
	}.doOn(t, SetupRouter(newTestAPI(
		WithPostService(stub),
		WithAuthService(&stubAuthenticator{user: registeredUser()}),
		WithTokenVerifier(&stubVerifier{err: assert.AnError}),
	)))

	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusUnauthorized, &got)
	assert.Equal(t, model.ErrCodeUnauthorized, got.Code)
}

func TestCreatePost_NonAuthorIs403(t *testing.T) {
	t.Parallel()

	other := registeredUser()
	other.Username = "notjimmy"
	stub := &stubPostService{}
	w := request{
		method: http.MethodPost, path: "/api/posts", body: validCreatePostBody,
		headers: authorHeaders(),
	}.doOn(t, SetupRouter(newTestAPI(
		WithPostService(stub),
		WithAuthService(&stubAuthenticator{user: other}),
		WithTokenVerifier(&stubVerifier{userID: other.ID}),
	)))

	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusForbidden, &got)
	assert.Equal(t, model.ErrCodeForbidden, got.Code)
	assert.Nil(t, stub.createCtx)
}

func TestCreatePost_SlugConflictIs409(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{createPost: func(ctx context.Context, in service.CreatePostInput) (*model.Post, error) {
		return nil, service.ErrSlugTaken
	}}
	w := request{
		method: http.MethodPost, path: "/api/posts", body: validCreatePostBody,
		headers: authorHeaders(),
	}.doOn(t, routerWithPosts(t, stub))

	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusConflict, &got)
	assert.Equal(t, model.ErrCodeConflict, got.Code)
}

func TestCreatePost_InvalidSlugIs400(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{createPost: func(ctx context.Context, in service.CreatePostInput) (*model.Post, error) {
		return nil, service.ErrSlugInvalid
	}}
	w := request{
		method: http.MethodPost, path: "/api/posts", body: validCreatePostBody,
		headers: authorHeaders(),
	}.doOn(t, routerWithPosts(t, stub))

	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusBadRequest, &got)
	assert.Equal(t, model.ErrCodeValidationFailed, got.Code)
	require.NotEmpty(t, got.Fields)
	assert.Equal(t, "slug", got.Fields[0].Field)
}

func TestCreatePost_BindingErrorIs400(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{}
	w := request{
		method: http.MethodPost, path: "/api/posts", body: `{}`,
		headers: authorHeaders(),
	}.doOn(t, routerWithPosts(t, stub))

	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusBadRequest, &got)
	assert.Equal(t, model.ErrCodeValidationFailed, got.Code)
	assert.Nil(t, stub.createCtx, "绑定失败不应到达 service")
}

func TestCreatePost_InternalErrorIsOpaque(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{createPost: func(ctx context.Context, in service.CreatePostInput) (*model.Post, error) {
		return nil, errors.New("connection reset at 10.0.0.5")
	}}
	w := request{
		method: http.MethodPost, path: "/api/posts", body: validCreatePostBody,
		headers: authorHeaders(),
	}.doOn(t, routerWithPosts(t, stub))

	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "10.0.0.5")
	assert.NotContains(t, w.Body.String(), "connection reset")
}

// --- ListPosts ---

func TestListPosts_ReturnsAllIncludingDrafts(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{listPosts: func(ctx context.Context) ([]model.Post, error) {
		return []model.Post{
			{ID: 1, Slug: "a", Title: "A", Body: "B", Published: false, CreatedAt: fixedTime, UpdatedAt: fixedTime},
			{ID: 2, Slug: "b", Title: "B", Body: "B", Published: true, CreatedAt: fixedTime, UpdatedAt: fixedTime},
		}, nil
	}}
	w := request{
		method: http.MethodGet, path: "/api/posts", headers: authorHeaders(),
	}.doOn(t, routerWithPosts(t, stub))

	var got []model.PostResponse
	requireJSONResponse(t, w, http.StatusOK, &got)
	require.Len(t, got, 2)
	assert.True(t, got[0].Draft, "列表应含 Draft")
	assert.False(t, got[1].Draft)
}

func TestListPosts_AnonymousIs401(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{}
	w := request{
		method: http.MethodGet, path: "/api/posts",
	}.doOn(t, routerWithPosts(t, stub))

	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusUnauthorized, &got)
	assert.Equal(t, `Bearer realm="api"`, w.Header().Get("WWW-Authenticate"))
	assert.Nil(t, stub.listCtx, "未认证的集合读取不应到达 service")
}

// --- GetPost ---

func TestGetPost_ReturnsDraft(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{getPost: func(ctx context.Context, slug string) (*model.Post, error) {
		return &model.Post{ID: 7, Slug: slug, Title: "T", Body: "B", Published: false,
			CreatedAt: fixedTime, UpdatedAt: fixedTime}, nil
	}}
	w := request{
		method: http.MethodGet, path: "/api/posts/hello", headers: authorHeaders(),
	}.doOn(t, routerWithPosts(t, stub))

	var got model.PostResponse
	requireJSONResponse(t, w, http.StatusOK, &got)
	assert.Equal(t, int64(7), got.ID)
	assert.True(t, got.Draft)
	assert.Equal(t, "hello", stub.getSlug)
}

func TestGetPost_NotFoundIs404(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{getPost: func(ctx context.Context, slug string) (*model.Post, error) {
		return nil, service.ErrPostNotFound
	}}
	w := request{
		method: http.MethodGet, path: "/api/posts/missing", headers: authorHeaders(),
	}.doOn(t, routerWithPosts(t, stub))

	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusNotFound, &got)
	assert.Equal(t, model.ErrCodeNotFound, got.Code)
}

func TestGetPost_AnonymousIs401Not404(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{getPost: func(ctx context.Context, slug string) (*model.Post, error) {
		return nil, service.ErrPostNotFound
	}}
	// An anonymous caller must get 401, not the 404 that would reveal whether
	// the slug exists. The stub would answer 404, but requireAuth runs first.
	w := request{
		method: http.MethodGet, path: "/api/posts/anything",
	}.doOn(t, routerWithPosts(t, stub))

	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusUnauthorized, &got)
	assert.Equal(t, model.ErrCodeUnauthorized, got.Code)
	assert.Empty(t, stub.getSlug, "未认证不应到达 service，因此不应知道 slug")
}

// --- UpdatePost ---

func TestUpdatePost_PartialUpdateKeepsFields(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{updatePost: func(ctx context.Context, in service.UpdatePostInput) (*model.Post, error) {
		title := "New Title"
		return &model.Post{ID: 1, Slug: in.SlugFromPath, Title: title, Body: "kept", Published: false,
			CreatedAt: fixedTime, UpdatedAt: fixedTime}, nil
	}}
	w := request{
		method: http.MethodPatch, path: "/api/posts/hello",
		body:    `{"title":"New Title"}`,
		headers: authorHeaders(),
	}.doOn(t, routerWithPosts(t, stub))

	var got model.PostResponse
	requireJSONResponse(t, w, http.StatusOK, &got)
	assert.Equal(t, "hello", stub.updateIn.SlugFromPath)
	assert.NotNil(t, stub.updateIn.Title)
	assert.Nil(t, stub.updateIn.Body, "未提供的 body 应为 nil")
}

func TestUpdatePost_EmptyStringIsAValue(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{}
	w := request{
		method: http.MethodPatch, path: "/api/posts/hello",
		body:    `{"body":""}`,
		headers: authorHeaders(),
	}.doOn(t, routerWithPosts(t, stub))

	require.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, stub.updateIn.Body)
	assert.Empty(t, *stub.updateIn.Body, "显式空串应传给 service，而非被当未提供")
	assert.Nil(t, stub.updateIn.Title)
}

func TestUpdatePost_SlugChangeIs400(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{updatePost: func(ctx context.Context, in service.UpdatePostInput) (*model.Post, error) {
		return nil, service.ErrSlugInvalid
	}}
	w := request{
		method: http.MethodPatch, path: "/api/posts/hello",
		body:    `{"slug":"other"}`,
		headers: authorHeaders(),
	}.doOn(t, routerWithPosts(t, stub))

	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusBadRequest, &got)
	assert.Equal(t, model.ErrCodeValidationFailed, got.Code)
	require.NotEmpty(t, got.Fields)
	assert.Equal(t, "slug", got.Fields[0].Field)
}

func TestUpdatePost_NotFoundIs404(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{updatePost: func(ctx context.Context, in service.UpdatePostInput) (*model.Post, error) {
		return nil, service.ErrPostNotFound
	}}
	w := request{
		method: http.MethodPatch, path: "/api/posts/missing",
		body:    `{"title":"x"}`,
		headers: authorHeaders(),
	}.doOn(t, routerWithPosts(t, stub))

	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusNotFound, &got)
	assert.Equal(t, model.ErrCodeNotFound, got.Code)
}

func TestUpdatePost_AnonymousIs401(t *testing.T) {
	t.Parallel()

	stub := &stubPostService{}
	w := request{
		method: http.MethodPatch, path: "/api/posts/hello",
		body: `{"title":"x"}`,
	}.doOn(t, routerWithPosts(t, stub))

	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusUnauthorized, &got)
	assert.Equal(t, model.ErrCodeUnauthorized, got.Code)
	assert.Nil(t, stub.updateCtx)
}

func TestUpdatePost_NonAuthorIs403(t *testing.T) {
	t.Parallel()

	other := registeredUser()
	other.Username = "notjimmy"
	stub := &stubPostService{}
	w := request{
		method: http.MethodPatch, path: "/api/posts/hello",
		body:    `{"title":"x"}`,
		headers: authorHeaders(),
	}.doOn(t, SetupRouter(newTestAPI(
		WithPostService(stub),
		WithAuthService(&stubAuthenticator{user: other}),
		WithTokenVerifier(&stubVerifier{userID: other.ID}),
	)))

	var got model.ErrorResponse
	requireJSONResponse(t, w, http.StatusForbidden, &got)
	assert.Equal(t, model.ErrCodeForbidden, got.Code)
	assert.Nil(t, stub.updateCtx)
}

// The global body-size guard (413) applies to Post routes the same way it
// applies to every route; that path is covered by middleware_test. The
// field-level byte limits on title/body live in the service and are
// covered by the service tests, so no extra 413 case is needed here.
