-- 0019_draft_activity_notifications.sql — the 'draft_activity' notification
-- kind (decision 0038): the staff-only notice for UNPUBLISHED content.
--
-- The migration runner owns the transaction; do not add BEGIN/COMMIT/ROLLBACK
-- statements here.
--
-- The kind CHECK is the modularity contract (0010's comment, 0013's rebuild):
-- a new kind ships as its own migration that REBUILDS the table to amend the
-- CHECK. Two rules are amended this time:
--
--   - the kind list gains 'draft_activity' in both tables;
--   - the per-kind comment_id rule gains the new kind to its exempt list.
--     That rule reads "comment_id is required UNLESS the kind is
--     content_updated or new_content" — a draft notice names no comment, so
--     leaving it out would reject every row the emitter writes.
--
-- draft_action is the new nullable column: WHICH unpublished transition the
-- notice reports (created | updated | unpublished — decision 0038 §1). It is
-- the draft kind's own field, so it is bound to that kind by a per-kind CHECK
-- in both directions: a draft_activity row always carries an action, and no
-- other kind may carry one. The iff form (rather than two separate CHECKs)
-- keeps the pair from drifting — the comment_id precedent, tightened.
--
-- The rebuilds are safe with foreign_keys ON: both tables are CHILDREN of
-- users (nothing references them), so the drops trigger no cascades, and
-- the renames have no inbound references to rewrite.
--
-- Each table's kind CHECK keeps its OWN historical order: user_notifications
-- appends to its original ('comment_reply', …) prefix, and
-- notification_preferences appends to its original ('heart', …) prefix
-- (migration 0014). The orders differ per table by design.

CREATE TABLE user_notifications_new (
    id            TEXT    PRIMARY KEY,
    recipient_id  TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind          TEXT    NOT NULL CHECK (kind IN ('comment_reply', 'heart', 'comment', 'content_updated', 'new_content', 'comment_removed', 'draft_activity')),
    actor_id      TEXT    REFERENCES users(id) ON DELETE SET NULL,
    content_kind  TEXT    NOT NULL CHECK (content_kind IN ('blog-posts', 'projects')),
    content_id    TEXT    NOT NULL,
    comment_id    TEXT    CHECK (kind IN ('content_updated', 'new_content', 'draft_activity') OR comment_id IS NOT NULL),
    draft_action  TEXT    CHECK ((kind = 'draft_activity') = (draft_action IS NOT NULL)
                                 AND (draft_action IS NULL OR draft_action IN ('created', 'updated', 'unpublished'))),
    created_at_ms INTEGER NOT NULL CHECK (created_at_ms > 0),
    read_at_ms    INTEGER CHECK (read_at_ms IS NULL OR read_at_ms > 0)
) STRICT;

INSERT INTO user_notifications_new
    (id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, draft_action, created_at_ms, read_at_ms)
SELECT id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, NULL, created_at_ms, read_at_ms
FROM user_notifications;

DROP TABLE user_notifications;
ALTER TABLE user_notifications_new RENAME TO user_notifications;

CREATE INDEX idx_user_notifications_recipient_created ON user_notifications(recipient_id, created_at_ms DESC, id ASC);
CREATE INDEX idx_user_notifications_unread ON user_notifications(recipient_id, created_at_ms DESC) WHERE read_at_ms IS NULL;

CREATE TABLE notification_preferences_new (
    user_id       TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind          TEXT    NOT NULL CHECK (kind IN ('heart', 'comment_reply', 'comment', 'content_updated', 'new_content', 'comment_removed', 'draft_activity')),
    push_enabled  INTEGER NOT NULL CHECK (push_enabled IN (0, 1)),
    updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms > 0),
    PRIMARY KEY (user_id, kind)
) STRICT;

INSERT INTO notification_preferences_new (user_id, kind, push_enabled, updated_at_ms)
SELECT user_id, kind, push_enabled, updated_at_ms
FROM notification_preferences;

DROP TABLE notification_preferences;
ALTER TABLE notification_preferences_new RENAME TO notification_preferences;
