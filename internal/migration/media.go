package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"sick-fansubs/internal/media"
)

// MediaFailure aggregates one failure reason for the reconciliation report.
// Reasons are machine codes, never URLs or filesystem paths.
type MediaFailure struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// MediaImportReport is the reconciliation output of one media import run.
type MediaImportReport struct {
	Source struct {
		BlogRowsScanned    int `json:"blogRowsScanned"`
		ProjectRowsScanned int `json:"projectRowsScanned"`
		DistinctURLs       int `json:"distinctLegacyUrls"`
	} `json:"source"`
	Outcome struct {
		Processed     int `json:"processed"`
		Failed        int `json:"failed"`
		RowsRewritten int `json:"rowsRewritten"`
	} `json:"outcome"`
	Failures []MediaFailure `json:"failures"`
}

// ImportMedia reads every legacy thumbnail reference from blog_posts and
// projects, processes each distinct URL once from sourceDir, and rewrites
// the references in the owning table. It returns the report even when
// individual objects fail — failures are counted, not fatal, so one broken
// object cannot hide the rest of the run. The join key, the content-derived
// id policy, and the re-run semantics are the contract in
// internal/migration/AGENTS.md.
func ImportMedia(ctx context.Context, db *sql.DB, dataDir, sourceDir string) (*MediaImportReport, error) {
	report := &MediaImportReport{}

	// mediaRow names the owning table next to each reference so the rewrite
	// targets the right row. The table list is a compile-time constant — the
	// query text is built over THIS list only, never from input.
	type mediaRow struct {
		table string
		id    string
		url   string
	}
	byURL := map[string][]mediaRow{}
	var urlOrder []string
	for _, table := range []string{"blog_posts", "projects"} {
		rows, err := db.QueryContext(ctx,
			fmt.Sprintf(`SELECT id, thumbnail_url FROM %s
			 WHERE thumbnail_url NOT LIKE 'media/%%'
			 ORDER BY thumbnail_url, id`, table))
		if err != nil {
			return nil, fmt.Errorf("migration: query legacy media references: %w", err)
		}
		for rows.Next() {
			var r mediaRow
			r.table = table
			if err := rows.Scan(&r.id, &r.url); err != nil {
				rows.Close()
				return nil, fmt.Errorf("migration: scan legacy media reference: %w", err)
			}
			if len(byURL[r.url]) == 0 {
				urlOrder = append(urlOrder, r.url)
			}
			byURL[r.url] = append(byURL[r.url], r)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("migration: iterate legacy media references: %w", err)
		}
		rows.Close()
	}

	failures := map[string]int{}
	addFailure := func(reason string) {
		failures[reason]++
		report.Outcome.Failed++
	}

	for _, u := range urlOrder {
		refs := byURL[u]
		for _, r := range refs {
			if r.table == "blog_posts" {
				report.Source.BlogRowsScanned++
			} else {
				report.Source.ProjectRowsScanned++
			}
		}
		report.Source.DistinctURLs++

		key, err := LegacyMediaKey(u)
		if err != nil {
			addFailure("badReferenceShape")
			continue
		}

		raw, err := readSourceBounded(filepath.Join(sourceDir, key))
		if err != nil {
			switch {
			case errors.Is(err, os.ErrNotExist):
				addFailure("missingSourceFile")
			case errors.Is(err, media.ErrInputTooLarge):
				addFailure("oversizedSource")
			default:
				addFailure("readSourceFile")
			}
			continue
		}

		// The ID derives from the source bytes, not the key (see
		// MediaIDForBytes): a changed object gets a fresh ID instead of
		// violating the immutable-cache contract.
		id := MediaIDForBytes(raw)
		rel, err := media.ProcessContentThumbnail(dataDir, id, bytes.NewReader(raw))
		if err != nil {
			addFailure(processFailureReason(err))
			continue
		}

		// One distinct URL may be shared across both tables — every row
		// referencing it is rewritten together, in its own table.
		for _, r := range refs {
			res, err := db.ExecContext(ctx,
				fmt.Sprintf(`UPDATE %s SET thumbnail_url = ? WHERE thumbnail_url = ?`, r.table), rel, u)
			if err != nil {
				return nil, fmt.Errorf("migration: rewrite thumbnail_url: %w", err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return nil, fmt.Errorf("migration: rows affected by thumbnail_url rewrite: %w", err)
			}
			report.Outcome.RowsRewritten += int(n)
		}

		report.Outcome.Processed++
	}

	report.Failures = sortedFailures(failures)
	return report, nil
}

// MediaIDForBytes derives the target media ID from the source bytes:
// hex(sha256(bytes))[:32]. Content-derived IDs are the backbone of the
// immutable-cache contract: identical bytes always map to the
// same ID (idempotent re-runs, cross-post dedupe), and changed bytes map to
// a fresh ID instead of silently overwriting a cached one.
func MediaIDForBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:32]
}

