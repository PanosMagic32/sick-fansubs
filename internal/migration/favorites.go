package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ImportFavorites imports the legacy favorites arrays into
// the blog_post_favorites / project_favorites join tables. It reads the
// USERS export — the arrays live on the legacy user documents — and MUST run
// after the users and content imports: both resolutions rely on the imported
// rows, checked inside the same transaction as each write. The anchor and
// step-back policy, the skip-reason counts, and the `INSERT OR IGNORE`
// safety argument are the contract in internal/migration/AGENTS.md.
// Each user imports in one transaction (safe units).
func ImportFavorites(ctx context.Context, db *sql.DB, records []LegacyUser) (*ReconciliationReport, error) {
	report := newReport()
	for _, rec := range records {
		if err := importUserFavorites(ctx, db, rec, report); err != nil {
			return report, fmt.Errorf("migration: user record %q: %w", rec.ID.OID, err)
		}
	}
	return report, nil
}

// favoritesTarget names the join table and its resolution columns for one
// content kind. All values are compile-time constants from our own schema —
// no dynamic input reaches the SQL.
type favoritesTarget struct {
	table      string // join table (blog_post_favorites / project_favorites)
	contentCol string // content id column in the join table
	contentTbl string // content table to resolve the derived id against
}

// favoritesKindSet bundles one kind's array, its target, and the report
// counts it feeds — the three always travel together.
type favoritesKindSet struct {
	refs   []MongoObjectID
	target favoritesTarget
	counts *FavoritesCounts
}

// importUserFavorites imports one user's arrays in a single transaction.
func importUserFavorites(ctx context.Context, db *sql.DB, rec LegacyUser, report *ReconciliationReport) error {
	blog := rec.FavoriteBlogPostIds
	projects := rec.FavoriteProjectIds
	if len(blog) == 0 && len(projects) == 0 {
		return nil
	}

	// Source-level counts first: the arrays are accounted for even when the
	// user turns out to be missing (they still existed in the source).
	if len(blog) > 0 {
		report.Favorites.Blog.Users++
		report.Favorites.Blog.SourceRows += len(blog)
	}
	if len(projects) > 0 {
		report.Favorites.Projects.Users++
		report.Favorites.Projects.SourceRows += len(projects)
	}

	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return fmt.Errorf("begin favorites tx: %w", err)
	}
	defer tx.Rollback()

	// The anchor is the IMPORTED users.updated_at_ms (after the createdAt
	// fallback), read inside the transaction — the user cannot disappear between the
	// resolution and the INSERT, so `INSERT OR IGNORE` below can only ever
	// ignore a PK collision.
	var anchorMS int64
	err = tx.QueryRowContext(ctx, `SELECT updated_at_ms FROM users WHERE id = ?`, rec.ID.OID).Scan(&anchorMS)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		report.Favorites.Blog.UserMissing += len(blog)
		report.Favorites.Projects.UserMissing += len(projects)
		return nil // empty tx rolls back via defer
	case err != nil:
		return fmt.Errorf("resolve user: %w", err)
	}

	kindSets := []favoritesKindSet{
		{
			refs: blog,
			target: favoritesTarget{
				table:      "blog_post_favorites",
				contentCol: "blog_post_id",
				contentTbl: "blog_posts",
			},
			counts: &report.Favorites.Blog,
		},
		{
			refs: projects,
			target: favoritesTarget{
				table:      "project_favorites",
				contentCol: "project_id",
				contentTbl: "projects",
			},
			counts: &report.Favorites.Projects,
		},
	}
	for _, set := range kindSets {
		if err := importFavoritesKind(ctx, tx, report, rec.ID.OID, anchorMS, set); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit favorites tx: %w", err)
	}
	return nil
}

// importFavoritesKind writes one kind's array for one user inside the
// caller's transaction.
func importFavoritesKind(ctx context.Context, tx *sql.Tx, report *ReconciliationReport, userID string, anchorMS int64, set favoritesKindSet) error {
	n := len(set.refs)
	for i, ref := range set.refs {
		contentID := deriveTargetID(ref.OID)

		// In-transaction content resolution (see importUserFavorites): the
		// content row cannot vanish before the INSERT, so OR IGNORE never
		// swallows a foreign-key violation. The table name is a compile-time
		// schema constant (favoritesTarget), never dynamic input.
		var one int
		err := tx.QueryRowContext(ctx, "SELECT 1 FROM "+set.target.contentTbl+" WHERE id = ?", contentID).Scan(&one)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			set.counts.ContentMissing++
			continue
		case err != nil:
			return fmt.Errorf("resolve content reference: %w", err)
		}

		// Newest (last) array entry lands on the anchor; earlier entries step
		// back 1 ms each. The floor keeps the created_at_ms > 0 CHECK
		// satisfied for pathological anchors.
		ts := anchorMS - int64(n-1-i)
		if ts < 1 {
			ts = 1
			report.Warnings = append(report.Warnings, MigrationWarning{
				SourceID: userID,
				Field:    "favorites.created_at_ms",
				Detail:   "anchor step-back underflowed; clamped to 1 ms (created_at_ms > 0 CHECK)",
			})
		}

		res, err := tx.ExecContext(ctx,
			"INSERT OR IGNORE INTO "+set.target.table+" (user_id, "+set.target.contentCol+", created_at_ms) VALUES (?, ?, ?)",
			userID, contentID, ts,
		)
		if err != nil {
			return fmt.Errorf("insert favorite: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("favorite rows affected: %w", err)
		}
		if affected == 1 {
			set.counts.Imported++
		} else {
			set.counts.AlreadyPresent++
		}
	}
	return nil
}
