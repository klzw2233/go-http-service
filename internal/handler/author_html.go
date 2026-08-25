package handler

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"

	"go-http-service/internal/markdown"
	"go-http-service/internal/model"
)

//go:embed templates/author.js
var authorJS []byte

//go:embed assets/site.css
var siteCSS []byte

//go:embed assets/theme.js
var themeJS []byte

type authorShellView struct {
	SiteName string
	Mode     string
}

// AuthorLogin is the HTML shell for signing in. Anyone may GET it; the
// form posts to JSON via JavaScript, not as a classic form POST.
func (a *API) AuthorLogin(c *gin.Context) {
	a.renderHTML(c, http.StatusOK, "author_login.tmpl", authorShellView{
		SiteName: model.SiteName,
	})
}

// AuthorPosts is the HTML shell for the Author's Post list. It does not
// embed Post Bodies; the page loads them over JSON after a token is present.
func (a *API) AuthorPosts(c *gin.Context) {
	a.renderHTML(c, http.StatusOK, "author_list.tmpl", authorShellView{
		SiteName: model.SiteName,
	})
}

// AuthorNew is the HTML shell for creating a Draft. Slug is writable.
func (a *API) AuthorNew(c *gin.Context) {
	a.renderHTML(c, http.StatusOK, "author_edit.tmpl", authorShellView{
		SiteName: model.SiteName,
		Mode:     "new",
	})
}

// AuthorEdit is the HTML shell for editing one Post. Slug is read-only
// in the page. The Body is not in this response.
func (a *API) AuthorEdit(c *gin.Context) {
	a.renderHTML(c, http.StatusOK, "author_edit.tmpl", authorShellView{
		SiteName: model.SiteName,
		Mode:     "edit",
	})
}

// AuthorJS serves the shared Author-area script.
func (a *API) AuthorJS(c *gin.Context) {
	c.Header("Content-Type", "text/javascript; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/javascript; charset=utf-8", authorJS)
}

// SiteCSS serves the shared stylesheet for public pages and the Author area.
func (a *API) SiteCSS(c *gin.Context) {
	c.Header("Content-Type", "text/css; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/css; charset=utf-8", siteCSS)
}

// ThemeJS serves the light/dark toggle. It is public: it only writes
// localStorage and a data-theme attribute, never tokens.
func (a *API) ThemeJS(c *gin.Context) {
	c.Header("Content-Type", "text/javascript; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/javascript; charset=utf-8", themeJS)
}

// PreviewPost renders Body as safe HTML for the in-editor Preview.
// It is not a Preview URL: the editor posts the current textarea.
func (a *API) PreviewPost(c *gin.Context) {
	var req model.PreviewPostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		a.respondBindError(c, err)
		return
	}
	c.JSON(http.StatusOK, model.PreviewPostResponse{
		HTML: markdown.Render(req.Body),
	})
}
