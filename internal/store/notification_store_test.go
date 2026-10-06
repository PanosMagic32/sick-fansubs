package store

import (
	"database/sql"
	"errors"
	"testing"

	"sick-fansubs/internal/identity"
)

// Notification store tests (the unified feed). The visibility gate reads the
// target_role snapshot — events
// are inserted directly with controlled roles, so the matrix below pins the
// SQL CASE against identity.RoleWeight for every viewer×target pair.

// insertEvent seeds one audit_events row. request_id/remote_addr are NOT
// NULL columns but carry no meaning for the feed — the empty strings
// mirror the maintenance actor's offline emission (cmd/resetpassword).
func insertEvent(t *testing.T, db *sql.DB, id, event, result, actorID, targetID, targetRole string, createdMs int64) {
	t.Helper()
	if _, err := db.Exec(`
			INSERT INTO audit_events
				(id, event, result, actor_id, target_id, target_role, request_id, remote_addr, created_at_ms)
			VALUES (?, ?, ?, ?, ?, ?, '', '', ?)`,
		id, event, result, nullableString(actorID), nullableString(targetID),
		nullableString(targetRole), createdMs); err != nil {
		t.Fatalf("insert event %s: %v", id, err)
	}
}

// eventIDsByCreated returns the feed item ids for a full page, newest
// first — the ordering contract (created_at_ms DESC, id ASC).
func eventIDsByCreated(t *testing.T, db *sql.DB, weight, limit int) []string {
	t.Helper()
	items, _, err := ListNotifications(t.Context(), db, "v1", weight, true, limit, nil)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	return ids
}

// TestNotificationVisibilityMatrix pins the feed's visibility rule against
// identity.RoleWeight for every viewer/target pair (the draft-gate canary
// precedent: a weight change in either Go or SQL fails here), plus the two
// hard exclusions — events outside the five feed kinds and events without
// a target_role snapshot are never visible to anyone.
func TestNotificationVisibilityMatrix(t *testing.T) {
	db := openStoreDB(t)

	mustCreateUser(t, db, "tuser", "tuser", "tuser", "tuser@example.com")
	mustCreateUser(t, db, "tmod", "tmod", "tmod", "tmod@example.com")
	mustCreateUser(t, db, "tadmin", "tadmin", "tadmin", "tadmin@example.com")
	mustCreateUser(t, db, "tsuper", "tsuper", "tsuper", "tsuper@example.com")
	mustCreateUser(t, db, "v1", "v1", "v1", "v1@example.com")

	roleEvents := map[string]string{
		identity.RoleUser:       "e-user",
		identity.RoleModerator:  "e-mod",
		identity.RoleAdmin:      "e-admin",
		identity.RoleSuperAdmin: "e-super",
	}
	for role, id := range roleEvents {
		insertEvent(t, db, id, "password_reset", "success", "actor", "t-"+role, role, 5000)
	}
	// A non-feed event WITH a super-admin target_role: the event-name
	// filter excludes it even for super-admins.
	insertEvent(t, db, "e-not-feed", "sign_in_success", "success", "actor", "tsuper", identity.RoleSuperAdmin, 5000)
	// A feed event WITHOUT a target_role snapshot: excluded for everyone.
	insertEvent(t, db, "e-no-role", "password_reset", "success", "actor", "tuser", "", 5000)
	// A feed event with a NON-NULL but UNKNOWN target_role: excluded for
	// everyone (the defensive half of the visibility rule — unknown roles
	// weigh above every staff role in the SQL CASE).
	insertEvent(t, db, "e-unknown-role", "password_reset", "success", "actor", "tuser", "owner", 5000)

	for viewerRole, weight := range map[string]int{
		identity.RoleUser:       0,
		identity.RoleModerator:  1,
		identity.RoleAdmin:      2,
		identity.RoleSuperAdmin: 3,
	} {
		want := make(map[string]bool)
		for targetRole, id := range roleEvents {
			if identity.RoleWeight(targetRole) <= weight {
				want[id] = true
			}
		}
		ids := eventIDsByCreated(t, db, weight, 100)
		if len(ids) != len(want) {
			t.Fatalf("viewer %s: got %d events %v, want %d (%v)",
				viewerRole, len(ids), ids, len(want), want)
		}
		for _, id := range ids {
			if !want[id] {
				t.Fatalf("viewer %s: unexpected event %s visible", viewerRole, id)
			}
		}
	}
}

