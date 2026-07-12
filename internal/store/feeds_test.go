package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"raven/internal/model"
)

func TestFeedImportIdempotencyKeyReplaysResultAndRejectsChangedRequest(t *testing.T) {
	database := openDB(t)
	clock := &fixedClock{t: time.Date(2026, 7, 11, 19, 0, 0, 0, time.UTC)}
	feeds := NewFeedStore(database, clock)
	candidates := []model.FeedCandidate{{URL: "https://example.com/feed.xml", Title: "Example"}}

	first, replayed, err := feeds.ImportIdempotently(context.Background(), "client-key-1", "hash-one", candidates)
	if err != nil {
		t.Fatalf("first ImportIdempotently(): %v", err)
	}
	if replayed || len(first.Created) != 1 {
		t.Errorf("first ImportIdempotently() = (%+v, replayed=%v), want one created feed and no replay", first, replayed)
	}

	second, replayed, err := feeds.ImportIdempotently(context.Background(), "client-key-1", "hash-one", candidates)
	if err != nil {
		t.Fatalf("replay ImportIdempotently(): %v", err)
	}
	if !replayed || len(second.Created) != 1 || second.Created[0].ID != first.Created[0].ID {
		t.Errorf("replay ImportIdempotently() = (%+v, replayed=%v), want original created result", second, replayed)
	}

	if _, _, err := feeds.ImportIdempotently(context.Background(), "client-key-1", "hash-two", candidates); !errors.Is(err, ErrIdempotencyKeyConflict) {
		t.Errorf("changed request error = %v, want ErrIdempotencyKeyConflict", err)
	}

	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM feeds").Scan(&count); err != nil {
		t.Fatalf("count feeds: %v", err)
	}
	if count != 1 {
		t.Errorf("persisted feed count = %d, want 1", count)
	}
}

func TestFeedPreviewReportsNewAndExistingWithoutWriting(t *testing.T) {
	database := openDB(t)
	clock := &fixedClock{t: time.Date(2026, 7, 11, 19, 0, 0, 0, time.UTC)}
	feeds := NewFeedStore(database, clock)

	if _, err := feeds.Import(context.Background(), []model.FeedCandidate{
		{URL: "https://example.com/already-there.xml", Title: "Existing"},
	}); err != nil {
		t.Fatalf("seed Import(): %v", err)
	}

	preview, err := feeds.PreviewImport(context.Background(), []model.FeedCandidate{
		{URL: "https://example.com/new.xml", Title: "New"},
		{URL: "https://example.com/already-there.xml", Title: "Existing"},
		{URL: "https://example.com/new.xml", Title: "Duplicate in document"},
	})
	if err != nil {
		t.Fatalf("PreviewImport(): %v", err)
	}
	if len(preview.New) != 1 || preview.New[0].URL != "https://example.com/new.xml" {
		t.Errorf("preview new = %+v, want only new.xml", preview.New)
	}
	if len(preview.Duplicates) != 2 {
		t.Errorf("preview duplicates = %+v, want 2 candidates", preview.Duplicates)
	}

	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM feeds").Scan(&count); err != nil {
		t.Fatalf("count feeds: %v", err)
	}
	if count != 1 {
		t.Errorf("PreviewImport() persisted %d rows, want 1 seed row", count)
	}
}

func TestFeedImportCreatesRowsAndReportsDuplicates(t *testing.T) {
	database := openDB(t)
	clock := &fixedClock{t: time.Date(2026, 7, 11, 19, 0, 0, 0, time.UTC)}
	feeds := NewFeedStore(database, clock)

	candidates := []model.FeedCandidate{
		{URL: "https://example.com/feed.xml", Title: "Example"},
		{URL: "https://example.com/feed.xml", Title: "Duplicate in OPML"},
	}
	first, err := feeds.Import(context.Background(), candidates)
	if err != nil {
		t.Fatalf("Import() first call: %v", err)
	}
	if len(first.Created) != 1 {
		t.Fatalf("first Import() created %d feeds, want 1", len(first.Created))
	}
	if len(first.Duplicates) != 1 {
		t.Fatalf("first Import() reported %d duplicates, want 1", len(first.Duplicates))
	}
	if first.Created[0].URL != "https://example.com/feed.xml" {
		t.Errorf("created URL = %q, want canonical feed URL", first.Created[0].URL)
	}
	if first.Created[0].Title != "Example" {
		t.Errorf("created title = %q, want first candidate title", first.Created[0].Title)
	}

	second, err := feeds.Import(context.Background(), []model.FeedCandidate{
		{URL: "https://example.com/feed.xml", Title: "Duplicate from later import"},
	})
	if err != nil {
		t.Fatalf("Import() second call: %v", err)
	}
	if len(second.Created) != 0 || len(second.Duplicates) != 1 {
		t.Errorf("second Import() = %+v, want 0 created and 1 duplicate", second)
	}

	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM feeds").Scan(&count); err != nil {
		t.Fatalf("count feeds: %v", err)
	}
	if count != 1 {
		t.Errorf("persisted feed count = %d, want 1", count)
	}
}

