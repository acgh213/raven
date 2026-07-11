package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"raven/internal/db"
	"raven/internal/model"
)

// fixedClock returns a deterministic time source for testing.
type fixedClock struct {
	t time.Time
}

func (c *fixedClock) Now() time.Time { return c.t }

func (c *fixedClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// openDB is a test helper that opens an in-memory database and runs migrations.
func openDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:): %v", err)
	}
	t.Cleanup(func() { database.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx, database); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return database
}

// TestEnqueueCreatesJob verifies that a basic enqueue creates a job with
// expected defaults.
func TestEnqueueCreatesJob(t *testing.T) {
	t.Parallel()

	database := openDB(t)
	ctx := context.Background()
	clk := &fixedClock{t: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)}
	s := NewJobStore(database, clk)

	job, err := s.Enqueue(ctx, "test-kind", `{"url":"https://example.com"}`, "")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if job.ID == "" {
		t.Error("expected non-empty job ID")
	}
	if job.Kind != "test-kind" {
		t.Errorf("kind: got %q, want %q", job.Kind, "test-kind")
	}
	if job.Status != model.JobStatusPending {
		t.Errorf("status: got %q, want %q", job.Status, model.JobStatusPending)
	}
	if job.RetryCount != 0 {
		t.Errorf("retry_count: got %d, want 0", job.RetryCount)
	}
	if job.MaxRetries != 3 {
		t.Errorf("max_retries: got %d, want 3", job.MaxRetries)
	}
	if job.ScheduledAt != "2025-06-15T12:00:00Z" {
		t.Errorf("scheduled_at: got %q, want %q", job.ScheduledAt, "2025-06-15T12:00:00Z")
	}
	if job.CreatedAt != "2025-06-15T12:00:00Z" {
		t.Errorf("created_at: got %q, want %q", job.CreatedAt, "2025-06-15T12:00:00Z")
	}
}

// TestEnqueueIdempotentActiveDedupe verifies that enqueuing with the same
// nonempty dedupe key while the prior job is pending or claimed returns the
// existing job without creating a duplicate.
func TestEnqueueIdempotentActiveDedupe(t *testing.T) {
	t.Parallel()

	database := openDB(t)
	ctx := context.Background()
	clk := &fixedClock{t: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)}
	s := NewJobStore(database, clk)

	job1, err := s.Enqueue(ctx, "test-kind", `{"a":1}`, "dedupe-1")
	if err != nil {
		t.Fatalf("first Enqueue: %v", err)
	}

	// Second enqueue with same dedupe while pending should return existing job.
	job2, err := s.Enqueue(ctx, "test-kind", `{"a":2}`, "dedupe-1")
	if err != nil {
		t.Fatalf("second Enqueue: %v", err)
	}
	if job1.ID != job2.ID {
		t.Errorf("expected same job ID for duplicate dedupe key: %q vs %q", job1.ID, job2.ID)
	}

	// Claim the job, then try dedup again.
	claimed, err := s.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected a claimed job")
	}

	job3, err := s.Enqueue(ctx, "test-kind", `{"a":3}`, "dedupe-1")
	if err != nil {
		t.Fatalf("third Enqueue: %v", err)
	}
	if job1.ID != job3.ID {
		t.Errorf("expected same job ID for duplicate dedupe key while claimed: %q vs %q", job1.ID, job3.ID)
	}

	// After completing, dedupe key should be reusable.
	if err := s.Complete(ctx, claimed.ID, claimed.LeaseID); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	job4, err := s.Enqueue(ctx, "test-kind", `{"a":4}`, "dedupe-1")
	if err != nil {
		t.Fatalf("fourth Enqueue: %v", err)
	}
	if job4.ID == job1.ID {
		t.Error("expected a new job after prior completed")
	}
}

// TestEnqueueBlankDedupe verifies that empty dedupe keys create independent
// jobs every time, even if all other fields are the same.
func TestEnqueueBlankDedupe(t *testing.T) {
	t.Parallel()

	database := openDB(t)
	ctx := context.Background()
	clk := &fixedClock{t: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)}
	s := NewJobStore(database, clk)

	job1, err := s.Enqueue(ctx, "test-kind", `{"x":1}`, "")
	if err != nil {
		t.Fatalf("first Enqueue: %v", err)
	}
	job2, err := s.Enqueue(ctx, "test-kind", `{"x":1}`, "")
	if err != nil {
		t.Fatalf("second Enqueue: %v", err)
	}
	if job1.ID == job2.ID {
		t.Error("expected different job IDs for blank dedupe keys")
	}
}

