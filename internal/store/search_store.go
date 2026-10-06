package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"sick-fansubs/internal/search"
)

// SearchScope selects which content types a search covers:
// type=all|posts|projects.
type SearchScope int

const (
	SearchAll SearchScope = iota
	SearchPosts
	SearchProjects
)

// SearchSort selects the ordering of a merged search page: by publish time
// newest first (the shipped default), by publish time oldest first, or
// alphabetically by title through the sf_greek collation.
type SearchSort int

const (
	SearchByDate SearchSort = iota
	SearchByTitle
	SearchByOldest
)

// SearchKeyset is the decoded continuation tuple of one search page: the
// slots the active sort reads — (PublishedAtMS, ID) for both date sorts,
// (TitleKey, ID) for the title sort. The handler owns the wire cursor.
type SearchKeyset struct {
	PublishedAtMS int64
	TitleKey      string
	ID            string
}

// SearchQuery is one store-level search request: match, scope, sort, window,
// limit, and keyset travel as one named shape instead of a positional list.
//
// Every field is caller-validated and caller-bounded: the store trusts the
// handler's wire contract (match built by search.BuildQuery, scope/sort from
// closed sets, Limit within 1..100, window bounds parsed from strict dates)
// and binds each value as a parameter.
type SearchQuery struct {
	// Match is the built FTS5 MATCH argument (search.BuildQuery).
	Match string
	// Scope selects which content types are searched.
	Scope SearchScope
	// Sort selects the ordering and which keyset slots After reads.
	Sort SearchSort
	// SinceMS is an inclusive lower bound on published_at_ms; nil is
	// unbounded. A pointer, not a sentinel: 1970-01-01 is a legal bound and
	// its millisecond value is 0 (a zero sentinel would silently
	// disable the filter for the epoch day).
	SinceMS *int64
	// UntilMS is an EXCLUSIVE upper bound on published_at_ms; nil is
	// unbounded. The handler converts an inclusive UTC end date into the next
	// day's start.
	UntilMS *int64
	// Limit must already satisfy the wire contract (1..100).
	Limit int
	// After is nil for the first page; non-nil carries the last item of the
	// previous page.
	After *SearchKeyset
}

// SearchResult is the SQLite projection of one merged search hit. The two
// content types share this row shape: projects have no subtitle column, so
// their branch selects an empty string and every scan reads the same slots.
type SearchResult struct {
	Type          string // "post" | "project" — the public discriminator
	ID            string
	Title         string
	Subtitle      string // blog posts only; empty for projects
	Description   string
	ThumbnailURL  string
	PublishedAtMS int64
	CommentCount  int64
	FavoriteCount int64
}

// searchBranches builds the per-kind FTS branches one search request covers,
// with the MATCH bind arguments in branch order — the one source the page
// read and the count share. The public filter applies in every branch.
//
// The branch projection and table names differ per kind; MATCH cannot be
// used with a table alias, so a branch names the real FTS table and aliases
// only the content table. The type name is the wire discriminator.
func searchBranches(q SearchQuery) ([]string, []any) {
	// The merged projection's display-subtitle slot: blog posts select their
	// own column, projects an aliased empty string (they have none), so every
	// branch scans the same shape.
	blogSubtitle := `p.` + BlogContent.ownColumn + ` AS subtitle`
	projectSubtitle := `'' AS subtitle`
	searchBranch := func(k ContentKind, typeName, subtitle string) string {
		return `SELECT '` + typeName + `' AS type, p.id, p.title, ` + subtitle + `, p.description, p.thumbnail_url, p.published_at_ms,
			(SELECT COUNT(*) FROM ` + k.comments + ` c WHERE c.` + k.commentFK + ` = p.id) AS comment_count,
			(SELECT COUNT(*) FROM ` + k.favorites + ` f WHERE f.` + k.favoriteFK + ` = p.id) AS favorite_count
			FROM ` + k.searchIndex + `
			JOIN ` + k.table + ` p ON p.rowid = ` + k.searchIndex + `.rowid
			WHERE ` + k.searchIndex + ` MATCH ? AND p.status = 'published' AND p.published_at_ms IS NOT NULL`
	}

	var branches []string
	switch q.Scope {
	case SearchPosts:
		branches = []string{searchBranch(BlogContent, "post", blogSubtitle)}
	case SearchProjects:
		branches = []string{searchBranch(ProjectContent, "project", projectSubtitle)}
	default:
		branches = []string{searchBranch(BlogContent, "post", blogSubtitle), searchBranch(ProjectContent, "project", projectSubtitle)}
	}

	args := make([]any, 0, len(branches)+2)
	for range branches {
		args = append(args, q.Match)
	}
	return branches, args
}

