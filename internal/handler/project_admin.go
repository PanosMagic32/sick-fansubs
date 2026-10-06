package handler

import (
	"context"
	"database/sql"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/store"
)

// Project staff-administration handlers: the
// blog-admin handlers' counterpart for projects — create with a
// server-generated slug, staff reads with the draft-visibility gate,
// revision-conditional update/delete, and the audit events the
// blog handlers emit.

// The title/description bounds mirror the blog write contract.
const (
	maxProjectTitleRunes       = 200
	maxProjectDescriptionRunes = 20_000

	// Slug collision retries: the base, then -2 … -100. A base
	// that survives 100 collisions means hundreds of same-title rows — the
	// loop is bounded to keep create a fixed-cost operation.
	maxSlugAttempts = 100
)

// projectCreateRequest is the create body: NO slug field — a
// slug key in a create body is an unknown field and a 400 under the strict
// body reader. Pointer fields distinguish absent from empty.
type projectCreateRequest struct {
	Title         string                 `json:"title"`
	Description   *string                `json:"description"`
	ThumbnailPath string                 `json:"thumbnailPath"`
	Status        *string                `json:"status"`
	Downloads     []projectDownloadWrite `json:"downloads"`
}

// projectUpdateRequest adds the optional slug — admin+ only; a
// moderator-submitted slug is a 403 and the form hides the field for them.
type projectUpdateRequest struct {
	Title         string                 `json:"title"`
	Description   *string                `json:"description"`
	Slug          *string                `json:"slug"`
	ThumbnailPath string                 `json:"thumbnailPath"`
	Status        *string                `json:"status"`
	Downloads     []projectDownloadWrite `json:"downloads"`
}

// projectDownloadWrite is one row of the write DTO's downloads array: the
// batch name plus the magnet/torrent link
// pair. The shared validator (downloads.go) applies the bounds and the
// link grammar; the violation paths carry this field's name.
type projectDownloadWrite struct {
	Name       string  `json:"name"`
	MagnetURL  *string `json:"magnetUrl"`
	TorrentURL *string `json:"torrentUrl"`
}

// projectWriteRequest is the validated common write shape. DownloadsParsed
// holds the validated download rows.
type projectWriteRequest struct {
	Title           string
	Description     string
	ThumbnailPath   string
	Status          string
	DownloadsParsed []store.Download
}

// staffProjectDTO is the staff-read wire shape: the public
// detail fields plus slug, thumbnailPath, status, revision, and
// timestamps — the admin form needs all of them. publishedAt is nullable
// (a draft has no stamp). Downloads are the STORED rows verbatim (no link
// guard — the form must show what is stored so a bad value can be fixed).
type staffProjectDTO struct {
	ID            string               `json:"id"`
	Title         string               `json:"title"`
	Description   string               `json:"description"`
	Slug          string               `json:"slug"`
	ThumbnailPath string               `json:"thumbnailPath"`
	ThumbnailURL  string               `json:"thumbnailUrl"`
	Status        string               `json:"status"`
	PublishedAt   *string              `json:"publishedAt"`
	CreatedAt     string               `json:"createdAt"`
	UpdatedAt     string               `json:"updatedAt"`
	Revision      int64                `json:"revision"`
	Creator       *userRefDTO          `json:"creator"`
	Updater       *userRefDTO          `json:"updater"`
	Downloads     []projectDownloadDTO `json:"downloads"`
}

// staffProjectSummaryDTO is the staff-list card: the summary plus status,
// slug, and revision (the dashboard's badges + edit link).
type staffProjectSummaryDTO struct {
	ID           string      `json:"id"`
	Title        string      `json:"title"`
	Description  string      `json:"description"`
	Slug         string      `json:"slug"`
	ThumbnailURL *string     `json:"thumbnailUrl"`
	Status       string      `json:"status"`
	PublishedAt  *string     `json:"publishedAt"`
	UpdatedAt    string      `json:"updatedAt"`
	Revision     int64       `json:"revision"`
	Creator      *userRefDTO `json:"creator"`
}

