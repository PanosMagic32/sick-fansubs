package handler

import (
	"context"
	"net/http"

	"sick-fansubs/internal/logging"
)

// The shared keyset collection mechanism: one envelope and one page builder,
// which every keyset endpoint answers through. The rules and the recorded
// exceptions live in docs/patterns/go/collections.md.

// pageInfo is the continuation metadata every keyset collection shares;
// endCursor is null on the final page and total is the filtered row count at
// read time (a display value, not a consistency guarantee).
type pageInfo struct {
	HasNextPage bool    `json:"hasNextPage"`
	EndCursor   *string `json:"endCursor"`
	Total       int     `json:"total"`
}

// collection is the shared keyset envelope: one page of items plus its
// continuation metadata.
type collection[T any] struct {
	Items    []T      `json:"items"`
	PageInfo pageInfo `json:"pageInfo"`
}

// keysetPage is one collection endpoint's contract for [writeKeysetPage]:
// the store read, the wire projection of its rows, and the mint of the
// continuation cursor from the last row actually returned.
type keysetPage[Row, Item any] struct {
	// read returns one page of rows and whether another page exists, having
	// captured the parsed page inputs (limit, cursor, filters); a true hasNext
	// comes with at least one row — the worker mints the cursor from the last.
	read func(ctx context.Context) ([]Row, bool, error)
	// count returns the filtered row total the page's walk covers; it runs
	// after a successful read, under the same filters and scope.
	count func(ctx context.Context) (int, error)
	// item projects one row for the wire. The worker builds the items slice,
	// so every endpoint answers [] and never null.
	item func(row Row) Item
	// next mints the opaque continuation cursor of the page's last row.
	next func(last Row) string
}

// writeKeysetPage serves one keyset collection page: it runs the read and its
// matching count, mints the continuation cursor from the last returned row,
// and writes the {"items", "pageInfo"} envelope.
func writeKeysetPage[Row, Item any](w http.ResponseWriter, r *http.Request, op string, page keysetPage[Row, Item]) {
	if page.count == nil {
		// Defensive: a caller that forgets its count is a wiring error, not a
		// request outcome — the same masked 500 every unmapped failure gets.
		writeInternalError(w, r, logging.From(r.Context()), "collection count not wired")
		return
	}
	rows, hasNext, err := page.read(r.Context())
	if err != nil {
		writeStoreError(w, r, err, op, "error", err)
		return
	}
	total, err := page.count(r.Context())
	if err != nil {
		writeStoreError(w, r, err, op, "error", err)
		return
	}

	// The cursor continues from the last row actually returned — the
	// limit+1th row only proved hasNextPage and stays invisible.
	var endCursor *string
	if hasNext {
		c := page.next(rows[len(rows)-1])
		endCursor = &c
	}

	items := make([]Item, 0, len(rows))
	for _, row := range rows {
		items = append(items, page.item(row))
	}
	writeJSON(w, r, http.StatusOK, collection[Item]{
		Items:    items,
		PageInfo: pageInfo{HasNextPage: hasNext, EndCursor: endCursor, Total: total},
	})
}