// TestListNotificationsOrderingAndKeyset pins the ordering contract and
// the two-shape keyset continuation.
func TestListNotificationsOrderingAndKeyset(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "v1", "v1", "v1", "v1@example.com")
	mustCreateUser(t, db, "t1", "t1", "t1", "t1@example.com")
	// Newest first: e5 (5000) … e1 (1000); e3a/e3b share 3000 and must
	// order by id ASC as the tie-breaker.
	insertEvent(t, db, "e1", "password_reset", "success", "actor", "t1", identity.RoleUser, 1000)
	insertEvent(t, db, "e3b", "password_reset", "success", "actor", "t1", identity.RoleUser, 3000)
	insertEvent(t, db, "e5", "password_reset", "success", "actor", "t1", identity.RoleUser, 5000)
	insertEvent(t, db, "e3a", "password_reset", "success", "actor", "t1", identity.RoleUser, 3000)
	insertEvent(t, db, "e4", "password_reset", "success", "actor", "t1", identity.RoleUser, 4000)

	weight := identity.RoleWeight(identity.RoleModerator)

	// First page: limit 2 → e5, e4; hasNext true.
	page1, hasNext, err := ListNotifications(ctx, db, "v1", weight, true, 2, nil)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if !hasNext {
		t.Error("page 1: want hasNext true")
	}
	if len(page1) != 2 || page1[0].ID != "e5" || page1[1].ID != "e4" {
		t.Fatalf("page 1: got %v", page1)
	}

	// Second page: after e4 → e3a, e3b (same ms, id ASC); hasNext true.
	page2, hasNext, err := ListNotifications(ctx, db, "v1", weight, true, 2,
		&NotificationPageKey{CreatedAtMS: page1[1].CreatedAtMS, ID: page1[1].ID})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if !hasNext {
		t.Error("page 2: want hasNext true")
	}
	if len(page2) != 2 || page2[0].ID != "e3a" || page2[1].ID != "e3b" {
		t.Fatalf("page 2: got %v", page2)
	}

	// Third page: after e3a → e1 only; hasNext false (no limit+1th row).
	page3, hasNext, err := ListNotifications(ctx, db, "v1", weight, true, 2,
		&NotificationPageKey{CreatedAtMS: page2[1].CreatedAtMS, ID: page2[1].ID})
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if hasNext {
		t.Error("page 3: want hasNext false")
	}
	if len(page3) != 1 || page3[0].ID != "e1" {
		t.Fatalf("page 3: got %v", page3)
	}
}

// TestListNotificationsUsernames pins the display-data JOINs: present
// users render their usernames, a deleted target is nil, and the
// maintenance actor (no users row) is nil — the frontend's system label.
func TestListNotificationsUsernames(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "v1", "v1", "v1", "v1@example.com")
	mustCreateUser(t, db, "actor", "Actorus", "actorus", "actorus@example.com")
	mustCreateUser(t, db, "target", "Targetus", "targetus", "targetus@example.com")

	insertEvent(t, db, "e-ok", "password_reset", "success", "actor", "target", identity.RoleUser, 1000)
	insertEvent(t, db, "e-deleted-target", "user_deleted", "success", "actor", "gone", identity.RoleUser, 1000)
	insertEvent(t, db, "e-maintenance", "password_reset", "success", "maintenance", "target", identity.RoleUser, 1000)

	items, _, err := ListNotifications(ctx, db, "v1", identity.RoleWeight(identity.RoleAdmin), true, 10, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	byID := make(map[string]NotificationItem, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}

	if ok := byID["e-ok"]; ok.ActorUsername == nil || *ok.ActorUsername != "Actorus" ||
		ok.TargetUsername == nil || *ok.TargetUsername != "Targetus" ||
		ok.TargetRole == nil || *ok.TargetRole != identity.RoleUser {
		t.Errorf("e-ok: got actor=%v target=%v role=%v", ok.ActorUsername, ok.TargetUsername, ok.TargetRole)
	}
	if ok := byID["e-ok"]; ok.Kind != "password_reset" || ok.Result != "success" {
		t.Errorf("e-ok kind/result: %q/%q", ok.Kind, ok.Result)
	}
	if dt := byID["e-deleted-target"]; dt.TargetUsername != nil {
		t.Errorf("e-deleted-target: want nil target username, got %q", *dt.TargetUsername)
	}
	if m := byID["e-maintenance"]; m.ActorUsername != nil {
		t.Errorf("e-maintenance: want nil actor username, got %q", *m.ActorUsername)
	}
}

