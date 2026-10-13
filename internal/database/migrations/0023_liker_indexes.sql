-- 0023_liker_indexes.sql — Composite indexes for the who-liked readers.
--
-- The migration runner owns the transaction; do not add BEGIN/COMMIT/ROLLBACK
-- statements here.
--
-- The four join tables gain a liker-list index shaped exactly like the
-- reader: (content column, created_at_ms DESC, user_id ASC) serves the
-- per-content filter and the newest-like-first order in one scan. Each
-- DROPS the old single-column content index it supersedes — the composite
-- covers that prefix, so the narrower copy only added write cost and space
-- (the index-cleanup precedent).

DROP INDEX idx_blog_post_favorites_post;
CREATE INDEX idx_blog_post_favorites_likers ON blog_post_favorites(blog_post_id, created_at_ms DESC, user_id);

DROP INDEX idx_project_favorites_project;
CREATE INDEX idx_project_favorites_likers ON project_favorites(project_id, created_at_ms DESC, user_id);

DROP INDEX idx_blog_post_comment_hearts_comment;
CREATE INDEX idx_blog_post_comment_hearts_likers ON blog_post_comment_hearts(comment_id, created_at_ms DESC, user_id);

DROP INDEX idx_project_comment_hearts_comment;
CREATE INDEX idx_project_comment_hearts_likers ON project_comment_hearts(comment_id, created_at_ms DESC, user_id);
