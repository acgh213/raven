// Package store provides transactional SQLite persistence for domain records.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"raven/internal/model"
)

// DefaultLeaseDuration is how long a job lease is valid.
const DefaultLeaseDuration = 60 * time.Second

// JobStore provides durable SQLite-backed job operations.
type JobStore struct {
	db  *sql.DB
	clk model.Clock
}

// NewJobStore creates a JobStore.
func NewJobStore(db *sql.DB, clk model.Clock) *JobStore {
	return &JobStore{db: db, clk: clk}
}

// nowRFC3339Nano returns the current clock time as an RFC3339Nano string.
func (s *JobStore) nowRFC3339Nano() string {
	return s.clk.Now().Format(time.RFC3339Nano)
}

// generateID creates a random hex ID.
func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback: should practically never happen.
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// scanJob scans a single job row from a row-like interface.
func scanJob(scanner interface {
	Scan(dest ...any) error
}) (model.Job, error) {
	var job model.Job
	err := scanner.Scan(
		&job.ID, &job.Kind, &job.Payload, &job.Status,
		&job.DedupeKey, &job.LeaseID, &job.LeasedUntil,
		&job.RetryCount, &job.MaxRetries, &job.ScheduledAt,
		&job.LastError, &job.CreatedAt, &job.UpdatedAt,
	)
	return job, err
}

const jobSelectCols = `id, kind, payload, status,
	COALESCE(dedupe_key, ''), COALESCE(lease_id, ''), COALESCE(leased_until, ''),
	retry_count, max_retries, scheduled_at, COALESCE(last_error, ''),
	created_at, updated_at`

