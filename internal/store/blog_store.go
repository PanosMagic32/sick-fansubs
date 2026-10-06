package store

import (
	"context"
	"database/sql"
)

// Public blog reads (docs/patterns/go/content-kinds.md): the list and detail
// projections, driven by the BlogContent descriptor. The shared reader body —
// the public filter, the keyset walk, the user-ref scan tail, the download
// read — lives in content_kinds.go.

// BlogPostSummary is the SQLite row projection for the public blog list: the
// card fields, the live counts, and the creator ref.
type BlogPostSummary struct {
	ID            string
	Title         string
	Subtitle      string
	Description   string
	ThumbnailURL  string
	PublishedAtMS int64
	UpdatedAtMS   int64
	CommentCount  int64
	FavoriteCount int64
	Creator       *UserRef
}

// BlogPostDetail is the SQLite row projection for the public blog detail: it
// carries the detail-only fields the list projection omits — the updater ref
// and the download rows.
type BlogPostDetail struct {
	ID            string
	Title         string
	Subtitle      string
	Description   string
	ThumbnailURL  string
	PublishedAtMS int64
	UpdatedAtMS   int64
	CommentCount  int64
	FavoriteCount int64
	Creator       *UserRef
	Updater       *UserRef
}

// scanBlogPostSummary reads one row of the public list projection.
func scanBlogPostSummary(scan func(dest ...any) error) (BlogPostSummary, error) {
	var (
		p       BlogPostSummary
		creator userRefTail
	)
	if err := scan(&p.ID, &p.Title, &p.Description, &p.Subtitle, &p.ThumbnailURL,
		&p.PublishedAtMS, &p.UpdatedAtMS, &p.CommentCount, &p.FavoriteCount,
		&creator.id, &creator.name, &creator.avatar); err != nil {
		return p, err
	}
	p.Creator = creator.ref()
	return p, nil
}

// scanBlogPostDetail reads one row of the public detail projection.
func scanBlogPostDetail(scan func(dest ...any) error) (BlogPostDetail, error) {
	var (
		p       BlogPostDetail
		creator userRefTail
		updater userRefTail
	)
	if err := scan(&p.ID, &p.Title, &p.Description, &p.Subtitle, &p.ThumbnailURL,
		&p.PublishedAtMS, &p.UpdatedAtMS, &p.CommentCount, &p.FavoriteCount,
		&creator.id, &creator.name, &creator.avatar,
		&updater.id, &updater.name, &updater.avatar); err != nil {
		return p, err
	}
	p.Creator = creator.ref()
	p.Updater = updater.ref()
	return p, nil
}

var (
	blogPostSummaryRead = contentRead[BlogPostSummary]{
		kind:    BlogContent,
		columns: publicListColumns(BlogContent),
		scan:    scanBlogPostSummary,
	}
	blogPostDetailRead = contentRead[BlogPostDetail]{
		kind:    BlogContent,
		columns: publicDetailColumns(BlogContent),
		scan:    scanBlogPostDetail,
	}
)

// GetPublishedBlogPost returns one published blog post with its
// creator/updater refs and download rows, or the masked ErrNotFound — drafts,
// archived posts, posts without a publish time, and unknown ids are
// indistinguishable.
func GetPublishedBlogPost(ctx context.Context, db *sql.DB, id string) (*BlogPostDetail, []Download, error) {
	return getPublishedContent(ctx, db, blogPostDetailRead, id)
}

// ListPublishedBlogPosts returns one keyset page of published blog posts,
// newest first, plus whether another page exists. limit must already satisfy
// the wire contract (1..100; the handler enforces it).
func ListPublishedBlogPosts(ctx context.Context, db *sql.DB, limit int, after *PageKey) ([]BlogPostSummary, bool, error) {
	return listPublishedContent(ctx, db, blogPostSummaryRead, limit, after)
}
