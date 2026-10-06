package handler_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/store/storetest"
)

// Project content-administration handler tests: role gates, the write
// contract with generated slugs, ETag/If-Match,
// masked draft visibility, and the slug-editing rules. The handlers read
// the session from the context, so tests attach a synthetic SessionUser
// directly — the cookie/CSRF/origin middleware is covered by the routes
// and middleware suites (the favorites pattern).

// setupProjectAdminHandlers builds a migrated database with the staff
// users, a valid thumbnail file, and a mux with the five project
// content-administration handlers at their production paths. Returns the
// db and the data dir.
func setupProjectAdminHandlers(t *testing.T, logger *slog.Logger) (*sql.DB, string, http.Handler) {
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

	for _, u := range []storetest.UserSpec{
		{ID: "admin1", Username: "Admin", Role: "admin"},
		{ID: "sa1", Username: "SA", Role: "super-admin"},
		{ID: "mod1", Username: "Mod", Role: "moderator"},
		{ID: "user1", Username: "User"},
	} {
		storetest.InsertUser(t, db, u)
	}

	// A real processed file so thumbnail references validate.
	mediaDir := filepath.Join(dir, "media", "images", "ab")
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		t.Fatalf("mkdir media fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mediaDir, "abcdef0123456789abcdef0123456789.jpg"), []byte("jpeg-bytes"), 0o644); err != nil {
		t.Fatalf("write media fixture: %v", err)
	}

	mux := http.NewServeMux()
	const publicBase = "https://fans.example"
	// The public reads share the mux with the admin handlers (production
	// registration) — the post-delete 404 check below depends on them.
	mux.HandleFunc("GET /api/v1/projects", handler.ProjectList(db, publicBase))
	mux.HandleFunc("GET /api/v1/projects/{id}", handler.ProjectDetail(db, publicBase))
	mux.HandleFunc("POST /api/v1/projects", handler.ProjectCreate(db, dir, publicBase, nil, nil))
	mux.HandleFunc("GET /api/v1/admin/projects", handler.ProjectStaffList(db, publicBase))
	mux.HandleFunc("GET /api/v1/admin/projects/{id}", handler.ProjectStaffDetail(db, publicBase))
	mux.HandleFunc("PUT /api/v1/projects/{id}", handler.ProjectUpdate(db, dir, publicBase, nil, nil))
	mux.HandleFunc("DELETE /api/v1/projects/{id}", handler.ProjectDelete(db))

	return db, dir, logContext(logger, mux)
}

// projWriteBody builds a valid create body: the downloads array is required —
// one batch row.
func projWriteBody(title, status string) string {
	return `{"title":"` + title + `","description":"Desc","thumbnailPath":"` + adminThumb + `","status":"` + status + `","downloads":[{"name":"Batch","magnetUrl":"magnet:?xt=urn:btih:abc123"}]}`
}

// projUpdateBody adds the optional slug (admin+).
func projUpdateBody(title, slug, status string) string {
	return `{"title":"` + title + `","description":"Desc","slug":"` + slug + `","thumbnailPath":"` + adminThumb + `","status":"` + status + `","downloads":[{"name":"Batch","magnetUrl":"magnet:?xt=urn:btih:abc123"}]}`
}

// seedProject inserts one project row directly (the create handler is the
// system under test in most cases; seeds give the list/gate tests control
// over creators and statuses).
func seedProject(t *testing.T, db *sql.DB, id, slug, status, creatorID string, publishedMS int64) {
	t.Helper()
	var pub any
	if publishedMS > 0 {
		pub = publishedMS
	}
	if _, err := db.Exec(
		`INSERT INTO projects (id, title, description, slug, thumbnail_url, status,
			creator_id, updater_id, published_at_ms, created_at_ms, updated_at_ms, revision)
		 VALUES (?, 'Title', 'Desc', ?, 'media/images/ab/abcdef0123456789abcdef0123456789.jpg', ?,
			?, ?, ?, 5000, 5000, 1)`,
		id, slug, status, creatorID, creatorID, pub); err != nil {
		t.Fatalf("seed project %s: %v", id, err)
	}
}

