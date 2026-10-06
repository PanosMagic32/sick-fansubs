package handler

import (
	"database/sql"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"sick-fansubs/internal/store"
)

// publicIDPattern pins the public identifier charset: case-sensitive opaque
// URL-safe strings of 1–64 characters — the RFC 3986 unreserved set. The path
// value is checked BEFORE the store lookup: a syntactically invalid id is
// structurally invalid input (400), while a well-formed id that resolves
// to nothing — or to unpublished content — is masked as 404. The two
// outcomes stay distinct: invalid input is not "not found".
var publicIDPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{1,64}$`)

// Blog-detail wire contract: GET /api/v1/blog-posts/{id}.
//
// Public individual resource — no authentication, no rate limit (public
// reads are deliberately unthrottled — internal/handler/AGENTS.md), and no
// CSRF/origin checks, which protect unsafe methods.
// The route chain is RequestID only, matching the list.
//
// The precise contract lives in docs/api/openapi.yaml (operationId
// getBlogPost). Drafts, archived posts, posts without a publish time, and
// unknown ids all answer 404; times and ids use the canonical formats.

// blogPostDetailResponse is the direct resource representation. Creator and
// Updater are pointers without omitempty: the public projection resolves a
// missing ref through the uploader fallback, so null remains only when no
// fallback account resolves — an explicit absence, not a missing field.
type blogPostDetailResponse struct {
	ID            string                `json:"id"`
	Title         string                `json:"title"`
	Subtitle      string                `json:"subtitle"`
	Description   string                `json:"description"`
	ThumbnailURL  string                `json:"thumbnailUrl"`
	PublishedAt   string                `json:"publishedAt"`
	UpdatedAt     string                `json:"updatedAt"`
	CommentCount  int64                 `json:"commentCount"`
	FavoriteCount int64                 `json:"favoriteCount"`
	Creator       *userRefDTO           `json:"creator"`
	Updater       *userRefDTO           `json:"updater"`
	Downloads     []blogPostDownloadDTO `json:"downloads"`
}

// blogPostDownloadDTO is one download choice.
type blogPostDownloadDTO struct {
	Resolution string  `json:"resolution"`
	MagnetURL  *string `json:"magnetUrl"`
	TorrentURL *string `json:"torrentUrl"`
}

// BlogDetail returns the public blog-post detail handler.
//
// publicBaseURL is the configured canonical public origin for the
// thumbnail URL (see mediaURL).
func BlogDetail(db *sql.DB, publicBaseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !allowNoQueryParams(w, r) {
			return
		}

		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			// Structurally invalid id: 400 before any lookup.
			writeBadRequest(w, r, "invalid blog post id")
			return
		}

		post, downloads, err := store.GetPublishedBlogPost(r.Context(), db, id)
		if err != nil {
			writeStoreError(w, r, err, "blog detail query", "error", err)
			return
		}

		fallback, ok := requireUploaderFallback(w, r, db)
		if !ok {
			return
		}

		creator := contentRefFrom(publicBaseURL, post.Creator, fallback)
		updater := contentRefFrom(publicBaseURL, post.Updater, fallback)

		downloadsDTO := make([]blogPostDownloadDTO, 0, len(downloads))
		for _, d := range downloads {
			downloadsDTO = append(downloadsDTO, blogPostDownloadDTO{
				Resolution: d.Label,
				MagnetURL:  safeDownloadLink(d.MagnetLink),
				TorrentURL: safeDownloadLink(d.TorrentLink),
			})
		}

		writeJSON(w, r, http.StatusOK, blogPostDetailResponse{
			ID:            post.ID,
			Title:         post.Title,
			Subtitle:      post.Subtitle,
			Description:   post.Description,
			ThumbnailURL:  mediaURL(publicBaseURL, post.ThumbnailURL),
			PublishedAt:   formatAPITime(post.PublishedAtMS),
			UpdatedAt:     formatAPITime(post.UpdatedAtMS),
			CommentCount:  post.CommentCount,
			FavoriteCount: post.FavoriteCount,
			Creator:       creator,
			Updater:       updater,
			Downloads:     downloadsDTO,
		})
	}
}

// allowNoQueryParams enforces the strict detail-boundary contract: the
// detail GET accepts no query parameters. Unknown names, duplicates, and a
// malformed raw query string all answer 400 — the same strict parsing the
// list applies (url.ParseQuery instead of r.URL.Query(), which would
// silently discard bad pairs).
func allowNoQueryParams(w http.ResponseWriter, r *http.Request) bool {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeBadRequest(w, r, "malformed query string")
		return false
	}
	for name, values := range query {
		if len(values) != 1 {
			writeBadRequest(w, r, "duplicate query parameter: "+name)
		} else {
			writeBadRequest(w, r, "unknown query parameter: "+name)
		}
		return false
	}
	return true
}

// safeDownloadLink is the read-boundary link guard.
//
// The database stores download values as opaque text: migration preserved
// malformed legacy values as-is with a warning rather
// than destroying data. The public read still re-validates each value and
// emits null for anything that is not a plausible link — an http(s) URL
// with a host, or a magnet URI (scheme magnet followed by a query, i.e.
// `magnet:?xt=…`). A malformed stored value can never smuggle another
// scheme (javascript:, data:) into the JSON. The stored row stays
// untouched; masking happens only at the wire boundary, exactly like
// mediaURL guards thumbnails.
func safeDownloadLink(v *string) *string {
	if v == nil || *v == "" {
		return nil
	}
	u, err := url.Parse(*v)
	if err != nil {
		return nil
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" {
			return nil
		}
		return v
	case "magnet":
		// Go's url.Parse splits "magnet:?xt=…" into an empty Opaque plus a
		// RawQuery (the '?' is a query delimiter, not opaque content). A magnet
		// URI carries its parameters as that query, so exactly
		// Opaque == "" && RawQuery != "" is a plausible magnet link;
		// "magnet:" and "magnet:garbage" are not.
		if u.Opaque == "" && u.RawQuery != "" {
			return v
		}
		return nil
	default:
		return nil
	}
}
