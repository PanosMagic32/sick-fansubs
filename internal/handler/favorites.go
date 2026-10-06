package handler

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/store"
)

// Favorites handlers: the current user's favorite
// blog posts and projects under /api/v1/users/me/favorites.
//
// The route chain is RequestID → TrustedOrigin → Session → CSRF (the CSRF
// middleware only validates unsafe methods, so the GETs pass through it).
// No rate limit — authenticated per-user operations protected by origin +
// CSRF, like the other account operations.
//
// Favoriting is constrained to PUBLISHED content by the store's
// INSERT…SELECT guard; the handlers map every rejection to the same masked
// 404 the content-detail endpoints use.

// favoriteKind bundles the per-content-type differences (content descriptor,
// cursor namespace, log label) so one handler serves both types without a
// polymorphic store interface.
type favoriteKind struct {
	label  string // log-only discriminator: "blog" | "project"
	prefix string // cursor namespace prefix

	content store.ContentKind
}

var (
	// BlogFavoritesKind / ProjectFavoritesKind select the content kind,
	// cursor namespace, and log label for each favoritable content type.
	BlogFavoritesKind = favoriteKind{
		label:   "blog",
		prefix:  blogFavCursorPrefix,
		content: store.BlogContent,
	}
	ProjectFavoritesKind = favoriteKind{
		label:   "project",
		prefix:  projectFavCursorPrefix,
		content: store.ProjectContent,
	}
)

// favoriteItemDTO is the wire card for one favorite list item — the
// search-list look without the type badge (the tabs already
// separate the types). subtitle is the blog post's; projects have none, so
// theirs is empty.
type favoriteItemDTO struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Subtitle      string `json:"subtitle"`
	Description   string `json:"description"`
	ThumbnailURL  string `json:"thumbnailUrl"`
	PublishedAt   string `json:"publishedAt"`
	FavoritedAt   string `json:"favoritedAt"`
	CommentCount  int64  `json:"commentCount"`
	FavoriteCount int64  `json:"favoriteCount"`
}

// favoriteStatusDTO is the wire envelope for the status read. A named
// type, not an ad-hoc map — the OpenAPI FavoriteStatus
// schema and the TS FavoriteStatusResponse mirror it.
type favoriteStatusDTO struct {
	Favorited bool `json:"favorited"`
}

// FavoritesList returns the handler for GET /api/v1/users/me/favorites/{kind}.
//
// Outcomes: 200 keyset list (only published favorites — the join filters);
// 400 malformed/unknown/duplicate query or invalid cursor; 401 unauthenticated;
// 422 limit out of range.
func FavoritesList(kind favoriteKind, db *sql.DB, publicBaseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		su, ok := requireSession(w, r, kind.label+" favorites list requires authentication")
		if !ok {
			return
		}

		_, limit, after, ok := parseKeysetParams(w, r, 20, 100, kind.prefix)
		if !ok {
			return
		}

		writeKeysetPage(w, r, kind.label+" favorites list", keysetPage[store.FavoriteItem, favoriteItemDTO]{
			count: func(ctx context.Context) (int, error) {
				return store.CountFavorites(ctx, db, kind.content, su.UserID)
			},
			read: func(ctx context.Context) ([]store.FavoriteItem, bool, error) {
				var key *store.FavoritePageKey
				if after != nil {
					// The shared codec's timestamp slot carries the favorite's
					// created_at_ms in this namespace.
					key = &store.FavoritePageKey{CreatedAtMS: after.PublishedAtMS, ContentID: after.ID}
				}
				return store.ListFavorites(ctx, db, kind.content, su.UserID, limit, key)
			},
			item: func(it store.FavoriteItem) favoriteItemDTO {
				return favoriteItemDTO{
					ID:            it.ID,
					Title:         it.Title,
					Subtitle:      it.Subtitle,
					Description:   it.Description,
					ThumbnailURL:  mediaURL(publicBaseURL, it.ThumbnailURL),
					PublishedAt:   formatAPITime(it.PublishedAtMS),
					FavoritedAt:   formatAPITime(it.FavoritedAtMS),
					CommentCount:  it.CommentCount,
					FavoriteCount: it.FavoriteCount,
				}
			},
			next: func(last store.FavoriteItem) string {
				return encodeCursor(kind.prefix, last.FavoritedAtMS, last.ID)
			},
		})
	}
}

// FavoritesStatus returns the handler for
// GET /api/v1/users/me/favorites/{kind}/{id}.
//
// Outcomes: 200 {"favorited": bool} for published content; 400 invalid id
// shape; 404 masked for unknown/draft/archived/unstamped content; 401
// unauthenticated.
func FavoritesStatus(kind favoriteKind, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su, ok := requireSession(w, r, kind.label+" favorites status requires authentication")
		if !ok {
			return
		}
		if !allowNoQueryParams(w, r) {
			return
		}
		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid "+kind.label+" id")
			return
		}

		published, favorited, err := store.FavoriteStatus(r.Context(), db, kind.content, su.UserID, id)
		if err != nil {
			writeInternalError(w, r, logger, kind.label+" favorite status failed", "error", err)
			return
		}
		if !published {
			// Masked absence — indistinguishable from the content detail.
			NotFound(w, r)
			return
		}

		writeJSON(w, r, http.StatusOK, favoriteStatusDTO{Favorited: favorited})
	}
}

// FavoritesAdd returns the handler for
// PUT /api/v1/users/me/favorites/{kind}/{id}.
//
// Outcomes: 204 idempotent success (repeat favorites succeed); 400 invalid
// id shape; 404 masked for unknown/non-published content; 401
// unauthenticated; 403 origin/CSRF failure (middleware).
func FavoritesAdd(kind favoriteKind, db *sql.DB, now func() time.Time) http.HandlerFunc {
	if now == nil {
		now = time.Now
	}
	return func(w http.ResponseWriter, r *http.Request) {
		su, ok := requireSession(w, r, kind.label+" favorite add requires authentication")
		if !ok {
			return
		}
		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid "+kind.label+" id")
			return
		}

		err := store.AddFavorite(r.Context(), db, kind.content, su.UserID, id, now().UnixMilli())
		if err != nil {
			writeStoreError(w, r, err, kind.label+" favorite add", "error", err)
			return
		}

		writeNoContent(w, r)
	}
}

// FavoritesRemove returns the handler for
// DELETE /api/v1/users/me/favorites/{kind}/{id}.
//
// Outcomes: 204 idempotent success (removing a non-favorite, or a favorite
// of now-deleted content, also succeeds); 400 invalid id shape; 401
// unauthenticated; 403 origin/CSRF failure (middleware).
func FavoritesRemove(kind favoriteKind, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su, ok := requireSession(w, r, kind.label+" favorite remove requires authentication")
		if !ok {
			return
		}
		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid "+kind.label+" id")
			return
		}

		if err := store.RemoveFavorite(r.Context(), db, kind.content, su.UserID, id); err != nil {
			writeInternalError(w, r, logger, kind.label+" favorite remove failed", "error", err)
			return
		}

		writeNoContent(w, r)
	}
}

// writeNoContent writes the baseline headers for a bodyless 204
// (request ID + no-store — no JSON body).
func writeNoContent(w http.ResponseWriter, r *http.Request) {
	setAPIHeaders(w, r)
	w.WriteHeader(http.StatusNoContent)
}
