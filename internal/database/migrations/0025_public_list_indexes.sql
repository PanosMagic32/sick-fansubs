-- Public list ordering indexes.
--
-- The public content lists order by (published_at_ms DESC, id ASC) and the
-- comments newest tab by (created_at_ms DESC, id ASC). A same-direction
-- composite index cannot serve a mixed-direction ORDER BY, so each list was
-- scanning its filtered set into a temp B-tree. These indexes match the ORDER
-- BY key-for-key and replace the public content prefix indexes they supersede;
-- the ascending comment index stays for the replies window.
DROP INDEX idx_blog_posts_published;
CREATE INDEX idx_blog_posts_published_order ON blog_posts(published_at_ms DESC, id ASC);

DROP INDEX idx_projects_published;
CREATE INDEX idx_projects_published_order ON projects(published_at_ms DESC, id ASC);

CREATE INDEX idx_blog_post_comments_post_newest ON blog_post_comments(blog_post_id, created_at_ms DESC, id ASC);
CREATE INDEX idx_project_comments_project_newest ON project_comments(project_id, created_at_ms DESC, id ASC);
