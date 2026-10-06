package store

import (
	"database/sql"
	"testing"

	"sick-fansubs/internal/identity"
)

// Emission tests for the 'draft_activity' notice: the
// staff-only, OPT-IN fan-out on writes that leave content unpublished. Real
// SQLite + real migrations — the rows and the returned events are pinned
// together (the push seam consumes the events, the feed consumes the rows).

// draftActivityFixture seeds the audience matrix: adm (admin — the
// creator/actor of the default scenario), adm2 (a second admin), sa
// (super-admin), mod (moderator), u1 (plain user). EVERY candidate carries an
// explicit opt-in row for draft_activity, so the observed recipient set
// isolates the draft-visibility gate: the same-role admin, the below-creator
// moderator, and the below-floor user must be filtered out by the gate (or
// the staff floor), never by the opt-in.
func draftActivityFixture(t *testing.T) *sql.DB {
	t.Helper()
	db := openStoreDB(t)
	ctx := t.Context()
	for _, u := range []struct{ id, role string }{
		{"adm", identity.RoleAdmin},
		{"adm2", identity.RoleAdmin},
		{"sa", identity.RoleSuperAdmin},
		{"mod", identity.RoleModerator},
		{"u1", identity.RoleUser},
	} {
		mustCreateUser(t, db, u.id, u.id, u.id, u.id+"@example.com")
		if _, err := db.Exec(`UPDATE users SET role = ? WHERE id = ?`, u.role, u.id); err != nil {
			t.Fatalf("set role %q for %s: %v", u.role, u.id, err)
		}
		if err := SetNotificationPreference(ctx, db, u.id, "draft_activity", true, 1_000); err != nil {
			t.Fatalf("opt in %s: %v", u.id, err)
		}
	}
	return db
}

// assertDraftActivity pins one emission batch against the expected audience:
// the returned events AND the committed rows must be exactly want, every
// event carries the action, and every row records the actor with a NULL
// comment_id (the per-kind rule migration 0019 enforces).
func assertDraftActivity(t *testing.T, db *sql.DB, events []NotificationEvent, contentID, title, actor, action string, want map[string]bool) {
	t.Helper()
	if len(events) != len(want) {
		t.Fatalf("events: got %d, want %d (%+v)", len(events), len(want), events)
	}
	for _, ev := range events {
		if !want[ev.RecipientID] {
			t.Errorf("unexpected recipient %q in events: %+v", ev.RecipientID, events)
		}
		if ev.Kind != "draft_activity" || ev.ContentID != contentID || ev.ContentTitle != title ||
			ev.CommentID != "" || ev.DraftAction != action {
			t.Errorf("event shape: got %+v", ev)
		}
	}

	rows, err := db.Query(
		`SELECT recipient_id, actor_id, comment_id, draft_action FROM user_notifications
		 WHERE kind = 'draft_activity' AND content_id = ?`, contentID)
	if err != nil {
		t.Fatalf("query draft_activity rows: %v", err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var (
			recipient, gotActor, gotAction string
			commentID                      sql.NullString
		)
		if err := rows.Scan(&recipient, &gotActor, &commentID, &gotAction); err != nil {
			t.Fatalf("scan draft_activity row: %v", err)
		}
		if gotActor != actor {
			t.Errorf("row actor = %q, want %q", gotActor, actor)
		}
		if commentID.Valid {
			t.Errorf("draft_activity row comment_id = %q, want NULL", commentID.String)
		}
		if gotAction != action {
			t.Errorf("row draft_action = %q, want %q", gotAction, action)
		}
		got[recipient] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate draft_activity rows: %v", err)
	}
	if len(got) != len(want) {
		t.Errorf("rows: got %d recipients (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for id := range want {
		if !got[id] {
			t.Errorf("missing recipient %q in rows", id)
		}
	}
}

// seedBlogRow and seedProjectRow insert one content row DIRECTLY, with no
// emission: the create path is covered by its own test, and an update test
// must compare exactly the rows its own legs write (a store create would
// contribute its own 'created' row for the same content id).
func seedBlogRow(t *testing.T, db *sql.DB, id, status, creatorID string, publishedMS int64) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status, creator_id, published_at_ms, created_at_ms, updated_at_ms)
		VALUES (?, 'Τίτλος', '', 'desc', 'media/images/ab/x.jpg', ?, ?, NULLIF(?, 0), 1000, 1000)`,
		id, status, creatorID, publishedMS); err != nil {
		t.Fatalf("seed blog row %s: %v", id, err)
	}
}

func seedProjectRow(t *testing.T, db *sql.DB, id, status, creatorID string, publishedMS int64) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO projects (id, title, description, slug, thumbnail_url, status, creator_id, published_at_ms, created_at_ms, updated_at_ms)
		VALUES (?, 'Τίτλος', 'desc', ?, 'media/images/ab/x.jpg', ?, ?, NULLIF(?, 0), 1000, 1000)`,
		id, id, status, creatorID, publishedMS); err != nil {
		t.Fatalf("seed project row %s: %v", id, err)
	}
}

