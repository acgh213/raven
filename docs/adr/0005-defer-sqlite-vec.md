# ADR 0005: defer sqlite-vec while retaining modernc.org/sqlite

**status:** accepted for Phase 1; revisit before Phase 2

Raven keeps `modernc.org/sqlite` for the Phase 1 reader and does not ship sqlite-vec yet.

## evidence

On 2026-07-11, a fresh compatibility spike used the `database/sql` driver from `github.com/ncruces/go-sqlite3` v0.17.1 with `github.com/asg017/sqlite-vec-go-bindings/ncruces` v0.1.6. The real query:

```sql
SELECT sqlite_version(), vec_version()
```

failed at runtime:

```text
i32.atomic.store invalid as feature "" is disabled
```

The sqlite-vec Go binding’s CGO mode explicitly does not support modernc. Its WASM/ncruces mode is not presently a viable replacement in this environment. A baseline ncruces `database/sql` program *without* sqlite-vec did run successfully, so the failure is the vector binding combination—not the basic driver path.

## consequence

Phase 1 continues with pure-Go modernc SQLite and versioned embedding fields are deferred until actual vectors are introduced. Before Phase 2, re-run the spike against a maintained sqlite-vec/ncruces binding. If vectors become urgent first, evaluate the explicit operational tradeoff of switching to CGO (`mattn/go-sqlite3` + sqlite-vec) rather than silently forcing a fragile driver migration.
