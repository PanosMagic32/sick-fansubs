-- 0003_blog_posts.sql — Blog content schema (decision 0009 §§1–2).
--
-- The migration runner owns the transaction; do not add BEGIN/COMMIT/ROLLBACK
-- statements here.
--
-- Follows decision 0009 (Content Schema and Download Representation):
--   - Blog posts: title/subtitle/description/thumbnail, status, creator/updater
--     foreign keys with ON DELETE SET NULL, published_at_ms as the public
--     ordering key, UTC Unix-millisecond timestamps (decision 0006), revision
--     for the ETag/If-Match concurrency model (decisions 0007/0014).
--   - Downloads normalized into blog_post_downloads rows: resolution label,
--     magnet/torrent links (NULL when absent), position for display order.
--     Legacy empty-string download fields produce NO row (decision 0009 §2).
--   - Opaque TEXT IDs: FRESH 32-hex random values for every migrated row
--     (owner decision 2026-08-14 — legacy ObjectIds are NOT imported as
--     target IDs; the source ID is reported in the reconciliation mapping
--     only). Legacy TIMESTAMPS are preserved so the public ordering key
--     (published_at_ms) keeps the legacy chronological order.
--   - STRICT tables (decision 0006 §6).

CREATE TABLE blog_posts (
    id              TEXT    PRIMARY KEY,
    title           TEXT    NOT NULL CHECK (length(title) >= 1),
    subtitle        TEXT    NOT NULL DEFAULT '',
    description     TEXT    NOT NULL DEFAULT '',
    thumbnail_url   TEXT    NOT NULL CHECK (length(thumbnail_url) > 0),
    status          TEXT    NOT NULL DEFAULT 'draft'
                            CHECK (status IN ('draft', 'published', 'archived')),
    creator_id      TEXT    REFERENCES users(id) ON DELETE SET NULL,
    updater_id      TEXT    REFERENCES users(id) ON DELETE SET NULL,
    published_at_ms INTEGER CHECK (published_at_ms IS NULL OR published_at_ms > 0),
    created_at_ms   INTEGER NOT NULL CHECK (created_at_ms > 0),
    updated_at_ms   INTEGER NOT NULL CHECK (updated_at_ms >= created_at_ms),
    revision        INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0)
) STRICT;

CREATE INDEX idx_blog_posts_published ON blog_posts(published_at_ms, id);
CREATE INDEX idx_blog_posts_creator ON blog_posts(creator_id);

CREATE TABLE blog_post_downloads (
    id             TEXT    PRIMARY KEY,
    blog_post_id   TEXT    NOT NULL REFERENCES blog_posts(id) ON DELETE CASCADE,
    resolution     TEXT    NOT NULL CHECK (length(resolution) >= 1),
    magnet_link    TEXT    CHECK (magnet_link IS NULL OR length(magnet_link) > 0),
    torrent_link   TEXT    CHECK (torrent_link IS NULL OR length(torrent_link) > 0),
    position       INTEGER NOT NULL DEFAULT 0,
    created_at_ms  INTEGER NOT NULL CHECK (created_at_ms > 0)
) STRICT;

CREATE INDEX idx_blog_post_downloads_post ON blog_post_downloads(blog_post_id, position);
