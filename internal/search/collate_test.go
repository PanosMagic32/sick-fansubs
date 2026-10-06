package search

import (
	"strings"
	"testing"
)

// TestCompare_GreekAlphabetOrder pins the accepted collation rule: Greek
// letters in alphabet order, then everything else by code point.
func TestCompare_GreekAlphabetOrder(t *testing.T) {
	t.Parallel()

	// The expected order as one list: comparing neighbours must be strictly
	// ascending, which pins both the alphabet rule and the class boundary.
	ordered := []string{
		"Αγάπη",    // α
		"Άλφα",     // α (accent-insensitive)
		"Βήτα",     // β
		"Γάμμα",    // γ
		"Ζωή",      // ζ
		"Λάμδα",    // λ
		"Πι",       // π
		"Σίγμα",    // σ
		"Ταυ",      // τ
		"Ωμέγα",    // ω
		"5 Αιώνες", // digits after Greek
		"One Piece",
		"zeta",
	}

	for i := range ordered {
		for j := range ordered {
			got := Compare(ordered[i], ordered[j])
			want := 0
			switch {
			case i < j:
				want = -1
			case i > j:
				want = 1
			}
			if sign(got) != want {
				t.Errorf("Compare(%q, %q) = %d, want sign %d", ordered[i], ordered[j], got, want)
			}
		}
	}
}

// TestCompare_Folding pins the folding rules: accents and case are ignored,
// final sigma equals sigma, and combining marks leave no key content.
func TestCompare_Folding(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"Άμλετ", "Αμλετ", 0},     // tonos ignored
		{"Άμλετ", "άμλετ", 0},     // accent + case ignored
		{"ΑΛΦΑ", "αλφα", 0},       // case ignored
		{"ς", "σ", 0},             // final sigma folds to sigma
		{"τέλος", "τελοσ", 0},     // …in the middle of a word too
		{"ι", "ϊ", 0},             // dialytika ignored
		{"με\u0301ρα", "μερα", 0}, // combining acute ignored
		{"Ά", "Β", -1},            // still ordered after folding
		{"α", "αα", -1},           // prefix sorts first
		{"", "α", -1},             // an empty key sorts before everything
		{"", "", 0},
	} {
		if got := Compare(tc.a, tc.b); sign(got) != tc.want {
			t.Errorf("Compare(%q, %q) = %d, want sign %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestSortKey_FoldingInvariance pins the invariant the two halves of the
// collation must satisfy: folding an input never changes a comparison. The
// store's keyset predicate compares raw titles against a cursor key, which
// is only sound because Compare(SortKey(a), SortKey(b)) == Compare(a, b).
// (The key's own byte order is NOT the sort order — Greek letters sort
// before Latin ones — so keys are comparable only through Compare.)
func TestSortKey_FoldingInvariance(t *testing.T) {
	t.Parallel()

	corpus := []string{
		"", "α", "Α", "ά", "ς", "σ", "Σ", "ω", "Ωμέγα", "Άμλετ", "Αμλετ",
		"Καλημέρα κόσμε", "One Piece", "one piece", "5 Centimeters", "BHA S8",
		"Ελληνικά", "ΕΛΛΗΝΙΚΑ", "ελληνικα", "ϊ", "ΐ", "με\u0301ρα", "αα", "αβ",
		strings.Repeat("α", SortKeyRuneLimit+50), "zzz", "  ", "！", "Ω",
	}

	for _, a := range corpus {
		for _, b := range corpus {
			want := sign(Compare(a, b))
			if got := sign(Compare(SortKey(a), SortKey(b))); got != want {
				t.Errorf("Compare(%q, %q) = %d but comparing the folded keys gives %d",
					a, b, want, got)
			}
		}
	}

	// Idempotence: folding a key again changes nothing (the store compares
	// titles against a cursor key, so a non-idempotent key would drift).
	for _, s := range corpus {
		if key, again := SortKey(s), SortKey(SortKey(s)); key != again {
			t.Errorf("SortKey not idempotent for %q: %q then %q", s, key, again)
		}
	}
}

// TestFold pins the string fold the SQL function and the staff content filter
// share: it is the collation's per-rune folding applied to the WHOLE string
// (no SortKey truncation), so a filter needle folds exactly like the title it
// is matched against.
func TestFold(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in, want string
	}{
		{"ΟΔΥΣΣΕΎΣ", "οδυσσευσ"},   // case + accent + final sigma
		{"Άμλετ", "αμλετ"},         // tonos ignored
		{"με\u0301ρα", "μερα"},     // combining acute ignored
		{"One Piece", "one piece"}, // Latin case folds, spacing kept
		{"", ""},                   // empty folds to empty
		{"！ΑΒΓ", "！αβγ"},           // non-letters pass through
		{"5 Centimeters", "5 centimeters"},
	} {
		if got := Fold(tc.in); got != tc.want {
			t.Errorf("Fold(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	// Fold agrees with SortKey below the truncation bound, and is idempotent
	// — the property that lets SQL fold a needle and a title independently.
	for _, s := range []string{"", "ς", "Άμλετ", "Καλημέρα κόσμε", "ΑΒΓ"} {
		if got, want := Fold(s), SortKey(s); got != want {
			t.Errorf("Fold(%q) = %q, want SortKey's %q below the bound", s, got, want)
		}
		if again := Fold(Fold(s)); again != Fold(s) {
			t.Errorf("Fold not idempotent for %q: %q then %q", s, Fold(s), again)
		}
	}
}

// TestFilterTokens pins the staff filter's word split: the query is cut at
// every non-letter and
// non-digit character, each piece is folded, and a query with no letters or
// digits yields no tokens at all — which the store reads as "no filter"
// rather than as an empty substring test.
func TestFilterTokens(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"dr stone", []string{"dr", "stone"}},
		{"Dr. Stone", []string{"dr", "stone"}},
		{"one-piece", []string{"one", "piece"}},
		{"1080p", []string{"1080p"}},
		{"Άμλετ", []string{"αμλετ"}},
		{"Τέλος Οδυσσεύς", []string{"τελοσ", "οδυσσευσ"}},
		{"  spaced  out  ", []string{"spaced", "out"}},
		// Punctuation-only, blank, and a lone combining mark: no needles.
		{"", nil},
		{"   ", nil},
		{"...", nil},
		{"\u0301", nil},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got := FilterTokens(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("FilterTokens(%q) = %q, want %q", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("FilterTokens(%q) = %q, want %q", tc.in, got, tc.want)
				}
			}
		})
	}
}

// TestSortKey_Truncation pins the rune bound: a long title is truncated so a
// continuation cursor carrying the key stays bounded.
func TestSortKey_Truncation(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("α", SortKeyRuneLimit+25)
	if got := len([]rune(SortKey(long))); got != SortKeyRuneLimit {
		t.Errorf("key runes = %d, want %d", got, SortKeyRuneLimit)
	}

	// Truncation makes two titles sharing the first SortKeyRuneLimit runes
	// equal under the collation — the id tie-break then orders them.
	shared := strings.Repeat("β", SortKeyRuneLimit)
	if Compare(shared+"x", shared+"y") != 0 {
		t.Error("titles beyond the key bound must compare equal (id is the tie-break)")
	}
}
