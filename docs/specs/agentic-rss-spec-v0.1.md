# Agentic RSS Reader — Design Spec v0.1

> **Historical product-spec snapshot.** This is the original design document that established Raven's product direction. Its working-name language predates the decision to call the project **Raven**. Where it conflicts with later implementation decisions, follow the accepted ADRs and the Phase 0/1 service plan—especially [ADR 0005](../adr/0005-defer-sqlite-vec.md), which records why vector storage is deferred beyond Phase 1.

*Working name: TBD (see Open Decisions). Referred to as "the reader" throughout.*

## 1. Concept

A self-hosted, phone-first RSS reader where an agent acts as **librarian, not editor**. It ingests full article content from subscribed feeds, enriches and organizes everything, learns the reader's interests over time, and supports contextual conversation about whatever is on screen — but it never hides or discards content. The user always retains a scrollable, browsable feed with full choice over what to read.

Explicit anti-goals (the Kagi News failure modes being avoided):

- No single synthesized pane replacing the feed.
- No fixed 24h refresh cycle — feeds poll on their natural cadence.
- No opaque filtering. Ranking and annotation, never deletion.

### Pillars

1. **Ingest everything, enrich everything.** Full-text extraction at ingest; summaries, tags, entities, clusters, relevance scores computed server-side.
2. **The feed is sacred.** Ranked and annotated, cluster-collapsed, filterable — but complete.
3. **Conversation is contextual.** Chat is anchored to the article/topic/source currently in view, via a bottom sheet. Context assembly is server-side and provider-agnostic.
4. **The agent has memory.** A transparent interest profile with provenance, durable conversational memory, and standing watches.
5. **Models are plumbing.** A provider router abstracts local and remote models; roles have fallback chains; chat model is switchable mid-conversation.

## 2. System Architecture

```
┌─────────────────────────────┐
│   Compose app (Android)     │  thin client
│   feed / article / chat /   │
│   editions / settings       │
└──────────┬──────────────────┘
           │ HTTPS over Tailscale
┌──────────▼──────────────────┐
│   Server (Go, homelab VM)   │
│                             │
│  ┌────────┐  ┌───────────┐  │
│  │ Poller │→ │ Extractor │  │
│  └────────┘  └─────┬─────┘  │
│                    ▼        │
│              ┌──────────┐   │
│              │ Enricher │   │
│              └────┬─────┘   │
│                   ▼         │
│   SQLite + sqlite-vec       │
│                   ▲         │
│  ┌──────────┐ ┌───┴──────┐  │
│  │ Digest   │ │ Chat     │  │
│  │ scheduler│ │ orchestr. │  │
│  └──────────┘ └───┬──────┘  │
│                   ▼         │
│        ┌────────────────┐   │
│        │ Provider router│   │
│        └───┬──┬──┬──┬───┘   │
└────────────┼──┼──┼──┼───────┘
             ▼  ▼  ▼  ▼
     OpenRouter  Hermes  LM Studio  (direct keys
                                     from the pi)
```

- **Client:** Android, Kotlin/Compose, Room for offline cache. Thin: all intelligence server-side. Desktop later = second client or web view against the same API.
- **Server:** Go. Single binary preferred. Runs on Proxmox VM/CT. Tailscale-only transport for v1 (no public exposure).
- **Storage:** SQLite + sqlite-vec for embeddings. Raw HTML retained alongside extracted text.
- **The API is the product.** App, digest generator, and any future desktop client are all consumers of the same API.

## 3. Ingestion & Enrichment Pipeline

### Poller
- Per-feed polling interval, respecting feed hints (TTL, sy:updatePeriod) with sane min/max clamps.
- Conditional GET (ETag / Last-Modified) to be a good citizen.
- OPML import to seed subscriptions.

### Extractor
- Fetch canonical article URL; readability pass (go-readability or trafilatura via sidecar) → clean text + lead image.
- Store: raw HTML, extracted text, metadata (author, published, canonical URL, word count).
- Failure fallback: RSS-provided summary/content, flagged as `extraction_failed` so chat context knows it's working from a snippet.

### Enricher (LLM role: `enrich`)
Runs per-article after extraction, batched where possible:
- **Summary** (2–4 sentences).
- **Topic tags** (controlled-ish vocabulary that grows over time).
- **Entities** (people, orgs, products, places).
- **Embedding** for the article (stored in sqlite-vec).
- **Cluster assignment:** embedding similarity + entity overlap groups same-story coverage across outlets into one cluster with member articles.
- **Relevance score** against the interest profile (weighted topic/source/entity match; embedding similarity to profile centroid as a component).
- **Watch matching:** check article against active standing watches (see §7); hits create watch events.

Enrichment is designed to run on cheap/local models. Quality bar: adequate, consistent, high-volume.

## 4. Model Layer

### Provider registry
Config-driven (YAML, server-side for v1):

