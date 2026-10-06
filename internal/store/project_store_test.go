package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
)

// Project detail.

// mustCreateProjectDownload inserts one project_downloads row.
func mustCreateProjectDownload(t *testing.T, db *sql.DB, projectID, name string, position int, magnet, torrent *string) {
	t.Helper()

	const q = `INSERT INTO project_downloads
		(id, project_id, name, magnet_link, torrent_link, position, created_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, 1)`
	id := fmt.Sprintf("d-%s-%d", name, position)
	if _, err := db.Exec(q, id, projectID, name, magnet, torrent, position); err != nil {
		t.Fatalf("insert download %q: %v", id, err)
	}
}

// TestGetPublishedProject_Found proves the detail lookup: the full row with
// the creator/updater joins and the batch-download rows ordered by position.
func TestGetPublishedProject_Found(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	mustCreateUser(t, db, "u1", "creator", "creator", "creator@example.com")
	if _, err := db.Exec(`UPDATE users SET avatar_url = 'https://example.com/c.png' WHERE id = 'u1'`); err != nil {
		t.Fatalf("set avatar: %v", err)
	}
	mustCreateUser(t, db, "u2", "editor", "editor", "editor@example.com")
	mustCreateProject(t, db, ProjectSummary{ID: "p1", Title: "T", ThumbnailURL: "https://example.com/t.jpg", PublishedAtMS: 3000}, "published")
	if _, err := db.Exec(`UPDATE projects
		SET creator_id = 'u1', updater_id = 'u2', description = 'desc', updated_at_ms = 3100
		WHERE id = 'p1'`); err != nil {
		t.Fatalf("set creator/updater/description: %v", err)
	}

	mag := "magnet:?xt=urn:btih:aaa"
	tor := "https://example.com/t.torrent"
	// Inserted deliberately OUT of display order (position 1 first): the
	// lookup must order by position, not by insertion order.
	mustCreateProjectDownload(t, db, "p1", "Batch B", 1, nil, &tor)
	mustCreateProjectDownload(t, db, "p1", "Batch A", 0, &mag, &tor)

	project, downloads, err := GetPublishedProject(ctx, db, "p1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if project.ID != "p1" || project.Title != "T" || project.Description != "desc" || project.PublishedAtMS != 3000 {
		t.Errorf("project = %+v", project)
	}
	if project.Slug != "slug-p1" {
		t.Errorf("slug = %q, want slug-p1", project.Slug)
	}
	if project.UpdatedAtMS != 3100 {
		t.Errorf("updatedAtMS = %d, want 3100", project.UpdatedAtMS)
	}
	if project.Creator == nil || project.Creator.ID != "u1" || project.Creator.Username != "creator" ||
		project.Creator.AvatarURL == nil || *project.Creator.AvatarURL != "https://example.com/c.png" {
		t.Errorf("creator = %+v, want u1/creator with avatar", project.Creator)
	}
	if project.Updater == nil || project.Updater.ID != "u2" || project.Updater.Username != "editor" ||
		project.Updater.AvatarURL != nil {
		t.Errorf("updater = %+v, want u2/editor without avatar", project.Updater)
	}

	if len(downloads) != 2 {
		t.Fatalf("downloads = %d rows, want 2", len(downloads))
	}
	if downloads[0].Label != "Batch A" || downloads[0].MagnetLink == nil || *downloads[0].MagnetLink != mag ||
		downloads[0].TorrentLink == nil || *downloads[0].TorrentLink != tor {
		t.Errorf("download[0] = %+v", downloads[0])
	}
	if downloads[1].Label != "Batch B" || downloads[1].MagnetLink != nil || downloads[1].TorrentLink == nil {
		t.Errorf("download[1] = %+v, want Batch B with NULL magnet", downloads[1])
	}
}

// TestGetPublishedProject_CreatorNull proves the nullable projection: a
// project whose creator_id/updater_id are NULL yields nil refs, not fake
// users.
func TestGetPublishedProject_CreatorNull(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	mustCreateProject(t, db, ProjectSummary{ID: "p1", Title: "T", ThumbnailURL: "https://example.com/t.jpg", PublishedAtMS: 3000}, "published")

	project, _, err := GetPublishedProject(ctx, db, "p1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if project.Creator != nil {
		t.Errorf("creator = %+v, want nil (NULL creator_id)", project.Creator)
	}
	if project.Updater != nil {
		t.Errorf("updater = %+v, want nil (NULL updater_id)", project.Updater)
	}
}

