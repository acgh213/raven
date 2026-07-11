# sqlite-vec compatibility spike — 2026-07-11

## question

Can Raven’s planned pure-Go SQLite setup use sqlite-vec now?

## verified result

No—not safely with the tested binding combination.

A temporary module used:

- `github.com/ncruces/go-sqlite3` `v0.17.1`
- `github.com/asg017/sqlite-vec-go-bindings/ncruces` `v0.1.6`
- the ncruces `database/sql` driver

The program opened `:memory:` and executed:

```sql
SELECT sqlite_version(), vec_version()
```

It actually ran and failed with:

```text
i32.atomic.store invalid as feature "" is disabled
```

A baseline ncruces program without the sqlite-vec binding successfully returned `sqlite_version=3.46.0`. The current Raven driver (`modernc.org/sqlite`) cannot use the binding’s CGO mode by design.

## decision

See [ADR 0005](../adr/0005-defer-sqlite-vec.md). Raven stays on modernc for Phase 1; vector integration is deferred to a Phase 2 re-evaluation rather than corrupting the reader foundation for a feature it does not need yet.
