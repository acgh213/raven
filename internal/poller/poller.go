// Package poller fetches subscribed feeds, parses entries, and enqueues
// downstream article fetch/extract jobs.
package poller

import (
	"strconv"
	"time"
)

const (
	minPollInterval     = 4 * time.Hour
	maxPollInterval     = 24 * time.Hour
	defaultPollInterval = 12 * time.Hour
)

// nextPoll calculates the next poll time based on the Retry-After header,
// clamping to the configured min/max window.
func nextPoll(now time.Time, retryAfter string, _ interface{}) time.Time {
	interval := defaultPollInterval
	if retryAfter != "" {
		if seconds, err := strconv.Atoi(retryAfter); err == nil {
			interval = time.Duration(seconds) * time.Second
		}
		// Clamp to valid poll window.
		if interval < minPollInterval {
			interval = minPollInterval
		}
		if interval > maxPollInterval {
			interval = maxPollInterval
		}
	}
	return now.Add(interval)
}
