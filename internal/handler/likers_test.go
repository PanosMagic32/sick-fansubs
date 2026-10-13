package handler_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store/storetest"
)

// Likers handler tests (the who-liked lists behind the likers modal). The
// handlers read the session from the request context (the favorites-harness
// pattern); the middleware chains are covered by the routes suite.

// likerItemWire and likerListWire mirror the LikerItem/LikerList schemas.
type likerItemWire struct {
	ID        string  `json:"id"`
	Username  string  `json:"username"`
	AvatarURL *string `json:"avatarUrl"`
}

type likerListWire struct {
	Items    []likerItemWire `json:"items"`
	PageInfo struct {
		HasNextPage bool    `json:"hasNextPage"`
		EndCursor   *string `json:"endCursor"`
		Total       int     `json:"total"`
	} `json:"pageInfo"`
}

// setupLikersHandlers creates a migrated database seeded with two users,
// published + draft content, one comment per content kind, and a mux with
// the four likers handlers at their production paths.
func setupLikersHandlers(t *testing.T) (*sql.DB, http.Handler) {
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

	storetest.InsertUser(t, db, storetest.UserSpec{ID: "u1", Username: "Katakuri"})
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:        "u2",
		Username:  "Perospero",
		AvatarURL: "media/images/aa/aabbccddeeff00112233445566778899.jpg",
	})
	for _, q := range []string{
		`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('b1', 'B1', '', 'blog one', 'https://example.com/b1.jpg', 'published', 2000, 1000, 1000)`,
		`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('b2', 'B2', '', 'blog two', 'https://example.com/b2.jpg', 'published', 3000, 1000, 1000)`,
		`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('bdraft', 'Draft', '', 'draft', 'https://example.com/b3.jpg', 'draft', NULL, 1000, 1000)`,
		`INSERT INTO projects (id, title, description, slug, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('p1', 'P1', 'project one', 'p1', 'https://example.com/p1.jpg', 'published', 2000, 1000, 1000)`,
		`INSERT INTO blog_post_comments (id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		 VALUES ('c1', 'u2', 'b1', NULL, 'top', 2, 1000, 1000)`,
		`INSERT INTO project_comments (id, user_id, project_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		 VALUES ('pc1', 'u2', 'p1', NULL, 'top', 1, 1000, 1000)`,
		`INSERT INTO blog_post_comment_hearts (user_id, comment_id, created_at_ms) VALUES ('u1', 'c1', 1000)`,
		`INSERT INTO blog_post_comment_hearts (user_id, comment_id, created_at_ms) VALUES ('u2', 'c1', 2000)`,
		`INSERT INTO project_comment_hearts (user_id, comment_id, created_at_ms) VALUES ('u2', 'pc1', 3000)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed fixtures: %v", err)
		}
	}

	mux := http.NewServeMux()
	const publicBase = "https://fans.example"
	mux.HandleFunc("GET /api/v1/blog-posts/{id}/favorites", handler.FavoriteLikers(handler.BlogFavoritersKind, db, publicBase))
	mux.HandleFunc("GET /api/v1/projects/{id}/favorites", handler.FavoriteLikers(handler.ProjectFavoritersKind, db, publicBase))
	mux.HandleFunc("GET /api/v1/blog-posts/{id}/comments/{commentId}/hearts", handler.CommentHearters(handler.BlogHeartersKind, db, publicBase))
	mux.HandleFunc("GET /api/v1/projects/{id}/comments/{commentId}/hearts", handler.CommentHearters(handler.ProjectHeartersKind, db, publicBase))

	return db, logContext(nil, middleware.RequestID()(mux))
}

// TestLikers_Unauthenticated pins the 401 on every likers endpoint without a
// session — the who-liked lists are never a public surface.
func TestLikers_Unauthenticated(t *testing.T) {
	t.Parallel()

	_, mux := setupLikersHandlers(t)

	for _, path := range []string{
		"/api/v1/blog-posts/b1/favorites",
		"/api/v1/projects/p1/favorites",
		"/api/v1/blog-posts/b1/comments/c1/hearts",
		"/api/v1/projects/p1/comments/pc1/hearts",
	} {
		rec := serve(mux, favRequest(http.MethodGet, path, false))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s: status = %d, want 401", path, rec.Code)
		}
	}
}

// TestFavoriteLikers_EnvelopeAndAvatar pins the wire envelope, the newest-first
// order, the total, and the absolute avatar URL (a stored media path).
func TestFavoriteLikers_EnvelopeAndAvatar(t *testing.T) {
	t.Parallel()

	db, mux := setupLikersHandlers(t)
	ctx := t.Context()
	for _, fav := range []struct {
		userID string
		atMS   int64
	}{
		{userID: "u1", atMS: 1_000},
		{userID: "u2", atMS: 2_000},
	} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO blog_post_favorites (user_id, blog_post_id, created_at_ms) VALUES (?, 'b1', ?)`,
			fav.userID, fav.atMS); err != nil {
			t.Fatalf("seed favorite %s: %v", fav.userID, err)
		}
	}

	rec := serve(mux, favRequest(http.MethodGet, "/api/v1/blog-posts/b1/favorites", true))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	var body likerListWire
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 2 || body.Items[0].ID != "u2" || body.Items[1].ID != "u1" {
		t.Fatalf("items = %+v, want [u2, u1] (newest first)", body.Items)
	}
	wantAvatar := "https://fans.example/media/images/aabbccddeeff00112233445566778899.jpg"
	if body.Items[0].AvatarURL == nil || *body.Items[0].AvatarURL != wantAvatar {
		t.Errorf("u2 avatarUrl = %v, want %q", body.Items[0].AvatarURL, wantAvatar)
	}
	if body.Items[1].AvatarURL != nil {
		t.Errorf("u1 avatarUrl = %q, want null", *body.Items[1].AvatarURL)
	}
	if body.PageInfo.Total != 2 || body.PageInfo.HasNextPage || body.PageInfo.EndCursor != nil {
		t.Errorf("pageInfo = %+v, want {total 2, no next, no cursor}", body.PageInfo)
	}
}

