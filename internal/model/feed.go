package model

// FeedCandidate is an unvalidated feed declaration read from an OPML document.
// The import service validates and canonicalizes URL before persistence.
type FeedCandidate struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// FeedCandidateRejection explains why a single OPML candidate cannot be
// imported while preserving the rest of the document's valid candidates.
type FeedCandidateRejection struct {
	URL    string `json:"url"`
	Reason string `json:"reason"`
}

// Feed is a persisted subscription. URL is canonicalized before it reaches
// the store and is unique across the single Raven reader.
type Feed struct {
	ID                 string `json:"id"`
	URL                string `json:"url"`
	Title              string `json:"title"`
	SiteURL            string `json:"site_url"`
	ETag               string `json:"etag"`
	LastModified       string `json:"last_modified"`
	PollIntervalSecs   *int   `json:"poll_interval_seconds"`
	LastPolledAt       string `json:"last_polled_at"`
	LastPollError      string `json:"last_poll_error"`
	IsActive           bool   `json:"is_active"`
	ErrorCount         int    `json:"error_count"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
}

// FeedImportResult separates subscriptions created by an import from
// candidates that were already present in the same document or database.
type FeedImportResult struct {
	Created    []Feed
	Duplicates []FeedCandidate
}

// FeedImportPreview describes an import without modifying subscriptions.
type FeedImportPreview struct {
	New        []FeedCandidate
	Duplicates []FeedCandidate
}
