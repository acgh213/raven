package model

// FeedEntry is a single article or post extracted from a feed.
type FeedEntry struct {
	GUID        string
	Title       string
	URL         string
	Author      string
	Summary     string
	PublishedAt string
	UpdatedAt   string
}