// TestFavoriteLikers_CursorPaging pins the keyset continuation and the 400/422
// parameter outcomes.
func TestFavoriteLikers_CursorPaging(t *testing.T) {
	t.Parallel()

	db, mux := setupLikersHandlers(t)
	ctx := t.Context()
	for _, fav := range []struct {
		userID string
		atMS   int64
	}{
		{userID: "u1", atMS: 1_000},
		{userID: "u2", atMS: 2_000},
	} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO blog_post_favorites (user_id, blog_post_id, created_at_ms) VALUES (?, 'b1', ?)`,
			fav.userID, fav.atMS); err != nil {
			t.Fatalf("seed favorite %s: %v", fav.userID, err)
		}
	}

	rec := serve(mux, favRequest(http.MethodGet, "/api/v1/blog-posts/b1/favorites?limit=1", true))
	var page1 likerListWire
	if err := json.NewDecoder(rec.Body).Decode(&page1); err != nil {
		t.Fatalf("decode page 1: %v", err)
	}
	if len(page1.Items) != 1 || page1.Items[0].ID != "u2" || page1.PageInfo.EndCursor == nil {
		t.Fatalf("page 1 = %+v, want [u2] with an endCursor", page1)
	}

	rec = serve(mux, favRequest(http.MethodGet, "/api/v1/blog-posts/b1/favorites?limit=1&after="+*page1.PageInfo.EndCursor, true))
	var page2 likerListWire
	if err := json.NewDecoder(rec.Body).Decode(&page2); err != nil {
		t.Fatalf("decode page 2: %v", err)
	}
	if len(page2.Items) != 1 || page2.Items[0].ID != "u1" || page2.PageInfo.HasNextPage {
		t.Fatalf("page 2 = %+v, want [u1] and no next page", page2)
	}

	for _, tc := range []struct {
		name  string
		query string
		want  int
	}{
		{name: "invalid cursor", query: "?after=nope", want: http.StatusBadRequest},
		{name: "limit zero", query: "?limit=0", want: http.StatusUnprocessableEntity},
		{name: "limit over max", query: "?limit=101", want: http.StatusUnprocessableEntity},
		{name: "unknown parameter", query: "?unknown=1", want: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := serve(mux, favRequest(http.MethodGet, "/api/v1/blog-posts/b1/favorites"+tc.query, true))
			if rec.Code != tc.want {
				t.Errorf("GET favorites%s: status = %d, want %d", tc.query, rec.Code, tc.want)
			}
		})
	}
}

// TestFavoriteLikers_MaskedNotFound pins the masked 404s (draft, unknown) and
// the 400 invalid id shape, plus the projects mirror path.
func TestFavoriteLikers_MaskedNotFound(t *testing.T) {
	t.Parallel()

	db, mux := setupLikersHandlers(t)
	if _, err := db.Exec(
		`INSERT INTO project_favorites (user_id, project_id, created_at_ms) VALUES ('u1', 'p1', 1000)`,
	); err != nil {
		t.Fatalf("seed project favorite: %v", err)
	}

	for _, tc := range []struct {
		name string
		path string
		want int
	}{
		{name: "draft post", path: "/api/v1/blog-posts/bdraft/favorites", want: http.StatusNotFound},
		{name: "unknown post", path: "/api/v1/blog-posts/unknown/favorites", want: http.StatusNotFound},
		{name: "invalid id shape", path: "/api/v1/blog-posts/bad%20id/favorites", want: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := serve(mux, favRequest(http.MethodGet, tc.path, true))
			if rec.Code != tc.want {
				t.Errorf("GET %s: status = %d, want %d", tc.path, rec.Code, tc.want)
			}
		})
	}

	rec := serve(mux, favRequest(http.MethodGet, "/api/v1/projects/p1/favorites", true))
	if rec.Code != http.StatusOK {
		t.Fatalf("projects favorites status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var body likerListWire
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode projects favorites: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].ID != "u1" {
		t.Fatalf("projects favorites = %+v, want [u1]", body.Items)
	}
}

// TestCommentHearters_EnvelopeAndGate pins the hearters envelope and the
// masked 404s: unknown content, foreign content, unknown comment.
func TestCommentHearters_EnvelopeAndGate(t *testing.T) {
	t.Parallel()

	_, mux := setupLikersHandlers(t)

	rec := serve(mux, favRequest(http.MethodGet, "/api/v1/blog-posts/b1/comments/c1/hearts", true))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var body likerListWire
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 2 ||
		body.Items[0].ID != "u2" || body.Items[1].ID != "u1" ||
		body.PageInfo.Total != 2 {
		t.Fatalf("hearters = %+v (total %d), want [u2, u1] and total 2",
			body.Items, body.PageInfo.Total)
	}

	for _, tc := range []struct {
		name string
		path string
		want int
	}{
		{name: "foreign content", path: "/api/v1/blog-posts/b2/comments/c1/hearts", want: http.StatusNotFound},
		{name: "unknown comment", path: "/api/v1/blog-posts/b1/comments/nope/hearts", want: http.StatusNotFound},
		{name: "draft content", path: "/api/v1/blog-posts/bdraft/comments/c1/hearts", want: http.StatusNotFound},
		{name: "invalid comment id shape", path: "/api/v1/blog-posts/b1/comments/bad%20id/hearts", want: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := serve(mux, favRequest(http.MethodGet, tc.path, true))
			if rec.Code != tc.want {
				t.Errorf("GET %s: status = %d, want %d", tc.path, rec.Code, tc.want)
			}
		})
	}

	rec = serve(mux, favRequest(http.MethodGet, "/api/v1/projects/p1/comments/pc1/hearts", true))
	if rec.Code != http.StatusOK {
		t.Fatalf("project hearters status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode project hearters: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].ID != "u2" {
		t.Fatalf("project hearters = %+v, want [u2]", body.Items)
	}
}

// TestFavoriteLikers_ActiveOnly proves a suspended liker disappears from the
// list and the total while the content's favorite count is untouched by this
// reader.
func TestFavoriteLikers_ActiveOnly(t *testing.T) {
	t.Parallel()

	db, mux := setupLikersHandlers(t)
	ctx := t.Context()
	for _, fav := range []struct {
		userID string
		atMS   int64
	}{
		{userID: "u1", atMS: 1_000},
		{userID: "u2", atMS: 2_000},
	} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO blog_post_favorites (user_id, blog_post_id, created_at_ms) VALUES (?, 'b1', ?)`,
			fav.userID, fav.atMS); err != nil {
			t.Fatalf("seed favorite %s: %v", fav.userID, err)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE users SET status = 'suspended' WHERE id = 'u2'`); err != nil {
		t.Fatalf("suspend u2: %v", err)
	}

	rec := serve(mux, favRequest(http.MethodGet, "/api/v1/blog-posts/b1/favorites", true))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var body likerListWire
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].ID != "u1" || body.PageInfo.Total != 1 {
		t.Fatalf("items = %+v (total %d), want [u1] and total 1", body.Items, body.PageInfo.Total)
	}
}
