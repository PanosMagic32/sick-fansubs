package handler_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store/storetest"
)

// Notifications handler tests. The handlers read
// the session from the request context, so tests attach a synthetic
// SessionUser directly — the session/cookie/CSRF middleware is covered by
// the routes and middleware suites (the favorites precedent).

func setupNotificationsHandlers(t *testing.T) (*sql.DB, http.Handler) {
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
		{ID: "viewer", Username: "Viewer", Role: "moderator"},
		{ID: "plain", Username: "Plain"},
		{ID: "actor", Username: "Actorus", Role: "admin"},
		{ID: "target", Username: "Targetus"},
	} {
		storetest.InsertUser(t, db, u)
	}
	for _, q := range []string{
		`INSERT INTO audit_events (id, event, result, actor_id, target_id, target_role, request_id, remote_addr, created_at_ms)
		 VALUES ('ev1', 'password_reset', 'success', 'actor', 'target', 'user', 'r1', 'addr', 1000)`,
		`INSERT INTO audit_events (id, event, result, actor_id, target_id, target_role, request_id, remote_addr, created_at_ms)
		 VALUES ('ev2', 'role_changed', 'failure', 'actor', 'target', 'user', 'r2', 'addr', 2000)`,
		`INSERT INTO audit_events (id, event, result, actor_id, target_id, target_role, request_id, remote_addr, created_at_ms)
		 VALUES ('ev-hidden', 'password_reset', 'success', 'actor', 'tsuper', 'super-admin', 'r3', 'addr', 3000)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed event: %v", err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/notifications", handler.NotificationsList(db))
	mux.HandleFunc("GET /api/v1/notifications/unread-count", handler.NotificationsUnreadCount(db))
	now := func() time.Time { return time.Unix(5, 0) }
	mux.HandleFunc("POST /api/v1/notifications/{id}/read", handler.NotificationsMarkRead(db, now))
	mux.HandleFunc("POST /api/v1/notifications/read-all", handler.NotificationsMarkAllRead(db, now))
	mux.HandleFunc("DELETE /api/v1/notifications/{id}", handler.NotificationsDelete(db, now))
	mux.HandleFunc("POST /api/v1/notifications/clear-read", handler.NotificationsClearRead(db))
	mux.HandleFunc("POST /api/v1/notifications/delete-all", handler.NotificationsDeleteAll(db, now))

	return db, logContext(nil, mux)
}

func notifRequest(method, path string, role string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if role != "" {
		req = req.WithContext(middleware.SetSession(req.Context(), &identity.SessionUser{
			SessionID: "s1",
			UserID:    "viewer",
			Username:  "Viewer",
			Role:      role,
		}))
	}
	return req
}

func serveNotif(mux http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestNotificationsList_EnvelopeAndItems(t *testing.T) {
	_, mux := setupNotificationsHandlers(t)

	rec := serveNotif(mux, notifRequest("GET", "/api/v1/notifications", "moderator"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	var body struct {
		Items []struct {
			ID             string  `json:"id"`
			Kind           string  `json:"kind"`
			Result         string  `json:"result"`
			ActorUsername  *string `json:"actorUsername"`
			TargetUsername *string `json:"targetUsername"`
			TargetRole     *string `json:"targetRole"`
			CreatedAt      string  `json:"createdAt"`
		} `json:"items"`
		PageInfo struct {
			HasNextPage bool    `json:"hasNextPage"`
			EndCursor   *string `json:"endCursor"`
		} `json:"pageInfo"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	// ev-hidden (super-admin target) is invisible to a moderator.
	if len(body.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(body.Items))
	}
	if body.Items[0].ID != "ev2" || body.Items[1].ID != "ev1" {
		t.Fatalf("order: got %s, %s; want ev2, ev1", body.Items[0].ID, body.Items[1].ID)
	}
	if body.PageInfo.HasNextPage || body.PageInfo.EndCursor != nil {
		t.Errorf("pageInfo: want final page, got hasNext=%v cursor=%v",
			body.PageInfo.HasNextPage, body.PageInfo.EndCursor)
	}

	first := body.Items[0]
	if first.Kind != "role_changed" || first.Result != "failure" {
		t.Errorf("ev2 fields: %+v", first)
	}
	// The event field is superseded by kind — the wire
	// never carries it.
	if strings.Contains(rec.Body.String(), `"event"`) {
		t.Error("the event field is superseded by kind")
	}
	// Account items carry NO contentTitle key (the per-kind shapes are
	// disjoint — the OpenAPI oneOf).
	if strings.Contains(rec.Body.String(), `"contentTitle"`) {
		t.Error("account-event items must not carry contentTitle")
	}
	if first.ActorUsername == nil || *first.ActorUsername != "Actorus" {
		t.Errorf("actorUsername = %v, want Actorus", first.ActorUsername)
	}
	if first.TargetUsername == nil || *first.TargetUsername != "Targetus" {
		t.Errorf("targetUsername = %v, want Targetus", first.TargetUsername)
	}
	if first.CreatedAt != "1970-01-01T00:00:02.000Z" {
		t.Errorf("createdAt = %q", first.CreatedAt)
	}
}