// parseProjectCreate validates one create body: title trimmed 1..200
// runes; description trimmed, default ""; thumbnailPath required; status
// draft|published, default published; downloads required + validated.
func parseProjectCreate(w http.ResponseWriter, r *http.Request) (projectWriteRequest, bool) {
	var body projectCreateRequest
	if err := readJSONLimit(w, r, &body, maxContentWriteBodySize); err != nil {
		writeJSONBodyError(w, r, err)
		return projectWriteRequest{}, false
	}
	return validateProjectWrite(w, r, body.Title, body.Description, body.ThumbnailPath, body.Status, body.Downloads, "draft", "published")
}

// parseProjectUpdate validates one update body: the create fields plus the
// optional slug (nil = absent — the caller falls back to the current row's
// slug). The slug's role/grammar/uniqueness checks happen in the caller
// (they need the session role and the store's unique backstop).
func parseProjectUpdate(w http.ResponseWriter, r *http.Request) (projectWriteRequest, *string, bool) {
	var body projectUpdateRequest
	if err := readJSONLimit(w, r, &body, maxContentWriteBodySize); err != nil {
		writeJSONBodyError(w, r, err)
		return projectWriteRequest{}, nil, false
	}
	req, ok := validateProjectWrite(w, r, body.Title, body.Description, body.ThumbnailPath, body.Status, body.Downloads, "draft", "published", "archived")
	if !ok {
		return req, nil, false
	}
	return req, body.Slug, true
}

// validateProjectWrite applies the shared field checks. allowedStatuses is
// the create form's draft|published or the update form's
// draft|published|archived (archived is an edit-time state).
func validateProjectWrite(w http.ResponseWriter, r *http.Request, title string, description *string, thumbnailPath string, statusField *string, downloads []projectDownloadWrite, allowedStatuses ...string) (projectWriteRequest, bool) {
	title = strings.TrimSpace(title)
	if title == "" {
		writeValidationErrors(w, r, []Violation{{Field: "title", Code: "required"}})
		return projectWriteRequest{}, false
	}
	if utf8.RuneCountInString(title) > maxProjectTitleRunes {
		writeValidationErrors(w, r, []Violation{{Field: "title", Code: "maxLength"}})
		return projectWriteRequest{}, false
	}

	desc := ""
	if description != nil {
		desc = strings.TrimSpace(*description)
		if utf8.RuneCountInString(desc) > maxProjectDescriptionRunes {
			writeValidationErrors(w, r, []Violation{{Field: "description", Code: "maxLength"}})
			return projectWriteRequest{}, false
		}
	}

	if strings.TrimSpace(thumbnailPath) == "" {
		writeValidationErrors(w, r, []Violation{{Field: "thumbnailPath", Code: "required"}})
		return projectWriteRequest{}, false
	}

	status := "published" // the form defaults to published
	if statusField != nil {
		status = *statusField
	}
	if !slices.Contains(allowedStatuses, status) {
		writeValidationErrors(w, r, []Violation{{Field: "status", Code: "invalidValue"}})
		return projectWriteRequest{}, false
	}

	// Downloads: the shared contract —
	// required, bounded, each row needs a name and at least one link, and
	// every link must pass the READ boundary's grammar.
	rows := make([]downloadRow, 0, len(downloads))
	for _, d := range downloads {
		rows = append(rows, downloadRow{Label: d.Name, MagnetURL: d.MagnetURL, TorrentURL: d.TorrentURL})
	}
	validated, ok := validateDownloadRows(w, r, "name", rows)
	if !ok {
		return projectWriteRequest{}, false
	}

	return projectWriteRequest{Title: title, Description: desc, ThumbnailPath: thumbnailPath, Status: status, DownloadsParsed: validated}, true
}

