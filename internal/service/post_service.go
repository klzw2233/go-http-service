package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"
	"unicode/utf8"

	"go-http-service/internal/model"
	"go-http-service/internal/repository"
)

// Post errors callers can act on. As with the user service, these mirror
// the repository's errors rather than re-exporting them, so handlers
// match on service errors and never import repository.
var (
	ErrSlugTaken    = errors.New("slug already taken")
	ErrPostNotFound = errors.New("post not found")

	// ErrSlugInvalid covers both a malformed slug at create time and an
	// attempt to change the slug at update time. The handler maps it to
	// a field-level validation error so the caller learns which field.
	ErrSlugInvalid  = errors.New("slug must match [a-z0-9]+(?:-[a-z0-9]+)* and cannot be changed")
	ErrTitleTooLong = errors.New("title exceeds the maximum length")
	ErrBodyTooLarge = errors.New("body exceeds the maximum length")
)

// slugRegexp is the shape a Slug must match. Compiled once; the pattern
// mirrors model.SlugPattern so the JSON contract and the service check
// cannot drift apart.
var slugRegexp = regexp.MustCompile(model.SlugPattern)

// postStore is the slice of the repository this service needs. Declared
// here rather than depending on the concrete type keeps the service
// testable without a database, the same way userStore does.
type postStore interface {
	Create(ctx context.Context, p *model.Post) error
	FindBySlug(ctx context.Context, slug string) (*model.Post, error)
	ListAll(ctx context.Context) ([]model.Post, error)
	UpdateTitleBody(ctx context.Context, slug, title, body string) (*model.Post, error)
	Publish(ctx context.Context, slug string, now time.Time) (*model.Post, error)
	Unpublish(ctx context.Context, slug string) (*model.Post, error)
	ListPublished(ctx context.Context) ([]model.Post, error)
}

// PostService implements Post operations.
type PostService struct {
	posts postStore
	now   func() time.Time
}

// PostOption customises a PostService.
type PostOption func(*PostService)

// WithPostClock replaces the time source so tests can pin published_at
// without sleeping. Production uses UTC now.
func WithPostClock(now func() time.Time) PostOption {
	return func(s *PostService) { s.now = now }
}

