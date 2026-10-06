package handler

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/media"
	"sick-fansubs/internal/store"
)

// Blog content-administration handlers:
// create, staff list/detail, revision-backed update, hard-delete.
//
// Authorization (server-enforced, explicit per handler):
//   - create → CanCreateContent (admin+);
//   - staff reads, update → CanModerateContent (moderator+);
//   - delete → CanDeleteContent (admin+);
//   - non-published rows additionally require the draft-visibility gate
//     (creator or strictly-higher role) — enforced by the store's staff
//     reads; the write handlers pre-read through the SAME gated read, so a
//     moderator cannot touch an admin's draft (masked 404, like the public
//     reads).
//
// Concurrency: staff detail emits a revision-backed
// strong ETag ("<revision>"); update and delete require If-Match — 428
// when missing, 400 when malformed, 412 when stale (store.ErrConflict).
//
// Audit: content_created / content_updated / content_deleted via
// audit.Event — a success row only after the commit, a failure row on the
// 500 arm; validation rejections emit nothing.

// Field bounds for the write DTO (bounded typed DTOs; the numbers are
// mirrored in OpenAPI).
const (
	maxBlogTitleRunes       = 200
	maxBlogSubtitleRunes    = 500
	maxBlogDescriptionRunes = 20_000
)

// blogPostWriteRequest is the create/update body (strict: unknown fields
// rejected by readJSON). Pointer fields distinguish absent from empty;
// absent falls back to the defaults below. Downloads is the full-
// replacement download-row set; DownloadsParsed holds the validated rows
// the parse function produces and is not a wire field (json:"-"), so no
// body key can reach it.
type blogPostWriteRequest struct {
	Title           string              `json:"title"`
	Subtitle        *string             `json:"subtitle"`
	Description     *string             `json:"description"`
	ThumbnailPath   string              `json:"thumbnailPath"`
	Status          *string             `json:"status"`
	Downloads       []blogDownloadWrite `json:"downloads"`
	DownloadsParsed []store.Download    `json:"-"`
}

// blogDownloadWrite is one row of the write DTO's downloads array: the
// resolution label plus the magnet/torrent link
// pair. The shared validator (downloads.go) applies the bounds and the
// link grammar; the violation paths carry this field's name.
type blogDownloadWrite struct {
	Resolution string  `json:"resolution"`
	MagnetURL  *string `json:"magnetUrl"`
	TorrentURL *string `json:"torrentUrl"`
}

// staffBlogPostDTO is the staff-read wire shape: the public
// detail fields plus status, revision, and createdAt — the admin form
// needs all of them. publishedAt is nullable (a draft has no stamp).
// Downloads are the STORED rows verbatim (no link guard — the form must
// show what is stored so a bad value can be fixed).
type staffBlogPostDTO struct {
	ID            string                `json:"id"`
	Title         string                `json:"title"`
	Subtitle      string                `json:"subtitle"`
	Description   string                `json:"description"`
	ThumbnailPath string                `json:"thumbnailPath"`
	ThumbnailURL  string                `json:"thumbnailUrl"`
	Status        string                `json:"status"`
	PublishedAt   *string               `json:"publishedAt"`
	CreatedAt     string                `json:"createdAt"`
	UpdatedAt     string                `json:"updatedAt"`
	Revision      int64                 `json:"revision"`
	Creator       *userRefDTO           `json:"creator"`
	Updater       *userRefDTO           `json:"updater"`
	Downloads     []blogPostDownloadDTO `json:"downloads"`
}

// staffBlogPostSummaryDTO is the staff-list card: the summary plus status
// and revision (the dashboard's badges + edit link).
type staffBlogPostSummaryDTO struct {
	ID           string      `json:"id"`
	Title        string      `json:"title"`
	Subtitle     string      `json:"subtitle"`
	Description  string      `json:"description"`
	ThumbnailURL string      `json:"thumbnailUrl"`
	Status       string      `json:"status"`
	PublishedAt  *string     `json:"publishedAt"`
	UpdatedAt    string      `json:"updatedAt"`
	Revision     int64       `json:"revision"`
	Creator      *userRefDTO `json:"creator"`
}

