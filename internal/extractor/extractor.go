// Package extractor provides simple HTML-to-text extraction using the
// standard library HTML tokenizer. It strips scripts and styles, extracts
// visible text, finds a lead image, and counts words.
package extractor

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
)

// Result holds the output of an extraction run.
type Result struct {
	Text         string
	WordCount    int
	LeadImageURL string
}

// Extract parses raw HTML and returns visible text with basic cleaning.
// Script and style contents are excluded. Text is whitespace-normalized.
func Extract(rawHTML []byte) (Result, error) {
	if len(rawHTML) == 0 {
		return Result{}, nil
	}

	doc, err := html.Parse(bytes.NewReader(rawHTML))
	if err != nil {
		return Result{}, err
	}

	var buf strings.Builder
	var leadImage string
	var foundLead bool

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			return // skip this entire subtree
		}

		// Lead image: first <img> with an http(s) src.
		if !foundLead && n.Type == html.ElementNode && n.Data == "img" {
			for _, a := range n.Attr {
				if a.Key == "src" && isHTTP(a.Val) {
					leadImage = a.Val
					foundLead = true
					break
				}
			}
		}

		// Text nodes.
		if n.Type == html.TextNode {
			t := strings.TrimSpace(n.Data)
			if t != "" {
				words := strings.Fields(t)
				for _, w := range words {
					if buf.Len() > 0 {
						buf.WriteByte(' ')
					}
					buf.WriteString(w)
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}

	walk(doc)

	text := buf.String()
	words := 0
	if text != "" {
		words = len(strings.Fields(text))
	}

	return Result{
		Text:         text,
		WordCount:    words,
		LeadImageURL: leadImage,
	}, nil
}

// isHTTP returns true if url starts with http:// or https://.
func isHTTP(url string) bool {
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")
}
