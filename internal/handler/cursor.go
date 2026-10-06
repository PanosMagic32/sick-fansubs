package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"

	"sick-fansubs/internal/search"
)

// Keyset-cursor contract (the per-endpoint wire contract
// is documented in docs/api/openapi.yaml):
//
//   - Format: "<prefix>" + base64url of {"v":1,"p":<publishedAtMs>,"i":<id>}
//   - Continuation tuple: (published_at_ms, id) — the last item of the
//     page, matching the accepted ordering published_at_ms DESC, id ASC
//   - Bound: at most cursorMaxLen characters
//   - Opaque to clients and strictly validated on decode: an invalid token
//     is a malformed request, never a way to influence the filter beyond
//     the accepted tuple
//
// The blog, project, and search lists share this one codec for timestamp
// keysets; the namespace prefix is one per-endpoint difference — decode
// checks its endpoint's exact prefix, so a cursor never crosses endpoints,
// and a foreign cursor is rejected as one opaque token without revealing
// which endpoint it belongs to. The comment, reply, and title-sort codecs
// below are siblings with their own payload shapes (see each one's docs).
const (
	cursorVersion = 1
	cursorMaxLen  = 256

	blogCursorPrefix    = "v1."
	projectCursorPrefix = "pv1."
	// Search date-sort namespaces: the newest-first default (sv1.) and the
	// oldest-first sort (so1.) read the same tuple in opposite directions,
	// so each prefix keeps a cursor from silently changing direction.
	searchCursorPrefix       = "sv1."
	searchOldestCursorPrefix = "so1."
	// Favorites namespaces: the codec's timestamp slot
	// carries the favorite's created_at_ms in these namespaces.
	blogFavCursorPrefix    = "fv1."
	projectFavCursorPrefix = "fpv1."
	// Staff blog list namespace: the timestamp slot
	// carries updated_at_ms here — the staff list sorts by last edit, not
	// by publish time.
	blogAdminCursorPrefix = "av1."
	// Staff project list namespace: same semantic as
	// av1. — the timestamp slot carries updated_at_ms.
	projectAdminCursorPrefix = "apv1."
	// Staff user list namespace: the timestamp slot
	// carries the account's created_at_ms — the staff list sorts by
	// account creation, not by edit time.
	staffUserCursorPrefix = "uv1."
	// Notifications feed namespace: the timestamp slot
	// carries the event's created_at_ms — the feed sorts newest event
	// first.
	notificationCursorPrefix = "nv1."
	// Comment list namespaces: SORT-BOUND cursors — the
	// payload carries the sort (a cursor minted under one sort is rejected
	// under another, 400), the timestamp slot carries created_at_ms, and
	// the top sort adds the hearts_count slot.
	blogCommentCursorPrefix    = "bc1."
	projectCommentCursorPrefix = "pc1."
	// Comment reply namespaces: the replies
	// operation's keyset is FIXED (created_at_ms, id ascending), so these
	// reuse the plain name+id codec above with created_at_ms in the
	// timestamp slot — the prefix is the only endpoint difference.
	blogReplyCursorPrefix    = "br1."
	projectReplyCursorPrefix = "pr1."
	// Search title-sort namespace: the keyset is (collation key, id)
	// ascending, so this prefix carries a TEXT key instead of the timestamp
	// slot and binds the cursor to its sort (the sort-binding precedent).
	searchTitleCursorPrefix = "st1."
	// Self-service session list namespace: the timestamp
	// slot carries the session's created_at_ms (ordering created_at_ms DESC,
	// id ASC).
	sessionCursorPrefix = "ss1."
)

// titleCursorMaxLen bounds the title-sort cursor. It is larger than
// cursorMaxLen because the payload carries a text key: at most 200 runes
// (search.SortKeyRuneLimit), which is up to 800 bytes as UTF-8 — roughly
// 1100 characters once base64-encoded and framed as JSON. The bound is
// documented with this contract (an
// implementation-recorded maximum).
const titleCursorMaxLen = 2048

// cursor is the decoded continuation tuple of one list endpoint. It
// deliberately mirrors store.PageKey: the codec owns the WIRE payload and
// the store owns the persistence key (wire and
// storage shapes stay separate even when they currently coincide).
type cursor struct {
	PublishedAtMS int64
	ID            string
}

// cursorPayload is the JSON payload shape shared by encode and decode —
// one named type so the field tags can never drift between the two halves.
type cursorPayload struct {
	V int    `json:"v"`
	P int64  `json:"p"`
	I string `json:"i"`
}

// encodeCursor serializes a continuation tuple into its opaque form under
// the given namespace prefix.
func encodeCursor(prefix string, publishedAtMS int64, id string) string {
	// json.Marshal of this fixed shape cannot fail; ignoring the error here
	// is narrower than threading an impossible failure through every caller.
	raw, _ := json.Marshal(cursorPayload{cursorVersion, publishedAtMS, id})
	return prefix + base64.RawURLEncoding.EncodeToString(raw)
}

// decodeCursor parses and validates an opaque cursor in the given namespace
// prefix.
//
// The payload is untrusted input: it is decoded under the
// same strict rules as a request body — known fields only, exactly one JSON
// value — and every decoded value must satisfy the public contract.
func decodeCursor(s, prefix string) (cursor, error) {
	if s == "" || len(s) > cursorMaxLen {
		return cursor{}, errInvalidCursor
	}
	if !strings.HasPrefix(s, prefix) {
		return cursor{}, errInvalidCursor
	}

	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, prefix))
	if err != nil {
		return cursor{}, errInvalidCursor
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var payload cursorPayload
	if err := dec.Decode(&payload); err != nil {
		return cursor{}, errInvalidCursor
	}
	// Reject trailing data: the cursor is exactly one JSON value.
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return cursor{}, errInvalidCursor
	}

	if payload.V != cursorVersion || payload.P <= 0 || !validOpaqueID(payload.I) {
		return cursor{}, errInvalidCursor
	}
	return cursor{PublishedAtMS: payload.P, ID: payload.I}, nil
}

