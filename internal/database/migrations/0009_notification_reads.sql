-- 0009_notification_reads.sql — Per-event notification read state (decision 0025 §4).
--
-- The migration runner owns the transaction; do not add BEGIN/COMMIT/ROLLBACK
-- statements here.
--
-- notification_reads is the feed's per-user read watermark: one row per
-- (viewer, event) pair marks that the viewer has seen the event. The unread
-- badge counts the viewer's visible audit events NOT in this table
-- (anti-join), and the feed itself keeps showing read events — read state
-- only feeds the badge, it never hides history.
--   - user_id references users(id) with ON DELETE CASCADE: read rows die
--     with the account (decision 0025 §4 — inbound references from
--     application state are a different thing from audit_events' own no-FK
--     rule, 0024 §3).
--   - event_id references audit_events(id) with ON DELETE CASCADE: read
--     rows die with the event, pairing their cleanup with the deferred
--     365-day event cleanup (0024 §3).
--   - read_at_ms is stamped by the store's application clock (decision
--     0006, the AuditWriter precedent) — never a schema default.
--   - The composite PK (user_id, event_id) covers the per-user anti-join;
--     idx_notification_reads_event serves the event-side cascade delete
--     (the favorites-post-index precedent, 0021).
--   - STRICT table (decision 0006 §6).

CREATE TABLE notification_reads (
    user_id     TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event_id    TEXT    NOT NULL REFERENCES audit_events(id) ON DELETE CASCADE,
    read_at_ms  INTEGER NOT NULL CHECK (read_at_ms > 0),
    PRIMARY KEY (user_id, event_id)
) STRICT;

CREATE INDEX idx_notification_reads_event ON notification_reads(event_id);
