package handler

import (
	"errors"
	"mime/multipart"
	"net/http"

	"sick-fansubs/internal/id"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/media"
)

// MediaUpload is the handler for POST /api/v1/media:
// the upload-first flow. An admin uploads an image,
// the pipeline processes it, and the response returns the exact
// storage-relative `path` the content create/update DTO accepts as its
// `thumbnailUrl` — the client never hand-builds a media reference.
//
// The route chain is RequestID → TrustedOrigin → Session →
// ForcePasswordChange → CSRF (the CSRF middleware validates the
// X-CSRF-Token HEADER, so the multipart body is untouched by it). No rate
// limit and no audit event — the upload rulings in internal/handler/AGENTS.md.
//
// Framing: the WHOLE request body is bounded with
// http.MaxBytesReader (file bound + multipart overhead slack), so the file
// itself is capped at media.MaxInputBytes. Extra/unknown form fields are
// rejected (strict known fields). The file's declared
// Content-Type is ignored — the pipeline sniffs magic bytes.

// mediaUploadSlack is the multipart framing allowance on top of the file
// bound (boundary, part headers). MaxBytesReader caps the total body, so
// the file bound and this slack compose the wire limit.
const mediaUploadSlack = 1 << 20

// mediaUploadDTO is the 201 wire response. Path is the
// storage-relative value for the content DTO; URL is the absolute served
// URL for the widget preview.
type mediaUploadDTO struct {
	Path string `json:"path"`
	URL  string `json:"url"`
}

// MediaUpload returns the upload handler. publicBaseURL feeds the absolute
// URL field (the same value every media-emitting handler receives).
func MediaUpload(dataDir, publicBaseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su, ok := requireSession(w, r, "media upload requires authentication")
		if !ok {
			return
		}
		if !identity.CanUploadMedia(su.Role) {
			writeForbidden(w, r, "media upload requires admin role")
			return
		}

		src, ok := readUploadFile(w, r)
		if !ok {
			return
		}
		defer src.Close()

		// Fresh random ID — uploads never reuse the migration's
		// content-derived IDs.
		uploadID, err := id.New()
		if err != nil {
			writeInternalError(w, r, logger, "media upload id generation failed", "error", err)
			return
		}

		rel, err := media.ProcessContentThumbnail(dataDir, uploadID, src)
		if err != nil {
			writeProcessError(w, r, err)
			return
		}

		// The created resource IS the image: Location points at its served
		// URL path (there is no media GET-by-id).
		w.Header().Set("Location", "/"+media.ServedPath(rel))
		writeJSON(w, r, http.StatusCreated, mediaUploadDTO{
			Path: rel,
			URL:  mediaURL(publicBaseURL, rel),
		})
	}
}

// readUploadFile frames and parses the multipart body shared by the two
// upload endpoints (POST /api/v1/media and PUT /api/v1/users/me/avatar).
//
// On failure it WRITES the matching response and returns nil, false — the
// caller must not continue. On success it returns the opened file part and
// true; the caller closes it.
func readUploadFile(w http.ResponseWriter, r *http.Request) (multipart.File, bool) {
	// Frame the whole multipart body before parsing: a request larger
	// than file bound + slack dies here with a MaxBytesError instead of
	// spooling an unbounded temp file.
	r.Body = http.MaxBytesReader(w, r.Body, media.MaxInputBytes+mediaUploadSlack)
	if err := r.ParseMultipartForm(mediaUploadSlack); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeViolationProblems(w, r, http.StatusRequestEntityTooLarge,
				[]Violation{{Field: "file", Code: "tooLarge"}})
			return nil, false
		}
		writeBadRequest(w, r, "multipart parse failed")
		return nil, false
	}

	// Strict known fields: non-file parts are always rejected, and more
	// than one part is ambiguous (unknown fields never
	// silently pass).
	if len(r.MultipartForm.Value) > 0 || len(r.MultipartForm.File) > 1 {
		writeBadRequest(w, r, "unexpected multipart fields")
		return nil, false
	}
	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		if len(r.MultipartForm.File) == 1 {
			// The one part present is not named "file".
			writeBadRequest(w, r, "unexpected multipart fields")
			return nil, false
		}
		writeValidationErrors(w, r, []Violation{{Field: "file", Code: "required"}})
		return nil, false
	}
	if len(files) > 1 {
		writeBadRequest(w, r, "duplicate file field")
		return nil, false
	}
	src, err := files[0].Open()
	if err != nil {
		writeBadRequest(w, r, "cannot open uploaded file")
		return nil, false
	}
	return src, true
}

// writeProcessError maps a media pipeline processing error to its HTTP
// response — the shared outcome table of both upload endpoints:
// size/pixel bounds share the 413 file:tooLarge violation,
// header/decode failures are 422 file:invalidFormat, and everything else
// is a logged generic 500.
func writeProcessError(w http.ResponseWriter, r *http.Request, err error) {
	logger := logging.From(r.Context())
	switch {
	case errors.Is(err, media.ErrInputTooLarge),
		errors.Is(err, media.ErrPixelsTooBig),
		errors.Is(err, media.ErrDimensionsTooBig):
		// Size-bound outcomes share one violation.
		writeViolationProblems(w, r, http.StatusRequestEntityTooLarge,
			[]Violation{{Field: "file", Code: "tooLarge"}})
	case errors.Is(err, media.ErrDecodeHeader),
		errors.Is(err, media.ErrDecode):
		writeValidationErrors(w, r, []Violation{{Field: "file", Code: "invalidFormat"}})
	default:
		writeInternalError(w, r, logger, "upload processing failed", "error", err)
	}
}
