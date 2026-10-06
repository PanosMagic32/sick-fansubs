-- 0004_projects.sql — Projects content schema (decision 0009 §§3–4).
--
-- The migration runner owns the transaction; do not add BEGIN/COMMIT/ROLLBACK
-- statements here.
--
-- Follows decision 0009 (Content Schema and Download Representation):
--   - Projects: title/description/slug/thumbnail, status, creator/updater
--     foreign keys with ON DELETE SET NULL, published_at_ms as the public
--     ordering key, UTC Unix-millisecond timestamps (decision 0006), revision
--     for the ETag/If-Match concurrency model (decisions 0007/0014).
--   - slug: unique, case-sensitive, URL-safe (decision 0009 §3). Migration
--     preserves existing slugs; new-slug generation rides the
--     project-creation slice.
--   - Project batch downloads normalized into project_downloads rows: entry
--     name, magnet/torrent links (NULL when absent), position for display
--     order (decision 0009 §4). Each batchDownloadLinks entry becomes one
--     row; the legacy sub-ObjectId `_id` is preserved as the row id.
--   - Opaque TEXT IDs: FRESH 32-hex random values for every migrated
--     project row (owner decision 2026-08-14 — legacy ObjectIds are NOT
--     imported as target IDs; the source ID is reported in the
--     reconciliation mapping only). Legacy TIMESTAMPS are preserved so the
--     public ordering key (published_at_ms) keeps the legacy chronological
--     order.
--   - STRICT tables (decision 0006 §6).

CREATE TABLE projects (
    id              TEXT    PRIMARY KEY,
    title           TEXT    NOT NULL CHECK (length(title) >= 1),
    description     TEXT    NOT NULL DEFAULT '',
    slug            TEXT    NOT NULL UNIQUE CHECK (length(slug) >= 1),
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

CREATE INDEX idx_projects_slug ON projects(slug);
CREATE INDEX idx_projects_published ON projects(published_at_ms, id);

CREATE TABLE project_downloads (
    id             TEXT    PRIMARY KEY,
    project_id     TEXT    NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name           TEXT    NOT NULL CHECK (length(name) >= 1),
    magnet_link    TEXT    CHECK (magnet_link IS NULL OR length(magnet_link) > 0),
    torrent_link   TEXT    CHECK (torrent_link IS NULL OR length(torrent_link) > 0),
    position       INTEGER NOT NULL DEFAULT 0,
    created_at_ms  INTEGER NOT NULL CHECK (created_at_ms > 0)
) STRICT;

CREATE INDEX idx_project_downloads_project ON project_downloads(project_id, position);
