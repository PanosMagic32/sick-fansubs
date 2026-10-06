# Collections

## Purpose

One mechanism for the keyset collection endpoints: one envelope, one page builder, and one parser for the
shared query contract. Rules marked _house rule_ are this repository's own convention; the code lives in
`internal/handler/collection.go` and `internal/handler/list_params.go`.

## Rules

1. **Every keyset collection answers through `writeKeysetPage`.** `internal/handler/collection.go` is the
   only file that declares or builds the envelope (`collection[T]`), the continuation shape (`pageInfo`),
   or their wire keys; the `internal/codestyle` guard `TestCollectionEnvelopeConfined` fails any other
   non-test Go file that does — by field name, by JSON tag, as an embedded field, as a map key, or as a
   composite literal. A test file may decode the wire shape. _House rule._
2. **The envelope is items plus pageInfo.** `items` is always an array — `[]` on an empty
   page, never null — `endCursor` is null on the final page, and `total` is the filtered row count
   at read time (the pager's "of N" number — a display value, never a consistency guarantee
   against concurrent writes).
3. **`hasNextPage`/`endCursor` derive from one read result, and `total` from its matching count.**
   The read reports whether another page exists; the worker mints the cursor from the last returned
   row exactly when it does. A count must state the same filters and scope as its read: the bigger
   predicates are built once and shared by both closures (`publishedFromWhere`, `favoritesFromWhere`,
   `searchBranches` + `searchWindowConditions`, `notificationsFrom`, `staffListWhere`,
   `staffUsersFromWhere`), and a one-line predicate is written literally beside its read (the
   comment counts, the session window). The counts run after a successful read, so they do not
   re-apply the read's existence gates (the comments' published/parent checks) — a count that drops
   a FILTER is a wrong number, not a broken page, and the count tests pin the difference.
4. **The cursor continues from the last row actually returned.** The limit+1th row only proved
   `hasNextPage` and stays invisible.
5. **One parser reads the shared list contract.** `parseKeysetParams` parses the allowlist, the limit
   bound (per-endpoint default and maximum), and the endpoint's opaque `after` cursor. An endpoint's own
   parameters extend the allowlist through `extra` and are validated from the returned query after it.
6. **Two compositions are documented exceptions.** The search and comments-list parsers parse the sort
   before the `after` decode, because the active sort selects the cursor codec; they compose
   `strictQueryParams` and `parseLimitParam` directly and decode their own cursor.
7. **A cursor namespace binds its codec to its endpoint.** Decode checks the exact prefix, so a cursor
   never crosses endpoints; the namespaces and codecs live in `internal/handler/cursor.go`.
8. **The items-only tails stay explicit.** `staffLogs` and `staffAuditEvents` are bounded tail reads over
   the log sink and the audit ledger, not keyset walks: they keep their own `{items}` bodies and take no
   `after`, no `pageInfo`, and no total.
9. **A read failure maps through the store vocabulary.** `writeKeysetPage` maps every read error through
   `writeStoreError`: the comment reads' absent parent is the masked 404, and everything unmapped is the
   masked 500, logged once at that boundary ([errors.md](errors.md) rule 12).

## Pattern

```go
// internal/handler/collection.go — the envelope, and the one page builder.
type collection[T any] struct {
	Items    []T      `json:"items"`
	PageInfo pageInfo `json:"pageInfo"`
}

func writeKeysetPage[Row, Item any](w http.ResponseWriter, r *http.Request, op string, page keysetPage[Row, Item]) {
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
	var endCursor *string
	if hasNext {
		c := page.next(rows[len(rows)-1])
		endCursor = &c
	}
	items := make([]Item, 0, len(rows))
	for _, row := range rows {
		items = append(items, page.item(row))
	}
	writeJSON(w, r, http.StatusOK, collection[Item]{Items: items, PageInfo: pageInfo{hasNext, endCursor, total}})
}

// internal/handler/blog.go — a caller parses, then hands over one keysetPage.
_, limit, after, ok := parseKeysetParams(w, r, blogListDefaultLimit, blogListMaxLimit, blogCursorPrefix)
if !ok {
	return
}
writeKeysetPage(w, r, "blog list query", keysetPage[store.BlogPostSummary, BlogPostItem]{
	read: func(ctx context.Context) ([]store.BlogPostSummary, bool, error) {
		var key *store.PageKey
		if after != nil {
			key = &store.PageKey{PublishedAtMS: after.PublishedAtMS, ID: after.ID}
		}
		return store.ListPublishedBlogPosts(ctx, db, limit, key)
	},
	count: func(ctx context.Context) (int, error) {
		return store.CountPublishedContent(ctx, db, store.BlogContent)
	},
	item: func(p store.BlogPostSummary) BlogPostItem { /* one row for the wire */ },
	next: func(last store.BlogPostSummary) string {
		return encodeCursor(blogCursorPrefix, last.PublishedAtMS, last.ID)
	},
})
```

## Examples

- `internal/handler/collection.go` — `pageInfo`, `collection[T]`, `keysetPage`, `writeKeysetPage`.
- `internal/handler/list_params.go` — `parseKeysetParams` beside the composition pieces.
- `internal/handler/cursor.go` — the cursor namespaces and the three codecs.
- `internal/handler/comments.go` and `internal/handler/search.go` — the two documented parser
  compositions.
- `internal/handler/staff_logs.go` and `internal/handler/staff_audit_events.go` — the items-only tails.
- `internal/codestyle/rules_test.go` — `TestCollectionEnvelopeConfined`, with the real declaration as its
  positive control.

## Gotchas

- **The read closure captures the parsed page inputs.** The worker never sees the cursor type, which is
  what lets the plain, sort-bound, and text-key codecs share one builder. A new cursor shape changes
  `cursor.go`, its endpoint, and OpenAPI together — never the worker.
- **`item` projects one row.** The worker builds the items slice, so an endpoint cannot answer `null`
  items by accident.
- **A parser exception is a recorded difference, not a license.** A new endpoint whose cursor needs
  another parameter moves that parameter before the decode in its own parser and records it here.
- **Do not stretch the envelope over the tails.** A tail read has no cursor and no honest `hasMore`;
  wrapping it in `pageInfo` would promise a walk the surface does not support.
- **The count is a second query, not a second read path.** It runs once per collection request, after
  a successful read; anything more expensive (a cached counter, a materialized total) is a
  different mechanism and needs its own recorded ruling.

## Pointers

- Index: [../../README.md](../../README.md)
- Error vocabularies and boundary mapping: [errors.md](errors.md)
- Type parameters and function values: [generics.md](generics.md)
- The cursor codec, the namespaces, and the wire contract: [`internal/handler/AGENTS.md`](../../../internal/handler/AGENTS.md)
- Mechanical guards for the checkable subset of these rules: [`internal/codestyle/AGENTS.md`](../../../internal/codestyle/AGENTS.md)