// titleCursor is the decoded continuation tuple of the search title sort.
// The generic cursor carries a timestamp; this sort's
// keyset is (collation key, id), so the payload carries the KEY string that
// internal/search produced for the last item of the page.
type titleCursor struct {
	Key string
	ID  string
}

// titleCursorPayload is the JSON payload shape shared by this codec's
// encode and decode halves.
type titleCursorPayload struct {
	V int    `json:"v"`
	K string `json:"k"`
	I string `json:"i"`
}

// encodeTitleCursor serializes a text-key continuation tuple into its opaque
// form under the given namespace prefix.
func encodeTitleCursor(prefix, key, id string) string {
	// json.Marshal of this fixed shape cannot fail; ignoring the error here
	// is narrower than threading an impossible failure through every caller.
	raw, _ := json.Marshal(titleCursorPayload{cursorVersion, key, id})
	return prefix + base64.RawURLEncoding.EncodeToString(raw)
}

// decodeTitleCursor parses and validates an opaque title-sort cursor. The
// same strict rules as the generic codec apply (known fields only, exactly
// one JSON value, contract bounds on every decoded value).
func decodeTitleCursor(s, prefix string) (titleCursor, error) {
	if s == "" || len(s) > titleCursorMaxLen {
		return titleCursor{}, errInvalidCursor
	}
	if !strings.HasPrefix(s, prefix) {
		return titleCursor{}, errInvalidCursor
	}

	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, prefix))
	if err != nil {
		return titleCursor{}, errInvalidCursor
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var payload titleCursorPayload
	if err := dec.Decode(&payload); err != nil {
		return titleCursor{}, errInvalidCursor
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return titleCursor{}, errInvalidCursor
	}

	if payload.V != cursorVersion || !validTitleKey(payload.K) || !validOpaqueID(payload.I) {
		return titleCursor{}, errInvalidCursor
	}
	return titleCursor{Key: payload.K, ID: payload.I}, nil
}

// validTitleKey reports whether a decoded collation key could be one the
// server minted. Only the rune bound is checked: the key is built by
// truncating a folded title to search.SortKeyRuneLimit runes, so any shorter
// text is a shape the server can produce — including an EMPTY key (a title
// made only of combining marks folds away) and text with control characters
// (titles are not stripped of them, so the folding keeps them). Decode must
// accept exactly what mint can produce, or a legitimate continuation cursor
// becomes a 400 mid-walk. The key is only ever a bound parameter, never SQL
// syntax, so nothing here is an injection boundary.
func validTitleKey(k string) bool {
	return utf8.RuneCountInString(k) <= search.SortKeyRuneLimit
}

// commentCursor is the decoded continuation tuple of a comment list
// cursor. Unlike the generic cursor, comment cursors are SORT-BOUND:
// the payload carries the sort, and decode requires it
// to match the active sort — a cursor minted under one sort is rejected
// under another (400, the strict-params convention). Hearts carries the
// top sort's third keyset slot (hearts_count); it is 0 for the two time
// sorts.
type commentCursor struct {
	Sort        string
	CreatedAtMS int64
	Hearts      int64
	ID          string
}

// commentCursorPayload is the JSON payload shape shared by the comment
// cursor's encode and decode halves.
type commentCursorPayload struct {
	V int    `json:"v"`
	S string `json:"s"`
	P int64  `json:"p"`
	H int64  `json:"h"`
	I string `json:"i"`
}

// encodeCommentCursor serializes a comment continuation tuple under the
// given namespace prefix. sort is one of the commentSort* constants
// (comments.go).
func encodeCommentCursor(prefix, sort string, createdAtMS, hearts int64, id string) string {
	raw, _ := json.Marshal(commentCursorPayload{cursorVersion, sort, createdAtMS, hearts, id})
	return prefix + base64.RawURLEncoding.EncodeToString(raw)
}

// decodeCommentCursor parses and validates an opaque comment cursor in the
// given namespace. wantSort must equal the payload's sort — the
// sort-binding contract. For the time sorts the hearts slot must be 0; for
// the top sort it may be any non-negative value.
func decodeCommentCursor(s, prefix, wantSort string) (commentCursor, error) {
	if s == "" || len(s) > cursorMaxLen {
		return commentCursor{}, errInvalidCursor
	}
	if !strings.HasPrefix(s, prefix) {
		return commentCursor{}, errInvalidCursor
	}

	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, prefix))
	if err != nil {
		return commentCursor{}, errInvalidCursor
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var payload commentCursorPayload
	if err := dec.Decode(&payload); err != nil {
		return commentCursor{}, errInvalidCursor
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return commentCursor{}, errInvalidCursor
	}

	if payload.V != cursorVersion || payload.S != wantSort || payload.P <= 0 || !validOpaqueID(payload.I) {
		return commentCursor{}, errInvalidCursor
	}
	if payload.S == sortTop {
		if payload.H < 0 {
			return commentCursor{}, errInvalidCursor
		}
	} else if payload.H != 0 {
		return commentCursor{}, errInvalidCursor
	}
	return commentCursor{Sort: payload.S, CreatedAtMS: payload.P, Hearts: payload.H, ID: payload.I}, nil
}

// validOpaqueID reports whether s is a public identifier: 1-64 ASCII
// characters from the RFC 3986 unreserved set
// [A-Za-z0-9._~-]. Clients must not infer type, time, or storage origin.
func validOpaqueID(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '_', r == '~', r == '-':
		default:
			return false
		}
	}
	return true
}
