package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-http-service/internal/model"
	"go-http-service/internal/repository"
)

// fakePostStore is an in-memory postStore. It is not safe to share across
// parallel subtests that write the same slug, but each test here builds
// its own, and the mutex keeps the map honest under -race.
type fakePostStore struct {
	mu        sync.Mutex
	rows      map[string]*model.Post
	createErr error
	nextID    int64
	lastCtx   context.Context
}

func newFakePostStore() *fakePostStore {
	return &fakePostStore{rows: map[string]*model.Post{}}
}

func (s *fakePostStore) Create(ctx context.Context, p *model.Post) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastCtx = ctx
	if s.createErr != nil {
		return s.createErr
	}
	if _, exists := s.rows[p.Slug]; exists {
		return repository.ErrSlugTaken
	}
	s.nextID++
	p.ID = s.nextID
	copy := *p
	s.rows[p.Slug] = &copy
	return nil
}

func (s *fakePostStore) FindBySlug(ctx context.Context, slug string) (*model.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.rows[slug]
	if !ok {
		return nil, repository.ErrPostNotFound
	}
	c := *p
	return &c, nil
}

func (s *fakePostStore) ListAll(ctx context.Context) ([]model.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.Post, 0, len(s.rows))
	for _, p := range s.rows {
		out = append(out, *p)
	}
	return out, nil
}

func (s *fakePostStore) UpdateTitleBody(ctx context.Context, slug, title, body string) (*model.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.rows[slug]
	if !ok {
		return nil, repository.ErrPostNotFound
	}
	p.Title = title
	p.Body = body
	c := *p
	return &c, nil
}

func validCreateInput() CreatePostInput {
	return CreatePostInput{
		Title: "Hello",
		Slug:  "hello-world",
		Body:  "# Hello",
	}
}

// TestCreatePost_AlwaysDraft is the core #3 rule: there is no way through
// CreatePost that produces a Published Post, no matter the input.
func TestCreatePost_AlwaysDraft(t *testing.T) {
	t.Parallel()

	store := newFakePostStore()
	post, err := NewPostService(store).CreatePost(t.Context(), validCreateInput())
	require.NoError(t, err)

	assert.False(t, post.Published, "新 Post 必须是 Draft")
	assert.Equal(t, "hello-world", post.Slug)
	assert.Nil(t, post.PublishedAt, "#3 不写 published_at")
}

func TestCreatePost_RejectsInvalidSlug(t *testing.T) {
	t.Parallel()

	tests := []string{
		"",          // required
		"Hello",     // uppercase
		"has space", // space
		"-leading",  // leading hyphen
		"trailing-", // trailing hyphen
		"double--dash",
		"under_score",
		"中文字",
	}

	for _, slug := range tests {
		t.Run(slug, func(t *testing.T) {
			t.Parallel()

			store := newFakePostStore()
			in := validCreateInput()
			in.Slug = slug

			_, err := NewPostService(store).CreatePost(t.Context(), in)

			require.ErrorIs(t, err, ErrSlugInvalid)
		})
	}
}

func TestCreatePost_AcceptsValidSlugs(t *testing.T) {
	t.Parallel()

	for _, slug := range []string{"a", "hello", "hello-world", "a-b-c-1-2-3"} {
		t.Run(slug, func(t *testing.T) {
			t.Parallel()

			store := newFakePostStore()
			in := validCreateInput()
			in.Slug = slug

			_, err := NewPostService(store).CreatePost(t.Context(), in)
			require.NoError(t, err)
		})
	}
}

func TestCreatePost_TranslatesSlugTaken(t *testing.T) {
	t.Parallel()

	store := newFakePostStore()
	store.createErr = repository.ErrSlugTaken

	_, err := NewPostService(store).CreatePost(t.Context(), validCreateInput())

	require.ErrorIs(t, err, ErrSlugTaken)
	assert.NotErrorIs(t, err, repository.ErrSlugTaken,
		"handler 只应看到 service 的错误，不应看到 repository 的")
}

