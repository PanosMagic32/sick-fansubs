package store

import (
	"context"
	"database/sql"
	"testing"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/store/storetest"
)

// Staff dashboard metrics store tests:
// every number is a live aggregate over existing tables and the audit ledger,
// and the activity window boundary is inclusive.

// metricsUser seeds one account row through the shared fixture with explicit
// role/status/creation time.
func metricsUser(t *testing.T, db *sql.DB, id, role, status string, createdAtMS int64) {
	t.Helper()
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:          id,
		Username:    id,
		Role:        role,
		Status:      status,
		CreatedAtMS: createdAtMS,
	})
}

// metricsBlogPost inserts one blog post with the given status.
func metricsBlogPost(t *testing.T, db *sql.DB, id, status string) {
	t.Helper()
	var published any
	if status == "published" {
		published = 1_000
	}
	if _, err := db.Exec(`INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status, published_at_ms, created_at_ms, updated_at_ms)
		VALUES (?, 'T', '', '', 'https://example.com/t.jpg', ?, ?, 1, 1)`,
		id, status, published); err != nil {
		t.Fatalf("insert blog post %s: %v", id, err)
	}
}

// metricsProject inserts one project with the given status.
func metricsProject(t *testing.T, db *sql.DB, id, status string) {
	t.Helper()
	var published any
	if status == "published" {
		published = 1_000
	}
	if _, err := db.Exec(`INSERT INTO projects
		(id, title, description, slug, thumbnail_url, status, published_at_ms, created_at_ms, updated_at_ms)
		VALUES (?, 'T', '', ?, 'https://example.com/t.jpg', ?, ?, 1, 1)`,
		id, id, status, published); err != nil {
		t.Fatalf("insert project %s: %v", id, err)
	}
}

// metricsEvent inserts one audit row with an explicit event/result/actor/time.
// An empty actor becomes SQL NULL (the unknown-actor shape).
func metricsEvent(t *testing.T, db *sql.DB, id, event, result, actorID string, createdAtMS int64) {
	t.Helper()
	var actor any
	if actorID != "" {
		actor = actorID
	}
	if _, err := db.Exec(`INSERT INTO audit_events
		(id, event, result, actor_id, request_id, remote_addr, created_at_ms)
		VALUES (?, ?, ?, ?, '', '', ?)`,
		id, event, result, actor, createdAtMS); err != nil {
		t.Fatalf("insert audit event %s: %v", id, err)
	}
}

// TestGetMetricsTotals_EmptyDatabase pins the empty-state shape: a freshly
// migrated database answers all zeros, not a NULL-scan failure.
func TestGetMetricsTotals_EmptyDatabase(t *testing.T) {
	t.Parallel()

	got, err := GetMetricsTotals(context.Background(), openStoreDB(t))
	if err != nil {
		t.Fatalf("GetMetricsTotals: %v", err)
	}
	if got != (MetricsTotals{}) {
		t.Errorf("empty database totals = %+v, want the zero value", got)
	}
}

