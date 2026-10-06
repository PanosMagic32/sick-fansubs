-- 0007_audit_events.sql — Durable audit event table (decision 0024 §3, 0014 §5).
--
-- The migration runner owns the transaction; do not add BEGIN/COMMIT/ROLLBACK
-- statements here.
--
-- audit_events is the durable half of the dual-write audit trail: the audit
-- package emits every event to slog AND, when the API process has registered
-- the writer (store.AuditWriter), to this table.
--   - No foreign keys: events must survive the deletion of the accounts they
--     reference — actor_id/target_id are plain references, correlated in
--     code, never constrained (0024 §3).
--   - result is constrained to the two audit outcomes.
--   - created_at_ms is stamped by the writer's application clock (never a
--     schema default, decision 0006).
--   - idx_audit_events_created serves the chronological audit viewer;
--     (target_id, created_at_ms DESC) serves the notifications-feed query
--     ("everything that happened to this account") — the feed UI is a
--     separate discussion (0024 §7), the index is schema only.
--   - STRICT table (decision 0006 §6).
--   - Retention: 365 days (0024 §3, 2026-08-20 revision). The cleanup task
--     is deferred to a later slice — bounded-batch deletes on the created
--     index, window ≥ backup retention (0014 §5).

CREATE TABLE audit_events (
    id            TEXT    NOT NULL,
    event         TEXT    NOT NULL,
    result        TEXT    NOT NULL CHECK (result IN ('success', 'failure')),
    actor_id      TEXT,
    target_id     TEXT,
    request_id    TEXT    NOT NULL,
    remote_addr   TEXT    NOT NULL,
    created_at_ms INTEGER NOT NULL CHECK (created_at_ms > 0),
    PRIMARY KEY (id)
) STRICT;

CREATE INDEX idx_audit_events_created ON audit_events(created_at_ms);

CREATE INDEX idx_audit_events_target ON audit_events(target_id, created_at_ms DESC);
