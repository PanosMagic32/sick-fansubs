package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// CommentSort is the accepted thread sort. The values are wire
// values — the handler validates the query parameter against them.
type CommentSort string

const (
	CommentSortTop    CommentSort = "top"
	CommentSortNewest CommentSort = "newest"
	CommentSortOldest CommentSort = "oldest"
)

// CommentAuthor is the public author projection on a comment:
// id, username, avatar, and the DERIVED staff state for the badge. Staff
// authors additionally carry StaffRole (the
// handler exposes it as `staffRole`); non-staff authors leave it empty, so
// a regular user's role never reaches the handler at all.
type CommentAuthor struct {
	ID        string
	Username  string
	AvatarURL *string
	IsStaff   bool
	StaffRole string
}

// Comment is the SQLite row projection for one comment. Replies is
// populated only for top-level comments (nil for a reply — the wire emits
// []); the handler owns the JSON shape.
//
// ReplyCount and HasMoreReplies describe a TOP-LEVEL item's reply window:
// Replies holds at most inlineReplyWindow
// replies, ReplyCount is the total, and HasMoreReplies reports whether
// replies exist after the window (the reader's continuation signal — the
// handler mints the cursor). Both stay zero on a reply row.
type Comment struct {
	ID             string
	Body           string
	Author         CommentAuthor
	CreatedAtMS    int64
	UpdatedAtMS    int64
	HeartsCount    int64
	Hearted        bool
	Replies        []Comment
	ReplyCount     int64
	HasMoreReplies bool
}

// CommentPageKey is the comment list's keyset continuation tuple. Hearts
// carries the top sort's third slot (hearts_count) and is 0 under the two
// time sorts — the shared cursor codec's payload gains that slot for the
// comment namespaces.
type CommentPageKey struct {
	CreatedAtMS int64
	Hearts      int64
	ID          string
}

// authorStaffRoleCase mirrors identity.CanViewStaffList for the wire badge
// (SQL cannot call Go): the role itself for staff authors, NULL
// for everyone else. IsStaff is derived from it in scanComment, so the
// staff set is listed ONCE — here. Pinned against the model by
// TestAuthorStaffRoleMirrorsModel (the targetRoleWeightCase canary
// precedent): the badge is moderator and above.
const authorStaffRoleCase = `CASE WHEN u.role IN ('moderator', 'admin', 'super-admin') THEN u.role END`

// commentItemSelect is the shared item projection: the comment's own
// fields, the author ref, the derived staff badge, and the viewer-aware
// hearted flag. The hearted LEFT JOIN binds the viewer id (nil — an
// anonymous read — binds NULL, which never matches, so hearted is false).
const commentItemSelect = `c.id, c.body, c.hearts_count, c.created_at_ms, c.updated_at_ms,
	u.id, u.username, u.avatar_url, ` + authorStaffRoleCase + `,
	CASE WHEN h.user_id IS NULL THEN 0 ELSE 1 END`

// scanComment reads one row of the shared item projection. When parent is
// non-nil, the row's trailing parent_id column is scanned into it — the
// reply query selects one extra column so replies can nest under their
// parents.
func scanComment(scan func(dest ...any) error, parent *sql.NullString) (Comment, error) {
	var (
		c         Comment
		avatar    sql.NullString
		staffRole sql.NullString
		hearted   int64
	)
	dest := []any{&c.ID, &c.Body, &c.HeartsCount, &c.CreatedAtMS, &c.UpdatedAtMS,
		&c.Author.ID, &c.Author.Username, &avatar, &staffRole, &hearted}
	if parent != nil {
		dest = append(dest, parent)
	}
	if err := scan(dest...); err != nil {
		return Comment{}, err
	}
	c.Author.AvatarURL = nullStringPtr(avatar)
	c.Author.StaffRole = nullStringValue(staffRole)
	c.Author.IsStaff = c.Author.StaffRole != ""
	c.Hearted = hearted == 1
	return c, nil
}

