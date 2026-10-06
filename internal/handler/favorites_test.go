package handler_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store"
	"sick-fansubs/internal/store/storetest"
)

// Favorites handler tests. The handlers read the session
// from the request context, so tests attach a synthetic SessionUser
// directly — the session/cookie/CSRF middleware is covered by the routes
// and middleware suites.

// setupFavoritesHandlers creates a migrated database seeded with one user,
// published + draft content, and a mux with the eight favorites handlers
// registered at their production paths.
func setupFavoritesHandlers(t *testing.T) (*sql.DB, http.Handler) {
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
		ID:       "u1",
		Username: "Katakuri",
	})
	for _, q := range []string{
		`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('b1', 'B1', 'Sub B1', 'blog one', 'media/images/ab/b1.jpg', 'published', 2000, 1000, 1000)`,
		`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('b2', 'B2', '', 'blog two', 'https://example.com/b2.jpg', 'published', 3000, 1000, 1000)`,
		`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('bdraft', 'Draft', '', 'draft', 'https://example.com/b3.jpg', 'draft', NULL, 1000, 1000)`,
		`INSERT INTO projects (id, title, description, slug, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('p1', 'P1', 'project one', 'p1', 'https://example.com/p1.jpg', 'published', 2000, 1000, 1000)`,
		`INSERT INTO projects (id, title, description, slug, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('pdraft', 'PD', 'draft', 'pd', 'https://example.com/pd.jpg', 'draft', NULL, 1000, 1000)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed content: %v", err)
		}
	}

	mux := http.NewServeMux()
	const publicBase = "https://fans.example"
	mux.HandleFunc("GET /api/v1/users/me/favorites/blog-posts", handler.FavoritesList(handler.BlogFavoritesKind, db, publicBase))
	mux.HandleFunc("GET /api/v1/users/me/favorites/blog-posts/{id}", handler.FavoritesStatus(handler.BlogFavoritesKind, db))
	mux.HandleFunc("PUT /api/v1/users/me/favorites/blog-posts/{id}", handler.FavoritesAdd(handler.BlogFavoritesKind, db, nil))
	mux.HandleFunc("DELETE /api/v1/users/me/favorites/blog-posts/{id}", handler.FavoritesRemove(handler.BlogFavoritesKind, db))
	mux.HandleFunc("GET /api/v1/users/me/favorites/projects", handler.FavoritesList(handler.ProjectFavoritesKind, db, publicBase))
	mux.HandleFunc("GET /api/v1/users/me/favorites/projects/{id}", handler.FavoritesStatus(handler.ProjectFavoritesKind, db))
	mux.HandleFunc("PUT /api/v1/users/me/favorites/projects/{id}", handler.FavoritesAdd(handler.ProjectFavoritesKind, db, nil))
	mux.HandleFunc("DELETE /api/v1/users/me/favorites/projects/{id}", handler.FavoritesRemove(handler.ProjectFavoritesKind, db))

	return db, logContext(nil, middleware.RequestID()(mux))
}

// favRequest builds a request with the session context attached (or without
// when withSession is false).
func favRequest(method, path string, withSession bool) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if withSession {
		req = req.WithContext(middleware.SetSession(req.Context(), &identity.SessionUser{
			SessionID: "s1",
			UserID:    "u1",
			Username:  "Katakuri",
			Role:      "user",
		}))
	}
	return req
}

func serve(mux http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestFavoritesList_Unauthenticated pins the 401 for every endpoint without
// a session.
func TestFavoritesList_Unauthenticated(t *testing.T) {
	t.Parallel()

	_, mux := setupFavoritesHandlers(t)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/users/me/favorites/blog-posts"},
		{http.MethodGet, "/api/v1/users/me/favorites/blog-posts/b1"},
		{http.MethodPut, "/api/v1/users/me/favorites/blog-posts/b1"},
		{http.MethodDelete, "/api/v1/users/me/favorites/blog-posts/b1"},
	} {
		rec := serve(mux, favRequest(tc.method, tc.path, false))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

// TestFavoritesAdd_InsertAndIdempotent pins 204, the row creation, and the
// repeat-favorite idempotency.
func TestFavoritesAdd_InsertAndIdempotent(t *testing.T) {
	t.Parallel()

	db, mux := setupFavoritesHandlers(t)

	rec := serve(mux, favRequest(http.MethodPut, "/api/v1/users/me/favorites/blog-posts/b1", true))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("add: status = %d, want 204 (body %q)", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID missing on 204")
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("204 body = %q, want empty", rec.Body.String())
	}

	// Row exists; repeat is also 204.
	_, favorited, err := store.FavoriteStatus(t.Context(), db, store.BlogContent, "u1", "b1")
	if err != nil || !favorited {
		t.Fatalf("status after add = (%v,%v), want favorited", favorited, err)
	}
	if rec = serve(mux, favRequest(http.MethodPut, "/api/v1/users/me/favorites/blog-posts/b1", true)); rec.Code != http.StatusNoContent {
		t.Fatalf("repeat add: status = %d, want 204", rec.Code)
	}
}

// TestFavoritesAdd_MaskedNotFound pins 404 for drafts and unknown ids, and
// 400 for an invalid id shape.
func TestFavoritesAdd_MaskedNotFound(t *testing.T) {
	t.Parallel()

	_, mux := setupFavoritesHandlers(t)

	for _, tc := range []struct {
		path string
		want int
	}{
		{"/api/v1/users/me/favorites/blog-posts/bdraft", http.StatusNotFound},
		{"/api/v1/users/me/favorites/blog-posts/unknown", http.StatusNotFound},
		{"/api/v1/users/me/favorites/blog-posts/bad%20id", http.StatusBadRequest},
	} {
		rec := serve(mux, favRequest(http.MethodPut, tc.path, true))
		if rec.Code != tc.want {
			t.Errorf("PUT %s: status = %d, want %d", tc.path, rec.Code, tc.want)
		}
	}
}

// TestFavoritesRemove_Idempotent pins 204 on remove and repeat-remove.
func TestFavoritesRemove_Idempotent(t *testing.T) {
	t.Parallel()

	db, mux := setupFavoritesHandlers(t)
	ctx := t.Context()
	if err := store.AddFavorite(ctx, db, store.BlogContent, "u1", "b1", 5_000); err != nil {
		t.Fatalf("seed favorite: %v", err)
	}

	rec := serve(mux, favRequest(http.MethodDelete, "/api/v1/users/me/favorites/blog-posts/b1", true))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("remove: status = %d, want 204", rec.Code)
	}
	if rec = serve(mux, favRequest(http.MethodDelete, "/api/v1/users/me/favorites/blog-posts/b1", true)); rec.Code != http.StatusNoContent {
		t.Fatalf("repeat remove: status = %d, want 204", rec.Code)
	}
	_, favorited, err := store.FavoriteStatus(ctx, db, store.BlogContent, "u1", "b1")
	if err != nil || favorited {
		t.Fatalf("status after remove: favorited = %v, err = %v", favorited, err)
	}
}

// TestFavoritesStatus pins the 200 {favorited} contract and the masked 404.
func TestFavoritesStatus(t *testing.T) {
	t.Parallel()

	db, mux := setupFavoritesHandlers(t)
	ctx := t.Context()

	rec := serve(mux, favRequest(http.MethodGet, "/api/v1/users/me/favorites/blog-posts/b1", true))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d, want 200", rec.Code)
	}
	var body map[string]bool
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["favorited"] {
		t.Error("favorited = true, want false before add")
	}

	if err := store.AddFavorite(ctx, db, store.BlogContent, "u1", "b1", 5_000); err != nil {
		t.Fatalf("seed favorite: %v", err)
	}
	rec = serve(mux, favRequest(http.MethodGet, "/api/v1/users/me/favorites/blog-posts/b1", true))
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body["favorited"] {
		t.Error("favorited = false, want true after add")
	}

	// Masked: draft and unknown ids answer the same 404.
	for _, id := range []string{"bdraft", "unknown"} {
		rec = serve(mux, favRequest(http.MethodGet, "/api/v1/users/me/favorites/blog-posts/"+id, true))
		if rec.Code != http.StatusNotFound {
			t.Errorf("status %s: %d, want 404", id, rec.Code)
		}
	}
}

// TestFavoritesList_EnvelopeAndOrdering pins the keyset envelope, the
// favoriting-time ordering, canonical instants, and the absolute thumbnail
// URL.
func TestFavoritesList_EnvelopeAndOrdering(t *testing.T) {
	t.Parallel()

	db, mux := setupFavoritesHandlers(t)
	ctx := t.Context()
	// Favorite b2 FIRST (older favorite time) — the list orders by
	// favoriting time, newest first, not publish time.
	if err := store.AddFavorite(ctx, db, store.BlogContent, "u1", "b2", 1_700_000_000_000); err != nil {
		t.Fatalf("seed b2: %v", err)
	}
	if err := store.AddFavorite(ctx, db, store.BlogContent, "u1", "b1", 1_700_000_000_500); err != nil {
		t.Fatalf("seed b1: %v", err)
	}

	rec := serve(mux, favRequest(http.MethodGet, "/api/v1/users/me/favorites/blog-posts", true))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}

	var list struct {
		Items []struct {
			ID           string  `json:"id"`
			Title        string  `json:"title"`
			Subtitle     *string `json:"subtitle"`
			ThumbnailURL string  `json:"thumbnailUrl"`
			PublishedAt  string  `json:"publishedAt"`
			FavoritedAt  string  `json:"favoritedAt"`
		} `json:"items"`
		PageInfo struct {
			HasNextPage bool    `json:"hasNextPage"`
			EndCursor   *string `json:"endCursor"`
		} `json:"pageInfo"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(list.Items))
	}
	if list.Items[0].ID != "b1" || list.Items[1].ID != "b2" {
		t.Errorf("order = %s,%s, want b1,b2 (newest favorite first)", list.Items[0].ID, list.Items[1].ID)
	}
	if list.Items[0].Subtitle == nil || *list.Items[0].Subtitle != "Sub B1" {
		t.Errorf("b1 subtitle = %v, want the blog post's subtitle present", list.Items[0].Subtitle)
	}
	if list.Items[1].Subtitle == nil || *list.Items[1].Subtitle != "" {
		t.Errorf("b2 subtitle = %v, want present and empty", list.Items[1].Subtitle)
	}
	if list.Items[0].ThumbnailURL != "https://fans.example/media/images/b1.jpg" {
		t.Errorf("thumbnailUrl = %q, want absolute media URL (2-hex subdir dropped)", list.Items[0].ThumbnailURL)
	}
	if list.Items[0].FavoritedAt != "2023-11-14T22:13:20.500Z" {
		t.Errorf("favoritedAt = %q, want canonical millisecond instant", list.Items[0].FavoritedAt)
	}
	if list.PageInfo.HasNextPage || list.PageInfo.EndCursor != nil {
		t.Errorf("pageInfo = %+v, want hasNext=false, endCursor=null", list.PageInfo)
	}
}

