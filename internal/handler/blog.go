package handler

import (
	"context"
	"database/sql"
	"net/http"

	"sick-fansubs/internal/store"
)

// Blog-list wire contract: GET /api/v1/blog-posts.
//
// Public collection — no authentication, no rate limit (public reads are
// deliberately unthrottled — internal/handler/AGENTS.md), and no
// CSRF/origin checks, which protect unsafe methods. The route chain is
// RequestID only.
//
// The precise contract lives in docs/api/openapi.yaml (operationId
// listBlogPosts); the pagination envelope and bounds follow the shared keyset
// contract in docs/patterns/go/collections.md, and the store owns the
// ordering and public filter.

// BlogPostItem is the public card projection of one blog post in the list
// response.
//
// The cards need description (clamped by the UI) and the creator ref
// (avatar/initial chip); updatedAt is still emitted for future card use —
// the current UI does not render it. Downloads and
// the updater stay detail fields — the list accepts what the cards
// need and nothing more.
type BlogPostItem struct {
	ID            string      `json:"id"`
	Title         string      `json:"title"`
	Subtitle      string      `json:"subtitle"`
	Description   string      `json:"description"`
	ThumbnailURL  string      `json:"thumbnailUrl"`
	PublishedAt   string      `json:"publishedAt"`
	UpdatedAt     string      `json:"updatedAt"`
	CommentCount  int64       `json:"commentCount"`
	FavoriteCount int64       `json:"favoriteCount"`
	Creator       *userRefDTO `json:"creator"`
}

// Blog-list pagination bounds: default 20, maximum 100.
const (
	blogListDefaultLimit = 20
	blogListMaxLimit     = 100
)

// BlogList returns the public blog-post list handler. publicBaseURL is the
// configured canonical public origin used to emit absolute thumbnail URLs
// (see mediaURL for the interim legacy pass-through rule).
func BlogList(db *sql.DB, publicBaseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, limit, after, ok := parseKeysetParams(w, r, blogListDefaultLimit, blogListMaxLimit, blogCursorPrefix)
		if !ok {
			return
		}

		fallback, ok := requireUploaderFallback(w, r, db)
		if !ok {
			return
		}

		writeKeysetPage(w, r, "blog list query", keysetPage[store.BlogPostSummary, BlogPostItem]{
			count: func(ctx context.Context) (int, error) {
				return store.CountPublishedContent(ctx, db, store.BlogContent)
			},
			read: func(ctx context.Context) ([]store.BlogPostSummary, bool, error) {
				var key *store.PageKey
				if after != nil {
					key = &store.PageKey{PublishedAtMS: after.PublishedAtMS, ID: after.ID}
				}
				return store.ListPublishedBlogPosts(ctx, db, limit, key)
			},
			item: func(p store.BlogPostSummary) BlogPostItem {
				return BlogPostItem{
					ID:            p.ID,
					Title:         p.Title,
					Subtitle:      p.Subtitle,
					Description:   p.Description,
					ThumbnailURL:  mediaURL(publicBaseURL, p.ThumbnailURL),
					PublishedAt:   formatAPITime(p.PublishedAtMS),
					UpdatedAt:     formatAPITime(p.UpdatedAtMS),
					CommentCount:  p.CommentCount,
					FavoriteCount: p.FavoriteCount,
					Creator:       contentRefFrom(publicBaseURL, p.Creator, fallback),
				}
			},
			next: func(last store.BlogPostSummary) string {
				return encodeCursor(blogCursorPrefix, last.PublishedAtMS, last.ID)
			},
		})
	}
}
