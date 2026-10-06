package handler

import (
	"database/sql"
	"net/http"

	"sick-fansubs/internal/store"
)

// Project-detail wire contract: GET /api/v1/projects/{id}.
//
// Public individual resource — no authentication, no rate limit (public
// reads are deliberately unthrottled — internal/handler/AGENTS.md), and no
// CSRF/origin checks, which protect unsafe methods.
// The route chain is RequestID only, matching the list.
//
// The precise contract lives in docs/api/openapi.yaml (operationId
// getProject); the masking rule: drafts, archived projects, projects without
// a publish time, and unknown ids all answer 404; times and ids use the
// canonical formats. ID syntax is validated FIRST against publicIDPattern — the
// blog-detail precedent: invalid shape → 400, valid shape that resolves to
// nothing → 404. The two outcomes stay distinct.

// projectDetailResponse is the direct resource representation. Creator and
// Updater are pointers without omitempty: the public projection resolves a
// missing ref through the uploader fallback, so null remains only when no
// fallback account resolves — an explicit absence, not a missing field.
type projectDetailResponse struct {
	ID            string               `json:"id"`
	Title         string               `json:"title"`
	Description   string               `json:"description"`
	Slug          string               `json:"slug"`
	ThumbnailURL  *string              `json:"thumbnailUrl"`
	PublishedAt   string               `json:"publishedAt"`
	UpdatedAt     string               `json:"updatedAt"`
	CommentCount  int64                `json:"commentCount"`
	FavoriteCount int64                `json:"favoriteCount"`
	Creator       *userRefDTO          `json:"creator"`
	Updater       *userRefDTO          `json:"updater"`
	Downloads     []projectDownloadDTO `json:"downloads"`
}

// projectDownloadDTO is one batch-download entry.
// Magnet/torrent values go through safeDownloadLink: a stored value that
// fails the link guard emits null — the stored row stays untouched.
type projectDownloadDTO struct {
	Name       string  `json:"name"`
	MagnetURL  *string `json:"magnetUrl"`
	TorrentURL *string `json:"torrentUrl"`
}

// ProjectDetail returns the public project-detail handler.
//
// publicBaseURL is the configured canonical public origin for the
// thumbnail URL (see mediaURL).
func ProjectDetail(db *sql.DB, publicBaseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !allowNoQueryParams(w, r) {
			return
		}

		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			// Structurally invalid id: 400 before any lookup.
			writeBadRequest(w, r, "invalid project id")
			return
		}

		project, downloads, err := store.GetPublishedProject(r.Context(), db, id)
		if err != nil {
			writeStoreError(w, r, err, "project detail query", "error", err)
			return
		}

		fallback, ok := requireUploaderFallback(w, r, db)
		if !ok {
			return
		}

		creator := contentRefFrom(publicBaseURL, project.Creator, fallback)
		updater := contentRefFrom(publicBaseURL, project.Updater, fallback)

		downloadsDTO := make([]projectDownloadDTO, 0, len(downloads))
		for _, d := range downloads {
			downloadsDTO = append(downloadsDTO, projectDownloadDTO{
				Name:       d.Label,
				MagnetURL:  safeDownloadLink(d.MagnetLink),
				TorrentURL: safeDownloadLink(d.TorrentLink),
			})
		}

		writeJSON(w, r, http.StatusOK, projectDetailResponse{
			ID:            project.ID,
			Title:         project.Title,
			Description:   project.Description,
			Slug:          project.Slug,
			ThumbnailURL:  thumbnailURLOrNull(publicBaseURL, project.ThumbnailURL),
			PublishedAt:   formatAPITime(project.PublishedAtMS),
			UpdatedAt:     formatAPITime(project.UpdatedAtMS),
			CommentCount:  project.CommentCount,
			FavoriteCount: project.FavoriteCount,
			Creator:       creator,
			Updater:       updater,
			Downloads:     downloadsDTO,
		})
	}
}

// thumbnailURLOrNull projects a stored thumbnail reference: values that
// fail the mediaURL scheme guard emit JSON null — a stored value can never
// smuggle another scheme into the JSON (the scheme-guard clause;
// same rationale as mediaURL/safeDownloadLink). thumbnail_url is NOT NULL
// in storage, so the null branch is the masked-malformed-value outcome
// only, never plain absence.
func thumbnailURLOrNull(publicBaseURL, stored string) *string {
	u := mediaURL(publicBaseURL, stored)
	if u == "" {
		return nil
	}
	return &u
}