// TestProjectCreate_StoreFailureAnswers500AndAuditsOnce pins the create
// failure arm: a store failure answers the masked 500 with exactly ONE
// content_created failure row.
func TestProjectCreate_StoreFailureAnswers500AndAuditsOnce(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, _, mux := setupProjectAdminHandlers(t, logger)
	db.Close() // the create's store call now fails

	rec := serveAdmin(t, mux, adminRequest(http.MethodPost, "/api/v1/projects", projWriteBody("New", "draft"), "admin", "admin1"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
	if got := strings.Count(buf.String(), `"event":"content_created"`); got != 1 {
		t.Errorf("content_created rows = %d, want exactly one failure row (%s)", got, buf.String())
	}
	if !strings.Contains(buf.String(), `"result":"failure"`) {
		t.Errorf("audit log = %s, want the failure result", buf.String())
	}
}

func TestProjectCreate_RoleMatrix(t *testing.T) {
	t.Parallel()
	_, _, mux := setupProjectAdminHandlers(t, nil)

	cases := []struct {
		name   string
		role   string
		userID string
		want   int
	}{
		{"admin creates", "admin", "admin1", http.StatusCreated},
		{"super-admin creates", "super-admin", "sa1", http.StatusCreated},
		{"moderator cannot create", "moderator", "mod1", http.StatusForbidden},
		{"user cannot create", "user", "user1", http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := adminRequest(http.MethodPost, "/api/v1/projects", projWriteBody("One Piece", "published"), c.role, c.userID)
			rec := serveAdmin(t, mux, req)
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, c.want, rec.Body.String())
			}
		})
	}
}

