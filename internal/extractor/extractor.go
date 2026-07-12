// Package extractor provides HTML-to-text extraction using go-readability,
// which handles boilerplate removal and main-content identification.
package extractor

import (
	"bytes"
	"net/url"
	"regexp"
	"strings"

	readability "codeberg.org/readeck/go-readability/v2"
)

var htmlTagRE = regexp.MustCompile(`<[^>]*>`)

// Result holds the output of an extraction run.
type Result struct {
	Title        string
	Text         string
	WordCount    int
	LeadImageURL string
}

// Extract parses raw HTML and returns cleaned, readable text. It uses
// go-readability to identify the main content and strip boilerplate.
func Extract(rawHTML []byte) (Result, error) {
	if len(rawHTML) == 0 {
		return Result{}, nil
	}

	article, err := readability.FromReader(bytes.NewReader(rawHTML), &url.URL{})
	if err != nil {
		return Result{}, err
	}

	var buf bytes.Buffer
	if err := article.RenderText(&buf); err != nil {
		return Result{}, err
	}

	text := strings.TrimSpace(buf.String())

	// Strip any residual inline HTML tags that go-readability may have left.
	text = htmlTagRE.ReplaceAllString(text, "")
	text = strings.TrimSpace(text)

	title := strings.TrimSpace(article.Title())

	words := 0
	if text != "" {
		words = len(strings.Fields(text))
	}

	leadImage := article.ImageURL()

	return Result{
		Title:        title,
		Text:         text,
		WordCount:    words,
		LeadImageURL: leadImage,
	}, nil
}
