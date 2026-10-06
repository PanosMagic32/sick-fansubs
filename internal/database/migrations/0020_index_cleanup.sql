-- 0020_index_cleanup.sql — Drop two indexes no query uses.
--
-- idx_audit_events_target was intended for an account-activity feed read that
-- was never built: the notifications feed filters audit rows by event and the
-- target-role snapshot, never by target_id, and no other query predicates on
-- it. idx_projects_slug duplicated the UNIQUE auto-index on the same column —
-- slug lookups are served by the auto-index, so the named copy only added
-- write cost and space.
--
-- The migration runner owns the transaction; do not add BEGIN/COMMIT/ROLLBACK
-- statements here.

DROP INDEX idx_audit_events_target;
DROP INDEX idx_projects_slug;
