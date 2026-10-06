package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"

	"sick-fansubs/internal/identity"
)

// commentFixture seeds one published blog post, three users, and comments
// with deliberately interleaved hearts/timestamps so every sort produces a
// distinct order.
func commentFixture(t *testing.T) (*sql.DB, string) {
	t.Helper()
	db := openStoreDB(t)
	mustCreateUser(t, db, "u1", "alice", "alice", "alice@example.com")
	mustCreateUser(t, db, "u2", "bob", "bob", "bob@example.com")
	mustCreateUser(t, db, "u3", "carol", "carol", "carol@example.com")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "p1", Title: "Post", ThumbnailURL: "/media/images/x.jpg", PublishedAtMS: 3000}, "published")

	// created asc: c1(1000), c2(2000), c3(3000)
	// hearts:      c1=0,      c2=2,      c3=1
	mustCreateComment(t, db, BlogContent, "c1", "u1", "p1", nil, "first", 0, 1000, 1000)
	mustCreateComment(t, db, BlogContent, "c2", "u2", "p1", nil, "second", 2, 2000, 2000)
	mustCreateComment(t, db, BlogContent, "c3", "u3", "p1", nil, "third", 1, 3000, 3000)
	return db, "p1"
}

func idsOf(items []Comment) []string {
	ids := make([]string, len(items))
	for i, c := range items {
		ids[i] = c.ID
	}
	return ids
}

func assertIDs(t *testing.T, got []Comment, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("item count: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Errorf("item %d: got %q, want %q", i, got[i].ID, want[i])
		}
	}
}

