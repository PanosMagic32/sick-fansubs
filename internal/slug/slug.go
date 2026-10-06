// Package slug generates and validates project slugs: lowercase latin
// letters, digits, and hyphens only.
//
// Slugs are data, never address keys: URLs are ID-addressed,
// and the slug exists for search and deduplication. This package owns the
// ONLY Greek→latin transliteration in the codebase — the search package's
// accent map folds Greek accents to GREEK letters (FTS5), while this map
// transliterates to LATIN letters, so the two maps are deliberately
// separate contracts.
package slug

import (
	"strings"
	"unicode"
)

// MaxRunes bounds a manual slug edit: the API contract
// bounds the length, not the SQLite schema.
const MaxRunes = 100

// BaseMaxRunes caps a generated base so any collision suffix ("-2", …, "-100")
// still fits MaxRunes.
const BaseMaxRunes = 90

// greekToLatin is the ELOT-743-style transliteration map.
// Precomposed Greek accents are folded straight into their latin
// base — no separate accent-stripping pass is needed for Greek. Digraph
// letters map to multi-rune strings (θ→th, χ→ch, ψ→ps, φ→f, ξ→x).
// The map covers the modern monotonic forms; polytonic (Greek Extended)
// forms are outside it and degrade to hyphens like any other unmapped rune
// (recorded acceptance).
var greekToLatin = map[rune]string{
	// Uppercase.
	'Α': "a", 'Β': "v", 'Γ': "g", 'Δ': "d", 'Ε': "e", 'Ζ': "z", 'Η': "i",
	'Θ': "th", 'Ι': "i", 'Κ': "k", 'Λ': "l", 'Μ': "m", 'Ν': "n", 'Ξ': "x",
	'Ο': "o", 'Π': "p", 'Ρ': "r", 'Σ': "s", 'Τ': "t", 'Υ': "y", 'Φ': "f",
	'Χ': "ch", 'Ψ': "ps", 'Ω': "o",
	'Ά': "a", 'Έ': "e", 'Ή': "i", 'Ί': "i", 'Ό': "o", 'Ύ': "y", 'Ώ': "o",
	'Ϊ': "i", 'Ϋ': "y",
	// Lowercase (final sigma included).
	'α': "a", 'β': "v", 'γ': "g", 'δ': "d", 'ε': "e", 'ζ': "z", 'η': "i",
	'θ': "th", 'ι': "i", 'κ': "k", 'λ': "l", 'μ': "m", 'ν': "n", 'ξ': "x",
	'ο': "o", 'π': "p", 'ρ': "r", 'σ': "s", 'ς': "s", 'τ': "t", 'υ': "y",
	'φ': "f", 'χ': "ch", 'ψ': "ps", 'ω': "o",
	'ά': "a", 'έ': "e", 'ή': "i", 'ί': "i", 'ό': "o", 'ύ': "y", 'ώ': "o",
	'ϊ': "i", 'ϋ': "y", 'ΰ': "y", 'ΐ': "i",
}

// generate transliterates a title into a lowercase latin slug:
//
//  1. Greek runes (accented or not) map through greekToLatin.
//  2. Remaining runes lowercase (A-Z → a-z); every other non-[a-z0-9]
//     rune becomes a hyphen. Latin accented letters (é, ü) are NOT
//     specially folded — they degrade to hyphens under the strict slug
//     grammar, and the product's titles are Greek/English. An admin can
//     hand-edit the slug afterwards.
//  3. Consecutive hyphens collapse and the edges are trimmed.
//
// The result is empty only when nothing survives the transliteration (e.g.
// a title with no latin or Greek letters); the caller falls back to an
// ID-derived slug so a create can never fail on slug generation alone.
func generate(title string) string {
	var b strings.Builder
	b.Grow(len(title))
	for _, r := range title {
		if s, ok := greekToLatin[r]; ok {
			b.WriteString(s)
			continue
		}
		r = unicode.ToLower(r)
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return collapse(b.String())
}

// GenerateBase is generate truncated to BaseMaxRunes — the form the create
// handler uses before applying collision suffixes.
func GenerateBase(title string) string {
	base := generate(title)
	if len(base) <= BaseMaxRunes {
		return base
	}
	base = base[:BaseMaxRunes]
	// Re-trim the cut edge: byte-truncation can leave a trailing hyphen,
	// which would break the grammar AND turn a collision suffix into "--2".
	return strings.TrimRight(base, "-")
}

// Valid reports whether s satisfies the manual-edit grammar:
// 1..MaxRunes runes of [a-z0-9-] with no leading/trailing hyphen and
// no consecutive hyphens.
func Valid(s string) bool {
	if s == "" || len(s) > MaxRunes {
		return false
	}
	prevHyphen := true // a leading hyphen is rejected
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			prevHyphen = false
		case r == '-':
			if prevHyphen {
				return false
			}
			prevHyphen = true
		default:
			return false
		}
	}
	return !prevHyphen // no trailing hyphen
}

// collapse squeezes hyphen runs and trims the edges. Input is already
// ASCII (the transliteration pass produced [a-z0-9-] only).
func collapse(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '-' {
			if len(out) > 0 && out[len(out)-1] != '-' {
				out = append(out, '-')
			}
			continue
		}
		out = append(out, s[i])
	}
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return string(out)
}
