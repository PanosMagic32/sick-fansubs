package handler_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store/storetest"
)

// The fallback account's stored avatar is canonical, so the served URL drops
// the 2-hex subdirectory (the mediaURL contract).
const (
	fallbackStoredAvatar = "media/images/ab/abcdef0123456789abcdef0123456789.png"
	fallbackServedAvatar = "https://fans.example/media/images/abcdef0123456789abcdef0123456789.png"
)

func seedPublishedBlog(t *testing.T, db *sql.DB, id, creator string, published int64) {
	t.Helper()

	const q = `INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status,
		 creator_id, published_at_ms, created_at_ms, updated_at_ms)
		VALUES (?, 'Title ' || ?, '', '', 'https://example.com/' || ? || '.jpg', 'published',
		 NULLIF(?, ''), ?, ?, ?)`
	if _, err := db.Exec(q, id, id, id, creator, published, published-500, published); err != nil {
		t.Fatalf("insert published blog fixture %q: %v", id, err)
	}
}

func seedPublishedProject(t *testing.T, db *sql.DB, id, creator string, published int64) {
	t.Helper()

	const q = `INSERT INTO projects
		(id, title, description, slug, thumbnail_url, status,
		 creator_id, published_at_ms, created_at_ms, updated_at_ms)
		VALUES (?, 'Title ' || ?, '', 'slug-' || ?, 'https://example.com/' || ? || '.jpg', 'published',
		 NULLIF(?, ''), ?, ?, ?)`
	if _, err := db.Exec(q, id, id, id, id, creator, published, published-500, published); err != nil {
		t.Fatalf("insert published project fixture %q: %v", id, err)
	}
}

// openFallbackFixture migrates a fresh database and seeds the uploader
// fallback account ("Kushoyarou", the username the handler's constant names),
// an avatarless creator, a creator with its own avatar, a creator whose
// stored avatar the scheme guard rejects, and the byline shapes on both
// content kinds (blog: own, guard-rejected, borrow, absent; projects: own,
// absent, borrow).
func openFallbackFixture(t *testing.T) *sql.DB {
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

	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:          "fallback",
		Username:    "Kushoyarou",
		AvatarURL:   fallbackStoredAvatar,
		CreatedAtMS: 1,
	})
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:          "u-anna",
		Username:    "Anna",
		CreatedAtMS: 1,
	})
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:          "u-owner",
		Username:    "Owner",
		AvatarURL:   "https://example.com/o.png",
		CreatedAtMS: 1,
	})
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:          "u-guard",
		Username:    "Guard",
		AvatarURL:   "javascript:alert(1)",
		CreatedAtMS: 1,
	})

	seedPublishedBlog(t, db, "b-own", "u-owner", 3000)
	seedPublishedBlog(t, db, "b-guard", "u-guard", 2500)
	seedPublishedBlog(t, db, "b-borrow", "u-anna", 2000)
	seedPublishedBlog(t, db, "b-anon", "", 1000)

	seedPublishedProject(t, db, "p-own", "u-owner", 3000)
	seedPublishedProject(t, db, "p-anon", "", 2000)
	seedPublishedProject(t, db, "p-borrow", "u-anna", 1000)

	return db
}

// byID indexes a decoded list body's items by id.
func byID(t *testing.T, body map[string]any) map[string]map[string]any {
	t.Helper()

	items, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("items missing or wrong type: %v", body)
	}
	out := make(map[string]map[string]any, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("item wrong type: %v", item)
		}
		id, ok := m["id"].(string)
		if !ok {
			t.Fatalf("item id missing or wrong type: %v", item)
		}
		out[id] = m
	}
	return out
}

// refOf narrows a decoded item's creator/updater/author field to a map.
func refOf(t *testing.T, item map[string]any, field string) map[string]any {
	t.Helper()

	ref, ok := item[field].(map[string]any)
	if !ok {
		t.Fatalf("%s = %v, want object", field, item[field])
	}
	return ref
}

