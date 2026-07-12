// Package handler provides job handlers for the Raven worker.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"raven/internal/model"
	"raven/internal/poller"
	"raven/internal/store"
)

// PollPayload is the JSON payload for poll_feed jobs.
type PollPayload struct {
	FeedID  string `json:"feed_id"`
	FeedURL string `json:"feed_url"`
}

// PendingCVLister finds content versions awaiting extraction.
type PendingCVLister interface {
	ListPendingForFeed(ctx context.Context, feedID string, limit int) ([]model.ContentVersion, error)
}

// PollHandler processes poll_feed jobs by fetching the feed, parsing entries,
// upserting new articles, enqueuing extraction jobs, and scheduling the next poll.
type PollHandler struct {
	poller   *poller.Poller
	jobs     *store.JobStore
	feedURLs map[string]string
	cvLister PendingCVLister
}

// NewPollHandler creates a PollHandler.
func NewPollHandler(p *poller.Poller, jobs *store.JobStore, feedURLs map[string]string, cvLister PendingCVLister) *PollHandler {
	return &PollHandler{
		poller:   p,
		jobs:     jobs,
		feedURLs: feedURLs,
		cvLister: cvLister,
	}
}

// Handle implements jobs.Handler for the poll_feed job kind.
func (h *PollHandler) Handle(ctx context.Context, job *model.Job) error {
	var payload PollPayload
	if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
		return fmt.Errorf("parse poll_feed payload: %w", err)
	}

	if payload.FeedID == "" {
		return fmt.Errorf("poll_feed payload missing feed_id")
	}

	feedURL := payload.FeedURL
	if feedURL == "" {
		if url, ok := h.feedURLs[payload.FeedID]; ok {
			feedURL = url
		} else {
			return fmt.Errorf("poll_feed: no feed_url for feed_id %q", payload.FeedID)
		}
	}

	result := h.poller.PollFeed(ctx, payload.FeedID, feedURL)

	if result.Error != nil {
		return fmt.Errorf("poll %q: %w", feedURL, result.Error)
	}

	// Enqueue fetch_article jobs for any new articles with pending content versions.
	if h.cvLister != nil && result.NewArticles > 0 {
		pending, listErr := h.cvLister.ListPendingForFeed(ctx, payload.FeedID, 100)
		if listErr != nil {
			// Non-fatal: poll itself succeeded.
			_ = listErr
		}
		for _, cv := range pending {
			extPayload, _ := json.Marshal(extractPayload{
				ArticleID:        cv.ArticleID,
				ContentVersionID: cv.ID,
				ArticleURL:       cv.ArticleURL,
			})
			dedupeKey := "fetch_article:" + cv.ID
			if _, err := h.jobs.Enqueue(ctx, "fetch_article", string(extPayload), dedupeKey); err != nil {
				// Non-fatal: one extraction job failing to enqueue shouldn't block the poll.
				_ = err
			}
		}
	}

	// Schedule the next poll for this feed.
	scheduledAt := result.NextPoll.Format(time.RFC3339Nano)
	nextPayload, _ := json.Marshal(PollPayload{FeedID: payload.FeedID, FeedURL: feedURL})
	dedupeKey := "poll_feed:" + payload.FeedID
	if _, err := h.jobs.EnqueueAt(ctx, "poll_feed", string(nextPayload), dedupeKey, scheduledAt); err != nil {
		return fmt.Errorf("schedule next poll for %q: %w", feedURL, err)
	}

	return nil
}
