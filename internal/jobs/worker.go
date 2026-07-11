// Package jobs provides a durable background worker that claims jobs from the
// store, dispatches them to registered handlers by kind, and completes or
// fails the job based on handler outcome.
package jobs

import (
	"context"
	"fmt"
	"sync"

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
// handlers. It processes jobs in a loop until no eligible jobs remain.
type Worker struct {
	store         *store.JobStore
	handlers      map[string]Handler // keyed by job kind
	maxConcurrent int
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
	}
}

// Run claims and processes jobs until no eligible jobs remain. It respects
// the maxConcurrent limit by using a semaphore channel.
func (w *Worker) Run(ctx context.Context) {
	sem := make(chan struct{}, w.maxConcurrent)
	var wg sync.WaitGroup

	for {
		job, err := w.store.ClaimNext(ctx)
		if err != nil {
			// Log-worthy but not fatal; break the loop.
			break
		}
		if job == nil {
			break // No more work.
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

	wg.Wait()
}

// process handles a single claimed job.
func (w *Worker) process(ctx context.Context, job *model.Job) {
	handler, ok := w.handlers[job.Kind]
	if !ok {
		// Unknown kind: fail with a visible error.
		_ = w.store.Fail(ctx, job.ID, job.LeaseID,
			fmt.Sprintf("no handler registered for job kind %q", job.Kind),
		)
		return
	}

	if err := handler.Handle(ctx, job); err != nil {
		_ = w.store.Fail(ctx, job.ID, job.LeaseID, err.Error())
		return
	}

	_ = w.store.Complete(ctx, job.ID, job.LeaseID)
}
