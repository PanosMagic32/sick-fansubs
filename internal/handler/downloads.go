package handler

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"sick-fansubs/internal/store"
)

// Shared download-row contract for the content write DTOs: both content
// types carry a full-replacement
// `downloads` array on create and update — blog rows key their label on
// `resolution`, project rows on `name`; both share magnetUrl/torrentUrl.
//
// The server-side link grammar is the READ boundary's safeDownloadLink
// (blog_detail.go): staff can never STORE a value the public read would
// null out. Field bounds: at least one row (download
// links are required), at most maxDownloadRows rows, labels 1..100 runes,
// links at most 2000 runes (the legacy magnet links measured ≤ 480), and
// each row needs at least one of its two links.

const (
	maxDownloadRows       = 20
	maxDownloadLabelRunes = 100
	maxDownloadLinkRunes  = 2000

	// maxContentWriteBodySize bounds the content create/update bodies: the
	// downloads array drives the size — 20 rows × (label + two 2000-rune links) ≈ 82,000 runes, up
	// to ~328,000 bytes (~320 KiB) in worst-case UTF-8 (4 bytes/rune),
	// plus the 20,000-rune description (~80 KiB) — so the content write
	// endpoints cannot share the 4 KiB auth bound. Still bounded BEFORE
	// decoding.
	maxContentWriteBodySize = 512 * 1024
)

// downloadRow is the wire-neutral input for one downloads entry. The
// per-type write structs (blogDownloadWrite/projectDownloadWrite) convert
// into this shape before validation so the loop is written once; labelField
// names the label's JSON field for the violation paths.
type downloadRow struct {
	Label      string
	MagnetURL  *string
	TorrentURL *string
}

// validateDownloadRows applies the shared contract and writes the 422 on
// failure. The violation field paths carry the array index so the client
// can point at the exact row (downloads[i].magnetUrl etc.).
func validateDownloadRows(w http.ResponseWriter, r *http.Request, labelField string, rows []downloadRow) ([]store.Download, bool) {
	if len(rows) == 0 {
		writeValidationErrors(w, r, []Violation{{Field: "downloads", Code: "required"}})
		return nil, false
	}
	if len(rows) > maxDownloadRows {
		writeValidationErrors(w, r, []Violation{{Field: "downloads", Code: "maxItems"}})
		return nil, false
	}

	out := make([]store.Download, 0, len(rows))
	for i, row := range rows {
		prefix := "downloads[" + strconv.Itoa(i) + "]"

		label := strings.TrimSpace(row.Label)
		if label == "" {
			writeValidationErrors(w, r, []Violation{{Field: prefix + "." + labelField, Code: "required"}})
			return nil, false
		}
		if utf8.RuneCountInString(label) > maxDownloadLabelRunes {
			writeValidationErrors(w, r, []Violation{{Field: prefix + "." + labelField, Code: "maxLength"}})
			return nil, false
		}

		link := func(raw *string, field string) (*string, bool) {
			if raw == nil {
				return nil, true
			}
			s := strings.TrimSpace(*raw)
			if s == "" {
				// An empty string means "no link for this slot" — the form
				// may submit "" for an unused slot.
				return nil, true
			}
			if utf8.RuneCountInString(s) > maxDownloadLinkRunes {
				writeValidationErrors(w, r, []Violation{{Field: prefix + "." + field, Code: "maxLength"}})
				return nil, false
			}
			if safeDownloadLink(&s) == nil {
				writeValidationErrors(w, r, []Violation{{Field: prefix + "." + field, Code: "invalidFormat"}})
				return nil, false
			}
			return &s, true
		}

		magnet, ok := link(row.MagnetURL, "magnetUrl")
		if !ok {
			return nil, false
		}
		torrent, ok := link(row.TorrentURL, "torrentUrl")
		if !ok {
			return nil, false
		}
		if magnet == nil && torrent == nil {
			writeValidationErrors(w, r, []Violation{{Field: prefix, Code: "required"}})
			return nil, false
		}

		out = append(out, store.Download{Label: label, MagnetLink: magnet, TorrentLink: torrent})
	}
	return out, true
}
