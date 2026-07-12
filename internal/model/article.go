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

// Article is a persisted article from a feed subscription.
type Article struct {
	ID                      string  `json:"id"`
	FeedID                  string  `json:"feed_id"`
	GUID                    string  `json:"guid"`
	URL                     string  `json:"url"`
	Title                   string  `json:"title"`
	Author                  string  `json:"author"`
	PublishedAt             string  `json:"published_at"`
	LatestContentVersionID  *string `json:"latest_content_version_id"`
	IsDeleted               bool    `json:"is_deleted"`
	CreatedAt               string  `json:"created_at"`
	UpdatedAt               string  `json:"updated_at"`
}

// UpsertArticlesResult separates newly created articles from those that
// already exist (by feed_id + guid).
type UpsertArticlesResult struct {
	New    []Article
	Exists int
}
