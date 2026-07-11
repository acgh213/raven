# ✧ raven — delivery plan ✧

## phase 0 — evidence before architecture

- [ ] establish this repository, documentation, ADRs, and local CI commands
- [ ] assemble a real-feed benchmark corpus (20–50 representative entries)
- [ ] compare extraction candidates on quality, failure mode, speed, and deployment cost
- [ ] prove the SQLite driver/migration path and investigate sqlite-vec deployment
- [ ] set service configuration, Tailscale route, bearer token, backup target, and restore procedure
- [ ] write OpenAPI examples for the Phase 1 API before the app is coupled to it

**exit:** a clean host can build Raven, migrate a database, run benchmark fixtures, and restore a backup.

## phase 1 — reader foundation

### service

- [ ] configuration, logging, health, migrations, backup/restore
- [ ] SSRF-safe fetcher and feed URL validation
- [ ] feed CRUD and OPML import
- [ ] conditional polling and durable scheduled jobs
- [ ] entry identity/dedupe, article fetching, source retention, extraction
- [ ] article river/detail API with cursor pagination
- [ ] append-only reader events, materialized state, and cursor sync
- [ ] feed/job diagnostics and retention controls

### android

- [ ] Android toolchain and Compose app scaffold
- [ ] encrypted server/token configuration
- [ ] Room schema, sync client, background WorkManager worker
- [ ] river, article, and source-link screens
- [ ] local event queue and reconnect reconciliation
- [ ] OPML import plus minimal add/remove feed controls

**exit:** after a successful sync, the phone can lose all network access, still read its intended cached corpus, record activity, and upload each action exactly once after reconnecting.

## phase 1.5 — only after dogfooding

- [ ] folders/source controls
- [ ] typography and reader polish
- [ ] per-feed polling override and manual refetch

## phase 2–4

The roadmap in [GOALS.md](GOALS.md) is intentionally gated on actual use of the reader. No model work gets to distract from a broken fetch or sync loop.