// searchWindowConditions appends the publication-window conditions to conds
// and args; the keyset predicate is NOT part of the window — callers add it
// under the active sort.
func searchWindowConditions(q SearchQuery, conds []string, args []any) ([]string, []any) {
	if q.SinceMS != nil {
		conds = append(conds, `published_at_ms >= ?`)
		args = append(args, *q.SinceMS)
	}
	if q.UntilMS != nil {
		conds = append(conds, `published_at_ms < ?`)
		args = append(args, *q.UntilMS)
	}
	return conds, args
}

// SearchContent returns one keyset page of merged search results plus
// whether another page exists; the store fetches limit+1 rows, so
// hasNextPage costs no extra query (CountSearchContent supplies the total).
//
// Every MATCH and window/keyset value is a bound parameter; the index is a
// matching hint only — the branches re-apply the public filter at query
// time. A keyset is meaningful only under the same q+type+window, and the
// title sort's cursor additionally carries a collation key.
func SearchContent(ctx context.Context, db *sql.DB, q SearchQuery) ([]SearchResult, bool, error) {
	branches, args := searchBranches(q)

	// MATCH cannot be used with a table alias — the branches use the real FTS
	// table names, so aliasing is confined to the content tables.
	var sb strings.Builder
	sb.WriteString(`SELECT type, id, title, subtitle, description, thumbnail_url, published_at_ms, comment_count, favorite_count FROM (`)
	sb.WriteString(strings.Join(branches, " UNION ALL "))
	sb.WriteString(`)`)

	// The publication window and the keyset predicate are bound-parameter
	// conditions layered onto the union. The collation name is a package
	// constant of internal/search, never user input, so splicing it is not an
	// injection path — every VALUE below stays a parameter.
	const titleSort = `title COLLATE ` + search.CollationGreek
	conds, args := searchWindowConditions(q, nil, args)
	if q.Sort == SearchByTitle {
		if q.After != nil {
			conds = append(conds, `(`+titleSort+` > ? OR (`+titleSort+` = ? AND id > ?))`)
			args = append(args, q.After.TitleKey, q.After.TitleKey, q.After.ID)
		}
	} else if q.After != nil {
		// Both date sorts read the same tuple; the direction of the ordering
		// decides which side of it continues the walk.
		if q.Sort == SearchByOldest {
			conds = append(conds, `(published_at_ms > ? OR (published_at_ms = ? AND id > ?))`)
		} else {
			conds = append(conds, `(published_at_ms < ? OR (published_at_ms = ? AND id > ?))`)
		}
		args = append(args, q.After.PublishedAtMS, q.After.PublishedAtMS, q.After.ID)
	}
	if len(conds) > 0 {
		sb.WriteString(` WHERE `)
		sb.WriteString(strings.Join(conds, ` AND `))
	}
	if q.Sort == SearchByTitle {
		fmt.Fprintf(&sb, " ORDER BY %s ASC, id ASC LIMIT ?", titleSort)
	} else if q.Sort == SearchByOldest {
		sb.WriteString(` ORDER BY published_at_ms ASC, id ASC LIMIT ?`)
	} else {
		sb.WriteString(` ORDER BY published_at_ms DESC, id ASC LIMIT ?`)
	}
	args = append(args, q.Limit+1)

	rows, err := db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, false, fmt.Errorf("store: search content: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.Type, &r.ID, &r.Title, &r.Subtitle, &r.Description, &r.ThumbnailURL, &r.PublishedAtMS, &r.CommentCount, &r.FavoriteCount); err != nil {
			return nil, false, fmt.Errorf("store: scan search result: %w", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("store: iterate search results: %w", err)
	}

	// The limit+1th row only proves a next page exists; it is not part of
	// this page's items.
	hasNext := len(results) > q.Limit
	if hasNext {
		results = results[:q.Limit]
	}
	return results, hasNext, nil
}

// CountSearchContent returns the filtered row count the search page walks —
// the pager's "of N" number. It shares the branches and the publication
// window with SearchContent and applies no keyset and no limit.
func CountSearchContent(ctx context.Context, db *sql.DB, q SearchQuery) (int, error) {
	branches, args := searchBranches(q)
	conds, args := searchWindowConditions(q, nil, args)
	query := `SELECT COUNT(*) FROM (` + strings.Join(branches, " UNION ALL ") + `)`
	if len(conds) > 0 {
		query += ` WHERE ` + strings.Join(conds, ` AND `)
	}
	var n int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count search content: %w", err)
	}
	return n, nil
}

