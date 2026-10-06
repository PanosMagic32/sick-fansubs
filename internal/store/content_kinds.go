package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"sick-fansubs/internal/id"
)

// Content-kind descriptors (docs/patterns/go/content-kinds.md): one
// ContentKind value holds every per-content-type storage name in this
// package, and the readers take the value instead of a second copy of the
// SQL. The field values are package constants — never request input — so
// interpolating them into query strings carries no injection risk.

// ContentKind describes one content type's storage names plus the two
// vocabulary strings the shared error text is built from.
type ContentKind struct {
	// kind is the wire and storage discriminator ("blog-posts" | "projects")
	// — the user_notifications.content_kind and content_follows.content_kind
	// value, and the URL segment the handlers serve the kind under.
	kind string
	// label names the kind bare in error text ("blog" | "project").
	label string
	// noun names one content row in error text ("blog post" | "project").
	noun string
	// ownColumn is the one content column only this kind has ("subtitle" |
	// "slug"); every projection selects it in one fixed position.
	ownColumn string
	// hasSubtitle marks ownColumn as the display subtitle: the shared card
	// rows select it into their subtitle slot, and a kind without one exposes
	// the empty string there.
	hasSubtitle bool

	table         string // content rows
	comments      string // comment rows
	commentFK     string // comment rows' content column
	hearts        string // comment heart rows
	favorites     string // favorite rows
	favoriteFK    string // favorite rows' content column
	downloads     string // download rows
	downloadFK    string // download rows' content column
	downloadLabel string // download rows' display-label column
	searchIndex   string // the FTS5 index table
}

// Kind returns the kind's wire and storage discriminator.
func (k ContentKind) Kind() string { return k.kind }

// BlogContent and ProjectContent are the store's content kinds. A new kind
// adds a value here and joins allContentKinds, so the sweeps that must cover
// every kind (the media reference set, the metrics totals) follow it.
var (
	BlogContent = ContentKind{
		kind:          "blog-posts",
		label:         "blog",
		noun:          "blog post",
		ownColumn:     "subtitle",
		hasSubtitle:   true,
		table:         "blog_posts",
		comments:      "blog_post_comments",
		commentFK:     "blog_post_id",
		hearts:        "blog_post_comment_hearts",
		favorites:     "blog_post_favorites",
		favoriteFK:    "blog_post_id",
		downloads:     "blog_post_downloads",
		downloadFK:    "blog_post_id",
		downloadLabel: "resolution",
		searchIndex:   "blog_post_search",
	}
	ProjectContent = ContentKind{
		kind:          "projects",
		label:         "project",
		noun:          "project",
		ownColumn:     "slug",
		hasSubtitle:   false,
		table:         "projects",
		comments:      "project_comments",
		commentFK:     "project_id",
		hearts:        "project_comment_hearts",
		favorites:     "project_favorites",
		favoriteFK:    "project_id",
		downloads:     "project_downloads",
		downloadFK:    "project_id",
		downloadLabel: "name",
		searchIndex:   "project_search",
	}

	// allContentKinds is the canonical order of the sweeps that must cover
	// every kind.
	allContentKinds = []ContentKind{BlogContent, ProjectContent}
)

// UserRef is the user projection on content responses: id, username, and
// avatar (nullable). Role deliberately stays out — it is account state, and
// public content must not reveal which users hold privileged roles.
type UserRef struct {
	ID        string
	Username  string
	AvatarURL *string
}

// Download is one normalized download row. Label carries the kind's display
// column — the resolution on a blog post, the batch name on a project; the
// link fields are nil when SQLite holds NULL for that slot.
type Download struct {
	Label       string
	MagnetLink  *string
	TorrentLink *string
}

// publicPredicate is the public-read condition: drafts, archived rows, and
// unstamped rows are invisible, and the masked ErrNotFound makes them
// indistinguishable from unknown ids. Every use aliases the content table p.
const publicPredicate = `p.status = 'published' AND p.published_at_ms IS NOT NULL`

// publicFilter appends the public-read condition to a WHERE clause that
// already carries one (the detail read's `WHERE p.id = ?`).
const publicFilter = ` AND ` + publicPredicate

