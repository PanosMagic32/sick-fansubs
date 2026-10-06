package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestParseIfMatch pins the If-Match contract: exactly one header carrying
// exactly one canonical strong validator `"<int>"` (revision ≥ 1, no sign or
// leading zeros). Missing → 428; duplicate headers, weak validators, lists,
// whitespace inside the quotes, non-integers, and out-of-range values → 400.
func TestParseIfMatch(t *testing.T) {
	t.Parallel()

	const maxRevision = int64(9223372036854775807)
	cases := []struct {
		name     string
		headers  []string
		wantCode int // 0 means accepted
		wantRev  int64
	}{
		{"missing", nil, http.StatusPreconditionRequired, 0},
		{"exactly one strong validator", []string{`"1"`}, 0, 1},
		{"large revision", []string{`"9223372036854775807"`}, 0, maxRevision},
		{"two headers", []string{`"1"`, `"2"`}, http.StatusBadRequest, 0},
		{"weak validator", []string{`W/"1"`}, http.StatusBadRequest, 0},
		{"comma list", []string{`"1", "2"`}, http.StatusBadRequest, 0},
		{"inner whitespace", []string{`" 1 "`}, http.StatusBadRequest, 0},
		{"plus sign", []string{`"+1"`}, http.StatusBadRequest, 0},
		{"leading zeros", []string{`"007"`}, http.StatusBadRequest, 0},
		{"zero", []string{`"0"`}, http.StatusBadRequest, 0},
		{"negative", []string{`"-1"`}, http.StatusBadRequest, 0},
		{"not a number", []string{`"abc"`}, http.StatusBadRequest, 0},
		{"int64 overflow", []string{`"9223372036854775808"`}, http.StatusBadRequest, 0},
		{"unquoted", []string{`1`}, http.StatusBadRequest, 0},
		{"empty", []string{``}, http.StatusBadRequest, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPut, "/", nil)
			for _, h := range c.headers {
				req.Header.Add("If-Match", h)
			}
			rec := httptest.NewRecorder()

			rev, ok := parseIfMatch(rec, req)
			if c.wantCode == 0 {
				if !ok || rev != c.wantRev {
					t.Fatalf("headers %v: got (%d, %v), want (%d, true)", c.headers, rev, ok, c.wantRev)
				}
				return
			}
			if ok || rec.Code != c.wantCode {
				t.Fatalf("headers %v: got (ok=%v, status %d), want a %d written", c.headers, ok, rec.Code, c.wantCode)
			}
		})
	}
}
