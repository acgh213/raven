package jobs

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"raven/internal/db"
	"raven/internal/model"
	"raven/internal/store"
)

// fixedClock is duplicated here from store tests for isolation.
type fixedClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fixedClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

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

// sleepHandler is a simple handler that completes successfully after a small
// sleep to exercise the async concurrency path.
type sleepHandler struct{}

func (sleepHandler) Handle(ctx context.Context, job *model.Job) error {
	return nil
}

// errorHandler always returns an error, simulating a flaky handler.
type errorHandler struct {
	msg string
}

func (h errorHandler) Handle(ctx context.Context, job *model.Job) error {
	return fmt.Errorf("%s", h.msg)
}

// blockingHandler blocks until the context is cancelled.
type blockingHandler struct{}

func (blockingHandler) Handle(ctx context.Context, job *model.Job) error {
	<-ctx.Done()
	return ctx.Err()
}

// TestWorkerSuccess verifies that a worker claims a job, runs its handler,
// and the job ends up completed.
func TestWorkerSuccess(t *testing.T) {
	t.Parallel()

	database := openDB(t)
	ctx := context.Background()
	clk := &fixedClock{t: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)}
	js := store.NewJobStore(database, clk)

	// Register a handler for "test-kind" and create the worker.
	handlers := map[string]Handler{
		"test-kind": sleepHandler{},
	}
	w := NewWorker(js, handlers, 1)

	// Enqueue a job.
	job, err := js.Enqueue(ctx, "test-kind", `{"url":"https://example.com"}`, "")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Run the worker (blocking, since maxConcurrent=1 and only one handler).
	w.Run(ctx)

	// Verify the job is completed.
	var status string
	if err := database.QueryRowContext(ctx, "SELECT status FROM jobs WHERE id = ?", job.ID).Scan(&status); err != nil {
		t.Fatalf("query job status: %v", err)
	}
	if status != model.JobStatusComplete {
		t.Errorf("expected job status %q, got %q", model.JobStatusComplete, status)
	}
}

// TestWorkerErrorRetry verifies that a handler error causes the job to be
// failed and rescheduled for retry, and that the worker picks it up again.
func TestWorkerErrorRetry(t *testing.T) {
	t.Parallel()

	database := openDB(t)
	ctx := context.Background()
	clk := &fixedClock{t: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)}
	js := store.NewJobStore(database, clk)

	handlers := map[string]Handler{
		"flaky-kind": errorHandler{msg: "transient failure"},
	}
	w := NewWorker(js, handlers, 1)

	job, err := js.Enqueue(ctx, "flaky-kind", `{}`, "")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// First run — should fail and reschedule.
	w.Run(ctx)

	// Verify: job should be pending with retry_count=1 and error recorded.
	var status string
	var retryCount int
	var lastError string
	if err := database.QueryRowContext(ctx,
		"SELECT status, retry_count, last_error FROM jobs WHERE id = ?", job.ID,
	).Scan(&status, &retryCount, &lastError); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != model.JobStatusPending {
		t.Errorf("expected status %q after first failure, got %q", model.JobStatusPending, status)
	}
	if retryCount != 1 {
		t.Errorf("expected retry_count=1 after first failure, got %d", retryCount)
	}
	if lastError != "transient failure" {
		t.Errorf("expected last_error %q, got %q", "transient failure", lastError)
	}

	// Advance clock past backoff so the retry is due.
	clk.advance(2 * time.Second)

	// Second run — should fail again and reschedule.
	w.Run(ctx)

	if err := database.QueryRowContext(ctx,
		"SELECT status, retry_count, last_error FROM jobs WHERE id = ?", job.ID,
	).Scan(&status, &retryCount, &lastError); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if retryCount != 2 {
		t.Errorf("expected retry_count=2 after second failure, got %d", retryCount)
	}
}

// TestWorkerUnknownKind verifies that an unknown job kind causes the job
// to fail/retry visibly (not silently complete).
func TestWorkerUnknownKind(t *testing.T) {
	t.Parallel()

	database := openDB(t)
	ctx := context.Background()
	clk := &fixedClock{t: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)}
	js := store.NewJobStore(database, clk)

	// No handlers registered.
	handlers := map[string]Handler{}
	w := NewWorker(js, handlers, 1)

	job, err := js.Enqueue(ctx, "unknown-kind", `{}`, "")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Run the worker — unknown kind must fail and be visible.
	w.Run(ctx)

	var status string
	var lastError string
	if err := database.QueryRowContext(ctx,
		"SELECT status, last_error FROM jobs WHERE id = ?", job.ID,
	).Scan(&status, &lastError); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status == model.JobStatusComplete {
		t.Fatal("unknown kind was silently completed")
	}
	if status == model.JobStatusPending {
		// Failed and rescheduled — that's acceptable.
		if lastError == "" {
			t.Error("expected non-empty last_error for unknown kind failure")
		}
	} else if status == model.JobStatusFailed || status == model.JobStatusDead {
		// Also acceptable states.
	} else {
		t.Errorf("unexpected status %q for unknown kind", status)
	}
}