// TestBlogList_UploaderFallback pins the public byline fallback: a creator
// without an avatar keeps its name and borrows the fallback picture, a
// guard-rejected stored avatar counts as absent, a null creator becomes the
// fallback account as a whole, and a creator with its own avatar is
// untouched.
func TestBlogList_UploaderFallback(t *testing.T) {
	t.Parallel()

	db := openFallbackFixture(t)
	h := middleware.RequestID()(handler.BlogList(db, "https://fans.example"))

	_, body := doBlogList(t, h, "/api/v1/blog-posts")
	items := byID(t, body)

	own := refOf(t, items["b-own"], "creator")
	if own["username"] != "Owner" || own["avatarUrl"] != "https://example.com/o.png" {
		t.Errorf("BlogList creator for b-own = %v, want Owner with its own avatar", own)
	}

	guard := refOf(t, items["b-guard"], "creator")
	if guard["username"] != "Guard" || guard["avatarUrl"] != fallbackServedAvatar {
		t.Errorf("BlogList creator for b-guard = %v, want Guard borrowing the fallback avatar", guard)
	}

	borrow := refOf(t, items["b-borrow"], "creator")
	if borrow["id"] != "u-anna" || borrow["username"] != "Anna" || borrow["avatarUrl"] != fallbackServedAvatar {
		t.Errorf("BlogList creator for b-borrow = %v, want Anna with the fallback avatar", borrow)
	}

	anon := refOf(t, items["b-anon"], "creator")
	if anon["id"] != "fallback" || anon["username"] != "Kushoyarou" || anon["avatarUrl"] != fallbackServedAvatar {
		t.Errorf("BlogList creator for b-anon = %v, want the fallback account as a whole", anon)
	}
}

// TestBlogDetail_UploaderFallback pins the detail bylines: the creator keeps
// its identity and borrows the picture, the absent updater becomes the
// fallback account as a whole.
func TestBlogDetail_UploaderFallback(t *testing.T) {
	t.Parallel()

	db := openFallbackFixture(t)
	h := middleware.RequestID()(handler.BlogDetail(db, "https://fans.example"))

	_, body := doBlogDetail(t, h, "b-borrow", "")
	creator := refOf(t, body, "creator")
	if creator["username"] != "Anna" || creator["avatarUrl"] != fallbackServedAvatar {
		t.Errorf("BlogDetail creator for b-borrow = %v, want Anna with the fallback avatar", creator)
	}
	updater := refOf(t, body, "updater")
	if updater["id"] != "fallback" || updater["username"] != "Kushoyarou" || updater["avatarUrl"] != fallbackServedAvatar {
		t.Errorf("BlogDetail updater for b-borrow = %v, want the fallback account as a whole", updater)
	}

	_, anon := doBlogDetail(t, h, "b-anon", "")
	anonCreator := refOf(t, anon, "creator")
	if anonCreator["id"] != "fallback" || anonCreator["avatarUrl"] != fallbackServedAvatar {
		t.Errorf("BlogDetail creator for b-anon = %v, want the fallback account as a whole", anonCreator)
	}
}

// TestProjectList_UploaderFallback pins the migration shape: projects with no
// creator render the fallback account as a whole, an avatarless creator
// keeps its name with the fallback picture, and an own avatar is untouched.
func TestProjectList_UploaderFallback(t *testing.T) {
	t.Parallel()

	db := openFallbackFixture(t)
	h := middleware.RequestID()(handler.ProjectList(db, "https://fans.example"))

	_, body := doProjectList(t, h, "/api/v1/projects")
	items := byID(t, body)

	own := refOf(t, items["p-own"], "creator")
	if own["username"] != "Owner" || own["avatarUrl"] != "https://example.com/o.png" {
		t.Errorf("ProjectList creator for p-own = %v, want Owner with its own avatar", own)
	}

	anon := refOf(t, items["p-anon"], "creator")
	if anon["id"] != "fallback" || anon["username"] != "Kushoyarou" || anon["avatarUrl"] != fallbackServedAvatar {
		t.Errorf("ProjectList creator for p-anon = %v, want the fallback account as a whole", anon)
	}

	borrow := refOf(t, items["p-borrow"], "creator")
	if borrow["username"] != "Anna" || borrow["avatarUrl"] != fallbackServedAvatar {
		t.Errorf("ProjectList creator for p-borrow = %v, want Anna with the fallback avatar", borrow)
	}
}