func assertIDSeq(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("id count: got %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("id %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCommentListSorts(t *testing.T) {
	t.Parallel()
	db, p := commentFixture(t)
	ctx := t.Context()

	cases := []struct {
		name string
		sort CommentSort
		want []string
	}{
		{"top orders by hearts then recency", CommentSortTop, []string{"c2", "c3", "c1"}},
		{"newest orders by created desc", CommentSortNewest, []string{"c3", "c2", "c1"}},
		{"oldest orders by created asc", CommentSortOldest, []string{"c1", "c2", "c3"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			items, hasNext, err := ListComments(ctx, db, BlogContent, p, nil, tc.sort, 10, nil)
			if err != nil {
				t.Fatalf("list %s: %v", tc.sort, err)
			}
			if hasNext {
				t.Errorf("list %s: unexpected next page", tc.sort)
			}
			assertIDs(t, items, tc.want...)
		})
	}
}

func TestCommentListKeysetPagesWithoutDuplicates(t *testing.T) {
	t.Parallel()
	db, p := commentFixture(t)
	ctx := t.Context()

	// Newest order, limit 1: three pages c3 → c2 → c1, each with a cursor.
	var all []string
	var after *CommentPageKey
	for i := range 4 {
		items, hasNext, err := ListComments(ctx, db, BlogContent, p, nil, CommentSortNewest, 1, after)
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		for _, c := range items {
			all = append(all, c.ID)
		}
		if !hasNext {
			break
		}
		last := items[len(items)-1]
		after = &CommentPageKey{CreatedAtMS: last.CreatedAtMS, ID: last.ID}
	}
	assertIDSeq(t, all, "c3", "c2", "c1")

	// Top sort, limit 1: c2 → c3 → c1 — the hearts slot rides the cursor.
	after = nil
	var topAll []string
	for i := range 4 {
		items, hasNext, err := ListComments(ctx, db, BlogContent, p, nil, CommentSortTop, 1, after)
		if err != nil {
			t.Fatalf("top page %d: %v", i, err)
		}
		for _, c := range items {
			topAll = append(topAll, c.ID)
		}
		if !hasNext {
			break
		}
		last := items[len(items)-1]
		after = &CommentPageKey{CreatedAtMS: last.CreatedAtMS, Hearts: last.HeartsCount, ID: last.ID}
	}
	assertIDSeq(t, topAll, "c2", "c3", "c1")
}

func TestCommentListRepliesNestUnderParents(t *testing.T) {
	t.Parallel()
	db, p := commentFixture(t)
	ctx := t.Context()

	mustCreateComment(t, db, BlogContent, "r1", "u2", "p1", new("c1"), "reply to first", 0, 1500, 1500)
	mustCreateComment(t, db, BlogContent, "r2", "u3", "p1", new("c1"), "later reply", 1, 2500, 2500)

	items, _, err := ListComments(ctx, db, BlogContent, p, nil, CommentSortNewest, 10, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	assertIDs(t, items, "c3", "c2", "c1")
	replies := items[2].Replies
	assertIDs(t, replies, "r1", "r2") // oldest first inside the thread
	for _, c := range items[:2] {
		if len(c.Replies) != 0 {
			t.Errorf("%s: got %d, want no replies", c.ID, len(c.Replies))
		}
	}
	if replies[1].Hearted {
		t.Error("reply hearted should be false for the anonymous viewer")
	}
}

// TestCountComments_TopLevelAndReplies proves both comment counts mirror
// their reads: the top-level count matches the list walk (a reply is never
// counted as top-level) and the reply count is scoped to one parent.
func TestCountComments_TopLevelAndReplies(t *testing.T) {
	t.Parallel()
	db, p := commentFixture(t)
	ctx := t.Context()

	// One reply under c1; c2 and c3 stay top-level-only.
	mustCreateComment(t, db, BlogContent, "r1", "u2", p, new("c1"), "reply to first", 0, 4000, 4000)

	topCount, err := CountTopLevelComments(ctx, db, BlogContent, p)
	if err != nil {
		t.Fatalf("CountTopLevelComments: %v", err)
	}
	if topCount != 3 {
		t.Errorf("top-level count = %d, want 3 (the fixture's c1/c2/c3)", topCount)
	}
	items, _, err := ListComments(ctx, db, BlogContent, p, nil, CommentSortOldest, 10, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != topCount {
		t.Errorf("list returned %d rows, count says %d — the mirror drifted", len(items), topCount)
	}

	// The reply count is parent-scoped: c1 has one, c2 has none.
	replyCount, err := CountReplies(ctx, db, BlogContent, p, "c1")
	if err != nil {
		t.Fatalf("CountReplies(c1): %v", err)
	}
	if replyCount != 1 {
		t.Errorf("c1 reply count = %d, want 1", replyCount)
	}
	replies, _, err := ListCommentReplies(ctx, db, BlogContent, p, "c1", nil, 10, nil)
	if err != nil {
		t.Fatalf("ListCommentReplies(c1): %v", err)
	}
	if len(replies) != replyCount {
		t.Errorf("c1 reply list returned %d rows, count says %d — the mirror drifted", len(replies), replyCount)
	}
	empty, err := CountReplies(ctx, db, BlogContent, p, "c2")
	if err != nil {
		t.Fatalf("CountReplies(c2): %v", err)
	}
	if empty != 0 {
		t.Errorf("c2 reply count = %d, want 0", empty)
	}
}

func TestCommentListHeartedViewerAware(t *testing.T) {
	t.Parallel()
	db, p := commentFixture(t)
	ctx := t.Context()

	mustCreateHeart(t, db, BlogContent, "u1", "c2", 100)
	// hearts_count fixture for c2 was 2; the heart row does not change it —
	// the fixture bypasses the counter, which is fine: this test pins the
	// hearted projection only.

	viewer := "u1"
	items, _, err := ListComments(ctx, db, BlogContent, p, &viewer, CommentSortTop, 10, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var c2 *Comment
	for i := range items {
		if items[i].ID == "c2" {
			c2 = &items[i]
		}
	}
	if c2 == nil || !c2.Hearted {
		t.Error("c2 should be hearted for u1")
	}
	other := "u2"
	items, _, err = ListComments(ctx, db, BlogContent, p, &other, CommentSortTop, 10, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for i := range items {
		if items[i].ID == "c2" && items[i].Hearted {
			t.Error("c2 should not be hearted for u2")
		}
	}
}

func TestCommentListUnpublishedContentMasked(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	mustCreateUser(t, db, "u1", "alice", "alice", "alice@example.com")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "d1", Title: "Draft", ThumbnailURL: "/media/images/x.jpg"}, "draft")
	ctx := t.Context()

	if _, _, err := ListComments(ctx, db, BlogContent, "d1", nil, CommentSortTop, 10, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("list on draft: got %v, want ErrNotFound", err)
	}
	if _, _, err := ListCommentsFocus(ctx, db, BlogContent, "d1", nil, CommentSortTop, 10, "c1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("focus on draft: got %v, want ErrNotFound", err)
	}
	if _, err := CommentCount(ctx, db, BlogContent, "d1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("count on draft: got %v, want ErrNotFound", err)
	}
	if _, _, err := ListComments(ctx, db, BlogContent, "missing", nil, CommentSortTop, 10, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("list on unknown: got %v, want ErrNotFound", err)
	}
}

func TestBlogCommentCount(t *testing.T) {
	t.Parallel()
	db, p := commentFixture(t)
	ctx := t.Context()

	mustCreateComment(t, db, BlogContent, "r1", "u2", "p1", new("c1"), "reply", 0, 1500, 1500)
	n, err := CommentCount(ctx, db, BlogContent, p)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 4 { // 3 top-level + 1 reply
		t.Errorf("count: got %d, want 4", n)
	}
}

func TestCommentFocusAnchorWindows(t *testing.T) {
	t.Parallel()
	db, p := commentFixture(t)
	ctx := t.Context()

	// Pad the fixture to 25 top-level comments (ids f1..f25, newest last
	// by creation time) so the window math crosses page boundaries.
	for i := 1; i <= 25; i++ {
		id := fmt.Sprintf("f%02d", i)
		mustCreateComment(t, db, BlogContent, id, "u1", "p1", nil, id, 0, int64(3000+i), int64(3000+i))
	}
	// Focus deep in the newest order with limit 4: f21 ranks 5th of the 28
	// top-level comments, so its fixed window is ranks 5..8.
	items, hasNext, err := ListCommentsFocus(ctx, db, BlogContent, p, nil, CommentSortNewest, 4, "f21")
	if err != nil {
		t.Fatalf("focus: %v", err)
	}
	if !hasNext {
		t.Error("expected a next page after the focus window")
	}
	assertIDs(t, items, "f21", "f20", "f19", "f18")

	// A reply target anchors on its parent's window (newest order, limit 4:
	// c1 ranks 28th of 28, so its window is [25..28]).
	reply := "r-target"
	mustCreateComment(t, db, BlogContent, reply, "u2", "p1", new("c1"), "reply", 0, 9000, 9000)
	items, _, err = ListCommentsFocus(ctx, db, BlogContent, p, nil, CommentSortNewest, 4, reply)
	if err != nil {
		t.Fatalf("focus reply: %v", err)
	}
	// c1 is the oldest top-level comment: the last window.
	wantLast := "c1"
	if items[len(items)-1].ID != wantLast {
		t.Errorf("reply focus page should contain the parent %s, got %v", wantLast, idsOf(items))
	}

	// A deleted/unknown anchor returns the FIRST page.
	items, _, err = ListCommentsFocus(ctx, db, BlogContent, p, nil, CommentSortNewest, 4, "gone")
	if err != nil {
		t.Fatalf("focus deleted: %v", err)
	}
	assertIDs(t, items, "f25", "f24", "f23", "f22")
}

// TestCommentReplyWindows pins the reply window: a top-level
// item ships at most inlineReplyWindow replies plus the total count and the
// continues-after-window flag; a parent with no replies keeps an empty
// non-nil slice.
func TestCommentReplyWindows(t *testing.T) {
	t.Parallel()
	db, p := commentFixture(t)
	ctx := t.Context()

	for i := 1; i <= 5; i++ {
		mustCreateComment(t, db, BlogContent, fmt.Sprintf("c1r%d", i), "u2", p, new("c1"), "reply", 0, int64(1500+i), int64(1500+i))
	}
	mustCreateComment(t, db, BlogContent, "c2r1", "u3", p, new("c2"), "one reply", 0, 1900, 1900)

	items, _, err := ListComments(ctx, db, BlogContent, p, nil, CommentSortOldest, 10, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	byID := make(map[string]Comment, len(items))
	for _, c := range items {
		byID[c.ID] = c
	}

	if got := byID["c1"]; got.ReplyCount != 5 || !got.HasMoreReplies {
		t.Errorf("c1: ReplyCount=%d HasMoreReplies=%v, want 5/true", got.ReplyCount, got.HasMoreReplies)
	} else {
		assertIDs(t, got.Replies, "c1r1", "c1r2", "c1r3")
	}
	if got := byID["c2"]; got.ReplyCount != 1 || got.HasMoreReplies {
		t.Errorf("c2: ReplyCount=%d HasMoreReplies=%v, want 1/false", got.ReplyCount, got.HasMoreReplies)
	} else {
		assertIDs(t, got.Replies, "c2r1")
	}
	if got := byID["c3"]; got.ReplyCount != 0 || got.HasMoreReplies || got.Replies == nil || len(got.Replies) != 0 {
		t.Errorf("c3: ReplyCount=%d HasMoreReplies=%v Replies=%v, want 0/false/[]", got.ReplyCount, got.HasMoreReplies, got.Replies)
	}
}

// TestCommentRepliesKeysetPages pins the replies operation: ascending
// (created_at_ms, id) keyset pages with no duplicates, the viewer-aware
// hearted flag, and the masked outcomes (unknown parent, a reply as parent,
// foreign content, unpublished content).
func TestCommentRepliesKeysetPages(t *testing.T) {
	t.Parallel()
	db, p := commentFixture(t)
	ctx := t.Context()

	for i := 1; i <= 5; i++ {
		mustCreateComment(t, db, BlogContent, fmt.Sprintf("c1r%d", i), "u2", p, new("c1"), "reply", 0, int64(1500+i), int64(1500+i))
	}
	mustCreateHeart(t, db, BlogContent, "u1", "c1r1", 100)

	viewer := "u1"
	var (
		all   []string
		after *CommentPageKey
	)
	for i := range 4 {
		items, hasNext, err := ListCommentReplies(ctx, db, BlogContent, p, "c1", &viewer, 2, after)
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		for _, c := range items {
			all = append(all, c.ID)
		}
		if i == 0 {
			if len(items) != 2 || !items[0].Hearted || items[1].Hearted {
				t.Errorf("page 0: hearted projection = %+v", items)
			}
		}
		if !hasNext {
			break
		}
		last := items[len(items)-1]
		after = &CommentPageKey{CreatedAtMS: last.CreatedAtMS, ID: last.ID}
	}
	assertIDSeq(t, all, "c1r1", "c1r2", "c1r3", "c1r4", "c1r5")

	// A reply item carries no reply window of its own.
	if items, _, err := ListCommentReplies(ctx, db, BlogContent, p, "c1", nil, 2, nil); err != nil {
		t.Fatalf("windowed replies: %v", err)
	} else if items[0].Replies != nil || items[0].ReplyCount != 0 || items[0].HasMoreReplies {
		t.Errorf("reply item must carry no reply window: %+v", items[0])
	}

	// Masked outcomes: an unknown parent, a REPLY as the parent (depth 1 is
	// an API rule), and a foreign content id all answer ErrNotFound.
	if _, _, err := ListCommentReplies(ctx, db, BlogContent, p, "nope", nil, 10, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown parent: got %v, want ErrNotFound", err)
	}
	if _, _, err := ListCommentReplies(ctx, db, BlogContent, p, "c1r1", nil, 10, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("reply as parent: got %v, want ErrNotFound", err)
	}
	if _, _, err := ListCommentReplies(ctx, db, BlogContent, "other-post", "c1", nil, 10, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign content: got %v, want ErrNotFound", err)
	}

	// Unpublished content masks the same way.
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "d1", Title: "Draft", ThumbnailURL: "/media/images/x.jpg"}, "draft")
	mustCreateComment(t, db, BlogContent, "dc1", "u1", "d1", nil, "draft comment", 0, 1000, 1000)
	if _, _, err := ListCommentReplies(ctx, db, BlogContent, "d1", "dc1", nil, 10, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("draft content: got %v, want ErrNotFound", err)
	}
}

// TestCommentRepliesKeysetTieBreak pins the id tie-breaker: replies sharing
// one created_at_ms page in id order, with no duplicate or skipped row.
func TestCommentRepliesKeysetTieBreak(t *testing.T) {
	t.Parallel()
	db, p := commentFixture(t)
	ctx := t.Context()

	for _, id := range []string{"t-a", "t-b", "t-c"} {
		mustCreateComment(t, db, BlogContent, id, "u2", p, new("c1"), "same instant", 0, 5000, 5000)
	}

	var (
		all   []string
		after *CommentPageKey
	)
	for i := range 4 {
		items, hasNext, err := ListCommentReplies(ctx, db, BlogContent, p, "c1", nil, 1, after)
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		all = append(all, idsOf(items)...)
		if !hasNext {
			break
		}
		last := items[len(items)-1]
		after = &CommentPageKey{CreatedAtMS: last.CreatedAtMS, ID: last.ID}
	}
	assertIDSeq(t, all, "t-a", "t-b", "t-c")
}

// TestCommentFocusShiftsReplyWindow pins the focus extension of R4: an
// anchor outside the inline window returns the FIXED reply page containing
// it (never anchor-centered), so the deep link still renders its target.
func TestCommentFocusShiftsReplyWindow(t *testing.T) {
	t.Parallel()
	db, p := commentFixture(t)
	ctx := t.Context()

	for i := 1; i <= 6; i++ {
		mustCreateComment(t, db, BlogContent, fmt.Sprintf("c1r%d", i), "u2", p, new("c1"), "reply", 0, int64(1500+i), int64(1500+i))
	}

	cases := []struct {
		name      string
		focus     string
		want      []string
		moreAfter bool
	}{
		{"anchor inside the first window", "c1r2", []string{"c1r1", "c1r2", "c1r3"}, true},
		{"anchor in the second window", "c1r4", []string{"c1r4", "c1r5", "c1r6"}, false},
		{"anchor mid second window", "c1r5", []string{"c1r4", "c1r5", "c1r6"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			items, _, err := ListCommentsFocus(ctx, db, BlogContent, p, nil, CommentSortOldest, 10, tc.focus)
			if err != nil {
				t.Fatalf("focus %s: %v", tc.focus, err)
			}
			var parent *Comment
			for i := range items {
				if items[i].ID == "c1" {
					parent = &items[i]
				}
			}
			if parent == nil {
				t.Fatalf("focus %s: parent c1 missing from the page", tc.focus)
			}
			assertIDs(t, parent.Replies, tc.want...)
			if parent.ReplyCount != 6 || parent.HasMoreReplies != tc.moreAfter {
				t.Errorf("focus %s: ReplyCount=%d HasMoreReplies=%v, want 6/%v",
					tc.focus, parent.ReplyCount, parent.HasMoreReplies, tc.moreAfter)
			}
		})
	}
}

func TestCreateComment(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	mustCreateUser(t, db, "u1", "alice", "alice", "alice@example.com")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "p1", Title: "Post", ThumbnailURL: "/media/images/x.jpg", PublishedAtMS: 3000}, "published")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "d1", Title: "Draft", ThumbnailURL: "/media/images/x.jpg"}, "draft")
	ctx := t.Context()

	if _, err := CreateComment(ctx, db, BlogContent, "p1", "u1", "hello", "new-1", 5000); err != nil {
		t.Fatalf("create: %v", err)
	}
	var body string
	if err := db.QueryRow(`SELECT body FROM blog_post_comments WHERE id = 'new-1'`).Scan(&body); err != nil {
		t.Fatalf("read created comment: %v", err)
	}
	if body != "hello" {
		t.Errorf("body: got %q, want hello", body)
	}

	if _, err := CreateComment(ctx, db, BlogContent, "d1", "u1", "nope", "new-2", 5000); !errors.Is(err, ErrNotFound) {
		t.Errorf("create on draft: got %v, want ErrNotFound", err)
	}
	if _, err := CreateComment(ctx, db, BlogContent, "missing", "u1", "nope", "new-3", 5000); !errors.Is(err, ErrNotFound) {
		t.Errorf("create on unknown content: got %v, want ErrNotFound", err)
	}
	if _, err := CreateComment(ctx, db, BlogContent, "p1", "ghost", "nope", "new-4", 5000); !errors.Is(err, ErrNotFound) {
		t.Errorf("create by deleted user: got %v, want ErrNotFound (FK mapped)", err)
	}
}

