# ✧ raven — design ✧

## purpose

Raven is a single-user RSS service and Android client. The server owns ingestion, content processing, durable storage, sync, and future model orchestration. The app is an offline-capable reader, not a second intelligence layer.

The system’s governing rule is simple: **ranking and annotation, never deletion.** An article may be clustered, deprioritized, marked read, or filtered by an explicit user control. It is never silently removed because a model thinks it knows better.

## scope boundary

v0.1 serves one reader on one tailnet. It has no user table, sharing, public web access, billing, or multi-tenant abstraction. IDs are stable UUIDs and API ownership is clean enough to add multi-user later, but Raven will not carry SaaS cosplay through its first useful release.

## service shape

```text
cmd/raven
  └─ config → database → stores/services → HTTP API
                       └→ durable job workers

poll_feed → fetch_feed → discover_entry → fetch_article → extract_article
                                                        └→ article / content version

Android Room ← cursor sync / event upload → SQLite source of truth
```

The Go service is one binary. SQLite runs in WAL mode with foreign keys and a busy timeout. A durable `jobs` table provides scheduled and retried background work; there is deliberately no external queue in v0.1.

## module boundaries

- `cmd/raven`: process startup and signal lifecycle only.
- `internal/config`: environment/file configuration parsing and validation.
- `internal/db`: connection setup and embedded SQL migrations.
- `internal/model`: domain records and request/response contracts.
- `internal/store`: transactional SQLite persistence, no HTTP concerns.
- `internal/fetcher`: SSRF-safe HTTP acquisition and response limits.
- `internal/extractor`: extraction interface and candidate adapters.
- `internal/jobs`: idempotent job enqueue, lease, retry, and workers.
- `internal/app`: HTTP routing, auth, JSON/SSE transport, dependency wiring.

## data policy

All persisted timestamps are UTC RFC3339Nano text. Raw HTML and extracted text are distinct content versions. Derived data—extraction, enrichment, embeddings, and clustering—records its engine/model/schema version and input content hash. Raven must be able to reprocess a corpus without mixing incompatible outputs.

Phase 1 core tables:

- `feeds`: normalized URL, conditional request headers, poll schedule, health/failure state.
- `articles`: stable entry identity, canonical URL, metadata, latest content reference.
- `article_content_versions`: raw source, extracted text, extraction status/version, word count, lead image.
- `activity_events`: append-only idempotent reader actions with device IDs.
- `article_state`: materialized read/star/dwell state used to render the river.
- `jobs`: durable background work with leases, retries, and dead-letter state.
- `_migrations`: applied migration ledger.

Future records for enrichment, profile, memories, conversations, watches, and editions live in later migrations. They do not belong in the Phase 1 code path.

## safe fetching

Every feed and article URL is untrusted input. Raven only fetches HTTP(S), rejects URL credentials, follows a limited redirect chain, and blocks loopback, link-local, private, Tailscale, and metadata-service targets before and after DNS/redirect resolution. It has strict dial/TLS/header/body/total timeouts, a response-size cap, host concurrency limits, and a Raven User-Agent.

Raw HTML is stored as source evidence; it is never inserted directly into an app WebView.

## activity and sync

The server is canonical. The client writes actions locally as UUID-addressed events, then posts them to `POST /v1/sync/events`. Duplicate event IDs are harmless. `GET /v1/sync?cursor=` delivers changes and tombstones with an opaque cursor. `article_state` is derived transactionally from events.

This separates evidence from presentation: a pattern-learning system can later see real behavior across sessions rather than infer philosophy from one skipped article.

## API contract

JSON endpoints are versioned under `/v1`. Lists use opaque cursors. Mutations accept idempotency keys. Errors have a stable JSON shape containing machine code, human message, retryability, and request ID. Bearer-token auth applies even on Tailscale; a small unauthenticated local health check is allowed only when explicitly configured.

Phase 1 endpoints:

- `GET /healthz`
- `GET|POST|DELETE /v1/feeds`
- `POST /v1/feeds/import`
- `GET /v1/articles`
- `GET /v1/articles/{id}`
- `POST /v1/articles/{id}/events`
- `GET /v1/sync`
- `POST /v1/sync/events`

## later model behavior

The application owns prompts, context selection, structured-output validation, and provenance. Background enrichment/digest roles may use configured fallbacks and record what actually ran. A user-selected chat model is never silently replaced by another model mid-conversation.

## operational contract

Migrations must be tested against an empty database and upgrade fixture. Daily SQLite backups and a restore drill are part of phase 1. Structured logs carry request/job IDs but never ordinary article bodies, tokens, or chat content. `/healthz` and diagnostics expose job lag, feed errors, and provider status without becoming a sprawling admin product.
