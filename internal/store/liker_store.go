package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Who-liked readers (the likers modal): one keyset page of the ACTIVE users
// behind a content row's favorites or a comment's hearts, newest like first.
//
// Suspended accounts keep their like — the counts are not rewritten — but do
// not appear here, so a page's total can trail the content chip's count by
// exactly the suspended likers. Unknown, unpublished, or unstamped content
// is the masked ErrNotFound; the heart readers additionally require the
// comment to belong to the addressed content.

// LikerItem is one user who liked content, with the like row's timestamp:
// the wire projects id/username/avatar, and the timestamp feeds the
// continuation cursor only.
type LikerItem struct {
	UserID    string
	Username  string
	AvatarURL *string
	LikedAtMS int64
}

// LikerPageKey is the likers keyset continuation tuple
// (created_at_ms, user_id): the ordering is created_at_ms DESC,
// user_id ASC, so "strictly after" follows that tuple.
type LikerPageKey struct {
	LikedAtMS int64
	UserID    string
}

// ListFavoriters returns one keyset page of a published content row's
// active favoriters, newest favorite first.
func ListFavoriters(ctx context.Context, db *sql.DB, k ContentKind, contentID string, limit int, after *LikerPageKey) ([]LikerItem, bool, error) {
	if err := requirePublishedContent(ctx, db, k.table, contentID); err != nil {
		return nil, false, err
	}
	return listLikers(ctx, db,
		`FROM `+k.favorites+` f
		JOIN users u ON u.id = f.user_id
		WHERE f.`+k.favoriteFK+` = ?
			AND u.status = 'active'`,
		[]any{contentID}, "f.created_at_ms", "f.user_id", limit, after)
}

// CountFavoriters returns the active-favoriter count the list walks — the
// page's "of N" number. The row filters mirror ListFavoriters; the
// VISIBILITY gate belongs to the reader, and the page builder runs the read
// first, so the count is only ever reached for content that already passed
// it.
func CountFavoriters(ctx context.Context, db *sql.DB, k ContentKind, contentID string) (int, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*)
		FROM `+k.favorites+` f
		JOIN users u ON u.id = f.user_id
		WHERE f.`+k.favoriteFK+` = ?
			AND u.status = 'active'`, contentID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count favoriters: %w", err)
	}
	return n, nil
}

// ListCommentHearters returns one keyset page of a published comment's
// active hearters, newest heart first.
func ListCommentHearters(ctx context.Context, db *sql.DB, k ContentKind, contentID, commentID string, limit int, after *LikerPageKey) ([]LikerItem, bool, error) {
	if err := requirePublishedComment(ctx, db, k, contentID, commentID); err != nil {
		return nil, false, err
	}
	return listLikers(ctx, db,
		`FROM `+k.hearts+` h
		JOIN users u ON u.id = h.user_id
		WHERE h.comment_id = ?
			AND u.status = 'active'`,
		[]any{commentID}, "h.created_at_ms", "h.user_id", limit, after)
}

// CountCommentHearters returns the active-hearter count the list walks. The
// row filters mirror ListCommentHearters; the visibility gate belongs to the
// reader (the CountFavoriters note).
func CountCommentHearters(ctx context.Context, db *sql.DB, k ContentKind, commentID string) (int, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*)
		FROM `+k.hearts+` h
		JOIN users u ON u.id = h.user_id
		WHERE h.comment_id = ?
			AND u.status = 'active'`, commentID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count hearters: %w", err)
	}
	return n, nil
}

// requirePublishedComment gates a heart list: the comment must belong to the
// addressed content and that content must be publicly visible. Everything
// else is the same masked ErrNotFound the comment reads use.
func requirePublishedComment(ctx context.Context, db *sql.DB, k ContentKind, contentID, commentID string) error {
	var one int
	err := db.QueryRowContext(ctx,
		`SELECT 1 FROM `+k.comments+` c
		JOIN `+k.table+` p ON p.id = c.`+k.commentFK+`
		WHERE c.id = ? AND c.`+k.commentFK+` = ? AND `+publicPredicate,
		commentID, contentID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: comment heart gate: %w", err)
	}
	return nil
}

// listLikers reads one keyset page from a liker query: the FROM/WHERE tail
// with its placeholder args, plus the like row's timestamp and user columns
// for the keyset predicate and the order. The join always aliases users u.
func listLikers(ctx context.Context, db *sql.DB, fromWhere string, args []any, atCol, userCol string, limit int, after *LikerPageKey) ([]LikerItem, bool, error) {
	var (
		query string
		bound []any
	)
	if after == nil {
		query = `SELECT u.id, u.username, u.avatar_url, ` + atCol + ` ` + fromWhere + `
			ORDER BY ` + atCol + ` DESC, ` + userCol + ` ASC LIMIT ?`
		bound = append(append([]any{}, args...), limit+1)
	} else {
		query = `SELECT u.id, u.username, u.avatar_url, ` + atCol + ` ` + fromWhere + `
			AND (` + atCol + ` < ? OR (` + atCol + ` = ? AND ` + userCol + ` > ?))
			ORDER BY ` + atCol + ` DESC, ` + userCol + ` ASC LIMIT ?`
		bound = append(append([]any{}, args...), after.LikedAtMS, after.LikedAtMS, after.UserID, limit+1)
	}

	rows, err := db.QueryContext(ctx, query, bound...)
	if err != nil {
		return nil, false, fmt.Errorf("store: list likers: %w", err)
	}
	defer rows.Close()

	var items []LikerItem
	for rows.Next() {
		var (
			it     LikerItem
			avatar sql.NullString
		)
		if err := rows.Scan(&it.UserID, &it.Username, &avatar, &it.LikedAtMS); err != nil {
			return nil, false, fmt.Errorf("store: scan liker item: %w", err)
		}
		it.AvatarURL = nullStringPtr(avatar)
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("store: iterate likers: %w", err)
	}

	hasNext := len(items) > limit
	if hasNext {
		items = items[:limit]
	}
	return items, hasNext, nil
}
