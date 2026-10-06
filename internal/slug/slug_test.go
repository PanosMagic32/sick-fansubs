package slug

import (
	"strings"
	"testing"
)

func TestGenerate_GreekTransliteration(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"digraphs", "θάλασσα χώρα ψάρι", "thalassa-chora-psari"},
		{"accented uppercase", "Όνομα Ταξίδι", "onoma-taxidi"},
		{"final sigma", "στάσης", "stasis"},
		{"eta upsilon beta gamma", "ήλιος ύδωρ βιβλίο γάτα", "ilios-ydor-vivlio-gata"},
		{"latin passthrough", "Naruto Shipuuden", "naruto-shipuuden"},
		{"mixed", "One Piece — Επεισόδιο 12", "one-piece-epeisodio-12"},
		{"punctuation collapses", "a!!b??c", "a-b-c"},
		{"leading trailing punctuation", "-- hi --", "hi"},
		{"digits survive", "project 2026", "project-2026"},
		{"empty input", "", ""},
		{"no transliterable runes", "你好 · 世界", ""},
		{"latin accented degrade to hyphens", "déjà vu", "d-j-vu"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := generate(tc.in); got != tc.want {
				t.Errorf("generate(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestGenerate_WholeAlphabet(t *testing.T) {
	t.Parallel()

	// Every letter of the Greek alphabet transliterates to its ELOT-743
	// value — the pin that keeps the map complete (a missing rune would
	// degrade to a hyphen and break this string).
	if got, want := generate("αβγδεζηθικλμνξοπρστυφχψω"), "avgdezithiklmnxoprstyfchpso"; got != want {
		t.Errorf("alphabet = %q, want %q", got, want)
	}
	if got, want := generate("ΑΒΓΔΕΖΗΘΙΚΛΜΝΞΟΠΡΣΤΥΦΧΨΩ"), "avgdezithiklmnxoprstyfchpso"; got != want {
		t.Errorf("uppercase alphabet = %q, want %q", got, want)
	}
}

// TestGenerate_EveryMapEntry pins every map entry against a literal table —
// iterating greekToLatin itself would leave a deleted entry silently
// untested. The count assertion fails when the map grows without the pin.
func TestGenerate_EveryMapEntry(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"Α": "a", "Β": "v", "Γ": "g", "Δ": "d", "Ε": "e", "Ζ": "z", "Η": "i",
		"Θ": "th", "Ι": "i", "Κ": "k", "Λ": "l", "Μ": "m", "Ν": "n", "Ξ": "x",
		"Ο": "o", "Π": "p", "Ρ": "r", "Σ": "s", "Τ": "t", "Υ": "y", "Φ": "f",
		"Χ": "ch", "Ψ": "ps", "Ω": "o",
		"Ά": "a", "Έ": "e", "Ή": "i", "Ί": "i", "Ό": "o", "Ύ": "y", "Ώ": "o",
		"Ϊ": "i", "Ϋ": "y",
		"α": "a", "β": "v", "γ": "g", "δ": "d", "ε": "e", "ζ": "z", "η": "i",
		"θ": "th", "ι": "i", "κ": "k", "λ": "l", "μ": "m", "ν": "n", "ξ": "x",
		"ο": "o", "π": "p", "ρ": "r", "σ": "s", "ς": "s", "τ": "t", "υ": "y",
		"φ": "f", "χ": "ch", "ψ": "ps", "ω": "o",
		"ά": "a", "έ": "e", "ή": "i", "ί": "i", "ό": "o", "ύ": "y", "ώ": "o",
		"ϊ": "i", "ϋ": "y", "ΰ": "y", "ΐ": "i",
	}
	if len(greekToLatin) != len(want) {
		t.Fatalf("greekToLatin has %d entries, the pin lists %d — update both together", len(greekToLatin), len(want))
	}
	for in, expected := range want {
		if got := generate(in); got != expected {
			t.Errorf("generate(%q) = %q, want %q", in, got, expected)
		}
	}
}

func TestGenerateBase_Truncates(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", BaseMaxRunes+50)
	if got := GenerateBase(long); len(got) != BaseMaxRunes {
		t.Errorf("GenerateBase len = %d, want %d", len(got), BaseMaxRunes)
	}
	short := "hello"
	if got := GenerateBase(short); got != "hello" {
		t.Errorf("GenerateBase(%q) = %q, want %q", short, got, short)
	}
}

func TestGenerateBase_TrimsTrailingHyphenAtTheBoundary(t *testing.T) {
	t.Parallel()

	// "α!" transliterates to "a-": the repeated base is "a-a-a-…", and a
	// byte-truncation at BaseMaxRunes lands exactly on a hyphen. The result
	// must stay on grammar — no trailing hyphen, so a "-2" collision suffix
	// can never produce "--2".
	base := GenerateBase(strings.Repeat("α!", 60))
	if len(base) > BaseMaxRunes {
		t.Errorf("GenerateBase boundary len = %d, want ≤ %d", len(base), BaseMaxRunes)
	}
	if strings.HasSuffix(base, "-") || strings.Contains(base, "--") || strings.HasPrefix(base, "-") {
		t.Errorf("GenerateBase boundary = %q, want a trimmed on-grammar base", base)
	}
}

func TestValid(t *testing.T) {
	t.Parallel()

	valid := []string{"a", "one-piece", "project-2026", "abc123", strings.Repeat("a", MaxRunes)}
	for _, s := range valid {
		if !Valid(s) {
			t.Errorf("Valid(%q) = false, want true", s)
		}
	}

	invalid := []string{
		"",
		"-leading",
		"trailing-",
		"double--hyphen",
		"Uppercase",
		"has space",
		"καλημέρα",
		"under_score",
		"déjà",
		strings.Repeat("a", MaxRunes+1),
	}
	for _, s := range invalid {
		if Valid(s) {
			t.Errorf("Valid(%q) = true, want false", s)
		}
	}
}
