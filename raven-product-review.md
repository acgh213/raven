# Raven Product/UX Review

**Date:** 2026-07-11
**Reviewer:** Hermes Agent (Product Audit Subagent)
**State:** 11 commits, Phase 0 documentation complete, Phase 1 scaffolding (config + db + jobs + health) implemented. All tests pass (`go test ./...` clean).

---

## 1. Product Vision: Coherent and Internally Consistent

**Score: 9/10 — exceptionally coherent.**

Raven has one of the clearest product theses I have seen in an early-stage project. Every document reinforces the same core idea:

> "ranking and annotation, never deletion"

This isn't a tagline. It's enforced at every layer:

- **Schema level:** `articles.is_deleted` exists but is explicitly a tombstone mechanism, not content removal. `activity_events` is append-only. `article_state` is derived, not source of truth.
- **ADR level:** ADR 0003 explicitly separates evidence (events) from presentation (materialized state) so that future learning can observe real behavior.
- **Code level:** The `jobs` table's `dead` status preserves failed work evidence rather than deleting it.
- **Non-goals level:** GOALS.md explicitly calls out "opaque model filtering or deletion" and "a synthesized pane that replaces the feed" as anti-patterns to avoid.
- **Operational level:** DESIGN.md mandates that raw HTML is preserved as "source evidence" and never inserted directly into a WebView.

The five pillars from the original spec (ingest everything, feed is sacred, contextual conversation, agent memory, models as plumbing) map cleanly to v0.1–v0.4 — nothing is orphaned or contradictory.

**Minor tension:** The original spec (§8, App Design) describes a "Chat bottom sheet" with "model chip in the sheet header" and "selection-to-chat" — quite detailed UI thinking. But GOALS.md gates chat at v0.3 and explicitly says "Models are explicitly not required for Raven to be useful." This is the right call philosophically, but the spec's UI detail for v0.3+ features creates a mild forward-design tension with the Phase 1 "no models" boundary. The spec doc handles this well with its header noting ADRs take precedence.

---

## 2. ADRs: Support and Constrain the Design

**Score: 8/10 — good scaffolding, some gaps.**

### Strengths

- **ADR 0001 (single-user tailnet):** Removes massive surface area. No user tables, no RBAC, no sharing. This is the most important constraint for v0.1 velocity and it's properly enforced.
- **ADR 0002 (SQLite + durable jobs):** The right call. The implementation proves it: `MaxOpenConns=1` with WAL, foreign keys, busy timeout, lease-based claims, idempotent dedupes, exponential backoff, dead-letter. Tests cover the critical paths (expired lease reclamation, wrong-lease rejection, race-avoidance for dedupe keys).
- **ADR 0003 (append-only activity):** Brilliant design decision. Separates evidence from presentation. The `activity_events` table with UUIDs + device IDs + event types (read/star/unstar/dwell/skip) enables offline reconciliation and future learning without losing fidelity. The `article_state` materialized view is correctly derived.
- **ADR 0004 (models are plumbing):** Properly constrains model integration to Phase 2+. The contract is clear: the application owns prompts, context, validation, and provenance. No model provider is silently swapped mid-conversation.
- **ADR 0005 (defer sqlite-vec):** Excellent engineering hygiene. They ran the spike, hit a real runtime failure (`i32.atomic.store invalid`), documented it, and made the disciplined call to defer rather than force a fragile migration. The re-evaluation gate before Phase 2 is well-placed.

### Gaps

- **No ADR for the extraction strategy.** The Phase 0 benchmark plan calls for comparing extraction candidates (go-readability, trafilatura via sidecar, etc.) and selecting the best whole-system result. This should produce an ADR before the extractor is implemented. Currently there's no ADR recording the decision, which means the "extraction engine/version" fields in `article_content_versions` exist without a documented selection criterion.
- **No ADR for the Android sync protocol.** DESIGN.md describes `POST /v1/sync/events` with cursor-based sync, but the details of conflict resolution, cursor semantics, offline queue behavior, and Room schema alignment deserve an ADR before the Android client is built. The original spec's `/v1/sync/offline` endpoint is mentioned but not yet addressed in ADRs.
- **No ADR for the API error shape.** DESIGN.md mandates "stable JSON shape containing machine code, human message, retryability, and request ID" but there's no ADR formalizing the error contract. The code currently has a `writeJSON` helper but no error-response implementation.
- **No ADR for the schema versioning / migration strategy.** The `_migrations` ledger and embedded SQL approach is solid, but forward/backward compatibility for schema changes (especially once Android Room has its own schema) is undecided.

---

## 3. Phase Sequencing: Logical and Risk-Aware

**Score: 9/10 — well-gated, practical ordering.**

The phase structure is one of the strongest aspects of this project:

```
Phase 0: Evidence before architecture (docs, benchmarks, ADRs, CI)
Phase 1: Reader foundation (no models, just fetch/extract/sync/read)
Phase 1.5: Dogfooding polish (folders, typography, per-feed overrides)
Phase 2: Enrichment (summaries, tags, clusters, embeddings)
Phase 3: Contextual conversation (chat anchored to articles)
Phase 4: Accountable agency (learning, watches, editions)
```

**Why this works:**

1. **Phase 0 is a real gate, not a checkbox.** The benchmark corpus methodology, sqlite-vec spike, and extraction candidate comparison are actual engineering work that prevents premature architectural commitments. The sqlite-vec spike already paid off — it caught a real runtime failure before it corrupted the Phase 1 foundation.

2. **Phase 1 delivers daily utility without models.** This is the anti-Kagi-News thesis made concrete. A reader that fetches, extracts, syncs, and works offline is already more useful than most "AI readers" that silently filter content. The exit gate — "phone can lose all network access, still read cached corpus, record activity, and upload each action exactly once after reconnecting" — is a genuine quality bar, not a completion checkbox.

3. **Phase 1.5 exists.** Many projects skip the "actually use it for a while" step. Raven explicitly gates model work behind dogfooding. This is the right call and prevents the common failure mode where enrichment features distract from a broken fetch/sync loop.

4. **Phase 2→3→4 sequencing is sound.** Enrichment (tags, clusters, relevance) before chat makes sense — chat needs something to chat about. Chat before agency makes sense — chat is a signal source for learning. The spec explicitly notes: "Sequencing logic: chat precedes learning because chat is a signal source, and a corpus of real usage should exist before tuning weights."

5. **The PLAN.md exit gates are measurable.** "A clean host can build Raven, migrate a database, run benchmark fixtures, and restore a backup" for Phase 0. "After a successful sync, the phone can lose all network access, still read its intended cached corpus, record activity, and upload each action exactly once after reconnecting" for Phase 1.

**One concern:** The Phase 1 service plan is detailed (10 implementation slices with test-first ordering), but the Android workstream is described in a single paragraph. For a "phone-first" product, this asymmetry is notable. The plan acknowledges this is blocked on toolchain availability, but the Android implementation surface (Room schema, Compose screens, WorkManager reconciliation, offline queue, OPML import, encrypted token storage) is substantial and deserves its own detailed plan.

---

## 4. Gaps: Promises vs. Current Scaffolding

**Score: 6/10 — expected at this stage, but gaps are real and large.**

### What exists (11 commits, all tests passing):

| Component | Status | Quality |
|-----------|--------|---------|
| Repository + docs | ✅ Complete | Excellent — 8 docs, 5 ADRs, benchmark methodology |
| Config (`internal/config`) | ✅ Implemented | Good — env parsing, validation, token redaction, 6 tests |
| SQLite + migrations (`internal/db`) | ✅ Implemented | Strong — WAL, FK, busy timeout, backup, 7 core tables, 9 tests |
| Durable jobs (`internal/store/jobs.go`) | ✅ Implemented | Excellent — idempotent enqueue, lease claims, lease recovery, exponential backoff, dead-letter, race-safe dedupe, 9 tests |
| Worker (`internal/jobs/worker.go`) | ✅ Implemented | Solid — handler dispatch, concurrency limit, context cancellation, unknown-kind handling, 5 tests |
| Drain loop (`internal/jobs/drainloop.go`) | ✅ Implemented | Good — context-aware, 3 tests |
| Health endpoint (`internal/app`) | ✅ Implemented | Good — GET-only `/healthz`, request-ID middleware, `writeJSON` helper, 6 tests |
| Build + test contract | ✅ Working | `go test ./...` and `go build ./cmd/raven` both pass |

### What's missing (Phase 1 incomplete):

