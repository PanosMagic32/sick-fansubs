// Package search owns the pure text-processing rules for search: Greek
// accent normalization, FTS5 query construction, and the
// Greek title-sort collation registered with the
// SQLite driver by internal/database.
//
// SQLite cannot sort Greek: its built-in BINARY collation orders by UTF-8
// code point (no accent or case folding, and ς U+03C2 before σ U+03C3), and
// NOCASE is ASCII-oriented (docs/patterns/go/sqlite.md rule 7). The package
// therefore owns the comparison (`collate.go`): fold each rune — accents
// stripped, lowercased, final sigma folded — truncate to SortKeyRuneLimit
// runes, then compare classes first (Greek letters in alphabet order,
// everything else by code point), so Greek titles sort α→ω and Latin/digit
// titles follow them.
//
// It has no dependencies on the database or HTTP packages — every caller
// (import tool, rebuild command, handler, store) shares one implementation,
// so the indexed text and the query text are normalized by the SAME
// function. That symmetry is what makes accent-insensitive Greek search
// correct.
package search

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// greekAccentMap strips precomposed Greek accents (tonos and dialytika
// forms) down to their base letters. The unicode61 tokenizer's
// remove_diacritics option only removes combining marks (Mn), and real
// Greek text uses precomposed letters — so this map is the actual
// accent-insensitivity mechanism.
// Uppercase entries preserve the letter for the tokenizer's case folding.
var greekAccentMap = map[rune]rune{
	'Ά': 'Α', 'Έ': 'Ε', 'Ή': 'Η', 'Ί': 'Ι', 'Ό': 'Ο', 'Ύ': 'Υ', 'Ώ': 'Ω',
	'ΐ': 'ι', 'Ϊ': 'Ι', 'Ϋ': 'Υ',
	'ά': 'α', 'έ': 'ε', 'ή': 'η', 'ί': 'ι', 'ό': 'ο', 'ύ': 'υ', 'ώ': 'ω',
	'ϊ': 'ι', 'ϋ': 'υ', 'ΰ': 'υ',
}

// fts5Specials are the FTS5 query-syntax characters that must never reach
// the MATCH argument as syntax. They are
// replaced with SPACES, not deleted: the tokenizer treats them as word
// separators, and deleting them would concatenate neighboring words
// ("one-piece" would become "onepiece" and never match).
const fts5Specials = `*"()+-{}[]^:`

// reservedWords are FTS5 boolean operators, dropped case-insensitively as
// whole words. The target exposes no Boolean UI, so they are user
// text to ignore, never syntax to honor.
var reservedWords = map[string]bool{
	"and":  true,
	"or":   true,
	"not":  true,
	"near": true,
}

// normalize strips Greek accents and combining marks from s. Case is
// preserved: the tokenizer's own case folding handles it (proven for
// Greek). Precomposed Latin text passes through untouched; a combining mark
// is stripped whatever script it belongs to.
func normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if base, ok := greekAccentMap[r]; ok {
			b.WriteRune(base)
			continue
		}
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Text builds the normalized indexed text for one content row by joining
// the non-empty parts with single spaces and normalizing the result.
// Blog posts pass (title, subtitle, description); projects pass
// (title, description) — one implementation for both.
func Text(parts ...string) string {
	nonEmpty := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	return normalize(strings.Join(nonEmpty, " "))
}

// BuildQuery converts raw user input into a safe FTS5 MATCH argument.
//
// # Steps
//
//  1. Replace every FTS5 special character with a space.
//  2. Split on whitespace; drop reserved words (case-insensitive).
//  3. Normalize each surviving token (the package's normalize step — Greek
//     accents stripped) and drop a token that leaves no searchable letter or
//     number — a punctuation-only or mark-only remnant cannot match anything,
//     and a zero-token phrase is not a query FTS5 should have to interpret.
//  4. Phrase-quote each token — user text is never FTS5 syntax.
//  5. Append `*` (prefix) to the LAST token when it has at least 2 runes
//     (shorter tokens get no star rather than rejecting the query).
//
// Quoted tokens are ANDed (FTS5's default). The result is a bound SQL
// parameter, never interpolated into the query string.
func BuildQuery(q string) (string, error) {
	cleaned := strings.Map(func(r rune) rune {
		if strings.ContainsRune(fts5Specials, r) {
			return ' '
		}
		return r
	}, q)

	words := strings.Fields(cleaned)
	tokens := make([]string, 0, len(words))
	for _, w := range words {
		if reservedWords[strings.ToLower(w)] {
			continue
		}
		norm := normalize(w)
		if !hasSearchRune(norm) {
			continue
		}
		tokens = append(tokens, norm)
	}
	if len(tokens) == 0 {
		return "", ErrNoTokens
	}

	for i, t := range tokens {
		if i == len(tokens)-1 && utf8.RuneCountInString(t) >= 2 {
			tokens[i] = `"` + t + `"*`
		} else {
			tokens[i] = `"` + t + `"`
		}
	}
	return strings.Join(tokens, " "), nil
}

// hasSearchRune reports whether the normalized token carries at least one
// rune the tokenizer can index. FTS5's unicode61 treats the Unicode letter
// and number categories as token characters, so a token without a letter or
// number produces no searchable term and BuildQuery drops it instead of
// emitting an empty phrase.
func hasSearchRune(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return true
		}
	}
	return false
}
