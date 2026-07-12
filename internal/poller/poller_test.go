package poller

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"raven/internal/model"
)

// stubFetcher returns canned responses keyed by URL.
type stubFetcher struct {
	responses map[string]*http.Response
	errors    map[string]error
}

func (f *stubFetcher) Fetch(url string) (*http.Response, error) {
	if err, ok := f.errors[url]; ok {
		return nil, err
	}
	if resp, ok := f.responses[url]; ok {
		return resp, nil
	}
	return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(bytes.NewReader(nil))}, nil
}

// stubFeedStore records UpdatePollResult calls for test inspection.
type stubFeedStore struct {
	calls []pollUpdate
}

type pollUpdate struct {
	feedID       string
	etag         string
	lastModified string
	errMsg       string
}

func (s *stubFeedStore) UpdatePollResult(_ context.Context, feedID, etag, lastModified, errMsg string) error {
	s.calls = append(s.calls, pollUpdate{feedID, etag, lastModified, errMsg})
	return nil
}

// stubArticleStore records UpsertArticles calls and returns canned results.
type stubArticleStore struct {
	result model.UpsertArticlesResult
	err    error
	calls  []upsertCall
}

type upsertCall struct {
	feedID  string
	entries []model.FeedEntry
}

func (s *stubArticleStore) UpsertArticles(_ context.Context, feedID string, entries []model.FeedEntry) (model.UpsertArticlesResult, error) {
	s.calls = append(s.calls, upsertCall{feedID, entries})
	return s.result, s.err
}

func rssBody() *http.Response {
	h := http.Header{}
	h.Set("ETag", `"abc123"`)
	h.Set("Last-Modified", "Mon, 01 Jan 2025 00:00:00 GMT")
	h.Set("Retry-After", "21600")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     h,
		Body: io.NopCloser(bytes.NewReader([]byte(`<?xml version="1.0"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <item>
      <title>Hello World</title>
      <link>https://example.com/hello</link>
      <guid>guid-1</guid>
      <author>Alice</author>
    </item>
    <item>
      <title>Second Post</title>
      <link>https://example.com/second</link>
      <guid>guid-2</guid>
      <author>Bob</author>
    </item>
  </channel>
</rss>`))),
	}
}

func atomBody() *http.Response {
	h := http.Header{}
	h.Set("ETag", `"xyz789"`)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     h,
		Body: io.NopCloser(bytes.NewReader([]byte(`<?xml version="1.0"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Atom Test</title>
  <entry>
    <title>Atom Entry</title>
    <id>atom-guid-1</id>
    <link rel="alternate" href="https://example.com/atom-entry"/>
    <author><name>Carol</name></author>
  </entry>
</feed>`))),
	}
}

func TestPollFeedParsesRSSAndUpsertsArticles(t *testing.T) {
	fetcher := &stubFetcher{
		responses: map[string]*http.Response{
			"https://example.com/rss": rssBody(),
		},
	}
	feeds := &stubFeedStore{}
	articles := &stubArticleStore{
		result: model.UpsertArticlesResult{New: make([]model.Article, 2), Exists: 0},
	}
	poller := New(fetcher, feeds, articles)

	result := poller.PollFeed(context.Background(), "feed-1", "https://example.com/rss")

	if result.Error != nil {
		t.Fatalf("PollFeed() error: %v", result.Error)
	}
	if result.Title != "Test Feed" {
		t.Errorf("title = %q, want Test Feed", result.Title)
	}
	if result.NewArticles != 2 {
		t.Errorf("newArticles = %d, want 2", result.NewArticles)
	}
	if result.StatusCode != 200 {
		t.Errorf("statusCode = %d, want 200", result.StatusCode)
	}

	// Verify feed store recorded success.
	if len(feeds.calls) != 1 {
		t.Fatalf("feed store calls = %d, want 1", len(feeds.calls))
	}
	if feeds.calls[0].etag != `"abc123"` {
		t.Errorf("recorded etag = %q, want \"abc123\"", feeds.calls[0].etag)
	}
	if feeds.calls[0].errMsg != "" {
		t.Errorf("recorded errMsg = %q, want empty", feeds.calls[0].errMsg)
	}
}