// TestGetPublishedProject_UpdaterOnly proves the realistic post-migration
// mixed shape: legacy projects carry updatedBy with NO creator field at all,
// so creator_id stays NULL while updater_id resolves.
// The two joins must stay independent.
func TestGetPublishedProject_UpdaterOnly(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	mustCreateUser(t, db, "u2", "editor", "editor", "editor@example.com")
	mustCreateProject(t, db, ProjectSummary{ID: "p1", Title: "T", ThumbnailURL: "https://example.com/t.jpg", PublishedAtMS: 3000}, "published")
	if _, err := db.Exec(`UPDATE projects SET updater_id = 'u2' WHERE id = 'p1'`); err != nil {
		t.Fatalf("set updater: %v", err)
	}

	project, _, err := GetPublishedProject(ctx, db, "p1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if project.Creator != nil {
		t.Errorf("creator = %+v, want nil (NULL creator_id)", project.Creator)
	}
	if project.Updater == nil || project.Updater.ID != "u2" {
		t.Errorf("updater = %+v, want u2", project.Updater)
	}
}

// TestGetPublishedProject_UserRefsDeleted proves the LEFT JOINs survive user
// deletion: both foreign keys are ON DELETE SET NULL, so a project whose
// creator/updater were deleted yields nil refs — same projection as no
// reference at all. Project content survives.
func TestGetPublishedProject_UserRefsDeleted(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	mustCreateUser(t, db, "u1", "creator", "creator", "creator@example.com")
	mustCreateUser(t, db, "u2", "editor", "editor", "editor@example.com")
	mustCreateProject(t, db, ProjectSummary{ID: "p1", Title: "T", ThumbnailURL: "https://example.com/t.jpg", PublishedAtMS: 3000}, "published")
	if _, err := db.Exec(`UPDATE projects SET creator_id = 'u1', updater_id = 'u2' WHERE id = 'p1'`); err != nil {
		t.Fatalf("set refs: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM users WHERE id IN ('u1', 'u2')`); err != nil {
		t.Fatalf("delete users: %v", err)
	}

	project, _, err := GetPublishedProject(ctx, db, "p1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if project.Creator != nil {
		t.Errorf("creator = %+v, want nil (creator user deleted)", project.Creator)
	}
	if project.Updater != nil {
		t.Errorf("updater = %+v, want nil (updater user deleted)", project.Updater)
	}
}

// TestGetPublishedProject_NotFoundAndMasked proves deliberate masking:
// unknown ids AND non-public rows (draft, archived,
// NULL published_at_ms) all answer ErrNotFound — the caller cannot tell
// which ids exist.
func TestGetPublishedProject_NotFoundAndMasked(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	mustCreateProject(t, db, ProjectSummary{ID: "draft", Title: "D", ThumbnailURL: "https://example.com/d.jpg", PublishedAtMS: 9000}, "draft")
	mustCreateProject(t, db, ProjectSummary{ID: "archived", Title: "A", ThumbnailURL: "https://example.com/a.jpg", PublishedAtMS: 8000}, "archived")
	mustCreateProject(t, db, ProjectSummary{ID: "null-published", Title: "N", ThumbnailURL: "https://example.com/n.jpg"}, "published")

	for _, id := range []string{"unknown", "draft", "archived", "null-published"} {
		_, _, err := GetPublishedProject(ctx, db, id)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("id %q: err = %v, want ErrNotFound", id, err)
		}
	}
}

// TestGetPublishedProject_DownloadsCascade proves the downloads foreign key:
// deleting a project removes its batch-download rows (ON DELETE CASCADE)
// — the content is one unit.
func TestGetPublishedProject_DownloadsCascade(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	mustCreateProject(t, db, ProjectSummary{ID: "p1", Title: "T", ThumbnailURL: "https://example.com/t.jpg", PublishedAtMS: 3000}, "published")
	mag := "magnet:?xt=urn:btih:aaa"
	mustCreateProjectDownload(t, db, "p1", "Batch A", 0, &mag, nil)

	if _, err := db.Exec(`DELETE FROM projects WHERE id = 'p1'`); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	var downloads int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM project_downloads").Scan(&downloads); err != nil {
		t.Fatalf("count downloads: %v", err)
	}
	if downloads != 0 {
		t.Errorf("project_downloads rows: got %d, want 0 (CASCADE)", downloads)
	}
}

// Project list.

