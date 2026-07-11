# ADR 0002: SQLite with a durable jobs table

**status:** accepted

Raven uses SQLite in WAL mode as its source of truth. Polling, fetching, extraction, enrichment, and scheduled work are represented by durable database jobs with idempotent dedupe keys, leases, retries, and dead-letter state.

A separate queue is not justified for a single-user service. In-memory goroutines alone are unacceptable because service restarts and transient provider failures would lose work or make failures uninspectable.