func TestNotificationsList_KeysetCursor(t *testing.T) {
	_, mux := setupNotificationsHandlers(t)

	// limit=1 → first page ev2 with a cursor; after ev2 → ev1, final page.
	rec := serveNotif(mux, notifRequest("GET", "/api/v1/notifications?limit=1", "moderator"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var page struct {
		Items    []json.RawMessage `json:"items"`
		PageInfo struct {
			HasNextPage bool    `json:"hasNextPage"`
			EndCursor   *string `json:"endCursor"`
		} `json:"pageInfo"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !page.PageInfo.HasNextPage || page.PageInfo.EndCursor == nil {
		t.Fatalf("pageInfo = %+v, want hasNextPage true with an endCursor", page.PageInfo)
	}

	rec = serveNotif(mux, notifRequest("GET", "/api/v1/notifications?limit=1&after="+*page.PageInfo.EndCursor, "moderator"))
	var page2 struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		PageInfo struct {
			HasNextPage bool `json:"hasNextPage"`
		} `json:"pageInfo"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page2); err != nil {
		t.Fatalf("decode page 2: %v", err)
	}
	if len(page2.Items) != 1 || page2.Items[0].ID != "ev1" {
		t.Fatalf("page 2 items = %+v", page2.Items)
	}
	if page2.PageInfo.HasNextPage {
		t.Error("page 2: want final page")
	}
}

func TestNotificationsList_ParamFailures(t *testing.T) {
	_, mux := setupNotificationsHandlers(t)

	// Out-of-range limit → 422 {limit, outOfRange}.
	rec := serveNotif(mux, notifRequest("GET", "/api/v1/notifications?limit=101", "moderator"))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("limit=101 status = %d, want 422", rec.Code)
	}

	// Malformed cursor → 400.
	rec = serveNotif(mux, notifRequest("GET", "/api/v1/notifications?after=not-a-cursor", "moderator"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad cursor status = %d, want 400", rec.Code)
	}

	// Cross-endpoint cursor (blog namespace) → 400, never accepted.
	rec = serveNotif(mux, notifRequest("GET", "/api/v1/notifications?after=v1.eyJ2IjoxLCJwIjoxLCJpIjoiYiJ9", "moderator"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("cross-endpoint cursor status = %d, want 400", rec.Code)
	}
}

func TestNotificationsAuthZ(t *testing.T) {
	_, mux := setupNotificationsHandlers(t)

	paths := []struct {
		method, path string
	}{
		{"GET", "/api/v1/notifications"},
		{"GET", "/api/v1/notifications/unread-count"},
		{"POST", "/api/v1/notifications/read-all"},
		{"POST", "/api/v1/notifications/ev1/read"},
		{"DELETE", "/api/v1/notifications/ev1"},
		{"POST", "/api/v1/notifications/clear-read"},
		{"POST", "/api/v1/notifications/delete-all"},
	}
	for _, p := range paths {
		// Anonymous → 401.
		if rec := serveNotif(mux, notifRequest(p.method, p.path, "")); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s anonymous: got %d, want 401", p.method, p.path, rec.Code)
		}
	}

	// The unified-feed revision: an authenticated role=user
	// viewer IS admitted — the staff half of the feed is simply empty for
	// them (no more 403).
	rec := serveNotif(mux, notifRequest("GET", "/api/v1/notifications", "user"))
	if rec.Code != http.StatusOK {
		t.Fatalf("role=user list: got %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Errorf("role=user feed should be empty: %s", rec.Body.String())
	}
	rec = serveNotif(mux, notifRequest("POST", "/api/v1/notifications/ev1/read", "user"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("role=user marking an audit id: got %d, want 404 (per-space auth)", rec.Code)
	}
}

func TestNotificationsUnreadCount(t *testing.T) {
	db, mux := setupNotificationsHandlers(t)

	rec := serveNotif(mux, notifRequest("GET", "/api/v1/notifications/unread-count", "moderator"))
	var body struct {
		Unread int64 `json:"unread"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Unread != 2 {
		t.Fatalf("unread = %d, want 2 (ev-hidden invisible)", body.Unread)
	}

	// Mark one read → count drops.
	rec = serveNotif(mux, notifRequest("POST", "/api/v1/notifications/ev1/read", "moderator"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("mark read status = %d, want 204", rec.Code)
	}
	rec = serveNotif(mux, notifRequest("GET", "/api/v1/notifications/unread-count", "moderator"))
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Unread != 1 {
		t.Fatalf("unread after mark = %d, want 1", body.Unread)
	}

	// The read_at_ms comes from the injected clock (5 s).
	var readAt int64
	if err := db.QueryRow(`SELECT read_at_ms FROM notification_reads WHERE user_id = 'viewer' AND event_id = 'ev1'`).Scan(&readAt); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if readAt != 5000 {
		t.Errorf("read_at_ms = %d, want 5000", readAt)
	}
}

func TestNotificationsMarkRead_Masking(t *testing.T) {
	_, mux := setupNotificationsHandlers(t)

	// Invalid id shape → 400.
	if rec := serveNotif(mux, notifRequest("POST", "/api/v1/notifications/bad+id/read", "moderator")); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid shape: got %d, want 400", rec.Code)
	}
	// Unknown id → 404.
	if rec := serveNotif(mux, notifRequest("POST", "/api/v1/notifications/nope/read", "moderator")); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id: got %d, want 404", rec.Code)
	}
	// Hidden event → the SAME masked 404.
	if rec := serveNotif(mux, notifRequest("POST", "/api/v1/notifications/ev-hidden/read", "moderator")); rec.Code != http.StatusNotFound {
		t.Fatalf("hidden event: got %d, want 404", rec.Code)
	}
	// Idempotent repeat → 204.
	if rec := serveNotif(mux, notifRequest("POST", "/api/v1/notifications/ev1/read", "moderator")); rec.Code != http.StatusNoContent {
		t.Fatalf("first mark: got %d, want 204", rec.Code)
	}
	if rec := serveNotif(mux, notifRequest("POST", "/api/v1/notifications/ev1/read", "moderator")); rec.Code != http.StatusNoContent {
		t.Fatalf("repeat mark: got %d, want 204", rec.Code)
	}
}

func TestNotificationsMarkAllRead(t *testing.T) {
	_, mux := setupNotificationsHandlers(t)

	rec := serveNotif(mux, notifRequest("POST", "/api/v1/notifications/read-all", "moderator"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("read-all status = %d, want 204", rec.Code)
	}
	// Idempotent repeat.
	if rec := serveNotif(mux, notifRequest("POST", "/api/v1/notifications/read-all", "moderator")); rec.Code != http.StatusNoContent {
		t.Fatalf("repeat read-all status = %d, want 204", rec.Code)
	}

	rec = serveNotif(mux, notifRequest("GET", "/api/v1/notifications/unread-count", "moderator"))
	var body struct {
		Unread int64 `json:"unread"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Unread != 0 {
		t.Fatalf("unread after read-all = %d, want 0", body.Unread)
	}
}

// TestNotificationsCommentItem pins the comment_reply wire shape in the
// unified list: the kind discriminator, the content fields (title joined
// regardless of status), no result/target fields, and the cross-space
// mark-read.
func TestNotificationsCommentItem(t *testing.T) {
	db, mux := setupNotificationsHandlers(t)

	if _, err := db.Exec(`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
		published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('b1', 'A post', '', '', 'media/images/ab/b1.jpg', 'published', 2000, 1000, 1000)`); err != nil {
		t.Fatalf("seed post: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO user_notifications
		(id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms)
		VALUES ('n1', 'viewer', 'comment_reply', 'actor', 'blog-posts', 'b1', 'c1', 2500)`); err != nil {
		t.Fatalf("seed notification: %v", err)
	}

	rec := serveNotif(mux, notifRequest("GET", "/api/v1/notifications", "moderator"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Items []struct {
			ID           string  `json:"id"`
			Kind         string  `json:"kind"`
			Result       string  `json:"result"`
			ContentTitle *string `json:"contentTitle"`
			ContentKind  string  `json:"contentKind"`
			ContentID    string  `json:"contentId"`
			CommentID    string  `json:"commentId"`
			Actor        *string `json:"actorUsername"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 3 {
		t.Fatalf("items = %d, want 3 (n1 + ev2 + ev1)", len(body.Items))
	}
	// Newest first: n1 (2500) interleaves with the audit events.
	n1 := body.Items[0]
	if n1.ID != "n1" || n1.Kind != "comment_reply" {
		t.Fatalf("first item: %+v", n1)
	}
	if n1.ContentTitle == nil || *n1.ContentTitle != "A post" || n1.ContentKind != "blog-posts" || n1.ContentID != "b1" || n1.CommentID != "c1" {
		t.Errorf("content fields: %+v", n1)
	}
	if n1.Result != "" || n1.Actor == nil || *n1.Actor != "Actorus" {
		t.Errorf("comment item actor/result: %+v", n1)
	}

	// The comment item marks read through the user_notifications space.
	rec = serveNotif(mux, notifRequest("POST", "/api/v1/notifications/n1/read", "moderator"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("mark n1: got %d, want 204", rec.Code)
	}
	var readAt int64
	if err := db.QueryRow(`SELECT read_at_ms FROM user_notifications WHERE id = 'n1'`).Scan(&readAt); err != nil {
		t.Fatalf("read n1 state: %v", err)
	}
	if readAt != 5000 {
		t.Errorf("read_at_ms = %d, want 5000 (injected clock)", readAt)
	}
}

// TestNotificationsHeartItem pins the heart kind's wire shape: the same
// comment-item fields as comment_reply, with
// kind "heart" as the discriminator value.
func TestNotificationsHeartItem(t *testing.T) {
	db, mux := setupNotificationsHandlers(t)

	if _, err := db.Exec(`INSERT INTO projects (id, title, description, slug, thumbnail_url, status,
		published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('p1', 'A project', 'proj', 'a-project', 'https://example.com/p1.jpg', 'published', 2000, 1000, 1000)`); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO user_notifications
		(id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms)
		VALUES ('h1', 'viewer', 'heart', 'actor', 'projects', 'p1', 'c2', 2500)`); err != nil {
		t.Fatalf("seed heart notification: %v", err)
	}

	rec := serveNotif(mux, notifRequest("GET", "/api/v1/notifications", "moderator"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Items []struct {
			ID           string  `json:"id"`
			Kind         string  `json:"kind"`
			ContentTitle *string `json:"contentTitle"`
			ContentKind  string  `json:"contentKind"`
			ContentID    string  `json:"contentId"`
			CommentID    string  `json:"commentId"`
			Actor        *string `json:"actorUsername"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	h1 := body.Items[0]
	if h1.ID != "h1" || h1.Kind != "heart" {
		t.Fatalf("first item: %+v", h1)
	}
	if h1.ContentTitle == nil || *h1.ContentTitle != "A project" || h1.ContentKind != "projects" || h1.ContentID != "p1" || h1.CommentID != "c2" {
		t.Errorf("heart item content fields: %+v", h1)
	}
	if h1.Actor == nil || *h1.Actor != "Actorus" {
		t.Errorf("heart item actor: %+v", h1)
	}
}

// TestNotificationsCommentKind pins the 'comment' kind's wire
// shape: the comment-item fields with
// kind "comment" as the discriminator.
func TestNotificationsCommentKind(t *testing.T) {
	db, mux := setupNotificationsHandlers(t)

	if _, err := db.Exec(`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
		published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('b1', 'A post', '', '', 'media/images/ab/b1.jpg', 'published', 2000, 1000, 1000)`); err != nil {
		t.Fatalf("seed post: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO user_notifications
		(id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms)
		VALUES ('k1', 'viewer', 'comment', 'actor', 'blog-posts', 'b1', 'c3', 2500)`); err != nil {
		t.Fatalf("seed notification: %v", err)
	}

	rec := serveNotif(mux, notifRequest("GET", "/api/v1/notifications", "moderator"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Items []struct {
			ID           string  `json:"id"`
			Kind         string  `json:"kind"`
			ContentTitle *string `json:"contentTitle"`
			ContentKind  string  `json:"contentKind"`
			ContentID    string  `json:"contentId"`
			CommentID    string  `json:"commentId"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	k1 := body.Items[0]
	if k1.ID != "k1" || k1.Kind != "comment" {
		t.Fatalf("first item: %+v", k1)
	}
	if k1.ContentTitle == nil || *k1.ContentTitle != "A post" || k1.ContentKind != "blog-posts" || k1.ContentID != "b1" || k1.CommentID != "c3" {
		t.Errorf("comment-kind content fields: %+v", k1)
	}
}

// TestNotificationsContentUpdatedKind pins the content_updated
// wire shape: the content fields WITHOUT
// a commentId key at all (the content is the navigation target).
func TestNotificationsContentUpdatedKind(t *testing.T) {
	db, mux := setupNotificationsHandlers(t)

	if _, err := db.Exec(`INSERT INTO projects (id, title, description, slug, thumbnail_url, status,
		published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('p1', 'A project', 'proj', 'a-project', 'https://example.com/p1.jpg', 'published', 2000, 1000, 1000)`); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO user_notifications
		(id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms)
		VALUES ('u1', 'viewer', 'content_updated', 'actor', 'projects', 'p1', NULL, 2500)`); err != nil {
		t.Fatalf("seed notification: %v", err)
	}

	rec := serveNotif(mux, notifRequest("GET", "/api/v1/notifications", "moderator"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	item := body.Items[0]
	if item["id"] != "u1" || item["kind"] != "content_updated" {
		t.Fatalf("first item: %+v", item)
	}
	if item["contentKind"] != "projects" || item["contentId"] != "p1" || item["contentTitle"] != "A project" {
		t.Errorf("content fields: %+v", item)
	}
	if _, ok := item["commentId"]; ok {
		t.Errorf("content_updated item must omit commentId, got %+v", item)
	}
	if _, ok := item["result"]; ok {
		t.Errorf("content_updated item must omit the account-event fields, got %+v", item)
	}
}

// TestNotificationsNewContentKind pins the new_content wire shape:
// the SAME content-level shape as
// content_updated — content fields, NO commentId key, no account-event
// fields (the kind is the only discriminator).
func TestNotificationsNewContentKind(t *testing.T) {
	db, mux := setupNotificationsHandlers(t)

	if _, err := db.Exec(`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
		published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('b1', 'A post', '', 'post', 'https://example.com/b1.jpg', 'published', 2000, 1000, 1000)`); err != nil {
		t.Fatalf("seed post: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO user_notifications
		(id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms)
		VALUES ('n1', 'viewer', 'new_content', 'actor', 'blog-posts', 'b1', NULL, 2500)`); err != nil {
		t.Fatalf("seed notification: %v", err)
	}

	rec := serveNotif(mux, notifRequest("GET", "/api/v1/notifications", "moderator"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	item := body.Items[0]
	if item["id"] != "n1" || item["kind"] != "new_content" {
		t.Fatalf("first item: %+v", item)
	}
	if item["contentKind"] != "blog-posts" || item["contentId"] != "b1" || item["contentTitle"] != "A post" {
		t.Errorf("content fields: %+v", item)
	}
	if _, ok := item["commentId"]; ok {
		t.Errorf("new_content item must omit commentId, got %+v", item)
	}
	if _, ok := item["result"]; ok {
		t.Errorf("new_content item must omit the account-event fields, got %+v", item)
	}
}

// TestNotificationsDraftActivityKind pins the draft_activity wire shape:
// the content-level shape (no commentId — the notice
// names no comment) PLUS draftAction, carrying the transition verbatim. The
// feed joins the title WITHOUT a status gate, so an unpublished row's title
// reaches the audience the emitter chose (the draft-visibility gate) — the seed here
// is a draft post on purpose.
func TestNotificationsDraftActivityKind(t *testing.T) {
	db, mux := setupNotificationsHandlers(t)

	if _, err := db.Exec(`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
		created_at_ms, updated_at_ms)
		VALUES ('b1', 'Unpublished post', '', 'post', 'https://example.com/b1.jpg', 'draft', 1000, 1000)`); err != nil {
		t.Fatalf("seed draft post: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO user_notifications
		(id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, draft_action, created_at_ms)
		VALUES ('da1', 'viewer', 'draft_activity', 'actor', 'blog-posts', 'b1', NULL, 'unpublished', 2500)`); err != nil {
		t.Fatalf("seed notification: %v", err)
	}

	rec := serveNotif(mux, notifRequest("GET", "/api/v1/notifications", "moderator"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	item := body.Items[0]
	if item["id"] != "da1" || item["kind"] != "draft_activity" {
		t.Fatalf("first item: %+v", item)
	}
	if item["draftAction"] != "unpublished" {
		t.Errorf("draftAction = %v, want unpublished", item["draftAction"])
	}
	if item["contentKind"] != "blog-posts" || item["contentId"] != "b1" || item["contentTitle"] != "Unpublished post" {
		t.Errorf("content fields: %+v", item)
	}
	if item["actorUsername"] != "Actorus" {
		t.Errorf("actorUsername = %v, want the stored actor's username", item["actorUsername"])
	}
	if _, ok := item["commentId"]; ok {
		t.Errorf("draft_activity item must omit commentId, got %+v", item)
	}
	if _, ok := item["result"]; ok {
		t.Errorf("draft_activity item must omit the account-event fields, got %+v", item)
	}
}

// TestNotificationsCommentRemovedKind pins the comment-removed wire shape:
// the comment-item shape (commentId present — the removed
// comment's own id) with kind comment_removed, and an actorUsername that IS
// present but null — the notice never names the moderator, so the row
// stores a NULL actor_id.
func TestNotificationsCommentRemovedKind(t *testing.T) {
	db, mux := setupNotificationsHandlers(t)

	if _, err := db.Exec(`INSERT INTO projects (id, title, description, slug, thumbnail_url, status,
		published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('p1', 'A project', 'proj', 'a-project', 'https://example.com/p1.jpg', 'published', 2000, 1000, 1000)`); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	// actor_id is NULL — exactly what the emitter writes.
	if _, err := db.Exec(`INSERT INTO user_notifications
		(id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms)
		VALUES ('rm1', 'viewer', 'comment_removed', NULL, 'projects', 'p1', 'c9', 2500)`); err != nil {
		t.Fatalf("seed notification: %v", err)
	}

	rec := serveNotif(mux, notifRequest("GET", "/api/v1/notifications", "moderator"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	item := body.Items[0]
	if item["id"] != "rm1" || item["kind"] != "comment_removed" {
		t.Fatalf("first item: %+v", item)
	}
	if item["contentKind"] != "projects" || item["contentId"] != "p1" || item["contentTitle"] != "A project" || item["commentId"] != "c9" {
		t.Errorf("content fields: %+v", item)
	}
	// The key is present (the DTO never omits it) and null — the moderator
	// is never named.
	actor, ok := item["actorUsername"]
	if !ok || actor != nil {
		t.Errorf("comment_removed actorUsername: got %v (present %v), want a present null", actor, ok)
	}
	if _, ok := item["result"]; ok {
		t.Errorf("comment_removed item must omit the account-event fields, got %+v", item)
	}

	// The removal item marks read through the user_notifications space.
	rec = serveNotif(mux, notifRequest("POST", "/api/v1/notifications/rm1/read", "moderator"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("mark rm1: got %d, want 204", rec.Code)
	}
	var readAt int64
	if err := db.QueryRow(`SELECT read_at_ms FROM user_notifications WHERE id = 'rm1'`).Scan(&readAt); err != nil {
		t.Fatalf("read state: %v", err)
	}
	if readAt == 0 {
		t.Error("rm1 must be marked read")
	}
}

// TestNotificationsList_ReadFlag pins the read flag on the wire: every
// item carries the VIEWER's read state, and the two spaces answer it from
// different places — the audit half from the viewer's notification_reads
// mark, the event half from the row's own read_at_ms.
func TestNotificationsList_ReadFlag(t *testing.T) {
	db, mux := setupNotificationsHandlers(t)

	if _, err := db.Exec(`INSERT INTO user_notifications
		(id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms, read_at_ms)
		VALUES ('n-read', 'viewer', 'comment_reply', 'actor', 'blog-posts', 'b1', 'c1', 2500, 3000),
		       ('n-unread', 'viewer', 'comment_reply', 'actor', 'blog-posts', 'b1', 'c2', 2400, NULL)`); err != nil {
		t.Fatalf("seed notifications: %v", err)
	}
	// ev1 (1000) is read by the viewer; ev2 (2000) stays unread.
	if rec := serveNotif(mux, notifRequest("POST", "/api/v1/notifications/ev1/read", "moderator")); rec.Code != http.StatusNoContent {
		t.Fatalf("mark ev1 read: got %d, want 204", rec.Code)
	}

	rec := serveNotif(mux, notifRequest("GET", "/api/v1/notifications", "moderator"))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", rec.Code)
	}
	var page struct {
		Items []struct {
			ID   string `json:"id"`
			Read bool   `json:"read"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := map[string]bool{}
	for _, it := range page.Items {
		got[it.ID] = it.Read
	}
	for id, want := range map[string]bool{
		"ev1": true, "ev2": false, "n-read": true, "n-unread": false,
	} {
		if got[id] != want {
			t.Errorf("item %s read = %v, want %v (page %+v)", id, got[id], want, got)
		}
	}
	// The flag is always PRESENT (the client styles every row from it).
	if !strings.Contains(rec.Body.String(), `"read":false`) {
		t.Errorf("read flag must never be omitted: %s", rec.Body.String())
	}
}

// TestNotificationsDelete_Scope pins the per-item delete contract: the
// viewer's OWN user_notifications rows are deleted; a visible audit event is
// DISMISSED for the viewer (the ledger row survives); another recipient's
// event row and an unknown id are the same masked 404; a repeat delete of the
// same id is a 404 too — the entry is gone from the viewer's feed.
func TestNotificationsDelete_Scope(t *testing.T) {
	db, mux := setupNotificationsHandlers(t)

	if _, err := db.Exec(`INSERT INTO user_notifications
		(id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms, read_at_ms)
		VALUES ('n-mine', 'viewer', 'comment_reply', 'actor', 'blog-posts', 'b1', 'c1', 2500, NULL),
		       ('n-other', 'plain', 'comment_reply', 'actor', 'blog-posts', 'b1', 'c2', 2400, NULL)`); err != nil {
		t.Fatalf("seed notifications: %v", err)
	}

	if rec := serveNotif(mux, notifRequest("DELETE", "/api/v1/notifications/bad+id", "moderator")); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid shape: got %d, want 400", rec.Code)
	}
	if rec := serveNotif(mux, notifRequest("DELETE", "/api/v1/notifications/nope", "moderator")); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id: got %d, want 404", rec.Code)
	}
	// A visible audit id is dismissed for this viewer — 204, then 404 on the
	// repeat because it left the feed.
	if rec := serveNotif(mux, notifRequest("DELETE", "/api/v1/notifications/ev1", "moderator")); rec.Code != http.StatusNoContent {
		t.Fatalf("audit id: got %d, want 204", rec.Code)
	}
	if rec := serveNotif(mux, notifRequest("DELETE", "/api/v1/notifications/ev1", "moderator")); rec.Code != http.StatusNotFound {
		t.Fatalf("repeat audit delete: got %d, want 404", rec.Code)
	}
	// Another recipient's row is not this viewer's to delete.
	if rec := serveNotif(mux, notifRequest("DELETE", "/api/v1/notifications/n-other", "moderator")); rec.Code != http.StatusNotFound {
		t.Fatalf("another recipient's row: got %d, want 404", rec.Code)
	}
	if rec := serveNotif(mux, notifRequest("DELETE", "/api/v1/notifications/n-mine", "moderator")); rec.Code != http.StatusNoContent {
		t.Fatalf("own row: got %d, want 204", rec.Code)
	}
	// Repeat delete: the entry is gone, so 404 again.
	if rec := serveNotif(mux, notifRequest("DELETE", "/api/v1/notifications/n-mine", "moderator")); rec.Code != http.StatusNotFound {
		t.Fatalf("repeat delete: got %d, want 404", rec.Code)
	}

	var remaining int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE id = 'n-other'`).Scan(&remaining); err != nil {
		t.Fatalf("count other row: %v", err)
	}
	if remaining != 1 {
		t.Error("another recipient's row must survive")
	}
	var audit int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE id = 'ev1'`).Scan(&audit); err != nil {
		t.Fatalf("count audit row: %v", err)
	}
	if audit != 1 {
		t.Error("the audit ledger must be untouched")
	}
	var dismissed int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notification_reads
		WHERE user_id = 'viewer' AND event_id = 'ev1' AND dismissed_at_ms IS NOT NULL`).Scan(&dismissed); err != nil {
		t.Fatalf("count dismissal row: %v", err)
	}
	if dismissed != 1 {
		t.Error("the dismissal must be recorded for the viewer")
	}
	// The dismissed audit entry leaves the viewer's feed; its sibling stays.
	rec := serveNotif(mux, notifRequest("GET", "/api/v1/notifications", "moderator"))
	if strings.Contains(rec.Body.String(), `"ev1"`) {
		t.Errorf("the dismissed audit entry must leave the feed: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ev2"`) {
		t.Errorf("the remaining audit entry must stay: %s", rec.Body.String())
	}
}

// TestNotificationsClearRead pins the bulk counterpart:
// only the viewer's READ event rows go, unread rows always survive, other
// recipients are untouched, and the audit half of the feed keeps its rows
// (a ledger is not the viewer's inbox).
func TestNotificationsClearRead(t *testing.T) {
	db, mux := setupNotificationsHandlers(t)

	if _, err := db.Exec(`INSERT INTO user_notifications
		(id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms, read_at_ms)
		VALUES ('n-read', 'viewer', 'comment_reply', 'actor', 'blog-posts', 'b1', 'c1', 2500, 3000),
		       ('n-unread', 'viewer', 'comment_reply', 'actor', 'blog-posts', 'b1', 'c2', 2400, NULL),
		       ('n-other', 'plain', 'comment_reply', 'actor', 'blog-posts', 'b1', 'c3', 2300, 3000)`); err != nil {
		t.Fatalf("seed notifications: %v", err)
	}

	if rec := serveNotif(mux, notifRequest("POST", "/api/v1/notifications/clear-read", "moderator")); rec.Code != http.StatusNoContent {
		t.Fatalf("clear-read: got %d, want 204", rec.Code)
	}
	// Idempotent repeat.
	if rec := serveNotif(mux, notifRequest("POST", "/api/v1/notifications/clear-read", "moderator")); rec.Code != http.StatusNoContent {
		t.Fatalf("repeat clear-read: got %d, want 204", rec.Code)
	}

	for id, want := range map[string]int{"n-read": 0, "n-unread": 1, "n-other": 1} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE id = ?`, id).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", id, err)
		}
		if n != want {
			t.Errorf("row %s count = %d, want %d", id, n, want)
		}
	}
	// The audit half still lists.
	rec := serveNotif(mux, notifRequest("GET", "/api/v1/notifications", "moderator"))
	if !strings.Contains(rec.Body.String(), `"ev1"`) {
		t.Errorf("the audit ledger must stay in the feed: %s", rec.Body.String())
	}
}

// TestNotificationsDeleteAll pins the whole-feed sweep: every event row of
// the viewer goes (read and unread alike), a visible audit event is dismissed
// for the viewer, other recipients and the ledger itself are untouched, and a
// repeat call finds nothing left to do.
func TestNotificationsDeleteAll(t *testing.T) {
	db, mux := setupNotificationsHandlers(t)

	if _, err := db.Exec(`INSERT INTO user_notifications
		(id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms, read_at_ms)
		VALUES ('n-read', 'viewer', 'comment_reply', 'actor', 'blog-posts', 'b1', 'c1', 2500, 3000),
		       ('n-unread', 'viewer', 'comment_reply', 'actor', 'blog-posts', 'b1', 'c2', 2400, NULL),
		       ('n-other', 'plain', 'comment_reply', 'actor', 'blog-posts', 'b1', 'c3', 2300, NULL)`); err != nil {
		t.Fatalf("seed notifications: %v", err)
	}

	if rec := serveNotif(mux, notifRequest("POST", "/api/v1/notifications/delete-all", "moderator")); rec.Code != http.StatusNoContent {
		t.Fatalf("delete-all: got %d, want 204", rec.Code)
	}
	if rec := serveNotif(mux, notifRequest("POST", "/api/v1/notifications/delete-all", "moderator")); rec.Code != http.StatusNoContent {
		t.Fatalf("repeat delete-all: got %d, want 204", rec.Code)
	}

	for id, want := range map[string]int{"n-read": 0, "n-unread": 0, "n-other": 1} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE id = ?`, id).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", id, err)
		}
		if n != want {
			t.Errorf("row %s count = %d, want %d", id, n, want)
		}
	}
	for _, id := range []string{"ev1", "ev2"} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE id = ?`, id).Scan(&n); err != nil {
			t.Fatalf("count audit %s: %v", id, err)
		}
		if n != 1 {
			t.Errorf("audit row %s count = %d, want the ledger untouched", id, n)
		}
		var dismissed int
		if err := db.QueryRow(`SELECT COUNT(*) FROM notification_reads
			WHERE user_id = 'viewer' AND event_id = ? AND dismissed_at_ms IS NOT NULL`, id).Scan(&dismissed); err != nil {
			t.Fatalf("count dismissal %s: %v", id, err)
		}
		if dismissed != 1 {
			t.Errorf("event %s must be dismissed for the viewer", id)
		}
	}

	// The viewer's feed is empty (rows and both audit entries gone) while
	// the other recipient's row survives.
	rec := serveNotif(mux, notifRequest("GET", "/api/v1/notifications", "moderator"))
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Errorf("the viewer's feed must be empty: %s", rec.Body.String())
	}
	rec = serveNotif(mux, notifRequest("GET", "/api/v1/notifications/unread-count", "moderator"))
	if !strings.Contains(rec.Body.String(), `"unread":0`) {
		t.Errorf("the unread badge must be zero: %s", rec.Body.String())
	}
}

// TestNotificationsDeleteAll_NonStaffArm pins the floor arm: a plain user's
// delete-all empties their own rows and never consults the audit space — it
// is still the idempotent 204.
func TestNotificationsDeleteAll_NonStaffArm(t *testing.T) {
	db, mux := setupNotificationsHandlers(t)

	if _, err := db.Exec(`INSERT INTO user_notifications
		(id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms, read_at_ms)
		VALUES ('n-mine', 'viewer', 'comment_reply', 'actor', 'blog-posts', 'b1', 'c1', 2500, NULL)`); err != nil {
		t.Fatalf("seed notification: %v", err)
	}

	if rec := serveNotif(mux, notifRequest("POST", "/api/v1/notifications/delete-all", "user")); rec.Code != http.StatusNoContent {
		t.Fatalf("delete-all: got %d, want 204", rec.Code)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE recipient_id = 'viewer'`).Scan(&rows); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rows != 0 {
		t.Errorf("non-staff delete-all left %d own rows, want 0", rows)
	}
	var reads int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notification_reads WHERE user_id = 'viewer'`).Scan(&reads); err != nil {
		t.Fatalf("count read rows: %v", err)
	}
	if reads != 0 {
		t.Errorf("non-staff delete-all wrote %d audit rows, want 0", reads)
	}
}