// TestListPublishedProjects_CreatorJoin proves the list projection: rows
// carry slug, description, updated_at_ms, and the creator ref with the
// avatar (nullable), while projects without a creator stay nil.
func TestListPublishedProjects_CreatorJoin(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	mustCreateUser(t, db, "u1", "creator", "creator", "creator@example.com")
	if _, err := db.Exec(`UPDATE users SET avatar_url = 'https://example.com/a.png' WHERE id = 'u1'`); err != nil {
		t.Fatalf("set avatar: %v", err)
	}
	mustCreateProject(t, db, ProjectSummary{ID: "with", Title: "W", ThumbnailURL: "https://example.com/w.jpg", PublishedAtMS: 3000}, "published")
	if _, err := db.Exec(`UPDATE projects
		SET creator_id = 'u1', description = 'desc', updated_at_ms = 3100
		WHERE id = 'with'`); err != nil {
		t.Fatalf("set creator/description: %v", err)
	}
	mustCreateProject(t, db, ProjectSummary{ID: "without", Title: "N", ThumbnailURL: "https://example.com/n.jpg", PublishedAtMS: 2000}, "published")

	projects, _, err := ListPublishedProjects(ctx, db, 10, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("got %d rows, want 2", len(projects))
	}

	first := projects[0]
	if first.ID != "with" || first.Description != "desc" || first.UpdatedAtMS != 3100 {
		t.Errorf("first = %+v, want with/desc/3100", first)
	}
	if first.Slug != "slug-with" {
		t.Errorf("first.Slug = %q, want slug-with", first.Slug)
	}
	if first.Creator == nil || first.Creator.ID != "u1" || first.Creator.Username != "creator" ||
		first.Creator.AvatarURL == nil || *first.Creator.AvatarURL != "https://example.com/a.png" {
		t.Errorf("first.Creator = %+v, want u1/creator with avatar", first.Creator)
	}

	second := projects[1]
	if second.Creator != nil {
		t.Errorf("second.Creator = %+v, want nil (no creator_id)", second.Creator)
	}
}

// TestListPublishedProjects_NewestFirst proves the accepted ordering:
// published_at_ms DESC with id ASC as the tie-breaker.
func TestListPublishedProjects_NewestFirst(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	mustCreateProject(t, db, ProjectSummary{ID: "old", Title: "Old", ThumbnailURL: "https://example.com/o.jpg", PublishedAtMS: 1000}, "published")
	mustCreateProject(t, db, ProjectSummary{ID: "new", Title: "New", ThumbnailURL: "https://example.com/n.jpg", PublishedAtMS: 3000}, "published")
	mustCreateProject(t, db, ProjectSummary{ID: "mid-b", Title: "Mid B", ThumbnailURL: "https://example.com/mb.jpg", PublishedAtMS: 2000}, "published")
	mustCreateProject(t, db, ProjectSummary{ID: "mid-a", Title: "Mid A", ThumbnailURL: "https://example.com/ma.jpg", PublishedAtMS: 2000}, "published")

	projects, hasNext, err := ListPublishedProjects(ctx, db, 10, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if hasNext {
		t.Error("hasNext = true, want false (4 rows < limit 10)")
	}

	var got []string
	for _, p := range projects {
		got = append(got, p.ID)
	}
	want := []string{"new", "mid-a", "mid-b", "old"}
	if len(got) != len(want) {
		t.Fatalf("got %d rows %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: got %q, want %q (order %v vs %v)", i, got[i], want[i], got, want)
		}
	}
}

