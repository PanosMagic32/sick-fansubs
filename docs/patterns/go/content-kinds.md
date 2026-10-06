# Content kinds

## Purpose

One mechanism for the store's per-content-type differences: each content type — a blog post and a
project — is described once by a `ContentKind` value, and every reader takes the descriptor instead of
a second copy of the SQL. The code lives in `internal/store/content_kinds.go` (the descriptors, the
shared projections, the user-ref and download helpers) and `internal/store/staff_content.go` (the staff
reads).

## Rules

1. **One descriptor per content kind.** The store's SQL names a content table, a comment/heart/
   favorite/download table, or a search index only through a `ContentKind` value. `TestContentKindTablesConfined`
   (`internal/codestyle`) fails any other non-test store file whose string literals carry the thirteen known
   names — the twelve table names plus the `blog-posts` kind value — case-insensitively, whole-word,
   because SQLite identifiers are case-insensitive. Prose comments,
   a name assembled from parts, and test fixtures are outside the check; the pattern doc and the review
   carry those.
2. **Readers take the descriptor; writers stay per kind.** Every read path — public list/detail, staff
   list/detail, comments, favorites, follows, the search branches, the metrics totals, the media
   reference set, the notification feed's title joins — consumes a `ContentKind`. The
   create/update/delete transactions stay per kind: their contracts (the slug backstop, the
   subtitle/slug slots, the per-kind error strings) genuinely differ.
3. **The descriptor owns names, not behavior.** It carries the storage names, the two error-text words
   (`label`, `noun`), the kind-specific names (`ownColumn`, and `hasSubtitle` marking that column as
   the display subtitle), and the wire/storage discriminator (`kind`). Values are package constants —
   never request input — so interpolating them into query strings carries no injection risk; every row
   value stays a bound parameter.
4. **A typed projection is per kind; the worker is generic.** `contentRead[T]` binds one kind to one
   projection (descriptor, SELECT columns, row scan), and the workers hold each SQL shape once. The
   exported functions name their row type (`GetStaffBlogPost`, `ListPublishedProjects`, …), the
   [generics.md](generics.md) pattern: one body, several concrete entry points.
5. **`allContentKinds` is the sweep order.** A reader that must cover every kind (the media reference
   set, the metrics cross-kind sums, the search-index clear, the notification feed's title joins) loops
   over it, so a new kind joins automatically. `TestContentKindDescriptors` pins the two values: every
   field populated, no name shared between the kinds.
6. **The user-ref tail is one scan call.** A content projection ends with the creator's
   `id, username, avatar` columns, the detail reads an updater triple after it; `userRefTail` collects
   those destinations, and its `ref()` answers nil for a NULL id. `database/sql` requires one `Scan`
   call for the whole row, so the tail's destinations join the row's in that call.
7. **Downloads are one shared row type.** `Download{Label, MagnetLink, TorrentLink}` is the store
   shape; the descriptor's `downloadLabel` names the kind's column (`resolution`, `name`), and the wire
   DTOs keep their per-kind field names. The reader and the create/update writers are shared; the label
   semantics stay per kind.
8. **The wire names are the handler's.** The handler keeps its own URL segment, cursor namespaces, and
   log label; the segment and the storage discriminator are the same value, and the handler reads it
   from the descriptor (`store.BlogContent.Kind()`). The route table still spells its patterns with the
   literal segment — the two are pinned together by the comment tests' expected URLs, not by the guard.
9. **A content kind never arrives as a runtime string.** The notification emitters and every reader take
   the descriptor, so no string→table lookup exists to fail; the two declared values, their fields pinned
   by `TestContentKindDescriptors`, are the only ones the package uses.

## Pattern

```go
// internal/store/content_kinds.go — the descriptor and its shared readers.
type ContentKind struct {
	kind          string // "blog-posts" | "projects" — the wire and storage value
	label, noun   string // error text: "blog"/"blog post", "project"/"project"
	ownColumn     string // the one column only this kind has: "subtitle" | "slug"
	hasSubtitle   bool   // ownColumn is the display subtitle (blog posts)

	table         string // content rows
	comments      string // comment rows
	commentFK     string // comment rows' content column
	hearts        string // comment heart rows
	favorites     string // favorite rows
	favoriteFK    string // favorite rows' content column
	downloads     string // download rows
	downloadFK    string // download rows' content column
	downloadLabel string // download rows' display-label column
	searchIndex   string // the FTS5 index table
}

var (
	BlogContent    = ContentKind{ /* ... */ }
	ProjectContent = ContentKind{ /* ... */ }

	// allContentKinds is the canonical order of the sweeps that must cover
	// every kind.
	allContentKinds = []ContentKind{BlogContent, ProjectContent}
)

// internal/store/comment_store.go — a shared reader takes the descriptor.
func ListComments(ctx context.Context, db *sql.DB, k ContentKind, contentID string, viewerID *string, ...) ([]Comment, bool, error)

// internal/handler/comments.go — the handler bundle carries the value.
type commentKind struct {
	label, segment, prefix, replyPrefix string
	content store.ContentKind
}
```

## Examples

- `internal/store/content_kinds.go` — the descriptors, `UserRef`, `Download`, the projections, the
  generic workers.
- `internal/store/staff_content.go` — the staff reads and the `AdminPageKey`/`StaffContentFilter`/
  `writeTitleFilter` trio.
- `internal/store/comment_store.go`, `favorite_store.go`, `follow_store.go` — the exported shared
  readers behind the handler bundles.
- `internal/handler/comments.go`, `favorites.go`, `follows.go` — the handler bundles carry the
  descriptor; the store's per-kind wrapper functions are gone.
- `internal/codestyle/rules_test.go` — `TestContentKindTablesConfined`, with the real descriptor file as
  its positive control.

## Gotchas

- **A tail scan cannot call `Scan` again.** Collect every destination in the single call
  (`userRefTail`); a second `Scan` on the same row fails on the argument count.
- **The descriptor's `noun` pluralizes with a plain `s`.** Error text is built as
  `"store: list staff %ss"`; a kind whose noun does not pluralize that way needs its own text, not a new
  field.
- **Splicing is for names only.** The tables and `kind` are package constants; the collation and the
  `sf_fold` function name come from `internal/search`. Any value from a row or a request is bound.
- **A new kind touches more than the descriptor.** Add the value, join `allContentKinds`, then wire the
  write transactions (their named call sites) and any handler bundle. The guard catches only literals; a
  name assembled from parts is a review matter.
- **`Download.Label` is a store name, not a wire name.** The handler maps it to `resolution` or `name`,
  and the public read still applies `safeDownloadLink`.

## Pointers

- Index: [../../README.md](../../README.md)
- The type-parameter pattern: [generics.md](generics.md)
- The store's scope doc: [`internal/store/AGENTS.md`](../../../internal/store/AGENTS.md)
- The handler's bundles: [`internal/handler/AGENTS.md`](../../../internal/handler/AGENTS.md)
- Mechanical guards for the checkable subset of these rules:
  [`internal/codestyle/AGENTS.md`](../../../internal/codestyle/AGENTS.md)
