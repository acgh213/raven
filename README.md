# ✧ raven ✧

*a librarian for your feeds, not an editor of them.*

Raven is a private, phone-first RSS reader for a person who wants their whole feed back. It fetches and preserves articles, makes them clean to read offline, and eventually adds summaries, clustering, contextual conversation, transparent learning, watches, and editions—without silently deciding that some of the feed does not deserve to exist.

## why

Most “AI news” products replace a reader’s judgment with a polished pane of selected material. Raven does the opposite: the river is sacred. It can be ranked, annotated, grouped, filtered, and discussed, but every ingested article remains reachable.

## what ships first

- RSS/Atom polling with conditional requests
- readable full-text extraction with original-source fallback
- a durable local corpus in SQLite
- an Android reader with an offline cache
- read/star telemetry that syncs safely after reconnecting

Models are explicitly **not** required for Raven to be useful. Enrichment comes after the reader earns daily use.

## architecture

```text
Android (Compose + Room)
          │ Tailscale HTTPS + bearer token
          ▼
Raven (Go single binary)
  poll → fetch → extract → SQLite → JSON API / sync
          │
          └── later: enrichment, chat, watches, editions
```

## principles

1. ingest everything; hide nothing.
2. preserve provenance and explain system behavior.
3. treat models as replaceable plumbing.
4. make offline reading real, not decorative.
5. choose boring durable systems before clever ones.

See [DESIGN.md](DESIGN.md), [GOALS.md](GOALS.md), [PLAN.md](PLAN.md), and [docs/adr](docs/adr/) for the implementation contract.