// TestProjectDetail_UploaderFallback pins the detail bylines for the
// migration shape: both the absent creator and the absent updater become the
// fallback account as a whole.
func TestProjectDetail_UploaderFallback(t *testing.T) {
	t.Parallel()

	db := openFallbackFixture(t)
	h := middleware.RequestID()(handler.ProjectDetail(db, "https://fans.example"))

	_, body := doProjectDetail(t, h, "p-anon", "")
	creator := refOf(t, body, "creator")
	if creator["id"] != "fallback" || creator["username"] != "Kushoyarou" || creator["avatarUrl"] != fallbackServedAvatar {
		t.Errorf("ProjectDetail creator for p-anon = %v, want the fallback account as a whole", creator)
	}
	updater := refOf(t, body, "updater")
	if updater["id"] != "fallback" || updater["username"] != "Kushoyarou" || updater["avatarUrl"] != fallbackServedAvatar {
		t.Errorf("ProjectDetail updater for p-anon = %v, want the fallback account as a whole", updater)
	}

	_, borrow := doProjectDetail(t, h, "p-borrow", "")
	borrowCreator := refOf(t, borrow, "creator")
	if borrowCreator["username"] != "Anna" || borrowCreator["avatarUrl"] != fallbackServedAvatar {
		t.Errorf("ProjectDetail creator for p-borrow = %v, want Anna with the fallback avatar", borrowCreator)
	}
}

// TestUploaderFallback_MissingAccountKeepsTheNeutralShape pins the implied
// no-op: without a resolvable fallback account the projection degrades to the
// pre-fallback shape (null creator, null avatar).
func TestUploaderFallback_MissingAccountKeepsTheNeutralShape(t *testing.T) {
	t.Parallel()

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

	storetest.InsertUser(t, db, storetest.UserSpec{
		ID: "u-anna", Username: "Anna", CreatedAtMS: 1,
	})
	seedPublishedBlog(t, db, "b-anon", "", 2000)
	seedPublishedBlog(t, db, "b-borrow", "u-anna", 1000)

	h := middleware.RequestID()(handler.BlogList(db, "https://fans.example"))
	rec, body := doBlogList(t, h, "/api/v1/blog-posts")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%v)", rec.Code, body)
	}
	items := byID(t, body)

	if anon := items["b-anon"]["creator"]; anon != nil {
		t.Errorf("BlogList creator for b-anon = %v, want null without a fallback account", anon)
	}
	borrow := refOf(t, items["b-borrow"], "creator")
	if borrow["username"] != "Anna" || borrow["avatarUrl"] != nil {
		t.Errorf("BlogList creator for b-borrow = %v, want Anna with a null avatar", borrow)
	}
}

