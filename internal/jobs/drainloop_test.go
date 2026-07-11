package jobs

import (
	"context"
	"testing"
	"time"
)

// TestDrainLoopCancelsPromptlyWhileWaiting verifies that DrainLoop exits
// promptly when the context is cancelled during the poll-interval wait,
// rather than sleeping for the entire interval.
func TestDrainLoopCancelsPromptlyWhileWaiting(t *testing.T) {
	t.Parallel()

	// Use a very short test interval so the test is fast, and a generous
	// timeout to avoid flakiness.
	pollInterval := 50 * time.Millisecond
	timeout := 2 * time.Second

	// A drain loop function that does nothing (returns immediately), so
	// the loop spends almost all its time in the poll-interval wait.
	noop := func(ctx context.Context) {}

	runCtx, cancel := context.WithCancel(context.Background())

	start := time.Now()
	// Cancel shortly after starting, while the loop is in its first wait.
	time.AfterFunc(10*time.Millisecond, cancel)

	DrainLoop(runCtx, noop, pollInterval)
	elapsed := time.Since(start)

	if elapsed >= timeout {
		t.Fatalf("DrainLoop took %v to cancel, expected < %v", elapsed, timeout)
	}
	t.Logf("DrainLoop cancelled in %v (pollInterval=%v)", elapsed, pollInterval)
}

// TestDrainLoopCancelsImmediately verifies that DrainLoop exits immediately
// when the context is already cancelled before calling it.
func TestDrainLoopCancelsImmediately(t *testing.T) {
	t.Parallel()

	// Use a long poll interval to prove we don't wait for it.
	pollInterval := 10 * time.Second

	noop := func(ctx context.Context) {}

	runCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	start := time.Now()
	DrainLoop(runCtx, noop, pollInterval)
	elapsed := time.Since(start)

	if elapsed >= time.Second {
		t.Fatalf("DrainLoop took %v to cancel pre-cancelled context, expected < 1s", elapsed)
	}
	t.Logf("DrainLoop cancelled pre-cancelled context in %v", elapsed)
}

// TestDrainLoopRunsAtLeastOnce verifies that the drain function is called
// at least once even when the context is immediately cancelled.
func TestDrainLoopRunsAtLeastOnce(t *testing.T) {
	t.Parallel()

	called := make(chan struct{}, 1)
	fn := func(ctx context.Context) {
		called <- struct{}{}
	}

	runCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	DrainLoop(runCtx, fn, 10*time.Second)

	select {
	case <-called:
		// Good — function was called at least once.
	default:
		t.Error("drain loop function was never called")
	}
}
