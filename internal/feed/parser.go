// Package feed parses feed interchange formats.
package feed

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"raven/internal/model"
)

// ParseOPML returns feed declarations from outlines within an OPML body,
// preserving document order. Category outlines without xmlUrl are intentionally
// ignored. URL validation and canonicalization happen at the import boundary
// before candidates are persisted.
func ParseOPML(data []byte) ([]model.FeedCandidate, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var feeds []model.FeedCandidate
	seenRoot := false
	rootClosed := false
	depth := 0
	bodyDepth := 0

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			if !seenRoot {
				return nil, fmt.Errorf("parse OPML: missing OPML root element")
			}
			if !rootClosed {
				return nil, fmt.Errorf("parse OPML: unclosed OPML root element")
			}
			return feeds, nil
		}
		if err != nil {
			return nil, fmt.Errorf("parse OPML: %w", err)
		}

		switch value := token.(type) {
		case xml.StartElement:
			if !seenRoot {
				seenRoot = true
				if value.Name.Local != "opml" {
					return nil, fmt.Errorf("parse OPML: expected OPML root element, got %q", value.Name.Local)
				}
				depth = 1
				continue
			}
			if rootClosed {
				return nil, fmt.Errorf("parse OPML: element %q appears after OPML root", value.Name.Local)
			}
			depth++
			if depth == 2 && value.Name.Local == "body" {
				bodyDepth = depth
			}
			if bodyDepth > 0 && value.Name.Local == "outline" {
				candidate := outlineCandidate(value)
				if candidate.URL != "" {
					feeds = append(feeds, candidate)
				}
			}
		case xml.EndElement:
			if value.Name.Local == "opml" && depth == 1 {
				rootClosed = true
			}
			if value.Name.Local == "body" && depth == bodyDepth {
				bodyDepth = 0
			}
			depth--
		case xml.CharData:
			if rootClosed && strings.TrimSpace(string(value)) != "" {
				return nil, fmt.Errorf("parse OPML: content appears after OPML root")
			}
		}
	}
}

func outlineCandidate(start xml.StartElement) model.FeedCandidate {
	candidate := model.FeedCandidate{}
	for _, attr := range start.Attr {
		switch attr.Name.Local {
		case "xmlUrl":
			candidate.URL = strings.TrimSpace(attr.Value)
		case "title":
			candidate.Title = strings.TrimSpace(attr.Value)
		case "text":
			if candidate.Title == "" {
				candidate.Title = strings.TrimSpace(attr.Value)
			}
		case "description":
			candidate.Description = strings.TrimSpace(attr.Value)
		}
	}
	return candidate
}