func TestListPollableReturnsFeedsNeverPolled(t *testing.T) {
	database := openDB(t)
	clock := &fixedClock{t: time.Date(2026, 7, 11, 19, 0, 0, 0, time.UTC)}
	feeds := NewFeedStore(database, clock)

	_, err := feeds.Import(context.Background(), []model.FeedCandidate{
		{URL: "https://example.com/feed.xml", Title: "Example"},
	})
	if err != nil {
		t.Fatalf("seed Import(): %v", err)
	}

	pollable, err := feeds.ListPollable(context.Background())
	if err != nil {
		t.Fatalf("ListPollable(): %v", err)
	}
	if len(pollable) != 1 {
		t.Fatalf("ListPollable() returned %d feeds, want 1 (never polled)", len(pollable))
	}
	if pollable[0].URL != "https://example.com/feed.xml" {
		t.Errorf("pollable URL = %q, want https://example.com/feed.xml", pollable[0].URL)
	}
}

func TestListPollableExcludesRecentlyPolledFeeds(t *testing.T) {
	database := openDB(t)
	clock := &fixedClock{t: time.Date(2026, 7, 11, 19, 0, 0, 0, time.UTC)}
	feeds := NewFeedStore(database, clock)

	result, err := feeds.Import(context.Background(), []model.FeedCandidate{
		{URL: "https://example.com/feed.xml", Title: "Example"},
	})
	if err != nil {
		t.Fatalf("seed Import(): %v", err)
	}
	feedID := result.Created[0].ID

	// Record a successful poll now.
	if err := feeds.UpdatePollResult(context.Background(), feedID, "etag-1", "Mon, 01 Jan 2025 00:00:00 GMT", ""); err != nil {
		t.Fatalf("UpdatePollResult(success): %v", err)
	}

	// Immediately after — should not be pollable.
	pollable, err := feeds.ListPollable(context.Background())
	if err != nil {
		t.Fatalf("ListPollable() after recent poll: %v", err)
	}
	if len(pollable) != 0 {
		t.Errorf("ListPollable() returned %d feeds, want 0 (just polled)", len(pollable))
	}

	// Advance 13 hours (past the 12h default) — should be pollable again.
	clock.advance(13 * time.Hour)
	pollable, err = feeds.ListPollable(context.Background())
	if err != nil {
		t.Fatalf("ListPollable() after advance: %v", err)
	}
	if len(pollable) != 1 {
		t.Errorf("ListPollable() returned %d feeds, want 1 (interval expired)", len(pollable))
	}
}

func TestListPollableExcludesInactiveFeeds(t *testing.T) {
	database := openDB(t)
	clock := &fixedClock{t: time.Date(2026, 7, 11, 19, 0, 0, 0, time.UTC)}
	feeds := NewFeedStore(database, clock)

	result, err := feeds.Import(context.Background(), []model.FeedCandidate{
		{URL: "https://example.com/feed.xml", Title: "Example"},
	})
	if err != nil {
		t.Fatalf("seed Import(): %v", err)
	}
	feedID := result.Created[0].ID

	if _, err := database.Exec("UPDATE feeds SET is_active = 0 WHERE id = ?", feedID); err != nil {
		t.Fatalf("deactivate feed: %v", err)
	}

	pollable, err := feeds.ListPollable(context.Background())
	if err != nil {
		t.Fatalf("ListPollable(): %v", err)
	}
	if len(pollable) != 0 {
		t.Errorf("ListPollable() returned %d feeds, want 0 (inactive)", len(pollable))
	}
}

func TestUpdatePollResultResetsErrorCountOnSuccess(t *testing.T) {
	database := openDB(t)
	clock := &fixedClock{t: time.Date(2026, 7, 11, 19, 0, 0, 0, time.UTC)}
	feeds := NewFeedStore(database, clock)

	result, err := feeds.Import(context.Background(), []model.FeedCandidate{
		{URL: "https://example.com/feed.xml", Title: "Example"},
	})
	if err != nil {
		t.Fatalf("seed Import(): %v", err)
	}
	feedID := result.Created[0].ID

	// Simulate a failure first.
	if err := feeds.UpdatePollResult(context.Background(), feedID, "", "", "timeout"); err != nil {
		t.Fatalf("UpdatePollResult(error): %v", err)
	}

	var errCount int
	var lastErr string
	database.QueryRow("SELECT error_count, last_poll_error FROM feeds WHERE id = ?", feedID).Scan(&errCount, &lastErr)
	if errCount != 1 || lastErr != "timeout" {
		t.Errorf("after error: error_count=%d last_poll_error=%q, want 1/timeout", errCount, lastErr)
	}

	// Then a success resets.
	if err := feeds.UpdatePollResult(context.Background(), feedID, "etag-ok", "Wed, 02 Jan 2025 00:00:00 GMT", ""); err != nil {
		t.Fatalf("UpdatePollResult(success): %v", err)
	}

	database.QueryRow("SELECT error_count, last_poll_error FROM feeds WHERE id = ?", feedID).Scan(&errCount, &lastErr)
	if errCount != 0 || lastErr != "" {
		t.Errorf("after success: error_count=%d last_poll_error=%q, want 0/empty", errCount, lastErr)
	}
}

func TestUpdatePollResultUnknownFeedReturnsError(t *testing.T) {
	database := openDB(t)
	clock := &fixedClock{t: time.Date(2026, 7, 11, 19, 0, 0, 0, time.UTC)}
	feeds := NewFeedStore(database, clock)

	err := feeds.UpdatePollResult(context.Background(), "nonexistent-id", "", "", "")
	if err == nil {
		t.Fatal("UpdatePollResult() for unknown feed should return error")
	}
}