// requirePublishedContent applies the published-only gate as a read
// precondition: unknown ids, drafts, archived rows, and unstamped rows are
// the same ErrNotFound (the masked 404). The table name comes from the
// caller's ContentKind descriptor.
func requirePublishedContent(ctx context.Context, db *sql.DB, table, id string) error {
	var one int
	err := db.QueryRowContext(ctx,
		`SELECT 1 FROM `+table+` WHERE id = ? AND status = 'published' AND published_at_ms IS NOT NULL`,
		id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: comment content gate: %w", err)
	}
	return nil
}

// commentOrder returns the ORDER BY for one sort (the ID tie-breaker makes
// the order deterministic).
func commentOrder(sort CommentSort) string {
	switch sort {
	case CommentSortTop:
		return `ORDER BY c.hearts_count DESC, c.created_at_ms DESC, c.id ASC`
	case CommentSortNewest:
		return `ORDER BY c.created_at_ms DESC, c.id ASC`
	default: // CommentSortOldest
		return `ORDER BY c.created_at_ms ASC, c.id ASC`
	}
}

// commentContinuation returns the strict "after" predicate plus its
// arguments for one sort. The top sort's predicate compares the whole
// (hearts_count, created_at_ms, id) tuple; the time sorts compare
// (created_at_ms, id).
func commentContinuation(sort CommentSort, after *CommentPageKey) (string, []any) {
	switch sort {
	case CommentSortTop:
		return ` AND (c.hearts_count < ? OR (c.hearts_count = ? AND (c.created_at_ms < ? OR (c.created_at_ms = ? AND c.id > ?))))`,
			[]any{after.Hearts, after.Hearts, after.CreatedAtMS, after.CreatedAtMS, after.ID}
	case CommentSortNewest:
		return ` AND (c.created_at_ms < ? OR (c.created_at_ms = ? AND c.id > ?))`,
			[]any{after.CreatedAtMS, after.CreatedAtMS, after.ID}
	default: // CommentSortOldest
		return ` AND (c.created_at_ms > ? OR (c.created_at_ms = ? AND c.id > ?))`,
			[]any{after.CreatedAtMS, after.CreatedAtMS, after.ID}
	}
}

// commentRankPredicate counts the top-level comments strictly BEFORE the
// anchor under one sort — the focus window's rank computation (the page is
// the fixed window containing the anchor's rank).
func commentRankPredicate(sort CommentSort) string {
	switch sort {
	case CommentSortTop:
		return ` AND (c.hearts_count > ? OR (c.hearts_count = ? AND (c.created_at_ms > ? OR (c.created_at_ms = ? AND c.id < ?))))`
	case CommentSortNewest:
		return ` AND (c.created_at_ms > ? OR (c.created_at_ms = ? AND c.id < ?))`
	default: // CommentSortOldest
		return ` AND (c.created_at_ms < ? OR (c.created_at_ms = ? AND c.id < ?))`
	}
}

// ListComments returns one keyset page of top-level comments with their
// inline reply windows for published content; hasNext reports another page
// (limit+1 rows fetched, the shared keyset contract).
func ListComments(ctx context.Context, db *sql.DB, k ContentKind, contentID string, viewerID *string, sort CommentSort, limit int, after *CommentPageKey) ([]Comment, bool, error) {
	if err := requirePublishedContent(ctx, db, k.table, contentID); err != nil {
		return nil, false, err
	}

	topQuery := `SELECT ` + commentItemSelect + `
		FROM ` + k.comments + ` c
		JOIN users u ON u.id = c.user_id
		LEFT JOIN ` + k.hearts + ` h ON h.comment_id = c.id AND h.user_id = ?
		WHERE c.` + k.commentFK + ` = ? AND c.parent_id IS NULL`
	args := []any{viewerID, contentID}
	if after != nil {
		pred, extra := commentContinuation(sort, after)
		topQuery += pred
		args = append(args, extra...)
	}
	topQuery += " " + commentOrder(sort) + ` LIMIT ?`
	args = append(args, limit+1)

	topItems, err := queryComments(ctx, db, topQuery, args...)
	if err != nil {
		return nil, false, err
	}

	hasNext := len(topItems) > limit
	if hasNext {
		topItems = topItems[:limit]
	}

	items, err := attachReplies(ctx, db, k, contentID, viewerID, topItems, "")
	if err != nil {
		return nil, false, err
	}
	return items, hasNext, nil
}