// TestUnifiedFeed_CommentItems pins the user_notifications half: the item
// shape (kind discriminator + content fields, no result/target fields),
// the contentTitle join regardless of status, the NULL title for deleted
// content, and the merged ordering across both sources.
func TestUnifiedFeed_CommentItems(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "v1", "v1", "v1", "v1@example.com")
	mustCreateUser(t, db, "actor", "Actorus", "actorus", "actorus@example.com")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "b1", Title: "A post", ThumbnailURL: "/media/images/x.jpg", PublishedAtMS: 3000}, "published")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "draft", Title: "A draft", ThumbnailURL: "/media/images/x.jpg"}, "draft")
	// The comment rows dangle deliberately (no FK) — the feed only needs
	// the ids.
	insertUserNotification(t, db, "n1", "v1", "actor", "blog-posts", "b1", "c1", 3000, nil)
	insertUserNotification(t, db, "n2", "v1", "actor", "blog-posts", "draft", "c2", 1000, nil)
	insertUserNotification(t, db, "n3", "v1", "actor", "blog-posts", "gone", "c3", 500, nil)
	// A staff audit event interleaves with the comment rows by time.
	insertEvent(t, db, "e1", "password_reset", "success", "actor", "t1", identity.RoleUser, 2000)

	items, _, err := ListNotifications(ctx, db, "v1", identity.RoleWeight(identity.RoleModerator), true, 10, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// Ordering across both sources: n1(3000), e1(2000), n2(1000), n3(500).
	if len(items) != 4 {
		t.Fatalf("items: got %d, want 4 (%v)", len(items), items)
	}
	ids := make([]string, 4)
	for i, it := range items {
		ids[i] = it.ID
	}
	wantIDs := []string{"n1", "e1", "n2", "n3"}
	for i := range wantIDs {
		if ids[i] != wantIDs[i] {
			t.Errorf("order %d: got %q, want %q", i, ids[i], wantIDs[i])
		}
	}

	n1 := items[0]
	if n1.Kind != "comment_reply" || n1.Result != "" || n1.TargetUsername != nil || n1.TargetRole != nil {
		t.Errorf("n1 shape: %+v", n1)
	}
	if n1.ContentTitle == nil || *n1.ContentTitle != "A post" || n1.ContentKind != "blog-posts" || n1.ContentID != "b1" || n1.CommentID != "c1" {
		t.Errorf("n1 content fields: %+v", n1)
	}
	// The draft's title joins regardless of status.
	if n2 := items[2]; n2.ContentTitle == nil || *n2.ContentTitle != "A draft" {
		t.Errorf("n2 title should join regardless of status: %+v", items[2].ContentTitle)
	}
	// Deleted content → NULL title (the frontend's generic label).
	if n3 := items[3]; n3.ContentTitle != nil {
		t.Errorf("n3 title should be nil for deleted content, got %q", *n3.ContentTitle)
	}
}

// TestUnifiedFeed_NonStaffScope pins the moderator+-only staff half: a
// role=user viewer sees ONLY their own user_notifications rows, never
// account audit events.
func TestUnifiedFeed_NonStaffScope(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "v1", "v1", "v1", "v1@example.com")
	mustCreateUser(t, db, "t1", "t1", "t1", "t1@example.com")
	mustCreateUser(t, db, "actor", "actor", "actor", "actor@example.com")
	insertEvent(t, db, "e1", "password_reset", "success", "actor", "t1", identity.RoleUser, 2000)
	insertUserNotification(t, db, "n1", "v1", "actor", "blog-posts", "b1", "c1", 1000, nil)
	insertUserNotification(t, db, "n-other", "t1", "actor", "blog-posts", "b1", "c2", 500, nil)

	items, _, err := ListNotifications(ctx, db, "v1", identity.RoleWeight(identity.RoleUser), false, 10, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 || items[0].ID != "n1" {
		t.Fatalf("non-staff feed: got %v, want only n1", items)
	}
}

// TestCountNotifications_ViewerScope proves the count is scoped like the
// read: the viewer's own user_notifications rows count, another recipient's
// row does not.
func TestCountNotifications_ViewerScope(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "v1", "v1", "v1", "v1@example.com")
	mustCreateUser(t, db, "v2", "v2", "v2", "v2@example.com")
	mustCreateUser(t, db, "actor", "actor", "actor", "actor@example.com")
	insertUserNotification(t, db, "n1", "v1", "actor", "blog-posts", "b1", "c1", 1000, nil)
	insertUserNotification(t, db, "n-other", "v2", "actor", "blog-posts", "b1", "c2", 2000, nil)

	weight := identity.RoleWeight(identity.RoleUser)
	n, err := CountNotifications(ctx, db, "v1", weight, false)
	if err != nil {
		t.Fatalf("CountNotifications: %v", err)
	}
	if n != 1 {
		t.Errorf("count = %d, want 1 (the viewer's own row only)", n)
	}

	items, _, err := ListNotifications(ctx, db, "v1", weight, false, 10, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != n {
		t.Errorf("list returned %d rows, count says %d — the mirror drifted", len(items), n)
	}
}

