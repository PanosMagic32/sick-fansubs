-- 0024_audit_moderation_targets.sql — Moderation object identifiers on the ledger.
--
-- The migration runner owns the transaction; do not add BEGIN/COMMIT/ROLLBACK
-- statements here.
--
-- A comment_deleted row names its actor and the comment's author, but before
-- these columns it carried nothing that identified the moderated object: the
-- comment is hard-deleted, so the ledger could not reconstruct which comment
-- on which content the moderator removed. The columns are nullable because
-- every other event has no content object.

ALTER TABLE audit_events ADD COLUMN content_kind TEXT;
ALTER TABLE audit_events ADD COLUMN content_id TEXT;
ALTER TABLE audit_events ADD COLUMN comment_id TEXT;