// TestDraftActivityCreateBlogDraft pins the create-as-draft audience: the
// super-admin above the creator receives; the same-role admin, the moderator
// below the creator, the plain user below the staff floor, and the creator
// itself (the actor) do not — with an opt-in row present for all of them.
func TestDraftActivityCreateBlogDraft(t *testing.T) {
	t.Parallel()

	db := draftActivityFixture(t)
	ctx := t.Context()

	_, events, err := CreateBlogPost(ctx, db, CreateBlogPostParams{
		ID: "d1", Title: "Πρόχειρο", Subtitle: "", Description: "desc",
		ThumbnailURL: "media/images/ab/x.jpg", Status: "draft",
		CreatorID: "adm", UpdaterID: "adm", PublishedAtMS: 0, NowMS: 5_000,
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	assertDraftActivity(t, db, events, "d1", "Πρόχειρο", "adm", "created",
		map[string]bool{"sa": true})
}

// TestDraftActivityUpdateLegs pins the update legs that reach the notice and
// the ones that must NOT: a non-published edit reports 'updated', leaving
// published reports 'unpublished', the FIRST publication belongs to
// new_content, a RE-PUBLISH is silent, and a published→published edit belongs
// to content_updated. Each leg owns its own content row, so every assertion
// compares exactly the rows that leg wrote.
func TestDraftActivityUpdateLegs(t *testing.T) {
	t.Parallel()

	db := draftActivityFixture(t)
	ctx := t.Context()

	seed := func(id, status string, publishedMS int64) {
		t.Helper()
		seedBlogRow(t, db, id, status, "adm", publishedMS)
	}
	update := func(id string, status string) []NotificationEvent {
		t.Helper()
		var revision int64
		if err := db.QueryRow(`SELECT revision FROM blog_posts WHERE id = ?`, id).Scan(&revision); err != nil {
			t.Fatalf("read revision of %s: %v", id, err)
		}
		_, events, err := UpdateBlogPost(ctx, db, id, revision, UpdateBlogPostParams{
			Title: "Τίτλος", Subtitle: "", Description: "desc",
			ThumbnailURL: "media/images/ab/x.jpg", Status: status, UpdaterID: "adm", NowMS: 9_000,
		})
		if err != nil {
			t.Fatalf("update %s to %s: %v", id, status, err)
		}
		return events
	}
	// assertNoDraftEvents guards the legs that belong to another kind: a
	// new_content broadcast or a content_updated fan-out may legitimately
	// return events here — a draft_activity event may not.
	assertNoDraftEvents := func(events []NotificationEvent) {
		t.Helper()
		for _, ev := range events {
			if ev.Kind == "draft_activity" {
				t.Errorf("unexpected draft_activity event: %+v", ev)
			}
		}
	}

	// draft → draft = 'updated'.
	seed("d1", "draft", 0)
	assertDraftActivity(t, db, update("d1", "draft"), "d1", "Τίτλος", "adm", "updated",
		map[string]bool{"sa": true})

	// draft → archived = still unpublished = 'updated'.
	seed("d2", "draft", 0)
	assertDraftActivity(t, db, update("d2", "archived"), "d2", "Τίτλος", "adm", "updated",
		map[string]bool{"sa": true})

	// draft → published = the FIRST publication — new_content's leg, not the
	// notice's.
	seed("d3", "draft", 0)
	assertNoDraftEvents(update("d3", "published"))

	// published → published = content_updated's leg.
	seed("b4", "published", 1_000)
	assertNoDraftEvents(update("b4", "published"))

	// published → draft = 'unpublished'.
	seed("b5", "published", 1_000)
	assertDraftActivity(t, db, update("b5", "draft"), "b5", "Τίτλος", "adm", "unpublished",
		map[string]bool{"sa": true})

	// published → archived = 'unpublished' too (archived is an edit-time
	// state that also leaves the public site).
	seed("b6", "published", 1_000)
	assertDraftActivity(t, db, update("b6", "archived"), "b6", "Τίτλος", "adm", "unpublished",
		map[string]bool{"sa": true})

	// draft → published again = a RE-PUBLISH (the row keeps its publish stamp
	// while unpublished): silent for every kind, the notice included.
	seed("b7", "draft", 1_000)
	if events := update("b7", "published"); len(events) != 0 {
		t.Errorf("re-publish: got %d events, want 0", len(events))
	}

	// Exactly the four notice legs landed rows: 1 + 1 + 1 + 1.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE kind = 'draft_activity'`).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if n != 4 {
		t.Errorf("draft_activity rows: got %d, want 4", n)
	}
}

// TestDraftActivityCreatorReceivesForeignEdit pins the creator leg of the
// gate: when a STRICTLY-HIGHER role edits someone else's draft, the draft's
// creator is notified about their own row even though their weight equals
// the creator weight (the gate's "p.creator_id = viewer" half) — and the
// editor (the actor) never is.
func TestDraftActivityCreatorReceivesForeignEdit(t *testing.T) {
	t.Parallel()

	db := draftActivityFixture(t)
	ctx := t.Context()

	seedBlogRow(t, db, "d1", "draft", "adm", 0)

	_, events, err := UpdateBlogPost(ctx, db, "d1", 1, UpdateBlogPostParams{
		Title: "Draft v2", Subtitle: "", Description: "desc",
		ThumbnailURL: "media/images/ab/x.jpg", Status: "draft", UpdaterID: "sa", NowMS: 9_000,
	})
	if err != nil {
		t.Fatalf("super-admin edit: %v", err)
	}
	// The audience is narrowed to the CREATOR: sa (the actor) never gets a
	// row for its own edit.
	assertDraftActivity(t, db, events, "d1", "Draft v2", "sa", "updated",
		map[string]bool{"adm": true})
}

// TestDraftActivityProjectLeg pins the projects table and content_kind leg:
// the same gate, action vocabulary, and emission gates over the second content
// table — every leg the blog test pins has its projects twin here (the
// projects update branch is its own code path).
func TestDraftActivityProjectLeg(t *testing.T) {
	t.Parallel()

	db := draftActivityFixture(t)
	ctx := t.Context()

	update := func(id, status string) []NotificationEvent {
		t.Helper()
		var revision int64
		if err := db.QueryRow(`SELECT revision FROM projects WHERE id = ?`, id).Scan(&revision); err != nil {
			t.Fatalf("read revision of %s: %v", id, err)
		}
		_, events, err := UpdateProject(ctx, db, id, revision, UpdateProjectParams{
			Title: "Τίτλος", Description: "desc", Slug: id,
			ThumbnailURL: "media/images/ab/x.jpg", Status: status, UpdaterID: "adm", NowMS: 9_000,
		})
		if err != nil {
			t.Fatalf("update %s to %s: %v", id, status, err)
		}
		return events
	}

	// The create leg over the projects table.
	_, events, err := CreateProject(ctx, db, CreateProjectParams{
		ID: "p1", Title: "Νέο Project", Description: "desc", Slug: "p1",
		ThumbnailURL: "media/images/ab/x.jpg", Status: "draft",
		CreatorID: "adm", UpdaterID: "adm", PublishedAtMS: 0, NowMS: 5_000,
	})
	if err != nil {
		t.Fatalf("create draft project: %v", err)
	}
	assertDraftActivity(t, db, events, "p1", "Νέο Project", "adm", "created",
		map[string]bool{"sa": true})

	// draft → draft = 'updated' (a non-published edit).
	seedProjectRow(t, db, "p2", "draft", "adm", 0)
	assertDraftActivity(t, db, update("p2", "draft"), "p2", "Τίτλος", "adm", "updated",
		map[string]bool{"sa": true})

	// The unpublish leg (a published row seeded directly, so this content id
	// carries exactly the rows this leg writes).
	seedProjectRow(t, db, "p3", "published", "adm", 1_000)
	assertDraftActivity(t, db, update("p3", "draft"), "p3", "Τίτλος", "adm", "unpublished",
		map[string]bool{"sa": true})

	// The silent legs: a first publication belongs to new_content, and a
	// published→published edit to content_updated — neither may write a
	// draft notice.
	seedProjectRow(t, db, "p4", "draft", "adm", 0)
	for _, ev := range update("p4", "published") {
		if ev.Kind == "draft_activity" {
			t.Errorf("first publication: unexpected draft_activity event %+v", ev)
		}
	}
	seedProjectRow(t, db, "p5", "published", "adm", 1_000)
	for _, ev := range update("p5", "published") {
		if ev.Kind == "draft_activity" {
			t.Errorf("published edit: unexpected draft_activity event %+v", ev)
		}
	}

	// Exactly the three notice legs wrote rows: p2 (updated), p3
	// (unpublished), p1 (created).
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE kind = 'draft_activity'`).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if n != 3 {
		t.Errorf("draft_activity rows: got %d, want 3", n)
	}
}

// TestDraftActivityGateMirrorsDraftVisibility pins the SQL weight comparison
// against identity.RoleWeight for EVERY accepted creator role: with every
// candidate opted in, the audience is exactly the opted-in staff whose weight
// is STRICTLY above the creator's — the read gate's rule, computed in Go here.
func TestDraftActivityGateMirrorsDraftVisibility(t *testing.T) {
	t.Parallel()

	db := draftActivityFixture(t)
	ctx := t.Context()

	staffOptedIn := map[string]string{"mod": identity.RoleModerator, "adm": identity.RoleAdmin, "adm2": identity.RoleAdmin, "sa": identity.RoleSuperAdmin}

	for _, role := range []string{identity.RoleUser, identity.RoleModerator, identity.RoleAdmin, identity.RoleSuperAdmin} {
		creator := "creator-" + role
		mustCreateUser(t, db, creator, creator, creator, creator+"@example.com")
		if _, err := db.Exec(`UPDATE users SET role = ? WHERE id = ?`, role, creator); err != nil {
			t.Fatalf("set creator role %q: %v", role, err)
		}

		want := map[string]bool{}
		for id, r := range staffOptedIn {
			if identity.RoleWeight(r) > identity.RoleWeight(role) {
				want[id] = true
			}
		}

		_, events, err := CreateBlogPost(ctx, db, CreateBlogPostParams{
			ID: "b-" + role, Title: "T", Subtitle: "", Description: "desc",
			ThumbnailURL: "media/images/ab/x.jpg", Status: "draft",
			CreatorID: creator, UpdaterID: creator, PublishedAtMS: 0, NowMS: 5_000,
		})
		if err != nil {
			t.Fatalf("create draft as %s: %v", role, err)
		}
		assertDraftActivity(t, db, events, "b-"+role, "T", creator, "created", want)
	}

	// u1 opts in but sits below the staff floor: never a recipient, in any
	// of the cases above (asserted implicitly — u1 is in no expected set).
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE kind = 'draft_activity' AND recipient_id = 'u1'`).Scan(&n); err != nil {
		t.Fatalf("count u1 rows: %v", err)
	}
	if n != 0 {
		t.Errorf("plain user rows: got %d, want 0 (the staff floor)", n)
	}
}

