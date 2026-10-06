package store

import (
	"context"
	"database/sql"
)

// Public project reads (docs/patterns/go/content-kinds.md): the blog store's
// counterparts, driven by the ProjectContent descriptor. The shared reader
// body lives in content_kinds.go.

// ProjectSummary is the SQLite row projection for the public project list.
// The projects table has no subtitle column, so the list card carries slug
// and description instead.
type ProjectSummary struct {
	ID            string
	Title         string
	Description   string
	Slug          string
	ThumbnailURL  string
	PublishedAtMS int64
	UpdatedAtMS   int64
	CommentCount  int64
	FavoriteCount int64
	Creator       *UserRef
}

// ProjectDetail is the SQLite row projection for the public project detail:
// it carries the detail-only updater ref (downloaded rows come from the
// shared download read).
type ProjectDetail struct {
	ID            string
	Title         string
	Description   string
	Slug          string
	ThumbnailURL  string
	PublishedAtMS int64
	UpdatedAtMS   int64
	CommentCount  int64
	FavoriteCount int64
	Creator       *UserRef
	Updater       *UserRef
}

// scanProjectSummary reads one row of the public list projection.
func scanProjectSummary(scan func(dest ...any) error) (ProjectSummary, error) {
	var (
		p       ProjectSummary
		creator userRefTail
	)
	if err := scan(&p.ID, &p.Title, &p.Description, &p.Slug, &p.ThumbnailURL,
		&p.PublishedAtMS, &p.UpdatedAtMS, &p.CommentCount, &p.FavoriteCount,
		&creator.id, &creator.name, &creator.avatar); err != nil {
		return p, err
	}
	p.Creator = creator.ref()
	return p, nil
}

// scanProjectDetail reads one row of the public detail projection.
func scanProjectDetail(scan func(dest ...any) error) (ProjectDetail, error) {
	var (
		p       ProjectDetail
		creator userRefTail
		updater userRefTail
	)
	if err := scan(&p.ID, &p.Title, &p.Description, &p.Slug, &p.ThumbnailURL,
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
	projectSummaryRead = contentRead[ProjectSummary]{
		kind:    ProjectContent,
		columns: publicListColumns(ProjectContent),
		scan:    scanProjectSummary,
	}
	projectDetailRead = contentRead[ProjectDetail]{
		kind:    ProjectContent,
		columns: publicDetailColumns(ProjectContent),
		scan:    scanProjectDetail,
	}
)

// GetPublishedProject returns one published project with its creator/updater
// refs and download rows, or the masked ErrNotFound (the blog detail
// contract).
func GetPublishedProject(ctx context.Context, db *sql.DB, id string) (*ProjectDetail, []Download, error) {
	return getPublishedContent(ctx, db, projectDetailRead, id)
}

// ListPublishedProjects returns one keyset page of published projects, newest
// first, plus whether another page exists. limit must already satisfy the
// wire contract (1..100; the handler enforces it).
func ListPublishedProjects(ctx context.Context, db *sql.DB, limit int, after *PageKey) ([]ProjectSummary, bool, error) {
	return listPublishedContent(ctx, db, projectSummaryRead, limit, after)
}
