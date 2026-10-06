package store

import (
	"database/sql"
	"testing"

	"sick-fansubs/internal/identity"
)

// Emission tests for the comment and content_updated kinds: the 'comment'
// fan-out on top-level
// comment create and the 'content_updated' fan-out on published→published
// content edits. Real
// SQLite + real migrations — the rows and the returned events are pinned
// together (the push seam consumes the events, the feed consumes the rows).

// emissionFixture seeds users (u1 author/editor, u2 + u3 followers, u4
// bystander) plus one published blog post and one published project.
func emissionFixture(t *testing.T) *sql.DB {
	t.Helper()
	db := openStoreDB(t)
	for _, u := range []struct{ id, name string }{
		{"u1", "alice"}, {"u2", "bob"}, {"u3", "carol"}, {"u4", "dave"},
	} {
		mustCreateUser(t, db, u.id, u.name, u.name, u.name+"@example.com")
	}
	for _, q := range []string{
		`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('b1', 'Blog One', '', 'desc', 'media/images/ab/b1.jpg', 'published', 2000, 1000, 1000)`,
		`INSERT INTO projects (id, title, description, slug, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('p1', 'Project One', 'desc', 'p1', 'media/images/ab/p1.jpg', 'published', 2000, 1000, 1000)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed content: %v", err)
		}
	}
	return db
}

func TestCreateComment_NotifiesFollowers(t *testing.T) {
	t.Parallel()

	db := emissionFixture(t)
	ctx := t.Context()

	for _, follower := range []string{"u2", "u3", "u1"} {
		if err := AddFollow(ctx, db, BlogContent, follower, "b1", 1_000); err != nil {
			t.Fatalf("follow %s: %v", follower, err)
		}
	}

	events, err := CreateComment(ctx, db, BlogContent, "b1", "u1", "hello", "new-1", 5_000)
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}

	// u1 follows too but is the author — self-suppressed; u4 never followed.
	if len(events) != 2 {
		t.Fatalf("events: got %d, want 2 (u2 + u3)", len(events))
	}
	recipients := map[string]bool{}
	for _, ev := range events {
		recipients[ev.RecipientID] = true
		if ev.Kind != "comment" || ev.ContentKind != "blog-posts" ||
			ev.ContentID != "b1" || ev.CommentID != "new-1" || ev.ContentTitle != "Blog One" {
			t.Errorf("event shape: got %+v", ev)
		}
	}
	if !recipients["u2"] || !recipients["u3"] || recipients["u1"] || recipients["u4"] {
		t.Errorf("recipients: got %v, want exactly u2+u3", recipients)
	}

	var rows []struct {
		Recipient, Kind, CommentID string
		Actor                      string
	}
	rowsQ, err := db.Query(`SELECT recipient_id, kind, comment_id, actor_id FROM user_notifications ORDER BY recipient_id`)
	if err != nil {
		t.Fatalf("query notifications: %v", err)
	}
	defer rowsQ.Close()
	for rowsQ.Next() {
		var r struct {
			Recipient, Kind, CommentID string
			Actor                      string
		}
		if err := rowsQ.Scan(&r.Recipient, &r.Kind, &r.CommentID, &r.Actor); err != nil {
			t.Fatalf("scan notification: %v", err)
		}
		rows = append(rows, r)
	}
	if err := rowsQ.Err(); err != nil {
		t.Fatalf("iterate notifications: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("notification rows: got %d, want 2", len(rows))
	}
	for _, r := range rows {
		if r.Kind != "comment" || r.CommentID != "new-1" || r.Actor != "u1" {
			t.Errorf("row shape: got %+v", r)
		}
	}
}

// TestCreateComment_NoFollowersSucceedsSilently pins the empty-followers
// leg: the comment lands, zero events, zero rows.
func TestCreateComment_NoFollowersSucceedsSilently(t *testing.T) {
	t.Parallel()

	db := emissionFixture(t)
	ctx := t.Context()

	events, err := CreateComment(ctx, db, BlogContent, "b1", "u1", "hello", "new-1", 5_000)
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("events: got %d, want 0", len(events))
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications`).Scan(&n); err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	if n != 0 {
		t.Errorf("notification rows: got %d, want 0", n)
	}
}