// TestUnreadNotificationCount pins the badge semantics: visible events
// minus read rows, per viewer, with hidden events never counted.
func TestUnreadNotificationCount(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "v1", "v1", "v1", "v1@example.com")
	mustCreateUser(t, db, "v2", "v2", "v2", "v2@example.com")
	mustCreateUser(t, db, "t1", "t1", "t1", "t1@example.com")
	mustCreateUser(t, db, "actor", "actor", "actor", "actor@example.com")

	insertEvent(t, db, "e1", "password_reset", "success", "actor", "t1", identity.RoleUser, 1000)
	insertEvent(t, db, "e2", "password_reset", "success", "actor", "t1", identity.RoleUser, 2000)
	insertEvent(t, db, "e3", "password_reset", "success", "actor", "t1", identity.RoleUser, 3000)
	// Hidden from moderators (super-admin target) — never counted.
	insertEvent(t, db, "e-hidden", "password_reset", "success", "actor", "tsuper", identity.RoleSuperAdmin, 4000)
	// The user_notifications half counts the viewer's unread rows.
	insertUserNotification(t, db, "n1", "v1", "actor", "blog-posts", "b1", "c1", 500, nil)

	weight := identity.RoleWeight(identity.RoleModerator)

	count, err := UnreadNotificationCount(ctx, db, "v1", weight, true)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 4 { // e1,e2,e3 + n1
		t.Fatalf("initial count = %d, want 4", count)
	}

	if err := MarkNotificationRead(ctx, db, "v1", "e1", weight, true, 1); err != nil {
		t.Fatalf("mark e1: %v", err)
	}
	if err := MarkNotificationRead(ctx, db, "v1", "n1", weight, true, 1); err != nil {
		t.Fatalf("mark n1: %v", err)
	}
	// A read row for ANOTHER viewer must not change v1's badge.
	if err := MarkNotificationRead(ctx, db, "v2", "e2", weight, true, 1); err != nil {
		t.Fatalf("mark e2 for v2: %v", err)
	}

	count, err = UnreadNotificationCount(ctx, db, "v1", weight, true)
	if err != nil {
		t.Fatalf("count after marks: %v", err)
	}
	if count != 2 {
		t.Fatalf("count after marks = %d, want 2", count)
	}

	// A non-staff viewer's count covers only their own rows.
	count, err = UnreadNotificationCount(ctx, db, "v2", identity.RoleWeight(identity.RoleUser), false)
	if err != nil {
		t.Fatalf("non-staff count: %v", err)
	}
	if count != 0 {
		t.Fatalf("non-staff count = %d, want 0 (no own rows)", count)
	}
}

// TestMarkNotificationRead pins the masked 404 and the idempotent repeat
// across both id spaces.
func TestMarkNotificationRead(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "v1", "v1", "v1", "v1@example.com")
	mustCreateUser(t, db, "v2", "v2", "v2", "v2@example.com")
	mustCreateUser(t, db, "t1", "t1", "t1", "t1@example.com")
	mustCreateUser(t, db, "actor", "actor", "actor", "actor@example.com")
	insertEvent(t, db, "e1", "password_reset", "success", "actor", "t1", identity.RoleUser, 1000)
	insertEvent(t, db, "e-hidden", "password_reset", "success", "actor", "tsuper", identity.RoleSuperAdmin, 2000)
	insertUserNotification(t, db, "n1", "v1", "actor", "blog-posts", "b1", "c1", 500, nil)

	weight := identity.RoleWeight(identity.RoleModerator)

	// Audit space: first mark, idempotent repeat, exactly one read row.
	if err := MarkNotificationRead(ctx, db, "v1", "e1", weight, true, 1); err != nil {
		t.Fatalf("first mark: %v", err)
	}
	if err := MarkNotificationRead(ctx, db, "v1", "e1", weight, true, 2); err != nil {
		t.Fatalf("repeat mark: %v", err)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notification_reads WHERE user_id = 'v1' AND event_id = 'e1'`).Scan(&rows); err != nil {
		t.Fatalf("count read rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("read rows = %d, want 1", rows)
	}

	// User space: the recipient marks their own row.
	if err := MarkNotificationRead(ctx, db, "v1", "n1", weight, true, 3); err != nil {
		t.Fatalf("mark n1: %v", err)
	}
	var readAt sql.NullInt64
	if err := db.QueryRow(`SELECT read_at_ms FROM user_notifications WHERE id = 'n1'`).Scan(&readAt); err != nil {
		t.Fatalf("read n1 state: %v", err)
	}
	if !readAt.Valid || readAt.Int64 != 3 {
		t.Errorf("n1 read_at_ms: %v, want 3", readAt)
	}

	// Per-space authorization: another user marking n1 is masked; a
	// non-staff viewer cannot mark an audit id; unknown ids are masked.
	if err := MarkNotificationRead(ctx, db, "v2", "n1", weight, true, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign user row: got %v, want ErrNotFound", err)
	}
	if err := MarkNotificationRead(ctx, db, "v1", "e1", identity.RoleWeight(identity.RoleUser), false, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("non-staff audit id: got %v, want ErrNotFound", err)
	}
	if err := MarkNotificationRead(ctx, db, "v1", "nope", weight, true, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: got %v, want ErrNotFound", err)
	}
	if err := MarkNotificationRead(ctx, db, "v1", "e-hidden", weight, true, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("hidden event: got %v, want ErrNotFound", err)
	}
}

// TestMarkAllNotificationsRead pins the read-all scope and idempotency
// across both sources.
func TestMarkAllNotificationsRead(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "v1", "v1", "v1", "v1@example.com")
	mustCreateUser(t, db, "t1", "t1", "t1", "t1@example.com")
	mustCreateUser(t, db, "actor", "actor", "actor", "actor@example.com")
	insertEvent(t, db, "e1", "password_reset", "success", "actor", "t1", identity.RoleUser, 1000)
	insertEvent(t, db, "e2", "role_changed", "failure", "actor", "t1", identity.RoleUser, 2000)
	insertEvent(t, db, "e-hidden", "password_reset", "success", "actor", "tsuper", identity.RoleSuperAdmin, 3000)
	insertUserNotification(t, db, "n1", "v1", "actor", "blog-posts", "b1", "c1", 500, nil)
	insertUserNotification(t, db, "n-other", "t1", "actor", "blog-posts", "b1", "c2", 500, nil)

	weight := identity.RoleWeight(identity.RoleModerator)

	n, err := MarkAllNotificationsRead(ctx, db, "v1", weight, true, 1)
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if n != 3 { // e1 + e2 read rows, n1 updated
		t.Fatalf("first read-all marked %d rows, want 3", n)
	}

	// Idempotent: the second call marks nothing new.
	n, err = MarkAllNotificationsRead(ctx, db, "v1", weight, true, 2)
	if err != nil {
		t.Fatalf("read all repeat: %v", err)
	}
	if n != 0 {
		t.Fatalf("repeat read-all marked %d rows, want 0", n)
	}

	// The hidden event never got a read row; another user's row is
	// untouched.
	var hidden int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notification_reads WHERE event_id = 'e-hidden'`).Scan(&hidden); err != nil {
		t.Fatalf("count hidden reads: %v", err)
	}
	if hidden != 0 {
		t.Fatalf("hidden event read rows = %d, want 0", hidden)
	}
	var otherRead sql.NullInt64
	if err := db.QueryRow(`SELECT read_at_ms FROM user_notifications WHERE id = 'n-other'`).Scan(&otherRead); err != nil {
		t.Fatalf("read other row: %v", err)
	}
	if otherRead.Valid {
		t.Errorf("another user's row must stay unread, got %v", otherRead.Int64)
	}

	// A non-staff read-all touches only the viewer's own rows.
	insertUserNotification(t, db, "n2", "v1", "actor", "blog-posts", "b1", "c3", 400, nil)
	n, err = MarkAllNotificationsRead(ctx, db, "v1", identity.RoleWeight(identity.RoleUser), false, 3)
	if err != nil {
		t.Fatalf("non-staff read all: %v", err)
	}
	if n != 1 {
		t.Fatalf("non-staff read-all marked %d rows, want 1 (n2 only)", n)
	}
}

