package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"raven/internal/model"
)

const feedImportOperation = "feed_import"

// ErrIdempotencyKeyConflict means a caller reused an idempotency key for a
// different request body. The original mutation remains authoritative.
var ErrIdempotencyKeyConflict = errors.New("idempotency key reused with a different request")

// FeedStore provides transactional persistence for feed subscriptions.
type FeedStore struct {
	db  *sql.DB
	clk model.Clock
}

// NewFeedStore creates a FeedStore backed by db.
func NewFeedStore(db *sql.DB, clk model.Clock) *FeedStore {
	return &FeedStore{db: db, clk: clk}
}

// PreviewImport identifies new and duplicate candidates without modifying the
// database. A candidate is duplicate if its canonical URL already exists or if
// an earlier candidate in the same document has the same URL.
func (s *FeedStore) PreviewImport(ctx context.Context, candidates []model.FeedCandidate) (model.FeedImportPreview, error) {
	preview := model.FeedImportPreview{}
	seen := make(map[string]struct{}, len(candidates))

	for _, candidate := range candidates {
		if _, duplicateInDocument := seen[candidate.URL]; duplicateInDocument {
			preview.Duplicates = append(preview.Duplicates, candidate)
			continue
		}
		seen[candidate.URL] = struct{}{}

		var found int
		err := s.db.QueryRowContext(ctx, "SELECT 1 FROM feeds WHERE feed_url = ?", candidate.URL).Scan(&found)
		switch {
		case err == nil:
			preview.Duplicates = append(preview.Duplicates, candidate)
		case err == sql.ErrNoRows:
			preview.New = append(preview.New, candidate)
		default:
			return model.FeedImportPreview{}, fmt.Errorf("preview feed %q: %w", candidate.URL, err)
		}
	}
	return preview, nil
}

// Import inserts all new candidates in one transaction and reports candidate
// URLs already present in the database or repeated within the same document.
// Candidates must have been syntactically validated and canonicalized before
// reaching this boundary.
func (s *FeedStore) Import(ctx context.Context, candidates []model.FeedCandidate) (model.FeedImportResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.FeedImportResult{}, fmt.Errorf("begin feed import: %w", err)
	}
	defer tx.Rollback()

	result, err := s.importTx(ctx, tx, candidates)
	if err != nil {
		return model.FeedImportResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.FeedImportResult{}, fmt.Errorf("commit feed import: %w", err)
	}
	return result, nil
}

// ImportIdempotently performs a feed import and stores its exact result under
// key in the same transaction. A matching retry replays the original result;
// reusing a key with a different request hash returns ErrIdempotencyKeyConflict.
func (s *FeedStore) ImportIdempotently(ctx context.Context, key, requestHash string, candidates []model.FeedCandidate) (model.FeedImportResult, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.FeedImportResult{}, false, fmt.Errorf("begin idempotent feed import: %w", err)
	}
	defer tx.Rollback()

	var storedHash, storedResponse string
	err = tx.QueryRowContext(ctx,
		`SELECT request_hash, response_json
		 FROM idempotency_keys
		 WHERE operation = ? AND key = ?`,
		feedImportOperation, key,
	).Scan(&storedHash, &storedResponse)
	switch {
	case err == nil:
		if storedHash != requestHash {
			return model.FeedImportResult{}, false, ErrIdempotencyKeyConflict
		}
		var replay model.FeedImportResult
		if err := json.Unmarshal([]byte(storedResponse), &replay); err != nil {
			return model.FeedImportResult{}, false, fmt.Errorf("decode stored feed import result: %w", err)
		}
		return replay, true, nil
	case err != sql.ErrNoRows:
		return model.FeedImportResult{}, false, fmt.Errorf("lookup idempotency key: %w", err)
	}

	result, err := s.importTx(ctx, tx, candidates)
	if err != nil {
		return model.FeedImportResult{}, false, err
	}
	responseJSON, err := json.Marshal(result)
	if err != nil {
		return model.FeedImportResult{}, false, fmt.Errorf("encode feed import result: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO idempotency_keys (operation, key, request_hash, response_json, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		feedImportOperation, key, requestHash, string(responseJSON), s.clk.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		return model.FeedImportResult{}, false, fmt.Errorf("store idempotency key: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return model.FeedImportResult{}, false, fmt.Errorf("commit idempotent feed import: %w", err)
	}
	return result, false, nil
}

