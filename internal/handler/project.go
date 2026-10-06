package handler

import (
	"context"
	"database/sql"
	"net/http"

	"sick-fansubs/internal/store"
)

// Project-list wire contract: GET /api/v1/projects.
//
// Public collection — no authentication, no rate limit (public reads are
// deliberately unthrottled — internal/handler/AGENTS.md), and no
// CSRF/origin checks, which protect unsafe methods. The route chain is
// RequestID only, matching the blog list.
//
// The precise contract lives in docs/api/openapi.yaml (operationId
// listProjects); the pagination envelope and bounds follow the shared keyset
// contract in docs/patterns/go/collections.md, and the store owns the
// ordering and public filter.

// ProjectItem is the public card projection of one project in the list
// response.
//
// The projects table has no subtitle column, so the card
// carries slug and description instead. ThumbnailURL is a pointer so a
// stored value that fails the mediaURL scheme guard emits JSON null —
// thumbnail_url is NOT NULL in storage, so null means "masked malformed
// value", never "absent" (the scheme-guard clause).
type ProjectItem struct {
	ID            string      `json:"id"`
	Title         string      `json:"title"`
	Description   string      `json:"description"`
	Slug          string      `json:"slug"`
	ThumbnailURL  *string     `json:"thumbnailUrl"`
	PublishedAt   string      `json:"publishedAt"`
	UpdatedAt     string      `json:"updatedAt"`
	CommentCount  int64       `json:"commentCount"`
	FavoriteCount int64       `json:"favoriteCount"`
	Creator       *userRefDTO `json:"creator"`
}

// Project-list pagination bounds: default 20, maximum 100.
const (
	projectListDefaultLimit = 20
	projectListMaxLimit     = 100
)

// ProjectList returns the public project-list handler. publicBaseURL is the
// configured canonical public origin used to emit absolute thumbnail URLs
// (see mediaURL for the interim legacy pass-through rule).
func ProjectList(db *sql.DB, publicBaseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, limit, after, ok := parseKeysetParams(w, r, projectListDefaultLimit, projectListMaxLimit, projectCursorPrefix)
		if !ok {
			return
		}

		fallback, ok := requireUploaderFallback(w, r, db)
		if !ok {
			return
		}

		writeKeysetPage(w, r, "project list query", keysetPage[store.ProjectSummary, ProjectItem]{
			count: func(ctx context.Context) (int, error) {
				return store.CountPublishedContent(ctx, db, store.ProjectContent)
			},
			read: func(ctx context.Context) ([]store.ProjectSummary, bool, error) {
				var key *store.PageKey
				if after != nil {
					key = &store.PageKey{PublishedAtMS: after.PublishedAtMS, ID: after.ID}
				}
				return store.ListPublishedProjects(ctx, db, limit, key)
			},
			item: func(p store.ProjectSummary) ProjectItem {
				return ProjectItem{
					ID:            p.ID,
					Title:         p.Title,
					Description:   p.Description,
					Slug:          p.Slug,
					ThumbnailURL:  thumbnailURLOrNull(publicBaseURL, p.ThumbnailURL),
					PublishedAt:   formatAPITime(p.PublishedAtMS),
					UpdatedAt:     formatAPITime(p.UpdatedAtMS),
					CommentCount:  p.CommentCount,
					FavoriteCount: p.FavoriteCount,
					Creator:       contentRefFrom(publicBaseURL, p.Creator, fallback),
				}
			},
			next: func(last store.ProjectSummary) string {
				return encodeCursor(projectCursorPrefix, last.PublishedAtMS, last.ID)
			},
		})
	}
}