// IndexBlogPostTx indexes one blog post row into the blog search index, inside
// the caller's transaction, right after the content INSERT. Callers index only
// rows with a publish time.
func IndexBlogPostTx(ctx context.Context, tx *sql.Tx, rowid int64, title, subtitle, description string) error {
	return insertSearchRowTx(ctx, tx, BlogContent, rowid, search.Text(title, subtitle, description))
}

// IndexProjectTx indexes one project row (title, description — projects have
// no subtitle) into the project search index. Same contract as
// IndexBlogPostTx.
func IndexProjectTx(ctx context.Context, tx *sql.Tx, rowid int64, title, description string) error {
	return insertSearchRowTx(ctx, tx, ProjectContent, rowid, search.Text(title, description))
}

func insertSearchRowTx(ctx context.Context, tx *sql.Tx, k ContentKind, rowid int64, text string) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO `+k.searchIndex+`(rowid, text) VALUES (?, ?)`, rowid, text); err != nil {
		return fmt.Errorf("store: index %s search: %w", k.noun, err)
	}
	return nil
}

// DeleteBlogPostTx removes one blog post's index entry — the content-row
// delete's companion (FTS5 supports plain DELETE by rowid; the
// external-content 'delete' command is not used).
func DeleteBlogPostTx(ctx context.Context, tx *sql.Tx, rowid int64) error {
	return deleteSearchRowTx(ctx, tx, BlogContent, rowid)
}

// DeleteProjectTx removes one project's index entry (see DeleteBlogPostTx).
func DeleteProjectTx(ctx context.Context, tx *sql.Tx, rowid int64) error {
	return deleteSearchRowTx(ctx, tx, ProjectContent, rowid)
}

func deleteSearchRowTx(ctx context.Context, tx *sql.Tx, k ContentKind, rowid int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+k.searchIndex+` WHERE rowid = ?`, rowid); err != nil {
		return fmt.Errorf("store: delete %s search entry: %w", k.noun, err)
	}
	return nil
}

// ReindexAll rebuilds both FTS tables from the content tables in one
// transaction and returns how many rows each index received. It is the
// restore-rebuild path and the backfill for
// existing dev databases after migration 0005.
//
// The text must be normalized in Go (SQL cannot strip Greek accents), so
// this reads every publishable row, computes its search text, and refills
// the index — it cannot be expressed as a single INSERT…SELECT.
func ReindexAll(ctx context.Context, db *sql.DB) (blogCount, projectCount int, err error) {
	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return 0, 0, fmt.Errorf("store: begin reindex: %w", err)
	}
	defer tx.Rollback()

	for _, k := range allContentKinds {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+k.searchIndex); err != nil {
			return 0, 0, fmt.Errorf("store: clear %s search index: %w", k.label, err)
		}
	}

	blogRows, err := tx.QueryContext(ctx,
		`SELECT rowid, title, subtitle, description FROM `+BlogContent.table+` WHERE published_at_ms IS NOT NULL`)
	if err != nil {
		return 0, 0, fmt.Errorf("store: read %ss for reindex: %w", BlogContent.noun, err)
	}
	for blogRows.Next() {
		var rowid int64
		var title, subtitle, description string
		if err := blogRows.Scan(&rowid, &title, &subtitle, &description); err != nil {
			blogRows.Close()
			return 0, 0, fmt.Errorf("store: scan %s for reindex: %w", BlogContent.noun, err)
		}
		if err := IndexBlogPostTx(ctx, tx, rowid, title, subtitle, description); err != nil {
			blogRows.Close()
			return 0, 0, err
		}
		blogCount++
	}
	if err := blogRows.Err(); err != nil {
		blogRows.Close()
		return 0, 0, fmt.Errorf("store: iterate %ss for reindex: %w", BlogContent.noun, err)
	}
	blogRows.Close()

	projectRows, err := tx.QueryContext(ctx,
		`SELECT rowid, title, description FROM `+ProjectContent.table+` WHERE published_at_ms IS NOT NULL`)
	if err != nil {
		return 0, 0, fmt.Errorf("store: read %ss for reindex: %w", ProjectContent.noun, err)
	}
	for projectRows.Next() {
		var rowid int64
		var title, description string
		if err := projectRows.Scan(&rowid, &title, &description); err != nil {
			projectRows.Close()
			return 0, 0, fmt.Errorf("store: scan %s for reindex: %w", ProjectContent.noun, err)
		}
		if err := IndexProjectTx(ctx, tx, rowid, title, description); err != nil {
			projectRows.Close()
			return 0, 0, err
		}
		projectCount++
	}
	if err := projectRows.Err(); err != nil {
		projectRows.Close()
		return 0, 0, fmt.Errorf("store: iterate %ss for reindex: %w", ProjectContent.noun, err)
	}
	projectRows.Close()

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("store: commit reindex: %w", err)
	}
	return blogCount, projectCount, nil
}