// NewPostService builds a service over the given store.
func NewPostService(posts postStore, opts ...PostOption) *PostService {
	s := &PostService{
		posts: posts,
		now:   func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// CreatePostInput carries the values needed to create a Post.
type CreatePostInput struct {
	Title string
	Slug  string
	Body  string
}

// CreatePost stores a new Post as a Draft. There is no way to Publish
// through this method: published is forced false, so a half-written Post
// can never reach the public web by accident.
//
// The returned Post still carries internal fields; project it through
// model.NewPostResponse before it reaches a client.
func (s *PostService) CreatePost(ctx context.Context, in CreatePostInput) (*model.Post, error) {
	if err := validateSlug(in.Slug); err != nil {
		return nil, err
	}
	if err := validateTitle(in.Title); err != nil {
		return nil, err
	}
	if err := validateBody(in.Body); err != nil {
		return nil, err
	}

	post := &model.Post{
		Slug:      in.Slug,
		Title:     in.Title,
		Body:      in.Body,
		Published: false,
	}

	if err := s.posts.Create(ctx, post); err != nil {
		return nil, translatePostCreateError(err)
	}
	return post, nil
}

// GetPost returns one Post by slug, Draft or Published. The Author area
// reads drafts this way; the public read (issue #4) will filter on
// Published before rendering. ErrPostNotFound surfaces as 404.
func (s *PostService) GetPost(ctx context.Context, slug string) (*model.Post, error) {
	post, err := s.posts.FindBySlug(ctx, slug)
	if err != nil {
		return nil, translatePostError(err)
	}
	return post, nil
}

// ListPosts returns every Post including Drafts, for the Author area.
func (s *PostService) ListPosts(ctx context.Context) ([]model.Post, error) {
	posts, err := s.posts.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("list posts: %w", err)
	}
	return posts, nil
}

// UpdatePostInput is the PATCH body decoded by the handler. Title and Body
// are pointers so absent (nil) means "leave unchanged"; the service only
// validates and writes the fields that are present.
type UpdatePostInput struct {
	Title *string
	Body  *string

	// SlugFromRequest is the slug field from the JSON body, if any. It is
	// only compared against the path slug to reject a change; it is never
	// written. Empty means the body omitted the field, which is fine.
	SlugFromRequest string
	SlugFromPath    string
}

// UpdatePost changes the Title and/or Body of the Post at pathSlug. The
// slug is immutable: a body slug that disagrees with the path is an
// ErrSlugInvalid, not a silent ignore. Last save wins.
func (s *PostService) UpdatePost(ctx context.Context, in UpdatePostInput) (*model.Post, error) {
	// Reject a slug change before any other work: comparing first means a
	// caller trying to rename never gets a title/body validation error
	// that would imply the Post was found and only the rename failed.
	if in.SlugFromRequest != "" && in.SlugFromRequest != in.SlugFromPath {
		return nil, ErrSlugInvalid
	}

	// Load the current row so absent fields keep their value. This is one
	// extra read, but it keeps the update honest: a PATCH of only title
	// must not clobber body, and we cannot express "leave this column" in
	// a single parameterised UPDATE without building the query dynamically.
	current, err := s.posts.FindBySlug(ctx, in.SlugFromPath)
	if err != nil {
		return nil, translatePostError(err)
	}

	title := current.Title
	if in.Title != nil {
		title = *in.Title
		if err := validateTitle(title); err != nil {
			return nil, err
		}
	}

	body := current.Body
	if in.Body != nil {
		body = *in.Body
		if err := validateBody(body); err != nil {
			return nil, err
		}
	}

	updated, err := s.posts.UpdateTitleBody(ctx, in.SlugFromPath, title, body)
	if err != nil {
		return nil, translatePostError(err)
	}
	return updated, nil
}

// PublishPost makes a Draft publicly readable. Publishing an already
// Published Post is idempotent: the first published_at is kept.
func (s *PostService) PublishPost(ctx context.Context, slug string) (*model.Post, error) {
	post, err := s.posts.Publish(ctx, slug, s.now())
	if err != nil {
		return nil, translatePostError(err)
	}
	return post, nil
}

// UnpublishPost returns a Published Post to a Draft without clearing
// published_at. Unpublishing a Draft is idempotent success.
func (s *PostService) UnpublishPost(ctx context.Context, slug string) (*model.Post, error) {
	post, err := s.posts.Unpublish(ctx, slug)
	if err != nil {
		return nil, translatePostError(err)
	}
	return post, nil
}

// ListPublishedPosts returns only Published Posts for the public Home.
func (s *PostService) ListPublishedPosts(ctx context.Context) ([]model.Post, error) {
	posts, err := s.posts.ListPublished(ctx)
	if err != nil {
		return nil, fmt.Errorf("list published posts: %w", err)
	}
	return posts, nil
}

// GetPublishedPost returns a Post only if it is currently Published.
// A Draft looks the same as a missing slug (ErrPostNotFound) so the
// public HTML 404 cannot leak whether a slug is reserved.
func (s *PostService) GetPublishedPost(ctx context.Context, slug string) (*model.Post, error) {
	post, err := s.posts.FindBySlug(ctx, slug)
	if err != nil {
		return nil, translatePostError(err)
	}
	if !post.Published {
		return nil, ErrPostNotFound
	}
	return post, nil
}

// validateSlug rejects a slug that is not the author-chosen ASCII shape.
// The database constraint only guards uniqueness; shape is a service rule.
func validateSlug(slug string) error {
	if !slugRegexp.MatchString(slug) {
		return ErrSlugInvalid
	}
	return nil
}

// validateTitle caps the Title in runes, matching the binding tag's unit.
func validateTitle(title string) error {
	if utf8.RuneCountInString(title) > model.MaxTitleRunes {
		return ErrTitleTooLong
	}
	return nil
}

// validateBody caps the Body in bytes, not runes. A binding tag counts
// runes and would let a 64 KiB-byte CJK body through a max tag, the same
// trap MaxPasswordBytes exists for.
func validateBody(body string) error {
	if len(body) > model.MaxBodyBytes {
		return ErrBodyTooLarge
	}
	return nil
}

func translatePostCreateError(err error) error {
	switch {
	case errors.Is(err, repository.ErrSlugTaken):
		return ErrSlugTaken
	default:
		return fmt.Errorf("create post: %w", err)
	}
}

func translatePostError(err error) error {
	switch {
	case errors.Is(err, repository.ErrPostNotFound):
		return ErrPostNotFound
	default:
		return fmt.Errorf("post: %w", err)
	}
}