// TestFavoritesList_ProjectSubtitleEmpty pins the project leg of the wire
// contract: the subtitle key is present and empty (OpenAPI marks it
// required), never omitted.
func TestFavoritesList_ProjectSubtitleEmpty(t *testing.T) {
	t.Parallel()

	db, mux := setupFavoritesHandlers(t)
	if err := store.AddFavorite(t.Context(), db, store.ProjectContent, "u1", "p1", 1_000); err != nil {
		t.Fatalf("seed favorite: %v", err)
	}

	rec := serve(mux, favRequest(http.MethodGet, "/api/v1/users/me/favorites/projects", true))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []struct {
			Subtitle *string `json:"subtitle"`
		} `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(list.Items))
	}
	if list.Items[0].Subtitle == nil {
		t.Fatal("subtitle key absent — OpenAPI marks it required")
	}
	if *list.Items[0].Subtitle != "" {
		t.Errorf("subtitle = %q, want empty for projects", *list.Items[0].Subtitle)
	}
}

// TestFavoritesList_CursorPaging pins the endCursor + after round trip.
func TestFavoritesList_CursorPaging(t *testing.T) {
	t.Parallel()

	db, mux := setupFavoritesHandlers(t)
	ctx := t.Context()
	if err := store.AddFavorite(ctx, db, store.BlogContent, "u1", "b2", 1_000); err != nil {
		t.Fatalf("seed b2: %v", err)
	}
	if err := store.AddFavorite(ctx, db, store.BlogContent, "u1", "b1", 2_000); err != nil {
		t.Fatalf("seed b1: %v", err)
	}

	rec := serve(mux, favRequest(http.MethodGet, "/api/v1/users/me/favorites/blog-posts?limit=1", true))
	if rec.Code != http.StatusOK {
		t.Fatalf("page 1: %d, want 200", rec.Code)
	}
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		PageInfo struct {
			HasNextPage bool    `json:"hasNextPage"`
			EndCursor   *string `json:"endCursor"`
		} `json:"pageInfo"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Items) != 1 || !list.PageInfo.HasNextPage || list.PageInfo.EndCursor == nil {
		t.Fatalf("page 1 = %+v, want 1 item + endCursor", list)
	}

	rec = serve(mux, favRequest(http.MethodGet, "/api/v1/users/me/favorites/blog-posts?limit=1&after="+*list.PageInfo.EndCursor, true))
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode page 2: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].ID != "b2" || list.PageInfo.HasNextPage {
		t.Fatalf("page 2 = %+v, want [b2] final", list)
	}
}

