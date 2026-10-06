package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sick-fansubs/internal/auth"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/store"
)

// problemBody is the decoded test view of a written problem document.
type problemBody struct {
	Type       string `json:"type"`
	Status     int    `json:"status"`
	Violations []struct {
		Field string `json:"field"`
		Code  string `json:"code"`
	} `json:"violations"`
}

// decodeProblem decodes a written problem document.
func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) problemBody {
	t.Helper()
	var body problemBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode problem body %q: %v", rec.Body.String(), err)
	}
	return body
}

// logRecords decodes the JSON log lines a capture buffer holds.
func logRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for line := range bytes.SplitSeq(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

// requestWithLogger returns a request whose context carries the capture logger,
// so the writers under test log into buf.
func requestWithLogger(buf *bytes.Buffer) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/blog-posts", nil)
	return req.WithContext(logging.With(req.Context(), slog.New(slog.NewJSONHandler(buf, nil))))
}

// TestWriteStoreError_MapsEverySentinel pins the store vocabulary's public
// outcomes and the masked 500 default, logged exactly once with the caller's
// operation name and cause.
func TestWriteStoreError_MapsEverySentinel(t *testing.T) {
	t.Parallel()

	type want struct {
		status   int
		problem  string
		field    string
		code     string
		wantLogs int
	}
	cases := []struct {
		name string
		err  error
		want want
	}{
		{"not found", store.ErrNotFound, want{http.StatusNotFound, "/problems/not-found", "", "", 0}},
		{"wrapped not found", fmt.Errorf("load: %w", store.ErrNotFound), want{http.StatusNotFound, "/problems/not-found", "", "", 0}},
		{"stale revision", store.ErrConflict, want{http.StatusPreconditionFailed, "/problems/precondition-failed", "", "", 0}},
		{"last active super-admin", store.ErrLastActiveSuperAdmin, want{http.StatusConflict, "/problems/auth/last-super-admin", "", "", 0}},
		{"not the author", store.ErrNotAuthor, want{http.StatusForbidden, "/problems/forbidden", "", "", 1}},
		{"invalid parent", store.ErrInvalidParent, want{http.StatusUnprocessableEntity, "/problems/validation", "parentId", "invalidValue", 1}},
		{"self heart", store.ErrSelfHeart, want{http.StatusUnprocessableEntity, "/problems/validation", "commentId", "invalidValue", 1}},
		{"slug taken", store.ErrSlugTaken, want{http.StatusUnprocessableEntity, "/problems/validation", "slug", "alreadyTaken", 1}},
		{"unmapped sentinel", store.ErrDuplicate, want{http.StatusInternalServerError, "/problems/internal-error", "", "", 1}},
		{"non-sentinel", errors.New("database is locked"), want{http.StatusInternalServerError, "/problems/internal-error", "", "", 1}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			rec := httptest.NewRecorder()

			status := writeStoreError(rec, requestWithLogger(&buf), tc.err, "blog update", "error", tc.err)

			if status != tc.want.status || rec.Code != tc.want.status {
				t.Fatalf("status for %v = %d/%d, want %d", tc.err, status, rec.Code, tc.want.status)
			}
			body := decodeProblem(t, rec)
			if body.Type != tc.want.problem || body.Status != tc.want.status {
				t.Errorf("problem for %v = %+v, want %q at %d", tc.err, body, tc.want.problem, tc.want.status)
			}
			if tc.want.field != "" {
				if len(body.Violations) != 1 || body.Violations[0].Field != tc.want.field || body.Violations[0].Code != tc.want.code {
					t.Errorf("violations for %v = %+v, want %s:%s", tc.err, body.Violations, tc.want.field, tc.want.code)
				}
			}
			if rec.Body.String() != "" && strings.Contains(rec.Body.String(), "database is locked") {
				t.Errorf("body for %v leaks the cause: %s", tc.err, rec.Body.String())
			}
			lines := logRecords(t, &buf)
			if len(lines) != tc.want.wantLogs {
				t.Fatalf("log lines for %v = %d, want %d:\n%s", tc.err, len(lines), tc.want.wantLogs, buf.String())
			}
			if len(lines) == 1 && tc.want.status == http.StatusInternalServerError {
				if lines[0]["msg"] != "blog update failed" || lines[0]["error"] != tc.err.Error() {
					t.Errorf("record for %v = %v, want the operation and cause", tc.err, lines[0])
				}
			}
		})
	}
}

