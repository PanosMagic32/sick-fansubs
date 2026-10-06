package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Favorites persistence (docs/patterns/go/content-kinds.md): two dedicated
// join tables — blog_post_favorites and project_favorites — with composite
// primary keys, reached through the shared ContentKind descriptor.
//
// Every function here is a single statement: no transactions are needed
// (each operation is one row's worth of work, and SQLite statements are
// atomic). Favoriting is constrained to PUBLISHED content: the add path
// validates status inside the same INSERT…SELECT statement, so the check and
// the write share one snapshot.

// FavoriteItem is the row projection shared by the two favorites lists.
// The queries differ per content type; the card-shaped projection does not,
// so one type serves both.
type FavoriteItem struct {
	ID            string
	Title         string
	Subtitle      string // blog posts only; empty for projects
	Description   string
	ThumbnailURL  string
	PublishedAtMS int64
	FavoritedAtMS int64
	CommentCount  int64
	FavoriteCount int64
}

// FavoritePageKey is the favorites keyset continuation tuple
// (created_at_ms, content_id): the ordering is created_at_ms DESC,
// content_id ASC, so "strictly after" follows that tuple.
type FavoritePageKey struct {
	CreatedAtMS int64
	ContentID   string
}

// AddFavorite inserts a favorite row for PUBLISHED content, idempotently:
// a repeat hits the composite primary key and returns nil, while unknown,
// unpublished, or unstamped content is the masked ErrNotFound.
func AddFavorite(ctx context.Context, db *sql.DB, k ContentKind, userID, contentID string, createdAtMS int64) error {
	return addFavorite(ctx, db,
		`INSERT INTO `+k.favorites+` (user_id, `+k.favoriteFK+`, created_at_ms)
		SELECT ?, ?, ?
		WHERE EXISTS (
			SELECT 1 FROM `+k.table+`
			WHERE id = ? AND status = 'published' AND published_at_ms IS NOT NULL
		)`,
		userID, contentID, createdAtMS, contentID)
}

func addFavorite(ctx context.Context, db *sql.DB, query string, args ...any) error {
	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		if IsUniqueViolation(err) {
			// Already favorited — idempotent success.
			return nil
		}
		if isForeignKeyViolation(err) {
			// A user deleted between the session lookup and this insert
			// (or, theoretically, content deleted inside the statement
			// window). The public outcome is the masked 404 — the client's
			// dead session heals through its own 401 transition paths.
			return ErrNotFound
		}
		return fmt.Errorf("store: add favorite: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: add favorite rows affected: %w", err)
	}
	if n == 0 {
		// The EXISTS guard rejected the content — not published, unstamped,
		// or unknown. Deliberately indistinguishable.
		return ErrNotFound
	}
	return nil
}

// RemoveFavorite deletes the favorite row if it exists. Idempotent: removing
// a non-favorite is success. Returns only an error — the 0-or-1 affected
// count is not part of the caller's contract.
func RemoveFavorite(ctx context.Context, db *sql.DB, k ContentKind, userID, contentID string) error {
	return removeFavorite(ctx, db,
		`DELETE FROM `+k.favorites+` WHERE user_id = ? AND `+k.favoriteFK+` = ?`,
		userID, contentID)
}

func removeFavorite(ctx context.Context, db *sql.DB, query string, args ...any) error {
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("store: remove favorite: %w", err)
	}
	return nil
}

// FavoriteStatus reports whether the content is published and whether the
// user favorited it. published=false covers unknown ids, drafts, archived
// rows, and unstamped rows (masked) — favorited is only meaningful when
// published=true.
func FavoriteStatus(ctx context.Context, db *sql.DB, k ContentKind, userID, contentID string) (published, favorited bool, err error) {
	return favoriteStatus(ctx, db,
		`SELECT
			EXISTS(SELECT 1 FROM `+k.table+`
				WHERE id = ? AND status = 'published' AND published_at_ms IS NOT NULL),
			EXISTS(SELECT 1 FROM `+k.favorites+`
				WHERE user_id = ? AND `+k.favoriteFK+` = ?)`,
		contentID, userID, contentID)
}

func favoriteStatus(ctx context.Context, db *sql.DB, query string, args ...any) (bool, bool, error) {
	var published, favorited bool
	err := db.QueryRowContext(ctx, query, args...).Scan(&published, &favorited)
	if err != nil {
		return false, false, fmt.Errorf("store: favorite status: %w", err)
	}
	return published, favorited, nil
}