// parseBlogPostWrite validates one write body against the write
// contract. allowedStatuses is the create form's draft|published or the
// update form's draft|published|archived (archived is an edit-time state).
func parseBlogPostWrite(w http.ResponseWriter, r *http.Request, allowedStatuses ...string) (blogPostWriteRequest, bool) {
	var req blogPostWriteRequest
	if err := readJSONLimit(w, r, &req, maxContentWriteBodySize); err != nil {
		writeJSONBodyError(w, r, err)
		return req, false
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeValidationErrors(w, r, []Violation{{Field: "title", Code: "required"}})
		return req, false
	}
	if utf8.RuneCountInString(title) > maxBlogTitleRunes {
		writeValidationErrors(w, r, []Violation{{Field: "title", Code: "maxLength"}})
		return req, false
	}
	req.Title = title

	subtitle := ""
	if req.Subtitle != nil {
		subtitle = strings.TrimSpace(*req.Subtitle)
		if utf8.RuneCountInString(subtitle) > maxBlogSubtitleRunes {
			writeValidationErrors(w, r, []Violation{{Field: "subtitle", Code: "maxLength"}})
			return req, false
		}
	}
	req.Subtitle = &subtitle

	description := ""
	if req.Description != nil {
		description = strings.TrimSpace(*req.Description)
		if utf8.RuneCountInString(description) > maxBlogDescriptionRunes {
			writeValidationErrors(w, r, []Violation{{Field: "description", Code: "maxLength"}})
			return req, false
		}
	}
	req.Description = &description

	if strings.TrimSpace(req.ThumbnailPath) == "" {
		writeValidationErrors(w, r, []Violation{{Field: "thumbnailPath", Code: "required"}})
		return req, false
	}

	status := "published" // the form defaults to published
	if req.Status != nil {
		status = *req.Status
	}
	if !slices.Contains(allowedStatuses, status) {
		writeValidationErrors(w, r, []Violation{{Field: "status", Code: "invalidValue"}})
		return req, false
	}
	req.Status = &status

	// Downloads: required, bounded, each row
	// needs a resolution and at least one link; every link must pass the
	// READ boundary's grammar so staff can never store a value the public
	// detail would mask. The shared validator (downloads.go) writes the 422.
	rows := make([]downloadRow, 0, len(req.Downloads))
	for _, d := range req.Downloads {
		rows = append(rows, downloadRow{Label: d.Resolution, MagnetURL: d.MagnetURL, TorrentURL: d.TorrentURL})
	}
	validated, ok := validateDownloadRows(w, r, "resolution", rows)
	if !ok {
		return req, false
	}
	req.DownloadsParsed = validated

	return req, true
}

// validThumbnailReference reports whether ref is an accepted
// storage-relative media reference AND the processed file exists. The
// grammar lives in media.ParseStorageReference (the media package owns
// the layout); the path is rebuilt from the validated components only.
func validThumbnailReference(dataDir, ref string) bool {
	id, ext, ok := media.ParseStorageReference(ref)
	if !ok {
		return false
	}
	if _, err := os.Stat(media.ImagePath(dataDir, id, ext)); err != nil {
		return false
	}
	return true
}

// etagForRevision formats the revision-backed strong ETag (the
// string representation of the revision integer).
func etagForRevision(revision int64) string {
	return strconv.Quote(strconv.FormatInt(revision, 10))
}

// parseIfMatch reads the required If-Match precondition: exactly one
// header carrying exactly `"<int>"`. Missing → 428; malformed/ambiguous →
// 400. Staleness surfaces later as store.ErrConflict → 412.
func parseIfMatch(w http.ResponseWriter, r *http.Request) (int64, bool) {
	vals := r.Header.Values("If-Match")
	if len(vals) == 0 {
		writeProblem(w, r, http.StatusPreconditionRequired,
			"/problems/precondition-required", "Precondition required")
		return 0, false
	}
	if len(vals) > 1 {
		writeBadRequest(w, r, "ambiguous If-Match header")
		return 0, false
	}
	raw := strings.TrimSpace(vals[0])
	if len(raw) < 3 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		writeBadRequest(w, r, "malformed If-Match header")
		return 0, false
	}
	digits := raw[1 : len(raw)-1]
	revision, err := strconv.ParseInt(digits, 10, 64)
	// The emitted ETag is canonical (`FormatInt`), so a non-canonical spelling
	// ("+1", "007") is malformed rather than an aliased revision.
	if err != nil || revision < 1 || digits != strconv.FormatInt(revision, 10) {
		writeBadRequest(w, r, "malformed If-Match header")
		return 0, false
	}
	return revision, true
}

// writePreconditionFailed writes the 412 stale-revision problem.
func writePreconditionFailed(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, r, http.StatusPreconditionFailed,
		"/problems/precondition-failed", "Precondition failed")
}

// staffDTO builds the staff wire shape for one store row.
func staffDTO(publicBaseURL string, p *store.StaffBlogPost) staffBlogPostDTO {
	dto := staffBlogPostDTO{
		ID:            p.ID,
		Title:         p.Title,
		Subtitle:      p.Subtitle,
		Description:   p.Description,
		ThumbnailPath: p.ThumbnailURL,
		ThumbnailURL:  mediaURL(publicBaseURL, p.ThumbnailURL),
		Status:        p.Status,
		CreatedAt:     formatAPITime(p.CreatedAtMS),
		UpdatedAt:     formatAPITime(p.UpdatedAtMS),
		Revision:      p.Revision,
	}
	if p.PublishedAtMS != nil {
		s := formatAPITime(*p.PublishedAtMS)
		dto.PublishedAt = &s
	}
	if p.Creator != nil {
		dto.Creator = userRefDTOFrom(publicBaseURL, p.Creator)
	}
	if p.Updater != nil {
		dto.Updater = userRefDTOFrom(publicBaseURL, p.Updater)
	}
	// Stored links pass through VERBATIM: the
	// staff surface shows the truth for fixing; the public read guards.
	downloads := make([]blogPostDownloadDTO, 0, len(p.Downloads))
	for _, d := range p.Downloads {
		downloads = append(downloads, blogPostDownloadDTO{
			Resolution: d.Label,
			MagnetURL:  d.MagnetLink,
			TorrentURL: d.TorrentLink,
		})
	}
	dto.Downloads = downloads
	return dto
}