// publicListColumns returns the public list projection: the card fields, the
// live counts, and the creator ref. The own column sits in one fixed position
// for every kind, and the scans follow this order.
func publicListColumns(k ContentKind) string {
	return `p.id, p.title, p.description, p.` + k.ownColumn + `, p.thumbnail_url,
		p.published_at_ms, p.updated_at_ms,
		(SELECT COUNT(*) FROM ` + k.comments + ` c WHERE c.` + k.commentFK + ` = p.id),
		(SELECT COUNT(*) FROM ` + k.favorites + ` f WHERE f.` + k.favoriteFK + ` = p.id),
		u.id, u.username, u.avatar_url`
}

// publicDetailColumns returns the public detail projection: the list fields
// plus the updater ref.
func publicDetailColumns(k ContentKind) string {
	return `p.id, p.title, p.description, p.` + k.ownColumn + `, p.thumbnail_url,
		p.published_at_ms, p.updated_at_ms,
		(SELECT COUNT(*) FROM ` + k.comments + ` c WHERE c.` + k.commentFK + ` = p.id),
		(SELECT COUNT(*) FROM ` + k.favorites + ` f WHERE f.` + k.favoriteFK + ` = p.id),
		u.id, u.username, u.avatar_url, u2.id, u2.username, u2.avatar_url`
}

// publicListFrom and publicDetailFrom return the FROM/JOIN tail of their
// projections; both join users with LEFT joins, so a NULL id column or a
// deleted user yields a nil ref (ON DELETE SET NULL).
func publicListFrom(k ContentKind) string {
	return `
		FROM ` + k.table + ` p
		LEFT JOIN users u ON u.id = p.creator_id`
}

func publicDetailFrom(k ContentKind) string {
	return publicListFrom(k) + `
		LEFT JOIN users u2 ON u2.id = p.updater_id`
}

// userRefTail is the nullable (id, username, avatar) scan target of one
// user-ref triple. database/sql takes one Scan call per row, so the tail's
// destinations join the row's in a single call.
type userRefTail struct {
	id     sql.NullString
	name   sql.NullString
	avatar sql.NullString
}

// ref returns the projection for the triple, or nil when the id column is
// NULL — no creator stamped (migrated rows), or a deleted user
// (ON DELETE SET NULL).
func (t userRefTail) ref() *UserRef {
	if !t.id.Valid {
		return nil
	}
	return &UserRef{ID: t.id.String, Username: t.name.String, AvatarURL: nullStringPtr(t.avatar)}
}

// listContentDownloads reads one content row's download rows in display
// order (position, then id). The rows are the STORED values verbatim; the
// public read masks its links at the wire boundary.
func listContentDownloads(ctx context.Context, db *sql.DB, k ContentKind, contentID string) ([]Download, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT `+k.downloadLabel+`, magnet_link, torrent_link
		 FROM `+k.downloads+`
		 WHERE `+k.downloadFK+` = ?
		 ORDER BY position, id`, contentID)
	if err != nil {
		return nil, fmt.Errorf("store: list %s downloads: %w", k.noun, err)
	}
	defer rows.Close()

	var downloads []Download
	for rows.Next() {
		var d Download
		var magnet, torrent sql.NullString
		if err := rows.Scan(&d.Label, &magnet, &torrent); err != nil {
			return nil, fmt.Errorf("store: scan %s download: %w", k.noun, err)
		}
		d.MagnetLink = nullStringPtr(magnet)
		d.TorrentLink = nullStringPtr(torrent)
		downloads = append(downloads, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate %s downloads: %w", k.noun, err)
	}
	return downloads, nil
}

// insertContentDownloadsTx inserts one download-row set (create path) inside
// the caller's transaction: a fresh id per row, position = array index, and
// the content row's created_at_ms.
func insertContentDownloadsTx(ctx context.Context, tx *sql.Tx, k ContentKind, contentID string, createdMS int64, downloads []Download) error {
	for i, d := range downloads {
		downloadID, err := id.New()
		if err != nil {
			return fmt.Errorf("store: %s download id: %w", k.label, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO `+k.downloads+` (id, `+k.downloadFK+`, `+k.downloadLabel+`, magnet_link, torrent_link, position, created_at_ms)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			downloadID, contentID, d.Label, d.MagnetLink, d.TorrentLink, i, createdMS); err != nil {
			return fmt.Errorf("store: insert %s download: %w", k.label, err)
		}
	}
	return nil
}

// replaceContentDownloadsTx swaps one content row's download set (update
// path): delete the old rows, insert the submitted ones. Callers run it only
// after the revision-guarded UPDATE matched.
func replaceContentDownloadsTx(ctx context.Context, tx *sql.Tx, k ContentKind, contentID string, createdMS int64, downloads []Download) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM `+k.downloads+` WHERE `+k.downloadFK+` = ?`, contentID); err != nil {
		return fmt.Errorf("store: clear %s downloads: %w", k.label, err)
	}
	return insertContentDownloadsTx(ctx, tx, k, contentID, createdMS, downloads)
}