// TestWriteStoreAccountError_NotFoundIsTheDeadSessionOutcome pins the
// viewer-account variant: a vanished row answers the generic 401, while every
// other sentinel keeps the shared mapping.
func TestWriteStoreAccountError_NotFoundIsTheDeadSessionOutcome(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	if status := writeStoreAccountError(rec, requestWithLogger(&bytes.Buffer{}), store.ErrNotFound, "profile lookup"); status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", status)
	}
	if body := decodeProblem(t, rec); body.Type != "/problems/auth/invalid-credentials" {
		t.Errorf("problem = %q, want the generic invalid-credentials problem", body.Type)
	}

	rec = httptest.NewRecorder()
	if status := writeStoreAccountError(rec, requestWithLogger(&bytes.Buffer{}), store.ErrConflict, "profile lookup"); status != http.StatusPreconditionFailed {
		t.Errorf("status = %d, want the shared 412 mapping", status)
	}
}

// TestWriteAuthError_MapsEverySentinel pins the auth vocabulary's public
// outcomes: the generic 401, the two taken-field violations, the race 409,
// the two masked token problems, and the structured validation extraction.
func TestWriteAuthError_MapsEverySentinel(t *testing.T) {
	t.Parallel()

	fieldErr := &auth.FieldError{Field: "email", Code: "invalidFormat", Message: "invalid email format"}
	bareValidation := fmt.Errorf("wrapped: %w", auth.ErrValidation)

	cases := []struct {
		name      string
		err       error
		status    int
		problem   string
		field     string
		code      string
		wantCause bool
	}{
		{"invalid credentials", auth.ErrInvalidCredentials, http.StatusUnauthorized, "/problems/auth/invalid-credentials", "", "", false},
		{"username taken", auth.ErrUsernameTaken, http.StatusUnprocessableEntity, "/problems/validation", "username", "alreadyTaken", false},
		{"email taken", auth.ErrEmailTaken, http.StatusUnprocessableEntity, "/problems/validation", "email", "alreadyTaken", false},
		{"race lost", auth.ErrConflict, http.StatusConflict, "/problems/conflict", "", "", false},
		{"reset token", auth.ErrResetTokenInvalid, http.StatusUnprocessableEntity, "/problems/auth/reset-token-invalid", "", "", false},
		{"verification token", auth.ErrVerificationTokenInvalid, http.StatusUnprocessableEntity, "/problems/auth/verification-token-invalid", "", "", false},
		{"field validation", fieldErr, http.StatusUnprocessableEntity, "/problems/validation", "email", "invalidFormat", true},
		{"bare validation", bareValidation, http.StatusUnprocessableEntity, "/problems/validation", "general", "invalid", true},
		{"unmapped", errors.New("smtp refused"), http.StatusInternalServerError, "/problems/internal-error", "", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			rec := httptest.NewRecorder()

			status := writeAuthError(rec, requestWithLogger(&buf), tc.err, "register", "error", tc.err)

			if status != tc.status || rec.Code != tc.status {
				t.Fatalf("status = %d/%d for %v, want %d", status, rec.Code, tc.err, tc.status)
			}
			body := decodeProblem(t, rec)
			if body.Type != tc.problem {
				t.Errorf("problem for %v = %q, want %q", tc.err, body.Type, tc.problem)
			}
			if tc.field != "" {
				if len(body.Violations) != 1 || body.Violations[0].Field != tc.field || body.Violations[0].Code != tc.code {
					t.Errorf("violations for %v = %+v, want %s:%s", tc.err, body.Violations, tc.field, tc.code)
				}
			}
			// Every outcome logs at most one record; the validation outcome
			// carries the cause on that same record.
			lines := logRecords(t, &buf)
			if len(lines) > 1 {
				t.Fatalf("log lines for %v = %d, want at most 1:\n%s", tc.err, len(lines), buf.String())
			}
			if !tc.wantCause {
				return
			}
			if len(lines) != 1 {
				t.Fatalf("cause of %v not logged: log lines = %d, want 1", tc.err, len(lines))
			}
			if lines[0]["msg"] != "validation failed" || lines[0]["error"] != tc.err.Error() {
				t.Errorf("record for %v = %v, want the violation record carrying the cause", tc.err, lines[0])
			}
		})
	}
}

