package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Media reference rules (docs/patterns/go/content-kinds.md): a media file is
// KEPT while its storage-relative path appears in at least one active
// content row or one user avatar. The reference set spans ALL rows of EVERY
// content kind — identical source bytes may be stored once and referenced by
// several posts, so one file can serve several rows, and deleting on a single
// row's lifecycle would break the others.

// ReferencedMediaPaths returns the set of canonical storage paths any active
// content row or user avatar references; the media orphan sweep treats
// membership as KEEP. canonical maps each raw stored reference to the layout's
// canonical path (`media/images/<id[:2]>/<id>.<ext>`); references it rejects
// (a path that does not name the media tree, a malformed segment) cannot name
// a local file and are skipped. An absolute http(s) reference protects the
// path it names regardless of host or path prefix. The store stays URL-free:
// the caller supplies the layout grammar.
func ReferencedMediaPaths(ctx context.Context, db *sql.DB, canonical func(string) (string, bool)) (map[string]struct{}, error) {
	// The content legs sweep the descriptor list, so a new content kind
	// joins the reference set without touching this function.
	terms := make([]string, 0, len(allContentKinds)+1)
	for _, k := range allContentKinds {
		terms = append(terms, `SELECT thumbnail_url FROM `+k.table)
	}
	terms = append(terms, `SELECT avatar_url FROM users WHERE avatar_url IS NOT NULL`)

	rows, err := db.QueryContext(ctx, strings.Join(terms, ` UNION `))
	if err != nil {
		return nil, fmt.Errorf("store: referenced media paths query: %w", err)
	}
	defer rows.Close()

	set := make(map[string]struct{})
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("store: scan referenced media path: %w", err)
		}
		if canonicalPath, ok := canonical(path); ok {
			set[canonicalPath] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate referenced media paths: %w", err)
	}
	return set, nil
}

// MediaPathReferenced reports whether any active content row or user avatar
// references the canonical storage path — the explicit-delete guard and the
// sweep's pre-removal recheck: a referenced file is never removed. The raw
// columns are canonicalized through the caller's layout grammar, so a legacy
// flat, wrong-subdirectory, or same-origin absolute reference still protects
// the canonical file.
func MediaPathReferenced(ctx context.Context, db *sql.DB, path string, canonical func(string) (string, bool)) (bool, error) {
	set, err := ReferencedMediaPaths(ctx, db, canonical)
	if err != nil {
		return false, err
	}
	_, ok := set[path]
	return ok, nil
}