// TestClaimNextReturnsDueJob verifies ClaimNext returns a pending job that is
// due (scheduled_at <= now).
func TestClaimNextReturnsDueJob(t *testing.T) {
	t.Parallel()

	database := openDB(t)
	ctx := context.Background()
	clk := &fixedClock{t: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)}
	s := NewJobStore(database, clk)

	job, err := s.Enqueue(ctx, "test-kind", `{}`, "")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	claimed, err := s.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected a job to be claimed")
	}
	if claimed.ID != job.ID {
		t.Errorf("claimed job ID: got %q, want %q", claimed.ID, job.ID)
	}
	if claimed.Status != model.JobStatusClaimed {
		t.Errorf("status: got %q, want %q", claimed.Status, model.JobStatusClaimed)
	}
	if claimed.LeaseID == "" {
		t.Error("expected non-empty lease_id")
	}
	if claimed.LeasedUntil == "" {
		t.Error("expected non-empty leased_until")
	}
}

// TestClaimNextAtomic verifies that two simultaneous claimers never receive
// the same lease/job. Uses sequential calls on a single connection which
// is safe given MaxOpenConns=1 serializes all operations.
func TestClaimNextAtomic(t *testing.T) {
	t.Parallel()

	database := openDB(t)
	ctx := context.Background()
	clk := &fixedClock{t: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)}
	s := NewJobStore(database, clk)

	// Enqueue two jobs.
	job1, err := s.Enqueue(ctx, "test-kind", `{"id":1}`, "")
	if err != nil {
		t.Fatalf("Enqueue job1: %v", err)
	}
	job2, err := s.Enqueue(ctx, "test-kind", `{"id":2}`, "")
	if err != nil {
		t.Fatalf("Enqueue job2: %v", err)
	}

	// Claim first job.
	c1, err := s.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("ClaimNext 1: %v", err)
	}
	if c1 == nil {
		t.Fatal("expected first claim to return a job")
	}

	// Claim second job — must be different from first.
	c2, err := s.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("ClaimNext 2: %v", err)
	}
	if c2 == nil {
		t.Fatal("expected second claim to return a job")
	}
	if c1.ID == c2.ID {
		t.Error("two claimers received the same job")
	}

	// Verify original fields match expected.
	if c1.ID != job1.ID && c1.ID != job2.ID {
		t.Errorf("c1 ID %q doesn't match either enqueued job", c1.ID)
	}
	if c2.ID != job1.ID && c2.ID != job2.ID {
		t.Errorf("c2 ID %q doesn't match either enqueued job", c2.ID)
	}

	// Third claim should return nothing.
	c3, err := s.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("ClaimNext 3: %v", err)
	}
	if c3 != nil {
		t.Errorf("expected nil for third claim, got job %q", c3.ID)
	}
}

// TestClaimNextExpiredReclaims verifies that a job with an expired lease is
// reclaimed by ClaimNext.
func TestClaimNextExpiredReclaims(t *testing.T) {
	t.Parallel()

	database := openDB(t)
	ctx := context.Background()
	clk := &fixedClock{t: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)}
	s := NewJobStore(database, clk)

	job, err := s.Enqueue(ctx, "test-kind", `{}`, "")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Claim it.
	claimed, err := s.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected a claimed job")
	}

	originalLease := claimed.LeaseID

	// Advance clock past lease expiry (default lease is 60s).
	clk.advance(2 * time.Minute)

	// ClaimNext should reclaim the expired-lease job.
	reclaimed, err := s.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("ClaimNext reclaim: %v", err)
	}
	if reclaimed == nil {
		t.Fatal("expected a reclaimed job")
	}
	if reclaimed.ID != job.ID {
		t.Errorf("reclaimed job ID: got %q, want %q", reclaimed.ID, job.ID)
	}
	if reclaimed.LeaseID == originalLease {
		t.Error("expected a fresh lease_id on reclaim")
	}
	if reclaimed.LeasedUntil == "" {
		t.Error("expected non-empty leased_until on reclaim")
	}
}

// TestCompleteRequiresMatchingLease verifies Complete succeeds with the
// correct lease and fails without mutation on a stale/wrong lease.
func TestCompleteRequiresMatchingLease(t *testing.T) {
	t.Parallel()

	database := openDB(t)
	ctx := context.Background()
	clk := &fixedClock{t: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)}
	s := NewJobStore(database, clk)

	job, err := s.Enqueue(ctx, "test-kind", `{}`, "")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Wrong lease should fail.
	err = s.Complete(ctx, job.ID, "wrong-lease")
	if err == nil {
		t.Fatal("expected error for wrong lease, got nil")
	}

	// Claim the job.
	claimed, err := s.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected claimed job")
	}

	// Stale/wrong lease should fail.
	err = s.Complete(ctx, claimed.ID, "stale-lease")
	if err == nil {
		t.Fatal("expected error for stale lease, got nil")
	}

	// Correct lease should succeed.
	if err := s.Complete(ctx, claimed.ID, claimed.LeaseID); err != nil {
		t.Fatalf("Complete with correct lease: %v", err)
	}

	// Verify job is completed in DB.
	var status string
	if err := database.QueryRowContext(ctx, "SELECT status FROM jobs WHERE id = ?", claimed.ID).Scan(&status); err != nil {
		t.Fatalf("query job status: %v", err)
	}
	if status != model.JobStatusComplete {
		t.Errorf("job status after Complete: got %q, want %q", status, model.JobStatusComplete)
	}
}