// TestDraftActivityUnattributedDraftAudience pins the gate's ELSE arm: an
// unpublished row with NO creator (the import shape) weighs -1, so every
// opted-in STAFF member sees it (the staff floor is what keeps a plain user
// out — the weight comparison alone would let them in).
func TestDraftActivityUnattributedDraftAudience(t *testing.T) {
	t.Parallel()

	db := draftActivityFixture(t)
	ctx := t.Context()

	if _, err := db.Exec(`
		INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status, created_at_ms, updated_at_ms)
		VALUES ('legacy', 'Legacy Draft', '', 'desc', 'media/images/ab/x.jpg', 'draft', 1000, 1000)`); err != nil {
		t.Fatalf("seed unattributed draft: %v", err)
	}

	_, events, err := UpdateBlogPost(ctx, db, "legacy", 1, UpdateBlogPostParams{
		Title: "Legacy Draft v2", Subtitle: "", Description: "desc",
		ThumbnailURL: "media/images/ab/x.jpg", Status: "draft", UpdaterID: "sa", NowMS: 9_000,
	})
	if err != nil {
		t.Fatalf("edit unattributed draft: %v", err)
	}
	// Every opted-in staff member except the actor; u1 (opted in, below the
	// floor) never receives.
	assertDraftActivity(t, db, events, "legacy", "Legacy Draft v2", "sa", "updated",
		map[string]bool{"adm": true, "adm2": true, "mod": true})
}

