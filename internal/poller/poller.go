// Package poller fetches subscribed feeds, parses entries, and enqueues
// downstream article fetch/extract jobs.
package poller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"raven/internal/feed"
	"raven/internal/model"
)

const (
	minPollInterval     = 4 * time.Hour
	maxPollInterval     = 24 * time.Hour
	defaultPollInterval = 12 * time.Hour
)

// Fetcher abstracts HTTP retrieval so tests can inject fixtures.
type Fetcher interface {
	Fetch(url string) (*http.Response, error)
}

// FeedStore is the subset of store.FeedStore needed by the poller.
type FeedStore interface {
	UpdatePollResult(ctx context.Context, feedID, etag, lastModified, errMsg string) error
}

// ArticleStore is the subset of store.ArticleStore needed by the poller.
type ArticleStore interface {
	UpsertArticles(ctx context.Context, feedID string, entries []model.FeedEntry) (model.UpsertArticlesResult, error)
}

// Poller fetches a feed, parses entries, upserts new articles, and records
// the poll outcome in the feed store.
type Poller struct {
	fetcher  Fetcher
	feeds    FeedStore
	articles ArticleStore
}

// New creates a Poller.
func New(fetcher Fetcher, feeds FeedStore, articles ArticleStore) *Poller {
	return &Poller{
		fetcher:  fetcher,
		feeds:    feeds,
		articles: articles,
	}
}

// PollResult summarises a single feed poll.
type PollResult struct {
	URL          string
	Title        string
	NewArticles  int
	Existing     int
	StatusCode   int
	NextPoll     time.Time
	Error        error
}

// PollFeed fetches url, parses entries, and upserts new articles. It always
// records the poll outcome (success or error) via the feed store.
func (p *Poller) PollFeed(ctx context.Context, feedID, feedURL string) PollResult {
	result := PollResult{
		URL:       feedURL,
		NextPoll:  time.Now().UTC().Add(defaultPollInterval),
	}

	resp, err := p.fetcher.Fetch(feedURL)
	if err != nil {
		result.Error = err
		_ = p.feeds.UpdatePollResult(ctx, feedID, "", "", err.Error())
		return result
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode

	if resp.StatusCode == http.StatusNotModified {
		etag := resp.Header.Get("ETag")
		lastMod := resp.Header.Get("Last-Modified")
		_ = p.feeds.UpdatePollResult(ctx, feedID, etag, lastMod, "")
		result.NextPoll = nextPoll(time.Now().UTC(), resp.Header.Get("Retry-After"), nil)
		return result
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("unexpected status %d", resp.StatusCode)
		result.Error = err
		_ = p.feeds.UpdatePollResult(ctx, feedID, "", "", err.Error())
		return result
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result.Error = fmt.Errorf("read body: %w", err)
		_ = p.feeds.UpdatePollResult(ctx, feedID, "", "", result.Error.Error())
		return result
	}

	entries, feedTitle, err := feed.ParseFeedEntries(body)
	if err != nil {
		result.Error = fmt.Errorf("parse feed: %w", err)
		_ = p.feeds.UpdatePollResult(ctx, feedID, "", "", result.Error.Error())
		return result
	}
	result.Title = feedTitle

	upsertResult, err := p.articles.UpsertArticles(ctx, feedID, entries)
	if err != nil {
		result.Error = fmt.Errorf("upsert articles: %w", err)
		_ = p.feeds.UpdatePollResult(ctx, feedID, "", "", result.Error.Error())
		return result
	}
	result.NewArticles = len(upsertResult.New)
	result.Existing = upsertResult.Exists

	etag := resp.Header.Get("ETag")
	lastMod := resp.Header.Get("Last-Modified")
	_ = p.feeds.UpdatePollResult(ctx, feedID, etag, lastMod, "")

	result.NextPoll = nextPoll(time.Now().UTC(), resp.Header.Get("Retry-After"), nil)
	return result
}

// nextPoll calculates the next poll time based on the Retry-After header,
// clamping to the configured min/max window.
func nextPoll(now time.Time, retryAfter string, _ interface{}) time.Time {
	interval := defaultPollInterval
	if retryAfter != "" {
		if seconds, err := strconv.Atoi(retryAfter); err == nil {
			interval = time.Duration(seconds) * time.Second
		}
		// Clamp to valid poll window.
		if interval < minPollInterval {
			interval = minPollInterval
		}
		if interval > maxPollInterval {
			interval = maxPollInterval
		}
	}
	return now.Add(interval)
}
