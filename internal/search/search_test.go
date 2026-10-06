package search

import (
	"errors"
	"testing"
)

func TestNormalize_GreekAccents(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"lowercase tonos", "καλημέρα", "καλημερα"},
		{"uppercase tonos", "ΚΑΛΗΜΈΡΑ", "ΚΑΛΗΜΕΡΑ"},
		{"all accented vowels", "ά έ ή ί ό ύ ώ", "α ε η ι ο υ ω"},
		{"all accented capitals", "Ά Έ Ή Ί Ό Ύ Ώ", "Α Ε Η Ι Ο Υ Ω"},
		{"dialytika", "ϊ ϋ Ϊ Ϋ", "ι υ Ι Υ"},
		{"dialytika plus tonos", "ΐ ΰ", "ι υ"},
		{"combining mark (NFD)", "\u03B5\u0301\u03BD\u03B1", "\u03B5\u03BD\u03B1"},
		{"latin untouched", "café résumé", "café résumé"},
		{"latin combining mark stripped (NFD)", "cafe\u0301", "cafe"},
		{"plain greek untouched", "ανεμος", "ανεμος"}, // no accents present
		{"plain ascii untouched", "One Piece", "One Piece"},
		{"mixed", "Όνομα και τίτλος", "Ονομα και τιτλος"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalize(tc.in); got != tc.want {
				t.Errorf("normalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalize_Consistency(t *testing.T) {
	t.Parallel()

	// The accent-insensitivity contract: accented and unaccented forms of
	// the same word must normalize to the same string — the symmetry the
	// whole design depends on.
	pairs := [][2]string{
		{"καλημέρα", "καλημερα"},
		{"μήλο", "μηλο"},
		{"Όνομα", "Ονομα"},
		{"Ταξίδι", "Ταξιδι"},
	}
	for _, p := range pairs {
		a, b := normalize(p[0]), normalize(p[1])
		if a != b {
			t.Errorf("normalize(%q) = %q != normalize(%q) = %q", p[0], a, p[1], b)
		}
	}
}

func TestText_JoinsAndNormalizes(t *testing.T) {
	t.Parallel()

	if got := Text("Καλημέρα", "κόσμε"); got != "Καλημερα κοσμε" {
		t.Errorf("Text = %q", got)
	}
	if got := Text("", "μόνο", ""); got != "μονο" {
		t.Errorf("Text with empties = %q", got)
	}
	if got := Text("", "", "  "); got != "" {
		t.Errorf("Text all-empty = %q", got)
	}
}

func TestBuildQuery(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"single greek token gets prefix", "καλημέρα", `"καλημερα"*`},
		{"multi-token prefixes only last", "one piece", `"one" "piece"*`},
		{"reserved words dropped", "one piece AND naruto", `"one" "piece" "naruto"*`},
		{"reserved case-insensitive", "One OR Not", `"One"*`},
		{"hyphen becomes separator", "one-piece", `"one" "piece"*`},
		{"double hyphen", "one--piece", `"one" "piece"*`},
		{"specials become separators", `(one)*"piece"`, `"one" "piece"*`},
		{"colon qualifier split", "title:foo", `"title" "foo"*`},
		{"underscore survives", "one_piece", `"one_piece"*`},
		{"apostrophe inside token", "δ'αυτό", `"δ'αυτο"*`},
		{"short last token no star", "one b", `"one" "b"`},
		{"short greek last token no star", "καλημέρα α", `"καλημερα" "α"`},
		{"short single token no star", "α", `"α"`},
		{"mark-only middle token dropped", "one \u0301 piece", `"one" "piece"*`},
		{"punctuation-only middle token dropped", "one ... piece", `"one" "piece"*`},
		{"numeral-only token kept", "Ⅷ", `"Ⅷ"`},
		{"greek accents normalized", "καλημέρα κόσμε", `"καλημερα" "κοσμε"*`},
		{"brackets braces", "one [piece] {naruto}", `"one" "piece" "naruto"*`},
		{"caret stripped", "one^piece", `"one" "piece"*`},
		{"unicode whitespace split", "one\u00A0piece", `"one" "piece"*`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BuildQuery(tc.in)
			if err != nil {
				t.Fatalf("BuildQuery(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("BuildQuery(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestBuildQuery_EverySpecialCharacter runs every FTS5 special through the
// sanitizer individually (the table-driven requirement): none may
// survive as syntax, and none may concatenate the words around it.
func TestBuildQuery_EverySpecialCharacter(t *testing.T) {
	t.Parallel()

	for _, special := range []string{`*`, `"`, `(`, `)`, `+`, `-`, `{`, `}`, `[`, `]`, `^`, `:`} {
		in := "one" + special + "piece"
		got, err := BuildQuery(in)
		if err != nil {
			t.Fatalf("BuildQuery(%q): %v", in, err)
		}
		if got != `"one" "piece"*` {
			t.Errorf("BuildQuery(%q) = %q, want %q (special %q must separate, not concatenate or survive)", in, got, `"one" "piece"*`, special)
		}
	}
}

func TestBuildQuery_NoTokens(t *testing.T) {
	t.Parallel()

	for _, in := range []string{`(*")`, `"()"`, "AND OR NOT NEAR", `- - -`, `"[]"`, "!!!", "...", "\u0301"} {
		_, err := BuildQuery(in)
		if !errors.Is(err, ErrNoTokens) {
			t.Errorf("BuildQuery(%q) error = %v, want ErrNoTokens", in, err)
		}
	}
}