| Component | Spec Reference | Gap |
|-----------|---------------|-----|
| **Fetcher** (`internal/fetcher/`) | DESIGN.md §"safe fetching", Phase 1 plan §4 | Not started: SSRF-safe HTTP client, URL validation, redirect safety, response caps |
| **Extractor** (`internal/extractor/`) | DESIGN.md §"module boundaries", Phase 1 plan §7 | Not started: extraction interface, adapters, benchmark runner |
| **Feed CRUD + OPML** | DESIGN.md §"API contract", GOALS.md v0.1 | Not started: `internal/store/feeds.go`, `internal/feed/parser.go`, `internal/poller/`, `internal/app/feeds.go` |
| **Article identity + storage** | DESIGN.md §"data policy", Phase 1 plan §6 | Not started: `internal/model/article.go`, `internal/store/articles.go`, GUID dedup |
| **River/detail API** | DESIGN.md §"API contract", Phase 1 plan §8 | Not started: `/v1/articles`, cursor pagination |
| **Activity events + sync** | ADR 0003, Phase 1 plan §9 | Not started: `POST /v1/sync/events`, `GET /v1/sync`, materialized state |
| **Android client** | Phase 1 plan "Android workstream" | Not started: no `android/` directory, no toolchain |
| **Benchmark corpus** | `docs/benchmarks/README.md` | `testdata/articles/manifest.json` is empty (`"articles": []`) |
| **Bearer token auth** | DESIGN.md §"API contract" | Not implemented: no auth middleware, `RAVEN_API_TOKEN` parsed but unused |
| **Backup documentation** | DESIGN.md §"operational contract" | Backup code exists but `docs/operations.md`, `config.example.yaml`, `systemd/raven.service` not created |
| **ADRs for extraction/sync/errors** | Phase 0 exit criteria | Missing: extraction selection, sync protocol, error contract |

### Specific blockers:

1. **Auth middleware is the highest-priority gap.** The DESIGN.md says "Bearer-token auth applies even on Tailscale," but `app.go` has no auth middleware. The Phase 1 service plan doesn't explicitly list auth middleware as a slice — it's mentioned in the feed endpoint slice (§5.4: "Test JSON endpoints and auth behavior") but the plan doesn't have a dedicated "add auth middleware" step.

2. **The empty benchmark corpus blocks the extractor decision.** The benchmark README says to populate 20–50 articles from "the feeds Cassie actually reads," but `manifest.json` is empty. Without this corpus, the extraction candidate comparison can't proceed, and the extractor choice remains undecided.

3. **No model directory for domain types beyond Job.** `internal/model/` contains only `job.go`. The migration creates tables for feeds, articles, article_content_versions, activity_events, and article_state, but no Go domain types exist for any of them yet.

---

## 5. Aesthetic / Product Thesis: What Does This Code Want to Be?

Raven wants to be a **librarian, not an editor**. This is the defining aesthetic.

The product thesis is explicitly oppositional: it positions itself against "Kagi News" (and by extension, Artifact, Bulletin, and other AI summarizers that replace feeds with curated panes). This gives it a clear identity and target audience.

**The code itself embodies this thesis through specific, defensible choices:**

1. **"Boring" as a virtue.** The CLAUDE.md mandates "choose boring durable systems before clever ones." SQLite instead of Postgres. No external queue. No microservices. One Go binary. This is a deliberate rejection of resume-driven architecture.

2. **Evidence over inference.** ADR 0003's append-only activity events, the `article_content_versions` table with extraction provenance, and the `content_hash` field all say: "we will know exactly what happened, not guess." This is the philosophical opposite of opaque model filtering.

3. **Phone-first as constraint.** The Android client is the primary interface. Server is a single binary on a homelab VM. Tailscale-only transport. Offline reading is a first-class requirement ("make offline reading real, not decorative"). These constraints force simplicity.

4. **Models as replaceable plumbing.** ADR 0004 and the spec's provider registry design treat models as interchangeable utilities. The application owns prompts, context, validation, and provenance. This is a mature architectural stance that most "AI wrapper" products miss.

5. **Provenance as product feature.** The interest profile design (spec §7) with first-class provenance ("you set this," "learned from 14 skips," "you said this in chat on <date>") turns model transparency into a user-facing value. This is rare and defensible.

**What this means for UX:** The reader will feel deliberate, not magical. It will show its work. It will be more like a well-organized library with a helpful librarian than a feed curated by an inscrutable algorithm. The target user is someone who wants to *read more*, not *decide less*.

---

## 6. Feature Surface Completeness vs. Stated Goals

**Score: 5/10 for current implementation, 9/10 for design completeness.**

### v0.1 goals (GOALS.md) vs. implementation:

| Goal | Design | Code |
|------|--------|------|
| Import OPML, add/remove feed URL | ✅ API defined | ❌ Not started |
| Poll RSS/Atom safely | ✅ DESIGN.md safe-fetching section | ❌ Not started |
| Retain source HTML, extract readable content | ✅ Schema has `article_content_versions` | ❌ Not started |
| Cursor-paginated river + article API | ✅ API contract defined | ❌ Not started |
| Durable jobs through restart/retry | ✅ ADR 0002, schema | ✅ Implemented |
| Sync idempotent reading events | ✅ ADR 0003, schema, API contract | ❌ Not started |
| Android Compose reader with Room | ✅ Spec §8, Phase 1 plan mention | ❌ Not started |
| Read cached articles completely offline | ✅ Design intent clear | ❌ Not started |
| Restore backup into working service | ✅ Backup code exists | Partial — code works, no documented restore drill |