// ListCommentsFocus returns the fixed window of the active sort containing
// the focus anchor's rank. A deleted, foreign, or unknown anchor falls back
// to the first page — the client then shows its deleted notice.
func ListCommentsFocus(ctx context.Context, db *sql.DB, k ContentKind, contentID string, viewerID *string, sort CommentSort, limit int, focusID string) ([]Comment, bool, error) {
	if err := requirePublishedContent(ctx, db, k.table, contentID); err != nil {
		return nil, false, err
	}

	var (
		parentID     sql.NullString
		anchorCreat  int64
		anchorHearts int64
	)
	err := db.QueryRowContext(ctx,
		`SELECT parent_id, created_at_ms, hearts_count FROM `+k.comments+` WHERE id = ? AND `+k.commentFK+` = ?`,
		focusID, contentID).Scan(&parentID, &anchorCreat, &anchorHearts)
	if errors.Is(err, sql.ErrNoRows) {
		return ListComments(ctx, db, k, contentID, viewerID, sort, limit, nil)
	}
	if err != nil {
		return nil, false, fmt.Errorf("store: focus lookup: %w", err)
	}

	// The anchor is the target itself when top-level, its parent when the
	// focus is a reply (depth 1 guarantees the parent is top-level).
	anchorID := focusID
	if parentID.Valid {
		anchorID = parentID.String
		var anchorParent sql.NullString
		if err := db.QueryRowContext(ctx,
			`SELECT parent_id, created_at_ms, hearts_count FROM `+k.comments+` WHERE id = ?`,
			anchorID).Scan(&anchorParent, &anchorCreat, &anchorHearts); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// Parent deleted between the two lookups — first page.
				return ListComments(ctx, db, k, contentID, viewerID, sort, limit, nil)
			}
			return nil, false, fmt.Errorf("store: focus anchor lookup: %w", err)
		}
		if anchorParent.Valid {
			// Depth corruption (the anchor is itself a reply) — first page
			// rather than a broken window.
			return ListComments(ctx, db, k, contentID, viewerID, sort, limit, nil)
		}
	}

	// The anchor's rank: count the top-level comments strictly before it
	// under the active sort, then +1.
	rankQuery := `SELECT COUNT(*) FROM ` + k.comments + ` c WHERE c.` + k.commentFK + ` = ? AND c.parent_id IS NULL` + commentRankPredicate(sort)
	var rankArgs []any
	switch sort {
	case CommentSortTop:
		rankArgs = []any{contentID, anchorHearts, anchorHearts, anchorCreat, anchorCreat, anchorID}
	default:
		rankArgs = []any{contentID, anchorCreat, anchorCreat, anchorID}
	}
	var before int64
	if err := db.QueryRowContext(ctx, rankQuery, rankArgs...).Scan(&before); err != nil {
		return nil, false, fmt.Errorf("store: focus rank: %w", err)
	}
	rank := before + 1
	offset := (rank - 1) / int64(limit) * int64(limit)

	topQuery := `SELECT ` + commentItemSelect + `
		FROM ` + k.comments + ` c
		JOIN users u ON u.id = c.user_id
		LEFT JOIN ` + k.hearts + ` h ON h.comment_id = c.id AND h.user_id = ?
		WHERE c.` + k.commentFK + ` = ? AND c.parent_id IS NULL
		` + commentOrder(sort) + ` LIMIT ? OFFSET ?`
	topItems, err := queryComments(ctx, db, topQuery, viewerID, contentID, limit+1, offset)
	if err != nil {
		return nil, false, err
	}
	hasNext := len(topItems) > limit
	if hasNext {
		topItems = topItems[:limit]
	}

	items, err := attachReplies(ctx, db, k, contentID, viewerID, topItems, focusID)
	if err != nil {
		return nil, false, err
	}
	return items, hasNext, nil
}

