package markdown

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRender_EscapesRawHTML(t *testing.T) {
	t.Parallel()

	got := Render(`hello <script>alert(1)</script> world`)
	assert.NotContains(t, got, "<script>")
	assert.NotContains(t, got, "<script")
	// goldmark omits raw HTML (comment placeholder) rather than
	// entity-escaping it. Either way the tag is not inserted live.
}

func TestRender_JavascriptLinkIsNotHref(t *testing.T) {
	t.Parallel()

	got := Render(`[click](javascript:alert(1))`)
	assert.NotContains(t, strings.ToLower(got), `href="javascript:`)
	assert.Contains(t, got, "click")
}

func TestRender_FileAndHTTPImagesHaveNoSrc(t *testing.T) {
	t.Parallel()

	file := Render(`![x](file:///etc/passwd)`)
	assert.NotContains(t, file, "<img")

	httpImg := Render(`![x](http://example.com/a.png)`)
	assert.NotContains(t, httpImg, `src="http://`)
}

func TestRender_HTTPSImageAndHTTPLinkWork(t *testing.T) {
	t.Parallel()

	got := Render(`[site](https://example.com) ![pic](https://cdn.example.com/a.png) [mail](mailto:a@b.com) [plain](http://example.com)`)
	require.Contains(t, got, `href="https://example.com"`)
	require.Contains(t, got, `href="http://example.com"`)
	require.Contains(t, got, `href="mailto:a@b.com"`)
	require.Contains(t, got, `src="https://cdn.example.com/a.png"`)
}

func TestRender_CommonMarkSubset(t *testing.T) {
	t.Parallel()

	src := "# Heading\n\n- item\n\n**bold** *em*\n\n> quote\n\n```\ncode\n```\n"
	got := Render(src)
	assert.Contains(t, got, "<h1>")
	assert.Contains(t, got, "<li>")
	assert.Contains(t, got, "<strong>")
	assert.Contains(t, got, "<em>")
	assert.Contains(t, got, "<blockquote>")
	assert.Contains(t, got, "<pre>")
	assert.Contains(t, got, "<code>")
}
