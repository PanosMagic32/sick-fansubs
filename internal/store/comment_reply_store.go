package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// inlineReplyWindow is the number of replies a top-level item ships inline —
// a reader appends the rest through the replies operation. Focus pages reuse
// it as their fixed window size (the fixed-window rule applied to replies).
const inlineReplyWindow = 3

// attachReplies nests each top-level item's reply window oldest-first:
// at most inlineReplyWindow replies, plus
// the item's total reply count and whether more replies exist after the
// window. Top-level items without replies keep a non-nil empty slice (the
// wire emits []).
//
// focusID shifts the window for the ONE anchor whose parent is on this page:
// when a deep link targets a reply outside the inline window, that parent's
// window becomes the fixed page containing the anchor instead of the first
// page (the focus contract extended to replies). Every other item keeps
// its first window.
func attachReplies(ctx context.Context, db *sql.DB, k ContentKind, contentID string, viewerID *string, topItems []Comment, focusID string) ([]Comment, error) {
	for i := range topItems {
		topItems[i].Replies = []Comment{}
	}
	if len(topItems) == 0 {
		return topItems, nil
	}

	parentIDs := make([]string, 0, len(topItems))
	for _, c := range topItems {
		parentIDs = append(parentIDs, c.ID)
	}

	counts, err := replyCounts(ctx, db, k, contentID, parentIDs)
	if err != nil {
		return nil, err
	}

	focusParent, focusOffset, err := replyFocusOffset(ctx, db, k, contentID, parentIDs, focusID)
	if err != nil {
		return nil, err
	}

	byParent, err := replyWindows(ctx, db, k, contentID, viewerID, parentIDs, focusParent, focusOffset)
	if err != nil {
		return nil, err
	}

	for i := range topItems {
		id := topItems[i].ID
		offset := int64(0)
		if id == focusParent {
			offset = focusOffset
		}
		if rs, ok := byParent[id]; ok {
			topItems[i].Replies = rs
		}
		topItems[i].ReplyCount = counts[id]
		topItems[i].HasMoreReplies = topItems[i].ReplyCount > offset+int64(len(topItems[i].Replies))
	}
	return topItems, nil
}

// replyCounts returns the total reply count of every given parent (one
// grouped query — a parent with no replies is simply absent from the map).
func replyCounts(ctx context.Context, db *sql.DB, k ContentKind, contentID string, parentIDs []string) (map[string]int64, error) {
	placeholders, args := parentArgs(contentID, parentIDs)
	rows, err := db.QueryContext(ctx,
		`SELECT parent_id, COUNT(*) FROM `+k.comments+`
		WHERE `+k.commentFK+` = ? AND parent_id IN (`+placeholders+`)
		GROUP BY parent_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: count comment replies: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int64, len(parentIDs))
	for rows.Next() {
		var (
			parent string
			n      int64
		)
		if err := rows.Scan(&parent, &n); err != nil {
			return nil, fmt.Errorf("store: scan reply count: %w", err)
		}
		counts[parent] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate reply counts: %w", err)
	}
	return counts, nil
}

// replyFocusOffset resolves the reply-window shift a focus id asks for:
// the anchor's parent (only when that parent is on the current page) and the
// window offset — the fixed page of the inline size containing the anchor's
// rank, so the window is never anchor-centered. A top-level
// anchor, a deleted/foreign anchor, or a parent outside this page shifts
// nothing.
func replyFocusOffset(ctx context.Context, db *sql.DB, k ContentKind, contentID string, parentIDs []string, focusID string) (string, int64, error) {
	if focusID == "" {
		return "", 0, nil
	}

	var (
		parent    sql.NullString
		createdMS int64
	)
	err := db.QueryRowContext(ctx,
		`SELECT parent_id, created_at_ms FROM `+k.comments+` WHERE id = ? AND `+k.commentFK+` = ?`,
		focusID, contentID).Scan(&parent, &createdMS)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, fmt.Errorf("store: focus reply lookup: %w", err)
	}
	if !parent.Valid {
		// The focus is top-level — its own item's first window contains it.
		return "", 0, nil
	}

	parentID := parent.String
	onPage := slices.Contains(parentIDs, parentID)
	if !onPage {
		// The focus page always contains the anchor's parent, so this is a
		// defensive branch — no shift rather than a broken window.
		return "", 0, nil
	}

	var before int64
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM `+k.comments+`
		WHERE `+k.commentFK+` = ? AND parent_id = ?
			AND (created_at_ms < ? OR (created_at_ms = ? AND id < ?))`,
		contentID, parentID, createdMS, createdMS, focusID).Scan(&before); err != nil {
		return "", 0, fmt.Errorf("store: focus reply rank: %w", err)
	}
	rank := before + 1
	return parentID, (rank - 1) / inlineReplyWindow * inlineReplyWindow, nil
}

