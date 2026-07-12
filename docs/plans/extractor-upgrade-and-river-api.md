# Extractor Upgrade + Article River API — Plan

Status: **in-progress**

## Slice 1: Upgrade extractor to go-readability

Replace the stdlib HTML tokenizer with `go-shiori/go-readability`, which handles
boilerplate removal, main content extraction, and produces much cleaner output.

**Keep the same interface:** `Extract(rawHTML []byte) (Result, error)`

**Additional field:** `Title` — go-readability extracts the article title from `<title>` or `<h1>`.

## Slice 2: Article river API

### Endpoints

```
GET /v1/articles
  Query params:
    feed_id    — filter by feed (optional)
    cursor     — opaque cursor for pagination (optional)
    limit      — max results (default 20, max 100)
  Response: { articles: [...], next_cursor: "..." | null }

GET /v1/articles/:id
  Response: { article: { ...full detail with extracted_text } }
```

### Design decisions
- Cursor-based pagination using `published_at` + `id` (compound cursor, stable ordering)
- Cursors are base64-encoded JSON: `{"published_at":"...","id":"..."}`
- `GET /v1/articles/:id` returns the full article including `extracted_text` from the latest completed content version
- Always-array collections, snake_case JSON

### Tasks

**1. Extractor upgrade**
- Add `github.com/go-shiori/go-readability` dependency
- Rewrite `internal/extractor/extractor.go` to use readability
- Add `Title` field to `Result`
- Update tests for new behavior

**2. Article store — ListArticles + GetArticle**
- `ListArticles(ctx, feedID, cursor, limit) ([]Article, nextCursor, error)`
- `GetArticle(ctx, id) (ArticleWithContent, error)` — joins article + latest content version

**3. Article river handler**
- `GET /v1/articles` — parse params, call store, format response
- `GET /v1/articles/{id}` — path-based lookup
- JSON contract: snake_case, always-array, cursor envelope

**4. Wire into app.go**
- Register `/v1/articles` and `/v1/articles/` routes
- Add ArticleList service to Config