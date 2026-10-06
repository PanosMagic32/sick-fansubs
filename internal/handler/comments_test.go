package handler_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store/storetest"
)

// Comments handler tests. The handlers read the session
// from the request context (the favorites-harness pattern); the
// middleware chains are covered by the routes suite.

// commentListBody is the decoded wire shape used by the list assertions.
type commentListBody struct {
	Items []struct {
		ID          string `json:"id"`
		Body        string `json:"body"`
		HeartsCount int64  `json:"heartsCount"`
		Hearted     bool   `json:"hearted"`
		Author      struct {
			Username string `json:"username"`
			IsStaff  bool   `json:"isStaff"`
			// Pointer, so a test can tell "absent" (non-staff) from an
			// empty string.
			StaffRole *string `json:"staffRole"`
		} `json:"author"`
		Replies []struct {
			ID string `json:"id"`
			// Pointers, so a test can pin the reply shape's ABSENCE of the
			// window fields (top-level items only).
			ReplyCount       *int64  `json:"replyCount"`
			RepliesEndCursor *string `json:"repliesEndCursor"`
		} `json:"replies"`
		// The reply-window continuation — pointers separate "absent"
		// (a reply item, or a complete window) from zero/empty.
		ReplyCount       *int64  `json:"replyCount"`
		RepliesEndCursor *string `json:"repliesEndCursor"`
	} `json:"items"`
	PageInfo struct {
		HasNextPage bool    `json:"hasNextPage"`
		EndCursor   *string `json:"endCursor"`
	} `json:"pageInfo"`
}

// setupCommentsHandlers seeds one published post (with comments), a draft
// post, users across roles, and registers the blog + project comment
// handlers at their production paths. logger (nil → default) is wired into
// every handler so the audit assertions can capture the delete handler's
// slog record.
func setupCommentsHandlers(t *testing.T, logger *slog.Logger) (*sql.DB, http.Handler) {
	t.Helper()
	return setupCommentsHandlersWithLimiters(t, logger, commentTestLimiters())
}

