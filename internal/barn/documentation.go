package barn

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
)

var documentationMarkdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(html.WithUnsafe()),
)

func renderDocumentation(source []byte, documentationURL string) ([]byte, error) {
	baseURL, err := url.Parse(documentationURL)
	if err != nil {
		return nil, fmt.Errorf("parse documentation URL: %w", err)
	}

	document := documentationMarkdown.Parser().Parse(text.NewReader(source))
	if err := rewriteDocumentationLinks(document, baseURL); err != nil {
		return nil, err
	}

	var rendered bytes.Buffer
	if err := documentationMarkdown.Renderer().Render(&rendered, source, document); err != nil {
		return nil, fmt.Errorf("render Markdown: %w", err)
	}

	sanitized := bytes.TrimSpace(documentationSanitizer().SanitizeBytes(rendered.Bytes()))
	if len(sanitized) == 0 {
		return []byte{}, nil
	}

	return append(sanitized, '\n'), nil
}

func rewriteDocumentationLinks(document ast.Node, baseURL *url.URL) error {
	return ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch value := node.(type) {
		case *ast.Link:
			value.Destination = resolveDocumentationLink(value.Destination, baseURL)
		case *ast.Image:
			value.Destination = resolveDocumentationLink(value.Destination, baseURL)
		}

		return ast.WalkContinue, nil
	})
}

func resolveDocumentationLink(value []byte, baseURL *url.URL) []byte {
	referenceText := string(value)
	if referenceText == "" || strings.HasPrefix(referenceText, "#") {
		return value
	}

	reference, err := url.Parse(referenceText)
	if err != nil || reference.IsAbs() || reference.Host != "" {
		return value
	}

	return []byte(baseURL.ResolveReference(reference).String())
}

func documentationSanitizer() *bluemonday.Policy {
	policy := bluemonday.UGCPolicy()
	policy.AllowAttrs("id").OnElements("h1", "h2", "h3", "h4", "h5", "h6")
	policy.AllowAttrs("class").Matching(regexp.MustCompile(`^(?:language-[A-Za-z0-9_+.-]+|contains-task-list|task-list-item|task-list-item-checkbox)$`)).OnElements("code", "ul", "li", "input")
	policy.AllowElements("input")
	policy.AllowAttrs("type").Matching(regexp.MustCompile(`^checkbox$`)).OnElements("input")
	policy.AllowAttrs("checked", "disabled").OnElements("input")

	return policy
}
