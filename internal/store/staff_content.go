package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"sick-fansubs/internal/search"
)

// Staff content reads (docs/patterns/go/content-kinds.md): both admin stores
// share these readers, and the two content kinds differ only in their
// ContentKind descriptor and their typed row projection. The write
// transactions stay per kind — their contracts (the slug backstop, the
// subtitle/slug slots, the per-kind error text) genuinely differ.

// roleWeightCase returns the SQL mirror of identity.RoleWeight for one role
// column alias: an unknown or NULL role weighs -1, below every real role.
// Callers pass a package-constant alias, never request input.
func roleWeightCase(alias string) string {
	return `CASE ` + alias + `.role
			WHEN 'super-admin' THEN 3
			WHEN 'admin' THEN 2
			WHEN 'moderator' THEN 1
			WHEN 'user' THEN 0
			ELSE -1 END`
}

// creatorRoleWeightCase is the gate's creator-side weight expression (the
// shared projections alias the creator as u).
var creatorRoleWeightCase = roleWeightCase("u")

// staffGateClause returns the draft-visibility predicate every staff read
// carries: published rows, the viewer's own rows, or rows of a strictly lower
// creator role. It binds viewer id then viewer weight.
func staffGateClause() string {
	return `(p.status = 'published' OR p.creator_id = ? OR (` + creatorRoleWeightCase + `) < ?)`
}

// AdminPageKey is the staff-list continuation tuple: (updated_at_ms, id) —
// the staff list sorts by LAST EDIT, not by publish time, so the honest
// field name matters (the FavoritePageKey precedent).
type AdminPageKey struct {
	UpdatedAtMS int64
	ID          string
}

// StaffContentFilter narrows a staff content list: Status is an exact match
// (empty = no filter) and Query matches the folded title, every word of it
// somewhere in the title (empty = no filter). The handler validates both.
type StaffContentFilter struct {
	Status string
	Query  string
}

// writeTitleFilter appends the title filter to a built WHERE clause: one
// folded substring test per query word, ANDed; a query with no words adds no
// clause. The returned slice is the caller's argument list plus the needles.
func writeTitleFilter(where *strings.Builder, args []any, titleExpr, query string) []any {
	for _, needle := range search.FilterTokens(query) {
		fmt.Fprintf(where, " AND instr(%s(%s), ?) > 0", search.FunctionFold, titleExpr)
		args = append(args, needle)
	}
	return args
}

// StaffBlogPost is the staff-read projection: the full row, the raw
// creator/updater ids, and the stored download rows verbatim (no link guard —
// the staff form fixes what is stored).
type StaffBlogPost struct {
	ID            string
	Title         string
	Subtitle      string
	Description   string
	ThumbnailURL  string
	Status        string
	CreatorID     string
	UpdaterID     string
	PublishedAtMS *int64
	CreatedAtMS   int64
	UpdatedAtMS   int64
	Revision      int64
	Creator       *UserRef
	Updater       *UserRef
	Downloads     []Download
}

// StaffBlogPostSummary is the staff-list projection: the summary card plus
// the administration fields (status badge, revision for the edit link).
type StaffBlogPostSummary struct {
	ID            string
	Title         string
	Subtitle      string
	Description   string
	ThumbnailURL  string
	Status        string
	PublishedAtMS *int64
	UpdatedAtMS   int64
	Revision      int64
	Creator       *UserRef
}

// StaffProject is the staff-read projection: the full row, the raw
// creator/updater ids, and the stored download rows verbatim.
type StaffProject struct {
	ID            string
	Title         string
	Description   string
	Slug          string
	ThumbnailURL  string
	Status        string
	CreatorID     string
	UpdaterID     string
	PublishedAtMS *int64
	CreatedAtMS   int64
	UpdatedAtMS   int64
	Revision      int64
	Creator       *UserRef
	Updater       *UserRef
	Downloads     []Download
}

// StaffProjectSummary is the staff-list projection: the summary card plus
// the administration fields (status badge, revision for the edit link).
type StaffProjectSummary struct {
	ID            string
	Title         string
	Description   string
	Slug          string
	ThumbnailURL  string
	Status        string
	PublishedAtMS *int64
	UpdatedAtMS   int64
	Revision      int64
	Creator       *UserRef
}

// staffListColumns returns the staff-list projection: the summary card plus
// status and revision.
func staffListColumns(k ContentKind) string {
	return `p.id, p.title, p.description, p.` + k.ownColumn + `, p.thumbnail_url, p.status,
		p.published_at_ms, p.updated_at_ms, p.revision,
		u.id, u.username, u.avatar_url`
}

