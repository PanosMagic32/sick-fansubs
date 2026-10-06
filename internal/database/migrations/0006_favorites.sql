-- 0006_favorites.sql — Favorites join tables (decision 0010 §1–2, 0021 §3).
--
-- The migration runner owns the transaction; do not add BEGIN/COMMIT/ROLLBACK
-- statements here.
--
-- Two dedicated join tables, one per favoritable content type (0010 option 1):
--   - Composite primary key (user_id, content_id) prevents duplicates — one
--     INSERT per favorite operation; a repeat INSERT hits the PK constraint.
--   - ON DELETE CASCADE on BOTH foreign keys: deleting a user or a content
--     row automatically removes its favorites (no orphan rows, no cleanup
--     pass).
--   - created_at_ms records when the favorite was added, supplied by the
--     injected application clock (never a schema default, decision 0006).
--   - The secondary content index serves "who favorited this content" and
--     future count queries without a counter column (0010 §4 — counts stay
--     deferred).
--   - STRICT tables (decision 0006 §6).
--
-- The legacy favoriteBlogPostIds array mapping is deferred to the full
-- production-data migration (decision 0021) — this migration is schema only.

CREATE TABLE blog_post_favorites (
    user_id       TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    blog_post_id  TEXT    NOT NULL REFERENCES blog_posts(id) ON DELETE CASCADE,
    created_at_ms INTEGER NOT NULL CHECK (created_at_ms > 0),
    PRIMARY KEY (user_id, blog_post_id)
) STRICT;

CREATE INDEX idx_blog_post_favorites_post ON blog_post_favorites(blog_post_id);

CREATE TABLE project_favorites (
    user_id       TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id    TEXT    NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_at_ms INTEGER NOT NULL CHECK (created_at_ms > 0),
    PRIMARY KEY (user_id, project_id)
) STRICT;

CREATE INDEX idx_project_favorites_project ON project_favorites(project_id);