// TestDraftActivityFeedProjection pins the feed side of the new column: a
// draft_activity row projects its action through ListNotifications, and the
// other kinds keep the empty action (the NULL column, never a stray value).
func TestDraftActivityFeedProjection(t *testing.T) {
	t.Parallel()

	db := draftActivityFixture(t)
	ctx := t.Context()

	if _, _, err := CreateBlogPost(ctx, db, CreateBlogPostParams{
		ID: "d1", Title: "Πρόχειρο", Subtitle: "", Description: "desc",
		ThumbnailURL: "media/images/ab/x.jpg", Status: "draft",
		CreatorID: "adm", UpdaterID: "adm", PublishedAtMS: 0, NowMS: 5_000,
	}); err != nil {
		t.Fatalf("create draft: %v", err)
	}
	insertUserNotification(t, db, "n-reply", "sa", "adm", "blog-posts", "d1", "c1", 4_000, nil)

	items, _, err := ListNotifications(ctx, db, "sa", identity.RoleWeight(identity.RoleSuperAdmin), true, 10, nil)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	byID := map[string]NotificationItem{}
	for _, it := range items {
		byID[it.ID] = it
	}
	draft, ok := byID[firstDraftActivityID(t, db)]
	if !ok {
		t.Fatalf("draft_activity item missing from the feed: %+v", items)
	}
	if draft.Kind != "draft_activity" || draft.DraftAction != "created" ||
		draft.ContentKind != "blog-posts" || draft.ContentID != "d1" ||
		draft.ContentTitle == nil || *draft.ContentTitle != "Πρόχειρο" || draft.CommentID != "" {
		t.Errorf("draft_activity item: got %+v", draft)
	}
	reply, ok := byID["n-reply"]
	if !ok {
		t.Fatalf("comment_reply item missing from the feed: %+v", items)
	}
	if reply.DraftAction != "" {
		t.Errorf("comment_reply item draft action = %q, want empty", reply.DraftAction)
	}
}

