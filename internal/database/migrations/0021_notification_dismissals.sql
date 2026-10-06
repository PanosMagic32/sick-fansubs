-- 0021_notification_dismissals.sql — Per-viewer dismissal of audit feed entries.
--
-- The migration runner owns the transaction; do not add BEGIN/COMMIT/ROLLBACK
-- statements here.
--
-- notification_reads gains the dismissal stamp: dismissed_at_ms set marks
-- that the viewer removed the audit event from THEIR feed. The audit event
-- itself is a ledger row and is never deleted by the feed (retention owns
-- its lifecycle), so the per-item delete resolves to per-viewer state — the
-- read watermark's sibling. A dismissal always stamps read_at_ms too (the
-- column stays NOT NULL), which keeps the unread badge's anti-join valid
-- without an extra predicate.
--   - Nullable on purpose: an ordinary read row carries NULL here and every
--     feed is_read check keeps reading it as read.
--   - CHECK mirrors read_at_ms: a stamp is the application clock's positive
--     millisecond value, never zero (docs/patterns/go/sql-mapping.md).
--   - The table stays STRICT (ALTER TABLE on a STRICT table keeps it).

ALTER TABLE notification_reads ADD COLUMN dismissed_at_ms INTEGER CHECK (dismissed_at_ms > 0);
