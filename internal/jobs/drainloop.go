// Package jobs provides a durable background worker.
package jobs

import (
	"context"
	"log/slog"
	"time"
)

// DrainLoopFunc is the signature for the worker drain function used by
// DrainLoop. It is typically Worker.Run.
type DrainLoopFunc func(ctx context.Context)

// DrainLoop runs fn in a loop with the given pollInterval between iterations.
// It continues until ctx is cancelled. If fn returns and ctx is still valid,
// DrainLoop waits for pollInterval (respecting ctx cancellation) before
// calling fn again. This allows prompt shutdown even while waiting between
// drain attempts.
func DrainLoop(ctx context.Context, fn DrainLoopFunc, pollInterval time.Duration) {
	for {
		fn(ctx)

		select {
		case <-ctx.Done():
			slog.Info("worker drain loop stopped")
			return
		case <-time.After(pollInterval):
			// Continue to next drain iteration.
		}
	}
}
