package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Media reference rules (docs/patterns/go/content-kinds.md): a media file is
// KEPT while its storage-relative path appears in at least one active
// content row or one user avatar. The reference set spans ALL rows of EVERY
// content kind — the media import deduplicates identical source bytes across
// posts, so one file can serve several rows, and deleting on a single row's
// lifecycle would break the others.

// ReferencedMediaPaths returns the set of storage-relative media paths any
// active content row or user avatar references; the media orphan sweep treats
// membership as KEEP. Legacy absolute URLs are harmless set members.
func ReferencedMediaPaths(ctx context.Context, db *sql.DB) (map[string]struct{}, error) {
	// The content legs sweep the descriptor list, so a new content kind
	// joins the reference set without touching this function.
	terms := make([]string, 0, len(allContentKinds)+1)
	for _, k := range allContentKinds {
		terms = append(terms, `SELECT thumbnail_url FROM `+k.table)
	}
	terms = append(terms, `SELECT avatar_url FROM users WHERE avatar_url IS NOT NULL`)

	rows, err := db.QueryContext(ctx, strings.Join(terms, ` UNION `))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	set := make(map[string]struct{})
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		set[path] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return set, nil
}

// MediaPathReferenced reports whether any active content row or user avatar
// references the given storage-relative media path — the explicit-delete
// guard: a referenced file is never removed.
func MediaPathReferenced(ctx context.Context, db *sql.DB, path string) (bool, error) {
	queries := make([]string, 0, len(allContentKinds)+1)
	for _, k := range allContentKinds {
		queries = append(queries, `SELECT 1 FROM `+k.table+` WHERE thumbnail_url = ? LIMIT 1`)
	}
	queries = append(queries, `SELECT 1 FROM users WHERE avatar_url = ? LIMIT 1`)

	for _, q := range queries {
		var one int
		err := db.QueryRowContext(ctx, q, path).Scan(&one)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, sql.ErrNoRows):
			continue
		default:
			return false, err
		}
	}
	return false, nil
}
