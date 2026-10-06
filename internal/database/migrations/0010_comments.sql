-- 0010_comments.sql — Comments thread, hearts, and per-recipient notifications
-- (decision 0028).
--
-- The migration runner owns the transaction; do not add BEGIN/COMMIT/ROLLBACK
-- statements here.
--
-- blog_post_comments / project_comments mirror each other against
-- blog_posts(id) / projects(id):
--   - parent_id is the self-referential one-reply-level link. Depth = 1 is an
--     API rule, not a schema rule: the FK permits arbitrary nesting, and the
--     reply endpoint rejects reply-to-reply with a 422 (0028 §2).
--   - Hard delete with full cascade: parent_id ON DELETE CASCADE removes
--     replies, the heart FK removes hearts, the content FK removes the whole
--     thread with its content, and the user FK removes a user's comments
--     with the account. No tombstones.
--   - hearts_count is MATERIALIZED (default 0, CHECK >= 0): the top sort's
--     keyset cursor encodes it, and a heart toggle is one transaction (heart
--     row insert/delete + counter delta) so the counter cannot drift —
--     the documented reason 0010 §4 withheld counters from favorites.
--   - created_at_ms / updated_at_ms come from the injected application clock
--     (decision 0006); updated_at_ms >= created_at_ms keeps the edited
--     marker honest.
--   - STRICT tables (decision 0006 §6).
--
-- blog_post_comment_hearts / project_comment_hearts: composite PK prevents
-- double hearts; both FKs cascade (0021's join-table philosophy).
--
-- user_notifications: per-recipient product notification rows — the unified
-- feed's second source alongside audit_events (0025 revision, 0028 §6).
--   - Read state is a read_at_ms column, not a join table: each row belongs
--     to exactly one recipient, so a join table would be pure ceremony
--     (unlike notification_reads, which exists because audit events are
--     shared across many staff viewers).
--   - kind CHECK ('comment_reply') is the modularity contract: a future kind
--     ships as its own migration (table rebuild to amend the CHECK), adding
--     its columns — no payload JSON (0028 §2).
--   - actor_id SET NULL: a deleted actor's notification to you survives
--     with the placeholder rendering (0025 §3's pattern).
--   - content_kind / content_id / comment_id are polymorphic refs with no
--     FK: comment_id deliberately dangles after a hard delete — the focus
--     fallback (§5) makes that safe. recipient_id cascades with the
--     account.
--   - idx_user_notifications_unread is PARTIAL (WHERE read_at_ms IS NULL) —
--     the badge scans only unread rows.

CREATE TABLE blog_post_comments (
    id            TEXT    PRIMARY KEY,
    user_id       TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    blog_post_id  TEXT    NOT NULL REFERENCES blog_posts(id) ON DELETE CASCADE,
    parent_id     TEXT    REFERENCES blog_post_comments(id) ON DELETE CASCADE,
    body          TEXT    NOT NULL,
    hearts_count  INTEGER NOT NULL DEFAULT 0 CHECK (hearts_count >= 0),
    created_at_ms INTEGER NOT NULL CHECK (created_at_ms > 0),
    updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms > 0 AND updated_at_ms >= created_at_ms)
) STRICT;

-- The newest/oldest orders scan created_at_ms; the top order needs the
-- hearts_count DESC prefix (the cursor encodes the triple).
CREATE INDEX idx_blog_post_comments_post ON blog_post_comments(blog_post_id, created_at_ms, id);
CREATE INDEX idx_blog_post_comments_post_top ON blog_post_comments(blog_post_id, hearts_count DESC, created_at_ms DESC, id ASC);
CREATE INDEX idx_blog_post_comments_parent ON blog_post_comments(parent_id);

CREATE TABLE blog_post_comment_hearts (
    user_id       TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    comment_id    TEXT    NOT NULL REFERENCES blog_post_comments(id) ON DELETE CASCADE,
    created_at_ms INTEGER NOT NULL CHECK (created_at_ms > 0),
    PRIMARY KEY (user_id, comment_id)
) STRICT;

CREATE INDEX idx_blog_post_comment_hearts_comment ON blog_post_comment_hearts(comment_id);

CREATE TABLE project_comments (
    id            TEXT    PRIMARY KEY,
    user_id       TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id    TEXT    NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    parent_id     TEXT    REFERENCES project_comments(id) ON DELETE CASCADE,
    body          TEXT    NOT NULL,
    hearts_count  INTEGER NOT NULL DEFAULT 0 CHECK (hearts_count >= 0),
    created_at_ms INTEGER NOT NULL CHECK (created_at_ms > 0),
    updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms > 0 AND updated_at_ms >= created_at_ms)
) STRICT;

CREATE INDEX idx_project_comments_project ON project_comments(project_id, created_at_ms, id);
CREATE INDEX idx_project_comments_project_top ON project_comments(project_id, hearts_count DESC, created_at_ms DESC, id ASC);
CREATE INDEX idx_project_comments_parent ON project_comments(parent_id);

CREATE TABLE project_comment_hearts (
    user_id       TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    comment_id    TEXT    NOT NULL REFERENCES project_comments(id) ON DELETE CASCADE,
    created_at_ms INTEGER NOT NULL CHECK (created_at_ms > 0),
    PRIMARY KEY (user_id, comment_id)
) STRICT;

CREATE INDEX idx_project_comment_hearts_comment ON project_comment_hearts(comment_id);

CREATE TABLE user_notifications (
    id            TEXT    PRIMARY KEY,
    recipient_id  TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind          TEXT    NOT NULL CHECK (kind IN ('comment_reply')),
    actor_id      TEXT    REFERENCES users(id) ON DELETE SET NULL,
    content_kind  TEXT    NOT NULL CHECK (content_kind IN ('blog-posts', 'projects')),
    content_id    TEXT    NOT NULL,
    comment_id    TEXT    NOT NULL,
    created_at_ms INTEGER NOT NULL CHECK (created_at_ms > 0),
    read_at_ms    INTEGER CHECK (read_at_ms IS NULL OR read_at_ms > 0)
) STRICT;

CREATE INDEX idx_user_notifications_recipient_created ON user_notifications(recipient_id, created_at_ms DESC, id ASC);
CREATE INDEX idx_user_notifications_unread ON user_notifications(recipient_id, created_at_ms DESC) WHERE read_at_ms IS NULL;
