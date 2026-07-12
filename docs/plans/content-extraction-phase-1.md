# Article Content Extraction — Plan

Status: **in-progress**

## Goal

When a poll discovers new articles, fetch the full article HTML, extract readable
text, and store it as a completed content version. This turns bare metadata (title,
URL) into readable content the client can display.

## Flow

```
poll_feed → discover articles → content versions (pending)
  → enqueue fetch_article jobs
  → worker fetches article URL (SSRF-safe)
  → extract text from HTML
  → update content version → completed
```

## Tasks

### 1. Extractor — `internal/extractor/extractor.go`
- `Extract(rawHTML []byte) (text string, wordCount int, leadImageURL string, err error)`
- Uses `golang.org/x/net/html` tokenizer (stdlib, no new deps)
- Strips `<script>` and `<style>` blocks
- Extracts text from `<body>`, collapses whitespace
- Finds first `<img>` with non-trivial src as lead image
- Counts space-delimited words

### 2. Article store — `UpdateContentVersion` + `ListPendingForFeed`
- `UpdateContentVersion(ctx, versionID, rawHTML, extractedText, wordCount, leadImageURL, contentHash, engine, version, status)` — updates a content version row
- `ListPendingForFeed(ctx, feedID) ([]ContentVersion, error)` — finds articles with pending extraction for a feed

### 3. fetch_article handler — `internal/handler/extract.go`
- Job kind: `"fetch_article"`
- Payload: `{"article_id": "...", "content_version_id": "...", "article_url": "..."}`
- Fetches article URL (SSRF-safe fetcher)
- Calls extractor
- Updates content version (completed or failed)

### 4. Wire into poll handler
- After a successful poll, list pending content versions for the feed
- Enqueue a `fetch_article` job for each (dedupe key: `"fetch_article:<content_version_id>"`)

### 5. Wire into main.go
- Register `"fetch_article"` handler
- Pass article store to poll handler

## Out of scope
- Readability extraction, boilerplate removal, multi-engine comparison
- Image downloading, lead image thumbnails
- Content diffing/re-versioning on subsequent fetches