// TestClassifyDecodeError_PinsTheLiveDecoder is the exposure tripwire for the
// tree's one decode-error text match: it drives the real encoding/json decoder
// and pins each failure to its body-vocabulary sentinel.
func TestClassifyDecodeError_PinsTheLiveDecoder(t *testing.T) {
	t.Parallel()

	type payload struct {
		Title string `json:"title"`
	}
	decode := func(body string) error {
		dec := json.NewDecoder(strings.NewReader(body))
		dec.DisallowUnknownFields()
		var dst payload
		return dec.Decode(&dst)
	}

	cases := []struct {
		name string
		body string
		want error
	}{
		{"unknown field", `{"title":"x","nope":1}`, errUnknownField},
		{"empty body", ``, errEmptyBody},
		{"truncated body", `{"title":`, errEmptyBody},
		{"type mismatch", `{"title":1}`, errUnexpectedFieldType},
		{"syntax error", `{"title": ]}`, errInvalidJSON},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := decode(tc.body)
			if err == nil {
				t.Fatalf("body %q decoded without error", tc.body)
			}
			if got := classifyDecodeError(err); !errors.Is(got, tc.want) {
				t.Errorf("classifyDecodeError(%v) = %v, want %v", err, got, tc.want)
			}
		})
	}
}

// TestReadJSONLimit_DuplicateMemberLastWins pins the recorded decoder contract
// through the PRODUCTION body reader: a repeated object member name is accepted
// and the LAST value wins. RFC 8259 §4 makes uniqueness a producer obligation,
// so the wire contract records the acceptance (docs/patterns/go/errors.md); a
// decoder switch that rejects duplicates fails here instead of silently
// changing the contract.
func TestReadJSONLimit_DuplicateMemberLastWins(t *testing.T) {
	t.Parallel()

	var dst struct {
		Status string `json:"status"`
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"status":"first","status":"last"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	if err := readJSONLimit(rec, req, &dst, maxAuthBodySize); err != nil {
		t.Fatalf("readJSONLimit rejected a duplicate member: %v", err)
	}
	if dst.Status != "last" {
		t.Errorf("status = %q, want %q (last value wins)", dst.Status, "last")
	}
}

// TestWriteTooManyRequests_LogsOnceWithCallerFields pins the one-log rule for
// rate-limit rejections: the writer logs the caller's bucket fields plus the
// window, on a single record, and sets Retry-After.
func TestWriteTooManyRequests_LogsOnceWithCallerFields(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	rec := httptest.NewRecorder()
	writeTooManyRequests(rec, requestWithLogger(&buf), 90*time.Second,
		"bucket", "comment-create", "userId", "u1")

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "90" {
		t.Errorf("Retry-After = %q, want 90", got)
	}
	lines := logRecords(t, &buf)
	if len(lines) != 1 {
		t.Fatalf("log lines = %d, want exactly 1:\n%s", len(lines), buf.String())
	}
	line := lines[0]
	if line["msg"] != "rate limit exceeded" || line["bucket"] != "comment-create" ||
		line["userId"] != "u1" || line["retryAfter"] != float64(90) {
		t.Errorf("record = %v, want the caller fields plus retryAfter", line)
	}
}

// TestWriteValidationErrors_LogsOnceWithTheCause pins the one-record shape of
// a validation rejection: the violations, plus the cause a caller passed.
func TestWriteValidationErrors_LogsOnceWithTheCause(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	rec := httptest.NewRecorder()
	cause := errors.New("validator exploded")
	writeValidationErrors(rec, requestWithLogger(&buf),
		[]Violation{{Field: "general", Code: "invalid"}}, "error", cause)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	lines := logRecords(t, &buf)
	if len(lines) != 1 {
		t.Fatalf("log lines = %d, want exactly 1:\n%s", len(lines), buf.String())
	}
	if lines[0]["msg"] != "validation failed" || lines[0]["error"] != cause.Error() {
		t.Errorf("record = %v, want the violation record carrying the cause", lines[0])
	}
}