// TestGetMetricsTotals_Breakdowns pins every partition against one seeded
// fixture: users by role/status, each content type by status, and the two
// cross-type sums (comments, favorites).
func TestGetMetricsTotals_Breakdowns(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	metricsUser(t, db, "u1", "user", "active", 1)
	metricsUser(t, db, "u2", "user", "suspended", 1)
	metricsUser(t, db, "m1", "moderator", "active", 1)
	metricsUser(t, db, "a1", "admin", "active", 1)
	metricsUser(t, db, "s1", "super-admin", "active", 1)

	metricsBlogPost(t, db, "p1", "published")
	metricsBlogPost(t, db, "p2", "published")
	metricsBlogPost(t, db, "p3", "draft")
	metricsBlogPost(t, db, "p4", "archived")

	metricsProject(t, db, "pr1", "published")
	metricsProject(t, db, "pr2", "archived")

	if _, err := db.Exec(`INSERT INTO blog_post_comments
		(id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		VALUES ('c1', 'u1', 'p1', NULL, 'x', 0, 1, 1),
		       ('c2', 'u2', 'p2', NULL, 'y', 0, 1, 1)`); err != nil {
		t.Fatalf("insert blog comments: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO project_comments
		(id, user_id, project_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		VALUES ('c3', 'u1', 'pr1', NULL, 'z', 0, 1, 1)`); err != nil {
		t.Fatalf("insert project comment: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO blog_post_favorites (user_id, blog_post_id, created_at_ms)
		VALUES ('u1', 'p1', 1)`); err != nil {
		t.Fatalf("insert blog favorite: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO project_favorites (user_id, project_id, created_at_ms)
		VALUES ('u1', 'pr1', 1), ('u2', 'pr2', 1)`); err != nil {
		t.Fatalf("insert project favorites: %v", err)
	}

	got, err := GetMetricsTotals(ctx, db)
	if err != nil {
		t.Fatalf("GetMetricsTotals: %v", err)
	}

	want := MetricsTotals{
		Users: MetricsUserTotals{
			Total:           5,
			RoleUser:        2,
			RoleModerator:   1,
			RoleAdmin:       1,
			RoleSuperAdmin:  1,
			StatusActive:    4,
			StatusSuspended: 1,
		},
		BlogPosts: MetricsContentTotals{Total: 4, Published: 2, Draft: 1, Archived: 1},
		Projects:  MetricsContentTotals{Total: 2, Published: 1, Archived: 1},
		Comments:  3,
		Favorites: 3,
	}
	if got != want {
		t.Errorf("totals = %+v, want %+v", got, want)
	}
}

// TestGetMetricsTotals_LiveAfterWrite pins the "no stored counter" half of
// the contract: a write moves every affected total on the next read
// — nothing is materialized — and a delete moves it back.
func TestGetMetricsTotals_LiveAfterWrite(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	before, err := GetMetricsTotals(ctx, db)
	if err != nil {
		t.Fatalf("GetMetricsTotals before: %v", err)
	}

	metricsUser(t, db, "u1", "moderator", "active", 1)
	metricsBlogPost(t, db, "p1", "published")
	if _, err := db.Exec(`INSERT INTO blog_post_comments
		(id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		VALUES ('c1', 'u1', 'p1', NULL, 'x', 0, 1, 1)`); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO blog_post_favorites (user_id, blog_post_id, created_at_ms)
		VALUES ('u1', 'p1', 1)`); err != nil {
		t.Fatalf("insert favorite: %v", err)
	}

	after, err := GetMetricsTotals(ctx, db)
	if err != nil {
		t.Fatalf("GetMetricsTotals after the writes: %v", err)
	}
	if after.Users.Total != before.Users.Total+1 || after.Users.RoleModerator != before.Users.RoleModerator+1 {
		t.Errorf("users after write = %+v, want one more moderator than %+v", after.Users, before.Users)
	}
	if after.BlogPosts.Total != before.BlogPosts.Total+1 || after.BlogPosts.Published != before.BlogPosts.Published+1 {
		t.Errorf("blog posts after write = %+v, want one more published post than %+v", after.BlogPosts, before.BlogPosts)
	}
	if after.Comments != before.Comments+1 {
		t.Errorf("comments after write = %d, want %d", after.Comments, before.Comments+1)
	}
	if after.Favorites != before.Favorites+1 {
		t.Errorf("favorites after write = %d, want %d", after.Favorites, before.Favorites+1)
	}

	// Deleting the post cascades its comment and favorite away — the totals
	// follow the data back down (nothing is cached).
	if _, err := db.Exec(`DELETE FROM blog_posts WHERE id = 'p1'`); err != nil {
		t.Fatalf("delete post: %v", err)
	}
	final, err := GetMetricsTotals(ctx, db)
	if err != nil {
		t.Fatalf("GetMetricsTotals after the delete: %v", err)
	}
	if final.BlogPosts != before.BlogPosts || final.Comments != before.Comments || final.Favorites != before.Favorites {
		t.Errorf("totals after the delete = %+v, want the pre-write content %+v", final, before)
	}
}

// TestGetMetricsActivity_EmptyDatabase pins the zero shape.
func TestGetMetricsActivity_EmptyDatabase(t *testing.T) {
	t.Parallel()

	got, err := GetMetricsActivity(context.Background(), openStoreDB(t), 1_000)
	if err != nil {
		t.Fatalf("GetMetricsActivity: %v", err)
	}
	if got != (MetricsActivity{}) {
		t.Errorf("empty database activity = %+v, want the zero value", got)
	}
}

// TestGetMetricsActivity_WindowAndCounts pins the window boundary (inclusive
// opening instant), the DISTINCT active-user count, the registrations source
// (users.created_at_ms, not an audit event), and the success-only rule for
// the change events.
func TestGetMetricsActivity_WindowAndCounts(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	const since = int64(1_700_000_000_000)

	// Registrations come from users.created_at_ms: three inside the window
	// (a different count from every other metric below, so the assertion
	// cannot pass by counting the wrong source), one outside.
	metricsUser(t, db, "u1", "user", "active", since-1)
	metricsUser(t, db, "u2", "user", "active", since)
	metricsUser(t, db, "u3", "user", "active", since+1)
	metricsUser(t, db, "u4", "user", "suspended", since+2)

	// Active users: DISTINCT actors over in-window sign_in_success. u2 signs
	// in twice (counts once), u3 once; u1's sign-in is outside the window.
	metricsEvent(t, db, "e1", audit.EventSignInSuccess, audit.ResultSuccess, "u2", since)
	metricsEvent(t, db, "e2", audit.EventSignInSuccess, audit.ResultSuccess, "u2", since+5)
	metricsEvent(t, db, "e3", audit.EventSignInSuccess, audit.ResultSuccess, "u3", since+10)
	metricsEvent(t, db, "e4", audit.EventSignInSuccess, audit.ResultSuccess, "u1", since-1)

	// Sign-in failures: one inside, one outside.
	metricsEvent(t, db, "e5", audit.EventSignInFailure, audit.ResultFailure, "", since+1)
	metricsEvent(t, db, "e6", audit.EventSignInFailure, audit.ResultFailure, "", since-1)

	// Change events inside the window.
	metricsEvent(t, db, "e7", audit.EventContentCreated, audit.ResultSuccess, "u2", since+2)
	metricsEvent(t, db, "e8", audit.EventContentUpdated, audit.ResultSuccess, "u2", since+3)
	metricsEvent(t, db, "e9", audit.EventCommentDeleted, audit.ResultSuccess, "m1", since+4)
	metricsEvent(t, db, "e10", audit.EventUserSuspended, audit.ResultSuccess, "a1", since+5)
	metricsEvent(t, db, "e11", audit.EventUserReactivated, audit.ResultSuccess, "a1", since+6)
	metricsEvent(t, db, "e12", audit.EventUserDeleted, audit.ResultSuccess, "a1", since+7)
	metricsEvent(t, db, "e13", audit.EventRoleChanged, audit.ResultSuccess, "s1", since+8)

	// A content event just outside the window does not count.
	metricsEvent(t, db, "e14", audit.EventContentDeleted, audit.ResultSuccess, "u2", since-1)

	// FAILURE rows are not activity: a blocked role change and a failed write
	// keep their events at zero (only sign_in_failure counts its failures).
	metricsEvent(t, db, "e15", audit.EventRoleChanged, audit.ResultFailure, "a1", since+9)
	metricsEvent(t, db, "e16", audit.EventContentCreated, audit.ResultFailure, "a1", since+10)

	// An unlisted event name counts nowhere.
	metricsEvent(t, db, "e17", audit.EventSignOut, audit.ResultSuccess, "u2", since+11)

	got, err := GetMetricsActivity(ctx, db, since)
	if err != nil {
		t.Fatalf("GetMetricsActivity: %v", err)
	}

	want := MetricsActivity{
		Registrations:    3,
		ActiveUsers:      2,
		SignInFailures:   1,
		ContentCreated:   1,
		ContentUpdated:   1,
		ContentDeleted:   0,
		CommentDeletions: 1,
		UsersSuspended:   1,
		UsersReactivated: 1,
		UsersDeleted:     1,
		RoleChanges:      1,
	}
	if got != want {
		t.Errorf("activity = %+v, want %+v", got, want)
	}
}