// TestNotificationReadsCascade pins the two cascade FKs: read rows die
// with the account and with the event.
func TestNotificationReadsCascade(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "v1", "v1", "v1", "v1@example.com")
	mustCreateUser(t, db, "t1", "t1", "t1", "t1@example.com")
	insertEvent(t, db, "e1", "password_reset", "success", "actor", "t1", identity.RoleUser, 1000)
	insertEvent(t, db, "e2", "password_reset", "success", "actor", "t1", identity.RoleUser, 2000)

	weight := identity.RoleWeight(identity.RoleModerator)
	if err := MarkNotificationRead(ctx, db, "v1", "e1", weight, true, 1); err != nil {
		t.Fatalf("mark e1: %v", err)
	}
	if err := MarkNotificationRead(ctx, db, "v1", "e2", weight, true, 1); err != nil {
		t.Fatalf("mark e2: %v", err)
	}

	readCount := func() int {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM notification_reads`).Scan(&n); err != nil {
			t.Fatalf("count reads: %v", err)
		}
		return n
	}
	if n := readCount(); n != 2 {
		t.Fatalf("before cascades: %d read rows, want 2", n)
	}

	// Event cascade: delete e1 → its read row dies.
	if _, err := db.Exec(`DELETE FROM audit_events WHERE id = 'e1'`); err != nil {
		t.Fatalf("delete event: %v", err)
	}
	if n := readCount(); n != 1 {
		t.Fatalf("after event delete: %d read rows, want 1", n)
	}

	// Account cascade: delete the viewer → the remaining read row dies.
	if err := DeleteUser(ctx, db, "v1"); err != nil {
		t.Fatalf("delete viewer: %v", err)
	}
	if n := readCount(); n != 0 {
		t.Fatalf("after user delete: %d read rows, want 0", n)
	}
}

// TestListNotificationsReadFlags pins the store half of the read flag: `read`
// comes from the viewer's notification_reads mark in the audit space and
// from the row's own read_at_ms in the event space, and one viewer's marks
// are never another viewer's.
func TestListNotificationsReadFlags(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "v1", "v1", "v1", "v1@example.com")
	mustCreateUser(t, db, "v2", "v2", "v2", "v2@example.com")
	mustCreateUser(t, db, "t1", "t1", "t1", "t1@example.com")
	mustCreateUser(t, db, "actor", "actor", "actor", "actor@example.com")

	insertEvent(t, db, "e-read", "password_reset", "success", "actor", "t1", identity.RoleUser, 3000)
	insertEvent(t, db, "e-unread", "password_reset", "success", "actor", "t1", identity.RoleUser, 2000)
	readMS := int64(4000)
	insertUserNotification(t, db, "n-read", "v1", "actor", "blog-posts", "b1", "c1", 1000, &readMS)
	insertUserNotification(t, db, "n-unread", "v1", "actor", "blog-posts", "b1", "c2", 500, nil)

	weight := identity.RoleWeight(identity.RoleModerator)
	if err := MarkNotificationRead(ctx, db, "v1", "e-read", weight, true, 5000); err != nil {
		t.Fatalf("mark read: %v", err)
	}

	items, _, err := ListNotifications(ctx, db, "v1", weight, true, 10, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := map[string]bool{}
	for _, it := range items {
		got[it.ID] = it.Read
	}
	for id, want := range map[string]bool{
		"e-read":   true,
		"e-unread": false,
		"n-read":   true,
		"n-unread": false,
	} {
		if v, ok := got[id]; !ok {
			t.Errorf("item %s missing from the page (got %v)", id, got)
		} else if v != want {
			t.Errorf("item %s read = %v, want %v", id, v, want)
		}
	}

	// A second viewer sees the same audit events with none of v1's marks,
	// and never v1's event rows.
	items2, _, err := ListNotifications(ctx, db, "v2", weight, true, 10, nil)
	if err != nil {
		t.Fatalf("list v2: %v", err)
	}
	for _, it := range items2 {
		if it.Read {
			t.Errorf("v2 must not inherit v1's read marks: %s", it.ID)
		}
		if it.ID == "n-read" || it.ID == "n-unread" {
			t.Errorf("v2 must not see v1's event rows: %s", it.ID)
		}
	}
}

// TestDraftActivityMutationsBelowFloor pins the re-gate on the user-space
// write paths: a below-floor viewer cannot address a draft_activity row at
// all — mark read, mark all read, and every delete path leave it untouched —
// while a staff viewer keeps the path.
func TestDraftActivityMutationsBelowFloor(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "v1", "v1", "v1", "v1@example.com")
	mustCreateUser(t, db, "actor", "actor", "actor", "actor@example.com")
	if _, err := db.Exec(`
		INSERT INTO user_notifications (id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, draft_action, created_at_ms)
		VALUES ('n-draft', 'v1', 'draft_activity', 'actor', 'blog-posts', 'b1', NULL, 'created', 1000)`); err != nil {
		t.Fatalf("seed draft notice: %v", err)
	}

	belowFloor := identity.RoleWeight(identity.RoleUser)
	if err := MarkNotificationRead(ctx, db, "v1", "n-draft", belowFloor, false, 2000); !errors.Is(err, ErrNotFound) {
		t.Errorf("below-floor mark read: got %v, want ErrNotFound", err)
	}
	if n, err := MarkAllNotificationsRead(ctx, db, "v1", belowFloor, false, 2000); err != nil || n != 0 {
		t.Errorf("below-floor mark all: n = %d, err = %v, want 0, nil", n, err)
	}
	if ok, err := DeleteNotification(ctx, db, "v1", "n-draft", belowFloor, false, 2000); err != nil || ok {
		t.Errorf("below-floor delete: ok = %v, err = %v, want false, nil", ok, err)
	}
	if n, err := DeleteReadNotifications(ctx, db, "v1", false); err != nil || n != 0 {
		t.Errorf("below-floor clear read: n = %d, err = %v, want 0, nil", n, err)
	}
	if n, err := DeleteAllNotifications(ctx, db, "v1", belowFloor, false, 2000); err != nil || n != 0 {
		t.Errorf("below-floor delete-all: n = %d, err = %v, want 0, nil", n, err)
	}
	var read sql.NullInt64
	if err := db.QueryRow(`SELECT read_at_ms FROM user_notifications WHERE id = 'n-draft'`).Scan(&read); err != nil {
		t.Fatalf("read draft row: %v", err)
	}
	if read.Valid {
		t.Errorf("draft notice read_at_ms = %d, want NULL after the below-floor writes", read.Int64)
	}

	// Staff keeps the path: the mark read lands.
	if err := MarkNotificationRead(ctx, db, "v1", "n-draft", identity.RoleWeight(identity.RoleAdmin), true, 3000); err != nil {
		t.Fatalf("staff mark read: %v", err)
	}
	if err := db.QueryRow(`SELECT read_at_ms FROM user_notifications WHERE id = 'n-draft'`).Scan(&read); err != nil {
		t.Fatalf("read draft row after staff mark: %v", err)
	}
	if !read.Valid || read.Int64 != 3000 {
		t.Errorf("draft notice read_at_ms = %+v, want 3000", read)
	}
}

// TestDeleteNotification pins the per-item delete at the store: an event row
// of the viewer's own is deletable exactly once; a visible audit event is
// DISMISSED for the viewer (the ledger row survives, the entry leaves that
// viewer's feed, and a dismissed id can no longer be marked read); another
// recipient's row and an unknown id report false — the same masked refusal
// the read path uses.
func TestDeleteNotification(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "v1", "v1", "v1", "v1@example.com")
	mustCreateUser(t, db, "v2", "v2", "v2", "v2@example.com")
	mustCreateUser(t, db, "actor", "actor", "actor", "actor@example.com")
	insertEvent(t, db, "e1", "password_reset", "success", "actor", "v1", identity.RoleModerator, 3000)
	insertUserNotification(t, db, "n-mine", "v1", "actor", "blog-posts", "b1", "c1", 2000, nil)
	insertUserNotification(t, db, "n-other", "v2", "actor", "blog-posts", "b1", "c2", 1000, nil)

	weight := identity.RoleWeight(identity.RoleModerator)

	for _, id := range []string{"n-other", "unknown"} {
		ok, err := DeleteNotification(ctx, db, "v1", id, weight, true, 4000)
		if err != nil {
			t.Fatalf("delete %s: %v", id, err)
		}
		if ok {
			t.Errorf("delete %s reported success — it is not v1's event row", id)
		}
	}

	// The visible audit event: dismissed for v1, repeat refused.
	ok, err := DeleteNotification(ctx, db, "v1", "e1", weight, true, 4000)
	if err != nil {
		t.Fatalf("dismiss audit event: %v", err)
	}
	if !ok {
		t.Fatal("dismissing a visible audit event must report success")
	}
	again, err := DeleteNotification(ctx, db, "v1", "e1", weight, true, 5000)
	if err != nil {
		t.Fatalf("repeat dismiss: %v", err)
	}
	if again {
		t.Error("a second dismiss of the same id must report false")
	}

	// The viewer's own event row: deleted, repeat refused.
	ok, err = DeleteNotification(ctx, db, "v1", "n-mine", weight, true, 4000)
	if err != nil {
		t.Fatalf("delete own row: %v", err)
	}
	if !ok {
		t.Fatal("deleting the viewer's own event row must report success")
	}
	if again, err = DeleteNotification(ctx, db, "v1", "n-mine", weight, true, 4000); err != nil {
		t.Fatalf("repeat delete: %v", err)
	} else if again {
		t.Error("a second delete of the same id must report false")
	}

	// The dismissal row is recorded, the ledger row survives, and the entry
	// leaves v1's feed while v2 — a peer with the same weight — still sees it.
	var dismissed int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notification_reads
		WHERE user_id = 'v1' AND event_id = 'e1' AND dismissed_at_ms IS NOT NULL`).Scan(&dismissed); err != nil {
		t.Fatalf("count dismissal: %v", err)
	}
	if dismissed != 1 {
		t.Errorf("dismissal rows = %d, want 1", dismissed)
	}
	var audits int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE id = 'e1'`).Scan(&audits); err != nil {
		t.Fatalf("count audits: %v", err)
	}
	if audits != 1 {
		t.Error("the audit ledger must be untouched")
	}
	if ids := eventIDsByCreated(t, db, weight, 10); len(ids) != 0 {
		t.Errorf("v1's feed = %v, want empty after the dismissals and deletes", ids)
	}
	if count, err := CountNotifications(ctx, db, "v1", weight, true); err != nil || count != 0 {
		t.Errorf("CountNotifications after the deletions = %d, err = %v, want 0, nil", count, err)
	}
	items, _, err := ListNotifications(ctx, db, "v2", weight, true, 10, nil)
	if err != nil {
		t.Fatalf("list for v2: %v", err)
	}
	found := false
	for _, it := range items {
		if it.ID == "e1" {
			found = true
		}
	}
	if !found {
		t.Errorf("v2's feed = %+v, want e1 still visible for the peer", items)
	}

	// A dismissed id is no longer markable, and the badge ignores it.
	if err := MarkNotificationRead(ctx, db, "v1", "e1", weight, true, 6000); !errors.Is(err, ErrNotFound) {
		t.Errorf("marking a dismissed event: got %v, want ErrNotFound", err)
	}
	if n, err := MarkAllNotificationsRead(ctx, db, "v1", weight, true, 6000); err != nil || n != 0 {
		t.Errorf("mark-all after dismissal: n = %d, err = %v, want 0, nil", n, err)
	}
	if unread, err := UnreadNotificationCount(ctx, db, "v1", weight, true); err != nil || unread != 0 {
		t.Errorf("unread after dismissal = %d, err = %v, want 0, nil", unread, err)
	}
}

// TestDeleteAllNotifications pins the whole-feed sweep at the store: every
// event row of the viewer leaves, every visible audit event is dismissed
// (upserting an existing read row instead of duplicating it), other viewers
// and the ledger itself are untouched, and the call is idempotent.
func TestDeleteAllNotifications(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "v1", "v1", "v1", "v1@example.com")
	mustCreateUser(t, db, "v2", "v2", "v2", "v2@example.com")
	mustCreateUser(t, db, "actor", "actor", "actor", "actor@example.com")
	insertEvent(t, db, "e1", "password_reset", "success", "actor", "v1", identity.RoleModerator, 3000)
	insertEvent(t, db, "e2", "role_changed", "failure", "actor", "v1", identity.RoleModerator, 2000)
	insertUserNotification(t, db, "n-read", "v1", "actor", "blog-posts", "b1", "c1", 1500, nil)
	insertUserNotification(t, db, "n-unread", "v1", "actor", "blog-posts", "b1", "c2", 1000, nil)
	insertUserNotification(t, db, "n-other", "v2", "actor", "blog-posts", "b1", "c3", 500, nil)

	weight := identity.RoleWeight(identity.RoleModerator)
	// e2 was already read: the sweep dismisses it in place (the upsert),
	// never duplicating the read row.
	if err := MarkNotificationRead(ctx, db, "v1", "e2", weight, true, 4000); err != nil {
		t.Fatalf("mark e2 read: %v", err)
	}

	n, err := DeleteAllNotifications(ctx, db, "v1", weight, true, 5000)
	if err != nil {
		t.Fatalf("delete all: %v", err)
	}
	if n != 4 {
		t.Errorf("delete all reported %d, want 4 (two rows + two audit events)", n)
	}
	again, err := DeleteAllNotifications(ctx, db, "v1", weight, true, 6000)
	if err != nil {
		t.Fatalf("repeat delete all: %v", err)
	}
	if again != 0 {
		t.Errorf("repeat delete all reported %d, want 0", again)
	}

	for id, want := range map[string]int{"n-read": 0, "n-unread": 0, "n-other": 1} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE id = ?`, id).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", id, err)
		}
		if count != want {
			t.Errorf("row %s count = %d, want %d", id, count, want)
		}
	}
	for _, eventID := range []string{"e1", "e2"} {
		var audits int
		if err := db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE id = ?`, eventID).Scan(&audits); err != nil {
			t.Fatalf("count audit %s: %v", eventID, err)
		}
		if audits != 1 {
			t.Errorf("audit row %s count = %d, want the ledger untouched", eventID, audits)
		}
		// Exactly ONE read row per (viewer, event): read calls and the
		// dismissal share it.
		var rows, dismissed int
		if err := db.QueryRow(`SELECT COUNT(*), COUNT(dismissed_at_ms) FROM notification_reads
			WHERE user_id = 'v1' AND event_id = ?`, eventID).Scan(&rows, &dismissed); err != nil {
			t.Fatalf("count read rows %s: %v", eventID, err)
		}
		if rows != 1 || dismissed != 1 {
			t.Errorf("event %s read rows = %d, dismissed = %d, want 1/1", eventID, rows, dismissed)
		}
	}
	if ids := eventIDsByCreated(t, db, weight, 10); len(ids) != 0 {
		t.Errorf("v1's feed = %v, want empty", ids)
	}
	items, _, err := ListNotifications(ctx, db, "v2", weight, true, 10, nil)
	if err != nil {
		t.Fatalf("list for v2: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("v2's feed has %d items, want 3 (two events + its own row)", len(items))
	}
}

// TestDeleteAllNotifications_NonStaffSkipsAudit pins the floor split: a
// non-staff viewer's delete-all removes their own rows and never touches the
// audit space — no dismissal row is written.
func TestDeleteAllNotifications_NonStaffSkipsAudit(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "v1", "v1", "v1", "v1@example.com")
	mustCreateUser(t, db, "actor", "actor", "actor", "actor@example.com")
	insertEvent(t, db, "e1", "password_reset", "success", "actor", "v1", identity.RoleUser, 3000)
	insertUserNotification(t, db, "n1", "v1", "actor", "blog-posts", "b1", "c1", 1000, nil)

	n, err := DeleteAllNotifications(ctx, db, "v1", identity.RoleWeight(identity.RoleUser), false, 5000)
	if err != nil {
		t.Fatalf("delete all: %v", err)
	}
	if n != 1 {
		t.Errorf("delete all reported %d, want 1 (the viewer's own row only)", n)
	}
	var reads int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notification_reads WHERE user_id = 'v1'`).Scan(&reads); err != nil {
		t.Fatalf("count read rows: %v", err)
	}
	if reads != 0 {
		t.Errorf("non-staff delete-all wrote %d audit read/dismissal rows, want 0", reads)
	}
}

