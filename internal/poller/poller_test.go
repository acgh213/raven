package poller

import (
	"testing"
	"time"
)

func TestPollerCalculatesNextPollFromResponseHeaders(t *testing.T) {
	now := time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		header  string
		minNext time.Duration
		maxNext time.Duration
	}{
		{"default with no header", "", 10*time.Hour, 14*time.Hour},
		{"respects Retry-After within window", "21600", 6*time.Hour, 6*time.Hour + time.Second},
		{"clamps Retry-After above max", "86401", 23*time.Hour, 24*time.Hour + time.Second},
		{"clamps Retry-After below min", "1", 4*time.Hour, 4*time.Hour + time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := nextPoll(now, tt.header, nil)
			delta := next.Sub(now)
			if delta < tt.minNext || delta > tt.maxNext {
				t.Errorf("nextPoll() = %v from now, want between %v and %v", delta, tt.minNext, tt.maxNext)
			}
		})
	}
}

func TestPollerEnqueuesArticleFetchAndExtractJobsPerEntry(t *testing.T) {
	// We'll test this after the polling loop is implemented.
}

func TestPollerMarksErrorStateOnRepeatedFailures(t *testing.T) {
	// We'll test this after the error-state store is implemented.
}
