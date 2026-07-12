package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"raven/internal/model"
)

// ArticleStore persists articles and their content versions.
type ArticleStore struct {
	db  *sql.DB
	clk model.Clock
}

// NewArticleStore creates an ArticleStore backed by db.
func NewArticleStore(db *sql.DB, clk model.Clock) *ArticleStore {
	return &ArticleStore{db: db, clk: clk}
}

// UpsertArticles inserts new articles (by feed_id + guid uniqueness) and
// creates an initial pending content version for each new article. Articles
// that already exist are counted but not modified.
func (s *ArticleStore) UpsertArticles(ctx context.Context, feedID string, entries []model.FeedEntry) (model.UpsertArticlesResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.UpsertArticlesResult{}, fmt.Errorf("upsert articles tx begin: %w", err)
	}
	defer tx.Rollback()

	var result model.UpsertArticlesResult
	now := s.clk.Now().UTC().Format(time.RFC3339Nano)

	for _, entry := range entries {
		articleID := generateID()
		guid := entry.GUID
		if guid == "" {
			guid = entry.URL // fallback: use URL as GUID when feed omits it
		}
		publishedAt := entry.PublishedAt
		if publishedAt == "" {
			publishedAt = now
		}

		insertResult, err := tx.ExecContext(ctx,
			`INSERT INTO articles (id, feed_id, guid, url, title, author, published_at, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(feed_id, guid) DO NOTHING`,
			articleID, feedID, guid, entry.URL, entry.Title, entry.Author,
			publishedAt, now, now,
		)
		if err != nil {
			return model.UpsertArticlesResult{}, fmt.Errorf("insert article %q: %w", guid, err)
		}
		rows, _ := insertResult.RowsAffected()
		if rows == 0 {
			result.Exists++
			continue
		}

		// Create initial content version for the new article.
		contentVersionID := generateID()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO article_content_versions
			 (id, article_id, extraction_status, is_latest, created_at)
			 VALUES (?, ?, 'pending', 1, ?)`,
			contentVersionID, articleID, now,
		); err != nil {
			return model.UpsertArticlesResult{}, fmt.Errorf("insert content version for article %q: %w", guid, err)
		}

		// Link the article to its latest content version.
		if _, err := tx.ExecContext(ctx,
			`UPDATE articles SET latest_content_version_id = ?, updated_at = ?
			 WHERE id = ?`,
			contentVersionID, now, articleID,
		); err != nil {
			return model.UpsertArticlesResult{}, fmt.Errorf("link content version for article %q: %w", guid, err)
		}

		result.New = append(result.New, model.Article{
			ID:                     articleID,
			FeedID:                 feedID,
			GUID:                   guid,
			URL:                    entry.URL,
			Title:                  entry.Title,
			Author:                 entry.Author,
			PublishedAt:            publishedAt,
			LatestContentVersionID: &contentVersionID,
			CreatedAt:              now,
			UpdatedAt:              now,
		})
	}

	if err := tx.Commit(); err != nil {
		return model.UpsertArticlesResult{}, fmt.Errorf("upsert articles commit: %w", err)
	}
	return result, nil
}