func TestCreatePost_WrapsUnknownError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("connection reset")
	store := newFakePostStore()
	store.createErr = sentinel

	_, err := NewPostService(store).CreatePost(t.Context(), validCreateInput())

	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
	assert.NotErrorIs(t, err, ErrSlugTaken)
}

// TestCreatePost_RejectsTitleOverRuneLimit is the rune-based cap: Title is
// display text, so the limit is fair to non-ASCII. A 121-rune string of
// ASCII is 121 bytes and still over.
func TestCreatePost_RejectsTitleOverRuneLimit(t *testing.T) {
	t.Parallel()

	store := newFakePostStore()
	in := validCreateInput()
	in.Title = strings.Repeat("a", model.MaxTitleRunes+1)

	_, err := NewPostService(store).CreatePost(t.Context(), in)

	require.ErrorIs(t, err, ErrTitleTooLong)
}

func TestCreatePost_AcceptsTitleAtRuneLimit(t *testing.T) {
	t.Parallel()

	store := newFakePostStore()
	in := validCreateInput()
	in.Title = strings.Repeat("字", model.MaxTitleRunes) // runes, more bytes

	_, err := NewPostService(store).CreatePost(t.Context(), in)

	assert.NoError(t, err, "按 rune 计恰好 120 应当接受")
}

// TestCreatePost_RejectsBodyOverByteLimit is the byte-based cap, the same
// trap as MaxPasswordBytes: a binding tag counts runes, so a CJK body of
// MaxBodyBytes runes is 3x the bytes and would slip through a tag.
func TestCreatePost_RejectsBodyOverByteLimit(t *testing.T) {
	t.Parallel()

	store := newFakePostStore()
	in := validCreateInput()
	in.Body = strings.Repeat("密", model.MaxBodyBytes) // bytes, way over

	require.Greater(t, len(in.Body), model.MaxBodyBytes)
	_, err := NewPostService(store).CreatePost(t.Context(), in)

	require.ErrorIs(t, err, ErrBodyTooLarge)
}

func TestCreatePost_AcceptsBodyAtByteLimit(t *testing.T) {
	t.Parallel()

	store := newFakePostStore()
	in := validCreateInput()
	in.Body = strings.Repeat("a", model.MaxBodyBytes)

	_, err := NewPostService(store).CreatePost(t.Context(), in)

	assert.NoError(t, err, "恰好 64 KiB 字节应当接受")
}

func TestCreatePost_PassesContextThrough(t *testing.T) {
	t.Parallel()

	type key struct{}
	ctx := context.WithValue(t.Context(), key{}, "marker")
	store := newFakePostStore()

	_, err := NewPostService(store).CreatePost(ctx, validCreateInput())
	require.NoError(t, err)

	require.NotNil(t, store.lastCtx)
	assert.Equal(t, "marker", store.lastCtx.Value(key{}))
}

func TestGetPost_TranslatesNotFound(t *testing.T) {
	t.Parallel()

	store := newFakePostStore()

	_, err := NewPostService(store).GetPost(t.Context(), "missing")

	require.ErrorIs(t, err, ErrPostNotFound)
	assert.NotErrorIs(t, err, repository.ErrPostNotFound)
}

func TestGetPost_ReturnsDraftOrPublished(t *testing.T) {
	t.Parallel()

	store := newFakePostStore()
	in := validCreateInput()
	created, err := NewPostService(store).CreatePost(t.Context(), in)
	require.NoError(t, err)

	got, err := NewPostService(store).GetPost(t.Context(), created.Slug)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
	assert.False(t, got.Published)
}

func TestListPosts_IncludesDrafts(t *testing.T) {
	t.Parallel()

	store := newFakePostStore()
	svc := NewPostService(store)

	_, err := svc.CreatePost(t.Context(), validCreateInput())
	require.NoError(t, err)

	got, err := svc.ListPosts(t.Context())
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.False(t, got[0].Published, "列表应包含 Draft")
}

