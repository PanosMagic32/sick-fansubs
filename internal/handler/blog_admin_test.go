package handler_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store/storetest"
)

// Blog content-administration handler tests:
// role gates, the write contract, ETag/If-Match, masked draft visibility,
// and the audit events. The handlers read the session from the context, so
// tests attach a synthetic SessionUser directly — the cookie/CSRF/origin
// middleware is covered by the routes and middleware suites (the favorites
// pattern).

const adminThumb = "media/images/ab/abcdef0123456789abcdef0123456789.jpg"

// setupBlogAdminHandlers builds a migrated database with the staff users, a
// valid thumbnail file, and a mux with the five content-administration
// handlers at their production paths. Returns the db and the data dir.
func setupBlogAdminHandlers(t *testing.T, logger *slog.Logger) (*sql.DB, string, http.Handler) {
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
	mux.HandleFunc("GET /api/v1/blog-posts", handler.BlogList(db, publicBase))
	mux.HandleFunc("GET /api/v1/blog-posts/{id}", handler.BlogDetail(db, publicBase))
	mux.HandleFunc("POST /api/v1/blog-posts", handler.BlogCreate(db, dir, publicBase, nil, nil))
	mux.HandleFunc("GET /api/v1/admin/blog-posts", handler.BlogStaffList(db, publicBase))
	mux.HandleFunc("GET /api/v1/admin/blog-posts/{id}", handler.BlogStaffDetail(db, publicBase))
	mux.HandleFunc("PUT /api/v1/blog-posts/{id}", handler.BlogUpdate(db, dir, publicBase, nil, nil))
	mux.HandleFunc("DELETE /api/v1/blog-posts/{id}", handler.BlogDelete(db))

	return db, dir, logContext(logger, mux)
}

// adminRequest builds a request with a synthetic session attached.
func adminRequest(method, path, body string, role, userID string) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req = req.WithContext(middleware.SetSession(req.Context(), &identity.SessionUser{
		SessionID: "s1", UserID: userID, Username: "Tester", Role: role,
	}))
	return req
}

func serveAdmin(t *testing.T, mux http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// writeBody builds a valid create/update body: the downloads array is
// required and carries one 1080p magnet row — the tests' minimal valid set.
func writeBody(status string) string {
	return `{"title":"New Post","subtitle":"Sub","description":"Desc","thumbnailPath":"` + adminThumb + `","status":"` + status + `","downloads":[{"resolution":"1080p","magnetUrl":"magnet:?xt=urn:btih:abc123"}]}`
}

// TestBlogCreate_RejectsParsedHelperKey pins the strict-decode contract on
// the write DTO: DownloadsParsed carries `json:"-"`, so a body key naming it
// (case-insensitively) is an unknown field — 400, never a silent decode.
func TestBlogCreate_RejectsParsedHelperKey(t *testing.T) {
	t.Parallel()
	_, _, mux := setupBlogAdminHandlers(t, nil)

	body := `{"title":"N","thumbnailPath":"` + adminThumb + `","status":"draft",` +
		`"downloads":[{"resolution":"1080p","magnetUrl":"magnet:?xt=urn:btih:abc123"}],` +
		`"downloadsParsed":[]}`
	rec := serveAdmin(t, mux, adminRequest(http.MethodPost, "/api/v1/blog-posts", body, "admin", "admin1"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for the downloadsParsed key (body %s)", rec.Code, rec.Body.String())
	}
}

// TestBlogCreate_BodyBound pins the 512 KiB content-write bound both ways:
// a body over it is 413 before decode; a body exactly at it is accepted and
// validated normally.
func TestBlogCreate_BodyBound(t *testing.T) {
	t.Parallel()
	_, _, mux := setupBlogAdminHandlers(t, nil)

	t.Run("over the bound", func(t *testing.T) {
		t.Parallel()
		body := writeBody("draft") + strings.Repeat(" ", 512*1024)
		rec := serveAdmin(t, mux, adminRequest(http.MethodPost, "/api/v1/blog-posts", body, "admin", "admin1"))
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413 (body %s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("exactly at the bound", func(t *testing.T) {
		t.Parallel()
		base := writeBody("draft")
		body := base + strings.Repeat(" ", 512*1024-len(base))
		rec := serveAdmin(t, mux, adminRequest(http.MethodPost, "/api/v1/blog-posts", body, "admin", "admin1"))
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
		}
	})
}

func TestBlogCreate_RoleMatrix(t *testing.T) {
	t.Parallel()
	_, _, mux := setupBlogAdminHandlers(t, nil)

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
		{"anonymous cannot create", "", "", http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var req *http.Request
			if c.role == "" {
				req = httptest.NewRequest(http.MethodPost, "/api/v1/blog-posts", strings.NewReader(writeBody("draft")))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = adminRequest(http.MethodPost, "/api/v1/blog-posts", writeBody("draft"), c.role, c.userID)
			}
			rec := serveAdmin(t, mux, req)
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, c.want, rec.Body.String())
			}
		})
	}
}

