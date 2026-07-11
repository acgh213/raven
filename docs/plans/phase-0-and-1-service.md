# Raven Phase 0 + Phase 1 service plan

> Implement in small, test-first commits. Every production behavior begins with a test observed failing for the intended reason. Run `go test ./...` and `go vet ./...` after each completed slice.

**Goal:** deliver a restart-safe, private RSS service that can acquire feeds/articles safely, extract readable text, preserve a corpus, and synchronize offline reader events.

**Architecture:** stdlib `net/http` service, `modernc.org/sqlite` database, embedded SQL migrations, and a transactional durable job queue. Internal packages own one responsibility each and are wired in `cmd/raven`.

## exact delivery order

### 0. Go module and build contract

**Files:** `go.mod`, `cmd/raven/main.go`, `.github/workflows/ci.yml` (or Forgejo workflow after remote convention is known).

1. Add a failing smoke test for service construction in `internal/app/app_test.go`.
2. Create the minimal main process and app constructor to pass it.
3. Add `go test ./...`, `go vet ./...`, and `go build ./cmd/raven` to local verification.

### 1. Configuration and health

**Files:** `internal/config/config.go`, `internal/config/config_test.go`, `internal/app/health.go`, `internal/app/health_test.go`.

1. Test defaults, required data directory, bearer token parsing, and invalid duration/address rejection.
2. Implement environment/config parsing without logging secrets.
3. Test `/healthz` success and its JSON contract.
4. Implement health route and request-ID middleware.

### 2. Database migration and backup foundation

**Files:** `internal/db/db.go`, `internal/db/db_test.go`, `internal/db/migrations/001_core.sql`, `internal/db/backup.go`, `internal/db/backup_test.go`.

1. Test fresh migration creates every Phase 1 table and records migration once.
2. Test second migration run is a no-op.
3. Test foreign keys are on and WAL/busy timeout are configured.
4. Test backup produces a restorable, queryable database.
5. Implement embedded migrations, connection setup, and SQLite online backup.

### 3. Domain contracts and durable jobs

**Files:** `internal/model/*.go`, `internal/store/jobs.go`, `internal/store/jobs_test.go`, `internal/jobs/worker.go`, `internal/jobs/worker_test.go`.

1. Test idempotent enqueue with a dedupe key.
2. Test a claim lease prevents a second worker from claiming the same job.
3. Test expired lease recovery, exponential retry scheduling, and dead-letter transition.
4. Implement stores and bounded worker loop.

### 4. Safe fetcher

**Files:** `internal/fetcher/fetcher.go`, `internal/fetcher/fetcher_test.go`, `internal/fetcher/policy.go`, `internal/fetcher/policy_test.go`.

1. Test reject non-HTTP(S), userinfo URLs, loopback/private/link-local/Tailscale/metadata addresses, redirect escape, oversize bodies, and excessive redirects.
2. Test ordinary public test-server fetch, conditional headers, response cap, and descriptive User-Agent.
3. Implement resolver-aware policy and bounded client.

### 5. Feeds, OPML, and polling

**Files:** `internal/model/feed.go`, `internal/store/feeds.go`, `internal/store/feeds_test.go`, `internal/feed/parser.go`, `internal/feed/parser_test.go`, `internal/poller/poller.go`, `internal/poller/poller_test.go`, `internal/app/feeds.go`, `internal/app/feeds_test.go`.

1. Test feed URL canonicalization, CRUD, and duplicate handling.
2. Test OPML preview/import and malformed OPML errors.
3. Test Atom/RSS entry parsing, conditional GET, next poll calculation, error state, and job enqueue.
4. Test JSON endpoints and auth behavior.
5. Implement the complete feed path.

### 6. Article identity, fetch, and content storage

**Files:** `internal/model/article.go`, `internal/store/articles.go`, `internal/store/articles_test.go`, `internal/article/identity.go`, `internal/article/identity_test.go`, `internal/article/worker.go`, `internal/article/worker_test.go`.

1. Test GUID identity first and canonical URL/hash fallback when GUID is absent.
2. Test repeat feed entries update metadata without duplicate articles.
3. Test article fetch persists source evidence and queues extraction.
4. Implement article/service storage and jobs.

### 7. Extraction adapter and benchmark runner

**Files:** `internal/extractor/extractor.go`, `internal/extractor/plaintext.go`, `internal/extractor/*_test.go`, `cmd/raven-benchmark/main.go`, `docs/benchmarks/results.md`.

1. Create fixture tests that define an extraction result and fallback status.
2. Start with a deterministic Phase 1 extraction adapter selected by benchmark; retain raw source and mark fallback explicitly.
3. Implement corpus runner outputting machine-readable results.
4. Evaluate candidate adapters against real user-supplied fixtures and record decision in a new ADR.

### 8. River/detail API

**Files:** `internal/app/articles.go`, `internal/app/articles_test.go`, `internal/store/articles.go`.

1. Test stable recency sort, opaque cursor pagination, article detail, missing article, and extraction caveat payload.
2. Implement contracts and JSON rendering.

### 9. Activity events and sync

**Files:** `internal/model/activity.go`, `internal/store/activity.go`, `internal/store/activity_test.go`, `internal/app/sync.go`, `internal/app/sync_test.go`.

1. Test event UUID idempotency and transactional materialized state updates.
2. Test star/unstar, read/skip, and dwell event behavior.
3. Test sync cursor, changed objects, tombstones, and duplicate upload acknowledgement.
4. Implement event ingest and sync endpoints.

### 10. Operational finish

**Files:** `internal/app/diagnostics.go`, `internal/app/diagnostics_test.go`, `docs/operations.md`, `config.example.yaml`, `systemd/raven.service`.

1. Test diagnostics require authentication and redact sensitive configuration.
2. Implement feed/job diagnostics.
3. Document deployment, Tailscale serve configuration, backup schedule, restore drill, retention, and routine verification.

## phase 1 server acceptance test

A test fixture feed is imported, polled into durable jobs, fetched safely, stored with extracted/fallback content, and visible through the river API. The process is restarted while work is leased; expired work resumes once without duplicated article records. A simulated offline device submits the same activity event twice and gets exactly one stored event/state transition. A database backup restores into a clean service and returns the same article.

## Android workstream

Android cannot begin until a supported JDK and Android SDK/Gradle toolchain are present on the development host or an Android build runner. Once available, create `android/` with Room-first local fixtures, token storage, cursor sync, WorkManager reconciliation, river/article Compose screens, and an emulator smoke test. Its API contract is defined by the server tests above; do not bind UI work to unstabilized handlers.
