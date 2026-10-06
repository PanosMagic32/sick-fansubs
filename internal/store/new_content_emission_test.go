package store

import (
	"database/sql"
	"testing"

	"sick-fansubs/internal/identity"
)

// Emission tests for the 'new_content' broadcast: the publish fan-out to
// opted-in recipients on a create-as-published and on the FIRST transition
// into published. Real SQLite + real migrations — the rows and the returned
// events are pinned together (the push seam consumes the events, the feed
// consumes the rows).

// newContentFixture seeds the recipient matrix: adm (the publisher, admin),
// adm2 (a second admin), sa (super-admin), mod (moderator) — all role
// default-ON; u-on (user with an explicit ON row), u-off (user with an
// explicit OFF row), u-plain (user, no row — default OFF), mod-off
// (moderator with an explicit OFF row). Every opted-in recipient except
// the actor should receive a new_content row.
func newContentFixture(t *testing.T) *sql.DB {
	t.Helper()
	db := openStoreDB(t)
	for _, u := range []struct{ id, name, role string }{
		{"adm", "adm", identity.RoleAdmin},
		{"adm2", "adm2", identity.RoleAdmin},
		{"sa", "sa", identity.RoleSuperAdmin},
		{"mod", "mod", identity.RoleModerator},
		{"u-on", "uon", identity.RoleUser},
		{"u-off", "uoff", identity.RoleUser},
		{"u-plain", "uplain", identity.RoleUser},
		{"mod-off", "modoff", identity.RoleModerator},
	} {
		mustCreateUser(t, db, u.id, u.name, u.name, u.name+"@example.com")
		if _, err := db.Exec(`UPDATE users SET role = ? WHERE id = ?`, u.role, u.id); err != nil {
			t.Fatalf("set role %q for %s: %v", u.role, u.id, err)
		}
	}
	ctx := t.Context()
	if err := SetNotificationPreference(ctx, db, "u-on", "new_content", true, 1_000); err != nil {
		t.Fatalf("seed u-on preference: %v", err)
	}
	if err := SetNotificationPreference(ctx, db, "u-off", "new_content", false, 1_000); err != nil {
		t.Fatalf("seed u-off preference: %v", err)
	}
	if err := SetNotificationPreference(ctx, db, "mod-off", "new_content", false, 1_000); err != nil {
		t.Fatalf("seed mod-off preference: %v", err)
	}
	return db
}

// optedInRecipients is the matrix's expected recipient set: every
// moderator+ user without an explicit OFF row plus every explicit ON row —
// the actor excluded. The set itself is derived from identity.RoleWeight, so
// the fixture pins the SQL gate against the model (the canary precedent).
func optedInRecipients() map[string]bool {
	return map[string]bool{"adm2": true, "sa": true, "mod": true, "u-on": true}
}

// assertNewContentEvents pins the event shapes for one publish batch.
func assertNewContentEvents(t *testing.T, events []NotificationEvent, wantRecipients map[string]bool, contentKind, contentID, title, actor string) {
	t.Helper()
	if len(events) != len(wantRecipients) {
		t.Fatalf("events: got %d, want %d", len(events), len(wantRecipients))
	}
	for _, ev := range events {
		if !wantRecipients[ev.RecipientID] {
			t.Errorf("unexpected recipient %q in events: %+v", ev.RecipientID, events)
		}
		if ev.Kind != "new_content" || ev.ContentKind != contentKind ||
			ev.ContentID != contentID || ev.ContentTitle != title || ev.CommentID != "" {
			t.Errorf("event shape: got %+v", ev)
		}
	}
}

