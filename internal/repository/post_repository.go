package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"go-http-service/internal/model"
)

// Errors this layer reports in terms a caller can act on. The service
// layer translates them into its own errors so that handlers never need
// to import this package — the same pattern as the user repository.
var (
	ErrSlugTaken    = errors.New("slug already taken")
	ErrPostNotFound = errors.New("post not found")
)

// Index name from 0003_create_posts.sql. Renaming the index there without
// changing this turns a precise 409 into a generic 500, exactly like the
// username/email indexes in the user repository.
const postSlugIndex = "posts_slug_key"

// PostRepository reads and writes the posts table.
type PostRepository struct {
	pool *pgxpool.Pool
}

// NewPostRepository wires the repository to a pool.
func NewPostRepository(pool *pgxpool.Pool) *PostRepository {
	return &PostRepository{pool: pool}
}

// Create inserts a Post and fills in the database-generated fields.
//
// It does NOT check whether the slug is free first: that is a
// time-of-check-to-time-of-use race, the same one the user repository
// avoids. Insert and translate the unique violation — the database
// evaluates the constraint atomically with the write.
//
// The caller sets Published; this method stores whatever it is given. The
// service layer guarantees a fresh Post is a Draft, so there is no
// create-and-Publish path through this method.
//
// ctx is passed straight through so a request deadline reaches the query.
// See CLAUDE.md 4.6.
func (r *PostRepository) Create(ctx context.Context, p *model.Post) error {
	const query = `
		INSERT INTO posts (slug, title, body, published)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at`

	err := r.pool.QueryRow(ctx, query, p.Slug, p.Title, p.Body, p.Published).
		Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return translatePostCreateError(err)
	}
	return nil
}

// FindBySlug looks a Post up by its slug. Used by both the Author JSON read
// and (later) the public HTML read, so it returns the row regardless of
// Draft/Published state — the caller decides whether to show it.
//
// Returns ErrPostNotFound rather than a nil Post, so a caller cannot
// dereference a miss by accident.
func (r *PostRepository) FindBySlug(ctx context.Context, slug string) (*model.Post, error) {
	const query = `
		SELECT id, slug, title, body, published, published_at, created_at, updated_at
		FROM posts
		WHERE slug = $1`

	var p model.Post
	err := r.pool.QueryRow(ctx, query, slug).Scan(
		&p.ID, &p.Slug, &p.Title, &p.Body, &p.Published, &p.PublishedAt,
		&p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPostNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find post by slug: %w", err)
	}
	return &p, nil
}

// ListAll returns every Post, including Drafts. The Author area needs the
// drafts; the public Home (issue #4) will filter to published and sort by
// first Publish time. Here the list is for the Author, so newest-created
// first is enough.
func (r *PostRepository) ListAll(ctx context.Context) ([]model.Post, error) {
	const query = `
		SELECT id, slug, title, body, published, published_at, created_at, updated_at
		FROM posts
		ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list posts: %w", err)
	}
	defer rows.Close()

	var out []model.Post
	for rows.Next() {
		var p model.Post
		if err := rows.Scan(
			&p.ID, &p.Slug, &p.Title, &p.Body, &p.Published, &p.PublishedAt,
			&p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan post: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list posts: %w", err)
	}
	return out, nil
}

// UpdateTitleBody changes the Title and Body of the Post identified by slug.
//
// The slug is NOT in the SET clause: a slug is immutable after creation
// (ADR-0006), and the service layer rejects a body that tries to change
// it. updated_at advances so a caller can tell a save happened. Last save
// wins; there is no version column (ADR-0015).
//
// Returns the updated row so the handler can echo it without a second
// query. ErrPostNotFound if no Post has that slug.
func (r *PostRepository) UpdateTitleBody(ctx context.Context, slug, title, body string) (*model.Post, error) {
	const query = `
		UPDATE posts
		SET title = $1, body = $2, updated_at = now()
		WHERE slug = $3
		RETURNING id, slug, title, body, published, published_at, created_at, updated_at`

	var p model.Post
	err := r.pool.QueryRow(ctx, query, title, body, slug).Scan(
		&p.ID, &p.Slug, &p.Title, &p.Body, &p.Published, &p.PublishedAt,
		&p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPostNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update post: %w", err)
	}
	return &p, nil
}

// Publish marks the Post published. published_at is set only on the first
// successful Publish (COALESCE): a second Publish is idempotent and keeps
// the original time, which is what Home sorts by after an Unpublish
// (ADR-0005). ErrPostNotFound if no row has that slug.
func (r *PostRepository) Publish(ctx context.Context, slug string, now time.Time) (*model.Post, error) {
	const query = `
		UPDATE posts
		SET published = true,
		    published_at = COALESCE(published_at, $2),
		    updated_at = now()
		WHERE slug = $1
		RETURNING id, slug, title, body, published, published_at, created_at, updated_at`

	var p model.Post
	err := r.pool.QueryRow(ctx, query, slug, now).Scan(
		&p.ID, &p.Slug, &p.Title, &p.Body, &p.Published, &p.PublishedAt,
		&p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPostNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("publish post: %w", err)
	}
	return &p, nil
}

// Unpublish returns a Published Post to a Draft. published_at is left
// alone so Home can still sort by first publication after a take-down.
// Unpublish of a Draft is idempotent: the row is found, published is
// already false, and the same row comes back. ErrPostNotFound if no row.
func (r *PostRepository) Unpublish(ctx context.Context, slug string) (*model.Post, error) {
	const query = `
		UPDATE posts
		SET published = false, updated_at = now()
		WHERE slug = $1
		RETURNING id, slug, title, body, published, published_at, created_at, updated_at`

	var p model.Post
	err := r.pool.QueryRow(ctx, query, slug).Scan(
		&p.ID, &p.Slug, &p.Title, &p.Body, &p.Published, &p.PublishedAt,
		&p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPostNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("unpublish post: %w", err)
	}
	return &p, nil
}

// ListPublished returns only Published Posts, newest first Publish time
// first. The public Home must never see a Draft.
func (r *PostRepository) ListPublished(ctx context.Context) ([]model.Post, error) {
	const query = `
		SELECT id, slug, title, body, published, published_at, created_at, updated_at
		FROM posts
		WHERE published = true
		ORDER BY published_at DESC`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list published posts: %w", err)
	}
	defer rows.Close()

	var out []model.Post
	for rows.Next() {
		var p model.Post
		if err := rows.Scan(
			&p.ID, &p.Slug, &p.Title, &p.Body, &p.Published, &p.PublishedAt,
			&p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan published post: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list published posts: %w", err)
	}
	return out, nil
}

// translatePostCreateError maps a driver error onto this layer's vocabulary.
// Only the slug can collide on insert; the other columns have no unique
// constraint.
func translatePostCreateError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgUniqueViolation {
		return fmt.Errorf("insert post: %w", err)
	}
	if pgErr.ConstraintName == postSlugIndex {
		return ErrSlugTaken
	}
	return fmt.Errorf("insert post: unexpected unique violation on %q: %w",
		pgErr.ConstraintName, err)
}
