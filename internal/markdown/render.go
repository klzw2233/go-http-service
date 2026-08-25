// Package markdown renders Post Body as a safe CommonMark subset.
//
// Raw HTML is never inserted (goldmark's default). Links are only
// http/https/mailto; images are only https://. Anything else is shown as
// text, not as an active href or src. GFM extras are not enabled.
package markdown

import (
	"bytes"
	"net/url"
	"strings"
	"sync"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var (
	once   sync.Once
	engine goldmark.Markdown
)

func rendererOnce() goldmark.Markdown {
	once.Do(func() {
		engine = goldmark.New(
			goldmark.WithParserOptions(
				parser.WithASTTransformers(
					util.Prioritized(&schemeFilter{}, 100),
				),
			),
		)
	})
	return engine
}

// Render converts CommonMark source to a safe HTML fragment.
func Render(src string) string {
	var buf bytes.Buffer
	if err := rendererOnce().Convert([]byte(src), &buf); err != nil {
		// goldmark.Convert only fails on writer errors; a bytes.Buffer
		// does not fail. Returning empty is safer than leaking source.
		return ""
	}
	return buf.String()
}

// schemeFilter rewrites the AST before HTML rendering so disallowed
// destinations never become href/src. Safer than fighting goldmark's
// default NodeRenderer registration order.
type schemeFilter struct{}

func (s *schemeFilter) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	var drop []ast.Node
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.Kind() {
		case ast.KindLink:
			link := n.(*ast.Link)
			if !allowedLink(string(link.Destination)) {
				drop = append(drop, n)
			}
		case ast.KindImage:
			img := n.(*ast.Image)
			if !allowedImage(string(img.Destination)) {
				drop = append(drop, n)
			}
		}
		return ast.WalkContinue, nil
	})
	for _, n := range drop {
		unwrap(n)
	}
}

// unwrap replaces n with its children so the visible text remains but
// the wrapping link/image is gone.
func unwrap(n ast.Node) {
	parent := n.Parent()
	if parent == nil {
		return
	}
	for c := n.FirstChild(); c != nil; {
		next := c.NextSibling()
		n.RemoveChild(n, c)
		parent.InsertBefore(parent, n, c)
		c = next
	}
	parent.RemoveChild(parent, n)
}

func allowedLink(dest string) bool {
	u, err := url.Parse(dest)
	if err != nil || u.Scheme == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto":
		return true
	default:
		return false
	}
}

func allowedImage(dest string) bool {
	u, err := url.Parse(dest)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "https")
}