// firstDraftActivityID reads the single committed draft_activity row's id.
func firstDraftActivityID(t *testing.T, db *sql.DB) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`SELECT id FROM user_notifications WHERE kind = 'draft_activity'`).Scan(&id); err != nil {
		t.Fatalf("read draft_activity id: %v", err)
	}
	return id
}

// TestDraftActivityFeedReGatedBelowFloor pins the feed's read-side re-gate:
// the write-time audience is not a standing grant. A recipient demoted below
// the staff floor stops reading the draft notice in the feed, the pager
// count, and the unread badge, while the other rows survive.
func TestDraftActivityFeedReGatedBelowFloor(t *testing.T) {
	t.Parallel()

	db := draftActivityFixture(t)
	ctx := t.Context()

	if _, _, err := CreateBlogPost(ctx, db, CreateBlogPostParams{
		ID: "d1", Title: "Πρόχειρο", Subtitle: "", Description: "desc",
		ThumbnailURL: "media/images/ab/x.jpg", Status: "draft",
		CreatorID: "adm", UpdaterID: "adm", PublishedAtMS: 0, NowMS: 5_000,
	}); err != nil {
		t.Fatalf("create draft: %v", err)
	}
	draftID := firstDraftActivityID(t, db)
	// One non-staff entry so the below-floor feed is not empty.
	insertUserNotification(t, db, "n-reply", "sa", "adm", "blog-posts", "d1", "c1", 4_000, nil)

	// As a super-admin, the notice is visible (the recipient check above the
	// floor).
	items, _, err := ListNotifications(ctx, db, "sa", identity.RoleWeight(identity.RoleSuperAdmin), true, 10, nil)
	if err != nil {
		t.Fatalf("list as staff: %v", err)
	}
	seen := map[string]bool{}
	for _, it := range items {
		seen[it.ID] = true
	}
	if !seen[draftID] || !seen["n-reply"] {
		t.Fatalf("staff feed: got %v, want both the notice and the reply", seen)
	}

	// Demoted: the role change revokes sessions; the next sign-in is a plain
	// user. The notice must leave the feed, the count, and the badge.
	if _, err := db.Exec(`UPDATE users SET role = ? WHERE id = 'sa'`, identity.RoleUser); err != nil {
		t.Fatalf("demote sa: %v", err)
	}
	weight := identity.RoleWeight(identity.RoleUser)
	items, _, err = ListNotifications(ctx, db, "sa", weight, false, 10, nil)
	if err != nil {
		t.Fatalf("list after demotion: %v", err)
	}
	for _, it := range items {
		if it.ID == draftID {
			t.Errorf("draft notice still in the below-floor feed: %+v", it)
		}
	}
	if len(items) != 1 || items[0].ID != "n-reply" {
		t.Errorf("below-floor feed: got %+v, want only n-reply", items)
	}
	if n, err := CountNotifications(ctx, db, "sa", weight, false); err != nil {
		t.Fatalf("count after demotion: %v", err)
	} else if n != 1 {
		t.Errorf("below-floor count = %d, want 1", n)
	}
	if n, err := UnreadNotificationCount(ctx, db, "sa", weight, false); err != nil {
		t.Fatalf("unread after demotion: %v", err)
	} else if n != 1 {
		t.Errorf("below-floor unread = %d, want 1 (the draft notice excluded)", n)
	}
}

