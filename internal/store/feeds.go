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
