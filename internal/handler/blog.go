package handler

import (
	"embed"
	"html/template"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"go-http-service/internal/markdown"
	"go-http-service/internal/model"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

var blogTemplates = template.Must(template.ParseFS(templateFS, "templates/*.tmpl"))

type homeView struct {
	SiteName string
	Posts    []homeRow
}

type homeRow struct {
	Slug        string
	Title       string
	PublishedAt time.Time
}

type postView struct {
	SiteName string
	Title    string
	BodyHTML template.HTML
}

type notFoundView struct {
	SiteName string
}

// Home handles GET /. Visitors see Published Posts only, newest first
// Publish time. Empty state is a short sentence, not an error.
func (a *API) Home(c *gin.Context) {
	if a.posts == nil {
		a.renderHTML(c, http.StatusOK, "home.tmpl", homeView{SiteName: model.SiteName})
		return
	}
	posts, err := a.posts.ListPublishedPosts(c.Request.Context())
	if err != nil {
		a.logFor(c).Error("list published posts failed",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"error", err)
		a.respondHTMLNotFound(c)
		return
	}

	rows := make([]homeRow, 0, len(posts))
	for _, p := range posts {
		row := homeRow{Slug: p.Slug, Title: p.Title}
		if p.PublishedAt != nil {
			row.PublishedAt = *p.PublishedAt
		}
		rows = append(rows, row)
	}

	a.renderHTML(c, http.StatusOK, "home.tmpl", homeView{
		SiteName: model.SiteName,
		Posts:    rows,
	})
}

// PostPage handles GET /posts/:slug. A Published Post is rendered as
// HTML. A Draft and an unknown slug share the same site-styled 404 so
// guessing URLs cannot discover unpublished writing.
func (a *API) PostPage(c *gin.Context) {
	if a.posts == nil {
		a.respondHTMLNotFound(c)
		return
	}
	post, err := a.posts.GetPublishedPost(c.Request.Context(), c.Param("slug"))
	if err != nil {
		a.respondHTMLNotFound(c)
		return
	}

	a.renderHTML(c, http.StatusOK, "post.tmpl", postView{
		SiteName: model.SiteName,
		Title:    post.Title,
		// BodyHTML is goldmark output only. html/template will not escape
		// template.HTML; never assign the raw Body here.
		BodyHTML: template.HTML(markdown.Render(post.Body)),
	})
}

// PublishPost handles POST /api/posts/:slug/publish.
func (a *API) PublishPost(c *gin.Context) {
	post, err := a.posts.PublishPost(c.Request.Context(), c.Param("slug"))
	if err != nil {
		a.respondPostError(c, err, "publish post failed")
		return
	}
	c.JSON(http.StatusOK, model.NewPostResponse(*post))
}

// UnpublishPost handles POST /api/posts/:slug/unpublish.
func (a *API) UnpublishPost(c *gin.Context) {
	post, err := a.posts.UnpublishPost(c.Request.Context(), c.Param("slug"))
	if err != nil {
		a.respondPostError(c, err, "unpublish post failed")
		return
	}
	c.JSON(http.StatusOK, model.NewPostResponse(*post))
}

func (a *API) respondHTMLNotFound(c *gin.Context) {
	a.renderHTML(c, http.StatusNotFound, "notfound.tmpl", notFoundView{
		SiteName: model.SiteName,
	})
}

func (a *API) renderHTML(c *gin.Context, status int, name string, data any) {
	c.Status(status)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := blogTemplates.ExecuteTemplate(c.Writer, name, data); err != nil {
		a.logFor(c).Error("html template failed",
			"template", name,
			"error", err)
	}
}