// ListCommentReplies returns one ascending keyset page (created_at_ms, id)
// of a top-level comment's replies for published content. Outcomes:
// ErrNotFound for unknown/foreign content or a parent that is not top-level.
func ListCommentReplies(ctx context.Context, db *sql.DB, k ContentKind, contentID, parentID string, viewerID *string, limit int, after *CommentPageKey) ([]Comment, bool, error) {
	if err := requirePublishedContent(ctx, db, k.table, contentID); err != nil {
		return nil, false, err
	}

	var one int
	err := db.QueryRowContext(ctx,
		`SELECT 1 FROM `+k.comments+` WHERE id = ? AND `+k.commentFK+` = ? AND parent_id IS NULL`,
		parentID, contentID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrNotFound
	}
	if err != nil {
		return nil, false, fmt.Errorf("store: replies parent gate: %w", err)
	}

	query := `SELECT ` + commentItemSelect + `
		FROM ` + k.comments + ` c
		JOIN users u ON u.id = c.user_id
		LEFT JOIN ` + k.hearts + ` h ON h.comment_id = c.id AND h.user_id = ?
		WHERE c.` + k.commentFK + ` = ? AND c.parent_id = ?`
	args := []any{viewerID, contentID, parentID}
	if after != nil {
		query += ` AND (c.created_at_ms > ? OR (c.created_at_ms = ? AND c.id > ?))`
		args = append(args, after.CreatedAtMS, after.CreatedAtMS, after.ID)
	}
	query += ` ORDER BY c.created_at_ms ASC, c.id ASC LIMIT ?`
	args = append(args, limit+1)

	items, err := queryComments(ctx, db, query, args...)
	if err != nil {
		return nil, false, err
	}
	hasNext := len(items) > limit
	if hasNext {
		items = items[:limit]
	}
	return items, hasNext, nil
}

// queryComments runs a query over the shared item projection and scans
// every row.
func queryComments(ctx context.Context, db *sql.DB, query string, args ...any) ([]Comment, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query comments: %w", err)
	}
	defer rows.Close()

	var items []Comment
	for rows.Next() {
		c, err := scanComment(rows.Scan, nil)
		if err != nil {
			return nil, fmt.Errorf("store: scan comment: %w", err)
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate comments: %w", err)
	}
	return items, nil
}

// CommentCount returns the total comment count (top-level + replies) for
// published content.
func CommentCount(ctx context.Context, db *sql.DB, k ContentKind, contentID string) (int64, error) {
	if err := requirePublishedContent(ctx, db, k.table, contentID); err != nil {
		return 0, err
	}
	var n int64
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM `+k.comments+` WHERE `+k.commentFK+` = ?`, contentID).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count comments: %w", err)
	}
	return n, nil
}

// CountTopLevelComments returns the top-level comment count the comments list
// walks — its "of N" number (CommentCount is the total, replies included).
func CountTopLevelComments(ctx context.Context, db *sql.DB, k ContentKind, contentID string) (int, error) {
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM `+k.comments+` c WHERE c.`+k.commentFK+` = ? AND c.parent_id IS NULL`,
		contentID).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count %s comments: %w", k.noun, err)
	}
	return n, nil
}

// CountReplies returns one top-level comment's reply count — the reply
// appender's total.
func CountReplies(ctx context.Context, db *sql.DB, k ContentKind, contentID, parentID string) (int, error) {
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM `+k.comments+` c WHERE c.`+k.commentFK+` = ? AND c.parent_id = ?`,
		contentID, parentID).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count %s replies: %w", k.noun, err)
	}
	return n, nil
}