// TestDraftActivityFeedGatedByCurrentCreatorWeight pins the read re-gate at
// the staff floor: a staff recipient whose weight is no longer strictly
// above the creator's stops reading the notice — the write-time audience is
// not a standing grant.
func TestDraftActivityFeedGatedByCurrentCreatorWeight(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		viewerRole string
		adjust     string // SQL value for the changed actor (adm or sa)
		changedID  string
	}{
		{"recipient demoted to the creator's weight", identity.RoleAdmin, identity.RoleAdmin, "sa"},
		{"creator promoted to the recipient's weight", identity.RoleSuperAdmin, identity.RoleSuperAdmin, "adm"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := draftActivityFixture(t)
			ctx := t.Context()

			if _, _, err := CreateBlogPost(ctx, db, CreateBlogPostParams{
				ID: "d1", Title: "Πρόχειρο", Subtitle: "", Description: "desc",
				ThumbnailURL: "media/images/ab/x.jpg", Status: "draft",
				CreatorID: "adm", UpdaterID: "adm", PublishedAtMS: 0, NowMS: 5_000,
			}); err != nil {
				t.Fatalf("create draft: %v", err)
			}
			draftID := firstDraftActivityID(t, db)
			insertUserNotification(t, db, "n-reply", "sa", "adm", "blog-posts", "d1", "c1", 4_000, nil)

			weight := identity.RoleWeight(identity.RoleSuperAdmin)
			it := listIDs(t, db, "sa", weight, true)
			if !it[draftID] || !it["n-reply"] {
				t.Fatalf("staff feed: got %v, want both the notice and the reply", it)
			}

			if _, err := db.Exec(`UPDATE users SET role = ? WHERE id = ?`, c.adjust, c.changedID); err != nil {
				t.Fatalf("adjust %s: %v", c.changedID, err)
			}
			weight = identity.RoleWeight(c.viewerRole)
			it = listIDs(t, db, "sa", weight, true)
			if it[draftID] {
				t.Error("draft notice still served after the weight inversion")
			}
			if !it["n-reply"] {
				t.Errorf("feed: got %v, want n-reply still served", it)
			}
			if n, err := CountNotifications(ctx, db, "sa", weight, true); err != nil {
				t.Fatalf("count: %v", err)
			} else if n != 1 {
				t.Errorf("count = %d, want 1", n)
			}
			if n, err := UnreadNotificationCount(ctx, db, "sa", weight, true); err != nil {
				t.Fatalf("unread: %v", err)
			} else if n != 1 {
				t.Errorf("unread = %d, want 1", n)
			}
		})
	}
}

// listIDs reads one viewer's feed ids through ListNotifications.
func listIDs(t *testing.T, db *sql.DB, viewerID string, weight int, includeStaff bool) map[string]bool {
	t.Helper()
	items, _, err := ListNotifications(t.Context(), db, viewerID, weight, includeStaff, 10, nil)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	ids := map[string]bool{}
	for _, it := range items {
		ids[it.ID] = true
	}
	return ids
}

// TestDraftActivitySkipsSuspended pins the active-recipient rule on the
// staff notice: a suspended opt-in receives neither a row nor an event.
func TestDraftActivitySkipsSuspended(t *testing.T) {
	t.Parallel()

	db := draftActivityFixture(t)
	ctx := t.Context()

	if _, err := db.Exec(`UPDATE users SET status = ? WHERE id = 'sa'`, identity.StatusSuspended); err != nil {
		t.Fatalf("suspend sa: %v", err)
	}

	_, events, err := CreateBlogPost(ctx, db, CreateBlogPostParams{
		ID: "d1", Title: "Πρόχειρο", Subtitle: "", Description: "desc",
		ThumbnailURL: "media/images/ab/x.jpg", Status: "draft",
		CreatorID: "adm", UpdaterID: "adm", PublishedAtMS: 0, NowMS: 5_000,
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	assertDraftActivity(t, db, events, "d1", "Πρόχειρο", "adm", "created", map[string]bool{})
}
