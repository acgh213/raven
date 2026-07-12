# Poller Phase 1 — Plan

Status: **in-progress**

## Goal

Wire the complete polling loop: enumerate feeds due for poll → fetch feed → parse entries →
upsert new articles → update feed metadata → schedule next poll. All driven through the existing
durable job infrastructure.

## Tasks (ordered, each test-first)

### 1. Feed store — `ListPollable` + `UpdatePollResult`

- `ListPollable(ctx)` queries active feeds where `last_polled_at IS NULL OR last_polled_at + poll_interval_seconds < now`. Returns `[]Feed` with all columns from the schema.
- `UpdatePollResult(ctx, feedID, etag, lastModified, errMsg)` updates `last_polled_at`, `etag`, `last_modified`, `error_count` (reset on success, increment on error), and `last_poll_error`.

### 2. Article store — `UpsertArticles`

- `UpsertArticles(ctx, feedID, entries)` inserts articles with `INSERT OR IGNORE` on `(feed_id, guid)`. Returns new articles, ignoring duplicates.
- Each new article also creates an initial `article_content_versions` row (status=pending).

### 3. Poller — `PollFeed(ctx, feedID) error`

- Loads feed metadata → fetches with SSRF-safe client → parses RSS/Atom → upserts articles → updates feed poll result.
- On HTTP 304 (conditional GET with etag/last-modified), updates poll time without parsing.
- Respects Retry-After header for next poll interval.

### 4. Job handler — `poll_feed`

- Worker handler registered under kind `"poll_feed"`.
- Payload: `{"feed_id": "..."}`.
- Calls `PollFeed`, then enqueues the next `poll_feed` job for this feed using `nextPoll()`.

### 5. Scheduler — startup seed

- On service startup, list all active feeds and enqueue a `poll_feed` job for each one that is due (or has never been polled).
- Dedupe key: `"poll_feed:<feed_id>"` so racing startups don't double-schedule.

### 6. Wire into app

- Update `Config` with Poller + JobStore.
- Register `"poll_feed"` handler.
- Start worker on service startup.
- Seed initial poll jobs.

## Verification

```bash
go test ./... -v -count=1  # all packages
go vet ./...               # clean
go build ./cmd/raven       # compiles
# Integration: start service, import OPML, observe poll jobs processed
```

## Out of scope (next slice)

- Article content extraction (fetch URL → extract text → store version)
- Conditional HTTP headers (If-None-Match / If-Modified-Since)
- Per-feed poll interval override from feed config
- HTTP endpoint to list articles