// TestDeleteReadNotifications pins clear-read's scope at the store: only the
// viewer's READ event rows leave; an unread row always survives (a bulk
// action can never discard something unread) and other recipients are
// untouched. Idempotent — the second pass removes nothing.
func TestDeleteReadNotifications(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "v1", "v1", "v1", "v1@example.com")
	mustCreateUser(t, db, "v2", "v2", "v2", "v2@example.com")
	mustCreateUser(t, db, "actor", "actor", "actor", "actor@example.com")
	readMS := int64(4000)
	insertUserNotification(t, db, "n-read", "v1", "actor", "blog-posts", "b1", "c1", 3000, &readMS)
	insertUserNotification(t, db, "n-unread", "v1", "actor", "blog-posts", "b1", "c2", 2000, nil)
	insertUserNotification(t, db, "n-other", "v2", "actor", "blog-posts", "b1", "c3", 1000, &readMS)

	n, err := DeleteReadNotifications(ctx, db, "v1", true)
	if err != nil {
		t.Fatalf("delete read: %v", err)
	}
	if n != 1 {
		t.Fatalf("deleted %d rows, want 1", n)
	}
	again, err := DeleteReadNotifications(ctx, db, "v1", true)
	if err != nil {
		t.Fatalf("repeat: %v", err)
	}
	if again != 0 {
		t.Errorf("repeat deleted %d rows, want 0", again)
	}

	for id, want := range map[string]int{"n-read": 0, "n-unread": 1, "n-other": 1} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE id = ?`, id).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", id, err)
		}
		if count != want {
			t.Errorf("row %s count = %d, want %d", id, count, want)
		}
	}
}
