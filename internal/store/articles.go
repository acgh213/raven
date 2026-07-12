package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
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

// cursorEntry is the wire format for cursor-based pagination.
type cursorEntry struct {
	P string `json:"p"` // published_at
	I string `json:"i"` // id
}

func decodeCursor(raw string) (cursorEntry, error) {
	if raw == "" {
		return cursorEntry{}, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return cursorEntry{}, fmt.Errorf("decode cursor: %w", err)
	}
	var c cursorEntry
	if err := json.Unmarshal(b, &c); err != nil {
		return cursorEntry{}, fmt.Errorf("decode cursor: %w", err)
	}
	return c, nil
}

func encodeCursor(publishedAt, id string) string {
	b, _ := json.Marshal(cursorEntry{P: publishedAt, I: id})
	return base64.RawURLEncoding.EncodeToString(b)
}

// ListArticles returns a cursor-paginated list of articles ordered by
// published_at DESC, id DESC (newest first). An empty cursor starts from the
// beginning. The returned NextCursor is empty when there are no more results.
func (s *ArticleStore) ListArticles(ctx context.Context, params model.ArticleListParams) (model.ArticleListResult, error) {
	limit := params.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	cursor, err := decodeCursor(params.Cursor)
	if err != nil {
		return model.ArticleListResult{}, err
	}

	var rows *sql.Rows
	if params.FeedID != "" {
		if cursor.P == "" {
			rows, err = s.db.QueryContext(ctx,
				`SELECT a.id, a.feed_id, a.guid, a.url, a.title, a.author,
				        a.published_at, a.created_at,
				        COALESCE(s.is_read, 0), COALESCE(s.is_starred, 0),
				        COALESCE(cv.word_count, 0), COALESCE(cv.lead_image_url, ''),
				        COALESCE(cv.extracted_text, '')
				 FROM articles a
				 LEFT JOIN article_state s ON s.article_id = a.id
				 LEFT JOIN article_content_versions cv ON cv.id = a.latest_content_version_id
				 WHERE a.feed_id = ? AND a.is_deleted = 0
				 ORDER BY a.published_at DESC, a.id DESC
				 LIMIT ?`,
				params.FeedID, limit+1,
			)
		} else {
			rows, err = s.db.QueryContext(ctx,
				`SELECT a.id, a.feed_id, a.guid, a.url, a.title, a.author,
				        a.published_at, a.created_at,
				        COALESCE(s.is_read, 0), COALESCE(s.is_starred, 0),
				        COALESCE(cv.word_count, 0), COALESCE(cv.lead_image_url, ''),
				        COALESCE(cv.extracted_text, '')
				 FROM articles a
				 LEFT JOIN article_state s ON s.article_id = a.id
				 LEFT JOIN article_content_versions cv ON cv.id = a.latest_content_version_id
				 WHERE a.feed_id = ? AND a.is_deleted = 0
				   AND (a.published_at < ? OR (a.published_at = ? AND a.id < ?))
				 ORDER BY a.published_at DESC, a.id DESC
				 LIMIT ?`,
				params.FeedID, cursor.P, cursor.P, cursor.I, limit+1,
			)
		}
	} else {
		if cursor.P == "" {
			rows, err = s.db.QueryContext(ctx,
				`SELECT a.id, a.feed_id, a.guid, a.url, a.title, a.author,
				        a.published_at, a.created_at,
				        COALESCE(s.is_read, 0), COALESCE(s.is_starred, 0),
				        COALESCE(cv.word_count, 0), COALESCE(cv.lead_image_url, ''),
				        COALESCE(cv.extracted_text, '')
				 FROM articles a
				 LEFT JOIN article_state s ON s.article_id = a.id
				 LEFT JOIN article_content_versions cv ON cv.id = a.latest_content_version_id
				 WHERE a.is_deleted = 0
				 ORDER BY a.published_at DESC, a.id DESC
				 LIMIT ?`,
				limit+1,
			)
		} else {
			rows, err = s.db.QueryContext(ctx,
				`SELECT a.id, a.feed_id, a.guid, a.url, a.title, a.author,
				        a.published_at, a.created_at,
				        COALESCE(s.is_read, 0), COALESCE(s.is_starred, 0),
				        COALESCE(cv.word_count, 0), COALESCE(cv.lead_image_url, ''),
				        COALESCE(cv.extracted_text, '')
				 FROM articles a
				 LEFT JOIN article_state s ON s.article_id = a.id
				 LEFT JOIN article_content_versions cv ON cv.id = a.latest_content_version_id
				 WHERE a.is_deleted = 0
				   AND (a.published_at < ? OR (a.published_at = ? AND a.id < ?))
				 ORDER BY a.published_at DESC, a.id DESC
				 LIMIT ?`,
				cursor.P, cursor.P, cursor.I, limit+1,
			)
		}
	}
	if err != nil {
		return model.ArticleListResult{}, fmt.Errorf("list articles: %w", err)
	}
	defer rows.Close()

	var summaries []model.ArticleSummary
	for rows.Next() {
		var s model.ArticleSummary
		var isRead, isStarred int
		var extractedText string
		if err := rows.Scan(
			&s.ID, &s.FeedID, &s.GUID, &s.URL, &s.Title, &s.Author,
			&s.PublishedAt, &s.CreatedAt,
			&isRead, &isStarred,
			&s.WordCount, &s.LeadImageURL,
			&extractedText,
		); err != nil {
			return model.ArticleListResult{}, fmt.Errorf("scan article: %w", err)
		}
		s.IsRead = isRead == 1
		s.IsStarred = isStarred == 1
		if len(extractedText) > 200 {
			s.Excerpt = extractedText[:200]
		} else {
			s.Excerpt = extractedText
		}
		summaries = append(summaries, s)
	}
	if err := rows.Err(); err != nil {
		return model.ArticleListResult{}, fmt.Errorf("iterate articles: %w", err)
	}

	result := model.ArticleListResult{
		Articles:   summaries,
		NextCursor: "",
	}

	if len(summaries) > limit {
		last := summaries[limit-1]
		result.NextCursor = encodeCursor(last.PublishedAt, last.ID)
		result.Articles = summaries[:limit]
	}

	return result, nil
}