// TestFollowerFanOutSkipsSuspended pins the active-recipient rule on the
// shared follower fan-out: a suspended follower receives neither a row nor
// an event, while the active follower next to them does.
func TestFollowerFanOutSkipsSuspended(t *testing.T) {
	t.Parallel()

	db := emissionFixture(t)
	ctx := t.Context()

	if err := AddFollow(ctx, db, BlogContent, "u2", "b1", 1_000); err != nil {
		t.Fatalf("follow u2: %v", err)
	}
	if err := AddFollow(ctx, db, BlogContent, "u3", "b1", 1_000); err != nil {
		t.Fatalf("follow u3: %v", err)
	}
	if _, err := db.Exec(`UPDATE users SET status = ? WHERE id = 'u2'`, identity.StatusSuspended); err != nil {
		t.Fatalf("suspend u2: %v", err)
	}

	events, err := CreateComment(ctx, db, BlogContent, "b1", "u1", "hello", "new-1", 5_000)
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	if len(events) != 1 || events[0].RecipientID != "u3" {
		t.Fatalf("events: got %+v, want exactly u3", events)
	}
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM user_notifications WHERE kind = 'comment'`).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if n != 1 {
		t.Errorf("comment rows = %d, want 1 (the suspended follower skipped)", n)
	}
}

// TestUpdateBlogPost_ContentUpdatedMatrix pins the published→published
// emission gate: a published edit notifies followers (minus the editor);
// a draft edit, an unpublish, and a re-publish emit nothing for
// content_updated (the FIRST publish emits new_content — pinned by
// TestUpdateBlogPost_FirstPublishBroadcasts).
func TestUpdateBlogPost_ContentUpdatedMatrix(t *testing.T) {
	t.Parallel()

	db := emissionFixture(t)
	ctx := t.Context()

	// Followers u2 + u3 follow b1; the editor u1 follows too (excluded).
	for _, follower := range []string{"u2", "u3", "u1"} {
		if err := AddFollow(ctx, db, BlogContent, follower, "b1", 1_000); err != nil {
			t.Fatalf("follow %s: %v", follower, err)
		}
	}
	// A second published post no one follows (the bystander post) and a draft.
	for _, q := range []string{
		`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('b2', 'Blog Two', '', 'desc', 'media/images/ab/b2.jpg', 'published', 2000, 1000, 1000)`,
		`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
			created_at_ms, updated_at_ms)
		 VALUES ('d1', 'Draft', '', 'desc', 'media/images/ab/d1.jpg', 'draft', 1000, 1000)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	update := func(id string, revision int64, status, updater, title string) ([]NotificationEvent, error) {
		_, events, err := UpdateBlogPost(ctx, db, id, revision, UpdateBlogPostParams{
			Title: title, Subtitle: "", Description: "desc",
			ThumbnailURL: "media/images/ab/x.jpg", Status: status, UpdaterID: updater, NowMS: 9_000,
		})
		return events, err
	}

	// 1. published → published: two events (u2 + u3; the editor u1 excluded).
	events, err := update("b1", 1, "published", "u1", "Blog One v2")
	if err != nil {
		t.Fatalf("published edit: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("published edit events: got %d, want 2", len(events))
	}
	for _, ev := range events {
		if ev.Kind != "content_updated" || ev.ContentKind != "blog-posts" ||
			ev.ContentID != "b1" || ev.CommentID != "" || ev.ContentTitle != "Blog One v2" {
			t.Errorf("event shape: got %+v", ev)
		}
	}

	// 2. published → draft (unpublish): silent.
	events, err = update("b1", 2, "draft", "u1", "Blog One v3")
	if err != nil {
		t.Fatalf("unpublish edit: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("unpublish events: got %d, want 0", len(events))
	}

	// 3. draft → published (first publish): NO content_updated events —
	// the first publication is the new_content broadcast (all
	// fixture users are role user default-off, so the broadcast lands zero
	// rows here; the opted-in legs live in the new_content matrix).
	events, err = update("d1", 1, "published", "u1", "Draft Now Live")
	if err != nil {
		t.Fatalf("publish edit: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("publish events: got %d, want 0 (no opted-in recipients in this fixture)", len(events))
	}

	// 4. draft → draft: silent.
	events, err = update("d1", 2, "draft", "u1", "Draft v2")
	if err != nil {
		t.Fatalf("draft edit: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("draft events: got %d, want 0", len(events))
	}

	// The committed rows match the one emitted batch: two content_updated
	// rows with a NULL comment_id and the editor as actor.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE kind = 'content_updated'`).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if n != 2 {
		t.Errorf("content_updated rows: got %d, want 2", n)
	}
	var commentID sql.NullString
	if err := db.QueryRow(
		`SELECT comment_id FROM user_notifications WHERE kind = 'content_updated' LIMIT 1`).Scan(&commentID); err != nil {
		t.Fatalf("read comment_id: %v", err)
	}
	if commentID.Valid {
		t.Errorf("content_updated comment_id = %q, want NULL", commentID.String)
	}
}

// TestUpdateProject_ContentUpdated pins the project leg of the same
// emission path (its own tables and content_kind).
func TestUpdateProject_ContentUpdated(t *testing.T) {
	t.Parallel()

	db := emissionFixture(t)
	ctx := t.Context()

	if err := AddFollow(ctx, db, ProjectContent, "u2", "p1", 1_000); err != nil {
		t.Fatalf("follow: %v", err)
	}

	_, events, err := UpdateProject(ctx, db, "p1", 1, UpdateProjectParams{
		Title: "Project One v2", Description: "desc", Slug: "p1",
		ThumbnailURL: "media/images/ab/y.jpg", Status: "published", UpdaterID: "u1", NowMS: 9_000,
	})
	if err != nil {
		t.Fatalf("update project: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events: got %d, want 1", len(events))
	}
	if events[0].Kind != "content_updated" || events[0].ContentKind != "projects" ||
		events[0].RecipientID != "u2" || events[0].ContentID != "p1" ||
		events[0].ContentTitle != "Project One v2" {
		t.Errorf("event shape: got %+v", events[0])
	}
}