// staffProjectDTOFrom builds the staff wire shape for one store row.
func staffProjectDTOFrom(publicBaseURL string, p *store.StaffProject) staffProjectDTO {
	dto := staffProjectDTO{
		ID:            p.ID,
		Title:         p.Title,
		Description:   p.Description,
		Slug:          p.Slug,
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
	downloads := make([]projectDownloadDTO, 0, len(p.Downloads))
	for _, d := range p.Downloads {
		downloads = append(downloads, projectDownloadDTO{
			Name:       d.Label,
			MagnetURL:  d.MagnetLink,
			TorrentURL: d.TorrentLink,
		})
	}
	dto.Downloads = downloads
	return dto
}

// projectStaffReadGate enforces the draft-visibility gate for a write AND
// returns the row (the update path needs the current slug as its
// absent-field fallback). The blog variant discards the row; this one keeps
// it — same visibility filter, different consumer.
func projectStaffReadGate(w http.ResponseWriter, r *http.Request, db *sql.DB, id string, su *identity.SessionUser, weight int, op string) (*store.StaffProject, bool) {
	cur, err := store.GetStaffProject(r.Context(), db, id, su.UserID, weight)
	if err != nil {
		writeStoreError(w, r, err, op+" pre-read", "error", err)
		return nil, false
	}
	return cur, true
}

// ProjectStaffList returns the handler for GET /api/v1/admin/projects:
// every status, keyset updated_at_ms DESC id ASC, the draft-visibility
// gate applied. Cursor namespace apv1. (the timestamp slot carries
// updated_at_ms).
func ProjectStaffList(db *sql.DB, publicBaseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		su, weight, ok := staffRequester(w, r)
		if !ok {
			return
		}
		if !identity.CanModerateContent(su.Role) {
			writeForbidden(w, r, "staff content list requires moderator role")
			return
		}

		query, limit, after, ok := parseKeysetParams(w, r, 20, 100, projectAdminCursorPrefix, "status", "q")
		if !ok {
			return
		}
		filter, ok := parseStaffContentFilter(w, r, query)
		if !ok {
			return
		}

		writeKeysetPage(w, r, "staff project list", keysetPage[store.StaffProjectSummary, staffProjectSummaryDTO]{
			count: func(ctx context.Context) (int, error) {
				return store.CountStaffProjects(ctx, db, su.UserID, weight, filter)
			},
			read: func(ctx context.Context) ([]store.StaffProjectSummary, bool, error) {
				var key *store.AdminPageKey
				if after != nil {
					// The apv1. namespace carries updated_at_ms in the codec's
					// timestamp slot — the staff list sorts by last edit.
					key = &store.AdminPageKey{UpdatedAtMS: after.PublishedAtMS, ID: after.ID}
				}
				return store.ListStaffProjects(ctx, db, limit, key, su.UserID, weight, filter)
			},
			item: func(p store.StaffProjectSummary) staffProjectSummaryDTO {
				item := staffProjectSummaryDTO{
					ID:           p.ID,
					Title:        p.Title,
					Description:  p.Description,
					Slug:         p.Slug,
					ThumbnailURL: thumbnailURLOrNull(publicBaseURL, p.ThumbnailURL),
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
			next: func(last store.StaffProjectSummary) string {
				return encodeCursor(projectAdminCursorPrefix, last.UpdatedAtMS, last.ID)
			},
		})
	}
}

// ProjectStaffDetail returns the handler for GET /api/v1/admin/projects/{id}:
// the staff-shaped row under the draft-visibility gate, with the
// revision-backed ETag the edit form sends back as If-Match.
func ProjectStaffDetail(db *sql.DB, publicBaseURL string) http.HandlerFunc {
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
			writeBadRequest(w, r, "invalid project id")
			return
		}

		project, err := store.GetStaffProject(r.Context(), db, id, su.UserID, weight)
		if err != nil {
			writeStoreError(w, r, err, "staff project detail", "error", err)
			return
		}

		w.Header().Set("ETag", etagForRevision(project.Revision))
		writeJSON(w, r, http.StatusOK, staffProjectDTOFrom(publicBaseURL, project))
	}
}
