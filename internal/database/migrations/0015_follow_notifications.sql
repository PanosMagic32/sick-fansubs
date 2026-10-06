-- 0015_follow_notifications.sql — the 'comment' and 'content_updated'
-- notification kinds (decision 0034, slice-2 revision 2026-09-13):
-- per-content follows drive both kinds.
--
-- The migration runner owns the transaction; do not add BEGIN/COMMIT/ROLLBACK
-- statements here.
--
-- The kind CHECK is the modularity contract (0010's comment, 0013's
-- rebuild): a new kind ships as its own migration that REBUILDS the table
-- to amend the CHECK. content_updated carries no comment — the content is
-- the deep-link target — so the rebuild makes comment_id NULLABLE and pins
-- the per-kind rule with a CHECK instead of a bare NOT NULL (0028 §2's
-- "adding its columns" contract, expressed as the column the kind needs).
--
-- The rebuilds are safe with foreign_keys ON: both tables are CHILDREN of
-- users (nothing references them), so the drops trigger no cascades, and
-- the renames have no inbound references to rewrite.
--
-- Each table's kind CHECK keeps its OWN historical order: user_notifications
-- appends to its original ('comment_reply','heart') prefix, and
-- notification_preferences appends to its original ('heart','comment_reply')
-- prefix (migration 0014). The orders differ per table by design.

CREATE TABLE content_follows (
    user_id       TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content_kind  TEXT    NOT NULL CHECK (content_kind IN ('blog-posts', 'projects')),
    content_id    TEXT    NOT NULL,
    created_at_ms INTEGER NOT NULL CHECK (created_at_ms > 0),
    PRIMARY KEY (user_id, content_kind, content_id)
) STRICT;

CREATE INDEX idx_content_follows_content ON content_follows(content_kind, content_id);

CREATE TABLE user_notifications_new (
    id            TEXT    PRIMARY KEY,
    recipient_id  TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind          TEXT    NOT NULL CHECK (kind IN ('comment_reply', 'heart', 'comment', 'content_updated')),
    actor_id      TEXT    REFERENCES users(id) ON DELETE SET NULL,
    content_kind  TEXT    NOT NULL CHECK (content_kind IN ('blog-posts', 'projects')),
    content_id    TEXT    NOT NULL,
    comment_id    TEXT    CHECK (kind = 'content_updated' OR comment_id IS NOT NULL),
    created_at_ms INTEGER NOT NULL CHECK (created_at_ms > 0),
    read_at_ms    INTEGER CHECK (read_at_ms IS NULL OR read_at_ms > 0)
) STRICT;

INSERT INTO user_notifications_new
    (id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms, read_at_ms)
SELECT id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms, read_at_ms
FROM user_notifications;

DROP TABLE user_notifications;
ALTER TABLE user_notifications_new RENAME TO user_notifications;

CREATE INDEX idx_user_notifications_recipient_created ON user_notifications(recipient_id, created_at_ms DESC, id ASC);
CREATE INDEX idx_user_notifications_unread ON user_notifications(recipient_id, created_at_ms DESC) WHERE read_at_ms IS NULL;

CREATE TABLE notification_preferences_new (
    user_id       TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind          TEXT    NOT NULL CHECK (kind IN ('heart', 'comment_reply', 'comment', 'content_updated')),
    push_enabled  INTEGER NOT NULL CHECK (push_enabled IN (0, 1)),
    updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms > 0),
    PRIMARY KEY (user_id, kind)
) STRICT;

INSERT INTO notification_preferences_new (user_id, kind, push_enabled, updated_at_ms)
SELECT user_id, kind, push_enabled, updated_at_ms
FROM notification_preferences;

DROP TABLE notification_preferences;
ALTER TABLE notification_preferences_new RENAME TO notification_preferences;