// TestUploaderFallback_SuspendedAccountWithoutAvatar pins both recorded
// rulings: a suspended fallback account still projects (suspension is not
// erasure), and its missing avatar leaves the borrowed picture and the
// fallback-as-a-whole avatar null while the identity still resolves.
func TestUploaderFallback_SuspendedAccountWithoutAvatar(t *testing.T) {
	t.Parallel()

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

	storetest.InsertUser(t, db, storetest.UserSpec{
		ID: "fallback", Username: "Kushoyarou", Status: "suspended", CreatedAtMS: 1,
	})
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID: "u-anna", Username: "Anna", CreatedAtMS: 1,
	})
	seedPublishedBlog(t, db, "b-anon", "", 2000)
	seedPublishedBlog(t, db, "b-borrow", "u-anna", 1000)

	h := middleware.RequestID()(handler.BlogList(db, "https://fans.example"))
	_, body := doBlogList(t, h, "/api/v1/blog-posts")
	items := byID(t, body)

	anon := refOf(t, items["b-anon"], "creator")
	if anon["id"] != "fallback" || anon["username"] != "Kushoyarou" || anon["avatarUrl"] != nil {
		t.Errorf("BlogList creator for b-anon = %v, want the suspended fallback account with a null avatar", anon)
	}
	borrow := refOf(t, items["b-borrow"], "creator")
	if borrow["username"] != "Anna" || borrow["avatarUrl"] != nil {
		t.Errorf("BlogList creator for b-borrow = %v, want Anna with a null avatar", borrow)
	}
}

// TestUploaderFallback_StoreFailureIsMaskedInternal pins the failure arm: a
// fallback lookup that fails for a reason other than ErrNotFound answers the
// standard masked 500, never a silent degrade.
func TestUploaderFallback_StoreFailureIsMaskedInternal(t *testing.T) {
	t.Parallel()

	db := openFallbackFixture(t)
	if err := db.Close(); err != nil {
		t.Fatalf("close fixture database: %v", err)
	}

	h := middleware.RequestID()(handler.BlogList(db, "https://fans.example"))
	rec, body := doBlogList(t, h, "/api/v1/blog-posts")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (%v)", rec.Code, body)
	}
}

// TestUploaderFallback_StaffAndCommentsKeepRawRefs pins the scope boundary:
// with the fallback account present, the staff DTOs and the comment authors
// still carry the raw refs — the fallback is a public content-byline
// projection only.
func TestUploaderFallback_StaffAndCommentsKeepRawRefs(t *testing.T) {
	t.Parallel()

	db := openFallbackFixture(t)
	if _, err := db.Exec(`INSERT INTO blog_post_comments (id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		VALUES ('c1', 'u-anna', 'b-borrow', NULL, 'body', 0, 1000, 1000)`); err != nil {
		t.Fatalf("insert comment fixture: %v", err)
	}

	staff := middleware.RequestID()(handler.BlogStaffList(db, "https://fans.example"))
	rec := serveAdmin(t, staff, adminRequest(http.MethodGet, "/api/v1/admin/blog-posts", "", "moderator", "mod1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("staff list status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var staffBody map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&staffBody); err != nil {
		t.Fatalf("decode staff body %q: %v", rec.Body.String(), err)
	}
	staffItems := byID(t, staffBody)
	if anon := staffItems["b-anon"]["creator"]; anon != nil {
		t.Errorf("staff creator for b-anon = %v, want null (the fallback is public-only)", anon)
	}
	staffBorrow := refOf(t, staffItems["b-borrow"], "creator")
	if staffBorrow["username"] != "Anna" || staffBorrow["avatarUrl"] != nil {
		t.Errorf("staff creator for b-borrow = %v, want Anna with a null avatar", staffBorrow)
	}

	comments := middleware.RequestID()(handler.CommentsList(handler.BlogCommentsKind, db, "https://fans.example"))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/blog-posts/b-borrow/comments", nil)
	req.SetPathValue("id", "b-borrow")
	crec := httptest.NewRecorder()
	comments.ServeHTTP(crec, req)
	if crec.Code != http.StatusOK {
		t.Fatalf("comments status = %d, want 200 (body %q)", crec.Code, crec.Body.String())
	}
	var commentsBody map[string]any
	if err := json.NewDecoder(crec.Body).Decode(&commentsBody); err != nil {
		t.Fatalf("decode comments body %q: %v", crec.Body.String(), err)
	}
	author := refOf(t, byID(t, commentsBody)["c1"], "author")
	if author["username"] != "Anna" || author["avatarUrl"] != nil {
		t.Errorf("comment author for c1 = %v, want Anna with a null avatar", author)
	}
}