```yaml
providers:
  - name: openrouter
    type: openai-compatible
    base_url: https://openrouter.ai/api/v1
    api_key_env: OPENROUTER_KEY
  - name: lmstudio
    type: openai-compatible
    base_url: http://<pc-tailscale-ip>:1234/v1
  - name: hermes
    type: hermes
    base_url: http://<hermes-host>:<port>
    # persona ALWAYS explicitly set per call; never rely on defaults
  - name: anthropic-direct
    type: openai-compatible   # or native adapter
    api_key_env: ANTHROPIC_KEY
  # ...other direct keys formerly living on the pi

roles:
  enrich:
    chain: [lmstudio/<model>, openrouter/<cheap-model>]
  chat:
    default: openrouter/<good-model>
    selectable: true          # exposed in app model picker
  digest:
    chain: [openrouter/<mid-model>, lmstudio/<model>]
```

### Router behavior
- **Health probing:** periodic + on-demand (short cache). LM Studio availability is dynamic (PC may be off); Hermes depends on homelab VM. The app's model picker reflects live availability; unreachable models shown greyed-out.
- **Fallback chains per role:** router walks the chain on failure/unavailability. Internet out → enrich falls back to local; PC off → enrich falls back to API; chat degrades gracefully (disabled with a clear reason) if nothing in its chain is reachable.
- **Roles:** `enrich` (high volume, cheap), `chat` (quality-sensitive, user-selectable per conversation), `digest` (scheduled, mid-tier). Per-role defaults configurable server-side in v1; settings UI later.

### Hermes handling
- Hermes adapter **always** passes an explicit persona/system override. Never rely on Hermes defaults — scaffold behavior must not bleed into reader chat.
- If a clean-slate call cannot be guaranteed for a given Hermes route, prefer hitting the underlying backend directly with its own key.
- **Opt-in feature:** distinct persona-bearing entries (e.g. `hermes/<backend>+<persona>`) may be registered as selectable chat models, enabling "discuss this article with a specific voice." Never a default.

### Prompt ownership
The system prompt and context assembly belong to **this app**, not to any provider. Switching chat models mid-conversation changes only the engine; voice, context discipline, and safety framing stay constant. This is also what makes cross-model comparison meaningful.

## 5. Data Model (sketch)

```
feeds        id, url, title, site_url, poll_interval, etag, last_modified,
             last_polled_at, status, folder/tag

articles     id, feed_id, guid, canonical_url, title, author, published_at,
             fetched_at, raw_html, extracted_text, summary, lead_image,
             word_count, extraction_status, cluster_id, relevance_score

article_tags     article_id, tag
article_entities article_id, entity_type, entity_name
embeddings       article_id, vector           (sqlite-vec)

clusters     id, representative_article_id, created_at, updated_at

read_state   article_id, state(unread/read/skipped), dwell_ms, opened_at,
             completed(bool), starred(bool)

interest_profile
             id, kind(topic/source/entity), key, weight,
             provenance(user_set / learned:<signal> / chat:<conv_id,date>),
             created_at, updated_at, active(bool)

conversations  id, anchor_type(article/cluster/source/topic/edition),
               anchor_id, model_used, created_at
messages       conversation_id, role, content, model, created_at

memories     id, content, kind(fact/stance/preference/followup),
             source_conversation_id, embedding, created_at, active(bool)

watches      id, description, matcher(embedding + entity/keyword terms),
             created_from(conv_id), created_at, active(bool)
watch_events watch_id, article_id, matched_at, surfaced(bool)

editions     id, slot(morning/midday/evening/night), generated_at,
             content(markdown/json), article_refs
```

## 6. API Surface (sketch)

```
GET    /v1/feeds                          list + status
POST   /v1/feeds                          add (URL or discovery)
DELETE /v1/feeds/{id}
POST   /v1/feeds/import                   OPML

GET    /v1/articles?sort=&filter=&cursor= river (cluster-collapsed option)
GET    /v1/articles/{id}                  full article + enrichment
GET    /v1/clusters/{id}                  cluster members
POST   /v1/articles/{id}/state            read/skip/star/dwell telemetry

GET    /v1/models                         available models + live health, per-role defaults
POST   /v1/conversations                  create (anchor_type, anchor_id, model?)
POST   /v1/conversations/{id}/messages    send message (SSE/stream response)
POST   /v1/conversations/{id}/model       switch model mid-conversation

GET    /v1/profile                        interest profile w/ provenance
PATCH  /v1/profile/{entry_id}             edit / revert / deactivate
GET    /v1/watches                        standing watches
PATCH  /v1/watches/{id}

GET    /v1/editions?slot=&date=           digest editions
GET    /v1/sync/offline                   batch payload for Room cache
```

Auth: Tailscale is the perimeter for v1; a simple bearer token as belt-and-suspenders.

## 7. Memory & Learning