// staffRequester resolves the authenticated session plus the role weight
// the draft-visibility gate needs. Writes the 401 and returns false when
// unauthenticated.
func staffRequester(w http.ResponseWriter, r *http.Request) (*identity.SessionUser, int, bool) {
	su, ok := requireSession(w, r, "staff operation requires authentication")
	if !ok {
		return nil, 0, false
	}
	return su, identity.RoleWeight(su.Role), true
}

// BlogStaffList returns the handler for GET /api/v1/admin/blog-posts:
// every status, keyset updated_at_ms DESC id ASC, the draft-visibility
// gate applied, narrowed by the optional status/q filters. Cursor
// namespace av1. (the timestamp slot carries updated_at_ms).
func BlogStaffList(db *sql.DB, publicBaseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		su, weight, ok := staffRequester(w, r)
		if !ok {
			return
		}
		if !identity.CanModerateContent(su.Role) {
			writeForbidden(w, r, "staff content list requires moderator role")
			return
		}

		query, limit, after, ok := parseKeysetParams(w, r, 20, 100, blogAdminCursorPrefix, "status", "q")
		if !ok {
			return
		}
		filter, ok := parseStaffContentFilter(w, r, query)
		if !ok {
			return
		}

		writeKeysetPage(w, r, "staff blog list", keysetPage[store.StaffBlogPostSummary, staffBlogPostSummaryDTO]{
			count: func(ctx context.Context) (int, error) {
				return store.CountStaffBlogPosts(ctx, db, su.UserID, weight, filter)
			},
			read: func(ctx context.Context) ([]store.StaffBlogPostSummary, bool, error) {
				var key *store.AdminPageKey
				if after != nil {
					// The av1. namespace carries updated_at_ms in the codec's
					// timestamp slot — the staff list sorts by last edit.
					key = &store.AdminPageKey{UpdatedAtMS: after.PublishedAtMS, ID: after.ID}
				}
				return store.ListStaffBlogPosts(ctx, db, limit, key, su.UserID, weight, filter)
			},
			item: func(p store.StaffBlogPostSummary) staffBlogPostSummaryDTO {
				item := staffBlogPostSummaryDTO{
					ID:           p.ID,
					Title:        p.Title,
					Subtitle:     p.Subtitle,
					Description:  p.Description,
					ThumbnailURL: mediaURL(publicBaseURL, p.ThumbnailURL),
					Status:       p.Status,
					UpdatedAt:    formatAPITime(p.UpdatedAtMS),
					Revision:     p.Revision,
				}
				if p.PublishedAtMS != nil {
					s := formatAPITime(*p.PublishedAtMS)
					item.PublishedAt = &s
				}
				if p.Creator != nil {
					item.Creator = userRefDTOFrom(publicBaseURL, p.Creator)
				}
				return item
			},
			next: func(last store.StaffBlogPostSummary) string {
				return encodeCursor(blogAdminCursorPrefix, last.UpdatedAtMS, last.ID)
			},
		})
	}
}

// BlogStaffDetail returns the handler for GET /api/v1/admin/blog-posts/{id}:
// the staff-shaped row under the draft-visibility gate, with the
// revision-backed ETag the edit form sends back as If-Match.
func BlogStaffDetail(db *sql.DB, publicBaseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		su, weight, ok := staffRequester(w, r)
		if !ok {
			return
		}
		if !identity.CanModerateContent(su.Role) {
			writeForbidden(w, r, "staff content detail requires moderator role")
			return
		}
		if !allowNoQueryParams(w, r) {
			return
		}
		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid blog post id")
			return
		}

		post, err := store.GetStaffBlogPost(r.Context(), db, id, su.UserID, weight)
		if err != nil {
			writeStoreError(w, r, err, "staff blog detail", "error", err)
			return
		}

		w.Header().Set("ETag", etagForRevision(post.Revision))
		writeJSON(w, r, http.StatusOK, staffDTO(publicBaseURL, post))
	}
}

// staffReadGate enforces the draft-visibility gate for a write: a pre-read
// through the SAME visibility filter as the staff detail. A moderator cannot
// address an admin's draft — masked 404, indistinguishable from an unknown
// id. It writes the response and returns false on any failure; op labels the
// failure log.
func staffReadGate(w http.ResponseWriter, r *http.Request, db *sql.DB, id string, su *identity.SessionUser, weight int, op string) bool {
	if _, err := store.GetStaffBlogPost(r.Context(), db, id, su.UserID, weight); err != nil {
		writeStoreError(w, r, err, op+" pre-read", "error", err)
		return false
	}
	return true
}