// TestUpdatePost_RejectsSlugChange is the immutability rule (ADR-0006):
// a body slug that disagrees with the path is a validation error, and it
// is rejected before any read or write, so the caller learns the slug is
// fixed rather than that the Post was not found.
func TestUpdatePost_RejectsSlugChange(t *testing.T) {
	t.Parallel()

	store := newFakePostStore()
	svc := NewPostService(store)
	created, err := svc.CreatePost(t.Context(), validCreateInput())
	require.NoError(t, err)

	title := "New"
	_, err = svc.UpdatePost(t.Context(), UpdatePostInput{
		Title:           &title,
		SlugFromRequest: "different-slug",
		SlugFromPath:    created.Slug,
	})

	require.ErrorIs(t, err, ErrSlugInvalid)
}

func TestUpdatePost_AllowsOmittedSlug(t *testing.T) {
	t.Parallel()

	store := newFakePostStore()
	svc := NewPostService(store)
	created, err := svc.CreatePost(t.Context(), validCreateInput())
	require.NoError(t, err)

	title := "New Title"
	updated, err := svc.UpdatePost(t.Context(), UpdatePostInput{
		Title:        &title,
		SlugFromPath: created.Slug,
	})
	require.NoError(t, err)

	assert.Equal(t, "New Title", updated.Title)
	assert.Equal(t, created.Body, updated.Body, "未提供的字段应保持原值")
	assert.Equal(t, created.Slug, updated.Slug)
}

func TestUpdatePost_PartialUpdateKeepsOtherField(t *testing.T) {
	t.Parallel()

	store := newFakePostStore()
	svc := NewPostService(store)
	created, err := svc.CreatePost(t.Context(), validCreateInput())
	require.NoError(t, err)

	title := "Only Title Changed"
	updated, err := svc.UpdatePost(t.Context(), UpdatePostInput{
		Title:        &title,
		SlugFromPath: created.Slug,
	})
	require.NoError(t, err)

	assert.Equal(t, title, updated.Title)
	assert.Equal(t, created.Body, updated.Body)
}

// TestUpdatePost_EmptyStringClearsField proves the pointer distinction
// earns its keep: a present body of "" clears the body, while an absent
// body leaves it. Without pointers these would be indistinguishable.
func TestUpdatePost_EmptyStringClearsField(t *testing.T) {
	t.Parallel()

	store := newFakePostStore()
	svc := NewPostService(store)
	created, err := svc.CreatePost(t.Context(), validCreateInput())
	require.NoError(t, err)

	empty := ""
	updated, err := svc.UpdatePost(t.Context(), UpdatePostInput{
		Body:         &empty,
		SlugFromPath: created.Slug,
	})
	require.NoError(t, err)

	assert.Empty(t, updated.Body, "显式空串应清空字段")
	assert.Equal(t, created.Title, updated.Title)
}

func TestUpdatePost_NotFound(t *testing.T) {
	t.Parallel()

	store := newFakePostStore()
	title := "x"

	_, err := NewPostService(store).UpdatePost(t.Context(), UpdatePostInput{
		Title:        &title,
		SlugFromPath: "missing",
	})

	require.ErrorIs(t, err, ErrPostNotFound)
}

func TestUpdatePost_RejectsTooLongTitle(t *testing.T) {
	t.Parallel()

	store := newFakePostStore()
	svc := NewPostService(store)
	created, err := svc.CreatePost(t.Context(), validCreateInput())
	require.NoError(t, err)

	long := strings.Repeat("a", model.MaxTitleRunes+1)
	_, err = svc.UpdatePost(t.Context(), UpdatePostInput{
		Title:        &long,
		SlugFromPath: created.Slug,
	})

	require.ErrorIs(t, err, ErrTitleTooLong)
}

func TestUpdatePost_RejectsTooLargeBody(t *testing.T) {
	t.Parallel()

	store := newFakePostStore()
	svc := NewPostService(store)
	created, err := svc.CreatePost(t.Context(), validCreateInput())
	require.NoError(t, err)

	big := strings.Repeat("密", model.MaxBodyBytes)
	_, err = svc.UpdatePost(t.Context(), UpdatePostInput{
		Body:         &big,
		SlugFromPath: created.Slug,
	})

	require.ErrorIs(t, err, ErrBodyTooLarge)
}