// TestListPublishedProjects_ExcludesNonPublicRows proves the public filter:
// drafts and rows without a publish time never appear, regardless of their
// position in the ordering.
func TestListPublishedProjects_ExcludesNonPublicRows(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	mustCreateProject(t, db, ProjectSummary{ID: "published", Title: "P", ThumbnailURL: "https://example.com/p.jpg", PublishedAtMS: 1000}, "published")
	mustCreateProject(t, db, ProjectSummary{ID: "draft", Title: "D", ThumbnailURL: "https://example.com/d.jpg", PublishedAtMS: 9000}, "draft")
	mustCreateProject(t, db, ProjectSummary{ID: "archived", Title: "A", ThumbnailURL: "https://example.com/a.jpg", PublishedAtMS: 8000}, "archived")
	mustCreateProject(t, db, ProjectSummary{ID: "published-null", Title: "N", ThumbnailURL: "https://example.com/n.jpg"}, "published")

	projects, hasNext, err := ListPublishedProjects(ctx, db, 10, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if hasNext {
		t.Error("hasNext = true, want false")
	}
	if len(projects) != 1 || projects[0].ID != "published" {
		t.Errorf("got %v, want exactly [published]", projects)
	}
}

// TestListPublishedProjects_KeysetPagesCoverExactlyOnce proves that
// consecutive pages formed from the returned rows cover the full ordering
// with no duplicates and no omissions, exercising both the
// published_at_ms < ? branch and the published_at_ms = ? AND id > ? branch.
func TestListPublishedProjects_KeysetPagesCoverExactlyOnce(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	// Full ordering: p5(5000), p4(4000), p3a(3000), p3b(3000), p2(2000).
	mustCreateProject(t, db, ProjectSummary{ID: "p2", Title: "T", ThumbnailURL: "https://example.com/2.jpg", PublishedAtMS: 2000}, "published")
	mustCreateProject(t, db, ProjectSummary{ID: "p3b", Title: "T", ThumbnailURL: "https://example.com/3b.jpg", PublishedAtMS: 3000}, "published")
	mustCreateProject(t, db, ProjectSummary{ID: "p3a", Title: "T", ThumbnailURL: "https://example.com/3a.jpg", PublishedAtMS: 3000}, "published")
	mustCreateProject(t, db, ProjectSummary{ID: "p4", Title: "T", ThumbnailURL: "https://example.com/4.jpg", PublishedAtMS: 4000}, "published")
	mustCreateProject(t, db, ProjectSummary{ID: "p5", Title: "T", ThumbnailURL: "https://example.com/5.jpg", PublishedAtMS: 5000}, "published")

	page1, hasNext, err := ListPublishedProjects(ctx, db, 2, nil)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if projectIDs(page1) != "p5,p4" {
		t.Errorf("page 1: got %v, want [p5 p4]", projectIDs(page1))
	}
	if !hasNext {
		t.Error("page 1 hasNext = false, want true")
	}

	// Page 2 continues strictly after (4000, p4): the next publish time down,
	// which is the tied pair at 3000.
	key1 := &PageKey{PublishedAtMS: page1[1].PublishedAtMS, ID: page1[1].ID}
	page2, hasNext, err := ListPublishedProjects(ctx, db, 2, key1)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if projectIDs(page2) != "p3a,p3b" {
		t.Errorf("page 2: got %v, want [p3a p3b]", projectIDs(page2))
	}
	if !hasNext {
		t.Error("page 2 hasNext = false, want true")
	}

	// Page 3 continues strictly after (3000, p3b): only the older project remains.
	key2 := &PageKey{PublishedAtMS: page2[1].PublishedAtMS, ID: page2[1].ID}
	page3, hasNext, err := ListPublishedProjects(ctx, db, 2, key2)
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if projectIDs(page3) != "p2" {
		t.Errorf("page 3: got %v, want [p2]", projectIDs(page3))
	}
	if hasNext {
		t.Error("page 3 hasNext = true, want false (final page)")
	}
}

// TestListPublishedProjects_EmptyTable proves the empty-collection outcome:
// an empty items slice and no next page.
func TestListPublishedProjects_EmptyTable(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	projects, hasNext, err := ListPublishedProjects(ctx, db, 20, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("got %v, want no projects", projects)
	}
	if hasNext {
		t.Error("hasNext = true, want false")
	}
}

// TestProjectIndicatorCounts mirrors TestBlogPostIndicatorCounts for
// projects: the list summary and the detail lookup both carry the
// live-derived comment + favorite counts.
func TestProjectIndicatorCounts(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	mustCreateUser(t, db, "u1", "alice", "alice", "alice@example.com")
	mustCreateUser(t, db, "u2", "bob", "bob", "bob@example.com")
	mustCreateProject(t, db, ProjectSummary{ID: "p1", Title: "T", ThumbnailURL: "https://example.com/t.jpg", PublishedAtMS: 3000}, "published")
	mustCreateComment(t, db, ProjectContent, "c1", "u1", "p1", nil, "top", 0, 1000, 1000)
	mustCreateComment(t, db, ProjectContent, "r1", "u2", "p1", new("c1"), "reply", 0, 2000, 2000)
	if err := AddFavorite(ctx, db, ProjectContent, "u1", "p1", 5000); err != nil {
		t.Fatalf("add favorite u1: %v", err)
	}
	if err := AddFavorite(ctx, db, ProjectContent, "u2", "p1", 6000); err != nil {
		t.Fatalf("add favorite u2: %v", err)
	}

	projects, hasNext, err := ListPublishedProjects(ctx, db, 20, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if hasNext || len(projects) != 1 {
		t.Fatalf("list = %d items (hasNext=%v), want exactly 1", len(projects), hasNext)
	}
	if projects[0].CommentCount != 2 || projects[0].FavoriteCount != 2 {
		t.Errorf("list counts = (%d comments, %d favorites), want (2, 2)",
			projects[0].CommentCount, projects[0].FavoriteCount)
	}

	project, _, err := GetPublishedProject(ctx, db, "p1")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if project.CommentCount != 2 || project.FavoriteCount != 2 {
		t.Errorf("detail counts = (%d comments, %d favorites), want (2, 2)",
			project.CommentCount, project.FavoriteCount)
	}
}

// projectIDs renders the IDs of a page for compact failure messages.
func projectIDs(projects []ProjectSummary) string {
	if len(projects) == 0 {
		return "(empty)"
	}
	out := projects[0].ID
	for _, p := range projects[1:] {
		out += "," + p.ID
	}
	return out
}
