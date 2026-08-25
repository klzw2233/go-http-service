package model

import "time"

// Post limits. These live here so both the binding tags (which count runes)
// and the service layer (which counts bytes for the body cap) share one
// source of truth, the same way MaxPasswordBytes does.
const (
	// MaxTitleRunes caps the Title. Titles are display text, so the limit is
	// in runes to be fair to non-ASCII authors. A binding tag can enforce it
	// directly because rune counting is exactly what gin's validator does.
	MaxTitleRunes = 120

	// MaxBodyBytes caps the Body in BYTES, not runes. The body is stored and
	// rendered verbatim, so its real size is bytes. A binding tag counts
	// runes and would let a 64 KiB-bytes / 3-runes-per-byte CJK body through
	// a max=65536 tag. The service layer enforces this, like MaxPasswordBytes.
	MaxBodyBytes = 64 * 1024

	// SlugPattern is the shape a Slug must match: lowercase ASCII letters or
	// digits in hyphen-separated segments. Author-chosen, immutable, and the
	// public URL. Enforced in the service layer (regex) so the database
	// constraint only has to guard uniqueness, not shape.
	SlugPattern = `^[a-z0-9]+(?:-[a-z0-9]+)*$`

	// SiteName is the public blog title. Distinct from Name, which identifies
	// the Go service. Home uses this as <title> and <h1>; a Post page uses
	// "{Title} · {SiteName}".
	SiteName = "Personal Blog - klzw2233"
)

// Post is a blog Post row as stored in the database.
//
// It carries internal fields (Published, PublishedAt) and must not be
// serialised to a client directly: hand a PostResponse over instead. The
// struct has no json tags on purpose, so an accidental c.JSON(post) produces
// ugly exported field names rather than a clean leak.
type Post struct {
	ID          int64
	Slug        string
	Title       string
	Body        string
	Published   bool
	PublishedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CreatePostRequest is the body of POST /api/posts.
//
// Creating a Post always yields a Draft: there is no `published` field here,
// and no create-and-Publish shortcut. Publish is a separate act (issue #4).
type CreatePostRequest struct {
	Title string `json:"title" binding:"required"`
	Slug  string `json:"slug"  binding:"required"`
	Body  string `json:"body"  binding:"required"`
}

// UpdatePostRequest is the body of PATCH /api/posts/:slug.
//
// Title and Body are pointers so "absent" and "empty string" differ: a
// PATCH that sends only `{"title":"x"}` leaves the Body untouched, while
// `{"title":"x","body":""}` clears it. Slug is present only so the service
// can reject a mismatch with the path slug — it can never be changed
// (ADR-0006); a non-empty mismatch yields a validation error, not a silent
// ignore.
type UpdatePostRequest struct {
	Title *string `json:"title"`
	Body  *string `json:"body"`
	Slug  string  `json:"slug"`
}

// PreviewPostRequest is the body of POST /api/posts/preview.
type PreviewPostRequest struct {
	Body string `json:"body"`
}

// PreviewPostResponse is goldmark output for the in-editor Preview.
type PreviewPostResponse struct {
	HTML string `json:"html"`
}

// PostResponse is what a client receives for a Post. It exposes the Draft
// state as `draft` (the negation of the stored `published`) so the Author
// area can mark unfinished writing without inverting a bool in the caller.
type PostResponse struct {
	ID        int64     `json:"id"`
	Slug      string    `json:"slug"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Draft     bool      `json:"draft"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NewPostResponse projects a Post onto its public fields.
func NewPostResponse(p Post) PostResponse {
	return PostResponse{
		ID:        p.ID,
		Slug:      p.Slug,
		Title:     p.Title,
		Body:      p.Body,
		Draft:     !p.Published,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}
}
