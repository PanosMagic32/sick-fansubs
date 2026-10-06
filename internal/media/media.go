// Package media owns the target media storage layout and
// the pure-Go image processing pipeline. It is the single source of truth
// for where a processed image lives and how an uploaded/migrated image
// becomes the served file — serving (internal/handler) and migration
// (internal/migration) both build paths through this package.
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
	"path/filepath"
	"strings"
)

// Accepted served extensions. The pipeline encodes JPEG (quality 85) or PNG
// (transparency); WebP is decode-only and never stored.
const (
	ExtJPG = "jpg"
	ExtPNG = "png"
)

// tempPrefix names the atomic-write temp files (writeAtomic). The orphan
// sweep removes stale ones older than the grace window —
// the prefix lives here so the writer and the sweeper can never drift apart.
const tempPrefix = ".processing-"

// RelativePath builds the database-facing relative path for a processed
// image. Callers must have validated id as 32 lowercase hex characters and
// ext as one of the accepted extensions — this package trusts its callers
// (the serving handler and the import command both validate up front).
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
	file := parts[3]
	dot := strings.LastIndexByte(file, '.')
	if dot <= 0 || dot == len(file)-1 {
		return "", "", false
	}
	id, ext = file[:dot], file[dot+1:]
	if len(id) != 32 || ext != ExtJPG && ext != ExtPNG {
		return "", "", false
	}
	if !isLowerHex(parts[2]) || !isLowerHex(id) || parts[2] != id[:2] {
		return "", "", false
	}
	return id, ext, true
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
