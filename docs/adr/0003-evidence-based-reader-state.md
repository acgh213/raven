# ADR 0003: append-only reader activity

**status:** accepted

The client records UUID-addressed activity events—open, dwell checkpoint, complete, skip, star, unstar—rather than only overwriting a read-state row. Raven derives a materialized `article_state` for river rendering in the same transaction.

This makes offline retries idempotent and retains evidence for later interest learning. A skip is not a durable preference declaration; Raven will only infer taste from repeated, explainable, cross-session evidence.