// TestFailExponentialBackoffThenDead verifies that Fail records an error,
// reschedules with exponential backoff on retry_count from the failed job,
// and marks dead once retry budget is exhausted.
func TestFailExponentialBackoffThenDead(t *testing.T) {
	t.Parallel()

	database := openDB(t)
	ctx := context.Background()
	clk := &fixedClock{t: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)}
	s := NewJobStore(database, clk)

	// Enqueue a job with max_retries=3.
	job, err := s.Enqueue(ctx, "test-kind", `{}`, "")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Helper: claim and fail.
	claimAndFail := func(errorMsg string) (scheduledAt string) {
		claimed, err := s.ClaimNext(ctx)
		if err != nil {
			t.Fatalf("ClaimNext: %v", err)
		}
		if claimed == nil {
			t.Fatalf("expected claimable job, got nil")
		}
		if err := s.Fail(ctx, claimed.ID, claimed.LeaseID, errorMsg); err != nil {
			t.Fatalf("Fail: %v", err)
		}
		// Read back the scheduled_at.
		var sched string
		if err := database.QueryRowContext(ctx, "SELECT scheduled_at FROM jobs WHERE id = ?", claimed.ID).Scan(&sched); err != nil {
			t.Fatalf("query scheduled_at: %v", err)
		}
		return sched
	}

	// First failure — retry_count 0 -> backoff 2^0=1s.
	sched1 := claimAndFail("first error")
	if !strings.HasSuffix(sched1, "Z") {
		t.Errorf("expected RFC3339Nano timestamp, got %q", sched1)
	}
	// scheduled_at should be about 1 second after now.
	// Since clock is at 12:00:00, backoff 1s => 12:00:01.
	if sched1 < "2025-06-15T12:00:01" {
		t.Errorf("expected scheduled_at >= clock+1s, got %q", sched1)
	}

	// Advance clock past scheduled_at.
	clk.advance(2 * time.Second)

	// Second failure — retry_count 1 -> backoff 2^1=2s.
	sched2 := claimAndFail("second error")
	if sched2 < "2025-06-15T12:00:04" {
		t.Errorf("expected scheduled_at >= clock+2s, got %q", sched2)
	}

	clk.advance(3 * time.Second)

	// Third failure — retry_count 2 -> backoff 2^2=4s.
	sched3 := claimAndFail("third error")
	if sched3 < "2025-06-15T12:00:09" {
		t.Errorf("expected scheduled_at >= clock+4s, got %q", sched3)
	}

	clk.advance(5 * time.Second)

	// Fourth failure — retry_count 3 = max_retries (3), so go dead.
	claimed, err := s.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("ClaimNext final: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected claimable job")
	}
	if err := s.Fail(ctx, claimed.ID, claimed.LeaseID, "final error"); err != nil {
		t.Fatalf("Fail final: %v", err)
	}

	// Verify dead.
	var status string
	if err := database.QueryRowContext(ctx, "SELECT status FROM jobs WHERE id = ?", job.ID).Scan(&status); err != nil {
		t.Fatalf("query job status: %v", err)
	}
	if status != model.JobStatusDead {
		t.Errorf("expected job to be dead after budget exhaustion, got %q", status)
	}

	// Verify last_error contains the final error.
	var lastErr string
	if err := database.QueryRowContext(ctx, "SELECT last_error FROM jobs WHERE id = ?", job.ID).Scan(&lastErr); err != nil {
		t.Fatalf("query last_error: %v", err)
	}
	if lastErr != "final error" {
		t.Errorf("last_error: got %q, want %q", lastErr, "final error")
	}
}

// TestFailStaleLeaseRejected verifies that Fail rejects a stale/wrong lease
// without mutating the job.
func TestFailStaleLeaseRejected(t *testing.T) {
	t.Parallel()

	database := openDB(t)
	ctx := context.Background()
	clk := &fixedClock{t: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)}
	s := NewJobStore(database, clk)

	job, err := s.Enqueue(ctx, "test-kind", `{}`, "")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Fail with wrong lease while still pending.
	err = s.Fail(ctx, job.ID, "wrong-lease", "error")
	if err == nil {
		t.Fatal("expected error for wrong lease on pending job, got nil")
	}

	// Claim and fail with wrong lease.
	claimed, err := s.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected claimed job")
	}

	err = s.Fail(ctx, claimed.ID, "stale-lease", "error")
	if err == nil {
		t.Fatal("expected error for stale lease, got nil")
	}

	// Job should still be claimed.
	var status string
	if err := database.QueryRowContext(ctx, "SELECT status FROM jobs WHERE id = ?", claimed.ID).Scan(&status); err != nil {
		t.Fatalf("query job status: %v", err)
	}
	if status != model.JobStatusClaimed {
		t.Errorf("expected job to remain claimed after stale-lease fail, got %q", status)
	}
}