// replyWindows fetches the reply window of every page parent. The first
// inlineReplyWindow replies of each parent come from one query; a shifted
// parent (the focus anchor's, offset > 0) is excluded from it and fetched
// separately at its own offset. The window bound is a ROW_NUMBER() window
// function, so the query never materializes an unbounded reply set (the
// endpoint's reason to exist).
func replyWindows(ctx context.Context, db *sql.DB, k ContentKind, contentID string, viewerID *string, parentIDs []string, focusParent string, focusOffset int64) (map[string][]Comment, error) {
	byParent := make(map[string][]Comment, len(parentIDs))
	shifted := focusParent != "" && focusOffset > 0

	mainIDs := parentIDs
	if shifted {
		mainIDs = make([]string, 0, len(parentIDs))
		for _, id := range parentIDs {
			if id != focusParent {
				mainIDs = append(mainIDs, id)
			}
		}
	}

	if len(mainIDs) > 0 {
		placeholders, args := parentArgs(contentID, mainIDs)
		// The rank window spans every page parent — the exclusion above only
		// narrows the OUTER rows, never the rn computation.
		rnPlaceholders, rnArgs := parentArgs(contentID, parentIDs)
		rnArgs = append(rnArgs, inlineReplyWindow)

		query := `SELECT ` + commentItemSelect + `, c.parent_id
			FROM ` + k.comments + ` c
			JOIN users u ON u.id = c.user_id
			LEFT JOIN ` + k.hearts + ` h ON h.comment_id = c.id AND h.user_id = ?
			WHERE c.` + k.commentFK + ` = ? AND c.parent_id IN (` + placeholders + `)
				AND c.id IN (
					SELECT id FROM (
						SELECT c2.id AS id,
							ROW_NUMBER() OVER (PARTITION BY c2.parent_id ORDER BY c2.created_at_ms ASC, c2.id ASC) AS rn
						FROM ` + k.comments + ` c2
						WHERE c2.` + k.commentFK + ` = ? AND c2.parent_id IN (` + rnPlaceholders + `)
					) WHERE rn <= ?
				)
			ORDER BY c.created_at_ms ASC, c.id ASC`
		args = append([]any{viewerID}, args...)
		args = append(args, rnArgs...)

		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("store: list comment replies: %w", err)
		}
		if err := collectReplies(rows, byParent); err != nil {
			return nil, err
		}
	}

	if shifted {
		query := `SELECT ` + commentItemSelect + `, c.parent_id
			FROM ` + k.comments + ` c
			JOIN users u ON u.id = c.user_id
			LEFT JOIN ` + k.hearts + ` h ON h.comment_id = c.id AND h.user_id = ?
			WHERE c.` + k.commentFK + ` = ? AND c.parent_id = ?
			ORDER BY c.created_at_ms ASC, c.id ASC LIMIT ? OFFSET ?`
		rows, err := db.QueryContext(ctx, query, viewerID, contentID, focusParent, inlineReplyWindow, focusOffset)
		if err != nil {
			return nil, fmt.Errorf("store: list focus replies: %w", err)
		}
		if err := collectReplies(rows, byParent); err != nil {
			return nil, err
		}
	}

	return byParent, nil
}

// parentArgs builds the `IN (?, …)` placeholder list and its bound arguments
// for one content id plus a parent-id set (the shared shape of the reply
// queries).
func parentArgs(contentID string, parentIDs []string) (string, []any) {
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(parentIDs)), ", ")
	args := make([]any, 0, len(parentIDs)+1)
	args = append(args, contentID)
	for _, id := range parentIDs {
		args = append(args, id)
	}
	return placeholders, args
}

// collectReplies groups the rows of a reply query (the shared item
// projection plus the trailing parent_id) by their parent.
func collectReplies(rows *sql.Rows, byParent map[string][]Comment) error {
	defer rows.Close()
	for rows.Next() {
		var parent sql.NullString
		c, err := scanComment(rows.Scan, &parent)
		if err != nil {
			return fmt.Errorf("store: scan comment reply: %w", err)
		}
		if parent.Valid {
			byParent[parent.String] = append(byParent[parent.String], c)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: iterate comment replies: %w", err)
	}
	return nil
}