// staffDetailColumns returns the staff-detail projection: the full row and
// both user refs.
func staffDetailColumns(k ContentKind) string {
	return `p.id, p.title, p.description, p.` + k.ownColumn + `, p.thumbnail_url, p.status,
		p.creator_id, p.updater_id, p.published_at_ms, p.created_at_ms, p.updated_at_ms, p.revision,
		u.id, u.username, u.avatar_url, u2.id, u2.username, u2.avatar_url`
}

// staffDetailFrom returns the staff-detail FROM/JOIN tail.
func staffDetailFrom(k ContentKind) string {
	return staffListFrom(k) + `
		LEFT JOIN users u2 ON u2.id = p.updater_id`
}

// staffListFrom returns the staff-list FROM/JOIN tail: the list projection
// carries only the creator ref, so it joins `u` alone.
func staffListFrom(k ContentKind) string {
	return `
		FROM ` + k.table + ` p
		LEFT JOIN users u ON u.id = p.creator_id`
}

// scanStaffBlogPost reads one row of the staff-detail projection.
func scanStaffBlogPost(scan func(dest ...any) error) (StaffBlogPost, error) {
	var (
		p            StaffBlogPost
		pub          sql.NullInt64
		rowCreatorID sql.NullString
		rowUpdaterID sql.NullString
		creator      userRefTail
		updater      userRefTail
	)
	if err := scan(&p.ID, &p.Title, &p.Description, &p.Subtitle, &p.ThumbnailURL, &p.Status,
		&rowCreatorID, &rowUpdaterID, &pub, &p.CreatedAtMS, &p.UpdatedAtMS, &p.Revision,
		&creator.id, &creator.name, &creator.avatar,
		&updater.id, &updater.name, &updater.avatar); err != nil {
		return p, err
	}
	if pub.Valid {
		p.PublishedAtMS = &pub.Int64
	}
	// The raw id columns are nullable — migrated rows never get stamped, and
	// user deletion SETs them NULL — so they read through the null family
	// like every other nullable column.
	p.CreatorID = nullStringValue(rowCreatorID)
	p.UpdaterID = nullStringValue(rowUpdaterID)
	p.Creator = creator.ref()
	p.Updater = updater.ref()
	return p, nil
}

// scanStaffBlogPostSummary reads one row of the staff-list projection.
func scanStaffBlogPostSummary(scan func(dest ...any) error) (StaffBlogPostSummary, error) {
	var (
		p       StaffBlogPostSummary
		pub     sql.NullInt64
		creator userRefTail
	)
	if err := scan(&p.ID, &p.Title, &p.Description, &p.Subtitle, &p.ThumbnailURL, &p.Status,
		&pub, &p.UpdatedAtMS, &p.Revision,
		&creator.id, &creator.name, &creator.avatar); err != nil {
		return p, err
	}
	if pub.Valid {
		p.PublishedAtMS = &pub.Int64
	}
	p.Creator = creator.ref()
	return p, nil
}

// scanStaffProject reads one row of the staff-detail projection.
func scanStaffProject(scan func(dest ...any) error) (StaffProject, error) {
	var (
		p            StaffProject
		pub          sql.NullInt64
		rowCreatorID sql.NullString
		rowUpdaterID sql.NullString
		creator      userRefTail
		updater      userRefTail
	)
	if err := scan(&p.ID, &p.Title, &p.Description, &p.Slug, &p.ThumbnailURL, &p.Status,
		&rowCreatorID, &rowUpdaterID, &pub, &p.CreatedAtMS, &p.UpdatedAtMS, &p.Revision,
		&creator.id, &creator.name, &creator.avatar,
		&updater.id, &updater.name, &updater.avatar); err != nil {
		return p, err
	}
	if pub.Valid {
		p.PublishedAtMS = &pub.Int64
	}
	p.CreatorID = nullStringValue(rowCreatorID)
	p.UpdaterID = nullStringValue(rowUpdaterID)
	p.Creator = creator.ref()
	p.Updater = updater.ref()
	return p, nil
}

