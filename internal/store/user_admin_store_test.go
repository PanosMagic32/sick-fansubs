package store

import (
	"database/sql"
	"errors"
	"testing"

	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/store/storetest"
)

// mustCreateStaffUser inserts a user row with explicit role, status, and
// creation time — the staff-list fixtures need control over all three.
func mustCreateStaffUser(t *testing.T, db *sql.DB, id, username, role, status string, createdAtMS int64) {
	t.Helper()
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:            id,
		Username:      username,
		UsernameCanon: username,
		Email:         username + "@example.com",
		Role:          role,
		Status:        status,
		CreatedAtMS:   createdAtMS,
	})
}

func TestSuspendUser_Success(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateStaffUser(t, db, "target", "PlainUser", "user", "active", 2000)
	// A live session that the suspension must revoke.
	digest := make([]byte, 32)
	digest[0] = 1
	if err := CreateSession(ctx, db, CreateSessionParams{
		ID: "sess-target", UserID: "target", TokenDigest: digest, CSRF: make([]byte, 32),
		AuthVersion: 1, CreatedAtMS: 2_000, ExpiresAtMS: 3_000,
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := SuspendUser(ctx, db, "target", 4_000); err != nil {
		t.Fatalf("SuspendUser: %v", err)
	}

	u, err := UserByID(ctx, db, "target")
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if u.Status != identity.StatusSuspended {
		t.Errorf("status: got %q, want suspended", u.Status)
	}
	if u.AuthVersion != 2 {
		t.Errorf("auth_version: got %d, want 2", u.AuthVersion)
	}
	if u.UpdatedAtMS != 4_000 {
		t.Errorf("updated_at_ms: got %d, want 4000", u.UpdatedAtMS)
	}
	if _, err := SessionByDigest(ctx, db, digest); err == nil {
		t.Error("the suspension must delete the target's session rows")
	}
}

func TestSuspendUser_NotFound(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)

	if err := SuspendUser(t.Context(), db, "nonexistent", 3_000); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestSuspendUser_LastActiveSuperAdminGuard(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateStaffUser(t, db, "lone-sa", "LoneSA", "super-admin", "active", 1000)

	// Suspending the only active super-admin — self or peer — is rejected.
	if err := SuspendUser(ctx, db, "lone-sa", 3_000); !errors.Is(err, ErrLastActiveSuperAdmin) {
		t.Fatalf("suspend lone super-admin: got %v, want ErrLastActiveSuperAdmin", err)
	}
}

func TestSuspendUser_SuspendedSuperAdminNoGuard(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	// A suspended super-admin is outside the ACTIVE count: the guard cannot
	// fire for one. The handler short-circuits the repeat anyway;
	// the store must still be safe when the write path races the read.
	mustCreateStaffUser(t, db, "sus-sa", "SuspendedSA", "super-admin", "suspended", 1000)

	if err := SuspendUser(ctx, db, "sus-sa", 3_000); err != nil {
		t.Fatalf("suspend already-suspended super-admin: %v", err)
	}
}

func TestReactivateUser_Success(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateStaffUser(t, db, "target", "SuspendedUser", "user", "suspended", 2000)

	if err := ReactivateUser(ctx, db, "target", 4_000); err != nil {
		t.Fatalf("ReactivateUser: %v", err)
	}

	u, err := UserByID(ctx, db, "target")
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if u.Status != identity.StatusActive {
		t.Errorf("status: got %q, want active", u.Status)
	}
	if u.AuthVersion != 2 {
		t.Errorf("auth_version: got %d, want 2 (reactivation requires a new sign-in)", u.AuthVersion)
	}
	if u.UpdatedAtMS != 4_000 {
		t.Errorf("updated_at_ms: got %d, want 4000", u.UpdatedAtMS)
	}
}

func TestReactivateUser_NotFound(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)

	if err := ReactivateUser(t.Context(), db, "nonexistent", 3_000); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// TestListStaffUsers_OrderingAndPagination pins the keyset
// contract: created_at_ms DESC, id ASC with the shared PageKey tuple, and
// hasNextPage from the limit+1 row with no separate count query.
func TestListStaffUsers_OrderingAndPagination(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	// Created out of order on purpose — the query, not the insert order,
	// must sort. u2/u3 share a timestamp to pin the id-ASC tie-break.
	mustCreateStaffUser(t, db, "u1", "Alpha", "user", "active", 3000)
	mustCreateStaffUser(t, db, "u3", "Gamma", "user", "active", 2000)
	mustCreateStaffUser(t, db, "u2", "Beta", "user", "active", 2000)
	mustCreateStaffUser(t, db, "u4", "Delta", "user", "active", 1000)

	page1, hasNext, err := ListStaffUsers(ctx, db, 2, nil, "", "")
	if err != nil {
		t.Fatalf("ListStaffUsers page 1: %v", err)
	}
	if !hasNext {
		t.Fatal("page 1: hasNext = false, want true (4 users, limit 2)")
	}
	if len(page1) != 2 {
		t.Fatalf("page 1: got %d items, want 2", len(page1))
	}
	if page1[0].ID != "u1" || page1[1].ID != "u2" {
		t.Errorf("page 1 = [%s %s], want [u1 u2] (created DESC, id ASC tie-break)",
			page1[0].ID, page1[1].ID)
	}

	after := &UserPageKey{CreatedAtMS: page1[1].CreatedAtMS, ID: page1[1].ID}
	page2, hasNext, err := ListStaffUsers(ctx, db, 2, after, "", "")
	if err != nil {
		t.Fatalf("ListStaffUsers page 2: %v", err)
	}
	if hasNext {
		t.Fatal("page 2: hasNext = true, want false (last page)")
	}
	if len(page2) != 2 {
		t.Fatalf("page 2: got %d items, want 2", len(page2))
	}
	if page2[0].ID != "u3" || page2[1].ID != "u4" {
		t.Errorf("page 2 = [%s %s], want [u3 u4]", page2[0].ID, page2[1].ID)
	}
}

// TestListStaffUsers_RoleFilter pins the optional role filter and that the
// cursor pages WITHIN the filtered set.
func TestListStaffUsers_RoleFilter(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateStaffUser(t, db, "u1", "Alpha", "user", "active", 3000)
	mustCreateStaffUser(t, db, "m1", "ModOne", "moderator", "active", 2000)
	mustCreateStaffUser(t, db, "u2", "Beta", "user", "active", 1000)

	mods, hasNext, err := ListStaffUsers(ctx, db, 10, nil, "moderator", "")
	if err != nil {
		t.Fatalf("ListStaffUsers moderator filter: %v", err)
	}
	if hasNext {
		t.Fatal("hasNext = true, want false (one moderator)")
	}
	if len(mods) != 1 || mods[0].ID != "m1" {
		t.Fatalf("moderator page = %+v, want just m1", mods)
	}

	// The cursor pages within the filtered set: after the moderator, no
	// user-role rows should leak into the continuation.
	after := &UserPageKey{CreatedAtMS: mods[0].CreatedAtMS, ID: mods[0].ID}
	rest, hasNext, err := ListStaffUsers(ctx, db, 10, after, "moderator", "")
	if err != nil {
		t.Fatalf("ListStaffUsers moderator continuation: %v", err)
	}
	if hasNext || len(rest) != 0 {
		t.Errorf("moderator continuation = %+v hasNext=%v, want empty/false", rest, hasNext)
	}
}

// TestListStaffUsers_StatusFilter pins the optional status filter.
func TestListStaffUsers_StatusFilter(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateStaffUser(t, db, "u1", "Alpha", "user", "active", 3000)
	mustCreateStaffUser(t, db, "u2", "Beta", "user", "suspended", 2000)

	active, hasNext, err := ListStaffUsers(ctx, db, 10, nil, "", "active")
	if err != nil {
		t.Fatalf("ListStaffUsers active filter: %v", err)
	}
	if hasNext || len(active) != 1 || active[0].ID != "u1" {
		t.Errorf("active page = %+v hasNext=%v, want just u1/false", active, hasNext)
	}

	suspended, _, err := ListStaffUsers(ctx, db, 10, nil, "", "suspended")
	if err != nil {
		t.Fatalf("ListStaffUsers suspended filter: %v", err)
	}
	if len(suspended) != 1 || suspended[0].ID != "u2" {
		t.Errorf("suspended page = %+v, want just u2", suspended)
	}
}

// TestListStaffUsers_CombinedFilters pins role+status together.
func TestListStaffUsers_CombinedFilters(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateStaffUser(t, db, "u1", "Alpha", "user", "active", 3000)
	mustCreateStaffUser(t, db, "u2", "Beta", "user", "suspended", 2000)
	mustCreateStaffUser(t, db, "m1", "ModOne", "moderator", "suspended", 1000)

	got, _, err := ListStaffUsers(ctx, db, 10, nil, "user", "suspended")
	if err != nil {
		t.Fatalf("ListStaffUsers combined: %v", err)
	}
	if len(got) != 1 || got[0].ID != "u2" {
		t.Errorf("combined page = %+v, want just u2", got)
	}
}

// TestCountStaffUsers_Filters proves the count narrows exactly like the list
// read under each role/status combination, including the combined pair.
func TestCountStaffUsers_Filters(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateStaffUser(t, db, "u1", "Alpha", "user", "active", 3000)
	mustCreateStaffUser(t, db, "u2", "Beta", "user", "suspended", 2000)
	mustCreateStaffUser(t, db, "m1", "ModOne", "moderator", "active", 1000)

	for _, tc := range []struct {
		name   string
		role   string
		status string
		want   int
	}{
		{"no filter", "", "", 3},
		{"role narrows", "user", "", 2},
		{"status narrows", "", "active", 2},
		{"role and status", "user", "suspended", 1},
		{"both match nothing", "moderator", "suspended", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := CountStaffUsers(ctx, db, tc.role, tc.status)
			if err != nil {
				t.Fatalf("CountStaffUsers: %v", err)
			}
			if n != tc.want {
				t.Errorf("count = %d, want %d", n, tc.want)
			}
			users, _, err := ListStaffUsers(ctx, db, 10, nil, tc.role, tc.status)
			if err != nil {
				t.Fatalf("ListStaffUsers: %v", err)
			}
			if len(users) != n {
				t.Errorf("list returned %d rows, count says %d — the mirror drifted", len(users), n)
			}
		})
	}
}

// TestListStaffUsers_Empty pins the empty-table outcome: nil items and no
// next page (the handler converts nil to the [] JSON shape).
func TestListStaffUsers_Empty(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	users, hasNext, err := ListStaffUsers(ctx, db, 10, nil, "", "")
	if err != nil {
		t.Fatalf("ListStaffUsers empty: %v", err)
	}
	if users != nil {
		t.Errorf("users = %+v, want nil for an empty table", users)
	}
	if hasNext {
		t.Error("hasNext = true, want false")
	}
}

// TestListStaffUsers_AvatarProjection pins the avatar projection end to end
// from storage (the field is the rc-mobile pass): the stored storage-form
// reference reaches the row VERBATIM — the store never builds URLs, the
// handler projects the absolute served form — and a NULL column scans as
// nil, so "no avatar" stays distinguishable from a stored value.
func TestListStaffUsers_AvatarProjection(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateStaffUser(t, db, "u1", "Alpha", "user", "active", 2000)
	mustCreateStaffUser(t, db, "u2", "Beta", "user", "active", 1000)
	const stored = "media/images/ab/abcdef0123456789abcdef0123456789.jpg"
	if _, err := db.Exec(`UPDATE users SET avatar_url = ? WHERE id = 'u1'`, stored); err != nil {
		t.Fatalf("set avatar: %v", err)
	}

	users, _, err := ListStaffUsers(ctx, db, 10, nil, "", "")
	if err != nil {
		t.Fatalf("ListStaffUsers: %v", err)
	}
	if len(users) != 2 || users[0].ID != "u1" || users[1].ID != "u2" {
		t.Fatalf("page = %+v, want [u1 u2] (created_at_ms DESC)", users)
	}
	if users[0].AvatarURL == nil || *users[0].AvatarURL != stored {
		t.Errorf("u1 avatarUrl = %v, want %q (the stored reference, unprojected)", users[0].AvatarURL, stored)
	}
	if users[1].AvatarURL != nil {
		t.Errorf("u2 avatarUrl = %q, want nil for a NULL column", *users[1].AvatarURL)
	}
}

func TestChangeUserRole_Success(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateStaffUser(t, db, "target", "PlainUser", "user", "active", 2000)
	// A live session that the role change must revoke.
	digest := make([]byte, 32)
	digest[0] = 1
	if err := CreateSession(ctx, db, CreateSessionParams{
		ID: "sess-target", UserID: "target", TokenDigest: digest, CSRF: make([]byte, 32),
		AuthVersion: 1, CreatedAtMS: 1000, ExpiresAtMS: 9999,
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := ChangeUserRole(ctx, db, "target", identity.RoleModerator, 3000); err != nil {
		t.Fatalf("ChangeUserRole: %v", err)
	}

	u, err := UserByID(ctx, db, "target")
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if u.Role != identity.RoleModerator {
		t.Errorf("role: got %q, want moderator", u.Role)
	}
	if u.AuthVersion != 2 {
		t.Errorf("auth_version: got %d, want 2 (the bump is the revocation)", u.AuthVersion)
	}
	if _, err := SessionByDigest(ctx, db, digest); !errors.Is(err, ErrNotFound) {
		t.Errorf("session must be deleted by the role change, got %v", err)
	}
}

func TestChangeUserRole_NotFound(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	if err := ChangeUserRole(ctx, db, "nonexistent", identity.RoleModerator, 3000); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestChangeUserRole_LastActiveSuperAdminGuard(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateStaffUser(t, db, "lone-sa", "LoneSA", "super-admin", "active", 1000)

	// Demoting the only active super-admin (self or peer) must be rejected.
	if err := ChangeUserRole(ctx, db, "lone-sa", identity.RoleAdmin, 3000); !errors.Is(err, ErrLastActiveSuperAdmin) {
		t.Fatalf("demote lone super-admin: got %v, want ErrLastActiveSuperAdmin", err)
	}
	u, err := UserByID(ctx, db, "lone-sa")
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if u.Role != identity.RoleSuperAdmin {
		t.Errorf("role changed despite the guard: got %q, want super-admin", u.Role)
	}

	// With a second active super-admin, the demotion is fine.
	mustCreateStaffUser(t, db, "sa2", "SecondSA", "super-admin", "active", 2000)
	if err := ChangeUserRole(ctx, db, "lone-sa", identity.RoleAdmin, 3000); err != nil {
		t.Fatalf("demote with two super-admins: %v", err)
	}
}

func TestChangeUserRole_SuspendedSuperAdminDemoteAllowed(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	// A suspended super-admin is outside the ACTIVE count:
	// demoting them cannot reduce it, so the guard stays silent.
	mustCreateStaffUser(t, db, "sus-sa", "SuspendedSA", "super-admin", "suspended", 1000)

	if err := ChangeUserRole(ctx, db, "sus-sa", identity.RoleUser, 3000); err != nil {
		t.Fatalf("demote suspended super-admin: %v", err)
	}
}

func TestDeleteUser_Success(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateStaffUser(t, db, "target", "Target", "user", "active", 2000)
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "p1", Title: "T", ThumbnailURL: "https://example.com/t.jpg", PublishedAtMS: 3000}, "published")
	if _, err := db.Exec(`UPDATE blog_posts SET creator_id = 'target' WHERE id = 'p1'`); err != nil {
		t.Fatalf("set creator: %v", err)
	}
	if err := AddFavorite(ctx, db, BlogContent, "target", "p1", 1000); err != nil {
		t.Fatalf("seed favorite: %v", err)
	}
	digest := make([]byte, 32)
	digest[0] = 1
	if err := CreateSession(ctx, db, CreateSessionParams{
		ID: "sess-target", UserID: "target", TokenDigest: digest, CSRF: make([]byte, 32),
		AuthVersion: 1, CreatedAtMS: 1000, ExpiresAtMS: 9999,
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// The project side: creator AND updater SET NULL,
	// project favorites cascade — the deletion contract covers both content
	// kinds, not just blog posts.
	mustCreateProject(t, db, ProjectSummary{ID: "proj1", Title: "P", ThumbnailURL: "https://example.com/p.jpg", PublishedAtMS: 4000}, "published")
	if _, err := db.Exec(`UPDATE projects SET creator_id = 'target', updater_id = 'target' WHERE id = 'proj1'`); err != nil {
		t.Fatalf("set project refs: %v", err)
	}
	if err := AddFavorite(ctx, db, ProjectContent, "target", "proj1", 1000); err != nil {
		t.Fatalf("seed project favorite: %v", err)
	}

	// Beyond favorites and sessions, every row the account owns must cascade:
	// comments (and the replies they parent — another user's reply dies with
	// the account's comment), comment hearts, follows, notification rows and
	// preferences, reset/verification tokens, and push subscriptions.
	ownedTables := []string{
		"blog_post_comments",
		"project_comments",
		"blog_post_comment_hearts",
		"project_comment_hearts",
		"content_follows",
		"notification_preferences",
		"email_verification_tokens",
		"password_reset_tokens",
		"push_subscriptions",
	}
	mustCreateUser(t, db, "other", "Other", "other", "other@example.com")
	mustCreateComment(t, db, BlogContent, "c1", "target", "p1", nil, "own comment", 1, 1000, 1000)
	replyParent := "c1"
	mustCreateComment(t, db, BlogContent, "c2", "other", "p1", &replyParent, "reply", 0, 1000, 1000)
	mustCreateComment(t, db, ProjectContent, "c3", "target", "proj1", nil, "project comment", 0, 1000, 1000)
	mustCreateHeart(t, db, BlogContent, "target", "c1", 1000)
	mustCreateHeart(t, db, ProjectContent, "other", "c3", 1000)
	// Survivor comments the account hearted: the delete cascades the heart
	// rows, so the materialized counters must move in the same transaction —
	// otherwise they drift permanently (the counter/source agreement below).
	mustCreateComment(t, db, BlogContent, "c4", "other", "p1", nil, "survivor", 1, 1000, 1000)
	mustCreateHeart(t, db, BlogContent, "target", "c4", 1000)
	mustCreateComment(t, db, ProjectContent, "c5", "other", "proj1", nil, "project survivor", 1, 1000, 1000)
	mustCreateHeart(t, db, ProjectContent, "target", "c5", 1000)
	if err := AddFollow(ctx, db, BlogContent, "target", "p1", 1000); err != nil {
		t.Fatalf("seed follow: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO user_notifications
		(id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms)
		VALUES ('n1', 'target', 'heart', 'other', 'blog-posts', 'p1', 'c1', 1000)`); err != nil {
		t.Fatalf("seed user notification: %v", err)
	}
	// Another recipient's row where the account is the ACTOR: it must survive
	// with the actor reference set NULL, not cascade away.
	if _, err := db.Exec(`INSERT INTO user_notifications
		(id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms)
		VALUES ('n2', 'other', 'heart', 'target', 'blog-posts', 'p1', 'c1', 1000)`); err != nil {
		t.Fatalf("seed actor-reference notification: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO notification_preferences (user_id, kind, push_enabled, updated_at_ms)
		VALUES ('target', 'heart', 1, 1000)`); err != nil {
		t.Fatalf("seed notification preference: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO push_subscriptions
		(id, user_id, endpoint, p256dh, auth, created_at_ms, updated_at_ms)
		VALUES ('sub1', 'target', 'https://push.example/e1', 'key', 'auth', 1000, 1000)`); err != nil {
		t.Fatalf("seed push subscription: %v", err)
	}
	tokenDigest := make([]byte, 32)
	tokenDigest[0] = 7
	for _, q := range []string{
		`INSERT INTO email_verification_tokens (token_digest, user_id, created_at_ms, expires_at_ms) VALUES (?, 'target', 1000, 9999)`,
		`INSERT INTO password_reset_tokens (token_digest, user_id, created_at_ms, expires_at_ms) VALUES (?, 'target', 1000, 9999)`,
	} {
		if _, err := db.Exec(q, tokenDigest); err != nil {
			t.Fatalf("seed token row: %v", err)
		}
	}

	// Every seeded table holds a row the account owns now — the scoped
	// cascade assertions below are not vacuous.
	for _, table := range ownedTables {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table + ` WHERE user_id = 'target'`).Scan(&n); err != nil {
			t.Fatalf("count %s before deletion: %v", table, err)
		}
		if n == 0 {
			t.Errorf("%s holds no seeded target row before deletion", table)
		}
	}
	var targetNotifications int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE recipient_id = 'target'`).Scan(&targetNotifications); err != nil {
		t.Fatalf("count target notifications before deletion: %v", err)
	}
	if targetNotifications == 0 {
		t.Error("user_notifications holds no seeded row for the target before deletion")
	}

	if err := DeleteUser(ctx, db, "target"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	// The row is gone.
	if _, err := UserByID(ctx, db, "target"); !errors.Is(err, ErrNotFound) {
		t.Errorf("user must be gone, got %v", err)
	}
	// Sessions cascade.
	if _, err := SessionByDigest(ctx, db, digest); !errors.Is(err, ErrNotFound) {
		t.Errorf("session must cascade, got %v", err)
	}
	// Favorites cascade.
	var favCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM blog_post_favorites WHERE user_id = 'target'`).Scan(&favCount); err != nil {
		t.Fatalf("count favorites: %v", err)
	}
	if favCount != 0 {
		t.Errorf("favorites must cascade, got %d rows", favCount)
	}
	// Content survives with the creator relationship broken (SET NULL).
	var creator sql.NullString
	if err := db.QueryRow(`SELECT creator_id FROM blog_posts WHERE id = 'p1'`).Scan(&creator); err != nil {
		t.Fatalf("read surviving post: %v", err)
	}
	if creator.Valid {
		t.Errorf("creator_id must be NULL after deletion, got %q", creator.String)
	}
	// Projects survive with BOTH relationships broken (SET NULL).
	var projCreator, projUpdater sql.NullString
	if err := db.QueryRow(`SELECT creator_id, updater_id FROM projects WHERE id = 'proj1'`).Scan(&projCreator, &projUpdater); err != nil {
		t.Fatalf("read surviving project: %v", err)
	}
	if projCreator.Valid || projUpdater.Valid {
		t.Errorf("project creator_id/updater_id must be NULL after deletion, got %q/%q", projCreator.String, projUpdater.String)
	}
	// Project favorites cascade.
	var projFavCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM project_favorites WHERE user_id = 'target'`).Scan(&projFavCount); err != nil {
		t.Fatalf("count project favorites: %v", err)
	}
	if projFavCount != 0 {
		t.Errorf("project favorites must cascade, got %d rows", projFavCount)
	}
	// Every other owned row cascades too — including the reply another user
	// posted under the account's comment. Scoped to the account's rows: the
	// survivor comments another user authored stay.
	for _, table := range ownedTables {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table + ` WHERE user_id = 'target'`).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s target rows after deletion: got %d, want 0", table, n)
		}
	}
	// The account's hearts on SURVIVING comments cascade as rows; the
	// materialized counter must move with them in the same transaction —
	// otherwise it drifts permanently (the counter/source agreement).
	for _, tc := range []struct {
		name      string
		kind      ContentKind
		commentID string
	}{
		{"blog survivor", BlogContent, "c4"},
		{"project survivor", ProjectContent, "c5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var counter, rows int64
			if err := db.QueryRow(`SELECT hearts_count FROM `+tc.kind.comments+` WHERE id = ?`, tc.commentID).Scan(&counter); err != nil {
				t.Fatalf("read %s hearts_count: %v", tc.commentID, err)
			}
			if err := db.QueryRow(`SELECT COUNT(*) FROM `+tc.kind.hearts+` WHERE comment_id = ?`, tc.commentID).Scan(&rows); err != nil {
				t.Fatalf("count %s hearts: %v", tc.commentID, err)
			}
			if counter != rows {
				t.Errorf("DeleteUser: %s hearts_count = %d, want %d (the heart-row count)", tc.commentID, counter, rows)
			}
		})
	}
	// The parent_id cascade still removes another user's reply under the
	// account's comment, while the survivor comments stay.
	for _, tc := range []struct {
		name  string
		query string
		id    string
		want  int
	}{
		{"reply under the account's comment cascades", `SELECT COUNT(*) FROM blog_post_comments WHERE id = ?`, "c2", 0},
		{"survivor comment stays", `SELECT COUNT(*) FROM blog_post_comments WHERE id = ?`, "c4", 1},
		{"project survivor stays", `SELECT COUNT(*) FROM project_comments WHERE id = ?`, "c5", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var n int
			if err := db.QueryRow(tc.query, tc.id).Scan(&n); err != nil {
				t.Fatalf("count comment %s: %v", tc.id, err)
			}
			if n != tc.want {
				t.Errorf("comment %s rows = %d, want %d", tc.id, n, tc.want)
			}
		})
	}
	var targetNotificationsAfter int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE recipient_id = 'target'`).Scan(&targetNotificationsAfter); err != nil {
		t.Fatalf("count target notifications after deletion: %v", err)
	}
	if targetNotificationsAfter != 0 {
		t.Errorf("target notification rows after deletion: got %d, want 0", targetNotificationsAfter)
	}
	// The account's actor reference on ANOTHER recipient's row is SET NULL —
	// the row belongs to that recipient and survives.
	var actor sql.NullString
	if err := db.QueryRow(`SELECT actor_id FROM user_notifications WHERE id = 'n2'`).Scan(&actor); err != nil {
		t.Fatalf("read the surviving actor reference: %v", err)
	}
	if actor.Valid {
		t.Errorf("actor_id on the other recipient's row must be NULL after deletion, got %q", actor.String)
	}
}

func TestDeleteUser_NotFound(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	if err := DeleteUser(ctx, db, "nonexistent"); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestDeleteUser_LastActiveSuperAdminGuard(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateStaffUser(t, db, "lone-sa", "LoneSA", "super-admin", "active", 1000)

	// Deleting the only active super-admin — self or peer — is rejected.
	if err := DeleteUser(ctx, db, "lone-sa"); !errors.Is(err, ErrLastActiveSuperAdmin) {
		t.Fatalf("delete lone super-admin: got %v, want ErrLastActiveSuperAdmin", err)
	}
	if _, err := UserByID(ctx, db, "lone-sa"); err != nil {
		t.Errorf("user must survive the guarded delete, got %v", err)
	}

	// With a second active super-admin, deletion is fine.
	mustCreateStaffUser(t, db, "sa2", "SecondSA", "super-admin", "active", 2000)
	if err := DeleteUser(ctx, db, "lone-sa"); err != nil {
		t.Fatalf("delete with two super-admins: %v", err)
	}
}

func TestDeleteUser_SuspendedSuperAdminDeletable(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	// A suspended super-admin is outside the ACTIVE count: deleting them
	// cannot reduce it, so the guard stays silent.
	mustCreateStaffUser(t, db, "sus-sa", "SuspendedSA", "super-admin", "suspended", 1000)

	if err := DeleteUser(ctx, db, "sus-sa"); err != nil {
		t.Fatalf("delete suspended super-admin: %v", err)
	}
}