// Enqueue inserts a new job or returns the existing one if dedupe_key is
// nonempty and a job with that key is still active (pending or claimed).
// Empty dedupe keys always create independent jobs.
// The lookup and insert are performed atomically in a transaction. If a
// concurrent enqueue wins the race and the schema's dedupe constraint fires,
// the existing active job is re-queried and returned.
func (s *JobStore) Enqueue(ctx context.Context, kind, payload, dedupeKey string) (*model.Job, error) {
	now := s.nowRFC3339Nano()

	// No dedupe key: simple insert outside a transaction.
	if dedupeKey == "" {
		id := generateID()
		job := &model.Job{
			ID:          id,
			Kind:        kind,
			Payload:     payload,
			Status:      model.JobStatusPending,
			DedupeKey:   dedupeKey,
			RetryCount:  0,
			MaxRetries:  3,
			ScheduledAt: now,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO jobs (id, kind, payload, status, dedupe_key, retry_count, max_retries, scheduled_at, created_at, updated_at)
			 VALUES (?, ?, ?, ?, NULL, 0, 3, ?, ?, ?)`,
			id, kind, payload, model.JobStatusPending, now, now, now,
		)
		if err != nil {
			return nil, fmt.Errorf("enqueue: %w", err)
		}
		return job, nil
	}

	// With a dedupe key, use a transaction for atomic lookup-and-insert.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("enqueue tx begin: %w", err)
	}
	defer tx.Rollback()

	// Look for existing active job with same dedupe key.
	var existing model.Job
	err = tx.QueryRowContext(ctx,
		`SELECT `+jobSelectCols+` FROM jobs
		 WHERE dedupe_key = ? AND status IN ('pending', 'claimed')`,
		dedupeKey,
	).Scan(
		&existing.ID, &existing.Kind, &existing.Payload, &existing.Status,
		&existing.DedupeKey, &existing.LeaseID, &existing.LeasedUntil,
		&existing.RetryCount, &existing.MaxRetries, &existing.ScheduledAt,
		&existing.LastError, &existing.CreatedAt, &existing.UpdatedAt,
	)
	if err == nil {
		// Found existing active job — return it.
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("enqueue dedupe commit: %w", err)
		}
		return &existing, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("enqueue dedupe lookup: %w", err)
	}

	// No existing active job — insert.
	id := generateID()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO jobs (id, kind, payload, status, dedupe_key, retry_count, max_retries, scheduled_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 0, 3, ?, ?, ?)`,
		id, kind, payload, model.JobStatusPending, dedupeKey, now, now, now,
	)
	if err != nil {
		// If the unique index constraint was violated (race lost), re-query
		// and return the existing active job.
		if isConstraintError(err) {
			if err := tx.Rollback(); err != nil {
				return nil, fmt.Errorf("enqueue race rollback: %w", err)
			}
			return s.findActiveByDedupe(ctx, dedupeKey)
		}
		return nil, fmt.Errorf("enqueue: %w", err)
	}

	if err := tx.Commit(); err != nil {
		// Check for constraint violation at commit time as well.
		if isConstraintError(err) {
			return s.findActiveByDedupe(ctx, dedupeKey)
		}
		return nil, fmt.Errorf("enqueue commit: %w", err)
	}

	job := &model.Job{
		ID:          id,
		Kind:        kind,
		Payload:     payload,
		Status:      model.JobStatusPending,
		DedupeKey:   dedupeKey,
		RetryCount:  0,
		MaxRetries:  3,
		ScheduledAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	return job, nil
}

// findActiveByDedupe re-queries for an active job by dedupe key after a race
// loss. It is called outside the transaction so the winning insert is visible.
func (s *JobStore) findActiveByDedupe(ctx context.Context, dedupeKey string) (*model.Job, error) {
	var existing model.Job
	err := s.db.QueryRowContext(ctx,
		`SELECT `+jobSelectCols+` FROM jobs
		 WHERE dedupe_key = ? AND status IN ('pending', 'claimed')
		 LIMIT 1`,
		dedupeKey,
	).Scan(
		&existing.ID, &existing.Kind, &existing.Payload, &existing.Status,
		&existing.DedupeKey, &existing.LeaseID, &existing.LeasedUntil,
		&existing.RetryCount, &existing.MaxRetries, &existing.ScheduledAt,
		&existing.LastError, &existing.CreatedAt, &existing.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("enqueue: concurrency race lost and no active job found for dedupe_key %q", dedupeKey)
	}
	if err != nil {
		return nil, fmt.Errorf("enqueue dedupe fallback: %w", err)
	}
	return &existing, nil
}

// isConstraintError returns true if the error is a SQLite UNIQUE or PRIMARY
// KEY constraint violation. It checks the modernc.org/sqlite error string.
func isConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// modernc.org/sqlite constraint violations contain these strings.
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "PRIMARY KEY must be unique") ||
		strings.Contains(msg, "constraint failed") ||
		strings.Contains(msg, "constraint violation") ||
		strings.Contains(msg, "SQLITE_CONSTRAINT")
}

// ClaimNext atomically claims one due pending job or reclaims a job with an
// expired lease. It returns nil when no eligible job exists. A fresh lease ID
// and expiration are assigned.
func (s *JobStore) ClaimNext(ctx context.Context) (*model.Job, error) {
	now := s.clk.Now()
	nowStr := now.Format(time.RFC3339Nano)
	leaseID := generateID()
	leasedUntil := now.Add(DefaultLeaseDuration).Format(time.RFC3339Nano)

	// Use a transaction for atomicity. With MaxOpenConns=1 this is serialized.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("claim tx begin: %w", err)
	}
	defer tx.Rollback()

	// Find one eligible job: pending and due, or claimed with expired lease.
	job, err := scanJob(tx.QueryRowContext(ctx,
		`SELECT `+jobSelectCols+` FROM jobs
		 WHERE (status = 'pending' AND scheduled_at <= ?)
		    OR (status = 'claimed' AND leased_until <= ?)
		 ORDER BY scheduled_at ASC
		 LIMIT 1`,
		nowStr, nowStr,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim find: %w", err)
	}

	// Atomically update the claim.
	result, err := tx.ExecContext(ctx,
		`UPDATE jobs SET status = 'claimed', lease_id = ?, leased_until = ?, updated_at = ?
		 WHERE id = ? AND status IN ('pending', 'claimed')`,
		leaseID, leasedUntil, nowStr, job.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("claim update: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		// Race lost; another claimer got it. Retry by recursing.
		// In practice with MaxOpenConns=1 this should not happen.
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("claim retry commit: %w", err)
		}
		return s.ClaimNext(ctx)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("claim commit: %w", err)
	}

	job.Status = model.JobStatusClaimed
	job.LeaseID = leaseID
	job.LeasedUntil = leasedUntil
	job.UpdatedAt = nowStr
	return &job, nil
}

