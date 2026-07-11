-- 001_core.sql: Phase 1 foundation tables.
-- See DESIGN.md for the schema contracts.
-- Note: _migrations ledger is created by Migrate before this file runs.

CREATE TABLE IF NOT EXISTS feeds (
    id                   TEXT PRIMARY KEY,
    feed_url             TEXT NOT NULL UNIQUE,
    title                TEXT NOT NULL DEFAULT '',
    site_url             TEXT NOT NULL DEFAULT '',
    etag                 TEXT NOT NULL DEFAULT '',
    last_modified        TEXT NOT NULL DEFAULT '',
    poll_interval_seconds INTEGER,
    last_polled_at       TEXT,
    last_poll_error      TEXT,
    is_active            INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
    error_count          INTEGER NOT NULL DEFAULT 0 CHECK (error_count >= 0),
    created_at           TEXT NOT NULL,
    updated_at           TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_feeds_active_poll
    ON feeds(is_active, poll_interval_seconds, last_polled_at);

CREATE TABLE IF NOT EXISTS articles (
    id                       TEXT PRIMARY KEY,
    feed_id                  TEXT NOT NULL REFERENCES feeds(id),
    guid                     TEXT NOT NULL,
    url                      TEXT NOT NULL DEFAULT '',
    title                    TEXT NOT NULL DEFAULT '',
    author                   TEXT NOT NULL DEFAULT '',
    published_at             TEXT,
    latest_content_version_id TEXT,
    is_deleted               INTEGER NOT NULL DEFAULT 0 CHECK (is_deleted IN (0, 1)),
    created_at               TEXT NOT NULL,
    updated_at               TEXT NOT NULL,
    UNIQUE(feed_id, guid),
    FOREIGN KEY (latest_content_version_id) REFERENCES article_content_versions(id)
        DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX IF NOT EXISTS idx_articles_feed_id
    ON articles(feed_id);

CREATE INDEX IF NOT EXISTS idx_articles_published_at
    ON articles(published_at);

CREATE TABLE IF NOT EXISTS article_content_versions (
    id                 TEXT PRIMARY KEY,
    article_id         TEXT NOT NULL REFERENCES articles(id),
    raw_html           TEXT,
    extracted_text     TEXT,
    extraction_status  TEXT NOT NULL DEFAULT 'pending'
                       CHECK (extraction_status IN ('pending', 'processing', 'completed', 'failed')),
    extraction_engine  TEXT,
    extraction_version TEXT,
    word_count         INTEGER NOT NULL DEFAULT 0 CHECK (word_count >= 0),
    lead_image_url     TEXT NOT NULL DEFAULT '',
    content_hash       TEXT NOT NULL DEFAULT '',
    is_latest          INTEGER NOT NULL DEFAULT 0 CHECK (is_latest IN (0, 1)),
    created_at         TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_content_versions_article_id
    ON article_content_versions(article_id);

CREATE INDEX IF NOT EXISTS idx_content_versions_latest
    ON article_content_versions(article_id, is_latest)
    WHERE is_latest = 1;

CREATE TABLE IF NOT EXISTS activity_events (
    id            TEXT PRIMARY KEY,
    article_id    TEXT NOT NULL REFERENCES articles(id),
    device_id     TEXT NOT NULL,
    event_type    TEXT NOT NULL
                  CHECK (event_type IN ('read', 'star', 'unstar', 'dwell', 'skip')),
    event_value   TEXT NOT NULL DEFAULT '',
    dwell_seconds REAL,
    created_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_activity_events_article_id
    ON activity_events(article_id);

CREATE INDEX IF NOT EXISTS idx_activity_events_created_at
    ON activity_events(created_at);

CREATE TABLE IF NOT EXISTS article_state (
    article_id     TEXT PRIMARY KEY REFERENCES articles(id),
    is_read        INTEGER NOT NULL DEFAULT 0 CHECK (is_read IN (0, 1)),
    is_starred     INTEGER NOT NULL DEFAULT 0 CHECK (is_starred IN (0, 1)),
    dwell_seconds  REAL NOT NULL DEFAULT 0 CHECK (dwell_seconds >= 0),
    read_at        TEXT,
    read_count     INTEGER NOT NULL DEFAULT 0 CHECK (read_count >= 0),
    updated_at     TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS jobs (
    id            TEXT PRIMARY KEY,
    kind          TEXT NOT NULL,
    payload       TEXT NOT NULL DEFAULT '{}',
    status        TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'claimed', 'completed', 'failed', 'dead')),
    priority      INTEGER NOT NULL DEFAULT 0,
    dedupe_key    TEXT,
    lease_id      TEXT,
    leased_until  TEXT,
    retry_count   INTEGER NOT NULL DEFAULT 0 CHECK (retry_count >= 0),
    max_retries   INTEGER NOT NULL DEFAULT 3 CHECK (max_retries >= 0),
    scheduled_at  TEXT NOT NULL,
    last_error    TEXT,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_jobs_scheduled_status
    ON jobs(status, scheduled_at)
    WHERE status IN ('pending', 'failed');

-- Enforce idempotent dedupe: only one active job (pending or claimed) per non-null dedupe_key.
-- Once a job is completed, failed, or dead, the same dedupe_key may be reused.
CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_active_dedupe
    ON jobs(dedupe_key)
    WHERE dedupe_key IS NOT NULL
      AND status IN ('pending', 'claimed');

CREATE INDEX IF NOT EXISTS idx_jobs_kind_status
    ON jobs(kind, status);
