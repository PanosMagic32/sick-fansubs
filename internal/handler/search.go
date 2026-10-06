package handler

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/search"
	"sick-fansubs/internal/store"
)

// Search wire contract: GET /api/v1/search.
//
// Public merged search over published blog posts and projects — no
// authentication, no CSRF/origin checks (which protect unsafe methods), and
// no rate limit (public reads are deliberately unthrottled —
// internal/handler/AGENTS.md). The route chain is RequestID only.
//
// The precise contract lives in docs/api/openapi.yaml (operationId
// searchContent); the pagination envelope and bounds follow the shared keyset
// contract in docs/patterns/go/collections.md, internal/search owns the query
// sanitization, and the store owns the merged ordering and public filter.

// SearchItem is the public projection of one merged search hit (field rules:
// internal/handler/AGENTS.md). ThumbnailURL is a pointer — a stored value
// that fails the scheme guard emits null, not an empty string.
type SearchItem struct {
	Type          string  `json:"type"`
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	Subtitle      string  `json:"subtitle"`
	Description   string  `json:"description"`
	ThumbnailURL  *string `json:"thumbnailUrl"`
	PublishedAt   string  `json:"publishedAt"`
	CommentCount  int64   `json:"commentCount"`
	FavoriteCount int64   `json:"favoriteCount"`
}

// Search-list pagination and query bounds.
const (
	searchDefaultLimit = 20
	searchMaxLimit     = 100
	searchMaxQueryLen  = 100 // runes, after trimming
)

// Search returns the public search handler. publicBaseURL is the configured
// canonical public origin used to emit absolute thumbnail URLs (see
// mediaURL for the scheme-masking rule).
func Search(db *sql.DB, publicBaseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// parseSearchParams translates the query string straight into the
		// store's request shape: the wire shape IS the parameter list here, so
		// a second mirror struct would only be able to drift from it.
		params, ok := parseSearchParams(w, r)
		if !ok {
			return
		}

		writeKeysetPage(w, r, "search query", keysetPage[store.SearchResult, SearchItem]{
			count: func(ctx context.Context) (int, error) {
				return store.CountSearchContent(ctx, db, params)
			},
			read: func(ctx context.Context) ([]store.SearchResult, bool, error) {
				return store.SearchContent(ctx, db, params)
			},
			item: func(res store.SearchResult) SearchItem {
				return SearchItem{
					Type:          res.Type,
					ID:            res.ID,
					Title:         res.Title,
					Subtitle:      res.Subtitle,
					Description:   res.Description,
					ThumbnailURL:  thumbnailURLOrNull(publicBaseURL, res.ThumbnailURL),
					PublishedAt:   formatAPITime(res.PublishedAtMS),
					CommentCount:  res.CommentCount,
					FavoriteCount: res.FavoriteCount,
				}
			},
			next: func(last store.SearchResult) string {
				// The active sort selects the namespace, so a cursor decodes only
				// under the sort that minted it.
				if params.Sort == store.SearchByTitle {
					return encodeTitleCursor(searchTitleCursorPrefix, search.SortKey(last.Title), last.ID)
				}
				if params.Sort == store.SearchByOldest {
					return encodeCursor(searchOldestCursorPrefix, last.PublishedAtMS, last.ID)
				}
				return encodeCursor(searchCursorPrefix, last.PublishedAtMS, last.ID)
			},
		})
	}
}

