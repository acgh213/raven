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

// ParseOPML returns feed declarations from every OPML outline with an xmlUrl
// attribute, preserving document order. Category outlines without xmlUrl are
// intentionally ignored. URL validation and canonicalization happen at the
// import boundary before candidates are persisted.
func ParseOPML(data []byte) ([]model.FeedCandidate, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var feeds []model.FeedCandidate
	seenRoot := false

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			if !seenRoot {
				return nil, fmt.Errorf("parse OPML: missing OPML root element")
			}
			return feeds, nil
		}
		if err != nil {
			return nil, fmt.Errorf("parse OPML: %w", err)
		}

		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if !seenRoot {
			seenRoot = true
			if start.Name.Local != "opml" {
				return nil, fmt.Errorf("parse OPML: expected OPML root element, got %q", start.Name.Local)
			}
			continue
		}
		if start.Name.Local != "outline" {
			continue
		}

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
		if candidate.URL != "" {
			feeds = append(feeds, candidate)
		}
	}
}