// assertNewContentRows pins the committed rows against the same set.
func assertNewContentRows(t *testing.T, db *sql.DB, wantRecipients map[string]bool, contentID, actor string) {
	t.Helper()
	rows, err := db.Query(
		`SELECT recipient_id, actor_id, comment_id FROM user_notifications
		 WHERE kind = 'new_content' AND content_id = ?`, contentID)
	if err != nil {
		t.Fatalf("query new_content rows: %v", err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var recipient, gotActor string
		var commentID sql.NullString
		if err := rows.Scan(&recipient, &gotActor, &commentID); err != nil {
			t.Fatalf("scan new_content row: %v", err)
		}
		if gotActor != actor {
			t.Errorf("row actor = %q, want %q", gotActor, actor)
		}
		if commentID.Valid {
			t.Errorf("new_content row comment_id = %q, want NULL", commentID.String)
		}
		got[recipient] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate new_content rows: %v", err)
	}
	if len(got) != len(wantRecipients) {
		t.Errorf("rows: got %d recipients, want %d (%v)", len(got), len(wantRecipients), got)
	}
	for id := range wantRecipients {
		if !got[id] {
			t.Errorf("missing recipient %q in rows", id)
		}
	}
}

// TestCreateBlogPost_PublishedBroadcasts pins the create-as-published
// matrix: the default-on roles (moderator, admin, super-admin) and the
// explicit ON user receive; the actor, the default-off user, and both
// explicit OFF rows receive nothing. The SQL gate's role half is thereby
// pinned against identity.RoleWeight via the fixture's expected set.
func TestCreateBlogPost_PublishedBroadcasts(t *testing.T) {
	t.Parallel()

	db := newContentFixture(t)
	ctx := t.Context()

	post, events, err := CreateBlogPost(ctx, db, CreateBlogPostParams{
		ID: "b1", Title: "Νέα Ανάρτηση", Subtitle: "", Description: "desc",
		ThumbnailURL: "media/images/ab/x.jpg", Status: "published",
		CreatorID: "adm", UpdaterID: "adm", PublishedAtMS: 5_000, NowMS: 5_000,
	})
	if err != nil {
		t.Fatalf("create published: %v", err)
	}
	if post.ID != "b1" {
		t.Fatalf("created post id = %q, want b1", post.ID)
	}
	assertNewContentEvents(t, events, optedInRecipients(), "blog-posts", "b1", "Νέα Ανάρτηση", "adm")
	assertNewContentRows(t, db, optedInRecipients(), "b1", "adm")
}

// TestCreateBlogPost_DraftNoticeNeedsOptIn pins the draft-create leg: no
// new_content rows and — because the fixture holds no draft_activity opt-ins
// — no notice rows either. The staff-only kind has NO role default, so a
// moderator+ account without an explicit row receives
// nothing: the matrix's audience lives in the draft_activity emission suite.
func TestCreateBlogPost_DraftNoticeNeedsOptIn(t *testing.T) {
	t.Parallel()

	db := newContentFixture(t)
	ctx := t.Context()

	_, events, err := CreateBlogPost(ctx, db, CreateBlogPostParams{
		ID: "d1", Title: "Draft", Subtitle: "", Description: "desc",
		ThumbnailURL: "media/images/ab/x.jpg", Status: "draft",
		CreatorID: "adm", UpdaterID: "adm", PublishedAtMS: 0, NowMS: 5_000,
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("draft create events: got %d, want 0", len(events))
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications`).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if n != 0 {
		t.Errorf("notification rows after draft create: got %d, want 0", n)
	}
}

// TestCreateProject_PublishedBroadcasts pins the project leg of the same
// create path (its own table and content_kind).
func TestCreateProject_PublishedBroadcasts(t *testing.T) {
	t.Parallel()

	db := newContentFixture(t)
	ctx := t.Context()

	project, events, err := CreateProject(ctx, db, CreateProjectParams{
		ID: "p1", Title: "Νέο Project", Description: "desc", Slug: "p1",
		ThumbnailURL: "media/images/ab/x.jpg", Status: "published",
		CreatorID: "adm", UpdaterID: "adm", PublishedAtMS: 5_000, NowMS: 5_000,
	})
	if err != nil {
		t.Fatalf("create published project: %v", err)
	}
	if project.ID != "p1" {
		t.Fatalf("created project id = %q, want p1", project.ID)
	}
	assertNewContentEvents(t, events, optedInRecipients(), "projects", "p1", "Νέο Project", "adm")
	assertNewContentRows(t, db, optedInRecipients(), "p1", "adm")
}

// TestUpdateBlogPost_FirstPublishBroadcasts pins the transition gate: the
// FIRST transition into published broadcasts once; a re-publish after an
// unpublish emits nothing (the publish stamp is the gate, not the status).
func TestUpdateBlogPost_FirstPublishBroadcasts(t *testing.T) {
	t.Parallel()

	db := newContentFixture(t)
	ctx := t.Context()

	if _, _, err := CreateBlogPost(ctx, db, CreateBlogPostParams{
		ID: "d1", Title: "Draft", Subtitle: "", Description: "desc",
		ThumbnailURL: "media/images/ab/x.jpg", Status: "draft",
		CreatorID: "adm", UpdaterID: "adm", PublishedAtMS: 0, NowMS: 1_000,
	}); err != nil {
		t.Fatalf("seed draft: %v", err)
	}

	update := func(status string) ([]NotificationEvent, error) {
		var revision int64
		if err := db.QueryRow(`SELECT revision FROM blog_posts WHERE id = 'd1'`).Scan(&revision); err != nil {
			t.Fatalf("read revision: %v", err)
		}
		_, events, err := UpdateBlogPost(ctx, db, "d1", revision, UpdateBlogPostParams{
			Title: "Live", Subtitle: "", Description: "desc",
			ThumbnailURL: "media/images/ab/x.jpg", Status: status, UpdaterID: "adm", NowMS: 9_000,
		})
		return events, err
	}

	// 1. draft → published: the FIRST publication broadcasts.
	events, err := update("published")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	assertNewContentEvents(t, events, optedInRecipients(), "blog-posts", "d1", "Live", "adm")

	// 2. published → draft: silent.
	if events, err = update("draft"); err != nil {
		t.Fatalf("unpublish: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("unpublish events: got %d, want 0", len(events))
	}

	// 3. draft → published again: silent — the stamp already exists, the
	// content is not new.
	if events, err = update("published"); err != nil {
		t.Fatalf("re-publish: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("re-publish events: got %d, want 0", len(events))
	}

	// Exactly ONE batch of rows landed (step 1 only).
	assertNewContentRows(t, db, optedInRecipients(), "d1", "adm")
}

// TestUpdateProject_FirstPublishBroadcasts pins the project leg of the
// same transition gate.
func TestUpdateProject_FirstPublishBroadcasts(t *testing.T) {
	t.Parallel()

	db := newContentFixture(t)
	ctx := t.Context()

	if _, _, err := CreateProject(ctx, db, CreateProjectParams{
		ID: "d1", Title: "Draft", Description: "desc", Slug: "d1",
		ThumbnailURL: "media/images/ab/x.jpg", Status: "draft",
		CreatorID: "adm", UpdaterID: "adm", PublishedAtMS: 0, NowMS: 1_000,
	}); err != nil {
		t.Fatalf("seed draft project: %v", err)
	}

	_, events, err := UpdateProject(ctx, db, "d1", 1, UpdateProjectParams{
		Title: "Live", Description: "desc", Slug: "d1",
		ThumbnailURL: "media/images/ab/x.jpg", Status: "published", UpdaterID: "adm", NowMS: 9_000,
	})
	if err != nil {
		t.Fatalf("publish project: %v", err)
	}
	assertNewContentEvents(t, events, optedInRecipients(), "projects", "d1", "Live", "adm")
	assertNewContentRows(t, db, optedInRecipients(), "d1", "adm")
}

// TestNewContentBroadcastSkipsSuspended pins the active-recipient rule on
// the publish broadcast: a suspended recipient is skipped even though its
// preference resolves ON (default or explicit).
func TestNewContentBroadcastSkipsSuspended(t *testing.T) {
	t.Parallel()

	db := newContentFixture(t)
	ctx := t.Context()

	if _, err := db.Exec(`UPDATE users SET status = ? WHERE id = 'adm2'`, identity.StatusSuspended); err != nil {
		t.Fatalf("suspend adm2: %v", err)
	}

	_, events, err := CreateBlogPost(ctx, db, CreateBlogPostParams{
		ID: "b1", Title: "Νέα Ανάρτηση", Subtitle: "", Description: "desc",
		ThumbnailURL: "media/images/ab/x.jpg", Status: "published",
		CreatorID: "adm", UpdaterID: "adm", PublishedAtMS: 5_000, NowMS: 5_000,
	})
	if err != nil {
		t.Fatalf("create published: %v", err)
	}
	assertNewContentEvents(t, events, map[string]bool{"sa": true, "mod": true, "u-on": true}, "blog-posts", "b1", "Νέα Ανάρτηση", "adm")
	assertNewContentRows(t, db, map[string]bool{"sa": true, "mod": true, "u-on": true}, "b1", "adm")
}

// TestUpdateBlogPost_ArchivedTransitionLegs pins the two archived legs of
// the stamp gate — the stamp, not the
// status string, is the gate: draft → archived → published has no stamp,
// so it IS the first publication and emits; published → archived →
// published keeps the stamp, so it is a re-publish and stays silent.
func TestUpdateBlogPost_ArchivedTransitionLegs(t *testing.T) {
	t.Parallel()

	db := newContentFixture(t)
	ctx := t.Context()

	// Leg A: never-published content archived then published = first
	// publication (the stamp arrives at the transition).
	if _, _, err := CreateBlogPost(ctx, db, CreateBlogPostParams{
		ID: "a1", Title: "A", Subtitle: "", Description: "desc",
		ThumbnailURL: "media/images/ab/x.jpg", Status: "draft",
		CreatorID: "adm", UpdaterID: "adm", PublishedAtMS: 0, NowMS: 1_000,
	}); err != nil {
		t.Fatalf("seed draft: %v", err)
	}
	if _, _, err := UpdateBlogPost(ctx, db, "a1", 1, UpdateBlogPostParams{
		Title: "A", Subtitle: "", Description: "desc",
		ThumbnailURL: "media/images/ab/x.jpg", Status: "archived", UpdaterID: "adm", NowMS: 2_000,
	}); err != nil {
		t.Fatalf("archive draft: %v", err)
	}
	_, events, err := UpdateBlogPost(ctx, db, "a1", 2, UpdateBlogPostParams{
		Title: "A", Subtitle: "", Description: "desc",
		ThumbnailURL: "media/images/ab/x.jpg", Status: "published", UpdaterID: "adm", NowMS: 3_000,
	})
	if err != nil {
		t.Fatalf("publish from archived: %v", err)
	}
	assertNewContentEvents(t, events, optedInRecipients(), "blog-posts", "a1", "A", "adm")

	// Leg B: published content archived then published again = re-publish,
	// the stamp exists — silent.
	if _, _, err := CreateBlogPost(ctx, db, CreateBlogPostParams{
		ID: "b1", Title: "B", Subtitle: "", Description: "desc",
		ThumbnailURL: "media/images/ab/x.jpg", Status: "published",
		CreatorID: "adm", UpdaterID: "adm", PublishedAtMS: 1_000, NowMS: 1_000,
	}); err != nil {
		t.Fatalf("seed published: %v", err)
	}
	if _, _, err := UpdateBlogPost(ctx, db, "b1", 1, UpdateBlogPostParams{
		Title: "B", Subtitle: "", Description: "desc",
		ThumbnailURL: "media/images/ab/x.jpg", Status: "archived", UpdaterID: "adm", NowMS: 2_000,
	}); err != nil {
		t.Fatalf("archive published: %v", err)
	}
	_, events, err = UpdateBlogPost(ctx, db, "b1", 2, UpdateBlogPostParams{
		Title: "B", Subtitle: "", Description: "desc",
		ThumbnailURL: "media/images/ab/x.jpg", Status: "published", UpdaterID: "adm", NowMS: 3_000,
	})
	if err != nil {
		t.Fatalf("re-publish from archived: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("re-publish events: got %d, want 0", len(events))
	}

	// Leg A's batch is the only archived-leg emission: a1's rows only.
	assertNewContentRows(t, db, optedInRecipients(), "a1", "adm")
}

// TestNewContentBroadcastRoleDefaultsMirrorModel pins the SQL role gate
// against identity.RoleWeight for EVERY accepted role: a user with no
// preference row receives the broadcast exactly when
// RoleWeight(role) >= RoleWeight(RoleModerator) — the draft-gate canary
// precedent (TestStaffBlogPostDraftVisibility).
func TestNewContentBroadcastRoleDefaultsMirrorModel(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := t.Context()

	// The actor (the publisher) is a plain user so it never shadows a
	// default-on role.
	mustCreateUser(t, db, "actor", "actor", "actor", "actor@example.com")
	roles := []string{identity.RoleUser, identity.RoleModerator, identity.RoleAdmin, identity.RoleSuperAdmin}
	for _, role := range roles {
		id := "u" + role
		mustCreateUser(t, db, id, id, id, id+"@example.com")
		if _, err := db.Exec(`UPDATE users SET role = ? WHERE id = ?`, role, id); err != nil {
			t.Fatalf("set role %q: %v", role, err)
		}
	}

	_, _, err := CreateBlogPost(ctx, db, CreateBlogPostParams{
		ID: "b1", Title: "T", Subtitle: "", Description: "desc",
		ThumbnailURL: "media/images/ab/x.jpg", Status: "published",
		CreatorID: "actor", UpdaterID: "actor", PublishedAtMS: 5_000, NowMS: 5_000,
	})
	if err != nil {
		t.Fatalf("create published: %v", err)
	}

	rows, err := db.Query(`SELECT recipient_id FROM user_notifications WHERE kind = 'new_content'`)
	if err != nil {
		t.Fatalf("query recipients: %v", err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan recipient: %v", err)
		}
		got[id] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate recipients: %v", err)
	}

	for _, role := range roles {
		want := identity.RoleWeight(role) >= identity.RoleWeight(identity.RoleModerator)
		if got["u"+role] != want {
			t.Errorf("role %q: got recipient %v, want %v (RoleWeight mirror)", role, got["u"+role], want)
		}
	}
	if got["actor"] {
		t.Error("the publisher must never receive their own new_content row")
	}
}
