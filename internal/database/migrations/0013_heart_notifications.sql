-- 0013_heart_notifications.sql — the 'heart' notification kind
-- (decision 0028, 2026-09-11 revision; decision 0034 §5 kind roadmap).
--
-- The migration runner owns the transaction; do not add BEGIN/COMMIT/ROLLBACK
-- statements here.
--
-- The kind CHECK is the modularity contract (0010's comment): a new kind
-- ships as its own migration that REBUILDS the table to amend the CHECK.
-- No columns or indexes change — a heart row carries the same fields as a
-- comment_reply row (the hearted comment is the deep-link target).
--
-- The rebuild is safe with foreign_keys ON: user_notifications is a CHILD
-- of users (nothing references it), so the drop triggers no cascades, and
-- the rename has no inbound references to rewrite.

CREATE TABLE user_notifications_new (
    id            TEXT    PRIMARY KEY,
    recipient_id  TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind          TEXT    NOT NULL CHECK (kind IN ('comment_reply', 'heart')),
    actor_id      TEXT    REFERENCES users(id) ON DELETE SET NULL,
    content_kind  TEXT    NOT NULL CHECK (content_kind IN ('blog-posts', 'projects')),
    content_id    TEXT    NOT NULL,
    comment_id    TEXT    NOT NULL,
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