// contentRead binds one content kind to one row projection: the descriptor,
// the kind-specific SELECT columns, and the scan of one row. The scan must
// read exactly the columns its list produced, in order.
type contentRead[T any] struct {
	kind    ContentKind
	columns string
	scan    func(scan func(dest ...any) error) (T, error)
}

// getPublishedContent returns one published row with its download rows, or
// the masked ErrNotFound. The public filter lives in the query, so unknown
// ids, drafts, archived rows, and unstamped rows are indistinguishable.
func getPublishedContent[T any](ctx context.Context, db *sql.DB, r contentRead[T], contentID string) (*T, []Download, error) {
	row := db.QueryRowContext(ctx,
		`SELECT `+r.columns+publicDetailFrom(r.kind)+`
		WHERE p.id = ?`+publicFilter, contentID)
	item, err := r.scan(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("store: get published %s: %w", r.kind.noun, err)
	}
	downloads, err := listContentDownloads(ctx, db, r.kind, contentID)
	if err != nil {
		return nil, nil, err
	}
	return &item, downloads, nil
}

// publishedFromWhere is the published-list FROM/WHERE the page read and its
// count both apply (the creator join and the public filter).
func publishedFromWhere(k ContentKind) string {
	return publicListFrom(k) + `
		WHERE ` + publicPredicate
}

// listPublishedContent returns one keyset page of published rows, newest
// first, plus whether another page exists. The store fetches limit+1 rows so
// hasNextPage needs no separate count query.
func listPublishedContent[T any](ctx context.Context, db *sql.DB, r contentRead[T], limit int, after *PageKey) ([]T, bool, error) {
	// Two explicit query shapes keep the keyset predicate visible instead of
	// hiding it behind a NULL-binding trick: the first page has no
	// continuation predicate; later pages apply the strict "after" tuple.
	fromWhere := publishedFromWhere(r.kind)
	var (
		query string
		args  []any
	)
	if after == nil {
		query = `SELECT ` + r.columns + fromWhere + `
		ORDER BY p.published_at_ms DESC, p.id ASC LIMIT ?`
		args = []any{limit + 1}
	} else {
		query = `SELECT ` + r.columns + fromWhere + `
		AND (p.published_at_ms < ? OR (p.published_at_ms = ? AND p.id > ?))
		ORDER BY p.published_at_ms DESC, p.id ASC LIMIT ?`
		args = []any{after.PublishedAtMS, after.PublishedAtMS, after.ID, limit + 1}
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("store: list published %ss: %w", r.kind.noun, err)
	}
	defer rows.Close()

	var items []T
	for rows.Next() {
		item, err := r.scan(rows.Scan)
		if err != nil {
			return nil, false, fmt.Errorf("store: scan %s: %w", r.kind.noun, err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("store: iterate %ss: %w", r.kind.noun, err)
	}

	hasNext := len(items) > limit
	if hasNext {
		items = items[:limit]
	}
	return items, hasNext, nil
}

// CountPublishedContent returns the published row count the public list walks
// — the pager's "of N" number. The WHERE mirrors listPublishedContent.
func CountPublishedContent(ctx context.Context, db *sql.DB, k ContentKind) (int, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*)`+publishedFromWhere(k)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count published %ss: %w", k.noun, err)
	}
	return n, nil
}
