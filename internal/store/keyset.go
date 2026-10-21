package store

import (
	"database/sql"
	"fmt"
)

// PageKey is the keyset continuation tuple shared by every list endpoint:
// (published_at_ms, id). The accepted ordering is
// published_at_ms DESC, id ASC, so "strictly after" means an earlier
// publish time, or the same publish time with a larger id (the opaque
// public ID is the final tie-breaker).
//
// One shape serves every collection: the endpoints share the ordering and the
// tuple rather than keeping per-endpoint copies that could diverge. Search's
// tuple is SearchKeyset — the title sort needs a third, text-keyed slot.
type PageKey struct {
	PublishedAtMS int64
	ID            string
}

// scanKeysetPage reads one keyset page from an open cursor. The query must
// have fetched limit+1 rows: the extra row is the continuation signal and is
// dropped from the page. scan reads exactly the selected columns. label names
// one row in the error ("favorite item"), so the caller keeps its query
// context without repeating the loop.
func scanKeysetPage[T any](rows *sql.Rows, limit int, label string, scan func(*sql.Rows) (T, error)) ([]T, bool, error) {
	var items []T
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, false, fmt.Errorf("store: scan %s: %w", label, err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("store: iterate %s: %w", label, err)
	}

	hasNext := len(items) > limit
	if hasNext {
		items = items[:limit]
	}
	return items, hasNext, nil
}
