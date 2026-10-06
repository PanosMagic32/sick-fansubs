package handler

import (
	"strings"
	"testing"
)

// Shared cursor-codec tests: the blog, project, and search namespaces share
// one implementation, so the contract is pinned once per prefix here. The
// HTTP-level tests (blog_test.go,
// project_test.go, search_test.go) still pin each endpoint's wire behavior
// end to end.

var cursorNamespaces = []struct {
	name   string
	prefix string
}{
	{"blog", blogCursorPrefix},
	{"project", projectCursorPrefix},
	{"search", searchCursorPrefix},
	{"search-oldest", searchOldestCursorPrefix},
}

// TestDecodeCursor_FixedWireFormat pins the accepted cursor encoding per
// namespace so a refactor cannot silently change what clients send back:
// "<prefix>" plus base64url of {"v":1,"p":<publishedAtMs>,"i":<id>}.
func TestDecodeCursor_FixedWireFormat(t *testing.T) {
	t.Parallel()

	// base64url of {"v":1,"p":1713039000000,"i":"abc"} — the shared payload
	// under each namespace prefix.
	payload := "eyJ2IjoxLCJwIjoxNzEzMDM5MDAwMDAwLCJpIjoiYWJjIn0"
	for _, ns := range cursorNamespaces {
		t.Run(ns.name, func(t *testing.T) {
			t.Parallel()

			c, err := decodeCursor(ns.prefix+payload, ns.prefix)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if c.PublishedAtMS != 1713039000000 || c.ID != "abc" {
				t.Errorf("got %+v, want {1713039000000 abc}", c)
			}
		})
	}
}

// TestCursor_RoundTrip proves encode and decode are inverses per namespace
// over a representative set of valid continuation tuples.
func TestCursor_RoundTrip(t *testing.T) {
	t.Parallel()

	values := []cursor{
		{PublishedAtMS: 1, ID: "a"},
		{PublishedAtMS: 1713039000000, ID: "0123456789abcdef0123456789abcdef"},
		{PublishedAtMS: 9223372036854775807, ID: "A-Z_a~z.0-9"},
	}
	for _, ns := range cursorNamespaces {
		t.Run(ns.name, func(t *testing.T) {
			t.Parallel()

			for _, want := range values {
				got, err := decodeCursor(
					encodeCursor(ns.prefix, want.PublishedAtMS, want.ID),
					ns.prefix,
				)
				if err != nil {
					t.Errorf("round trip %+v: %v", want, err)
					continue
				}
				if got != want {
					t.Errorf("round trip: got %+v, want %+v", got, want)
				}
			}
		})
	}
}

// TestDecodeCursor_RejectsInvalid proves each contract bound fails closed
// with errInvalidCursor and never partially decodes. The length-bound
// cases are computed from the namespace prefix so the 256-character bound
// is tested exactly for every prefix length.
func TestDecodeCursor_RejectsInvalid(t *testing.T) {
	t.Parallel()

	for _, ns := range cursorNamespaces {
		t.Run(ns.name, func(t *testing.T) {
			t.Parallel()

			cases := map[string]string{
				"empty":                      "",
				"at the length bound":        ns.prefix + strings.Repeat("A", cursorMaxLen-len(ns.prefix)),   // 256 chars — fails at base64
				"over the length bound":      ns.prefix + strings.Repeat("A", cursorMaxLen-len(ns.prefix)+1), // 257 chars — fails at the size check
				"no version prefix":          "eyJ2IjoxLCJwIjoxLCJpIjoiYSJ9",
				"bad base64":                 ns.prefix + "!!!not-base64!!!",
				"not JSON":                   ns.prefix + "bm90LWpzb24",
				"wrong version":              ns.prefix + "eyJ2IjoyLCJwIjoxLCJpIjoiYSJ9",
				"missing version":            ns.prefix + "eyJwIjoxLCJpIjoiYSJ9",
				"non-positive timestamp":     ns.prefix + "eyJ2IjoxLCJwIjowLCJpIjoiYSJ9",
				"negative timestamp":         ns.prefix + "eyJ2IjoxLCJwIjotNSwiaSI6ImEifQ",
				"missing id":                 ns.prefix + "eyJ2IjoxLCJwIjoxfQ",
				"empty id":                   ns.prefix + "eyJ2IjoxLCJwIjoxLCJpIjoiIn0",
				"id with reserved character": ns.prefix + "eyJ2IjoxLCJwIjoxLCJpIjoiYS9iIn0",
				"unknown field":              ns.prefix + "eyJ2IjoxLCJwIjoxLCJpIjoiYSIsIngiOjF9",
				"trailing JSON value":        ns.prefix + "eyJ2IjoxLCJwIjoxLCJpIjoiYSJ9eyJ4IjoxfQ",
			}
			for name, token := range cases {
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					if _, err := decodeCursor(token, ns.prefix); err == nil {
						t.Errorf("decodeCursor(%q, %q): got nil, want errInvalidCursor", token, ns.prefix)
					}
				})
			}
		})
	}
}

// TestCursorNamespacesDoNotCross pins the namespace rule: a cursor from one
// endpoint never decodes on another — the prefixes are distinct by design,
// and a foreign cursor is rejected as one opaque token.
func TestCursorNamespacesDoNotCross(t *testing.T) {
	t.Parallel()

	for _, from := range cursorNamespaces {
		for _, to := range cursorNamespaces {
			if from.prefix == to.prefix {
				continue
			}
			t.Run(from.name+" cursor rejected by "+to.name, func(t *testing.T) {
				t.Parallel()

				token := encodeCursor(from.prefix, 1713039000000, "abc")
				if _, err := decodeCursor(token, to.prefix); err == nil {
					t.Errorf("%s cursor %q decoded under %s — the %s prefix must reject it",
						from.name, token, to.name, to.prefix)
				}
			})
		}
	}
}

// TestValidOpaqueID pins the identifier contract used by
// cursor validation: 1-64 ASCII characters from [A-Za-z0-9._~-].
func TestValidOpaqueID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		id   string
		want bool
	}{
		{"single character", "a", true},
		{"migrated 24-hex", "65b0a1c2d3e4f5a6b7c8d9e0", true},
		{"new 32-hex", "0123456789abcdef0123456789abcdef", true},
		{"full unreserved set", "ABC-def_ghi.jkl~mno", true},
		{"maximum length", strings.Repeat("a", 64), true},
		{"empty", "", false},
		{"too long", strings.Repeat("a", 65), false},
		{"non-ASCII", "αβγ", false},
		{"reserved solidus", "a/b", false},
		{"space", "a b", false},
		{"reserved question mark", "a?b", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := validOpaqueID(tc.id); got != tc.want {
				t.Errorf("validOpaqueID(%q) = %v, want %v", tc.id, got, tc.want)
			}
		})
	}
}