### Interest profile (tier 1 — long-term taste memory)
- A real, inspectable document: entries of (kind, key, weight, **provenance**).
- Provenance is first-class: "you set this," "learned from 14 skips," "you said this in chat on <date>." Every learned entry is visible and revertible in the app. Debug view for when the feed gets weird.
- **Write paths / signal strength (strongest → weakest):**
  1. Explicit chat statements ("stop showing me crypto funding rounds")
  2. Chat engagement depth (long conversation about an article)
  3. Read completion / dwell time
  4. Opens/taps
  5. Skips — noisy (busy ≠ uninterested); decay weights slowly, never nuke topics
- v1 profile is hand-written; the learning loop arrives in Phase 4 and *proposes or applies* changes with provenance.

### Conversational memory (tier 2)
- Post-conversation extraction pass pulls durable facts, stances, and preferences into `memories` with embeddings.
- Retrieved at context-assembly time via vector search, scoped by relevance to the current anchor.

### Standing watches
- Created from chat ("tell me when there's movement on this") or manually.
- Matcher = embedding of the watch description + key entities/terms.
- Enricher checks each new article against active watches; hits surface as feed annotations, and in editions.
- This is the feature that makes it an agent rather than a reader with a chatbot.

## 8. App Design (Android, Compose)

### Surfaces
- **Feed (river):** scrollable list, cluster-collapsed by default (expandable), sort by recency or relevance, filter by topic/source/starred/watch-hits. Nothing hidden, ever.
- **Article view:** clean extracted text, lead image, summary card, cluster siblings row, source info.
- **Chat bottom sheet:** slides up over the current article/topic/source — context stays visually anchored. Model chip in the sheet header (tap to switch mid-conversation; shows live availability). **Selection-to-chat:** long-press a paragraph → "ask about this" → passage enters context.
- **Editions tab:** the 4x-daily digests as scrollable documents (morning/midday/evening/night), deep-linking into full articles and their conversations. History browsable.
- **Profile view:** interest profile with provenance, edit/revert; watches list.
- **Settings:** server URL/token, sync behavior, (later: per-role model defaults).

### Offline
- Aggressive article caching via Room (extracted text + summaries + lead images) through the `/v1/sync/offline` batch endpoint.
- Reading fully works offline. **Chat disables gracefully offline** (no queuing — hours-late replies feel wrong). Read-state telemetry queues and syncs on reconnect.

## 9. Digests / Editions

- Scheduler generates 4 editions daily (configurable slots), role `digest`.
- Input: enriched articles from the window, weighted by relevance, cluster-aware (one entry per story cluster), watch hits promoted.
- Output: a structured document (headline synthesis + per-story blurbs + links) stored as an edition, rendered natively in the Editions tab.
- Additional export sinks (email via existing infra, static HTML to Nextcloud, push notification with edition headline) are pluggable later; **in-app Editions is primary** pending decision (see Open Decisions).

## 10. Phases

Each phase ends with something used daily.

**Phase 1 — A reader that works (no LLM).**
Poller, extractor, SQLite schema, OPML import, Go API over Tailscale, Compose app with river/article view, Room offline cache, read-state telemetry (recorded even before anything consumes it). Feed management: minimum add/remove in-app; folders can wait.

**Phase 2 — Enrichment.**
Provider registry + router + YAML config, `enrich` role (local-first chain), summaries/tags/entities/embeddings/clusters, hand-written interest profile, relevance scoring. Feed gains cluster-collapse, relevance sort, topic filters.

**Phase 3 — Chat.**
Conversation API with server-side context assembly (article + cluster siblings + source history + profile slice + memory retrieval), bottom sheet UI, model chip with live availability, selection-to-chat, post-conversation memory extraction.

**Phase 4 — The agent.**
Learning loop writing to the profile with provenance (propose-then-approve initially), standing watches + matching, editions generation + Editions tab, profile/watch management UI.

Sequencing logic: chat precedes learning because chat is a signal source, and a corpus of real usage should exist before tuning weights.

## 11. Open Decisions

1. **Name.** Required before the Gitea repo condemns it to `rss-agent-thing/`. Direction discussed: herald / archivist / fetching-bird energy.
2. **Digest export scope.** Is "export" satisfied by in-app Editions, or is content *leaving the system* (email, Nextcloud drop) a v1 requirement? Current plan: Editions primary, sinks pluggable later.
3. **Enrich/digest model picks.** Which concrete models in each chain — needs a quick bake-off once Phase 2 scaffolding exists (cheap to test: run the same 20 articles through candidates, eyeball tags/summaries).
4. **Feed management depth in v1.** Add/remove in-app is planned; folders/organization can be Phase 1.5.
5. **Hermes persona entries.** Whether to register any persona-bearing chat models at launch or leave the adapter capability dormant until wanted.

## 12. Risks & Mitigations

- **Extraction quality variance** → keep raw HTML, flag failures, allow per-feed extraction overrides later.
- **Hermes default bleed-through** → explicit persona override on every call; prefer direct backend keys when clean-slate can't be guaranteed.
- **Skip-signal misinterpretation** → slow decay, provenance-visible learning, revert affordances.
- **LM Studio unavailability** → fallback chains; health-aware picker.
- **Scope creep in Phase 1** → the reader must ship boring and solid before any model touches it.
