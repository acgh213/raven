-- 002_feed_import_idempotency.sql: replay-safe OPML import mutations.
-- A key is scoped to this operation so future mutation endpoints can reuse the
-- same table without colliding with feed-import requests.

CREATE TABLE IF NOT EXISTS idempotency_keys (
    operation     TEXT NOT NULL,
    key           TEXT NOT NULL,
    request_hash  TEXT NOT NULL,
    response_json TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    PRIMARY KEY (operation, key)
);