func TestCreateCommentReply(t *testing.T) {
	t.Parallel()
	db, p := commentFixture(t)
	ctx := t.Context()

	// A plain reply emits a notification to the parent's author AND returns
	// the fan-out event (the push channel input).
	ev, emitted, err := CreateCommentReply(ctx, db, BlogContent, p, "c1", "u2", "nice", "r1", "n1", 5000)
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if !emitted {
		t.Fatal("plain reply must emit an event")
	}
	want := NotificationEvent{RecipientID: "u1", Kind: "comment_reply", ContentKind: "blog-posts", ContentID: p, CommentID: "r1", ContentTitle: "Post"}
	if ev != want {
		t.Errorf("reply event: got %+v, want %+v", ev, want)
	}
	var (
		kind, recipient, actor, contentKind, contentID, commentID string
	)
	err = db.QueryRow(`SELECT kind, recipient_id, actor_id, content_kind, content_id, comment_id
		FROM user_notifications WHERE id = 'n1'`).Scan(&kind, &recipient, &actor, &contentKind, &contentID, &commentID)
	if err != nil {
		t.Fatalf("read notification: %v", err)
	}
	if kind != "comment_reply" || recipient != "u1" || actor != "u2" || contentKind != "blog-posts" || contentID != p || commentID != "r1" {
		t.Errorf("notification row: %+v", []string{kind, recipient, actor, contentKind, contentID, commentID})
	}

	// Self-reply is suppressed — no row AND no event.
	if ev, emitted, err = CreateCommentReply(ctx, db, BlogContent, p, "c1", "u1", "self", "r2", "n2", 6000); err != nil {
		t.Fatalf("self reply: %v", err)
	} else if emitted || ev.RecipientID != "" {
		t.Errorf("self-reply must not emit an event, got %+v", ev)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE id = 'n2'`).Scan(&n); err != nil {
		t.Fatalf("count self notification: %v", err)
	}
	if n != 0 {
		t.Error("self-reply must not emit a notification")
	}

	// Reply-to-reply is the API's depth-1 422.
	mustCreateComment(t, db, BlogContent, "deep", "u3", "p1", new("c1"), "nested", 0, 7000, 7000)
	if _, _, err := CreateCommentReply(ctx, db, BlogContent, p, "deep", "u2", "no", "r3", "n3", 8000); !errors.Is(err, ErrInvalidParent) {
		t.Errorf("reply-to-reply: got %v, want ErrInvalidParent", err)
	}
	// Unknown parent → masked.
	if _, _, err := CreateCommentReply(ctx, db, BlogContent, p, "gone", "u2", "no", "r4", "n4", 8000); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown parent: got %v, want ErrNotFound", err)
	}
	// Foreign parent (a comment on another content) → masked.
	mustCreateUser(t, db, "u4", "dave", "dave", "dave@example.com")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "p2", Title: "Other", ThumbnailURL: "/media/images/x.jpg", PublishedAtMS: 3000}, "published")
	mustCreateComment(t, db, BlogContent, "foreign", "u4", "p2", nil, "other post", 0, 1000, 1000)
	if _, _, err := CreateCommentReply(ctx, db, BlogContent, p, "foreign", "u2", "no", "r5", "n5", 8000); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign parent: got %v, want ErrNotFound", err)
	}
	// A foreign parent that is ITSELF a reply is also the masked outcome —
	// the content-scoped lookup never leaks the parent's depth.
	mustCreateComment(t, db, BlogContent, "foreign-reply", "u4", "p2", new("foreign"), "nested on other", 0, 1500, 1500)
	if _, _, err := CreateCommentReply(ctx, db, BlogContent, p, "foreign-reply", "u2", "no", "r5b", "n5b", 8000); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign reply parent: got %v, want ErrNotFound", err)
	}
	// Unpublished content → masked (the parent is on the published post —
	// use a draft post with its own comment instead).
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "d2", Title: "Draft", ThumbnailURL: "/media/images/x.jpg"}, "draft")
	mustCreateComment(t, db, BlogContent, "dc", "u4", "d2", nil, "draft comment", 0, 1000, 1000)
	if _, _, err := CreateCommentReply(ctx, db, BlogContent, "d2", "dc", "u2", "no", "r6", "n6", 8000); !errors.Is(err, ErrNotFound) {
		t.Errorf("reply on draft content: got %v, want ErrNotFound", err)
	}
}

func TestUpdateComment(t *testing.T) {
	t.Parallel()
	db, p := commentFixture(t)
	ctx := t.Context()

	// Author edit succeeds and stamps updated_at_ms.
	c, err := UpdateComment(ctx, db, BlogContent, p, "c1", "u1", "edited body", 9000)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if c.Body != "edited body" || c.UpdatedAtMS != 9000 {
		t.Errorf("updated item: %+v", c)
	}
	if len(c.Replies) != 0 {
		t.Errorf("updated top-level item should carry a (possibly empty) replies slice, got %v", c.Replies)
	}

	// Non-author is ErrNotAuthor.
	if _, err := UpdateComment(ctx, db, BlogContent, p, "c1", "u2", "mine now", 9100); !errors.Is(err, ErrNotAuthor) {
		t.Errorf("non-author: got %v, want ErrNotAuthor", err)
	}
	// Unknown/foreign comment → masked.
	if _, err := UpdateComment(ctx, db, BlogContent, p, "gone", "u1", "x", 9200); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown: got %v, want ErrNotFound", err)
	}
	// Unpublished content → masked (the gate applies to PATCH).
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "d3", Title: "Draft", ThumbnailURL: "/media/images/x.jpg"}, "draft")
	mustCreateComment(t, db, BlogContent, "dc", "u1", "d3", nil, "draft comment", 0, 1000, 1000)
	if _, err := UpdateComment(ctx, db, BlogContent, "d3", "dc", "u1", "x", 9300); !errors.Is(err, ErrNotFound) {
		t.Errorf("unpublished: got %v, want ErrNotFound", err)
	}
}

func TestDeleteCommentCascade(t *testing.T) {
	t.Parallel()
	db, p := commentFixture(t)
	ctx := t.Context()

	mustCreateComment(t, db, BlogContent, "r1", "u2", "p1", new("c2"), "reply", 0, 4000, 4000)
	mustCreateHeart(t, db, BlogContent, "u1", "c2", 100)
	mustCreateHeart(t, db, BlogContent, "u2", "r1", 100)

	author, published, err := CommentAuthorID(ctx, db, BlogContent, p, "c2")
	if err != nil {
		t.Fatalf("author: %v", err)
	}
	if author != "u2" {
		t.Errorf("author: got %q, want u2", author)
	}
	if !published {
		t.Error("published: got false, want true (the content is published)")
	}

	// The author deletes their own comment: the cascade is what this test
	// pins — the notice emission is covered separately (R5 tests below).
	if _, emitted, err := DeleteComment(ctx, db, BlogContent, "c2", "u2", "n1", 5000); err != nil {
		t.Fatalf("delete: %v", err)
	} else if emitted {
		t.Error("a self-delete must not emit a comment_removed notice")
	}
	for _, q := range []string{
		`SELECT COUNT(*) FROM blog_post_comments WHERE id IN ('c2','r1')`,
		`SELECT COUNT(*) FROM blog_post_comment_hearts WHERE comment_id IN ('c2','r1')`,
	} {
		var n int
		if err := db.QueryRow(q).Scan(&n); err != nil {
			t.Fatalf("count after delete: %v", err)
		}
		if n != 0 {
			t.Errorf("cascade left %d rows for %q", n, q)
		}
	}
	if _, _, err := DeleteComment(ctx, db, BlogContent, "c2", "u2", "n2", 5000); !errors.Is(err, ErrNotFound) {
		t.Errorf("repeat delete: got %v, want ErrNotFound", err)
	}
	if _, _, err := CommentAuthorID(ctx, db, BlogContent, p, "c2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("author of deleted: got %v, want ErrNotFound", err)
	}
}

// TestDeleteCommentRemovalNotice pins the removal notice: a
// non-author (moderator+) delete emits ONE actorless comment_removed row for
// the addressed comment — actor_id NULL, the deleted comment's id as the
// polymorphic ref, and no per-reply notice for the cascade sweep.
func TestDeleteCommentRemovalNotice(t *testing.T) {
	t.Parallel()
	db, p := commentFixture(t)
	ctx := t.Context()

	// c1 (u1) carries two replies by u2 and u3; deleting c1 as u3 (a
	// non-author) sweeps both replies.
	mustCreateComment(t, db, BlogContent, "r1", "u2", p, new("c1"), "reply a", 0, 4000, 4000)
	mustCreateComment(t, db, BlogContent, "r2", "u3", p, new("c1"), "reply b", 0, 4100, 4100)

	ev, emitted, err := DeleteComment(ctx, db, BlogContent, "c1", "u3", "n-removal", 9000)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !emitted {
		t.Fatal("a non-author delete must emit the removal notice")
	}
	wantEv := NotificationEvent{
		RecipientID:  "u1",
		Kind:         "comment_removed",
		ContentKind:  "blog-posts",
		ContentID:    p,
		CommentID:    "c1",
		ContentTitle: "Post",
	}
	if ev != wantEv {
		t.Errorf("removal event: got %+v, want %+v", ev, wantEv)
	}

	rows, err := db.Query(`SELECT id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms
		FROM user_notifications`)
	if err != nil {
		t.Fatalf("read notifications: %v", err)
	}
	defer rows.Close()
	type row struct {
		id, recipient, kind, contentKind, contentID, commentID string
		actorID                                                sql.NullString
		createdMS                                              int64
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.recipient, &r.kind, &r.actorID, &r.contentKind, &r.contentID, &r.commentID, &r.createdMS); err != nil {
			t.Fatalf("scan notification: %v", err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate notifications: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("notifications: got %d rows, want exactly 1 (no per-reply notice)", len(got))
	}
	r := got[0]
	if r.id != "n-removal" || r.recipient != "u1" || r.kind != "comment_removed" ||
		r.contentKind != "blog-posts" || r.contentID != p || r.commentID != "c1" || r.createdMS != 9000 {
		t.Errorf("notification row: %+v", r)
	}
	if r.actorID.Valid {
		t.Errorf("the removal notice must be ACTORLESS (actor_id NULL), got %q", r.actorID.String)
	}

	// Every swept comment really is gone (the notice dangles by design —
	// comment_id has no FK).
	var remaining int
	if err := db.QueryRow(`SELECT COUNT(*) FROM blog_post_comments`).Scan(&remaining); err != nil {
		t.Fatalf("count comments: %v", err)
	}
	if remaining != 2 { // c2 and c3 survive
		t.Errorf("comments left: got %d, want 2", remaining)
	}
}

// TestDeleteProjectCommentRemovalNotice pins the projects leg of the R5
// emission (the blog path is covered above): the polymorphic ref carries
// "projects"/the project id, and the notice's dangling comment_id cannot
// collide when a later comment reuses the id (comment_id has no FK).
func TestDeleteProjectCommentRemovalNotice(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()
	mustCreateUser(t, db, "u1", "alice", "alice", "alice@example.com")
	mustCreateUser(t, db, "mod", "moddy", "moddy", "moddy@example.com")
	mustCreateProject(t, db, ProjectSummary{ID: "p1", Title: "Project", ThumbnailURL: "https://example.com/t.jpg", PublishedAtMS: 3000}, "published")
	mustCreateComment(t, db, ProjectContent, "pc1", "u1", "p1", nil, "top", 0, 1000, 1000)

	ev, emitted, err := DeleteComment(ctx, db, ProjectContent, "pc1", "mod", "n-removal", 9000)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !emitted {
		t.Fatal("a non-author delete must emit the removal notice")
	}
	wantEv := NotificationEvent{
		RecipientID:  "u1",
		Kind:         "comment_removed",
		ContentKind:  "projects",
		ContentID:    "p1",
		CommentID:    "pc1",
		ContentTitle: "Project",
	}
	if ev != wantEv {
		t.Errorf("removal event: got %+v, want %+v", ev, wantEv)
	}

	// A new comment reusing the deleted id must not disturb the notice.
	mustCreateComment(t, db, ProjectContent, "pc1", "mod", "p1", nil, "reborn", 0, 9500, 9500)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE comment_id = 'pc1'`).Scan(&n); err != nil {
		t.Fatalf("count notices: %v", err)
	}
	if n != 1 {
		t.Errorf("notices for pc1: got %d, want 1", n)
	}
}