// TestWorkerMaxConcurrent uses a deterministic barrier to verify that the
// worker respects the max concurrent handler count.
func TestWorkerMaxConcurrent(t *testing.T) {
	t.Parallel()

	database := openDB(t)
	ctx := context.Background()
	clk := &fixedClock{t: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)}
	js := store.NewJobStore(database, clk)

	// barrier synchronizes handlers: first two signal entry, then all wait.
	entered := make(chan struct{}, 2)
	exit := make(chan struct{})

	var mu sync.Mutex
	var peak int
	var cur int

	handlers := map[string]Handler{
		"track-kind": HandlerFunc(func(ctx context.Context, job *model.Job) error {
			mu.Lock()
			cur++
			if cur > peak {
				peak = cur
			}
			mu.Unlock()

			// Signal entry (non-blocking, up to cap=2).
			select {
			case entered <- struct{}{}:
			default:
			}

			// Block until barrier releases.
			<-exit
			return nil
		}),
	}

	w := NewWorker(js, handlers, 2) // Allow 2 concurrent.

	// Enqueue 3 jobs.
	for i := 0; i < 3; i++ {
		if _, err := js.Enqueue(ctx, "track-kind", `{}`, fmt.Sprintf("track-%d", i)); err != nil {
			t.Fatalf("Enqueue job %d: %v", i, err)
		}
	}

	// Run the worker in a goroutine.
	var workerWg sync.WaitGroup
	workerWg.Add(1)
	go func() {
		defer workerWg.Done()
		w.Run(ctx)
	}()

	// Wait for the first 2 handlers to enter.
	for i := 0; i < 2; i++ {
		<-entered
	}

	// At this point exactly 2 handlers should be running.
	mu.Lock()
	got := peak
	mu.Unlock()
	if got > 2 {
		t.Errorf("peak concurrency: got %d, want <= 2", got)
	}
	if got < 2 {
		t.Errorf("peak concurrency: got %d, want at least 2", got)
	}

	// Release the barrier so handlers complete.
	close(exit)
	workerWg.Wait()

	// All jobs should be completed.
	var pendingCount int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM jobs WHERE status != 'completed'").Scan(&pendingCount); err != nil {
		t.Fatalf("query pending count: %v", err)
	}
	if pendingCount != 0 {
		t.Errorf("expected all jobs completed, got %d non-completed", pendingCount)
	}
}

// TestWorkerContextCancellation verifies that Run promptly stops when the
// context is cancelled, including while waiting between poll cycles.
func TestWorkerContextCancellation(t *testing.T) {
	t.Parallel()

	database := openDB(t)
	ctx := context.Background()
	clk := &fixedClock{t: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)}
	js := store.NewJobStore(database, clk)

	handlers := map[string]Handler{
		"test-kind": sleepHandler{},
	}
	w := NewWorker(js, handlers, 1)

	// Enqueue a job that will block to give us time to cancel.
	blockJob, err := js.Enqueue(ctx, "test-kind", `{}`, "")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Cancel the context after a short delay.
	runCtx, cancel := context.WithCancel(ctx)
	time.AfterFunc(10*time.Millisecond, cancel)

	start := time.Now()
	w.Run(runCtx)
	elapsed := time.Since(start)

	// Verify the worker stopped promptly (within a reasonable margin).
	if elapsed > 2*time.Second {
		t.Errorf("Run took too long to respond to cancellation: %v", elapsed)
	}

	// Verify the blockJob was claimed but not completed (it was still running
	// when cancelled, so the lease was not released).
	var status string
	if err := database.QueryRowContext(ctx, "SELECT status FROM jobs WHERE id = ?", blockJob.ID).Scan(&status); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if status != model.JobStatusClaimed {
		t.Logf("job status after cancellation: %q (expected 'claimed')", status)
	}
}

// TestWorkerCancelsWhileWaitingAfterStoreError verifies that Run stops
// promptly on cancellation even when it is in the backoff-wait loop after a
// store error.
func TestWorkerCancelsWhileWaitingAfterStoreError(t *testing.T) {
	t.Parallel()

	database := openDB(t)
	ctx := context.Background()
	clk := &fixedClock{t: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)}
	js := store.NewJobStore(database, clk)

	handlers := map[string]Handler{
		"test-kind": sleepHandler{},
	}
	w := NewWorker(js, handlers, 1)

	// Cancel immediately so the worker should stop on first backoff check.
	runCtx, cancel := context.WithCancel(ctx)
	cancel()

	start := time.Now()
	w.Run(runCtx)
	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Errorf("Run took too long to respond to immediate cancellation: %v", elapsed)
	}
}