// readSourceBounded reads one source object within the media input bound
// (media.MaxInputBytes) so an oversized file is rejected before it is
// hashed or decoded.
func readSourceBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, media.MaxInputBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > media.MaxInputBytes {
		return nil, media.ErrInputTooLarge
	}
	return raw, nil
}

// LegacyMediaKey derives the fetch↔import join key for a stored legacy
// thumbnail URL. A clean legacy MinIO URL — an absolute URL whose path is
// exactly "/media/images/<key>.<ext>" with no query or fragment — keeps
// its object key. Every other absolute URL derives a stable key from the
// exact URL string: the production data hosts
// most blog thumbnails on external CDNs, so the key rule must not assume
// the MinIO shape. Only a non-absolute value is an error (reported as
// badReferenceShape) — derivation is deterministic, never a guess.
func LegacyMediaKey(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("not an absolute URL")
	}
	if rest, ok := strings.CutPrefix(u.Path, "/media/images/"); ok &&
		rest != "" && u.RawQuery == "" && u.Fragment == "" &&
		!strings.ContainsAny(rest, "/\\") && rest != "." && rest != ".." {
		if dot := strings.LastIndexByte(rest, '.'); dot > 0 && dot != len(rest)-1 {
			return rest, nil
		}
	}
	// External-host URL: hash the exact stored string so identical URLs
	// always agree on a key while URL variants (query params included) stay
	// distinct. No host allowlist — any future host is covered by the same
	// rule. The key is only the fetch↔import join; the served media ID
	// stays content-derived (MediaIDForBytes). The observed legacy keys are
	// `<ts>-<uuid>.<ext>` — never shaped like `x-<32hex>` — so the two key
	// namespaces cannot collide in production data.
	sum := sha256.Sum256([]byte(rawURL))
	return "x-" + hex.EncodeToString(sum[:])[:32] + boundedKeyExt(u.Path), nil
}

// boundedKeyExt returns a lowercase alphanumeric extension of at most five
// characters from the final URL path segment, or "" when absent or
// unsuitable. The key extension is cosmetic (the import sniffs bytes) —
// the hash carries the uniqueness, and the bound keeps the key a safe
// filename regardless of URL shape.
func boundedKeyExt(p string) string {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(p), "."))
	if ext == "" || len(ext) > 5 {
		return ""
	}
	for _, r := range ext {
		if r < 'a' || r > 'z' {
			if r < '0' || r > '9' {
				return ""
			}
		}
	}
	return "." + ext
}

// processFailureReason maps processing errors to stable report codes.
func processFailureReason(err error) string {
	switch {
	case errors.Is(err, media.ErrInputTooLarge):
		return "oversizedSource"
	case errors.Is(err, media.ErrDimensionsTooBig):
		return "dimensionsTooLarge"
	case errors.Is(err, media.ErrPixelsTooBig):
		return "pixelsTooLarge"
	case errors.Is(err, media.ErrDecodeHeader):
		return "unreadableHeader"
	case errors.Is(err, media.ErrDecode):
		return "undecodable"
	case errors.Is(err, media.ErrEncode):
		return "encodeFailed"
	case errors.Is(err, media.ErrWrite):
		return "writeFailed"
	default:
		return "processingFailed"
	}
}

// sortedFailures renders the failure map deterministically for stable
// reports across runs. The empty case is an empty slice — the report's
// section is [] when there are no failures, never null.
func sortedFailures(failures map[string]int) []MediaFailure {
	if len(failures) == 0 {
		return []MediaFailure{}
	}
	reasons := make([]string, 0, len(failures))
	for reason := range failures {
		reasons = append(reasons, reason)
	}
	slices.Sort(reasons)
	out := make([]MediaFailure, 0, len(reasons))
	for _, reason := range reasons {
		out = append(out, MediaFailure{Reason: reason, Count: failures[reason]})
	}
	return out
}
