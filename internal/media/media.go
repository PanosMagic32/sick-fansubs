// Package media owns the media storage layout and the pure-Go image
// processing pipeline. It is the single source of truth for where a
// processed image lives and how an uploaded image becomes the served
// file — the upload handlers and the serving path both build through this
// package.
//
// # Layout
//
//	<data-directory>/media/images/<first-2-hex-of-id>/<id>.<ext>
//
// IDs are opaque 32-hex strings minted by internal/id (docs/patterns/go/ids.md).
// The database stores the relative path `media/images/<first-2-hex-of-id>/<id>.<ext>`;
// API responses construct the absolute URL from the configured public base URL.
package media

import (
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Accepted served extensions. The pipeline encodes JPEG (quality 85) or PNG
// (transparency); WebP is decode-only and never stored.
const (
	ExtJPG  = "jpg"
	ExtJPEG = "jpeg"
	ExtPNG  = "png"
)

// tempPrefix names the atomic-write temp files (writeAtomic). The orphan
// sweep removes stale ones older than the grace window —
// the prefix lives here so the writer and the sweeper can never drift apart.
const tempPrefix = ".processing-"

// RelativePath builds the database-facing relative path for a processed
// image. Callers must have validated id as 32 lowercase hex characters and
// ext as one of the accepted extensions (the pipeline writes jpg or png;
// jpeg is a legacy serving form); the upload handlers validate up front, so
// this package trusts its callers.
func RelativePath(id, ext string) string {
	return filepath.Join("media", "images", id[:2], id+"."+ext)
}

// imageDir returns the absolute directory that holds a given image's file.
func imageDir(dataDir, id string) string {
	return filepath.Join(dataDir, "media", "images", id[:2])
}

// ImagePath returns the absolute filesystem path of a processed image.
func ImagePath(dataDir, id, ext string) string {
	return filepath.Join(dataDir, RelativePath(id, ext))
}

// ServedPath maps a stored relative media path to its served URL path:
// the storage layout nests files under a 2-hex subdirectory
// (`media/images/ab/<id>.<ext>`) but the served pattern omits it —
// `/media/images/<id>.<ext>`. The serving handler derives the
// subdirectory from the ID, so the wire URL never exposes it. A stored
// value that does not match the four-segment `media/images/<two-char>/<file>`
// shape is returned unchanged.
func ServedPath(stored string) string {
	parts := strings.Split(stored, "/")
	if len(parts) == 4 && parts[0] == "media" && parts[1] == "images" && len(parts[2]) == 2 {
		return parts[0] + "/" + parts[1] + "/" + parts[3]
	}
	return stored
}

// ParseStorageReference validates one storage-relative media reference —
// the value form the content DTO accepts:
// `media/images/<2hex>/<id>.<ext>` with a 32-hex id, a jpg/png extension,
// and the subdirectory equal to the id's first two characters (the layout's
// own derivation, so a reference the write gate accepts can never address a
// different file than the sweep and the delete guard resolve). It returns the
// validated components; callers must not touch the filesystem when ok is
// false. The grammar deliberately differs from the SERVED pattern
// (`/media/images/<id>.<ext>`) — the storage form carries the 2-hex
// subdirectory the wire never exposes.
func ParseStorageReference(ref string) (id, ext string, ok bool) {
	parts := strings.Split(ref, "/")
	if len(parts) != 4 || parts[0] != "media" || parts[1] != "images" || len(parts[2]) != 2 {
		return "", "", false
	}
	id, ext, ok = parseStorageFile(parts[3])
	if !ok {
		return "", "", false
	}
	if !isLowerHex(parts[2]) || parts[2] != id[:2] {
		return "", "", false
	}
	return id, ext, true
}

// parseStorageFile validates one `<32hex>.<ext>` storage filename.
func parseStorageFile(file string) (id, ext string, ok bool) {
	return parseFileWithExt(file, ExtJPG, ExtPNG)
}

// parseCanonicalFile is the serving parser's extension set: the serving
// handler also accepts a hand-placed .jpeg, so the reference guard must
// resolve it to a local file rather than treating it as unreferenced. The
// write gate stays on the stricter parseStorageFile set (the pipeline never
// writes .jpeg).
func parseCanonicalFile(file string) (id, ext string, ok bool) {
	return parseFileWithExt(file, ExtJPG, ExtJPEG, ExtPNG)
}

func parseFileWithExt(file string, exts ...string) (id, ext string, ok bool) {
	dot := strings.LastIndexByte(file, '.')
	if dot <= 0 || dot == len(file)-1 {
		return "", "", false
	}
	id, ext = file[:dot], file[dot+1:]
	if len(id) != 32 || !isLowerHex(id) {
		return "", "", false
	}
	if slices.Contains(exts, ext) {
		return id, ext, true
	}
	return "", "", false
}

// CanonicalStoragePath maps a stored media reference to the canonical
// storage path `media/images/<id[:2]>/<id>.<ext>`. It accepts every form the
// read path still serves: the canonical storage form, the flat served form
// (`media/images/<id>.<ext>`), the served form carrying a subdirectory
// (`media/images/<any-two-chars>/<id>.<ext>` — ServedPath drops the segment),
// a relative form carrying a query, fragment, `.` segment, or percent-encoded
// bytes (the browser and ServeMux clean and decode it), and an absolute
// http(s) URL on the configured public host with any port, either scheme, a
// case-insensitive scheme, a trailing-dot hostname, or the host's `www` twin
// (the edge redirects between the two). The absolute form compares normalized
// hostnames, not raw Host values, and ignoring the port can only widen the
// keep-set, never drop a live reference. Values that cannot name a local file
// (another hostname, an escaping path, a malformed segment, an unsupported
// extension) are reported not-ok; callers skip them.
func CanonicalStoragePath(stored, publicBaseURL string) (string, bool) {
	candidate, ok := localMediaPath(stored, publicBaseURL)
	if !ok {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(candidate, "media/images/"), "/")
	var file string
	switch len(parts) {
	case 1:
		file = parts[0]
	case 2:
		// The read path drops any two-character subdirectory, so every
		// such form addresses the canonical file.
		if len(parts[0]) != 2 {
			return "", false
		}
		file = parts[1]
	default:
		return "", false
	}
	id, ext, ok := parseCanonicalFile(file)
	if !ok {
		return "", false
	}
	return RelativePath(id, ext), true
}

// localMediaPath reduces a stored reference to the cleaned, decoded
// `media/images/...` path the read path serves, or reports not-ok. An absolute
// http(s) URL must name the configured public host (or its `www` twin); the
// base path prefix, when present, is stripped like the serving URL does.
// Relative values lose their query and fragment, then decode and clean exactly
// as the browser and the router do. The leading slash anchors path.Clean, so
// `..` can never escape the media tree.
func localMediaPath(stored, publicBaseURL string) (string, bool) {
	candidate := stored
	if u, err := url.Parse(candidate); err == nil && u.Scheme != "" && u.Host != "" {
		switch strings.ToLower(u.Scheme) {
		case "http", "https":
		default:
			return "", false
		}
		if publicBaseURL == "" {
			return "", false
		}
		base, err := url.Parse(publicBaseURL)
		if err != nil || base.Hostname() == "" || !sameMediaHost(u.Hostname(), base.Hostname()) {
			return "", false
		}
		candidate = u.Path
		if prefix := strings.TrimRight(base.Path, "/"); prefix != "" && prefix != "/" {
			if !strings.HasPrefix(candidate, prefix+"/") {
				return "", false
			}
			candidate = strings.TrimPrefix(candidate, prefix)
		}
	} else if i := strings.IndexAny(candidate, "?#"); i >= 0 {
		candidate = candidate[:i]
	}

	decoded, err := url.PathUnescape(candidate)
	if err != nil {
		return "", false
	}
	cleaned := path.Clean("/" + strings.TrimPrefix(decoded, "/"))
	cleaned = strings.TrimPrefix(cleaned, "/")
	if !strings.HasPrefix(cleaned, "media/images/") {
		return "", false
	}
	return cleaned, true
}

// sameMediaHost compares two hostnames after lowercasing, trimming one
// trailing dot, and treating a leading `www.` as optional: the production edge
// redirects www to the apex (and the reverse alias is possible), so both names
// address the same served media file.
func sameMediaHost(a, b string) bool {
	normalize := func(host string) string {
		host = strings.ToLower(host)
		host = strings.TrimSuffix(host, ".")
		return strings.TrimPrefix(host, "www.")
	}
	return normalize(a) == normalize(b)
}

// isLowerHex reports whether s consists of lowercase hex characters only.
// Callers bound the length.
func isLowerHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
