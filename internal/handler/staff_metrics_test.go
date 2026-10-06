package handler_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/middleware"
)

// Staff dashboard metrics handler tests: the
// role gates, the strict no-parameters contract, the wire shape, and the
// baseline response headers.

// metricsBody mirrors the wire contract in docs/api/openapi.yaml. byRole and
// byStatus are maps here so the test pins the exact JSON KEY NAMES (the
// handler owns the camelCase spelling), with explicit length checks so a
// missing key cannot pass as a zero.
type metricsBody struct {
	Totals struct {
		Users struct {
			Total    int64            `json:"total"`
			ByRole   map[string]int64 `json:"byRole"`
			ByStatus map[string]int64 `json:"byStatus"`
		} `json:"users"`
		BlogPosts struct {
			Total     int64 `json:"total"`
			Published int64 `json:"published"`
			Draft     int64 `json:"draft"`
			Archived  int64 `json:"archived"`
		} `json:"blogPosts"`
		Projects struct {
			Total     int64 `json:"total"`
			Published int64 `json:"published"`
			Draft     int64 `json:"draft"`
			Archived  int64 `json:"archived"`
		} `json:"projects"`
		Comments  int64 `json:"comments"`
		Favorites int64 `json:"favorites"`
	} `json:"totals"`
	Activity30d struct {
		Since            string `json:"since"`
		Registrations    int64  `json:"registrations"`
		ActiveUsers      int64  `json:"activeUsers"`
		SignInFailures   int64  `json:"signInFailures"`
		ContentCreated   int64  `json:"contentCreated"`
		ContentUpdated   int64  `json:"contentUpdated"`
		ContentDeleted   int64  `json:"contentDeleted"`
		CommentDeletions int64  `json:"commentDeletions"`
		UsersSuspended   int64  `json:"usersSuspended"`
		UsersReactivated int64  `json:"usersReactivated"`
		UsersDeleted     int64  `json:"usersDeleted"`
		RoleChanges      int64  `json:"roleChanges"`
	} `json:"activity30d"`
}