// TestFavoritesList_BadCursor pins the 400 on a foreign/invalid cursor.
func TestFavoritesList_BadCursor(t *testing.T) {
	t.Parallel()

	_, mux := setupFavoritesHandlers(t)

	// A cursor from the blog list namespace (v1.) is foreign to the
	// favorites namespace (fv1.) — one opaque 400.
	rec := serve(mux, favRequest(http.MethodGet, "/api/v1/users/me/favorites/blog-posts?after=bm9wZQ", true))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid cursor: %d, want 400", rec.Code)
	}
}

// TestFavoritesAdd_ProjectKind proves the project namespace end to end.
func TestFavoritesAdd_ProjectKind(t *testing.T) {
	t.Parallel()

	_, mux := setupFavoritesHandlers(t)

	rec := serve(mux, favRequest(http.MethodPut, "/api/v1/users/me/favorites/projects/p1", true))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("add project: %d, want 204 (body %q)", rec.Code, rec.Body.String())
	}
	if rec = serve(mux, favRequest(http.MethodPut, "/api/v1/users/me/favorites/projects/pdraft", true)); rec.Code != http.StatusNotFound {
		t.Fatalf("add draft project: %d, want 404", rec.Code)
	}
}

// TestFavoritesAdd_InjectedClock proves the handler's clock supplies
// created_at_ms (the favoritedAt the list emits).
func TestFavoritesAdd_InjectedClock(t *testing.T) {
	t.Parallel()

	db, _ := setupFavoritesHandlers(t)

	fixed := time.Date(2024, 5, 6, 7, 8, 9, 123_000_000, time.UTC)
	h := handler.FavoritesAdd(handler.BlogFavoritesKind, db, func() time.Time { return fixed })

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /x/{id}", h)
	req := favRequest(http.MethodPut, "/x/b1", true)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("add: %d, want 204", rec.Code)
	}

	items, _, err := store.ListFavorites(t.Context(), db, store.BlogContent, "u1", 10, nil)
	if err != nil || len(items) != 1 {
		t.Fatalf("list after add: %d items, err %v", len(items), err)
	}
	if items[0].FavoritedAtMS != fixed.UnixMilli() {
		t.Errorf("favoritedAtMS = %d, want %d (injected clock)", items[0].FavoritedAtMS, fixed.UnixMilli())
	}
}

// TestFavoritesList_IndicatorCounts pins the favorite-item counts:
// the account-page cards carry the same comment + favorite indicators as
// every other content surface.
func TestFavoritesList_IndicatorCounts(t *testing.T) {
	t.Parallel()

	db, mux := setupFavoritesHandlers(t)
	ctx := t.Context()
	if err := store.AddFavorite(ctx, db, store.BlogContent, "u1", "b1", 1_000); err != nil {
		t.Fatalf("add favorite: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO blog_post_comments
		(id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		VALUES ('c1', 'u1', 'b1', NULL, 'body', 0, 1, 1)`); err != nil {
		t.Fatalf("insert comment: %v", err)
	}

	rec := serve(mux, favRequest(http.MethodGet, "/api/v1/users/me/favorites/blog-posts", true))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(body.Items))
	}
	if body.Items[0]["commentCount"] != float64(1) || body.Items[0]["favoriteCount"] != float64(1) {
		t.Errorf("indicator counts = (%v comments, %v favorites), want (1, 1)",
			body.Items[0]["commentCount"], body.Items[0]["favoriteCount"])
	}
}
