package handler

import (
	"context"
	"database/sql"
	"net/http"

	"sick-fansubs/internal/store"
)

// Likers handlers: the auth-only who-liked lists behind the likers modal —
// a blog post's or project's favoriters and a comment's hearters.
//
// The route chains are authenticatedReadsChain: every reader requires a
// session, so a like list is never a public surface. Unknown, unpublished,
// or unstamped content — and, for hearts, a comment that does not belong to
// the addressed content — answer the same masked 404 the content reads use.
// No rate limit — authenticated per-user reads (the favorites precedent).

// likerKind bundles the per-content-type differences (content descriptor,
// cursor namespace, log label) so the liker handlers share one shape without
// a polymorphic store interface.
type likerKind struct {
	label  string // log-only discriminator: "blog" | "project"
	prefix string // cursor namespace prefix

	content store.ContentKind
}

var (
	// BlogFavoritersKind / ProjectFavoritersKind select the content kind,
	// cursor namespace, and log label for a content row's favoriters list.
	BlogFavoritersKind = likerKind{
		label:   "blog",
		prefix:  blogFavoritersCursorPrefix,
		content: store.BlogContent,
	}
	ProjectFavoritersKind = likerKind{
		label:   "project",
		prefix:  projectFavoritersCursorPrefix,
		content: store.ProjectContent,
	}
	// BlogHeartersKind / ProjectHeartersKind select the same for a comment's
	// hearters list.
	BlogHeartersKind = likerKind{
		label:   "blog",
		prefix:  blogHeartersCursorPrefix,
		content: store.BlogContent,
	}
	ProjectHeartersKind = likerKind{
		label:   "project",
		prefix:  projectHeartersCursorPrefix,
		content: store.ProjectContent,
	}
)

// FavoriteLikers returns the handler for
// GET /api/v1/{segment}/{id}/favorites.
//
// Outcomes: 200 keyset list of active favoriters, newest first; 400 invalid
// id shape, malformed/duplicate query, or invalid cursor; 401
// unauthenticated; 404 masked for unknown/non-published content; 422 limit
// out of range.
func FavoriteLikers(kind likerKind, db *sql.DB, publicBaseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireSession(w, r, kind.label+" favoriters list requires authentication"); !ok {
			return
		}
		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid "+kind.label+" id")
			return
		}
		_, limit, after, ok := parseKeysetParams(w, r, 20, 100, kind.prefix)
		if !ok {
			return
		}

		writeKeysetPage(w, r, kind.label+" favoriters list", keysetPage[store.LikerItem, userRefDTO]{
			count: func(ctx context.Context) (int, error) {
				return store.CountFavoriters(ctx, db, kind.content, id)
			},
			read: func(ctx context.Context) ([]store.LikerItem, bool, error) {
				var key *store.LikerPageKey
				if after != nil {
					// The shared codec's timestamp slot carries the like
					// row's created_at_ms and the id slot the liker's user id.
					key = &store.LikerPageKey{LikedAtMS: after.PublishedAtMS, UserID: after.ID}
				}
				return store.ListFavoriters(ctx, db, kind.content, id, limit, key)
			},
			item: likerDTO(publicBaseURL),
			next: func(last store.LikerItem) string {
				return encodeCursor(kind.prefix, last.LikedAtMS, last.UserID)
			},
		})
	}
}

// CommentHearters returns the handler for
// GET /api/v1/{segment}/{id}/comments/{commentId}/hearts.
//
// Outcomes: 200 keyset list of active hearters, newest first; 400 invalid
// content or comment id shape, malformed/duplicate query, or invalid cursor;
// 401 unauthenticated; 404 masked for unknown/non-published content or a
// comment that is unknown or foreign; 422 limit out of range.
func CommentHearters(kind likerKind, db *sql.DB, publicBaseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireSession(w, r, kind.label+" comment hearters list requires authentication"); !ok {
			return
		}
		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid "+kind.label+" id")
			return
		}
		commentID := r.PathValue("commentId")
		if !publicIDPattern.MatchString(commentID) {
			writeBadRequest(w, r, "invalid comment id")
			return
		}
		_, limit, after, ok := parseKeysetParams(w, r, 20, 100, kind.prefix)
		if !ok {
			return
		}

		writeKeysetPage(w, r, kind.label+" comment hearters list", keysetPage[store.LikerItem, userRefDTO]{
			count: func(ctx context.Context) (int, error) {
				return store.CountCommentHearters(ctx, db, kind.content, commentID)
			},
			read: func(ctx context.Context) ([]store.LikerItem, bool, error) {
				var key *store.LikerPageKey
				if after != nil {
					key = &store.LikerPageKey{LikedAtMS: after.PublishedAtMS, UserID: after.ID}
				}
				return store.ListCommentHearters(ctx, db, kind.content, id, commentID, limit, key)
			},
			item: likerDTO(publicBaseURL),
			next: func(last store.LikerItem) string {
				return encodeCursor(kind.prefix, last.LikedAtMS, last.UserID)
			},
		})
	}
}

// likerDTO projects one liker row: the content-card user ref shape (id,
// username, nullable absolute avatar), with no role — the same public
// projection the content bylines use.
func likerDTO(publicBaseURL string) func(store.LikerItem) userRefDTO {
	return func(it store.LikerItem) userRefDTO {
		return userRefDTO{
			ID:        it.UserID,
			Username:  it.Username,
			AvatarURL: avatarURLOrNull(publicBaseURL, it.AvatarURL),
		}
	}
}
