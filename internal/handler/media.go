package handler

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/media"
)

// Media serving and URL construction.
//
// Storage contract: the media directory lives under the data directory as
//
//	<data-dir>/media/images/<first-2-hex-of-id>/<id>.<ext>
//
// The database stores the storage-relative path
// `media/images/<first-2-hex>/<id>.<ext>`; API responses emit the absolute
// URL of the served pattern `/media/images/<id>.<ext>` (via
// media.ServedPath). Serving is public, read-only, and immutable: IDs are
// never reused for different content (the import derives them from the
// source bytes), so a long-lived Cache-Control is safe.

// mediaIDPattern pins the served identifier: exactly 32 lowercase hex
// characters — the generated identifier shape (docs/patterns/go/ids.md),
// matching the serving rule "[0-9a-f]{32}". Anything else is rejected
// before any filesystem path is built.
var mediaIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// mediaExtensions maps a served extension to its Content-Type. The pipeline
// writes jpg or png (JPEG 85 output, PNG for transparency). `.jpeg` is
// tolerated by the parser for hand-placed legacy files but is never written
// by the pipeline, so it normally 404s; WebP is decode-only and never stored.
var mediaExtensions = map[string]string{
	"jpg":  "image/jpeg",
	"jpeg": "image/jpeg",
	"png":  "image/png",
}

// ServeMedia returns the handler for GET /media/images/{file}.
//
// Path safety: the raw segment is never concatenated into a
// filesystem path. The handler first requires the segment to be exactly
// `<32-hex>.<ext>` with an accepted extension — which rejects `..`, extra
// separators (including %2F-encoded ones, since r.PathValue returns the
// decoded segment), null bytes, and unknown extensions — and then rebuilds
// the filesystem path from the validated components only.
//
// Responses are plain status errors, not RFC 9457 problems: media serving
// is asset delivery outside /api/v1, and the problem vocabulary is the API
// contract. X-Request-ID still comes from the route middleware.
func ServeMedia(dataDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		id, ext, ok := parseMediaFile(r.PathValue("file"))
		if !ok {
			// Malformed media segment: 400 — the
			// request never reaches the filesystem.
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Rebuild the path from validated components only; the
		// layout itself lives in internal/media (single source of truth).
		rel := media.RelativePath(id, ext)
		full := filepath.Join(dataDir, rel)

		f, err := os.Open(full)
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			// The asset id identifies the request; the *os.PathError text stays
			// out because it carries the storage path (docs/patterns/go/logging.md
			// rule 8), so an error with no cause records none.
			attrs := []any{"file", id}
			if cause := errors.Unwrap(err); cause != nil {
				attrs = append(attrs, "error", cause)
			}
			logger.ErrorContext(r.Context(), "media open failed", attrs...)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		defer f.Close()

		w.Header().Set("Content-Type", mediaExtensions[ext])
		// Immutable: the ID is never reused for different bytes.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		// No modtime: Last-Modified would add nothing for immutable content.
		http.ServeContent(w, r, "", time.Time{}, f)
	}
}

// parseMediaFile validates one served media segment: exactly 32 lowercase
// hex characters, a dot, and an accepted extension. The boolean result is
// the whole contract — callers must not touch the filesystem when it is
// false.
func parseMediaFile(file string) (id, ext string, ok bool) {
	dot := strings.LastIndexByte(file, '.')
	if dot <= 0 || dot == len(file)-1 {
		return "", "", false
	}
	id, ext = file[:dot], file[dot+1:]
	if !mediaIDPattern.MatchString(id) {
		return "", "", false
	}
	if _, known := mediaExtensions[ext]; !known {
		return "", "", false
	}
	return id, ext, true
}

// unwrappedCause returns the cause of a filesystem error — the form safe to
// log: an *os.PathError's text carries the storage path, which never reaches
// a log record (docs/patterns/go/logging.md rule 8). A cause-less error is
// returned unchanged.
func unwrappedCause(err error) error {
	if cause := errors.Unwrap(err); cause != nil {
		return cause
	}
	return err
}

// mediaURL builds the absolute public URL for a stored media reference
// (the DB stores the relative path; the full URL is
// constructed from the configured public base URL). The storage-to-served
// mapping (dropping the 2-hex subdirectory) lives in media.ServedPath —
// layout knowledge has exactly one home.
//
// Interim rule: until cmd/importmedia rewrites legacy references,
// the database can still hold the verbatim legacy absolute URL that
// cmd/importdata copied in. Those values pass through unchanged, but only
// when they parse as absolute http(s) URLs — anything else yields "" so a
// malformed stored value can never smuggle another scheme (javascript:,
// data:) into the JSON. The reference rewrite belongs to the migration
// command, not to the read API.
func mediaURL(baseURL, stored string) string {
	if stored == "" {
		return ""
	}
	if strings.HasPrefix(stored, "media/") {
		return baseURL + "/" + media.ServedPath(stored)
	}
	if u, err := url.Parse(stored); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return stored
}

// avatarURLOrNull projects a stored avatar reference: NULL and values that
// fail the mediaURL scheme guard both emit JSON null — a stored value can
// never smuggle another scheme into the JSON (same rationale as
// mediaURL/safeDownloadLink).
func avatarURLOrNull(publicBaseURL string, stored *string) *string {
	if stored == nil {
		return nil
	}
	u := mediaURL(publicBaseURL, *stored)
	if u == "" {
		return nil
	}
	return &u
}