// scanStaffProjectSummary reads one row of the staff-list projection.
func scanStaffProjectSummary(scan func(dest ...any) error) (StaffProjectSummary, error) {
	var (
		p       StaffProjectSummary
		pub     sql.NullInt64
		creator userRefTail
	)
	if err := scan(&p.ID, &p.Title, &p.Description, &p.Slug, &p.ThumbnailURL, &p.Status,
		&pub, &p.UpdatedAtMS, &p.Revision,
		&creator.id, &creator.name, &creator.avatar); err != nil {
		return p, err
	}
	if pub.Valid {
		p.PublishedAtMS = &pub.Int64
	}
	p.Creator = creator.ref()
	return p, nil
}

var (
	staffBlogPostRead = contentRead[StaffBlogPost]{
		kind:    BlogContent,
		columns: staffDetailColumns(BlogContent),
		scan:    scanStaffBlogPost,
	}
	staffBlogPostSummaryRead = contentRead[StaffBlogPostSummary]{
		kind:    BlogContent,
		columns: staffListColumns(BlogContent),
		scan:    scanStaffBlogPostSummary,
	}
	staffProjectRead = contentRead[StaffProject]{
		kind:    ProjectContent,
		columns: staffDetailColumns(ProjectContent),
		scan:    scanStaffProject,
	}
	staffProjectSummaryRead = contentRead[StaffProjectSummary]{
		kind:    ProjectContent,
		columns: staffListColumns(ProjectContent),
		scan:    scanStaffProjectSummary,
	}
)

// getStaffContent returns one row under the staff-visibility gate, or the
// masked ErrNotFound.
func getStaffContent[T any](ctx context.Context, db *sql.DB, r contentRead[T], contentID, viewerID string, viewerRoleWeight int) (*T, error) {
	row := db.QueryRowContext(ctx,
		`SELECT `+r.columns+staffDetailFrom(r.kind)+`
		WHERE p.id = ? AND `+staffGateClause(), contentID, viewerID, viewerRoleWeight)
	item, err := r.scan(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: get staff %s: %w", r.kind.noun, err)
	}
	return &item, nil
}

// staffListWhere builds the staff list's shared predicate — the gate, the
// optional filters, and (when after is set) the keyset continuation — with
// its bind args. The page read and the count share this one source.
func staffListWhere(k ContentKind, viewerID string, viewerRoleWeight int, filter StaffContentFilter, after *AdminPageKey) (string, []any) {
	var where strings.Builder
	where.WriteString(staffListFrom(k))
	where.WriteString(`
		WHERE `)
	where.WriteString(staffGateClause())
	args := []any{viewerID, viewerRoleWeight}

	if filter.Status != "" {
		where.WriteString(` AND p.status = ?`)
		args = append(args, filter.Status)
	}
	args = writeTitleFilter(&where, args, "p.title", filter.Query)
	if after != nil {
		where.WriteString(` AND (p.updated_at_ms < ? OR (p.updated_at_ms = ? AND p.id > ?))`)
		args = append(args, after.UpdatedAtMS, after.UpdatedAtMS, after.ID)
	}
	return where.String(), args
}

