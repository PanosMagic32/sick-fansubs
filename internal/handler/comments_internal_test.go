package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"sick-fansubs/internal/store"
)

// Internal comments-handler tests: the wire-sort constants must pin the
// store's CommentSort values, and the body contract's normalization and
// violation order need direct coverage (the handler-level tests exercise
// them through the endpoints).

func TestCommentSortConstantsPinStoreValues(t *testing.T) {
	t.Parallel()
	if sortTop != string(store.CommentSortTop) ||
		sortNewest != string(store.CommentSortNewest) ||
		sortOldest != string(store.CommentSortOldest) {
		t.Fatalf("handler sort constants drifted from the store: %q/%q/%q vs %q/%q/%q",
			sortTop, sortNewest, sortOldest,
			store.CommentSortTop, store.CommentSortNewest, store.CommentSortOldest)
	}
}

func TestValidateCommentBody(t *testing.T) {
	t.Parallel()

	valid := []struct {
		name, in, want string
	}{
		{"plain", "hello", "hello"},
		{"trimmed", "  hello  ", "hello"},
		{"crlf normalized", "a\r\nb", "a\nb"},
		{"lone cr normalized", "a\rb", "a\nb"},
		{"newlines preserved", "a\n\nb", "a\n\nb"},
		{"tab kept inside", "a\tb", "a\tb"},
		{"greek and emoji pass through", "γεια 🙂", "γεια 🙂"},
		{"exactly 5000 runes", strings.Repeat("α", 5000), strings.Repeat("α", 5000)},
	}
	for _, tc := range valid {
		t.Run("valid: "+tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest("POST", "/", nil)
			rec := httptest.NewRecorder()
			got, ok := validateCommentBody(rec, req, tc.in)
			if !ok || got != tc.want {
				t.Errorf("validateCommentBody(%q): got (%q, %v), want (%q, true)", tc.in, got, ok, tc.want)
			}
		})
	}

	invalid := []struct {
		name, in, code string
	}{
		{"empty", "", "required"},
		{"whitespace only", " \t\n ", "required"},
		{"too long", strings.Repeat("α", 5001), "maxLength"},
		{"nul", "a\x00b", "invalidFormat"},
		{"escape", "a\x1bb", "invalidFormat"},
		{"del", "a\x7fb", "invalidFormat"},
		{"c1 control", "a\u0085b", "invalidFormat"},
	}
	for _, tc := range invalid {
		t.Run("invalid: "+tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest("POST", "/", nil)
			rec := httptest.NewRecorder()
			if _, ok := validateCommentBody(rec, req, tc.in); ok {
				t.Errorf("accepted %q, want a %s violation", tc.in, tc.code)
				return
			}
			body := rec.Body.String()
			if !strings.Contains(body, `"field":"body"`) || !strings.Contains(body, `"code":"`+tc.code+`"`) {
				t.Errorf("violation %q missing in %s", tc.code, body)
			}
		})
	}
}

func TestIsCommentControlRune(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		r    rune
		want bool
	}{
		{"LF allowed", '\n', false},
		{"TAB allowed", '\t', false},
		{"CR is a control rune", '\r', true},
		{"NUL", '\x00', true},
		{"unit separator", '\x1f', true},
		{"DEL", '\x7f', true},
		{"NEL (C1)", '\u0085', true},
		{"C1 end", '\u009f', true},
		{"ASCII letter", 'a', false},
		{"Greek letter", 'α', false},
		{"emoji", '🙂', false},
		{"NBSP", '\u00a0', false}, // not a control character
		// Format characters outside the C0/C1/DEL set pass on purpose: they
		// render as text and can only reorder their own comment visually.
		{"zero-width space passes", '\u200b', false},
		{"right-to-left override passes", '\u202e', false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isCommentControlRune(tc.r); got != tc.want {
				t.Errorf("isCommentControlRune(%U): got %v, want %v", tc.r, got, tc.want)
			}
		})
	}
}