// setupCommentsHandlersWithLimiters is the rate-limit seam: the same fixture with
// caller-supplied buckets, so the 429 test can pass tight ones.
func setupCommentsHandlersWithLimiters(t *testing.T, logger *slog.Logger, lim commentLimiters) (*sql.DB, http.Handler) {
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

	for _, u := range []struct{ id, name, role string }{
		{"u1", "alice", "user"},
		{"u2", "bob", "user"},
		{"mod", "moddy", "moderator"},
	} {
		storetest.InsertUser(t, db, storetest.UserSpec{
			ID:       u.id,
			Username: u.name,
			Role:     u.role,
		})
	}

	for _, q := range []string{
		`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('b1', 'B1', '', 'blog one', 'media/images/ab/b1.jpg', 'published', 2000, 1000, 1000)`,
		`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('bdraft', 'Draft', '', 'draft', 'media/images/ab/bd.jpg', 'draft', NULL, 1000, 1000)`,
		`INSERT INTO projects (id, title, description, slug, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('p1', 'P1', 'project one', 'p1', 'https://example.com/p1.jpg', 'published', 2000, 1000, 1000)`,
		`INSERT INTO blog_post_comments (id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		 VALUES ('c1', 'u1', 'b1', NULL, 'first', 0, 1000, 1000)`,
		`INSERT INTO blog_post_comments (id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		 VALUES ('c2', 'u2', 'b1', NULL, 'second', 2, 2000, 2000)`,
		`INSERT INTO blog_post_comments (id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		 VALUES ('r1', 'u2', 'b1', 'c1', 'reply one', 0, 1500, 1500)`,
		`INSERT INTO blog_post_comments (id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		 VALUES ('cdraft', 'u1', 'bdraft', NULL, 'draft comment', 0, 1000, 1000)`,
		`INSERT INTO project_comments (id, user_id, project_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		 VALUES ('pc1', 'u1', 'p1', NULL, 'project comment', 0, 1000, 1000)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	mux := http.NewServeMux()
	const publicBase = "https://fans.example"
	register := func(segment string, list, count, replies, create, reply, update, del, heartOn, heartOff http.HandlerFunc) {
		base := "/api/v1/" + segment + "/{id}/comments"
		mux.HandleFunc("GET "+base, list)
		mux.HandleFunc("GET "+base+"/count", count)
		mux.HandleFunc("GET "+base+"/{commentId}/replies", replies)
		mux.HandleFunc("POST "+base, create)
		mux.HandleFunc("POST "+base+"/{commentId}/replies", reply)
		mux.HandleFunc("PATCH "+base+"/{commentId}", update)
		mux.HandleFunc("DELETE "+base+"/{commentId}", del)
		mux.HandleFunc("PUT "+base+"/{commentId}/heart", heartOn)
		mux.HandleFunc("DELETE "+base+"/{commentId}/heart", heartOff)
	}
	register("blog-posts",
		handler.CommentsList(handler.BlogCommentsKind, db, publicBase),
		handler.CommentsCount(handler.BlogCommentsKind, db),
		handler.CommentsReplies(handler.BlogCommentsKind, db, publicBase),
		handler.CommentsCreate(handler.BlogCommentsKind, db, nil, fixedClock(5000), lim.create),
		handler.CommentsReply(handler.BlogCommentsKind, db, nil, fixedClock(5000), lim.create),
		handler.CommentsUpdate(handler.BlogCommentsKind, db, publicBase, fixedClock(9000), lim.edit),
		handler.CommentsDelete(handler.BlogCommentsKind, db, nil, fixedClock(9000), lim.del),
		handler.CommentsHeart(handler.BlogCommentsKind, db, nil, fixedClock(5000), true, lim.heart),
		handler.CommentsHeart(handler.BlogCommentsKind, db, nil, fixedClock(5000), false, lim.heart),
	)
	register("projects",
		handler.CommentsList(handler.ProjectCommentsKind, db, publicBase),
		handler.CommentsCount(handler.ProjectCommentsKind, db),
		handler.CommentsReplies(handler.ProjectCommentsKind, db, publicBase),
		handler.CommentsCreate(handler.ProjectCommentsKind, db, nil, fixedClock(5000), lim.create),
		handler.CommentsReply(handler.ProjectCommentsKind, db, nil, fixedClock(5000), lim.create),
		handler.CommentsUpdate(handler.ProjectCommentsKind, db, publicBase, fixedClock(9000), lim.edit),
		handler.CommentsDelete(handler.ProjectCommentsKind, db, nil, fixedClock(9000), lim.del),
		handler.CommentsHeart(handler.ProjectCommentsKind, db, nil, fixedClock(5000), true, lim.heart),
		handler.CommentsHeart(handler.ProjectCommentsKind, db, nil, fixedClock(5000), false, lim.heart),
	)

	return db, logContext(logger, mux)
}

// commentLimiters bundles the four comment-write buckets a test mux is
// wired with.
type commentLimiters struct {
	create, edit, heart, del *middleware.RateLimiter
}

// commentTestLimiter is a bucket generous enough that ordinary handler
// tests never trip it; the 429 test passes tight ones instead.
func commentTestLimiter() *middleware.RateLimiter {
	return middleware.NewRateLimiter(1000, time.Minute, time.Minute)
}

func commentTestLimiters() commentLimiters {
	return commentLimiters{
		create: commentTestLimiter(),
		edit:   commentTestLimiter(),
		heart:  commentTestLimiter(),
		del:    commentTestLimiter(),
	}
}

func fixedClock(ms int64) func() time.Time {
	return func() time.Time { return time.UnixMilli(ms) }
}

// commentRequest builds a request with the given role's session attached
// (or none when role is "").
func commentRequest(method, path, body, role, userID, username string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if role != "" {
		req = req.WithContext(middleware.SetSession(req.Context(), &identity.SessionUser{
			SessionID: "s1",
			UserID:    userID,
			Username:  username,
			Role:      role,
		}))
	}
	return req
}

func serveComments(mux http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeCommentList(t *testing.T, rec *httptest.ResponseRecorder) commentListBody {
	t.Helper()
	var out commentListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return out
}

// auditLogLines returns the captured slog "audit" records from a buffer.
func auditLogLines(t *testing.T, buf *bytes.Buffer) []map[string]string {
	t.Helper()
	var out []map[string]string
	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]string
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("parse log line: %v", err)
		}
		if rec["msg"] == "audit" {
			out = append(out, rec)
		}
	}
	return out
}

func TestCommentsList_DefaultTopSortWithReplies(t *testing.T) {
	t.Parallel()
	_, mux := setupCommentsHandlers(t, nil)

	rec := serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/b1/comments", "", "", "", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control: got %q, want no-store (session-dependent)", rec.Header().Get("Cache-Control"))
	}
	out := decodeCommentList(t, rec)
	// Top order: c2 (2 hearts) before c1 (0 hearts).
	if len(out.Items) != 2 || out.Items[0].ID != "c2" || out.Items[1].ID != "c1" {
		t.Fatalf("top order: %+v", out.Items)
	}
	if len(out.Items[1].Replies) != 1 || out.Items[1].Replies[0].ID != "r1" {
		t.Errorf("replies not nested: %+v", out.Items[1].Replies)
	}
	if out.Items[0].Hearted {
		t.Error("anonymous viewer must see hearted=false")
	}
	if out.Items[0].Author.IsStaff {
		t.Error("regular user must not carry the staff badge")
	}
	if out.PageInfo.HasNextPage {
		t.Error("unexpected next page")
	}
}

// seedReplyWindow creates the reply-window fixture: a published post with one parent
// carrying five replies (w1/r1..r5) and a second parent carrying one (w2/s1).
func seedReplyWindow(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
		published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('b-window', 'Window', '', 'window post', 'media/images/ab/w.jpg', 'published', 2000, 1000, 1000)`); err != nil {
		t.Fatalf("seed window post: %v", err)
	}
	insert := func(id string, parent any, body string, createdMS int64) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO blog_post_comments (id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
			VALUES (?, 'u1', 'b-window', ?, ?, 0, ?, ?)`, id, parent, body, createdMS, createdMS); err != nil {
			t.Fatalf("seed comment %s: %v", id, err)
		}
	}
	insert("w1", nil, "parent one", 1100)
	insert("w2", nil, "parent two", 1200)
	for i := 1; i <= 5; i++ {
		insert(fmt.Sprintf("wr%d", i), "w1", fmt.Sprintf("reply %d", i), int64(1100+10*i))
	}
	insert("ws1", "w2", "single reply", 1250)
}

// TestCommentsList_ReplyWindowOnWire pins the reply-window item shape: a top-level item
// carries its inline reply window, the total replyCount, and — only when the
// window is incomplete — the repliesEndCursor; reply items carry none of
// the three (no null keys either).
func TestCommentsList_ReplyWindowOnWire(t *testing.T) {
	t.Parallel()
	db, mux := setupCommentsHandlers(t, nil)
	seedReplyWindow(t, db)

	rec := serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/b-window/comments?sort=oldest", "", "", "", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	out := decodeCommentList(t, rec)
	if len(out.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(out.Items))
	}

	full := out.Items[0]
	if full.ID != "w1" {
		t.Fatalf("first item = %q, want w1", full.ID)
	}
	if full.ReplyCount == nil || *full.ReplyCount != 5 {
		t.Errorf("w1 replyCount = %v, want 5", full.ReplyCount)
	}
	if len(full.Replies) != 3 || full.Replies[0].ID != "wr1" || full.Replies[1].ID != "wr2" || full.Replies[2].ID != "wr3" {
		t.Errorf("w1 inline window = %+v, want wr1..wr3", full.Replies)
	}
	if full.RepliesEndCursor == nil {
		t.Error("w1 must carry a repliesEndCursor (5 replies, 3 inline)")
	}
	if len(full.Replies) > 0 && full.Replies[0].ReplyCount != nil {
		t.Errorf("a reply item must not carry replyCount: %v", *full.Replies[0].ReplyCount)
	}
	if len(full.Replies) > 0 && full.Replies[0].RepliesEndCursor != nil {
		t.Errorf("a reply item must not carry repliesEndCursor: %q", *full.Replies[0].RepliesEndCursor)
	}

	complete := out.Items[1]
	if complete.ID != "w2" || complete.ReplyCount == nil || *complete.ReplyCount != 1 {
		t.Errorf("w2 = %+v, want replyCount 1", complete)
	}
	if len(complete.Replies) != 1 || complete.Replies[0].ID != "ws1" {
		t.Errorf("w2 inline window = %+v, want ws1", complete.Replies)
	}
	if complete.RepliesEndCursor != nil {
		t.Errorf("w2 window is complete — no cursor, got %q", *complete.RepliesEndCursor)
	}

	// The optional keys are OMITTED, never null (the pointer convention).
	if strings.Contains(rec.Body.String(), `"repliesEndCursor":null`) {
		t.Errorf("repliesEndCursor must be omitted, not null: %s", rec.Body.String())
	}

	// A zero-reply parent still carries the key: replyCount is the depth
	// discriminator, so it cannot be omitted for a value of 0.
	if _, err := db.Exec(`INSERT INTO blog_post_comments (id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		VALUES ('w0', 'u1', 'b-window', NULL, 'no replies', 0, 1050, 1050)`); err != nil {
		t.Fatalf("seed w0: %v", err)
	}
	rec = serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/b-window/comments?sort=oldest", "", "", "", ""))
	out = decodeCommentList(t, rec)
	if len(out.Items) != 3 || out.Items[0].ID != "w0" {
		t.Fatalf("zero-reply fixture: %+v", out.Items)
	}
	if out.Items[0].ReplyCount == nil || *out.Items[0].ReplyCount != 0 {
		t.Errorf("w0 replyCount = %v, want a present 0", out.Items[0].ReplyCount)
	}
	if out.Items[0].RepliesEndCursor != nil {
		t.Errorf("w0 must have no cursor: %q", *out.Items[0].RepliesEndCursor)
	}
}

// TestCommentsRepliesEndpoint pins the operation: ascending keyset
// pages on the reply namespace, the strict parameter allowlist, the
// masked 404s, and the project mirror.
func TestCommentsRepliesEndpoint(t *testing.T) {
	t.Parallel()
	db, mux := setupCommentsHandlers(t, nil)
	seedReplyWindow(t, db)

	base := "/api/v1/blog-posts/b-window/comments/w1/replies"
	rec := serveComments(mux, commentRequest(http.MethodGet, base+"?limit=2", "", "", "", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("page 1: status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	first := decodeCommentList(t, rec)
	if len(first.Items) != 2 || first.Items[0].ID != "wr1" || first.Items[1].ID != "wr2" {
		t.Fatalf("page 1 items = %+v, want wr1,wr2", first.Items)
	}
	if !first.PageInfo.HasNextPage || first.PageInfo.EndCursor == nil {
		t.Fatalf("page 1 pageInfo = %+v, want a next page with a cursor", first.PageInfo)
	}
	if first.Items[0].ReplyCount != nil || first.Items[0].RepliesEndCursor != nil || first.Items[0].Replies != nil {
		t.Errorf("reply items must carry no reply window: %+v", first.Items[0])
	}

	rec = serveComments(mux, commentRequest(http.MethodGet, base+"?limit=2&after="+*first.PageInfo.EndCursor, "", "", "", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("page 2: status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	second := decodeCommentList(t, rec)
	if len(second.Items) != 2 || second.Items[0].ID != "wr3" || second.Items[1].ID != "wr4" {
		t.Fatalf("page 2 items = %+v, want wr3,wr4", second.Items)
	}

	// Strict params: an unknown name, a thread-only name, and a non-reply
	// cursor are all 400; an out-of-range limit is 422.
	for _, q := range []string{"?nope=1", "?sort=top", "?after=bc1.not-a-cursor", "?after=pc1.not-a-cursor"} {
		rec = serveComments(mux, commentRequest(http.MethodGet, base+q, "", "", "", ""))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s: status = %d, want 400 (body %q)", q, rec.Code, rec.Body.String())
		}
	}
	rec = serveComments(mux, commentRequest(http.MethodGet, base+"?limit=101", "", "", "", ""))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("limit 101: status = %d, want 422", rec.Code)
	}

	// Masked 404s: an unknown parent, a reply addressed as a parent, and a
	// draft post's comment.
	rec = serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/b-window/comments/nope/replies", "", "", "", ""))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown parent: status = %d, want 404", rec.Code)
	}
	rec = serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/b-window/comments/wr1/replies", "", "", "", ""))
	if rec.Code != http.StatusNotFound {
		t.Errorf("reply as parent: status = %d, want 404", rec.Code)
	}
	rec = serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/bdraft/comments/cdraft/replies", "", "", "", ""))
	if rec.Code != http.StatusNotFound {
		t.Errorf("draft content: status = %d, want 404", rec.Code)
	}

	// The project mirror is registered on the same contract.
	rec = serveComments(mux, commentRequest(http.MethodGet, "/api/v1/projects/p1/comments/pc1/replies", "", "", "", ""))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("project replies: status = %d, body %q", rec.Code, rec.Body.String())
	}
}

// TestCommentsList_FocusReplyOutsideWindow pins the focus extension at the
// wire boundary: a deep link to a reply past the inline window returns the
// page containing it, so the target is rendered (the jump must not break).
func TestCommentsList_FocusReplyOutsideWindow(t *testing.T) {
	t.Parallel()
	db, mux := setupCommentsHandlers(t, nil)
	seedReplyWindow(t, db)
	// One more reply, so the anchor sits on the second reply page (offset 3).
	if _, err := db.Exec(`INSERT INTO blog_post_comments (id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		VALUES ('wr6', 'u2', 'b-window', 'w1', 'reply 6', 0, 1200, 1200)`); err != nil {
		t.Fatalf("seed wr6: %v", err)
	}

	rec := serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/b-window/comments?sort=oldest&focus=wr5", "", "", "", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	out := decodeCommentList(t, rec)
	idx := -1
	for i := range out.Items {
		if out.Items[i].ID == "w1" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("parent w1 missing from the focus page: %+v", out.Items)
	}
	if len(out.Items[idx].Replies) != 3 || out.Items[idx].Replies[1].ID != "wr5" {
		t.Fatalf("focus window = %+v, want the page containing wr5", out.Items[idx].Replies)
	}
}

// TestCommentsList_StaffRoleOnWire pins the staff-role projection: a staff
// author's item carries `staffRole` with the derived role, and a non-staff
// author OMITS the key entirely (absent — never null, never empty).
func TestCommentsList_StaffRoleOnWire(t *testing.T) {
	t.Parallel()
	db, mux := setupCommentsHandlers(t, nil)

	// A dedicated post, so these comments never disturb the shared fixture's
	// counts. The three staff roles plus one regular author cover both wires.
	if _, err := db.Exec(`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
		published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('b-staff', 'Staff', '', 'staff post', 'media/images/ab/st.jpg', 'published', 2000, 1000, 1000)`); err != nil {
		t.Fatalf("seed post: %v", err)
	}
	for _, u := range []struct{ id, name, role string }{
		{"u-adm", "admy", "admin"},
		{"u-sadm", "sadmy", "super-admin"},
	} {
		storetest.InsertUser(t, db, storetest.UserSpec{
			ID:       u.id,
			Username: u.name,
			Role:     u.role,
		})
	}
	for i, userID := range []string{"mod", "u-adm", "u-sadm", "u1"} {
		if _, err := db.Exec(`INSERT INTO blog_post_comments (id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
			VALUES (?, ?, 'b-staff', NULL, 'body', 0, ?, ?)`,
			fmt.Sprintf("sc%d", i), userID, 1000+i, 1000+i); err != nil {
			t.Fatalf("seed comment: %v", err)
		}
	}
	// A staff-authored REPLY, so the nested projection is covered too (it
	// shares commentItemSelect/scanComment with the list).
	if _, err := db.Exec(`INSERT INTO blog_post_comments (id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		VALUES ('sr0', 'u-adm', 'b-staff', 'sc0', 'reply body', 0, 1004, 1004)`); err != nil {
		t.Fatalf("seed reply: %v", err)
	}

	rec := serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/b-staff/comments?sort=oldest", "", "", "", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	out := decodeCommentList(t, rec)
	if len(out.Items) != 4 {
		t.Fatalf("fixture: got %d items, want 4", len(out.Items))
	}
	if len(out.Items[0].Replies) != 1 || out.Items[0].Replies[0].ID != "sr0" {
		t.Fatalf("fixture: the staff reply is not nested under sc0: %+v", out.Items[0].Replies)
	}

	want := map[string]*string{
		"moddy": new("moderator"),
		"admy":  new("admin"),
		"sadmy": new("super-admin"),
		"alice": nil,
	}
	for _, item := range out.Items {
		got := item.Author.StaffRole
		expected, known := want[item.Author.Username]
		if !known {
			t.Fatalf("fixture: unexpected author %q", item.Author.Username)
		}
		switch {
		case expected == nil && got != nil:
			t.Errorf("author %q: staffRole = %q, want absent", item.Author.Username, *got)
		case expected != nil && (got == nil || *got != *expected):
			t.Errorf("author %q: staffRole = %v, want %q", item.Author.Username, got, *expected)
		}
		// isStaff and staffRole read the same derivation — they can never
		// disagree on the wire.
		if item.Author.IsStaff != (got != nil) {
			t.Errorf("author %q: isStaff=%v with staffRole=%v", item.Author.Username, item.Author.IsStaff, got)
		}
	}

	// Three top-level staff comments + the staff reply = 4 keys; the regular
	// author omits the field entirely (never an explicit null).
	body := rec.Body.String()
	if n := strings.Count(body, `"staffRole"`); n != 4 {
		t.Errorf("staffRole keys = %d, want 4 (body: %s)", n, body)
	}
	if n := strings.Count(body, `"staffRole":"admin"`); n != 2 {
		t.Errorf("admin staffRole keys = %d, want 2 (the comment and its reply): %s", n, body)
	}
	if strings.Contains(body, `"staffRole":null`) {
		t.Errorf("a non-staff author must omit staffRole entirely, got %s", body)
	}
}

func TestCommentsList_SortParamAndViewerHearted(t *testing.T) {
	t.Parallel()
	_, mux := setupCommentsHandlers(t, nil)

	rec := serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/b1/comments?sort=newest", "", "", "", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	out := decodeCommentList(t, rec)
	if out.Items[0].ID != "c2" || out.Items[1].ID != "c1" {
		t.Fatalf("newest order: %+v", out.Items)
	}

	rec = serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/b1/comments?sort=oldest", "", "", "", ""))
	out = decodeCommentList(t, rec)
	if out.Items[0].ID != "c1" || out.Items[1].ID != "c2" {
		t.Fatalf("oldest order: %+v", out.Items)
	}

	// Unknown sort → 422 {sort, invalidValue} (the enumerated-filter
	// precedent).
	rec = serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/b1/comments?sort=hot", "", "", "", ""))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown sort status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"field":"sort"`) || !strings.Contains(rec.Body.String(), `"code":"invalidValue"`) {
		t.Errorf("unknown sort violation: %s", rec.Body.String())
	}

	// Heart c1 (author u1) as u2 — hearted is viewer-aware.
	rec = serveComments(mux, commentRequest(http.MethodPut, "/api/v1/blog-posts/b1/comments/c1/heart", "", "user", "u2", "bob"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("heart status = %d, want 204", rec.Code)
	}
	rec = serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/b1/comments?sort=newest", "", "user", "u2", "bob"))
	out = decodeCommentList(t, rec)
	if !out.Items[1].Hearted || out.Items[1].HeartsCount != 1 {
		t.Errorf("u2 view of c1: hearted=%v hearts=%d, want true/1", out.Items[1].Hearted, out.Items[1].HeartsCount)
	}
	rec = serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/b1/comments?sort=newest", "", "user", "u1", "alice"))
	out = decodeCommentList(t, rec)
	if out.Items[1].Hearted {
		t.Error("u1 (the author) must not see u2's heart")
	}
}

func TestCommentsList_SortBoundCursors(t *testing.T) {
	t.Parallel()
	_, mux := setupCommentsHandlers(t, nil)

	// Page size 1 under top: the cursor encodes the sort.
	rec := serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/b1/comments?limit=1", "", "", "", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	out := decodeCommentList(t, rec)
	if !out.PageInfo.HasNextPage || out.PageInfo.EndCursor == nil {
		t.Fatalf("expected a cursor: %+v", out.PageInfo)
	}
	cursor := *out.PageInfo.EndCursor

	// The top cursor under newest → 400.
	rec = serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/b1/comments?limit=1&sort=newest&after="+cursor, "", "", "", ""))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("cross-sort cursor status = %d, want 400", rec.Code)
	}
	// The top cursor under top → next page (c1).
	rec = serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/b1/comments?limit=1&after="+cursor, "", "", "", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("same-sort cursor status = %d", rec.Code)
	}
	out = decodeCommentList(t, rec)
	if len(out.Items) != 1 || out.Items[0].ID != "c1" {
		t.Errorf("page 2 under top: %+v", out.Items)
	}

	// A time-sort cursor must round-trip even when the page's last item is
	// HEARTED (time tuples carry no hearts slot): newest,
	// limit 1 → c2 (hearts=2) → next page c1.
	rec = serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/b1/comments?limit=1&sort=newest", "", "", "", ""))
	out = decodeCommentList(t, rec)
	if !out.PageInfo.HasNextPage || out.PageInfo.EndCursor == nil {
		t.Fatalf("newest page 1: want a cursor: %+v", out.PageInfo)
	}
	if out.Items[0].ID != "c2" {
		t.Fatalf("newest page 1: want c2 (hearted), got %v", out.Items[0].ID)
	}
	rec = serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/b1/comments?limit=1&sort=newest&after="+*out.PageInfo.EndCursor, "", "", "", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("newest page 2 (hearted-last cursor): status = %d, want 200", rec.Code)
	}
	out = decodeCommentList(t, rec)
	if len(out.Items) != 1 || out.Items[0].ID != "c1" {
		t.Errorf("newest page 2: %+v", out.Items)
	}
}

func TestCommentsList_Focus(t *testing.T) {
	t.Parallel()
	_, mux := setupCommentsHandlers(t, nil)

	// Focus on a reply anchors its parent's page.
	rec := serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/b1/comments?focus=r1", "", "", "", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("focus status = %d", rec.Code)
	}
	out := decodeCommentList(t, rec)
	if len(out.Items) != 2 || out.Items[0].ID != "c2" {
		t.Fatalf("focus page: %+v", out.Items)
	}

	// A deleted/unknown anchor → first page.
	rec = serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/b1/comments?focus=gone", "", "", "", ""))
	out = decodeCommentList(t, rec)
	if len(out.Items) != 2 {
		t.Fatalf("deleted-anchor page: %+v", out.Items)
	}

	// focus + after are mutually exclusive → 400.
	rec = serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/b1/comments?focus=c1&after=x", "", "", "", ""))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("focus+after status = %d, want 400", rec.Code)
	}
	// Malformed focus id → 400.
	rec = serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/b1/comments?focus=%20", "", "", "", ""))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("malformed focus status = %d, want 400", rec.Code)
	}
}

