package search

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// CollationGreek is the SQL collation name registered with the SQLite
// driver (internal/database). It is a package constant, never user input —
// the store may splice it into a query's ORDER BY, but bound parameters
// remain the only data path.
const CollationGreek = "sf_greek"

// SortKeyRuneLimit bounds a collation key in runes. It matches the content
// title bound (200 runes, internal/handler), so keys are lossless for every
// row the API itself can write; a longer legacy title is truncated for
// ordering and cursor purposes. The bound exists so a continuation cursor
// carrying a text key has a bounded size.
const SortKeyRuneLimit = 200

// FunctionFold is the SQL scalar function name registered with the driver
// (internal/database): sf_fold(text) returns Fold(text). The staff content
// filters splice the NAME (a package constant, never user input) into SQL and
// bind the folded needle, so SQL folds a title exactly as this package does.
const FunctionFold = "sf_fold"

// greekAlphabet lists the 24 lowercase Greek letters in alphabet order. A
// folded rune found here sorts by its position; every other rune sorts after
// all Greek letters by code point.
const greekAlphabet = "αβγδεζηθικλμνξοπρστυφχψω"

// greekRank maps each alphabet letter to its position.
var greekRank = func() map[rune]int {
	m := make(map[rune]int, len(greekAlphabet))
	for i, r := range greekAlphabet {
		m[r] = i
	}
	return m
}()

// foldRune applies the collation's per-rune folding: Greek accents and
// combining marks are stripped (the same greekAccentMap that normalizes
// indexed text), the letter is lowercased, and final sigma is folded to
// sigma. The second return is false for a rune that leaves no key content.
func foldRune(r rune) (rune, bool) {
	if base, ok := greekAccentMap[r]; ok {
		r = base
	}
	if unicode.Is(unicode.Mn, r) {
		return 0, false
	}
	r = unicode.ToLower(r)
	if r == 'ς' {
		r = 'σ'
	}
	return r, true
}

// Compare is the collation function handed to the SQLite driver. It returns
// a negative, zero, or positive integer — normalized to -1/0/1, the shape
// the driver documents — and is deterministic and transitive as SQLite
// requires.
//
// It folds and compares on the fly rather than calling SortKey, so a sort
// over n rows allocates nothing per comparison. The two halves must agree in
// the way the store relies on: folding an input never changes a comparison,
// i.e. Compare(SortKey(a), SortKey(b)) == Compare(a, b) for every a and b.
// That is what lets a keyset predicate compare raw titles against a cursor
// key. Note the ordering is NOT code-point order over the key — Greek
// letters sort before everything else — so keys are only comparable through
// Compare, never through strings.Compare.
func Compare(a, b string) int {
	as, bs := foldScanner{s: a}, foldScanner{s: b}
	for range SortKeyRuneLimit {
		ra, oka := as.next()
		rb, okb := bs.next()
		switch {
		case !oka && !okb:
			return 0
		case !oka:
			return -1
		case !okb:
			return 1
		}
		classA, rankA := ranked(ra)
		classB, rankB := ranked(rb)
		if classA != classB {
			return sign(classA - classB)
		}
		if rankA != rankB {
			return sign(rankA - rankB)
		}
	}
	return 0
}

// Fold applies the collation's folding to EVERY rune of s — the string form
// of foldRune, without SortKey's truncation: Greek accents and combining
// marks are stripped, the letter is lowercased, and final sigma folds to
// sigma. It backs the SQL `sf_fold` function (FunctionFold) and the staff
// content filter's needle, so a substring match folds both sides the same
// way a sort key does.
func Fold(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	f := foldScanner{s: s}
	for {
		r, ok := f.next()
		if !ok {
			return b.String()
		}
		b.WriteRune(r)
	}
}

// FilterTokens splits a staff-list filter query into folded needles: the
// query is cut at every character that is neither a letter nor a digit, each
// piece is folded (Fold), and the pieces keep their order.
//
// The staff content filter ANDs one substring test per needle, and that split
// is what makes the two search surfaces agree on where a WORD ends: `dr stone`
// finds a title stored as `Dr. Stone`, exactly like the public search, whose
// FTS5 query is cut at the same boundaries (BuildQuery turns FTS5 syntax into
// spaces and the unicode61 tokenizer treats every other punctuation mark as a
// separator). The two rules are NOT identical: BuildQuery also drops the
// reserved words (and/or/not/near) and prefix-matches the LAST token, while
// this split keeps a reserved word as a word, every needle is a plain
// substring, and a query with no letters and no digits is NO filter here
// (the public search answers 422 invalidFormat for the same input). One rune
// class diverges: FTS5's tokenizer counts every letter and number category,
// while this split keeps the decimal digits only — a numeral like `Ⅷ` is a
// word for the public search and a separator here (the recorded difference).
//
// A query with no needles — blank, or punctuation only — means NO filter,
// never an empty substring test: `instr(title, ”)` matches every row, so a
// non-empty query would silently stop filtering.
func FilterTokens(q string) []string {
	pieces := strings.FieldsFunc(q, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	tokens := make([]string, 0, len(pieces))
	for _, p := range pieces {
		tokens = append(tokens, Fold(p))
	}
	return tokens
}

// SortKey returns the collation key of s: the folded text truncated to
// SortKeyRuneLimit runes. It is what a title-sort continuation cursor
// carries (the store compares it against titles through Compare).
func SortKey(s string) string {
	var b strings.Builder
	b.Grow(min(len(s), SortKeyRuneLimit*4))
	f := foldScanner{s: s}
	for range SortKeyRuneLimit {
		r, ok := f.next()
		if !ok {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

// foldScanner yields the folded runes of one string in order, skipping
// runes that leave no key content.
type foldScanner struct {
	s string
	i int
}

// next returns the next folded rune, or false at the end of the string.
func (f *foldScanner) next() (rune, bool) {
	for f.i < len(f.s) {
		r, size := utf8.DecodeRuneInString(f.s[f.i:])
		f.i += size
		if folded, keep := foldRune(r); keep {
			return folded, true
		}
	}
	return 0, false
}

// ranked maps a folded rune to its (class, rank) pair: class 0 holds the
// Greek alphabet by position, class 1 holds everything else by code point.
func ranked(r rune) (int, int) {
	if idx, ok := greekRank[r]; ok {
		return 0, idx
	}
	return 1, int(r)
}

// sign reduces a difference to the -1/0/1 shape the driver's collation
// contract specifies.
func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}
