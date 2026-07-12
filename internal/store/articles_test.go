package store

import (
	"context"
	"testing"
	"time"

	"raven/internal/model"
)

func TestUpsertArticlesCreatesNewArticlesAndContentVersions(t *testing.T) {
	database := openDB(t)
	clock := &fixedClock{t: time.Date(2026, 7, 11, 19, 0, 0, 0, time.UTC)}
	feeds := NewFeedStore(database, clock)
	articles := NewArticleStore(database, clock)

	// Seed a feed first.
	importResult, err := feeds.Import(context.Background(), []model.FeedCandidate{
		{URL: "https://example.com/feed.xml", Title: "Example"},
	})
	if err != nil {
		t.Fatalf("seed Import(): %v", err)
	}
	feedID := importResult.Created[0].ID

	entries := []model.FeedEntry{
		{GUID: "post-1", Title: "First Post", URL: "https://example.com/posts/1", Author: "Alice"},
		{GUID: "post-2", Title: "Second Post", URL: "https://example.com/posts/2", Author: "Bob"},
	}

	result, err := articles.UpsertArticles(context.Background(), feedID, entries)
	if err != nil {
		t.Fatalf("UpsertArticles(): %v", err)
	}
	if len(result.New) != 2 {
		t.Fatalf("UpsertArticles() new count = %d, want 2", len(result.New))
	}
	if result.Exists != 0 {
		t.Errorf("UpsertArticles() exists = %d, want 0 on first insert", result.Exists)
	}

	// Verify content versions were created.
	for _, a := range result.New {
		if a.LatestContentVersionID == nil || *a.LatestContentVersionID == "" {
			t.Errorf("article %q has no content version", a.GUID)
		}
		var status string
		if err := database.QueryRow(
			"SELECT extraction_status FROM article_content_versions WHERE id = ?",
			*a.LatestContentVersionID,
		).Scan(&status); err != nil {
			t.Errorf("query content version for %q: %v", a.GUID, err)
		}
		if status != "pending" {
			t.Errorf("content version status = %q, want pending", status)
		}
	}
}

func TestUpsertArticlesDeduplicatesByFeedAndGUID(t *testing.T) {
	database := openDB(t)
	clock := &fixedClock{t: time.Date(2026, 7, 11, 19, 0, 0, 0, time.UTC)}
	feeds := NewFeedStore(database, clock)
	articles := NewArticleStore(database, clock)

	importResult, err := feeds.Import(context.Background(), []model.FeedCandidate{
		{URL: "https://example.com/feed.xml", Title: "Example"},
	})
	if err != nil {
		t.Fatalf("seed Import(): %v", err)
	}
	feedID := importResult.Created[0].ID

	entries := []model.FeedEntry{
		{GUID: "post-1", Title: "First Post", URL: "https://example.com/posts/1"},
	}

	first, err := articles.UpsertArticles(context.Background(), feedID, entries)
	if err != nil {
		t.Fatalf("first UpsertArticles(): %v", err)
	}
	if len(first.New) != 1 {
		t.Fatalf("first UpsertArticles() new = %d, want 1", len(first.New))
	}

	// Re-insert same GUID — should be a duplicate.
	second, err := articles.UpsertArticles(context.Background(), feedID, entries)
	if err != nil {
		t.Fatalf("second UpsertArticles(): %v", err)
	}
	if len(second.New) != 0 {
		t.Errorf("second UpsertArticles() new = %d, want 0", len(second.New))
	}
	if second.Exists != 1 {
		t.Errorf("second UpsertArticles() exists = %d, want 1", second.Exists)
	}

	// Same GUID, different feed — should be new.
	importResult2, err := feeds.Import(context.Background(), []model.FeedCandidate{
		{URL: "https://example.com/other.xml", Title: "Other"},
	})
	if err != nil {
		t.Fatalf("seed second feed: %v", err)
	}
	feedID2 := importResult2.Created[0].ID

	third, err := articles.UpsertArticles(context.Background(), feedID2, entries)
	if err != nil {
		t.Fatalf("third UpsertArticles() (different feed): %v", err)
	}
	if len(third.New) != 1 {
		t.Errorf("third UpsertArticles() new = %d, want 1 (different feed, same GUID)", len(third.New))
	}

	var count int
	database.QueryRow("SELECT COUNT(*) FROM articles").Scan(&count)
	if count != 2 {
		t.Errorf("total articles = %d, want 2 (one per feed)", count)
	}
}

func TestUpsertArticlesUsesURLAsFallbackGUID(t *testing.T) {
	database := openDB(t)
	clock := &fixedClock{t: time.Date(2026, 7, 11, 19, 0, 0, 0, time.UTC)}
	feeds := NewFeedStore(database, clock)
	articles := NewArticleStore(database, clock)

	importResult, err := feeds.Import(context.Background(), []model.FeedCandidate{
		{URL: "https://example.com/feed.xml", Title: "Example"},
	})
	if err != nil {
		t.Fatalf("seed Import(): %v", err)
	}
	feedID := importResult.Created[0].ID

	// Entry with no GUID — should fall back to URL.
	entries := []model.FeedEntry{
		{GUID: "", Title: "No GUID", URL: "https://example.com/posts/no-guid"},
	}

	result, err := articles.UpsertArticles(context.Background(), feedID, entries)
	if err != nil {
		t.Fatalf("UpsertArticles() with empty GUID: %v", err)
	}
	if len(result.New) != 1 {
		t.Fatalf("new = %d, want 1", len(result.New))
	}
	if result.New[0].GUID != "https://example.com/posts/no-guid" {
		t.Errorf("stored GUID = %q, want URL fallback", result.New[0].GUID)
	}
}