func TestSetCommentHeart(t *testing.T) {
	t.Parallel()
	db, p := commentFixture(t)
	ctx := t.Context()

	// c1 is authored by u1 — u2 hearts it.
	// On → counter +1 AND one heart notification to the author (plus the
	// fan-out event); repeat on → idempotent (no second row, no event).
	ev, emitted, err := SetCommentHeart(ctx, db, BlogContent, "u2", p, "c1", true, "n1", 1000)
	if err != nil {
		t.Fatalf("heart on: %v", err)
	}
	if !emitted {
		t.Fatal("fresh heart must emit an event")
	}
	wantEv := NotificationEvent{RecipientID: "u1", Kind: "heart", ContentKind: "blog-posts", ContentID: p, CommentID: "c1", ContentTitle: "Post"}
	if ev != wantEv {
		t.Errorf("heart event: got %+v, want %+v", ev, wantEv)
	}
	if ev, emitted, err := SetCommentHeart(ctx, db, BlogContent, "u2", p, "c1", true, "n2", 1100); err != nil {
		t.Fatalf("heart on repeat: %v", err)
	} else if emitted || ev.RecipientID != "" {
		t.Errorf("repeat heart must not emit an event, got %+v", ev)
	}
	var hearts int64
	if err := db.QueryRow(`SELECT hearts_count FROM blog_post_comments WHERE id = 'c1'`).Scan(&hearts); err != nil {
		t.Fatalf("read hearts: %v", err)
	}
	if hearts != 1 {
		t.Errorf("hearts after repeat on: got %d, want 1", hearts)
	}
	var kind, recipient, actor, contentKind, contentID, commentID string
	if err := db.QueryRow(`SELECT kind, recipient_id, actor_id, content_kind, content_id, comment_id FROM user_notifications`).
		Scan(&kind, &recipient, &actor, &contentKind, &contentID, &commentID); err != nil {
		t.Fatalf("read heart notification: %v", err)
	}
	if kind != "heart" || recipient != "u1" || actor != "u2" || contentKind != "blog-posts" || contentID != p || commentID != "c1" {
		t.Errorf("heart notification: kind=%s recipient=%s actor=%s contentKind=%s contentID=%s commentID=%s",
			kind, recipient, actor, contentKind, contentID, commentID)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications`).Scan(&n); err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	if n != 1 {
		t.Errorf("notification rows after repeat on: got %d, want 1", n)
	}

	// Off → counter -1 AND the heart notification is removed in the same
	// transaction (no stale feed claim); repeat off → idempotent.
	if _, _, err := SetCommentHeart(ctx, db, BlogContent, "u2", p, "c1", false, "", 1200); err != nil {
		t.Fatalf("heart off: %v", err)
	}
	if _, _, err := SetCommentHeart(ctx, db, BlogContent, "u2", p, "c1", false, "", 1300); err != nil {
		t.Fatalf("heart off repeat: %v", err)
	}
	if err := db.QueryRow(`SELECT hearts_count FROM blog_post_comments WHERE id = 'c1'`).Scan(&hearts); err != nil {
		t.Fatalf("read hearts: %v", err)
	}
	if hearts != 0 {
		t.Errorf("hearts after repeat off: got %d, want 0", hearts)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications`).Scan(&n); err != nil {
		t.Fatalf("count notifications after off: %v", err)
	}
	if n != 0 {
		t.Errorf("notification rows after off: got %d, want 0", n)
	}

	// Re-heart after an unheart → a FRESH notification row (the revision:
	// re-hearting naturally re-notifies).
	if _, _, err := SetCommentHeart(ctx, db, BlogContent, "u2", p, "c1", true, "n2b", 1250); err != nil {
		t.Fatalf("re-heart: %v", err)
	}
	if err := db.QueryRow(`SELECT hearts_count FROM blog_post_comments WHERE id = 'c1'`).Scan(&hearts); err != nil {
		t.Fatalf("read hearts after re-heart: %v", err)
	}
	if hearts != 1 {
		t.Errorf("hearts after re-heart: got %d, want 1", hearts)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE kind = 'heart'`).Scan(&n); err != nil {
		t.Fatalf("count notifications after re-heart: %v", err)
	}
	if n != 1 {
		t.Errorf("notification rows after re-heart: got %d, want 1", n)
	}
	// Back to neutral for the self-heart leg below.
	if _, _, err := SetCommentHeart(ctx, db, BlogContent, "u2", p, "c1", false, "", 1350); err != nil {
		t.Fatalf("unheart after re-heart: %v", err)
	}

	// Self-heart → ErrSelfHeart: no row, no notification.
	if _, _, err := SetCommentHeart(ctx, db, BlogContent, "u1", p, "c1", true, "n3", 1400); !errors.Is(err, ErrSelfHeart) {
		t.Errorf("self-heart: got %v, want ErrSelfHeart", err)
	}
	if err := db.QueryRow(`SELECT hearts_count FROM blog_post_comments WHERE id = 'c1'`).Scan(&hearts); err != nil {
		t.Fatalf("read hearts after self-heart: %v", err)
	}
	if hearts != 0 {
		t.Errorf("hearts after self-heart: got %d, want 0", hearts)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications`).Scan(&n); err != nil {
		t.Fatalf("count notifications after self-heart: %v", err)
	}
	if n != 0 {
		t.Errorf("notification rows after self-heart: got %d, want 0", n)
	}

	// Unpublished content → masked, in BOTH directions. A heart placed while
	// published cannot be removed once the content is unpublished: the row
	// and counter freeze and return with re-publication (the masked 404).
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "d4", Title: "Draft", ThumbnailURL: "/media/images/x.jpg"}, "draft")
	mustCreateComment(t, db, BlogContent, "dc", "u1", "d4", nil, "draft", 0, 1000, 1000)
	if _, _, err := SetCommentHeart(ctx, db, BlogContent, "u2", "d4", "dc", true, "n4", 1500); !errors.Is(err, ErrNotFound) {
		t.Errorf("heart on draft content: got %v, want ErrNotFound", err)
	}
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "p9", Title: "P9", ThumbnailURL: "/media/images/x.jpg", PublishedAtMS: 5000}, "published")
	mustCreateComment(t, db, BlogContent, "pc9", "u1", "p9", nil, "hearted", 0, 1000, 1000)
	if _, _, err := SetCommentHeart(ctx, db, BlogContent, "u2", "p9", "pc9", true, "n6", 1600); err != nil {
		t.Fatalf("heart on published content: %v", err)
	}
	if _, err := db.Exec(`UPDATE blog_posts SET status = 'archived' WHERE id = 'p9'`); err != nil {
		t.Fatalf("archive the hearted content: %v", err)
	}
	if _, _, err := SetCommentHeart(ctx, db, BlogContent, "u2", "p9", "pc9", false, "", 1700); !errors.Is(err, ErrNotFound) {
		t.Errorf("unheart on unpublished content: got %v, want ErrNotFound", err)
	}
	// The 404 is a freeze, not a cleanup: the row and the counter stay put
	// and return with re-publication.
	var frozenCounter int64
	if err := db.QueryRow(`SELECT hearts_count FROM blog_post_comments WHERE id = 'pc9'`).Scan(&frozenCounter); err != nil {
		t.Fatalf("read frozen counter: %v", err)
	}
	var frozenRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM blog_post_comment_hearts WHERE comment_id = 'pc9'`).Scan(&frozenRows); err != nil {
		t.Fatalf("count frozen heart rows: %v", err)
	}
	if frozenCounter != 1 || frozenRows != 1 {
		t.Errorf("frozen heart state: hearts_count=%d heart rows=%d, want 1/1", frozenCounter, frozenRows)
	}
	// The unstamped draft answers the same masked 404 on the way out.
	if _, _, err := SetCommentHeart(ctx, db, BlogContent, "u2", "d4", "dc", false, "", 1800); !errors.Is(err, ErrNotFound) {
		t.Errorf("unheart on draft content: got %v, want ErrNotFound", err)
	}
	// Deleted comment → masked.
	if _, _, err := SetCommentHeart(ctx, db, BlogContent, "u2", p, "gone", true, "n5", 1500); !errors.Is(err, ErrNotFound) {
		t.Errorf("heart on deleted comment: got %v, want ErrNotFound", err)
	}
}

// TestCommentNoticesSkipSuspendedRecipients pins the active-recipient rule
// on the three single-recipient notices (reply, heart, removal): a suspended
// recipient gets neither a row nor an event, while the write itself still
// succeeds.
func TestCommentNoticesSkipSuspendedRecipients(t *testing.T) {
	t.Parallel()
	db, p := commentFixture(t)
	ctx := t.Context()

	// Reply to a suspended parent author: the reply lands, no notice.
	if _, err := db.Exec(`UPDATE users SET status = ? WHERE id = 'u1'`, identity.StatusSuspended); err != nil {
		t.Fatalf("suspend u1: %v", err)
	}
	if _, emitted, err := CreateCommentReply(ctx, db, BlogContent, p, "c1", "u2", "nice", "r1", "n1", 5000); err != nil {
		t.Fatalf("reply: %v", err)
	} else if emitted {
		t.Error("reply to a suspended author emitted a notice")
	}

	// Heart on a suspended author's comment: the heart lands, no notice.
	if _, emitted, err := SetCommentHeart(ctx, db, BlogContent, "u3", p, "c1", true, "n2", 6000); err != nil {
		t.Fatalf("heart: %v", err)
	} else if emitted {
		t.Error("heart on a suspended author's comment emitted a notice")
	}

	// Removal notice to a suspended author: the delete lands, no notice.
	if _, err := db.Exec(`UPDATE users SET status = ? WHERE id = 'u2'`, identity.StatusSuspended); err != nil {
		t.Fatalf("suspend u2: %v", err)
	}
	if _, emitted, err := DeleteComment(ctx, db, BlogContent, "r1", "u3", "n3", 7000); err != nil {
		t.Fatalf("delete: %v", err)
	} else if emitted {
		t.Error("removal notice to a suspended author emitted")
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications`).Scan(&n); err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	if n != 0 {
		t.Errorf("notification rows = %d, want 0", n)
	}
}

