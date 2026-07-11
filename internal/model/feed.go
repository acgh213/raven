package model

// FeedCandidate is an unvalidated feed declaration read from an OPML document.
// The import service validates and canonicalizes URL before persistence.
type FeedCandidate struct {
	URL         string
	Title       string
	Description string
}
