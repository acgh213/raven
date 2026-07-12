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

// ArticleListParams is the input for ListArticles pagination.
type ArticleListParams struct {
	FeedID string // optional filter
	Cursor string // base64-encoded pagination cursor
	Limit  int    // max results (1-100, default 20)
}

// ArticleSummary is a lightweight article view for the river endpoint.
type ArticleSummary struct {
	ID            string `json:"id"`
	FeedID        string `json:"feed_id"`
	GUID          string `json:"guid"`
	URL           string `json:"url"`
	Title         string `json:"title"`
	Author        string `json:"author"`
	PublishedAt   string `json:"published_at"`
	Excerpt       string `json:"excerpt"`
	ExtractedText string `json:"extracted_text"`
	IsRead        bool   `json:"is_read"`
	IsStarred     bool   `json:"is_starred"`
	WordCount     int    `json:"word_count"`
	LeadImageURL  string `json:"lead_image_url"`
	CreatedAt     string `json:"created_at"`
}

// ArticleListResult is the output of ListArticles.
type ArticleListResult struct {
	Articles   []ArticleSummary `json:"articles"`
	NextCursor string           `json:"next_cursor"`
}

// ArticleDetail is the full article view with extracted content.
type ArticleDetail struct {
	ID            string `json:"id"`
	FeedID        string `json:"feed_id"`
	GUID          string `json:"guid"`
	URL           string `json:"url"`
	Title         string `json:"title"`
	Author        string `json:"author"`
	PublishedAt   string `json:"published_at"`
	ExtractedText string `json:"extracted_text"`
	IsRead        bool   `json:"is_read"`
	IsStarred     bool   `json:"is_starred"`
	WordCount     int    `json:"word_count"`
	LeadImageURL  string `json:"lead_image_url"`
	CreatedAt     string `json:"created_at"`
}
type UpsertArticlesResult struct {
	New    []Article
	Exists int
}

// ContentVersion is a stored extraction run for an article.
type ContentVersion struct {
	ID                 string `json:"id"`
	ArticleID          string `json:"article_id"`
	ArticleURL         string `json:"article_url"`
	ExtractionStatus   string `json:"extraction_status"`
	ExtractionEngine   string `json:"extraction_engine"`
	ExtractionVersion  string `json:"extraction_version"`
	WordCount          int    `json:"word_count"`
	LeadImageURL       string `json:"lead_image_url"`
	ContentHash        string `json:"content_hash"`
	IsLatest           bool   `json:"is_latest"`
	CreatedAt          string `json:"created_at"`
}