// listStaffContent returns one keyset page of the staff list: all statuses,
// ordered updated_at_ms DESC, id ASC, under the staff-visibility gate and the
// caller's optional filter. A filter never changes the cursor tuple.
func listStaffContent[T any](ctx context.Context, db *sql.DB, r contentRead[T], limit int, after *AdminPageKey, viewerID string, viewerRoleWeight int, filter StaffContentFilter) ([]T, bool, error) {
	where, args := staffListWhere(r.kind, viewerID, viewerRoleWeight, filter, after)
	query := `SELECT ` + r.columns + where + `
		ORDER BY p.updated_at_ms DESC, p.id ASC LIMIT ?`
	args = append(args, limit+1)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("store: list staff %ss: %w", r.kind.noun, err)
	}
	defer rows.Close()

	var items []T
	for rows.Next() {
		item, err := r.scan(rows.Scan)
		if err != nil {
			return nil, false, fmt.Errorf("store: scan staff %s: %w", r.kind.noun, err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("store: iterate staff %ss: %w", r.kind.noun, err)
	}

	hasNext := len(items) > limit
	if hasNext {
		items = items[:limit]
	}
	return items, hasNext, nil
}

// GetStaffBlogPost returns one blog post under the staff-visibility gate, or
// the masked ErrNotFound; invisible and unknown rows are indistinguishable.
func GetStaffBlogPost(ctx context.Context, db *sql.DB, id, viewerID string, viewerRoleWeight int) (*StaffBlogPost, error) {
	post, err := getStaffContent(ctx, db, staffBlogPostRead, id, viewerID, viewerRoleWeight)
	if err != nil {
		return nil, err
	}
	post.Downloads, err = listContentDownloads(ctx, db, BlogContent, post.ID)
	if err != nil {
		return nil, err
	}
	return post, nil
}

// ListStaffBlogPosts returns one keyset page of the staff blog list.
func ListStaffBlogPosts(ctx context.Context, db *sql.DB, limit int, after *AdminPageKey, viewerID string, viewerRoleWeight int, filter StaffContentFilter) ([]StaffBlogPostSummary, bool, error) {
	return listStaffContent(ctx, db, staffBlogPostSummaryRead, limit, after, viewerID, viewerRoleWeight, filter)
}

// GetStaffProject returns one project under the staff-visibility gate (the
// GetStaffBlogPost contract).
func GetStaffProject(ctx context.Context, db *sql.DB, id, viewerID string, viewerRoleWeight int) (*StaffProject, error) {
	project, err := getStaffContent(ctx, db, staffProjectRead, id, viewerID, viewerRoleWeight)
	if err != nil {
		return nil, err
	}
	project.Downloads, err = listContentDownloads(ctx, db, ProjectContent, project.ID)
	if err != nil {
		return nil, err
	}
	return project, nil
}

// ListStaffProjects returns one keyset page of the staff project list.
func ListStaffProjects(ctx context.Context, db *sql.DB, limit int, after *AdminPageKey, viewerID string, viewerRoleWeight int, filter StaffContentFilter) ([]StaffProjectSummary, bool, error) {
	return listStaffContent(ctx, db, staffProjectSummaryRead, limit, after, viewerID, viewerRoleWeight, filter)
}

// CountStaffBlogPosts returns the staff-visible blog row count the list walks
// — the pager's "of N" number, under the same gate and filters.
func CountStaffBlogPosts(ctx context.Context, db *sql.DB, viewerID string, viewerRoleWeight int, filter StaffContentFilter) (int, error) {
	return countStaffContent(ctx, db, staffBlogPostSummaryRead, viewerID, viewerRoleWeight, filter)
}

// CountStaffProjects returns the staff-visible project row count.
func CountStaffProjects(ctx context.Context, db *sql.DB, viewerID string, viewerRoleWeight int, filter StaffContentFilter) (int, error) {
	return countStaffContent(ctx, db, staffProjectSummaryRead, viewerID, viewerRoleWeight, filter)
}

// countStaffContent counts the rows the staff list walks; the count shares
// staffListWhere and carries no keyset (after is nil).
func countStaffContent[T any](ctx context.Context, db *sql.DB, r contentRead[T], viewerID string, viewerRoleWeight int, filter StaffContentFilter) (int, error) {
	where, args := staffListWhere(r.kind, viewerID, viewerRoleWeight, filter, nil)
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*)`+where, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count staff %ss: %w", r.kind.noun, err)
	}
	return n, nil
}

// getStaffBlogPostUngated reads one blog post with NO visibility gate — the
// write paths' read-back for the row they just created/updated. Never expose
// it to request handling.
func getStaffBlogPostUngated(ctx context.Context, db *sql.DB, id string) (*StaffBlogPost, error) {
	post, err := getUngatedContent(ctx, db, staffBlogPostRead, id)
	if err != nil {
		return nil, err
	}
	post.Downloads, err = listContentDownloads(ctx, db, BlogContent, post.ID)
	if err != nil {
		return nil, err
	}
	return post, nil
}

// getStaffProjectUngated is the project counterpart of
// getStaffBlogPostUngated.
func getStaffProjectUngated(ctx context.Context, db *sql.DB, id string) (*StaffProject, error) {
	project, err := getUngatedContent(ctx, db, staffProjectRead, id)
	if err != nil {
		return nil, err
	}
	project.Downloads, err = listContentDownloads(ctx, db, ProjectContent, project.ID)
	if err != nil {
		return nil, err
	}
	return project, nil
}

// getUngatedContent reads one row with no visibility gate — the write paths'
// own read-back.
func getUngatedContent[T any](ctx context.Context, db *sql.DB, r contentRead[T], contentID string) (*T, error) {
	row := db.QueryRowContext(ctx,
		`SELECT `+r.columns+staffDetailFrom(r.kind)+`
		WHERE p.id = ?`, contentID)
	item, err := r.scan(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: get staff %s: %w", r.kind.noun, err)
	}
	return &item, nil
}