func TestProjectCreate_GeneratesSlugAndLocation(t *testing.T) {
	t.Parallel()
	_, _, mux := setupProjectAdminHandlers(t, nil)

	cases := []struct {
		name     string
		title    string
		wantSlug string
	}{
		{"greek transliteration", "Όνομα Ταξίδι", "onoma-taxidi"},
		{"latin lowercase", "One Piece", "one-piece"},
		{"punctuation collapses", "Naruto — Επεισόδιο 12", "naruto-epeisodio-12"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := adminRequest(http.MethodPost, "/api/v1/projects", projWriteBody(c.title, "published"), "admin", "admin1")
			rec := serveAdmin(t, mux, req)
			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201 (body %q)", rec.Code, rec.Body.String())
			}

			var body struct {
				ID            string  `json:"id"`
				Slug          string  `json:"slug"`
				ThumbnailPath string  `json:"thumbnailPath"`
				ThumbnailURL  string  `json:"thumbnailUrl"`
				Status        string  `json:"status"`
				Revision      int64   `json:"revision"`
				PublishedAt   *string `json:"publishedAt"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Slug != c.wantSlug {
				t.Errorf("slug = %q, want %q", body.Slug, c.wantSlug)
			}
			if body.Status != "published" || body.Revision != 1 {
				t.Errorf("status/revision = %s/%d, want published/1", body.Status, body.Revision)
			}
			if body.PublishedAt == nil {
				t.Error("publishedAt = nil, want the first-published stamp")
			}
			if body.ThumbnailPath != adminThumb {
				t.Errorf("thumbnailPath = %q, want the storage-relative form", body.ThumbnailPath)
			}
			if body.ThumbnailURL == "" {
				t.Error("thumbnailUrl empty — the staff DTO carries both forms")
			}
			// The created resource's canonical address is the STAFF detail.
			if loc := rec.Header().Get("Location"); loc != "/api/v1/admin/projects/"+body.ID {
				t.Errorf("Location = %q, want /api/v1/admin/projects/%s", loc, body.ID)
			}
		})
	}
}

func TestProjectCreate_SlugCollisionSuffixes(t *testing.T) {
	t.Parallel()
	_, _, mux := setupProjectAdminHandlers(t, nil)

	first := adminRequest(http.MethodPost, "/api/v1/projects", projWriteBody("One Piece", "published"), "admin", "admin1")
	if rec := serveAdmin(t, mux, first); rec.Code != http.StatusCreated {
		t.Fatalf("first create status = %d (body %q)", rec.Code, rec.Body.String())
	}

	second := adminRequest(http.MethodPost, "/api/v1/projects", projWriteBody("One Piece", "published"), "admin", "admin1")
	rec := serveAdmin(t, mux, second)
	if rec.Code != http.StatusCreated {
		t.Fatalf("second create status = %d (body %q)", rec.Code, rec.Body.String())
	}
	var body struct {
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Slug != "one-piece-2" {
		t.Errorf("slug = %q, want the collision-suffixed one-piece-2", body.Slug)
	}
}

func TestProjectCreate_RejectsUnknownSlugField(t *testing.T) {
	t.Parallel()
	_, _, mux := setupProjectAdminHandlers(t, nil)

	// The create DTO has NO slug field — a hand-edited body carrying one is
	// an unknown field under the strict reader: 400, not silently ignored.
	req := adminRequest(http.MethodPost, "/api/v1/projects",
		projUpdateBody("One Piece", "mine", "published"), "admin", "admin1")
	rec := serveAdmin(t, mux, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unknown slug field (body %q)", rec.Code, rec.Body.String())
	}
}

func TestProjectCreate_Validation(t *testing.T) {
	t.Parallel()
	_, _, mux := setupProjectAdminHandlers(t, nil)

	cases := []struct {
		name      string
		body      string
		want      int
		wantField string
		wantCode  string
	}{
		{"missing title", `{"description":"D","thumbnailPath":"` + adminThumb + `"}`, http.StatusUnprocessableEntity, "title", "required"},
		{"bad status", projWriteBody("T", "gone"), http.StatusUnprocessableEntity, "status", "invalidValue"},
		{"missing thumbnail", `{"title":"T"}`, http.StatusUnprocessableEntity, "thumbnailPath", "required"},
		// The handler's file-existence check (validThumbnailReference) runs
		// AFTER validateProjectWrite, which validates the downloads array —
		// so this body carries a valid download row to reach the thumbnail
		// shape violation.
		{"invalid thumbnail reference", `{"title":"T","thumbnailPath":"media/images/zz/nope.jpg","downloads":[{"name":"Batch","magnetUrl":"magnet:?xt=x"}]}`, http.StatusUnprocessableEntity, "thumbnailPath", "invalidFormat"},
		// The canonical file EXISTS at media/images/ab/<id>.jpg; the mismatch
		// is the subdirectory — the grammar rejects it before the file check.
		{"mismatched thumbnail subdirectory", `{"title":"T","thumbnailPath":"media/images/cd/abcdef0123456789abcdef0123456789.jpg","downloads":[{"name":"Batch","magnetUrl":"magnet:?xt=x"}]}`, http.StatusUnprocessableEntity, "thumbnailPath", "invalidFormat"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := adminRequest(http.MethodPost, "/api/v1/projects", c.body, "admin", "admin1")
			rec := serveAdmin(t, mux, req)
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, c.want, rec.Body.String())
			}
			var problem struct {
				Violations []struct {
					Field string `json:"field"`
					Code  string `json:"code"`
				} `json:"violations"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			found := false
			for _, v := range problem.Violations {
				if v.Field == c.wantField {
					found = true
					if v.Code != c.wantCode {
						t.Errorf("violation %s code = %q, want %q", c.wantField, v.Code, c.wantCode)
					}
				}
			}
			if !found {
				t.Errorf("violations = %+v, want a %s violation", problem.Violations, c.wantField)
			}
		})
	}
}

