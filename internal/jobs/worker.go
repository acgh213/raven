// Package jobs provides a durable background worker that claims jobs from the
// store, dispatches them to registered handlers by kind, and completes or
// fails the job based on handler outcome.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"raven/internal/model"
	"raven/internal/store"
)

// Handler processes a claimed job. If the handler returns an error, the job
// is failed (retried or dead-lettered). A nil result marks the job completed.
type Handler interface {
	Handle(context.Context, *model.Job) error
}

// HandlerFunc is an adapter to use a plain function as a Handler.
type HandlerFunc func(context.Context, *model.Job) error

// Handle calls the underlying function.
func (f HandlerFunc) Handle(ctx context.Context, job *model.Job) error {
	return f(ctx, job)
}

// Worker claims jobs from a JobStore and dispatches them to registered
// handlers. It processes jobs in a loop until no eligible jobs remain or
// the context is cancelled.
type Worker struct {
	store         *store.JobStore
	handlers      map[string]Handler // keyed by job kind
	maxConcurrent int
	log           *slog.Logger
}

// NewWorker creates a Worker. maxConcurrent is the maximum number of handler
// invocations running simultaneously (minimum 1).
func NewWorker(s *store.JobStore, handlers map[string]Handler, maxConcurrent int) *Worker {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &Worker{
		store:         s,
		handlers:      handlers,
		maxConcurrent: maxConcurrent,
		log:           slog.Default(),
	}
}

// maxBackoff is the maximum duration for exponential backoff after store errors.
const maxBackoff = 1 * time.Second

// Run claims and processes jobs until no eligible jobs remain. It respects
// the maxConcurrent limit by using a semaphore channel.
// Transient store errors from ClaimNext are surfaced through the logger and
// retried with bounded short exponential backoff rather than permanently
// killing the worker. The method promptly stops when ctx is cancelled,
// including while waiting during backoff.
func (w *Worker) Run(ctx context.Context) {
	sem := make(chan struct{}, w.maxConcurrent)
	var wg sync.WaitGroup

	backoff := 50 * time.Millisecond

	for {
		select {
		case <-ctx.Done():
			// Context cancelled — drain and stop.
			wg.Wait()
			return
		default:
		}

		job, err := w.store.ClaimNext(ctx)
		if err != nil {
			// Transient store error: log and retry with bounded backoff.
			w.log.Error("claim next error", "error", err)
			select {
			case <-ctx.Done():
				wg.Wait()
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		// Reset backoff on successful claim (or nil).
		backoff = 50 * time.Millisecond

		if job == nil {
			// No more work — drain is complete.
			wg.Wait()
			return
		}

		// Acquire semaphore slot.
		sem <- struct{}{}
		wg.Add(1)

		go func(j *model.Job) {
			defer wg.Done()
			defer func() { <-sem }()

			w.process(ctx, j)
		}(job)
	}
}

// process handles a single claimed job.
func (w *Worker) process(ctx context.Context, job *model.Job) {
	handler, ok := w.handlers[job.Kind]
	if !ok {
		// Unknown kind: fail with a visible error.
		err := w.store.Fail(ctx, job.ID, job.LeaseID,
			fmt.Sprintf("no handler registered for job kind %q", job.Kind),
		)
		if err != nil {
			w.log.Error("failed to fail unknown-kind job",
				"job_id", job.ID,
				"kind", job.Kind,
				"error", err,
			)
		}
		return
	}

	if err := handler.Handle(ctx, job); err != nil {
		failErr := w.store.Fail(ctx, job.ID, job.LeaseID, err.Error())
		if failErr != nil {
			w.log.Error("failed to record job failure",
				"job_id", job.ID,
				"kind", job.Kind,
				"handler_error", err,
				"fail_error", failErr,
			)
		}
		return
	}

	if err := w.store.Complete(ctx, job.ID, job.LeaseID); err != nil {
		w.log.Error("failed to complete job",
			"job_id", job.ID,
			"kind", job.Kind,
			"error", err,
		)
	}
}
