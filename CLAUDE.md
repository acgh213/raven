# ✧ raven — agent notes ✧

Raven is a private, phone-first RSS reader. Its central law is **ranking and annotation, never deletion**. The Go service is the source of truth; Android is an offline-capable client.

## commands

```sh
go test ./...
go vet ./...
go build ./cmd/raven
```

Android commands will be added after an Android SDK is installed and the client module exists.

## non-negotiables

1. Use TDD: add a failing behavioral test, observe the expected failure, implement minimally, then run the full suite.
2. Keep the service single-user and Tailscale-only in v0.1. Do not add fake multi-tenancy.
3. Preserve article access. Do not add opaque filtering.
4. Treat URLs and feed/article HTML as hostile input. Fetch only through `internal/fetcher`.
5. Use durable SQLite jobs, never a restart-fragile in-memory background queue.
6. Store timestamps as UTC RFC3339Nano text.
7. Client activity is append-only/idempotent; `article_state` is derived state.
8. Derived model output must carry version/provenance. Do not add model calls in Phase 1.
9. Do not log tokens, raw article bodies, or chat content in normal logs.

## intended layout

```text
cmd/raven/               process entrypoint
internal/config/         config parsing
internal/db/             SQLite setup + embedded migrations
internal/model/          domain + wire contracts
internal/store/          transactional persistence
internal/fetcher/        safe HTTP fetches
internal/extractor/      extraction adapters
internal/jobs/           durable background worker
internal/app/            HTTP transport and wiring
android/                 Compose + Room client (later in phase 1)
docs/adr/                architectural decisions
docs/benchmarks/         phase 0 methodology/results
testdata/                deterministic feed/article fixtures
```

Read [DESIGN.md](DESIGN.md) before changing any core path. ADRs overrule an attractive implementation shortcut. Update docs when a decision changes; stale architecture docs are worse than no architecture docs.