// parseSearchParams applies the strict query-parameter contract:
//
//   - only "q", "type", "sort", "from", "to", "limit", and "after" are
//     known; duplicates and unknown names → 400
//   - q: required. Trimmed, 1..100 runes — empty/missing → 422 {q,
//     required}; over 100 runes → 422 {q, maxLength}; sanitized down to no
//     tokens (only specials/reserved words) → 422 {q, invalidFormat}
//   - type: all | posts | projects, absent means all; any other value →
//     422 {type, invalidValue}
//   - sort: date | oldest | title, absent means date; any other value → 422
//     {sort, invalidValue}
//   - from / to: optional publication-date bounds as EXACTLY YYYY-MM-DD in
//     UTC. A value that is not canonical, or a day the calendar rejects
//     (2025-02-30), is 422 {field, invalidFormat}; a window whose end day
//     precedes its start day is 422 {to, invalidValue}. Both are converted
//     to millisecond bounds here, so the store never sees a date string.
//   - limit: integer in 1..100, absent means 20. Non-integer/empty → 400;
//     an integer outside the range is semantically invalid → 422 with
//     violation field "limit", code "outOfRange"
//   - after: one opaque cursor in the namespace the active sort owns;
//     anything that fails strict decode → 400
//
// It returns (query, false) after writing an error response. Match is the
// built FTS5 MATCH argument — user text never reaches the SQL layer as
// syntax. The shared limit plumbing lives in list_params.go.
func parseSearchParams(w http.ResponseWriter, r *http.Request) (store.SearchQuery, bool) {
	logger := logging.From(r.Context())
	query, ok := strictQueryParams(w, r, "q", "type", "sort", "from", "to", "limit", "after")
	if !ok {
		return store.SearchQuery{}, false
	}

	rawQ, ok := query["q"]
	if !ok {
		writeValidationErrors(w, r, []Violation{{Field: "q", Code: "required"}})
		return store.SearchQuery{}, false
	}
	trimmed := strings.TrimSpace(rawQ[0])
	if trimmed == "" {
		writeValidationErrors(w, r, []Violation{{Field: "q", Code: "required"}})
		return store.SearchQuery{}, false
	}
	if utf8.RuneCountInString(trimmed) > searchMaxQueryLen {
		writeValidationErrors(w, r, []Violation{{Field: "q", Code: "maxLength"}})
		return store.SearchQuery{}, false
	}
	match, err := search.BuildQuery(trimmed)
	if err != nil {
		if errors.Is(err, search.ErrNoTokens) {
			writeValidationErrors(w, r, []Violation{{Field: "q", Code: "invalidFormat"}})
			return store.SearchQuery{}, false
		}
		// Unreachable for the current BuildQuery implementation, but a
		// future sanitizer failure must not leak: treat it as server-side.
		writeInternalError(w, r, logger, "search query construction failed", "error", err)
		return store.SearchQuery{}, false
	}

	scope := store.SearchAll
	if raw, ok := query["type"]; ok {
		switch raw[0] {
		case "all":
			scope = store.SearchAll
		case "posts":
			scope = store.SearchPosts
		case "projects":
			scope = store.SearchProjects
		default:
			writeValidationErrors(w, r, []Violation{{Field: "type", Code: "invalidValue"}})
			return store.SearchQuery{}, false
		}
	}

	sort := store.SearchByDate
	if raw, ok := query["sort"]; ok {
		switch raw[0] {
		case "date":
			sort = store.SearchByDate
		case "oldest":
			sort = store.SearchByOldest
		case "title":
			sort = store.SearchByTitle
		default:
			writeValidationErrors(w, r, []Violation{{Field: "sort", Code: "invalidValue"}})
			return store.SearchQuery{}, false
		}
	}

	limit, ok := parseLimitParam(w, r, query, searchDefaultLimit, searchMaxLimit)
	if !ok {
		return store.SearchQuery{}, false
	}

	sinceMS, ok := parseSearchDateParam(w, r, query, "from")
	if !ok {
		return store.SearchQuery{}, false
	}
	untilMS, ok := parseSearchDateParam(w, r, query, "to")
	if !ok {
		return store.SearchQuery{}, false
	}
	// An inclusive end date becomes the next day's start (exclusive bound).
	// The bounds are POINTERS: nil means absent, so the epoch day (whose
	// millisecond value is 0) is a real bound, not "unbounded".
	if untilMS != nil {
		next := time.UnixMilli(*untilMS).UTC().AddDate(0, 0, 1).UnixMilli()
		untilMS = &next
	}
	if sinceMS != nil && untilMS != nil && *sinceMS >= *untilMS {
		writeValidationErrors(w, r, []Violation{{Field: "to", Code: "invalidValue"}})
		return store.SearchQuery{}, false
	}

	keyset, ok := parseSearchAfterParam(w, r, query, sort)
	if !ok {
		return store.SearchQuery{}, false
	}
	return store.SearchQuery{
		Match:   match,
		Scope:   scope,
		Sort:    sort,
		SinceMS: sinceMS,
		UntilMS: untilMS,
		Limit:   limit,
		After:   keyset,
	}, true
}

// searchDateLayout is the only accepted window format: a canonical UTC
// calendar day.
const searchDateLayout = "2006-01-02"

// parseSearchDateParam parses one optional from/to bound. It returns the
// day's UTC start in milliseconds, or nil when the parameter is absent — a
// pointer, not a zero sentinel, because 1970-01-01 is a legal bound whose
// value IS zero.
//
// The value is untrusted input: it must round-trip through the layout (that
// rejects an alternative spelling or trailing text the parser might
// otherwise tolerate), the calendar must accept the day (2025-02-30 fails),
// and the year is bounded to 1970-9999 so the millisecond value is positive
// and comparable with published_at_ms. A failure is 422 {field,
// invalidFormat} — the store is never reached, so a malformed date can never
// touch SQL.
func parseSearchDateParam(w http.ResponseWriter, r *http.Request, query url.Values, field string) (*int64, bool) {
	raw, ok := query[field]
	if !ok {
		return nil, true
	}
	value := raw[0]
	t, err := time.Parse(searchDateLayout, value)
	if err != nil || t.Format(searchDateLayout) != value {
		writeValidationErrors(w, r, []Violation{{Field: field, Code: "invalidFormat"}})
		return nil, false
	}
	if year := t.Year(); year < 1970 || year > 9999 {
		writeValidationErrors(w, r, []Violation{{Field: field, Code: "invalidFormat"}})
		return nil, false
	}
	ms := t.UnixMilli()
	return &ms, true
}

// parseSearchAfterParam decodes "after" in the namespace the active sort
// owns (sv1. newest-first date, so1. oldest-first date, st1. title); a
// cursor minted under one sort is 400 under any other (sort binding).
func parseSearchAfterParam(w http.ResponseWriter, r *http.Request, query url.Values, sort store.SearchSort) (*store.SearchKeyset, bool) {
	raw, ok := query["after"]
	if !ok {
		return nil, true
	}

	if sort == store.SearchByTitle {
		c, err := decodeTitleCursor(raw[0], searchTitleCursorPrefix)
		if err != nil {
			writeBadRequest(w, r, "invalid after cursor")
			return nil, false
		}
		return &store.SearchKeyset{TitleKey: c.Key, ID: c.ID}, true
	}

	prefix := searchCursorPrefix
	if sort == store.SearchByOldest {
		prefix = searchOldestCursorPrefix
	}
	c, err := decodeCursor(raw[0], prefix)
	if err != nil {
		writeBadRequest(w, r, "invalid after cursor")
		return nil, false
	}
	return &store.SearchKeyset{PublishedAtMS: c.PublishedAtMS, ID: c.ID}, true
}
