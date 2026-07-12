package feed

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"raven/internal/model"
)

// rssDocument is the minimal RSS 2.0 structure needed for entry extraction.
type rssDocument struct {
	XMLName xml.Name   `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title string    `xml:"title"`
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
	GUID        string `xml:"guid"`
	Author      string `xml:"author"`
}

// ParseFeedEntries auto-detects RSS or Atom and returns feed-level metadata
// plus parsed individual entries.
func ParseFeedEntries(data []byte) ([]model.FeedEntry, string, error) {
	// Try RSS first — it has the simpler root-element check.
	var rss rssDocument
	if err := xml.Unmarshal(data, &rss); err == nil && rss.Channel.Title != "" || len(rss.Channel.Items) > 0 {
		entries, title, _ := parseRSS(rss)
		return entries, title, nil
	}

	// Try Atom.
	var atom atomFeed
	if err := xml.Unmarshal(data, &atom); err == nil && atom.XMLName.Local == "feed" {
		entries, title, _ := parseAtom(atom)
		return entries, title, nil
	}

	return nil, "", fmt.Errorf("unsupported or empty feed format")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func parseRSS(rss rssDocument) ([]model.FeedEntry, string, error) {
	if rss.Channel.Title == "" && len(rss.Channel.Items) == 0 {
		return nil, "", fmt.Errorf("feed contains no channel or items")
	}
	entries := make([]model.FeedEntry, 0, len(rss.Channel.Items))
	for _, item := range rss.Channel.Items {
		entry := model.FeedEntry{
			Title:   strings.TrimSpace(item.Title),
			URL:     firstNonEmpty(item.Link, item.GUID),
			Summary: strings.TrimSpace(item.Description),
			GUID:    firstNonEmpty(item.GUID, item.Link),
			Author:  strings.TrimSpace(item.Author),
		}
		if item.PubDate != "" {
			entry.PublishedAt = parseFlexibleTime(item.PubDate)
		}
		entries = append(entries, entry)
	}
	return entries, rss.Channel.Title, nil
}

// --- Atom types and parser ---

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Title   string      `xml:"title"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Title     string      `xml:"title"`
	Links     []atomLink  `xml:"link"`
	ID        string      `xml:"id"`
	Published string      `xml:"published"`
	Updated   string      `xml:"updated"`
	Summary   string      `xml:"summary"`
	Author    *atomAuthor `xml:"author"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

func parseAtom(feed atomFeed) ([]model.FeedEntry, string, error) {
	entries := make([]model.FeedEntry, 0, len(feed.Entries))
	for _, entry := range feed.Entries {
		e := model.FeedEntry{
			Title:   strings.TrimSpace(entry.Title),
			GUID:    strings.TrimSpace(entry.ID),
			Summary: strings.TrimSpace(entry.Summary),
			URL:     firstNonEmpty(atomLinkHref(entry.Links, "alternate"), atomLinkHref(entry.Links, ""), entry.ID),
		}
		if entry.Author != nil {
			e.Author = strings.TrimSpace(entry.Author.Name)
		}
		if raw := firstNonEmpty(entry.Published, entry.Updated); raw != "" {
			e.PublishedAt = parseFlexibleTime(raw)
		}
		entries = append(entries, e)
	}
	return entries, feed.Title, nil
}

func atomLinkHref(links []atomLink, rel string) string {
	for _, link := range links {
		if link.Href == "" {
			continue
		}
		if rel == "" || strings.EqualFold(link.Rel, rel) {
			return strings.TrimSpace(link.Href)
		}
	}
	return ""
}

func parseFlexibleTime(raw string) string {
	formats := []string{
		time.RFC1123Z, // Mon, 02 Jan 2006 15:04:05 -0700
		time.RFC1123,  // Mon, 02 Jan 2006 15:04:05 MST
		time.RFC3339,  // 2006-01-02T15:04:05Z07:00
		"2006-01-02T15:04:05Z",
		"2006-01-02",
	}
	for _, layout := range formats {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return raw
}