**Current completion of v0.1 feature surface: ~20%.** The infrastructure (config, db, jobs, health) is solid, but the actual reader functionality is entirely unbuilt.

### Design completeness of all versions (v0.1–v0.4):

The design is thorough. The original spec covers ingestion, enrichment, chat, memory, watches, editions, and the Android UI. The ADRs constrain critical decisions. The migration schema has all Phase 1 tables plus `extraction_engine`/`extraction_version`/`content_hash` fields ready for Phase 2. The `article_content_versions.is_latest` pattern enables re-extraction without data loss.

**Notable thoroughness:** The spec even covers edge cases like "Hermes default bleed-through" (§5 Hermes Handling, §12 Risks) and "skip-signal misinterpretation" (§7 Write Paths). These show deep thinking about failure modes.

---

## 7. Non-Goals: Properly Scoped and Enforced?

**Score: 9/10 — well-scoped and well-enforced.**

GOALS.md non-goals:
1. **"A synthesized pane that replaces the feed"** — Enforced by: the river API design (scrollable, filterable, nothing hidden), the `article_state` derived-from-events pattern (can always reconstruct), and the absence of any "digest as primary view" in Phase 1.
2. **"Opaque model filtering or deletion"** — Enforced by: ADR 0003 (append-only events), the `is_deleted` tombstone pattern rather than actual deletion, and the explicit "no model calls in Phase 1" rule in CLAUDE.md.
3. **"A mandatory cloud account"** — Enforced by: ADR 0001 (single-user tailnet, no user table), Tailscale-only transport, self-hosted binary.
4. **"A public news service"** — Enforced by: Tailscale perimeter, bearer token auth, no public endpoints except optionally healthz.
5. **"Multi-user permissions in v0.1"** — Enforced by: ADR 0001 explicitly rejects SaaS cosplay. The CLAUDE.md non-negotiable #2 says "Do not add fake multi-tenancy."
6. **"Making model use a prerequisite for basic reading"** — Enforced by: Phase 1 explicitly excludes models. CLAUDE.md non-negotiable #8: "Do not add model calls in Phase 1."
7. **"Building four editions full of filler because a clock happened to ring"** — Enforced by: Editions are deferred to v0.4, and the spec's thin-window suppression design explicitly addresses this anti-pattern.

**The scaffolding enforces these well:**

- The `handlers := map[string]Handler{}` in `main.go` is intentionally empty — there's literally no way for unplanned work to run.
- The schema has no user/account/auth tables — ADR 0001 is physically enforced.
- The `modernc.org/sqlite` dependency blocks sqlite-vec (per ADR 0005), preventing premature embedding integration.
- The `go.mod` has zero model/LLM dependencies — no temptation to "just add a quick summarization."
- The plan's test-first ordering forces each feature to justify itself through behavioral tests before implementation.

**One minor gap:** The CLAUDE.md non-negotiable #9 says "Do not log tokens, raw article bodies, or chat content in normal logs." This is a good constraint, but there's currently no structured logging discipline in the code — `slog.Info` and `slog.Error` are used directly. As article fetching and extraction are implemented, this constraint will need active enforcement (e.g., a logging wrapper that redacts sensitive fields).

---

## Summary Assessment

| Dimension | Score | Key Takeaway |
|-----------|-------|-------------|
| Vision coherence | 9/10 | Exceptionally clear. Every document reinforces the same thesis. |
| ADR quality | 8/10 | Strong constraints, good evidence. Missing ADRs for extraction strategy, sync protocol, and error contract. |
| Phase sequencing | 9/10 | Well-gated, risk-aware, with a dogfooding phase. Android plan needs more detail. |
| Implementation gap | 6/10 | Infrastructure solid. Reader functionality entirely unbuilt. Expected at 11 commits. |
| Product thesis | 9/10 | "Librarian, not editor" — coherent, defensible, oppositional positioning. |
| Feature completeness | 5/10 | ~20% of v0.1 implemented. Design is thorough for all versions. |
| Non-goal enforcement | 9/10 | Physically enforced by schema, ADRs, and empty handler map. Logging discipline needs attention. |

**Overall:** Raven is a thoughtfully designed project with a clear product identity and excellent engineering discipline. The documentation is unusually complete for an 11-commit codebase. The 5 ADRs make defensible, evidence-backed decisions. The Phase 0/1 plan is practical and test-first. The primary risk is execution velocity — there's a lot of reader functionality to build before the product becomes daily-useful, and the Android client doubles the surface area. But the architectural decisions made so far (single binary, SQLite, durable jobs, append-only events, no models in v0.1) are all velocity-enabling, not velocity-destroying. If the team maintains this discipline through the fetcher/extractor/API implementation, Raven will deliver on its "boring and solid" promise.