// favoritesFromWhere is the favorites-list FROM/WHERE the page read and its
// count both apply (the published-only join and the owner predicate).
func favoritesFromWhere(k ContentKind) string {
	return `
		FROM ` + k.favorites + ` f
		JOIN ` + k.table + ` p ON p.id = f.` + k.favoriteFK + `
		WHERE f.user_id = ?
			AND p.status = 'published' AND p.published_at_ms IS NOT NULL`
}

// ListFavorites returns one keyset page of the user's PUBLISHED favorites,
// newest favorite first. Favorites of content that has since been
// archived/unpublished/deleted disappear from the list automatically (the
// join applies the public filter).
func ListFavorites(ctx context.Context, db *sql.DB, k ContentKind, userID string, limit int, after *FavoritePageKey) ([]FavoriteItem, bool, error) {
	// The count subqueries alias the favorites table `fav` in BOTH queries
	// below: the outer query's `f` is the same table (the favorite row being
	// listed), so an inner `f` would shadow it and make the column ambiguous
	// to readers even where SQLite's scoping would accept it.
	// The projection's subtitle slot: a kind whose own column is the display
	// subtitle selects it; projects have none and expose an empty string, so
	// both kinds scan the same shape.
	subtitle := `''`
	if k.hasSubtitle {
		subtitle = "p." + k.ownColumn
	}
	columns := `f.` + k.favoriteFK + `, p.title, ` + subtitle + `, p.description,
		p.thumbnail_url, p.published_at_ms, f.created_at_ms,
		(SELECT COUNT(*) FROM ` + k.comments + ` c WHERE c.` + k.commentFK + ` = p.id),
		(SELECT COUNT(*) FROM ` + k.favorites + ` fav WHERE fav.` + k.favoriteFK + ` = p.id)`
	fromWhere := favoritesFromWhere(k)
	order := `ORDER BY f.created_at_ms DESC, f.` + k.favoriteFK + ` ASC`
	return listFavorites(ctx, db, columns, fromWhere, order, "f."+k.favoriteFK, userID, limit, after)
}

// CountFavorites returns the published-favorite count the list walks — the
// pager's "of N" number; the join and predicate mirror ListFavorites.
func CountFavorites(ctx context.Context, db *sql.DB, k ContentKind, userID string) (int, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*)`+favoritesFromWhere(k), userID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count favorites: %w", err)
	}
	return n, nil
}

func listFavorites(ctx context.Context, db *sql.DB, columns, fromWhere, order, keyCol, userID string, limit int, after *FavoritePageKey) ([]FavoriteItem, bool, error) {
	// Two explicit query shapes keep the keyset predicate visible instead of
	// hiding it behind a NULL-binding trick (same pattern as the content
	// list). The key column name differs per content type, so callers pass
	// it — the rest of the shape is shared.
	var (
		query string
		args  []any
	)
	if after == nil {
		query = `SELECT ` + columns + fromWhere + " " + order + ` LIMIT ?`
		args = []any{userID, limit + 1}
	} else {
		query = `SELECT ` + columns + fromWhere + `
			AND (f.created_at_ms < ? OR (f.created_at_ms = ? AND ` + keyCol + ` > ?))
			` + order + ` LIMIT ?`
		args = []any{userID, after.CreatedAtMS, after.CreatedAtMS, after.ContentID, limit + 1}
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("store: list favorites: %w", err)
	}
	defer rows.Close()

	var items []FavoriteItem
	for rows.Next() {
		var it FavoriteItem
		if err := rows.Scan(
			&it.ID, &it.Title, &it.Subtitle, &it.Description,
			&it.ThumbnailURL, &it.PublishedAtMS, &it.FavoritedAtMS,
			&it.CommentCount, &it.FavoriteCount,
		); err != nil {
			return nil, false, fmt.Errorf("store: scan favorite item: %w", err)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("store: iterate favorites: %w", err)
	}

	hasNext := len(items) > limit
	if hasNext {
		items = items[:limit]
	}
	return items, hasNext, nil
}

// deleteContentFavoritesTx removes every favorite row for one content row
// inside the CALLER'S transaction — the content lifecycle's companion to the
// write that makes it inaccessible.
func deleteContentFavoritesTx(ctx context.Context, tx *sql.Tx, k ContentKind, contentID string) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM `+k.favorites+` WHERE `+k.favoriteFK+` = ?`, contentID); err != nil {
		return fmt.Errorf("store: delete %s favorites: %w", k.noun, err)
	}
	return nil
}

// isForeignKeyViolation reports whether err is a SQLite FOREIGN KEY
// constraint failure (driver text; database/sql has no portable sentinel),
// pinned by the favorite-store canary test alongside IsUniqueViolation.
func isForeignKeyViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}