// setupStaffMetricsHandler creates a fresh migrated database and the metrics
// handler behind the session chain at GET /api/v1/staff/metrics.
func setupStaffMetricsHandler(t *testing.T) (*sql.DB, http.Handler) {
	t.Helper()

	dir := testDataDir(t)
	db, err := database.Open(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := database.Apply(db); err != nil {
		db.Close()
		t.Fatalf("apply migrations: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/staff/metrics", handler.StaffMetrics(db))

	var h http.Handler = mux
	h = middleware.Session(db, false)(h)
	h = middleware.TrustedOrigin("http://localhost:5173")(h)
	h = middleware.RequestID()(h)

	return db, h
}

// getStaffMetrics issues GET /api/v1/staff/metrics with the given query
// string and session cookie.
func getStaffMetrics(h http.Handler, cookie, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/staff/metrics"+query, nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// seedMetricsContent inserts one blog post and one project in each status,
// plus one comment and one favorite per content type, all owned by userID.
func seedMetricsContent(t *testing.T, db *sql.DB, userID string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status, published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('b1', 'A', '', '', 'https://example.com/a.jpg', 'published', 1, 1, 1),
		       ('b2', 'B', '', '', 'https://example.com/b.jpg', 'draft', NULL, 1, 1)`); err != nil {
		t.Fatalf("insert blog posts: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO projects
		(id, title, description, slug, thumbnail_url, status, published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('pr1', 'P', '', 'p', 'https://example.com/p.jpg', 'published', 1, 1, 1)`); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO blog_post_comments
		(id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		VALUES ('c1', ?, 'b1', NULL, 'x', 0, 1, 1)`, userID); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO blog_post_favorites (user_id, blog_post_id, created_at_ms)
		VALUES (?, 'b1', 1)`, userID); err != nil {
		t.Fatalf("insert favorite: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO project_favorites (user_id, project_id, created_at_ms)
		VALUES (?, 'pr1', 1)`, userID); err != nil {
		t.Fatalf("insert project favorite: %v", err)
	}
}

// TestStaffMetrics_Moderator pins the 200 contract: the payload sections and
// their live values, the JSON key spelling, the activity window's echoed
// instant, and the baseline response headers.
func TestStaffMetrics_Moderator(t *testing.T) {
	t.Parallel()

	db, h := setupStaffMetricsHandler(t)
	// The moderator is the viewer; the other accounts are the totals fixture.
	mustCreateStaffUser(t, db, "mod1", "Mod", "moderator", "active", 3_000)
	mustCreateStaffUser(t, db, "u1", "User One", "user", "active", 3_000)
	mustCreateStaffUser(t, db, "u2", "User Two", "user", "suspended", 3_000)
	seedMetricsContent(t, db, "u1")
	cookie := mustCreateStaffSession(t, db, "mod1")

	rec := getStaffMetrics(h, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID missing — RequestID middleware not applied")
	}

	var body metricsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v (body %q)", err, rec.Body.String())
	}

	if got := body.Totals.Users.Total; got != 3 {
		t.Errorf("users.total = %d, want 3", got)
	}
	if len(body.Totals.Users.ByRole) != 4 {
		t.Errorf("byRole keys = %d (%v), want all four roles", len(body.Totals.Users.ByRole), body.Totals.Users.ByRole)
	}
	if got := body.Totals.Users.ByRole["superAdmin"]; got != 0 {
		t.Errorf("byRole.superAdmin = %d, want 0 (the camelCase key)", got)
	}
	if got := body.Totals.Users.ByRole["user"]; got != 2 {
		t.Errorf("byRole.user = %d, want 2", got)
	}
	if got := body.Totals.Users.ByRole["moderator"]; got != 1 {
		t.Errorf("byRole.moderator = %d, want 1", got)
	}
	if len(body.Totals.Users.ByStatus) != 2 {
		t.Errorf("byStatus keys = %d (%v), want active + suspended", len(body.Totals.Users.ByStatus), body.Totals.Users.ByStatus)
	}
	if got := body.Totals.Users.ByStatus["active"]; got != 2 {
		t.Errorf("byStatus.active = %d, want 2", got)
	}
	if got := body.Totals.Users.ByStatus["suspended"]; got != 1 {
		t.Errorf("byStatus.suspended = %d, want 1", got)
	}

	if got := body.Totals.BlogPosts; got.Total != 2 || got.Published != 1 || got.Draft != 1 || got.Archived != 0 {
		t.Errorf("blogPosts = %+v, want total 2 / 1 published / 1 draft / 0 archived", got)
	}
	if got := body.Totals.Projects; got.Total != 1 || got.Published != 1 {
		t.Errorf("projects = %+v, want total 1 / 1 published", got)
	}
	if got := body.Totals.Comments; got != 1 {
		t.Errorf("comments = %d, want 1", got)
	}
	if got := body.Totals.Favorites; got != 2 {
		t.Errorf("favorites = %d, want 2 (one per content type)", got)
	}

	// The activity window's opening instant is the API time format and sits
	// one metricsActivityWindow before now (30 × 24h; a minute of slack).
	since, err := time.Parse(time.RFC3339, body.Activity30d.Since)
	if err != nil {
		t.Fatalf("parse activity30d.since %q: %v", body.Activity30d.Since, err)
	}
	want := time.Now().UTC().Add(-30 * 24 * time.Hour)
	if diff := since.Sub(want); diff < -time.Minute || diff > time.Minute {
		t.Errorf("activity30d.since = %s, want ~%s (diff %s)", since, want, diff)
	}

	// The fixture accounts were created long ago, so nothing registers in the
	// window; the section must still carry every counter key at zero rather
	// than omitting fields.
	if got := body.Activity30d; got.Registrations != 0 || got.ActiveUsers != 0 || got.SignInFailures != 0 ||
		got.ContentCreated != 0 || got.ContentUpdated != 0 || got.ContentDeleted != 0 ||
		got.CommentDeletions != 0 || got.UsersSuspended != 0 || got.UsersReactivated != 0 ||
		got.UsersDeleted != 0 || got.RoleChanges != 0 {
		t.Errorf("activity30d = %+v, want every counter at 0", got)
	}
}

// TestStaffMetrics_EmptyDatabase pins the all-zero content payload on a fresh
// database (no NULL-scan failure, sections present). The only account is the
// viewing moderator, so the users section reflects exactly that one row.
func TestStaffMetrics_EmptyDatabase(t *testing.T) {
	t.Parallel()

	db, h := setupStaffMetricsHandler(t)
	mustCreateStaffUser(t, db, "mod1", "Mod", "moderator", "active", 3_000)
	cookie := mustCreateStaffSession(t, db, "mod1")

	rec := getStaffMetrics(h, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var body metricsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Totals.Users.Total != 1 || body.Totals.Users.ByRole["moderator"] != 1 ||
		body.Totals.Users.ByStatus["active"] != 1 {
		t.Errorf("users = %+v, want exactly the viewing moderator", body.Totals.Users)
	}
	if body.Totals.BlogPosts.Total != 0 || body.Totals.Projects.Total != 0 ||
		body.Totals.Comments != 0 || body.Totals.Favorites != 0 {
		t.Errorf("content totals = %+v, want every counter at 0", body.Totals)
	}
}

// TestStaffMetrics_RoleFloorIsUnmasked pins the floor: every aggregate is
// visible to the whole moderator+ floor. The store readers take no viewer
// identity, and the bodies
// for the three admitted roles must be identical — a future "hide something
// from moderators" change would fail here.
func TestStaffMetrics_RoleFloorIsUnmasked(t *testing.T) {
	t.Parallel()

	db, h := setupStaffMetricsHandler(t)
	mustCreateStaffUser(t, db, "m1", "Mod", "moderator", "active", 3_000)
	mustCreateStaffUser(t, db, "a1", "Admin", "admin", "active", 3_000)
	mustCreateStaffUser(t, db, "s1", "Super", "super-admin", "active", 3_000)
	mustCreateStaffUser(t, db, "u1", "User One", "user", "suspended", 3_000)
	seedMetricsContent(t, db, "u1")

	bodies := make([]metricsBody, 0, 3)
	for _, id := range []string{"m1", "a1", "s1"} {
		rec := getStaffMetrics(h, mustCreateStaffSession(t, db, id), "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200 (body %q)", id, rec.Code, rec.Body.String())
		}
		var body metricsBody
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: decode body: %v", id, err)
		}
		// The window instant is per-request by design; clear it before the
		// cross-role comparison.
		body.Activity30d.Since = ""
		bodies = append(bodies, body)
	}

	for i := 1; i < len(bodies); i++ {
		if !reflect.DeepEqual(bodies[i], bodies[0]) {
			t.Errorf("role %d body = %+v, want the same aggregates as the moderator body %+v", i, bodies[i], bodies[0])
		}
	}
}

// TestStaffMetrics_Gates pins the floor: unauthenticated → 401, role=user →
// 403, and the store is never touched below the floor. The second half proves
// the same database SHAPE fails the store when a permitted viewer asks, so
// the 403 above cannot be passing by accident.
func TestStaffMetrics_Gates(t *testing.T) {
	t.Parallel()

	db, h := setupStaffMetricsHandler(t)
	mustCreateStaffUser(t, db, "mod1", "Mod", "moderator", "active", 3_000)
	mustCreateStaffUser(t, db, "u1", "User One", "user", "active", 3_000)
	modCookie := mustCreateStaffSession(t, db, "mod1")
	userCookie := mustCreateStaffSession(t, db, "u1")

	if rec := getStaffMetrics(h, "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: status = %d, want 401", rec.Code)
	} else if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
		t.Errorf("anonymous: Content-Type = %q, want a problem", ct)
	}
	if rec := getStaffMetrics(h, userCookie, ""); rec.Code != http.StatusForbidden {
		t.Errorf("role=user: status = %d, want 403", rec.Code)
	} else if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
		t.Errorf("role=user: Content-Type = %q, want a problem", ct)
	}

	// Break the aggregate the store needs, then re-run both gates: the
	// moderator must now fail with the 500, the role=user request must stay a
	// 403 — proof that the floor check precedes every store call.
	if _, err := db.Exec(`DROP TABLE blog_post_comments`); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	if rec := getStaffMetrics(h, userCookie, ""); rec.Code != http.StatusForbidden {
		t.Errorf("role=user after the table drop: status = %d, want 403 (the store must not run)", rec.Code)
	}
	if rec := getStaffMetrics(h, modCookie, ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("moderator after the table drop: status = %d, want 500 (the store does run)", rec.Code)
	}
}

// TestStaffMetrics_RejectsQueryParams pins the strict no-parameters contract:
// an unknown name, a duplicate, and a malformed query string all answer 400.
func TestStaffMetrics_RejectsQueryParams(t *testing.T) {
	t.Parallel()

	db, h := setupStaffMetricsHandler(t)
	mustCreateStaffUser(t, db, "mod1", "Mod", "moderator", "active", 3_000)
	cookie := mustCreateStaffSession(t, db, "mod1")

	for _, query := range []string{"?limit=5", "?limit=5&limit=6", "?%zz=1"} {
		rec := getStaffMetrics(h, cookie, query)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("GET %q: status = %d, want 400", query, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
			t.Errorf("GET %q: Content-Type = %q, want a problem", query, ct)
		}
	}
}