func TestCommentsList_UnpublishedMasked(t *testing.T) {
	t.Parallel()
	_, mux := setupCommentsHandlers(t, nil)

	for _, tc := range []struct {
		name string
		path string
	}{
		{"draft content", "/api/v1/blog-posts/bdraft/comments"},
		{"draft content count", "/api/v1/blog-posts/bdraft/comments/count"},
		{"missing content", "/api/v1/blog-posts/missing/comments"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := serveComments(mux, commentRequest(http.MethodGet, tc.path, "", "", "", ""))
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s: status = %d, want 404", tc.path, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "/problems/not-found") {
				t.Errorf("%s: want the not-found problem", tc.path)
			}
		})
	}
}

func TestCommentsCount(t *testing.T) {
	t.Parallel()
	_, mux := setupCommentsHandlers(t, nil)

	rec := serveComments(mux, commentRequest(http.MethodGet, "/api/v1/blog-posts/b1/comments/count", "", "", "", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"count":3`) { // c1, c2, r1
		t.Errorf("count body: %s", rec.Body.String())
	}
}

// TestCommentsWriteRateLimits pins the rate-limit contract: every
// comment write consumes its user-keyed bucket (429 + Retry-After once hot),
// create and reply share the create bucket, heart PUT/DELETE share the heart
// bucket, and one user's exhaustion never blocks another's.
func TestCommentsWriteRateLimits(t *testing.T) {
	t.Parallel()
	_, mux := setupCommentsHandlersWithLimiters(t, nil, commentLimiters{
		create: middleware.NewRateLimiter(1, time.Minute, time.Minute),
		edit:   middleware.NewRateLimiter(1, time.Minute, time.Minute),
		heart:  middleware.NewRateLimiter(1, time.Minute, time.Minute),
		del:    middleware.NewRateLimiter(1, time.Minute, time.Minute),
	})

	assertLimited := func(t *testing.T, rec *httptest.ResponseRecorder, what string) {
		t.Helper()
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("%s: status = %d, want 429", what, rec.Code)
		}
		seconds, err := strconv.Atoi(rec.Header().Get("Retry-After"))
		if err != nil || seconds < 1 {
			t.Errorf("%s: Retry-After = %q, want an integer >= 1", what, rec.Header().Get("Retry-After"))
		}
	}

	// An INVALID body is rejected before the bucket is touched: with a
	// one-write bucket the 422 must not cost u1 their only write.
	if rec := serveComments(mux, commentRequest("POST", "/api/v1/blog-posts/b1/comments", `{"body":""}`, "user", "u1", "alice")); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid create: status = %d, want 422", rec.Code)
	}
	// create: u1's first valid write lands, the second is limited.
	if rec := serveComments(mux, commentRequest("POST", "/api/v1/blog-posts/b1/comments", `{"body":"γεια"}`, "user", "u1", "alice")); rec.Code != http.StatusCreated {
		t.Fatalf("create u1: status = %d, want 201", rec.Code)
	}
	assertLimited(t, serveComments(mux, commentRequest("POST", "/api/v1/blog-posts/b1/comments", `{"body":"ξανά"}`, "user", "u1", "alice")), "create u1 second")
	// ONE bucket spans both content kinds: u1's projects write is limited too.
	assertLimited(t, serveComments(mux, commentRequest("POST", "/api/v1/projects/p1/comments", `{"body":"έργο"}`, "user", "u1", "alice")), "project create u1 shares the bucket")

	// u2 has a fresh bucket; a reply consumes the SAME (create) bucket.
	if rec := serveComments(mux, commentRequest("POST", "/api/v1/blog-posts/b1/comments", `{"body":"του μπομπ"}`, "user", "u2", "bob")); rec.Code != http.StatusCreated {
		t.Fatalf("create u2: status = %d, want 201", rec.Code)
	}
	assertLimited(t, serveComments(mux, commentRequest("POST", "/api/v1/blog-posts/b1/comments/c1/replies", `{"body":"απάντηση"}`, "user", "u2", "bob")), "reply u2 shares the create bucket")

	// edit: an independent bucket.
	if rec := serveComments(mux, commentRequest("PATCH", "/api/v1/blog-posts/b1/comments/c1", `{"body":"άλλα"}`, "user", "u1", "alice")); rec.Code != http.StatusOK {
		t.Fatalf("edit u1: status = %d, want 200", rec.Code)
	}
	assertLimited(t, serveComments(mux, commentRequest("PATCH", "/api/v1/blog-posts/b1/comments/c1", `{"body":"ξανά"}`, "user", "u1", "alice")), "edit u1 second")

	// heart: PUT and DELETE share one bucket.
	if rec := serveComments(mux, commentRequest("PUT", "/api/v1/blog-posts/b1/comments/c1/heart", "", "user", "u2", "bob")); rec.Code != http.StatusNoContent {
		t.Fatalf("heart u2: status = %d, want 204", rec.Code)
	}
	assertLimited(t, serveComments(mux, commentRequest("DELETE", "/api/v1/blog-posts/b1/comments/c1/heart", "", "user", "u2", "bob")), "unheart u2 shares the heart bucket")

	// delete: an independent bucket.
	if rec := serveComments(mux, commentRequest("DELETE", "/api/v1/blog-posts/b1/comments/c1", "", "user", "u1", "alice")); rec.Code != http.StatusNoContent {
		t.Fatalf("delete u1: status = %d, want 204", rec.Code)
	}
	assertLimited(t, serveComments(mux, commentRequest("DELETE", "/api/v1/blog-posts/b1/comments/cdraft", "", "user", "u1", "alice")), "delete u1 second")
}

func TestCommentsCreate(t *testing.T) {
	t.Parallel()
	db, mux := setupCommentsHandlers(t, nil)

	// 201 + Location with ?focus=<id>; the CRLF body is normalized and
	// trimmed before storage.
	rec := serveComments(mux, commentRequest(http.MethodPost, "/api/v1/blog-posts/b1/comments", `{"body":"  hello\r\nworld  "}`, "user", "u1", "alice"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/api/v1/blog-posts/b1/comments?focus=") {
		t.Errorf("Location: %q", loc)
	}
	focusID := strings.TrimPrefix(loc, "/api/v1/blog-posts/b1/comments?focus=")
	var stored string
	if err := db.QueryRow(`SELECT body FROM blog_post_comments WHERE id = ?`, focusID).Scan(&stored); err != nil {
		t.Fatalf("read created comment: %v", err)
	}
	if stored != "hello\nworld" {
		t.Errorf("stored body: %q, want CRLF-normalized trimmed", stored)
	}

	// 401 without a session.
	rec = serveComments(mux, commentRequest(http.MethodPost, "/api/v1/blog-posts/b1/comments", `{"body":"x"}`, "", "", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous create status = %d, want 401", rec.Code)
	}

	// 422s: required / maxLength / invalidFormat.
	for _, tc := range []struct {
		name, body, code string
	}{
		{"whitespace body is required", `{"body":"   "}`, "required"},
		{"over-long body", `{"body":"` + strings.Repeat("α", 5001) + `"}`, "maxLength"},
		{"control characters rejected", `{"body":"a\u0000b"}`, "invalidFormat"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := serveComments(mux, commentRequest(http.MethodPost, "/api/v1/blog-posts/b1/comments", tc.body, "user", "u1", "alice"))
			if rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("body %q: status = %d, want 422", tc.code, rec.Code)
				return
			}
			if !strings.Contains(rec.Body.String(), `"field":"body"`) || !strings.Contains(rec.Body.String(), `"code":"`+tc.code+`"`) {
				t.Errorf("body %q: violation %s", tc.code, rec.Body.String())
			}
		})
	}

	// Unknown field → 400; wrong media type → 415.
	rec = serveComments(mux, commentRequest(http.MethodPost, "/api/v1/blog-posts/b1/comments", `{"body":"x","extra":1}`, "user", "u1", "alice"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field status = %d, want 400", rec.Code)
	}
	req := commentRequest(http.MethodPost, "/api/v1/blog-posts/b1/comments", `{"body":"x"}`, "user", "u1", "alice")
	req.Header.Set("Content-Type", "text/plain")
	rec = serveComments(mux, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("media type status = %d, want 415", rec.Code)
	}

	// Draft content → masked 404 (the published gate).
	rec = serveComments(mux, commentRequest(http.MethodPost, "/api/v1/blog-posts/bdraft/comments", `{"body":"x"}`, "user", "u1", "alice"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("draft create status = %d, want 404", rec.Code)
	}
}

func TestCommentsCreate_BodyBound(t *testing.T) {
	t.Parallel()
	_, mux := setupCommentsHandlers(t, nil)

	// > 32 KiB of JSON body → 413 before decoding.
	body := `{"body":"` + strings.Repeat("a", 40*1024) + `"}`
	rec := serveComments(mux, commentRequest(http.MethodPost, "/api/v1/blog-posts/b1/comments", body, "user", "u1", "alice"))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body status = %d, want 413", rec.Code)
	}

	// Exactly at the 32 KiB bound is accepted.
	base := `{"body":"hello"}`
	atLimit := base + strings.Repeat(" ", 32*1024-len(base))
	rec = serveComments(mux, commentRequest(http.MethodPost, "/api/v1/blog-posts/b1/comments", atLimit, "user", "u1", "alice"))
	if rec.Code != http.StatusCreated {
		t.Errorf("at-limit body status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestCommentsReply(t *testing.T) {
	t.Parallel()
	db, mux := setupCommentsHandlers(t, nil)

	// u2 replies to u1's comment c1 — 201 + Location + a notification row
	// for u1.
	rec := serveComments(mux, commentRequest(http.MethodPost, "/api/v1/blog-posts/b1/comments/c1/replies", `{"body":"nice"}`, "user", "u2", "bob"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/api/v1/blog-posts/b1/comments?focus=") {
		t.Errorf("Location: %q", loc)
	}
	var kind, recipient, actor, contentKind, contentID, commentID string
	if err := db.QueryRow(`SELECT kind, recipient_id, actor_id, content_kind, content_id, comment_id FROM user_notifications`).
		Scan(&kind, &recipient, &actor, &contentKind, &contentID, &commentID); err != nil {
		t.Fatalf("read notification: %v", err)
	}
	if kind != "comment_reply" || recipient != "u1" || actor != "u2" || contentKind != "blog-posts" || contentID != "b1" {
		t.Errorf("notification: kind=%s recipient=%s actor=%s contentKind=%s contentID=%s", kind, recipient, actor, contentKind, contentID)
	}
	if commentID != strings.TrimPrefix(loc, "/api/v1/blog-posts/b1/comments?focus=") {
		t.Errorf("notification comment_id %q should be the Location focus id", commentID)
	}

	// Self-reply → no notification.
	if _, err := db.Exec(`DELETE FROM user_notifications`); err != nil {
		t.Fatalf("clear notifications: %v", err)
	}
	rec = serveComments(mux, commentRequest(http.MethodPost, "/api/v1/blog-posts/b1/comments/c1/replies", `{"body":"self"}`, "user", "u1", "alice"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("self reply status = %d", rec.Code)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications`).Scan(&n); err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	if n != 0 {
		t.Error("self-reply must not emit a notification")
	}

	// Reply-to-reply → 422 {parentId, invalidValue}.
	rec = serveComments(mux, commentRequest(http.MethodPost, "/api/v1/blog-posts/b1/comments/r1/replies", `{"body":"deep"}`, "user", "u2", "bob"))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("reply-to-reply status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"field":"parentId"`) || !strings.Contains(rec.Body.String(), `"code":"invalidValue"`) {
		t.Errorf("reply-to-reply violation: %s", rec.Body.String())
	}

	// Unknown parent → masked 404.
	rec = serveComments(mux, commentRequest(http.MethodPost, "/api/v1/blog-posts/b1/comments/gone/replies", `{"body":"x"}`, "user", "u2", "bob"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown parent status = %d, want 404", rec.Code)
	}

	// Reply on draft content → masked 404 (the published gate).
	rec = serveComments(mux, commentRequest(http.MethodPost, "/api/v1/blog-posts/bdraft/comments/cdraft/replies", `{"body":"x"}`, "user", "u2", "bob"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("draft reply status = %d, want 404", rec.Code)
	}
}

func TestCommentsUpdate(t *testing.T) {
	t.Parallel()
	_, mux := setupCommentsHandlers(t, nil)

	// Author edit → 200 with the updated item (updatedAt > createdAt).
	rec := serveComments(mux, commentRequest(http.MethodPatch, "/api/v1/blog-posts/b1/comments/c1", `{"body":"edited"}`, "user", "u1", "alice"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var item struct {
		Body      string `json:"body"`
		UpdatedAt string `json:"updatedAt"`
		Replies   []any  `json:"replies"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &item); err != nil {
		t.Fatalf("decode item: %v", err)
	}
	if item.Body != "edited" {
		t.Errorf("updated item: %+v", item)
	}
	if item.Replies == nil {
		t.Error("a patched TOP-LEVEL comment must carry the replies key")
	}
	if !strings.HasPrefix(item.UpdatedAt, "1970-01-01T00:00:09.000") { // fixed clock 9000
		t.Errorf("updatedAt: %q", item.UpdatedAt)
	}

	// Patching a REPLY omits the replies key entirely ("replies = same
	// minus replies").
	rec = serveComments(mux, commentRequest(http.MethodPatch, "/api/v1/blog-posts/b1/comments/r1", `{"body":"edited reply"}`, "user", "u2", "bob"))
	if rec.Code != http.StatusOK {
		t.Fatalf("reply patch status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `"replies"`) {
		t.Errorf("a patched reply must omit the replies key: %s", rec.Body.String())
	}

	// Non-author → 403.
	rec = serveComments(mux, commentRequest(http.MethodPatch, "/api/v1/blog-posts/b1/comments/c1", `{"body":"mine"}`, "user", "u2", "bob"))
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-author status = %d, want 403", rec.Code)
	}

	// Unknown comment → masked 404.
	rec = serveComments(mux, commentRequest(http.MethodPatch, "/api/v1/blog-posts/b1/comments/gone", `{"body":"x"}`, "user", "u1", "alice"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown status = %d, want 404", rec.Code)
	}
}

func TestCommentsDelete(t *testing.T) {
	t.Parallel()
	db, mux := setupCommentsHandlers(t, nil)

	// Author self-delete → 204.
	rec := serveComments(mux, commentRequest(http.MethodDelete, "/api/v1/blog-posts/b1/comments/c1", "", "user", "u1", "alice"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("self-delete status = %d, want 204", rec.Code)
	}

	// A non-author regular user → 403.
	rec = serveComments(mux, commentRequest(http.MethodDelete, "/api/v1/blog-posts/b1/comments/c2", "", "user", "u1", "alice"))
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-author delete status = %d, want 403", rec.Code)
	}

	// Unknown → 404.
	rec = serveComments(mux, commentRequest(http.MethodDelete, "/api/v1/blog-posts/b1/comments/gone", "", "user", "u1", "alice"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown delete status = %d, want 404", rec.Code)
	}

	// A non-author regular user on DRAFT content → the content detail's
	// masked 404, never a 403 that would confirm the comment exists on a
	// post the caller cannot read.
	rec = serveComments(mux, commentRequest(http.MethodDelete, "/api/v1/blog-posts/bdraft/comments/cdraft", "", "user", "u2", "bob"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("draft non-author delete status = %d, want 404 (masked)", rec.Code)
	}

	// The published gate does NOT apply to DELETE for the author: the author
	// deletes their own comment on DRAFT content (moderation must work on
	// archived/draft content).
	rec = serveComments(mux, commentRequest(http.MethodDelete, "/api/v1/blog-posts/bdraft/comments/cdraft", "", "user", "u1", "alice"))
	if rec.Code != http.StatusNoContent {
		t.Errorf("draft delete status = %d, want 204 (gate exemption)", rec.Code)
	}

	// The staff floor keeps its path too: a moderator deletes another user's
	// comment on draft content.
	seedComment(t, db, "cdraft2", "u1", "bdraft", nil)
	rec = serveComments(mux, commentRequest(http.MethodDelete, "/api/v1/blog-posts/bdraft/comments/cdraft2", "", "moderator", "u9", "mod"))
	if rec.Code != http.StatusNoContent {
		t.Errorf("draft moderator delete status = %d, want 204 (gate exemption)", rec.Code)
	}

	// Unauthenticated → 401.
	rec = serveComments(mux, commentRequest(http.MethodDelete, "/api/v1/blog-posts/b1/comments/c2", "", "", "", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous delete status = %d, want 401", rec.Code)
	}
}

func TestCommentsDelete_StaffAudit(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, mux := setupCommentsHandlers(t, logger)

	seedComment(t, db, "c-staff", "u1", "b1", nil)

	// A moderator deleting ANOTHER user's comment → 204 + the
	// comment_deleted audit event (actor + author target).
	rec := serveComments(mux, commentRequest(http.MethodDelete, "/api/v1/blog-posts/b1/comments/c-staff", "", "moderator", "mod", "moddy"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("staff delete status = %d, want 204", rec.Code)
	}
	lines := auditLogLines(t, &buf)
	if len(lines) != 1 {
		t.Fatalf("audit lines: got %d, want 1", len(lines))
	}
	if lines[0]["event"] != "comment_deleted" || lines[0]["result"] != "success" {
		t.Errorf("audit line: %v", lines[0])
	}
	if lines[0]["actorId"] != "mod" || lines[0]["targetId"] != "u1" {
		t.Errorf("audit actor/target: %v", lines[0])
	}
	if _, has := lines[0]["targetRole"]; has {
		t.Errorf("comment_deleted carries no target role snapshot: %v", lines[0])
	}

	// A staff member deleting their OWN comment emits no audit.
	seedComment(t, db, "c-own", "mod", "b1", nil)
	buf.Reset()
	rec = serveComments(mux, commentRequest(http.MethodDelete, "/api/v1/blog-posts/b1/comments/c-own", "", "moderator", "mod", "moddy"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("staff self-delete status = %d, want 204", rec.Code)
	}
	if lines := auditLogLines(t, &buf); len(lines) != 0 {
		t.Errorf("staff self-delete must not audit, got %v", lines)
	}
	// A staff delete that FAILS (unknown comment) emits no audit line — the
	// success-only contract's negative arm.
	buf.Reset()
	rec = serveComments(mux, commentRequest(http.MethodDelete, "/api/v1/blog-posts/b1/comments/missing", "", "moderator", "mod", "moddy"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown staff delete status = %d, want 404", rec.Code)
	}
	if lines := auditLogLines(t, &buf); len(lines) != 0 {
		t.Errorf("failed staff delete must not audit, got %v", lines)
	}
}

func TestCommentsHeart(t *testing.T) {
	t.Parallel()
	db, mux := setupCommentsHandlers(t, nil)

	// PUT heart → 204 + one heart notification to the author (u2); repeat
	// → 204 (idempotent, no second row); DELETE → 204 and the notification
	// is removed in the same transaction.
	rec := serveComments(mux, commentRequest(http.MethodPut, "/api/v1/blog-posts/b1/comments/c2/heart", "", "user", "u1", "alice"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("heart status = %d, want 204", rec.Code)
	}
	var kind, recipient, actor string
	if err := db.QueryRow(`SELECT kind, recipient_id, actor_id FROM user_notifications`).
		Scan(&kind, &recipient, &actor); err != nil {
		t.Fatalf("read heart notification: %v", err)
	}
	if kind != "heart" || recipient != "u2" || actor != "u1" {
		t.Errorf("heart notification: kind=%s recipient=%s actor=%s", kind, recipient, actor)
	}
	rec = serveComments(mux, commentRequest(http.MethodPut, "/api/v1/blog-posts/b1/comments/c2/heart", "", "user", "u1", "alice"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("repeat heart status = %d, want 204", rec.Code)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications`).Scan(&n); err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	if n != 1 {
		t.Errorf("notification rows after repeat heart: got %d, want 1", n)
	}
	rec = serveComments(mux, commentRequest(http.MethodDelete, "/api/v1/blog-posts/b1/comments/c2/heart", "", "user", "u1", "alice"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unheart status = %d, want 204", rec.Code)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications`).Scan(&n); err != nil {
		t.Fatalf("count notifications after unheart: %v", err)
	}
	if n != 0 {
		t.Errorf("notification rows after unheart: got %d, want 0", n)
	}

	// Self-heart → 422 {commentId, invalidValue} — c1 is authored by u1.
	rec = serveComments(mux, commentRequest(http.MethodPut, "/api/v1/blog-posts/b1/comments/c1/heart", "", "user", "u1", "alice"))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("self-heart status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"field":"commentId"`) || !strings.Contains(rec.Body.String(), `"code":"invalidValue"`) {
		t.Errorf("self-heart violation: %s", rec.Body.String())
	}

	// DELETE on an own comment stays an idempotent 204 (the revision's
	// ungated path — it cleans any pre-revision self-heart row).
	rec = serveComments(mux, commentRequest(http.MethodDelete, "/api/v1/blog-posts/b1/comments/c1/heart", "", "user", "u1", "alice"))
	if rec.Code != http.StatusNoContent {
		t.Errorf("own unheart status = %d, want 204", rec.Code)
	}

	// Unauthenticated → 401; unknown comment → 404; draft content → 404.
	rec = serveComments(mux, commentRequest(http.MethodPut, "/api/v1/blog-posts/b1/comments/c2/heart", "", "", "", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous heart status = %d, want 401", rec.Code)
	}
	rec = serveComments(mux, commentRequest(http.MethodPut, "/api/v1/blog-posts/b1/comments/gone/heart", "", "user", "u1", "alice"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown heart status = %d, want 404", rec.Code)
	}
	rec = serveComments(mux, commentRequest(http.MethodPut, "/api/v1/blog-posts/bdraft/comments/cdraft/heart", "", "user", "u1", "alice"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("draft heart status = %d, want 404", rec.Code)
	}
	// The gate applies to BOTH directions: an unheart attempt on unpublished
	// content is the same masked 404 (the store test pins the freeze of a
	// real heart).
	rec = serveComments(mux, commentRequest(http.MethodDelete, "/api/v1/blog-posts/bdraft/comments/cdraft/heart", "", "user", "u1", "alice"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("draft unheart status = %d, want 404 (the gate applies to both directions)", rec.Code)
	}
}

func TestComments_ProjectMirror(t *testing.T) {
	t.Parallel()
	_, mux := setupCommentsHandlers(t, nil)

	rec := serveComments(mux, commentRequest(http.MethodGet, "/api/v1/projects/p1/comments", "", "", "", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("project list status = %d", rec.Code)
	}
	out := decodeCommentList(t, rec)
	if len(out.Items) != 1 || out.Items[0].ID != "pc1" {
		t.Fatalf("project list: %+v", out.Items)
	}
	rec = serveComments(mux, commentRequest(http.MethodGet, "/api/v1/projects/p1/comments/count", "", "", "", ""))
	if !strings.Contains(rec.Body.String(), `"count":1`) {
		t.Errorf("project count: %s", rec.Body.String())
	}
}

func seedComment(t *testing.T, db *sql.DB, id, userID, contentID string, parentID *string) {
	t.Helper()
	parent := "NULL"
	if parentID != nil {
		parent = fmt.Sprintf("'%s'", *parentID)
	}
	if _, err := db.Exec(fmt.Sprintf(
		`INSERT INTO blog_post_comments (id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		 VALUES (?, ?, ?, %s, 'body', 0, 3000, 3000)`, parent),
		id, userID, contentID); err != nil {
		t.Fatalf("seed comment %s: %v", id, err)
	}
}
