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

func TestListPendingForFeedReturnsPendingContentVersions(t *testing.T) {
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

	// Create articles with pending content versions.
	_, err = articles.UpsertArticles(context.Background(), feedID, []model.FeedEntry{
		{GUID: "post-1", Title: "First", URL: "https://example.com/1"},
		{GUID: "post-2", Title: "Second", URL: "https://example.com/2"},
	})
	if err != nil {
		t.Fatalf("UpsertArticles(): %v", err)
	}

	pending, err := articles.ListPendingForFeed(context.Background(), feedID, 10)
	if err != nil {
		t.Fatalf("ListPendingForFeed(): %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("pending count = %d, want 2", len(pending))
	}
	for _, v := range pending {
		if v.ExtractionStatus != CVStatusPending {
			t.Errorf("version %q status = %q, want pending", v.ID, v.ExtractionStatus)
		}
	}
}

func TestUpdateContentVersionMarksComplete(t *testing.T) {
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

	result, _ := articles.UpsertArticles(context.Background(), feedID, []model.FeedEntry{
		{GUID: "post-1", Title: "First", URL: "https://example.com/1"},
	})
	cvID := *result.New[0].LatestContentVersionID

	err = articles.UpdateContentVersion(context.Background(), cvID,
		[]byte("<html><p>Hello</p></html>"), "Hello", 1, "", "abc123", "test-engine", "1.0", CVStatusCompleted,
	)
	if err != nil {
		t.Fatalf("UpdateContentVersion(completed): %v", err)
	}

	var status string
	var wordCount int
	var extractedText string
	database.QueryRow(
		"SELECT extraction_status, word_count, extracted_text FROM article_content_versions WHERE id = ?", cvID,
	).Scan(&status, &wordCount, &extractedText)
	if status != CVStatusCompleted {
		t.Errorf("status = %q, want completed", status)
	}
	if wordCount != 1 {
		t.Errorf("word_count = %d, want 1", wordCount)
	}
	if extractedText != "Hello" {
		t.Errorf("extracted_text = %q, want Hello", extractedText)
	}

	// Pending list should be empty now.
	pending, _ := articles.ListPendingForFeed(context.Background(), feedID, 10)
	if len(pending) != 0 {
		t.Errorf("pending after completion = %d, want 0", len(pending))
	}
}

func TestUpdateContentVersionMarksFailed(t *testing.T) {
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

	result, _ := articles.UpsertArticles(context.Background(), feedID, []model.FeedEntry{
		{GUID: "post-1", Title: "First", URL: "https://example.com/1"},
	})
	cvID := *result.New[0].LatestContentVersionID

	err = articles.UpdateContentVersion(context.Background(), cvID,
		nil, "", 0, "", "", "", "", CVStatusFailed,
	)
	if err != nil {
		t.Fatalf("UpdateContentVersion(failed): %v", err)
	}

	var status string
	database.QueryRow("SELECT extraction_status FROM article_content_versions WHERE id = ?", cvID).Scan(&status)
	if status != CVStatusFailed {
		t.Errorf("status = %q, want failed", status)
	}
}

func TestListArticlesPaginatesByPublishedAt(t *testing.T) {
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

	// Insert articles with different published_at times.
	_, err = articles.UpsertArticles(context.Background(), feedID, []model.FeedEntry{
		{GUID: "post-1", Title: "Oldest", URL: "https://example.com/1", PublishedAt: "2026-01-01T00:00:00Z"},
		{GUID: "post-2", Title: "Middle", URL: "https://example.com/2", PublishedAt: "2026-06-01T00:00:00Z"},
		{GUID: "post-3", Title: "Newest", URL: "https://example.com/3", PublishedAt: "2026-07-01T00:00:00Z"},
	})
	if err != nil {
		t.Fatalf("UpsertArticles(): %v", err)
	}

	result, err := articles.ListArticles(context.Background(), model.ArticleListParams{Limit: 2})
	if err != nil {
		t.Fatalf("ListArticles(): %v", err)
	}
	if len(result.Articles) != 2 {
		t.Fatalf("first page = %d, want 2", len(result.Articles))
	}
	// Newest first.
	if result.Articles[0].Title != "Newest" {
		t.Errorf("first article = %q, want Newest", result.Articles[0].Title)
	}
	if result.Articles[1].Title != "Middle" {
		t.Errorf("second article = %q, want Middle", result.Articles[1].Title)
	}
	if result.NextCursor == "" {
		t.Error("next_cursor should not be empty")
	}

	// Second page with cursor.
	result2, err := articles.ListArticles(context.Background(), model.ArticleListParams{
		Limit:  2,
		Cursor: result.NextCursor,
	})
	if err != nil {
		t.Fatalf("ListArticles() page 2: %v", err)
	}
	if len(result2.Articles) != 1 {
		t.Fatalf("second page = %d, want 1", len(result2.Articles))
	}
	if result2.Articles[0].Title != "Oldest" {
		t.Errorf("last article = %q, want Oldest", result2.Articles[0].Title)
	}
	if result2.NextCursor != "" {
		t.Errorf("next_cursor on last page = %q, want empty", result2.NextCursor)
	}
}

func TestListArticlesFiltersByFeed(t *testing.T) {
	database := openDB(t)
	clock := &fixedClock{t: time.Date(2026, 7, 11, 19, 0, 0, 0, time.UTC)}
	feeds := NewFeedStore(database, clock)
	articles := NewArticleStore(database, clock)

	r1, _ := feeds.Import(context.Background(), []model.FeedCandidate{
		{URL: "https://one.example.com/feed.xml", Title: "One"},
	})
	r2, _ := feeds.Import(context.Background(), []model.FeedCandidate{
		{URL: "https://two.example.com/feed.xml", Title: "Two"},
	})
	feed1 := r1.Created[0].ID
	feed2 := r2.Created[0].ID

	articles.UpsertArticles(context.Background(), feed1, []model.FeedEntry{
		{GUID: "a1", Title: "From One", URL: "https://one.example.com/a1"},
	})
	articles.UpsertArticles(context.Background(), feed2, []model.FeedEntry{
		{GUID: "b1", Title: "From Two", URL: "https://two.example.com/b1"},
	})

	result, err := articles.ListArticles(context.Background(), model.ArticleListParams{
		FeedID: feed1,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListArticles(filtered): %v", err)
	}
	if len(result.Articles) != 1 {
		t.Fatalf("filtered count = %d, want 1", len(result.Articles))
	}
	if result.Articles[0].Title != "From One" {
		t.Errorf("filtered title = %q, want From One", result.Articles[0].Title)
	}
}

func TestGetArticleReturnsFullDetail(t *testing.T) {
	database := openDB(t)
	clock := &fixedClock{t: time.Date(2026, 7, 11, 19, 0, 0, 0, time.UTC)}
	feeds := NewFeedStore(database, clock)
	articles := NewArticleStore(database, clock)

	importResult, _ := feeds.Import(context.Background(), []model.FeedCandidate{
		{URL: "https://example.com/feed.xml", Title: "Example"},
	})
	feedID := importResult.Created[0].ID

	result, _ := articles.UpsertArticles(context.Background(), feedID, []model.FeedEntry{
		{GUID: "post-1", Title: "Test Article", Author: "Alice", URL: "https://example.com/1"},
	})
	articleID := result.New[0].ID
	cvID := *result.New[0].LatestContentVersionID

	articles.UpdateContentVersion(context.Background(), cvID,
		[]byte("<html>...</html>"), "Extracted body text here.", 5, "https://example.com/img.jpg",
		"abc123", "raven-extract", "0.1.0", CVStatusCompleted,
	)

	detail, err := articles.GetArticle(context.Background(), articleID)
	if err != nil {
		t.Fatalf("GetArticle(): %v", err)
	}
	if detail.Title != "Test Article" {
		t.Errorf("title = %q", detail.Title)
	}
	if detail.Author != "Alice" {
		t.Errorf("author = %q", detail.Author)
	}
	if detail.ExtractedText != "Extracted body text here." {
		t.Errorf("extracted_text = %q", detail.ExtractedText)
	}
	if detail.WordCount != 5 {
		t.Errorf("word_count = %d", detail.WordCount)
	}
	if detail.LeadImageURL != "https://example.com/img.jpg" {
		t.Errorf("lead_image_url = %q", detail.LeadImageURL)
	}
}

func TestGetArticleReturnsErrNoRowsForMissing(t *testing.T) {
	database := openDB(t)
	clock := &fixedClock{t: time.Date(2026, 7, 11, 19, 0, 0, 0, time.UTC)}
	articles := NewArticleStore(database, clock)

	_, err := articles.GetArticle(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("GetArticle() should return error for missing article")
	}
}
