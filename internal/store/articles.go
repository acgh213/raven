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

// ContentVersion status constants.
const (
	CVStatusPending    = "pending"
	CVStatusProcessing = "processing"
	CVStatusCompleted  = "completed"
	CVStatusFailed     = "failed"
)

// ListPendingForFeed returns content versions with status='pending' for articles
// belonging to the given feed, ordered by created_at ascending.
func (s *ArticleStore) ListPendingForFeed(ctx context.Context, feedID string, limit int) ([]model.ContentVersion, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT cv.id, cv.article_id, a.url,
		        cv.extraction_status,
		        COALESCE(cv.extraction_engine, ''), COALESCE(cv.extraction_version, ''),
		        cv.word_count, COALESCE(cv.lead_image_url, ''),
		        COALESCE(cv.content_hash, ''), cv.is_latest, cv.created_at
		 FROM article_content_versions cv
		 JOIN articles a ON a.id = cv.article_id
		 WHERE a.feed_id = ? AND cv.extraction_status = 'pending'
		 ORDER BY cv.created_at ASC
		 LIMIT ?`,
		feedID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list pending content versions: %w", err)
	}
	defer rows.Close()

	var versions []model.ContentVersion
	for rows.Next() {
		var v model.ContentVersion
		var isLatest int
		if err := rows.Scan(
			&v.ID, &v.ArticleID, &v.ArticleURL,
			&v.ExtractionStatus,
			&v.ExtractionEngine, &v.ExtractionVersion,
			&v.WordCount, &v.LeadImageURL,
			&v.ContentHash, &isLatest, &v.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan content version: %w", err)
		}
		v.IsLatest = isLatest == 1
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate content versions: %w", err)
	}
	return versions, nil
}

// UpdateContentVersion records the result of an extraction run. On success
// (status=CVStatusCompleted), text, word count, engine, and version are
// stored. On failure (status=CVStatusFailed), only the status is updated.
func (s *ArticleStore) UpdateContentVersion(
	ctx context.Context,
	versionID string,
	rawHTML []byte,
	extractedText string,
	wordCount int,
	leadImageURL string,
	contentHash string,
	engine string,
	version string,
	status string,
) error {
	now := s.clk.Now().UTC().Format(time.RFC3339Nano)

	var err error
	if status == CVStatusCompleted {
		_, err = s.db.ExecContext(ctx,
			`UPDATE article_content_versions
			 SET raw_html = ?, extracted_text = ?, extraction_status = ?,
			     extraction_engine = ?, extraction_version = ?,
			     word_count = ?, lead_image_url = ?, content_hash = ?,
			     created_at = created_at
			 WHERE id = ?`,
			string(rawHTML), extractedText, status,
			engine, version,
			wordCount, leadImageURL, contentHash,
			versionID,
		)
	} else {
		_, err = s.db.ExecContext(ctx,
			`UPDATE article_content_versions
			 SET extraction_status = ?, created_at = created_at
			 WHERE id = ?`,
			status, versionID,
		)
	}
	if err != nil {
		return fmt.Errorf("update content version %q: %w", versionID, err)
	}

	// Touch the parent article's updated_at.
	_, _ = s.db.ExecContext(ctx,
		`UPDATE articles SET updated_at = ? WHERE id = (
		 SELECT article_id FROM article_content_versions WHERE id = ?
	 )`, now, versionID,
	)

	return nil
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