// Complete marks a job as completed. It requires the matching active lease;
// a stale or wrong lease fails without mutating the job.
func (s *JobStore) Complete(ctx context.Context, id, leaseID string) error {
	now := s.nowRFC3339Nano()

	result, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status = 'completed', lease_id = NULL, leased_until = NULL, updated_at = ?
		 WHERE id = ? AND lease_id = ? AND status = 'claimed'`,
		now, id, leaseID,
	)
	if err != nil {
		return fmt.Errorf("complete: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("complete: no matching claimed job with lease %q for id %q", leaseID, id)
	}
	return nil
}

// Fail marks a job as failed. If the job still has retries remaining, it is
// rescheduled as pending with deterministic exponential backoff (2^retry_count
// seconds). If the retry budget is exhausted, the job is marked dead. The
// error string is truncated to 1024 characters.
func (s *JobStore) Fail(ctx context.Context, id, leaseID, errMsg string) error {
	now := s.clk.Now()

	// Truncate error message.
	if len(errMsg) > 1024 {
		errMsg = errMsg[:1024]
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("fail tx begin: %w", err)
	}
	defer tx.Rollback()

	// Verify lease match and read current state.
	var retryCount, maxRetries int
	err = tx.QueryRowContext(ctx,
		"SELECT retry_count, max_retries FROM jobs WHERE id = ? AND lease_id = ? AND status = 'claimed'",
		id, leaseID,
	).Scan(&retryCount, &maxRetries)
	if err == sql.ErrNoRows {
		return fmt.Errorf("fail: no matching claimed job with lease %q for id %q", leaseID, id)
	}
	if err != nil {
		return fmt.Errorf("fail query: %w", err)
	}

	newRetryCount := retryCount + 1

	if newRetryCount > maxRetries {
		// Exhausted retries -> dead.
		nowStr := now.Format(time.RFC3339Nano)
		_, err = tx.ExecContext(ctx,
			`UPDATE jobs SET status = 'dead', lease_id = NULL, leased_until = NULL,
			        retry_count = ?, last_error = ?, updated_at = ?
			 WHERE id = ? AND lease_id = ? AND status = 'claimed'`,
			newRetryCount, errMsg, nowStr, id, leaseID,
		)
		if err != nil {
			return fmt.Errorf("fail dead: %w", err)
		}
	} else {
		// Reschedule with exponential backoff: 2^retry_count seconds (0-indexed).
		backoff := time.Duration(1<<uint(retryCount)) * time.Second
		scheduledAt := now.Add(backoff).Format(time.RFC3339Nano)

		_, err = tx.ExecContext(ctx,
			`UPDATE jobs SET status = 'pending', lease_id = NULL, leased_until = NULL,
			        retry_count = ?, last_error = ?, scheduled_at = ?, updated_at = ?
			 WHERE id = ? AND lease_id = ? AND status = 'claimed'`,
			newRetryCount, errMsg, scheduledAt, scheduledAt, id, leaseID,
		)
		if err != nil {
			return fmt.Errorf("fail reschedule: %w", err)
		}
	}

	return tx.Commit()
}