// TestBlogCreate_StoreFailureAnswers500AndAuditsOnce pins the create failure
// arm: a store failure answers the masked 500 with exactly ONE
// content_created failure row.
func TestBlogCreate_StoreFailureAnswers500AndAuditsOnce(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, _, mux := setupBlogAdminHandlers(t, logger)
	db.Close() // the create's store call now fails

	rec := serveAdmin(t, mux, adminRequest(http.MethodPost, "/api/v1/blog-posts", writeBody("draft"), "admin", "admin1"))
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

func TestBlogCreate_Success(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	_, _, mux := setupBlogAdminHandlers(t, logger)

	rec := serveAdmin(t, mux, adminRequest(http.MethodPost, "/api/v1/blog-posts", writeBody("published"), "admin", "admin1"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/api/v1/admin/blog-posts/") {
		t.Errorf("Location = %q, want the staff detail address", loc)
	}
	var body struct {
		Status   string `json:"status"`
		Revision int64  `json:"revision"`
		Creator  *struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"creator"`
		PublishedAt string `json:"publishedAt"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "published" || body.Revision != 1 {
		t.Errorf("body = status %q revision %d, want published/1", body.Status, body.Revision)
	}
	if body.Creator == nil || body.Creator.ID != "admin1" {
		t.Errorf("creator = %+v, want admin1", body.Creator)
	}
	if body.PublishedAt == "" {
		t.Error("publishedAt missing for a published create")
	}

	// The audit trail carries content_created/success.
	logs := buf.String()
	if !strings.Contains(logs, `"event":"content_created"`) || !strings.Contains(logs, `"result":"success"`) {
		t.Errorf("audit log = %s, want content_created success", logs)
	}

	// The row is visible to a moderator through the staff list.
	listRec := serveAdmin(t, mux, adminRequest(http.MethodGet, "/api/v1/admin/blog-posts", "", "moderator", "mod1"))
	if listRec.Code != http.StatusOK {
		t.Fatalf("staff list status = %d, want 200", listRec.Code)
	}
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(listRec.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("staff list items = %d, want the 1 created post", len(list.Items))
	}
}

// TestBlogStaffList_Filters pins the blog half of the staff content filter at
// the HTTP boundary: the
// parameter allowlist, the value reaching the query, and an unknown parameter
// still being a 400 inside the new allowlist.
func TestBlogStaffList_Filters(t *testing.T) {
	t.Parallel()
	db, _, mux := setupBlogAdminHandlers(t, nil)

	// Titles matter here (the filter matches the title), so seed directly
	// rather than through the create handler. b3 carries the punctuation the
	// word-boundary rule exists for.
	for id, row := range map[string]struct{ title, status string }{
		"b1": {"One Piece", "published"},
		"b2": {"Άμλετ", "draft"},
		"b3": {"Dr. Stone", "published"},
	} {
		if _, err := db.Exec(
			`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
				creator_id, updater_id, published_at_ms, created_at_ms, updated_at_ms, revision)
			 VALUES (?, ?, '', '', 'media/images/ab/abcdef0123456789abcdef0123456789.jpg', ?, 'admin1', 'admin1',
				5000, 5000, 5000, 1)`, id, row.title, row.status); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	list := func(t *testing.T, target string) []string {
		t.Helper()
		rec := serveAdmin(t, mux, adminRequest(http.MethodGet, target, "", "super-admin", "sa1"))
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

	if got := list(t, "/api/v1/admin/blog-posts?status=draft"); len(got) != 1 || got[0] != "b2" {
		t.Errorf("status=draft = %v, want [b2]", got)
	}
	if got := list(t, "/api/v1/admin/blog-posts?q=αμλετ"); len(got) != 1 || got[0] != "b2" {
		t.Errorf("q=αμλετ = %v, want [b2] (accent-folded title match)", got)
	}
	if got := list(t, "/api/v1/admin/blog-posts?q=piece&status=published"); len(got) != 1 || got[0] != "b1" {
		t.Errorf("q+status = %v, want [b1]", got)
	}

	// The query is split at word boundaries before folding, so a space in the
	// query reaches a punctuation-separated title — the owner's "dr stone must
	// find Dr. Stone" finding, matching what the public search does.
	if got := list(t, "/api/v1/admin/blog-posts?q="+url.QueryEscape("dr stone")); len(got) != 1 || got[0] != "b3" {
		t.Errorf("q=dr stone = %v, want [b3]", got)
	}
	if got := list(t, "/api/v1/admin/blog-posts?q="+url.QueryEscape("dr.stone")); len(got) != 1 || got[0] != "b3" {
		t.Errorf("q=dr.stone = %v, want [b3]", got)
	}
	// Every word must appear: a query whose second word is absent matches
	// nothing, even though its first word does.
	if got := list(t, "/api/v1/admin/blog-posts?q="+url.QueryEscape("dr house")); len(got) != 0 {
		t.Errorf("q=dr house = %v, want no match", got)
	}

	// An empty filter value means "no filter" (the staff user list's
	// precedent), not a 422 — and so does a value that only whitespace, which
	// the q filter trims.
	if got := list(t, "/api/v1/admin/blog-posts?status=&q=%20"); len(got) != 3 {
		t.Errorf("blank filters = %v, want every row", got)
	}

	// The allowlist widened by exactly two names: a third unknown one is
	// still a 400, and the problem is not silently ignored now that the
	// endpoint accepts more parameters.
	rec := serveAdmin(t, mux, adminRequest(http.MethodGet, "/api/v1/admin/blog-posts?sort=title", "", "super-admin", "sa1"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown parameter status = %d, want 400 (body %q)", rec.Code, rec.Body.String())
	}
}

func TestBlogCreate_Validation(t *testing.T) {
	t.Parallel()
	_, _, mux := setupBlogAdminHandlers(t, nil)

	cases := []struct {
		name      string
		body      string
		code      int
		wantField string
		wantCode  string
	}{
		{"empty title", `{"title":"  ","thumbnailPath":"` + adminThumb + `","status":"draft","downloads":[{"resolution":"1080p","magnetUrl":"magnet:?xt=x"}]}`, 422, "", ""},
		{"missing thumbnail", `{"title":"T","status":"draft","downloads":[{"resolution":"1080p","magnetUrl":"magnet:?xt=x"}]}`, 422, "", ""},
		{"bad thumbnail shape", `{"title":"T","thumbnailPath":"media/images/ab/zz.jpg","status":"draft","downloads":[{"resolution":"1080p","magnetUrl":"magnet:?xt=x"}]}`, 422, "thumbnailPath", "invalidFormat"},
		// The canonical file EXISTS at media/images/ab/<id>.jpg; the mismatch
		// is the subdirectory, which the grammar must reject before the file
		// check — a stored non-canonical reference would later look
		// unreferenced to the sweep and the delete guard.
		{"mismatched thumbnail subdirectory", `{"title":"T","thumbnailPath":"media/images/cd/abcdef0123456789abcdef0123456789.jpg","status":"draft","downloads":[{"resolution":"1080p","magnetUrl":"magnet:?xt=x"}]}`, 422, "thumbnailPath", "invalidFormat"},
		{"bad status", `{"title":"T","thumbnailPath":"` + adminThumb + `","status":"bogus","downloads":[{"resolution":"1080p","magnetUrl":"magnet:?xt=x"}]}`, 422, "status", "invalidValue"},
		{"archived at create", `{"title":"T","thumbnailPath":"` + adminThumb + `","status":"archived","downloads":[{"resolution":"1080p","magnetUrl":"magnet:?xt=x"}]}`, 422, "status", "invalidValue"},
		{"unknown field", `{"title":"T","thumbnailPath":"` + adminThumb + `","status":"draft","downloads":[{"resolution":"1080p","magnetUrl":"magnet:?xt=x"}],"nope":1}`, 400, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := serveAdmin(t, mux, adminRequest(http.MethodPost, "/api/v1/blog-posts", c.body, "admin", "admin1"))
			if rec.Code != c.code {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, c.code, rec.Body.String())
			}
			if c.wantCode == "" {
				return
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
				if v.Field == c.wantField && v.Code == c.wantCode {
					found = true
				}
			}
			if !found {
				t.Errorf("violations = %+v, want a {%s, %s} violation", problem.Violations, c.wantField, c.wantCode)
			}
		})
	}
}

func TestBlogStaffDetail_ETagAndDraftGate(t *testing.T) {
	t.Parallel()
	_, _, mux := setupBlogAdminHandlers(t, nil)

	// Create a draft as admin1 (id comes from the Location header).
	rec := serveAdmin(t, mux, adminRequest(http.MethodPost, "/api/v1/blog-posts", writeBody("draft"), "admin", "admin1"))
	id := strings.TrimPrefix(rec.Header().Get("Location"), "/api/v1/admin/blog-posts/")

	// The creator sees it with the ETag.
	detail := serveAdmin(t, mux, adminRequest(http.MethodGet, "/api/v1/admin/blog-posts/"+id, "", "admin", "admin1"))
	if detail.Code != http.StatusOK {
		t.Fatalf("creator detail status = %d, want 200", detail.Code)
	}
	if got := detail.Header().Get("ETag"); got != `"1"` {
		t.Errorf("ETag = %q, want %q (revision-backed)", got, `"1"`)
	}

	// Another admin (same role) gets the masked 404.
	other := serveAdmin(t, mux, adminRequest(http.MethodGet, "/api/v1/admin/blog-posts/"+id, "", "super-admin", "sa1"))
	if other.Code != http.StatusOK {
		t.Fatalf("super-admin should see the admin's draft: %d", other.Code)
	}
	mod := serveAdmin(t, mux, adminRequest(http.MethodGet, "/api/v1/admin/blog-posts/"+id, "", "moderator", "mod1"))
	if mod.Code != http.StatusNotFound {
		t.Fatalf("moderator detail status = %d, want masked 404", mod.Code)
	}
}

func TestBlogUpdate_Preconditions(t *testing.T) {
	t.Parallel()
	_, _, mux := setupBlogAdminHandlers(t, nil)

	rec := serveAdmin(t, mux, adminRequest(http.MethodPost, "/api/v1/blog-posts", writeBody("published"), "admin", "admin1"))
	id := strings.TrimPrefix(rec.Header().Get("Location"), "/api/v1/admin/blog-posts/")

	// Missing If-Match → 428.
	noMatch := serveAdmin(t, mux, adminRequest(http.MethodPut, "/api/v1/blog-posts/"+id, writeBody("published"), "moderator", "mod1"))
	if noMatch.Code != http.StatusPreconditionRequired {
		t.Fatalf("status = %d, want 428 without If-Match", noMatch.Code)
	}

	// Stale If-Match → 412.
	stale := adminRequest(http.MethodPut, "/api/v1/blog-posts/"+id, writeBody("published"), "moderator", "mod1")
	stale.Header.Set("If-Match", `"99"`)
	staleRec := serveAdmin(t, mux, stale)
	if staleRec.Code != http.StatusPreconditionFailed {
		t.Fatalf("status = %d, want 412 for a stale revision", staleRec.Code)
	}

	// Matching If-Match → 200 with the bumped revision and fresh ETag.
	match := adminRequest(http.MethodPut, "/api/v1/blog-posts/"+id, writeBody("published"), "moderator", "mod1")
	match.Header.Set("If-Match", `"1"`)
	matchRec := serveAdmin(t, mux, match)
	if matchRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", matchRec.Code, matchRec.Body.String())
	}
	if got := matchRec.Header().Get("ETag"); got != `"2"` {
		t.Errorf("ETag = %q, want %q", got, `"2"`)
	}
	var body struct {
		Revision int64 `json:"revision"`
	}
	if err := json.NewDecoder(matchRec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Revision != 2 {
		t.Errorf("revision = %d, want 2", body.Revision)
	}
}

func TestBlogUpdate_DraftInvisibleToModerator(t *testing.T) {
	t.Parallel()
	_, _, mux := setupBlogAdminHandlers(t, nil)

	rec := serveAdmin(t, mux, adminRequest(http.MethodPost, "/api/v1/blog-posts", writeBody("draft"), "admin", "admin1"))
	id := strings.TrimPrefix(rec.Header().Get("Location"), "/api/v1/admin/blog-posts/")

	// The moderator cannot address the admin's draft — masked 404, before
	// any precondition check.
	req := adminRequest(http.MethodPut, "/api/v1/blog-posts/"+id, writeBody("draft"), "moderator", "mod1")
	req.Header.Set("If-Match", `"1"`)
	rec2 := serveAdmin(t, mux, req)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want masked 404 (a moderator cannot modify an admin's draft)", rec2.Code)
	}
}

func TestBlogDelete_SuccessAndStale(t *testing.T) {
	t.Parallel()
	db, _, mux := setupBlogAdminHandlers(t, nil)

	rec := serveAdmin(t, mux, adminRequest(http.MethodPost, "/api/v1/blog-posts", writeBody("published"), "admin", "admin1"))
	id := strings.TrimPrefix(rec.Header().Get("Location"), "/api/v1/admin/blog-posts/")

	// The delete floor is admin+: a moderator
	// is 403 — before any precondition check.
	mod := adminRequest(http.MethodDelete, "/api/v1/blog-posts/"+id, "", "moderator", "mod1")
	mod.Header.Set("If-Match", `"1"`)
	if got := serveAdmin(t, mux, mod).Code; got != http.StatusForbidden {
		t.Fatalf("moderator delete status = %d, want 403", got)
	}

	// Stale → 412, the row survives.
	stale := adminRequest(http.MethodDelete, "/api/v1/blog-posts/"+id, "", "admin", "admin1")
	stale.Header.Set("If-Match", `"42"`)
	if got := serveAdmin(t, mux, stale).Code; got != http.StatusPreconditionFailed {
		t.Fatalf("stale delete status = %d, want 412", got)
	}

	// Matching → 204 and the row is gone.
	match := adminRequest(http.MethodDelete, "/api/v1/blog-posts/"+id, "", "admin", "admin1")
	match.Header.Set("If-Match", `"1"`)
	if got := serveAdmin(t, mux, match).Code; got != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", got)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM blog_posts WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatalf("count after delete: %v", err)
	}
	if n != 0 {
		t.Errorf("row count = %d, want 0", n)
	}

	// The public detail now 404s (masked).
	req := httptest.NewRequest(http.MethodGet, "/api/v1/blog-posts/"+id, nil)
	if got := serveAdmin(t, mux, req).Code; got != http.StatusNotFound {
		t.Errorf("public detail after delete = %d, want 404", got)
	}
}

// TestBlogWrite_DownloadValidation pins the download-row contract:
// required + bounded array, label bounds,
// per-link grammar (the READ boundary's safeDownloadLink), and the
// at-least-one-link-per-row rule.
func TestBlogWrite_DownloadValidation(t *testing.T) {
	t.Parallel()
	_, _, mux := setupBlogAdminHandlers(t, nil)

	base := func(downloads string) string {
		return `{"title":"T","thumbnailPath":"` + adminThumb + `","status":"draft"` + downloads + `}`
	}
	longLabel := strings.Repeat("a", 101)
	longLink := "https://fans.example/" + strings.Repeat("x", 2000)

	cases := []struct {
		name          string
		downloads     string
		want          int
		wantViolation string
	}{
		{"missing downloads", ``, 422, `"field":"downloads","code":"required"`},
		{"empty downloads", `,"downloads":[]`, 422, `"field":"downloads","code":"required"`},
		{"empty label", `,"downloads":[{"resolution":"  ","magnetUrl":"magnet:?xt=x"}]`, 422, `"field":"downloads[0].resolution","code":"required"`},
		{"long label", `,"downloads":[{"resolution":"` + longLabel + `","magnetUrl":"magnet:?xt=x"}]`, 422, `"field":"downloads[0].resolution","code":"maxLength"`},
		{"both links empty", `,"downloads":[{"resolution":"1080p","magnetUrl":"","torrentUrl":""}]`, 422, `"field":"downloads[0]","code":"required"`},
		{"javascript link", `,"downloads":[{"resolution":"1080p","magnetUrl":"javascript:alert(1)"}]`, 422, `"field":"downloads[0].magnetUrl","code":"invalidFormat"`},
		{"plain text link", `,"downloads":[{"resolution":"1080p","torrentUrl":"not-a-link"}]`, 422, `"field":"downloads[0].torrentUrl","code":"invalidFormat"`},
		{"hostless http link", `,"downloads":[{"resolution":"1080p","magnetUrl":"http://"}]`, 422, `"field":"downloads[0].magnetUrl","code":"invalidFormat"`},
		{"long link", `,"downloads":[{"resolution":"1080p","magnetUrl":"` + longLink + `"}]`, 422, `"field":"downloads[0].magnetUrl","code":"maxLength"`},
		{"empty magnet magnet link", `,"downloads":[{"resolution":"1080p","magnetUrl":"magnet:"}]`, 422, `"field":"downloads[0].magnetUrl","code":"invalidFormat"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := serveAdmin(t, mux, adminRequest(http.MethodPost, "/api/v1/blog-posts", base(c.downloads), "admin", "admin1"))
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, c.want, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), c.wantViolation) {
				t.Errorf("violation %q missing from body: %s", c.wantViolation, rec.Body.String())
			}
		})
	}
}

// TestBlogWrite_TooManyDownloads pins the 20-row bound.
func TestBlogWrite_TooManyDownloads(t *testing.T) {
	t.Parallel()
	_, _, mux := setupBlogAdminHandlers(t, nil)

	rows := make([]string, 21)
	for i := range rows {
		rows[i] = `{"resolution":"1080p","magnetUrl":"magnet:?xt=x"}`
	}
	body := `{"title":"T","thumbnailPath":"` + adminThumb + `","status":"draft","downloads":[` + strings.Join(rows, ",") + `]}`
	rec := serveAdmin(t, mux, adminRequest(http.MethodPost, "/api/v1/blog-posts", body, "admin", "admin1"))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"field":"downloads","code":"maxItems"`) {
		t.Errorf("violation downloads:maxItems missing from body: %s", rec.Body.String())
	}
}

// TestBlogDownloads_RoundTrip pins the full-replacement contract end to
// end: create stores the rows (staff detail returns them verbatim, in
// order), update replaces the whole set in one revision, and the links
// round-trip unmodified.
func TestBlogDownloads_RoundTrip(t *testing.T) {
	t.Parallel()
	_, _, mux := setupBlogAdminHandlers(t, nil)

	createBody := `{"title":"T","thumbnailPath":"` + adminThumb + `","status":"published","downloads":[
		{"resolution":"1080p","magnetUrl":"magnet:?xt=urn:btih:one","torrentUrl":"https://fans.example/one.torrent"},
		{"resolution":"2160p","magnetUrl":"magnet:?xt=urn:btih:two"}
	]}`
	rec := serveAdmin(t, mux, adminRequest(http.MethodPost, "/api/v1/blog-posts", createBody, "admin", "admin1"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	id := strings.TrimPrefix(rec.Header().Get("Location"), "/api/v1/admin/blog-posts/")

	detail := serveAdmin(t, mux, adminRequest(http.MethodGet, "/api/v1/admin/blog-posts/"+id, "", "admin", "admin1"))
	var got struct {
		Downloads []struct {
			Resolution string  `json:"resolution"`
			MagnetURL  *string `json:"magnetUrl"`
			TorrentURL *string `json:"torrentUrl"`
		} `json:"downloads"`
	}
	if err := json.NewDecoder(detail.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Downloads) != 2 || got.Downloads[0].Resolution != "1080p" || got.Downloads[1].Resolution != "2160p" {
		t.Fatalf("downloads = %+v, want 1080p then 2160p", got.Downloads)
	}
	if got.Downloads[0].MagnetURL == nil || *got.Downloads[0].MagnetURL != "magnet:?xt=urn:btih:one" {
		t.Errorf("magnet round-trip = %v", got.Downloads[0].MagnetURL)
	}
	if got.Downloads[0].TorrentURL == nil || *got.Downloads[0].TorrentURL != "https://fans.example/one.torrent" {
		t.Errorf("torrent round-trip = %v", got.Downloads[0].TorrentURL)
	}
	if got.Downloads[1].TorrentURL != nil {
		t.Errorf("torrent slot = %v, want null for the second row", got.Downloads[1].TorrentURL)
	}

	// Update replaces the whole set with one row.
	updateBody := `{"title":"T","thumbnailPath":"` + adminThumb + `","status":"published","downloads":[
		{"resolution":"720p","torrentUrl":"https://fans.example/replace.torrent"}
	]}`
	upd := adminRequest(http.MethodPut, "/api/v1/blog-posts/"+id, updateBody, "admin", "admin1")
	upd.Header.Set("If-Match", `"1"`)
	updRec := serveAdmin(t, mux, upd)
	if updRec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200 (body %s)", updRec.Code, updRec.Body.String())
	}

	detail = serveAdmin(t, mux, adminRequest(http.MethodGet, "/api/v1/admin/blog-posts/"+id, "", "admin", "admin1"))
	got.Downloads = nil
	if err := json.NewDecoder(detail.Body).Decode(&got); err != nil {
		t.Fatalf("decode after update: %v", err)
	}
	if len(got.Downloads) != 1 || got.Downloads[0].Resolution != "720p" {
		t.Fatalf("downloads after update = %+v, want the single 720p row", got.Downloads)
	}
}
