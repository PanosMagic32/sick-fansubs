package handler

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"sick-fansubs/internal/store"
)

// Shared strict query-parameter plumbing for the keyset collection
// endpoints. parseKeysetParams is the one parser of the shared contract —
// the allowlist, the limit bound, and the endpoint's opaque "after" cursor.
// An endpoint whose cursor codec depends on another parameter, or whose
// parse order differs, composes the same pieces directly; the allowed
// differences are recorded in docs/patterns/go/collections.md.

// strictQueryParams parses the raw query string strictly and allowlists the
// given parameter names, each appearing at most once.
//
// r.URL.Query() would silently discard pairs url.ParseQuery rejects
// (semicolon separators, malformed percent-escapes) and serve the default
// page. A malformed query string is structurally invalid input — 400.
// Unknown names and duplicates are rejected the same
// way: the query contract is an allowlist, not a filter.
func strictQueryParams(w http.ResponseWriter, r *http.Request, known ...string) (url.Values, bool) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeBadRequest(w, r, "malformed query string")
		return nil, false
	}
	for name, values := range query {
		if !slices.Contains(known, name) {
			writeBadRequest(w, r, "unknown query parameter: "+name)
			return nil, false
		}
		if len(values) != 1 {
			writeBadRequest(w, r, "duplicate query parameter: "+name)
			return nil, false
		}
	}
	return query, true
}

// parseLimitParam reads the optional "limit" parameter under the shared
// contract: an integer in 1..max, absent means def. Non-integer/empty →
// 400; an integer outside the range is semantically invalid → 422 with
// violation field "limit", code "outOfRange". On failure it writes the
// response and returns (0, false).
func parseLimitParam(w http.ResponseWriter, r *http.Request, query url.Values, def, max int) (int, bool) {
	raw, ok := query["limit"]
	if !ok {
		return def, true
	}
	n, err := strconv.ParseInt(raw[0], 10, 64)
	if err != nil {
		// ErrRange is a syntactically valid integer outside int64 — the
		// contract treats an integer outside 1..max as 422, not 400.
		if numErr, ok := errors.AsType[*strconv.NumError](err); ok && errors.Is(numErr.Err, strconv.ErrRange) {
			writeValidationErrors(w, r, []Violation{{Field: "limit", Code: "outOfRange"}})
			return 0, false
		}
		writeBadRequest(w, r, "limit is not an integer")
		return 0, false
	}
	if n < 1 || n > int64(max) {
		writeValidationErrors(w, r, []Violation{{Field: "limit", Code: "outOfRange"}})
		return 0, false
	}
	return int(n), true
}

// parseKeysetParams parses the shared keyset list contract: the allowlist
// (extra names included — their values read back from the returned query), the
// limit bound, and the opaque "after" cursor; ok=false wrote an error response.
func parseKeysetParams(w http.ResponseWriter, r *http.Request, def, max int, prefix string, extra ...string) (url.Values, int, *cursor, bool) {
	query, ok := strictQueryParams(w, r, append([]string{"limit", "after"}, extra...)...)
	if !ok {
		return nil, 0, nil, false
	}
	limit, ok := parseLimitParam(w, r, query, def, max)
	if !ok {
		return nil, 0, nil, false
	}

	raw, present := query["after"]
	if !present {
		return query, limit, nil, true
	}
	// Anything that fails strict decode — including a cursor from a different
	// endpoint — is a 400: the cursor is one opaque token and the reason is
	// never distinguished.
	after, err := decodeCursor(raw[0], prefix)
	if err != nil {
		writeBadRequest(w, r, "invalid after cursor")
		return nil, 0, nil, false
	}
	return query, limit, &after, true
}

// staffContentFilterMaxLen bounds the staff content filter's text query in
// runes — the public search's bound (searchMaxQueryLen), because both filter
// a title with the same fold and an over-long query can only be garbage.
const staffContentFilterMaxLen = searchMaxQueryLen

// staffContentStatuses is the status filter's vocabulary:
// every status a content row can hold. It is deliberately NOT the create
// form's allowlist — a create may only land as draft|published, while the
// filtering staff member may look for archived rows too.
var staffContentStatuses = []string{"draft", "published", "archived"}

// parseStaffContentFilter reads the staff content list's two optional filters:
// "status" (exact) and "q" (matched as words against the
// folded title — every word must appear). Both are optional and an
// empty value means "no filter" (the staff user list's role/status precedent,
// which treats a blank filter as absent rather than as a 422).
//
// A non-empty status outside the content vocabulary is semantically invalid →
// 422 {status, invalidValue}; a non-empty q longer than the bound is → 422
// {q, maxLength}. The store splits and folds the query, so this layer only
// trims and bounds it — one fold rule in the tree. A q with no letters and no
// digits is passed through as-is: the store reads it as no filter, and the
// public search's 422 {q, invalidFormat} for the same input is deliberately
// not mirrored here.
func parseStaffContentFilter(w http.ResponseWriter, r *http.Request, query url.Values) (store.StaffContentFilter, bool) {
	filter := store.StaffContentFilter{Status: query.Get("status")}
	if filter.Status != "" && !slices.Contains(staffContentStatuses, filter.Status) {
		writeValidationErrors(w, r, []Violation{{Field: "status", Code: "invalidValue"}})
		return store.StaffContentFilter{}, false
	}
	filter.Query = strings.TrimSpace(query.Get("q"))
	if utf8.RuneCountInString(filter.Query) > staffContentFilterMaxLen {
		writeValidationErrors(w, r, []Violation{{Field: "q", Code: "maxLength"}})
		return store.StaffContentFilter{}, false
	}
	return filter, true
}