// defaultPollIntervalSecs is the fallback poll interval in seconds when a
// feed row has no explicit poll_interval_seconds.
const defaultPollIntervalSecs = 12 * 3600 // 12 hours

// ListPollable returns all active feeds whose last poll is older than their
// configured interval (or never polled). Rows with NULL poll_interval_seconds
// use defaultPollIntervalSecs.
func (s *FeedStore) ListPollable(ctx context.Context) ([]model.Feed, error) {
	now := s.clk.Now().UTC().Format(time.RFC3339Nano)

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, feed_url, title, COALESCE(site_url, ''),
		        COALESCE(etag, ''), COALESCE(last_modified, ''),
		        COALESCE(poll_interval_seconds, ?),
		        COALESCE(last_polled_at, ''), COALESCE(last_poll_error, ''),
		        is_active, error_count, created_at, updated_at
		 FROM feeds
		 WHERE is_active = 1
		   AND (last_polled_at IS NULL
		        OR datetime(last_polled_at, '+' || COALESCE(poll_interval_seconds, ?) || ' seconds') < datetime(?))`,
		defaultPollIntervalSecs, defaultPollIntervalSecs, now,
	)
	if err != nil {
		return nil, fmt.Errorf("list pollable: %w", err)
	}
	defer rows.Close()

	var feeds []model.Feed
	for rows.Next() {
		var f model.Feed
		var isActive int
		if err := rows.Scan(
			&f.ID, &f.URL, &f.Title, &f.SiteURL,
			&f.ETag, &f.LastModified,
			&f.PollIntervalSecs,
			&f.LastPolledAt, &f.LastPollError,
			&isActive, &f.ErrorCount, &f.CreatedAt, &f.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan pollable feed: %w", err)
		}
		f.IsActive = isActive == 1
		feeds = append(feeds, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pollable: %w", err)
	}
	return feeds, nil
}

// UpdatePollResult records the outcome of a feed poll. On success (errMsg == ""),
// error_count is reset to 0. On failure, error_count is incremented and
// last_poll_error is stored.
func (s *FeedStore) UpdatePollResult(ctx context.Context, feedID, etag, lastModified, errMsg string) error {
	now := s.clk.Now().UTC().Format(time.RFC3339Nano)

	var result sql.Result
	var execErr error
	if errMsg == "" {
		result, execErr = s.db.ExecContext(ctx,
			`UPDATE feeds
			 SET last_polled_at = ?, etag = ?, last_modified = ?,
			     last_poll_error = '', error_count = 0, updated_at = ?
			 WHERE id = ?`,
			now, etag, lastModified, now, feedID,
		)
	} else {
		// Truncate error message to 500 chars for feed row.
		msg := errMsg
		if len(msg) > 500 {
			msg = msg[:500]
		}
		result, execErr = s.db.ExecContext(ctx,
			`UPDATE feeds
			 SET last_polled_at = ?, last_poll_error = ?,
			     error_count = error_count + 1, updated_at = ?
			 WHERE id = ?`,
			now, msg, now, feedID,
		)
	}
	if execErr != nil {
		return fmt.Errorf("update poll result: %w", execErr)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("update poll result: feed %q not found", feedID)
	}
	return nil
}

func (s *FeedStore) importTx(ctx context.Context, tx *sql.Tx, candidates []model.FeedCandidate) (model.FeedImportResult, error) {
	result := model.FeedImportResult{}
	now := s.clk.Now().UTC().Format(time.RFC3339Nano)
	for _, candidate := range candidates {
		id := generateID()
		_, err := tx.ExecContext(ctx,
			`INSERT INTO feeds (id, feed_url, title, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?)`,
			id, candidate.URL, candidate.Title, now, now,
		)
		if err != nil {
			if isConstraintError(err) {
				result.Duplicates = append(result.Duplicates, candidate)
				continue
			}
			return model.FeedImportResult{}, fmt.Errorf("insert feed %q: %w", candidate.URL, err)
		}
		result.Created = append(result.Created, model.Feed{
			ID:        id,
			URL:       candidate.URL,
			Title:     candidate.Title,
			CreatedAt: now,
			UpdatedAt: now,
		})
	}
	return result, nil
}
