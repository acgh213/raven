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
	ID        string `json:"id"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
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