func TestProjectStaffList_GateAndCursor(t *testing.T) {
	t.Parallel()
	db, _, mux := setupProjectAdminHandlers(t, nil)

	seedProject(t, db, "pub1", "pub1", "published", "admin1", 1000)
	seedProject(t, db, "draft1", "draft1", "draft", "admin1", 0)
	seedProject(t, db, "draftmod", "draftmod", "draft", "mod1", 0)

	// The moderator sees published rows only — the admin's draft is
	// draft-invisible to a strictly-lower role; the moderator's own draft IS
	// visible to its creator.
	req := adminRequest(http.MethodGet, "/api/v1/admin/projects", "", "moderator", "mod1")
	rec := serveAdmin(t, mux, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("moderator sees %d rows, want 2 (published + own draft)", len(list.Items))
	}
	ids := map[string]bool{}
	for _, it := range list.Items {
		ids[it.ID] = true
	}
	if !ids["pub1"] || !ids["draftmod"] || ids["draft1"] {
		t.Errorf("items = %+v, want [pub1 draftmod] (the admin's draft masked)", ids)
	}

	// The super-admin sees all three.
	req = adminRequest(http.MethodGet, "/api/v1/admin/projects?limit=10", "", "super-admin", "sa1")
	rec = serveAdmin(t, mux, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(list.Items) != 3 {
		t.Errorf("super-admin sees %d rows, want 3", len(list.Items))
	}

	// The cursor carries the apv1. namespace.
	req = adminRequest(http.MethodGet, "/api/v1/admin/projects?limit=1&after=nope", "", "super-admin", "sa1")
	rec = serveAdmin(t, mux, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("foreign cursor status = %d, want 400", rec.Code)
	}

	// Page with hasNext exposes the endCursor under the right prefix.
	req = adminRequest(http.MethodGet, "/api/v1/admin/projects?limit=1", "", "super-admin", "sa1")
	rec = serveAdmin(t, mux, req)
	var page struct {
		PageInfo struct {
			HasNextPage bool    `json:"hasNextPage"`
			EndCursor   *string `json:"endCursor"`
		} `json:"pageInfo"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !page.PageInfo.HasNextPage || page.PageInfo.EndCursor == nil ||
		!strings.HasPrefix(*page.PageInfo.EndCursor, "apv1.") {
		t.Errorf("pageInfo = %+v, want hasNext with an apv1. endCursor", page.PageInfo)
	}

	// Below moderator: the staff read floor holds.
	req = adminRequest(http.MethodGet, "/api/v1/admin/projects", "", "user", "user1")
	if rec := serveAdmin(t, mux, req); rec.Code != http.StatusForbidden {
		t.Errorf("user staff list status = %d, want 403", rec.Code)
	}
}

// TestProjectStaffList_Filters pins the two optional staff-list filters at the
// HTTP boundary: the values reach the store (a filtered
// page is genuinely narrowed, even for a viewer entitled to everything), and
// invalid values are refused before any query runs.
func TestProjectStaffList_Filters(t *testing.T) {
	t.Parallel()
	db, _, mux := setupProjectAdminHandlers(t, nil)

	seedProject(t, db, "pub1", "pub1", "published", "admin1", 1000)
	seedProject(t, db, "draft1", "draft1", "draft", "admin1", 0)
	seedProject(t, db, "arch1", "arch1", "archived", "admin1", 0)

	// Distinct titles: the filter matches the TITLE, so identical seed titles
	// would make the assertions below unable to fail.
	for id, title := range map[string]string{
		"pub1":   "One Piece",
		"draft1": "Draft Piece",
		"arch1":  "Άμλετ",
	} {
		if _, err := db.Exec(`UPDATE projects SET title = ? WHERE id = ?`, title, id); err != nil {
			t.Fatalf("retitle %s: %v", id, err)
		}
	}

	list := func(t *testing.T, target string, role, id string) []string {
		t.Helper()
		rec := serveAdmin(t, mux, adminRequest(http.MethodGet, target, "", role, id))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200 (body %q)", target, rec.Code, rec.Body.String())
		}
		var body struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		ids := make([]string, 0, len(body.Items))
		for _, it := range body.Items {
			ids = append(ids, it.ID)
		}
		return ids
	}

	// The super-admin is entitled to every row, so a narrowing here can only
	// come from the filter.
	if got := list(t, "/api/v1/admin/projects?status=draft", "super-admin", "sa1"); len(got) != 1 || got[0] != "draft1" {
		t.Errorf("status=draft = %v, want [draft1]", got)
	}
	if got := list(t, "/api/v1/admin/projects?q=PIECE", "super-admin", "sa1"); len(got) != 2 {
		t.Errorf("q=PIECE = %v, want the two Piece rows (case folds)", got)
	}
	if got := list(t, "/api/v1/admin/projects?q=piece&status=published", "super-admin", "sa1"); len(got) != 1 || got[0] != "pub1" {
		t.Errorf("q+status = %v, want [pub1]", got)
	}
	if got := list(t, "/api/v1/admin/projects?q=αμλετ", "super-admin", "sa1"); len(got) != 1 || got[0] != "arch1" {
		t.Errorf("q=αμλετ = %v, want [arch1] (an accent-free lowercase needle matches the accent-folded title)", got)
	}
	if got := list(t, "/api/v1/admin/projects?status=draft&q=nothing-matches", "super-admin", "sa1"); len(got) != 0 {
		t.Errorf("no-match filter = %v, want []", got)
	}
	// An empty filter value means "no filter" (the staff user list's
	// precedent), not a 422.
	if got := list(t, "/api/v1/admin/projects?status=&q=", "super-admin", "sa1"); len(got) != 3 {
		t.Errorf("blank filters = %v, want all 3 rows", got)
	}

	// Invalid values are refused before any store call: an unknown status is
	// semantically invalid, an over-long q breaks the bound.
	for _, tc := range []struct {
		target, field string
	}{
		{"/api/v1/admin/projects?status=gone", "status"},
		{"/api/v1/admin/projects?q=" + strings.Repeat("a", 101), "q"},
	} {
		rec := serveAdmin(t, mux, adminRequest(http.MethodGet, tc.target, "", "super-admin", "sa1"))
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: status = %d, want 422 (body %q)", tc.target, rec.Code, rec.Body.String())
		}
		var problem struct {
			Violations []struct {
				Field string `json:"field"`
				Code  string `json:"code"`
			} `json:"violations"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
			t.Fatalf("decode problem: %v", err)
		}
		wantCode := "invalidValue"
		if tc.field == "q" {
			wantCode = "maxLength"
		}
		found := false
		for _, v := range problem.Violations {
			if v.Field == tc.field && v.Code == wantCode {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: violations = %+v, want %s/%s", tc.target, problem.Violations, tc.field, wantCode)
		}
	}
}

func TestProjectStaffDetail_ETagAndGate(t *testing.T) {
	t.Parallel()
	db, _, mux := setupProjectAdminHandlers(t, nil)

	seedProject(t, db, "pub1", "pub1", "published", "admin1", 1000)
	seedProject(t, db, "draft1", "draft1", "draft", "admin1", 0)

	// Published: visible to a moderator with the revision ETag.
	req := adminRequest(http.MethodGet, "/api/v1/admin/projects/pub1", "", "moderator", "mod1")
	rec := serveAdmin(t, mux, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if etag := rec.Header().Get("ETag"); etag != `"1"` {
		t.Errorf("ETag = %q, want the revision-backed %q", etag, `"1"`)
	}
	var body struct {
		Slug          string `json:"slug"`
		ThumbnailPath string `json:"thumbnailPath"`
		Revision      int64  `json:"revision"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Slug != "pub1" || body.Revision != 1 {
		t.Errorf("body = slug %s revision %d, want pub1/1", body.Slug, body.Revision)
	}

	// The admin's draft is draft-invisible to a moderator: masked 404.
	req = adminRequest(http.MethodGet, "/api/v1/admin/projects/draft1", "", "moderator", "mod1")
	if rec := serveAdmin(t, mux, req); rec.Code != http.StatusNotFound {
		t.Errorf("moderator draft detail status = %d, want masked 404", rec.Code)
	}

	// The creator sees their own draft.
	req = adminRequest(http.MethodGet, "/api/v1/admin/projects/draft1", "", "admin", "admin1")
	if rec := serveAdmin(t, mux, req); rec.Code != http.StatusOK {
		t.Errorf("creator draft detail status = %d, want 200", rec.Code)
	}
}

func TestProjectUpdate_IfMatchAndRoles(t *testing.T) {
	t.Parallel()
	db, _, mux := setupProjectAdminHandlers(t, nil)

	seedProject(t, db, "pub1", "pub1", "published", "admin1", 1000)

	// Missing If-Match → 428.
	req := adminRequest(http.MethodPut, "/api/v1/projects/pub1", projWriteBody("Updated", "published"), "moderator", "mod1")
	if rec := serveAdmin(t, mux, req); rec.Code != http.StatusPreconditionRequired {
		t.Fatalf("missing If-Match status = %d, want 428 (body %q)", rec.Code, rec.Body.String())
	}

	// Stale If-Match → 412.
	req = adminRequest(http.MethodPut, "/api/v1/projects/pub1", projWriteBody("Updated", "published"), "moderator", "mod1")
	req.Header.Set("If-Match", `"99"`)
	if rec := serveAdmin(t, mux, req); rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale If-Match status = %d, want 412 (body %q)", rec.Code, rec.Body.String())
	}

	// A moderator (edit/delete capability) updates a published row → 200
	// with a fresh ETag and the revision bumped.
	req = adminRequest(http.MethodPut, "/api/v1/projects/pub1", projWriteBody("Updated", "published"), "moderator", "mod1")
	req.Header.Set("If-Match", `"1"`)
	rec := serveAdmin(t, mux, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("moderator update status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if etag := rec.Header().Get("ETag"); etag != `"2"` {
		t.Errorf("ETag = %q, want %q after the update", etag, `"2"`)
	}

	// Below moderator → 403.
	req = adminRequest(http.MethodPut, "/api/v1/projects/pub1", projWriteBody("Updated", "published"), "user", "user1")
	req.Header.Set("If-Match", `"2"`)
	if rec := serveAdmin(t, mux, req); rec.Code != http.StatusForbidden {
		t.Errorf("user update status = %d, want 403", rec.Code)
	}

	// A moderator cannot even ADDRESS an admin's draft: masked 404.
	seedProject(t, db, "draft1", "draft1", "draft", "admin1", 0)
	req = adminRequest(http.MethodPut, "/api/v1/projects/draft1", projWriteBody("Updated", "draft"), "moderator", "mod1")
	req.Header.Set("If-Match", `"1"`)
	if rec := serveAdmin(t, mux, req); rec.Code != http.StatusNotFound {
		t.Errorf("moderator draft update status = %d, want masked 404", rec.Code)
	}
}

func TestProjectUpdate_SlugRules(t *testing.T) {
	t.Parallel()
	db, _, mux := setupProjectAdminHandlers(t, nil)

	seedProject(t, db, "p1", "one", "published", "admin1", 1000)
	seedProject(t, db, "p2", "two", "published", "admin1", 1000)

	// A moderator submitting ANY slug is a 403 — the capability is admin+.
	req := adminRequest(http.MethodPut, "/api/v1/projects/p1", projUpdateBody("T", "one", "published"), "moderator", "mod1")
	req.Header.Set("If-Match", `"1"`)
	if rec := serveAdmin(t, mux, req); rec.Code != http.StatusForbidden {
		t.Fatalf("moderator slug status = %d, want 403 (body %q)", rec.Code, rec.Body.String())
	}

	// Admin + invalid grammar → 422 {slug, invalidFormat}.
	for _, bad := range []string{"Uppercase", "has space", "-leading", "trailing-", "double--dash", ""} {
		req = adminRequest(http.MethodPut, "/api/v1/projects/p1", projUpdateBody("T", bad, "published"), "admin", "admin1")
		req.Header.Set("If-Match", `"1"`)
		rec := serveAdmin(t, mux, req)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("slug %q status = %d, want 422 (body %q)", bad, rec.Code, rec.Body.String())
		}
		var problem struct {
			Violations []struct {
				Field string `json:"field"`
				Code  string `json:"code"`
			} `json:"violations"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
			t.Fatalf("decode problem: %v", err)
		}
		if len(problem.Violations) != 1 || problem.Violations[0].Field != "slug" || problem.Violations[0].Code != "invalidFormat" {
			t.Errorf("slug %q violations = %+v, want slug:invalidFormat", bad, problem.Violations)
		}
	}

	// Admin + taken slug → 422 {slug, alreadyTaken} (the store's UNIQUE
	// backstop, not a pre-check).
	req = adminRequest(http.MethodPut, "/api/v1/projects/p1", projUpdateBody("T", "two", "published"), "admin", "admin1")
	req.Header.Set("If-Match", `"1"`)
	rec := serveAdmin(t, mux, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("taken slug status = %d, want 422 (body %q)", rec.Code, rec.Body.String())
	}
	var problem struct {
		Violations []struct {
			Field string `json:"field"`
			Code  string `json:"code"`
		} `json:"violations"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if len(problem.Violations) != 1 || problem.Violations[0].Field != "slug" || problem.Violations[0].Code != "alreadyTaken" {
		t.Errorf("violations = %+v, want slug:alreadyTaken", problem.Violations)
	}

	// Admin + valid new slug → 200 and the slug lands.
	req = adminRequest(http.MethodPut, "/api/v1/projects/p1", projUpdateBody("T", "renamed", "published"), "admin", "admin1")
	req.Header.Set("If-Match", `"1"`)
	rec = serveAdmin(t, mux, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var body struct {
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Slug != "renamed" {
		t.Errorf("slug = %q, want renamed", body.Slug)
	}

	// Absent slug keeps the existing one (moderator-safe edit).
	req = adminRequest(http.MethodPut, "/api/v1/projects/p1", projWriteBody("Again", "published"), "moderator", "mod1")
	req.Header.Set("If-Match", `"2"`)
	rec = serveAdmin(t, mux, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("slug-less update status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Slug != "renamed" {
		t.Errorf("slug = %q, want renamed preserved on a slug-less update", body.Slug)
	}
}

// TestProjectDownloads_RoundTrip pins the project-side full-replacement
// contract: the rows round-trip through the staff detail (name label),
// and an update swaps the whole set.
func TestProjectDownloads_RoundTrip(t *testing.T) {
	t.Parallel()
	_, _, mux := setupProjectAdminHandlers(t, nil)

	createBody := `{"title":"P","thumbnailPath":"` + adminThumb + `","status":"published","downloads":[
		{"name":"Batch","magnetUrl":"magnet:?xt=urn:btih:one","torrentUrl":"https://fans.example/one.torrent"},
		{"name":"Special","torrentUrl":"https://fans.example/two.torrent"}
	]}`
	rec := serveAdmin(t, mux, adminRequest(http.MethodPost, "/api/v1/projects", createBody, "admin", "admin1"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	id := strings.TrimPrefix(rec.Header().Get("Location"), "/api/v1/admin/projects/")

	var got struct {
		Downloads []struct {
			Name       string  `json:"name"`
			MagnetURL  *string `json:"magnetUrl"`
			TorrentURL *string `json:"torrentUrl"`
		} `json:"downloads"`
	}
	detail := serveAdmin(t, mux, adminRequest(http.MethodGet, "/api/v1/admin/projects/"+id, "", "admin", "admin1"))
	if err := json.NewDecoder(detail.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Downloads) != 2 || got.Downloads[0].Name != "Batch" || got.Downloads[1].Name != "Special" {
		t.Fatalf("downloads = %+v, want Batch then Special", got.Downloads)
	}
	if got.Downloads[0].MagnetURL == nil || *got.Downloads[0].MagnetURL != "magnet:?xt=urn:btih:one" {
		t.Errorf("magnet round-trip = %v", got.Downloads[0].MagnetURL)
	}
	if got.Downloads[1].MagnetURL != nil {
		t.Errorf("magnet slot = %v, want null for the second row", got.Downloads[1].MagnetURL)
	}

	// Replace with one row (the update body has NO slug key).
	updateBody := `{"title":"P","thumbnailPath":"` + adminThumb + `","status":"published","downloads":[
		{"name":"Remastered","magnetUrl":"magnet:?xt=urn:btih:new"}
	]}`
	upd := adminRequest(http.MethodPut, "/api/v1/projects/"+id, updateBody, "admin", "admin1")
	upd.Header.Set("If-Match", `"1"`)
	if rec := serveAdmin(t, mux, upd); rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	got.Downloads = nil
	detail = serveAdmin(t, mux, adminRequest(http.MethodGet, "/api/v1/admin/projects/"+id, "", "admin", "admin1"))
	if err := json.NewDecoder(detail.Body).Decode(&got); err != nil {
		t.Fatalf("decode after update: %v", err)
	}
	if len(got.Downloads) != 1 || got.Downloads[0].Name != "Remastered" {
		t.Fatalf("downloads after update = %+v, want the single Remastered row", got.Downloads)
	}
}

func TestProjectDelete_Flow(t *testing.T) {
	t.Parallel()
	db, _, mux := setupProjectAdminHandlers(t, nil)

	seedProject(t, db, "pub1", "pub1", "published", "admin1", 1000)

	// The delete floor is admin+: a moderator
	// is 403 — before any precondition check.
	mod := adminRequest(http.MethodDelete, "/api/v1/projects/pub1", "", "moderator", "mod1")
	mod.Header.Set("If-Match", `"1"`)
	if rec := serveAdmin(t, mux, mod); rec.Code != http.StatusForbidden {
		t.Fatalf("moderator delete status = %d, want 403", rec.Code)
	}

	// Missing If-Match → 428; stale → 412.
	req := adminRequest(http.MethodDelete, "/api/v1/projects/pub1", "", "admin", "admin1")
	if rec := serveAdmin(t, mux, req); rec.Code != http.StatusPreconditionRequired {
		t.Fatalf("missing If-Match status = %d, want 428", rec.Code)
	}
	req = adminRequest(http.MethodDelete, "/api/v1/projects/pub1", "", "admin", "admin1")
	req.Header.Set("If-Match", `"9"`)
	if rec := serveAdmin(t, mux, req); rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale If-Match status = %d, want 412", rec.Code)
	}

	// Admin deletes → 204.
	req = adminRequest(http.MethodDelete, "/api/v1/projects/pub1", "", "admin", "admin1")
	req.Header.Set("If-Match", `"1"`)
	if rec := serveAdmin(t, mux, req); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204 (body %q)", rec.Code, rec.Body.String())
	}

	// The public detail now 404s (the shared mux proves the cascade).
	pub := httptest.NewRequest(http.MethodGet, "/api/v1/projects/pub1", nil)
	if rec := serveAdmin(t, mux, pub); rec.Code != http.StatusNotFound {
		t.Errorf("public detail after delete status = %d, want 404", rec.Code)
	}

	// A second delete is a masked 404.
	req = adminRequest(http.MethodDelete, "/api/v1/projects/pub1", "", "admin", "admin1")
	req.Header.Set("If-Match", `"1"`)
	if rec := serveAdmin(t, mux, req); rec.Code != http.StatusNotFound {
		t.Errorf("second delete status = %d, want 404", rec.Code)
	}
}