// GetArticle returns the full article detail including extracted text from the
// latest content version. Returns sql.ErrNoRows if the article is not found.
func (s *ArticleStore) GetArticle(ctx context.Context, id string) (model.ArticleDetail, error) {
	var d model.ArticleDetail
	var isRead, isStarred int
	var extractedText, leadImage sql.NullString
	var wordCount sql.NullInt64

	err := s.db.QueryRowContext(ctx,
		`SELECT a.id, a.feed_id, a.guid, a.url, a.title, a.author,
		        a.published_at, a.created_at,
		        COALESCE(s.is_read, 0), COALESCE(s.is_starred, 0),
		        cv.word_count, cv.lead_image_url,
		        cv.extracted_text
		 FROM articles a
		 LEFT JOIN article_state s ON s.article_id = a.id
		 LEFT JOIN article_content_versions cv ON cv.id = a.latest_content_version_id
		 WHERE a.id = ? AND a.is_deleted = 0`,
		id,
	).Scan(
		&d.ID, &d.FeedID, &d.GUID, &d.URL, &d.Title, &d.Author,
		&d.PublishedAt, &d.CreatedAt,
		&isRead, &isStarred,
		&wordCount, &leadImage,
		&extractedText,
	)
	if err != nil {
		return model.ArticleDetail{}, err
	}

	d.IsRead = isRead == 1
	d.IsStarred = isStarred == 1
	d.WordCount = int(wordCount.Int64)
	d.LeadImageURL = leadImage.String
	d.ExtractedText = extractedText.String

	return d, nil
}

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
			 SET raw_html = ?,
			     extracted_text = CASE WHEN ? != '' THEN ? ELSE extracted_text END,
			     extraction_status = ?,
			     extraction_engine = ?, extraction_version = ?,
			     word_count = ?, lead_image_url = ?, content_hash = ?,
			     created_at = created_at
			 WHERE id = ?`,
			string(rawHTML), extractedText, extractedText, status,
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
			guid = entry.URL
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

		// Create initial content version for the new article,
		// seeded with the RSS/Atom summary so the user sees
		// something even before full extraction completes.
		contentVersionID := generateID()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO article_content_versions
			 (id, article_id, extracted_text, extraction_status, is_latest, created_at)
			 VALUES (?, ?, ?, 'pending', 1, ?)`,
			contentVersionID, articleID, entry.Summary, now,
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
