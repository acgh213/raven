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