// TestSetCommentHeartNotifications pins the notification side of the heart
// toggle beyond the happy path: replies notify the REPLY author, an unheart
// removes only that actor's heart row (other actors and kinds survive).
func TestSetCommentHeartNotifications(t *testing.T) {
	t.Parallel()
	db, p := commentFixture(t)
	ctx := t.Context()

	// u1 authors the top-level c1; u2 authors a reply r1 to it.
	mustCreateComment(t, db, BlogContent, "r1", "u2", "p1", new("c1"), "reply", 0, 4000, 4000)

	// u3 hearts the reply → the notification goes to u2 (the reply author).
	if _, _, err := SetCommentHeart(ctx, db, BlogContent, "u3", p, "r1", true, "n1", 5000); err != nil {
		t.Fatalf("heart reply: %v", err)
	}
	var recipient string
	if err := db.QueryRow(`SELECT recipient_id FROM user_notifications WHERE comment_id = 'r1'`).Scan(&recipient); err != nil {
		t.Fatalf("read reply heart notification: %v", err)
	}
	if recipient != "u2" {
		t.Errorf("reply heart recipient: got %s, want u2", recipient)
	}

	// u1 hearts the top-level c2 (author u2) and a comment_reply row from
	// u1 to u2 exists on c1.
	if _, _, err := SetCommentHeart(ctx, db, BlogContent, "u1", p, "c2", true, "n2", 6000); err != nil {
		t.Fatalf("heart c2: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO user_notifications
		(id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms)
		VALUES ('nr1', 'u2', 'comment_reply', 'u1', 'blog-posts', 'p1', 'c1', 6100)`); err != nil {
		t.Fatalf("seed comment_reply row: %v", err)
	}

	// u1 unhearts c2 → only u1's heart row on c2 goes; u3's heart row on r1
	// and the comment_reply row survive.
	if _, _, err := SetCommentHeart(ctx, db, BlogContent, "u1", p, "c2", false, "", 7000); err != nil {
		t.Fatalf("unheart c2: %v", err)
	}
	var heartRows, replyRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE kind = 'heart'`).Scan(&heartRows); err != nil {
		t.Fatalf("count heart rows: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE kind = 'comment_reply'`).Scan(&replyRows); err != nil {
		t.Fatalf("count comment_reply rows: %v", err)
	}
	if heartRows != 1 {
		t.Errorf("heart rows after unheart: got %d, want 1 (u3's r1 heart)", heartRows)
	}
	if replyRows != 1 {
		t.Errorf("comment_reply rows after unheart: got %d, want 1", replyRows)
	}
}

func TestSetCommentHeartConcurrentNoDrift(t *testing.T) {
	t.Parallel()
	db, p := commentFixture(t)
	ctx := t.Context()

	// 10 goroutines toggle the SAME heart (u2 on c1, author u1) on
	// concurrently — exactly one row wins; the counter must read 1 (no
	// double increments, no drift) and exactly one notification exists.
	const workers = 10
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() {
			if _, _, err := SetCommentHeart(ctx, db, BlogContent, "u2", p, "c1", true, fmt.Sprintf("n%d", i), int64(1000+i)); err != nil {
				t.Errorf("worker %d: %v", i, err)
			}
		})
	}
	wg.Wait()

	var hearts int64
	if err := db.QueryRow(`SELECT hearts_count FROM blog_post_comments WHERE id = 'c1'`).Scan(&hearts); err != nil {
		t.Fatalf("read hearts: %v", err)
	}
	if hearts != 1 {
		t.Errorf("concurrent toggles drifted the counter: got %d, want 1", hearts)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE kind = 'heart' AND comment_id = 'c1'`).Scan(&n); err != nil {
		t.Fatalf("count heart notifications: %v", err)
	}
	if n != 1 {
		t.Errorf("concurrent heart notifications: got %d, want 1", n)
	}
}

func TestCommentCascadesWithContentAndUsers(t *testing.T) {
	t.Parallel()
	db, p := commentFixture(t)
	ctx := t.Context()

	// Two cross-user replies: u2 → c1 (notifies u1) and u1 → c2 (notifies u2).
	if _, _, err := CreateCommentReply(ctx, db, BlogContent, p, "c1", "u2", "first", "r1", "n-a", 9000); err != nil {
		t.Fatalf("reply a: %v", err)
	}
	if _, _, err := CreateCommentReply(ctx, db, BlogContent, p, "c2", "u1", "second", "r2", "n-b", 9100); err != nil {
		t.Fatalf("reply b: %v", err)
	}
	mustCreateHeart(t, db, BlogContent, "u2", "c1", 100)

	// Deleting the content removes comments and hearts. The notification
	// rows deliberately survive — user_notifications has no content FK
	// (comment_id dangles after a hard delete; the focus fallback
	// makes that safe).
	if _, err := db.Exec(`DELETE FROM blog_posts WHERE id = 'p1'`); err != nil {
		t.Fatalf("delete content: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM blog_post_comments`).Scan(&n); err != nil {
		t.Fatalf("count comments: %v", err)
	}
	if n != 0 {
		t.Errorf("content cascade left %d comment rows", n)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM blog_post_comment_hearts`).Scan(&n); err != nil {
		t.Fatalf("count hearts: %v", err)
	}
	if n != 0 {
		t.Errorf("content cascade left %d heart rows", n)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications`).Scan(&n); err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	if n != 2 {
		t.Errorf("content deletion must NOT cascade notifications (no FK): got %d, want 2", n)
	}

	// Deleting the RECIPIENT cascades their notification rows.
	if _, err := db.Exec(`DELETE FROM users WHERE id = 'u1'`); err != nil {
		t.Fatalf("delete recipient: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE id = 'n-a'`).Scan(&n); err != nil {
		t.Fatalf("count recipient rows: %v", err)
	}
	if n != 0 {
		t.Errorf("recipient cascade left the u1-addressed notification")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE id = 'n-b'`).Scan(&n); err != nil {
		t.Fatalf("count survivor: %v", err)
	}
	if n != 1 {
		t.Errorf("the u2-addressed notification should survive u1's deletion")
	}

	// Deleting the ACTOR nulls the actor, keeping the row (SET NULL).
	var actor sql.NullString
	if err := db.QueryRow(`SELECT actor_id FROM user_notifications WHERE id = 'n-b'`).Scan(&actor); err != nil {
		t.Fatalf("read notification actor: %v", err)
	}
	if actor.Valid {
		t.Errorf("actor should be SET NULL after deletion, got %q", actor.String)
	}
}

func TestAuthorStaffRoleMirrorsModel(t *testing.T) {
	t.Parallel()
	// The SQL CASE (authorStaffRoleCase) must match identity.CanViewStaffList
	// for every accepted role (the targetRoleWeightCase canary precedent),
	// and IsStaff must be exactly "the role came back" — one derivation, one
	// staff set.
	db := openStoreDB(t)
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "p1", Title: "Post", ThumbnailURL: "/media/images/x.jpg", PublishedAtMS: 3000}, "published")

	roles := []string{identity.RoleUser, identity.RoleModerator, identity.RoleAdmin, identity.RoleSuperAdmin}
	for i, role := range roles {
		id := fmt.Sprintf("u%d", i)
		mustCreateUser(t, db, id, "user"+id, "user"+id, id+"@example.com")
		if _, err := db.Exec(`UPDATE users SET role = ? WHERE id = ?`, role, id); err != nil {
			t.Fatalf("set role %q: %v", role, err)
		}
		mustCreateComment(t, db, BlogContent, "c"+id, id, "p1", nil, "body", 0, 1000, 1000)
	}

	ctx := t.Context()
	items, _, err := ListComments(ctx, db, BlogContent, "p1", nil, CommentSortNewest, 20, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != len(roles) {
		t.Fatalf("fixture: got %d items, want %d", len(items), len(roles))
	}
	got := make(map[string]CommentAuthor, len(items))
	for _, c := range items {
		got[c.Author.Username] = c.Author
	}
	for i, role := range roles {
		author := got["user"+fmt.Sprintf("u%d", i)]
		want := identity.CanViewStaffList(role)
		if author.IsStaff != want {
			t.Errorf("role %q: isStaff got %v, want %v", role, author.IsStaff, want)
		}
		// R3: the role text comes back for staff only — a regular user's
		// role is never projected, so the handler has nothing to leak.
		wantRole := ""
		if want {
			wantRole = role
		}
		if author.StaffRole != wantRole {
			t.Errorf("role %q: staffRole got %q, want %q", role, author.StaffRole, wantRole)
		}
	}
}