func TestPollFeedParsesAtom(t *testing.T) {
	fetcher := &stubFetcher{
		responses: map[string]*http.Response{
			"https://example.com/atom": atomBody(),
		},
	}
	feeds := &stubFeedStore{}
	articles := &stubArticleStore{
		result: model.UpsertArticlesResult{New: make([]model.Article, 1), Exists: 0},
	}
	poller := New(fetcher, feeds, articles)

	result := poller.PollFeed(context.Background(), "feed-2", "https://example.com/atom")

	if result.Error != nil {
		t.Fatalf("PollFeed() error: %v", result.Error)
	}
	if result.Title != "Atom Test" {
		t.Errorf("title = %q, want Atom Test", result.Title)
	}
	if result.NewArticles != 1 {
		t.Errorf("newArticles = %d, want 1", result.NewArticles)
	}
}

func TestPollFeedRecordsErrorOnFetchFailure(t *testing.T) {
	fetcher := &stubFetcher{
		errors: map[string]error{
			"https://example.com/broken": io.ErrUnexpectedEOF,
		},
	}
	feeds := &stubFeedStore{}
	poller := New(fetcher, feeds, &stubArticleStore{})

	result := poller.PollFeed(context.Background(), "feed-3", "https://example.com/broken")

	if result.Error == nil {
		t.Fatal("PollFeed() should return error on fetch failure")
	}
	if len(feeds.calls) != 1 {
		t.Fatalf("feed store calls = %d, want 1 (error recorded)", len(feeds.calls))
	}
	if feeds.calls[0].errMsg == "" {
		t.Error("feed store error message should not be empty")
	}
}

func TestPollFeedRecordsErrorOnBadStatus(t *testing.T) {
	fetcher := &stubFetcher{
		responses: map[string]*http.Response{
			"https://example.com/500": {
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewReader(nil)),
			},
		},
	}
	feeds := &stubFeedStore{}
	poller := New(fetcher, feeds, &stubArticleStore{})

	result := poller.PollFeed(context.Background(), "feed-4", "https://example.com/500")

	if result.Error == nil {
		t.Fatal("PollFeed() should return error on 500 status")
	}
	if result.StatusCode != 500 {
		t.Errorf("statusCode = %d, want 500", result.StatusCode)
	}
	if len(feeds.calls) != 1 {
		t.Fatal("expected feed store error to be recorded")
	}
}

func TestPollFeedRecordsErrorOnUnparseableBody(t *testing.T) {
	fetcher := &stubFetcher{
		responses: map[string]*http.Response{
			"https://example.com/garbage": {
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte("not xml at all"))),
			},
		},
	}
	feeds := &stubFeedStore{}
	poller := New(fetcher, feeds, &stubArticleStore{})

	result := poller.PollFeed(context.Background(), "feed-5", "https://example.com/garbage")

	if result.Error == nil {
		t.Fatal("PollFeed() should return error on unparseable body")
	}
	if len(feeds.calls) != 1 {
		t.Fatal("expected feed store error to be recorded")
	}
}

func TestNextPollClamps(t *testing.T) {
	now := time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		header  string
		minNext time.Duration
		maxNext time.Duration
	}{
		{"default with no header", "", 10 * time.Hour, 14 * time.Hour},
		{"respects Retry-After within window", "21600", 6 * time.Hour, 6*time.Hour + time.Second},
		{"clamps Retry-After above max", "86401", 23 * time.Hour, 24*time.Hour + time.Second},
		{"clamps Retry-After below min", "1", 4 * time.Hour, 4*time.Hour + time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := nextPoll(now, tt.header, nil)
			delta := next.Sub(now)
			if delta < tt.minNext || delta > tt.maxNext {
				t.Errorf("nextPoll() = %v from now, want between %v and %v", delta, tt.minNext, tt.maxNext)
			}
		})
	}
}
