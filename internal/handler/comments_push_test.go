package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/push"
	"sick-fansubs/internal/store/storetest"
)

// The reply/heart → push seam: the handlers hand
// the committed notification event to the notifier ONLY when a row landed.

type recordingNotifier struct {
	mu     sync.Mutex
	events []push.Event
}

func (n *recordingNotifier) NotifyPush(_ context.Context, ev push.Event) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, ev)
}

func (n *recordingNotifier) snapshot() []push.Event {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]push.Event, len(n.events))
	copy(out, n.events)
	return out
}

// clear drops the recorded events so one notifier can serve several phases
// of one test (the publish-transition matrix).
func (n *recordingNotifier) clear() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = nil
}

func setupCommentsPushSeam(t *testing.T, notify push.Notifier) (*http.ServeMux, func(method, path, body, userID, username string) int, func(role, method, path, body, userID, username string) int) {
	t.Helper()
	db, err := database.Open(database.Config{DataDir: testDataDir(t)})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := database.Apply(db); err != nil {
		db.Close()
		t.Fatalf("apply migrations: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	for _, u := range []struct{ id, name, role string }{
		{"u1", "alice", "user"}, {"u2", "bob", "user"}, {"u3", "carol", "user"},
		{"mod", "moddy", "moderator"},
	} {
		storetest.InsertUser(t, db, storetest.UserSpec{
			ID: u.id, Username: u.name, Role: u.role,
		})
	}
	for _, q := range []string{
		`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('b1', 'Ένα Post', '', '', 'media/images/ab/b1.jpg', 'published', 2000, 1000, 1000)`,
		`INSERT INTO blog_post_comments (id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		 VALUES ('c1', 'u1', 'b1', NULL, 'first', 0, 1000, 1000)`,
		`INSERT INTO blog_post_comments (id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		 VALUES ('c2', 'u2', 'b1', NULL, 'δεύτερο', 0, 1100, 1100)`,
		`INSERT INTO blog_post_comments (id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		 VALUES ('c3', 'mod', 'b1', NULL, 'του συντονιστή', 0, 1150, 1150)`,
		`INSERT INTO content_follows (user_id, content_kind, content_id, created_at_ms)
		 VALUES ('u3', 'blog-posts', 'b1', 1500)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	mux := http.NewServeMux()
	now := func() time.Time { return time.Unix(5, 0) }
	limiter := commentTestLimiter()
	mux.HandleFunc("POST /api/v1/blog-posts/{id}/comments",
		handler.CommentsCreate(handler.BlogCommentsKind, db, notify, now, limiter))
	mux.HandleFunc("POST /api/v1/blog-posts/{id}/comments/{commentId}/replies",
		handler.CommentsReply(handler.BlogCommentsKind, db, notify, now, limiter))
	mux.HandleFunc("PUT /api/v1/blog-posts/{id}/comments/{commentId}/heart",
		handler.CommentsHeart(handler.BlogCommentsKind, db, notify, now, true, limiter))
	mux.HandleFunc("DELETE /api/v1/blog-posts/{id}/comments/{commentId}",
		handler.CommentsDelete(handler.BlogCommentsKind, db, notify, now, limiter))

	serveRole := func(role, method, path, body, userID, username string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if userID != "" {
			req = req.WithContext(middleware.SetSession(req.Context(), &identity.SessionUser{
				SessionID: "s1", UserID: userID, Username: username, Role: role,
			}))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	serve := func(method, path, body, userID, username string) int {
		return serveRole("user", method, path, body, userID, username)
	}
	return mux, serve, serveRole
}

func TestCommentsReplyNotifiesOnlyOnEmission(t *testing.T) {
	t.Parallel()
	notify := &recordingNotifier{}
	_, serve, _ := setupCommentsPushSeam(t, notify)

	// u2 replies to u1's comment → one event, full payload.
	if code := serve("POST", "/api/v1/blog-posts/b1/comments/c1/replies", `{"body":"γεια"}`, "u2", "bob"); code != http.StatusCreated {
		t.Fatalf("reply: status = %d, want 201", code)
	}
	events := notify.snapshot()
	if len(events) != 1 {
		t.Fatalf("reply events: got %d, want 1", len(events))
	}
	want := push.Event{
		RecipientID:   "u1",
		Kind:          "comment_reply",
		ActorUsername: "bob",
		ContentTitle:  "Ένα Post",
		ContentKind:   "blog-posts",
		ContentID:     "b1",
	}
	if events[0].RecipientID != want.RecipientID || events[0].Kind != want.Kind ||
		events[0].ActorUsername != want.ActorUsername || events[0].ContentTitle != want.ContentTitle ||
		events[0].ContentKind != want.ContentKind || events[0].ContentID != want.ContentID ||
		events[0].CommentID == "" {
		t.Errorf("reply event: got %+v, want %+v with a comment id", events[0], want)
	}
}

func TestCommentsHeartNotifiesOnlyOnFreshHeart(t *testing.T) {
	t.Parallel()
	notify := &recordingNotifier{}
	_, serve, _ := setupCommentsPushSeam(t, notify)

	// u2 hearts u1's comment → one heart event.
	if code := serve("PUT", "/api/v1/blog-posts/b1/comments/c1/heart", "", "u2", "bob"); code != http.StatusNoContent {
		t.Fatalf("heart: status = %d, want 204", code)
	}
	events := notify.snapshot()
	if len(events) != 1 {
		t.Fatalf("heart events: got %d, want 1", len(events))
	}
	if events[0].Kind != "heart" || events[0].RecipientID != "u1" || events[0].CommentID != "c1" {
		t.Errorf("heart event: got %+v, want kind=heart recipient=u1 comment=c1", events[0])
	}

	// A repeat heart is idempotent — no second event.
	if code := serve("PUT", "/api/v1/blog-posts/b1/comments/c1/heart", "", "u2", "bob"); code != http.StatusNoContent {
		t.Fatalf("repeat heart: status = %d, want 204", code)
	}
	if events := notify.snapshot(); len(events) != 1 {
		t.Errorf("repeat heart events: got %d, want still 1", len(events))
	}

	// A self-heart is rejected — 422 and no event.
	if code := serve("PUT", "/api/v1/blog-posts/b1/comments/c1/heart", "", "u1", "alice"); code != http.StatusUnprocessableEntity {
		t.Fatalf("self-heart: status = %d, want 422", code)
	}
	if events := notify.snapshot(); len(events) != 1 {
		t.Errorf("self-heart events: got %d, want still 1", len(events))
	}
}

// TestCommentsCreateNotifiesFollowers pins the 'comment' fan-out seam:
// a top-level comment hands ONE event
// per follower (u3 follows b1; u1 and the author u2 do not).
func TestCommentsCreateNotifiesFollowers(t *testing.T) {
	t.Parallel()
	notify := &recordingNotifier{}
	_, serve, _ := setupCommentsPushSeam(t, notify)

	if code := serve("POST", "/api/v1/blog-posts/b1/comments", `{"body":"νέο σχόλιο"}`, "u2", "bob"); code != http.StatusCreated {
		t.Fatalf("create: status = %d, want 201", code)
	}
	events := notify.snapshot()
	if len(events) != 1 {
		t.Fatalf("create events: got %d, want 1 (only u3 follows)", len(events))
	}
	ev := events[0]
	if ev.RecipientID != "u3" || ev.Kind != "comment" || ev.ActorUsername != "bob" ||
		ev.ContentKind != "blog-posts" || ev.ContentID != "b1" || ev.ContentTitle != "Ένα Post" ||
		ev.CommentID == "" {
		t.Errorf("create event: got %+v", ev)
	}
}

// TestCommentsDeleteNotifiesAuthorActorless pins the moderation-notifier
// contract at the push seam: a moderator's delete of someone else's comment hands
// ONE comment_removed event for that author, WITHOUT an actor username (the
// notice is actorless by ruling); a self-delete emits nothing.
func TestCommentsDeleteNotifiesAuthorActorless(t *testing.T) {
	t.Parallel()
	notify := &recordingNotifier{}
	_, serve, serveRole := setupCommentsPushSeam(t, notify)

	if code := serveRole("moderator", "DELETE", "/api/v1/blog-posts/b1/comments/c1", "", "mod", "moddy"); code != http.StatusNoContent {
		t.Fatalf("moderator delete: status = %d, want 204", code)
	}
	events := notify.snapshot()
	if len(events) != 1 {
		t.Fatalf("delete events: got %d, want 1", len(events))
	}
	ev := events[0]
	if ev.RecipientID != "u1" || ev.Kind != "comment_removed" || ev.ActorUsername != "" ||
		ev.ContentKind != "blog-posts" || ev.ContentID != "b1" || ev.ContentTitle != "Ένα Post" ||
		ev.CommentID != "c1" {
		t.Errorf("removal event: got %+v (actorUsername MUST stay empty)", ev)
	}

	// The author deleting their own comment notifies no one (c2 is u2's),
	// and staff self-deletes suppress too (c3 is the moderator's own).
	notify.clear()
	if code := serve("DELETE", "/api/v1/blog-posts/b1/comments/c2", "", "u2", "bob"); code != http.StatusNoContent {
		t.Fatalf("self delete: status = %d, want 204", code)
	}
	if code := serveRole("moderator", "DELETE", "/api/v1/blog-posts/b1/comments/c3", "", "mod", "moddy"); code != http.StatusNoContent {
		t.Fatalf("staff self delete: status = %d, want 204", code)
	}
	if events := notify.snapshot(); len(events) != 0 {
		t.Errorf("self-delete events: got %+v, want none", events)
	}
}
