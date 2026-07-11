// Package model defines domain records and wire contracts for Raven.
package model

import "time"

// Job status constants.
const (
	JobStatusPending  = "pending"
	JobStatusClaimed  = "claimed"
	JobStatusComplete = "completed"
	JobStatusFailed   = "failed"
	JobStatusDead     = "dead"
)

// Job represents a durable background work item.
type Job struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Payload     string `json:"payload"`
	Status      string `json:"status"`
	DedupeKey   string `json:"dedupe_key,omitempty"`
	LeaseID     string `json:"lease_id,omitempty"`
	LeasedUntil string `json:"leased_until,omitempty"` // RFC3339Nano
	RetryCount  int    `json:"retry_count"`
	MaxRetries  int    `json:"max_retries"`
	ScheduledAt string `json:"scheduled_at"` // RFC3339Nano
	LastError   string `json:"last_error,omitempty"`
	CreatedAt   string `json:"created_at"` // RFC3339Nano
	UpdatedAt   string `json:"updated_at"` // RFC3339Nano
}

// Clock provides time source for deterministic testing.
type Clock interface {
	Now() time.Time
}

// RealClock uses time.Now.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }
