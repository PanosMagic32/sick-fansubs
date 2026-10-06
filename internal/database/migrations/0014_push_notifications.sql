-- 0014_push_notifications.sql — web push delivery channel (decision 0034,
-- revision 2026-09-13): per-device subscription rows and per-user per-kind
-- push preference toggles.
--
-- The migration runner owns the transaction; do not add BEGIN/COMMIT/ROLLBACK
-- statements here.
--
-- notification_preferences kind CHECK starts with the two shipped in-app
-- kinds and amends per new kind — the same modularity contract as
-- user_notifications (0010 comment / 0013 rebuild).

CREATE TABLE push_subscriptions (
    id            TEXT    PRIMARY KEY,
    user_id       TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint      TEXT    NOT NULL UNIQUE,
    p256dh        TEXT    NOT NULL,
    auth          TEXT    NOT NULL,
    created_at_ms INTEGER NOT NULL CHECK (created_at_ms > 0),
    updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms > 0 AND updated_at_ms >= created_at_ms)
) STRICT;

CREATE INDEX idx_push_subscriptions_user ON push_subscriptions(user_id);

CREATE TABLE notification_preferences (
    user_id       TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind          TEXT    NOT NULL CHECK (kind IN ('heart', 'comment_reply')),
    push_enabled  INTEGER NOT NULL CHECK (push_enabled IN (0, 1)),
    updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms > 0),
    PRIMARY KEY (user_id, kind)
) STRICT;
