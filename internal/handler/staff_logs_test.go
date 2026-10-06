package handler_test

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/middleware"
)

// Staff logs handler tests: the super-admin
// floor, the level/substring/limit contract, the items-only envelope, and the
// degraded-input behavior.

// logsBody mirrors the wire contract in docs/api/openapi.yaml.
type logsBody struct {
	Items []struct {
		Time   string         `json:"time"`
		Level  string         `json:"level"`
		Msg    string         `json:"msg"`
		Fields map[string]any `json:"fields"`
	} `json:"items"`
}

// setupStaffLogsHandler creates a fresh migrated database and the logs handler
// behind the session chain at GET /api/v1/staff/logs, reading logDir.
func setupStaffLogsHandler(t *testing.T, logDir string) (*sql.DB, http.Handler) {
	t.Helper()

	db, err := database.Open(database.Config{DataDir: testDataDir(t)})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := database.Apply(db); err != nil {
		db.Close()
		t.Fatalf("apply migrations: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/staff/logs",
		handler.StaffLogs(logDir))

	var h http.Handler = mux
	h = middleware.Session(db, false)(h)
	h = middleware.TrustedOrigin("http://localhost:5173")(h)
	h = middleware.RequestID()(h)

	return db, h
}

// seedLogSink writes genuine slog records through the real file handler, so
// the writer's format and the reader's parser stay pinned to each other.
func seedLogSink(t *testing.T, dir string) {
	t.Helper()

	w, err := logging.NewRotatingWriter(dir, logging.DefaultMaxBytes, logging.DefaultMaxFiles, nil)
	if err != nil {
		t.Fatalf("new rotating writer: %v", err)
	}
	t.Cleanup(func() { w.Close() })

	logger := slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
	logger.Debug("debug noise")
	logger.Info("info line", "requestId", "r-info")
	logger.Warn("warn line", "requestId", "r-warn")
	logger.Error("error line")
}

func getStaffLogs(h http.Handler, cookie, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/staff/logs"+query, nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func mustSuperAdminSession(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	mustCreateStaffUser(t, db, id, "Super", "super-admin", "active", 3_000)
	return mustCreateStaffSession(t, db, id)
}

// TestStaffLogs_DefaultIsWarnAndAbove pins the default view and the wire
// shape: warn+error, newest first, the level lower-cased, and each record's
// extra attributes carried through untouched.
func TestStaffLogs_DefaultIsWarnAndAbove(t *testing.T) {
	t.Parallel()

	dir := testDataDir(t)
	seedLogSink(t, dir)
	db, h := setupStaffLogsHandler(t, dir)
	cookie := mustSuperAdminSession(t, db, "s1")

	rec := getStaffLogs(h, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID missing — RequestID middleware not applied")
	}

	var body logsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v (body %q)", err, rec.Body.String())
	}
	if len(body.Items) != 2 {
		t.Fatalf("items = %d (%v), want warn and error only", len(body.Items), body.Items)
	}
	if body.Items[0].Msg != "error line" || body.Items[1].Msg != "warn line" {
		t.Errorf("items = %q, %q — want newest first", body.Items[0].Msg, body.Items[1].Msg)
	}
	if body.Items[0].Level != "error" || body.Items[1].Level != "warn" {
		t.Errorf("levels = %q, %q — want the lower-cased level names", body.Items[0].Level, body.Items[1].Level)
	}
	if body.Items[1].Fields["requestId"] != "r-warn" {
		t.Errorf("fields.requestId = %v, want r-warn", body.Items[1].Fields["requestId"])
	}
	if body.Items[0].Time == "" {
		t.Error("time missing from the record")
	} else if _, err := time.Parse("2006-01-02T15:04:05.000Z07:00", body.Items[0].Time); err != nil {
		t.Errorf("time = %q, want the canonical millisecond instant (%v)", body.Items[0].Time, err)
	}
}

// TestStaffLogs_LevelFilter pins that `level` is a minimum severity.
func TestStaffLogs_LevelFilter(t *testing.T) {
	t.Parallel()

	dir := testDataDir(t)
	seedLogSink(t, dir)
	db, h := setupStaffLogsHandler(t, dir)
	cookie := mustSuperAdminSession(t, db, "s1")

	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"?level=error", []string{"error line"}},
		{"?level=warn", []string{"error line", "warn line"}},
		{"?level=info", []string{"error line", "warn line", "info line"}},
		{"?level=debug", []string{"error line", "warn line", "info line", "debug noise"}},
		{"?level=INFO", []string{"error line", "warn line", "info line"}},
	} {
		rec := getStaffLogs(h, cookie, tc.query)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %q: status = %d, want 200", tc.query, rec.Code)
			continue
		}
		var body logsBody
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("GET %q: decode body: %v", tc.query, err)
		}
		var got []string
		for _, item := range body.Items {
			got = append(got, item.Msg)
		}
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("GET %q: items = %v, want %v", tc.query, got, tc.want)
		}
	}
}

// TestStaffLogs_QueryAndLimit pins the substring filter (over the raw line,
// so it reaches field values) and the last-N cap.
func TestStaffLogs_QueryAndLimit(t *testing.T) {
	t.Parallel()

	dir := testDataDir(t)
	seedLogSink(t, dir)
	db, h := setupStaffLogsHandler(t, dir)
	cookie := mustSuperAdminSession(t, db, "s1")

	var body logsBody
	rec := getStaffLogs(h, cookie, "?q=r-info")
	if rec.Code != http.StatusOK {
		t.Fatalf("q status = %d, want 200", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Items) != 0 {
		t.Errorf("q=r-info (below the default warn floor) = %d items, want 0", len(body.Items))
	}

	rec = getStaffLogs(h, cookie, "?q=R-INFO&level=info")
	if rec.Code != http.StatusOK {
		t.Fatalf("q+level status = %d, want 200", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].Msg != "info line" {
		t.Errorf("q=R-INFO&level=info = %v, want the info line (case-insensitive)", body.Items)
	}

	rec = getStaffLogs(h, cookie, "?limit=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("limit status = %d, want 200", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].Msg != "error line" {
		t.Errorf("limit=1 = %v, want the newest match only", body.Items)
	}
}

// TestStaffLogs_Gates pins the super-admin floor: anonymous → 401, and every
// below-floor role → 403 (an admin is NOT enough for this surface).
func TestStaffLogs_Gates(t *testing.T) {
	t.Parallel()

	dir := testDataDir(t)
	seedLogSink(t, dir)
	db, h := setupStaffLogsHandler(t, dir)
	mustCreateStaffUser(t, db, "mod1", "Mod", "moderator", "active", 3_000)
	mustCreateStaffUser(t, db, "admin1", "Admin", "admin", "active", 3_000)
	mustCreateStaffUser(t, db, "u1", "User One", "user", "active", 3_000)
	mustCreateStaffUser(t, db, "s1", "Super", "super-admin", "active", 3_000)

	cookies := map[string]string{
		"u1":     mustCreateStaffSession(t, db, "u1"),
		"mod1":   mustCreateStaffSession(t, db, "mod1"),
		"admin1": mustCreateStaffSession(t, db, "admin1"),
		"s1":     mustCreateStaffSession(t, db, "s1"),
	}

	for _, tc := range []struct {
		name   string
		cookie string
		want   int
	}{
		{"anonymous", "", http.StatusUnauthorized},
		{"role=user", cookies["u1"], http.StatusForbidden},
		{"moderator", cookies["mod1"], http.StatusForbidden},
		{"admin", cookies["admin1"], http.StatusForbidden},
		{"super-admin", cookies["s1"], http.StatusOK},
	} {
		rec := getStaffLogs(h, tc.cookie, "")
		if rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d (body %q)", tc.name, rec.Code, tc.want, rec.Body.String())
		}
		if tc.want != http.StatusOK {
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
				t.Errorf("%s: Content-Type = %q, want a problem", tc.name, ct)
			}
		}
	}

	// The floor also precedes the QUERY parse: a below-floor request carrying a
	// parameter the endpoint does not allow is still a 403, not a 400.
	if rec := getStaffLogs(h, cookies["admin1"], "?unknown=1"); rec.Code != http.StatusForbidden {
		t.Errorf("admin with an unknown parameter: status = %d, want 403 (the floor answers first)", rec.Code)
	}
}

// TestStaffLogs_FloorPrecedesTheRead pins the ordering the metrics
// handler established: with the sink path broken, a below-floor request stays
// a 403 while the super-admin gets the 500 — proof that no file is touched
// before the capability check.
func TestStaffLogs_FloorPrecedesTheRead(t *testing.T) {
	t.Parallel()

	// A regular file where the log directory should be: every read of it fails
	// with something other than "not exist".
	shadow := filepath.Join(testDataDir(t), "not-a-directory")
	if err := os.WriteFile(shadow, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed shadow path: %v", err)
	}

	db, h := setupStaffLogsHandler(t, shadow)
	mustCreateStaffUser(t, db, "u1", "User One", "user", "active", 3_000)
	mustCreateStaffUser(t, db, "s1", "Super", "super-admin", "active", 3_000)

	if rec := getStaffLogs(h, mustCreateStaffSession(t, db, "u1"), ""); rec.Code != http.StatusForbidden {
		t.Errorf("role=user with a broken sink: status = %d, want 403 (the read must not run)", rec.Code)
	}
	if rec := getStaffLogs(h, mustCreateStaffSession(t, db, "s1"), ""); rec.Code != http.StatusInternalServerError {
		t.Errorf("super-admin with a broken sink: status = %d, want 500 (the read does run)", rec.Code)
	}
}

// TestStaffLogs_QueryParamContract pins 400 vs 422: a structurally bad query
// is a 400, a well-formed value outside the contract is a 422 violation.
func TestStaffLogs_QueryParamContract(t *testing.T) {
	t.Parallel()

	dir := testDataDir(t)
	seedLogSink(t, dir)
	db, h := setupStaffLogsHandler(t, dir)
	cookie := mustSuperAdminSession(t, db, "s1")

	for _, query := range []string{
		"?unknown=1",
		"?limit=1&limit=2",
		"?limit=abc",
		"?limit=",
		"?%zz=1",
	} {
		rec := getStaffLogs(h, cookie, query)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("GET %q: status = %d, want 400", query, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
			t.Errorf("GET %q: Content-Type = %q, want a problem", query, ct)
		}
	}

	for _, tc := range []struct {
		query string
		field string
	}{
		{"?level=bogus", "level"},
		{"?level=", "level"},
		{"?limit=0", "limit"},
		{"?limit=100000", "limit"},
		{"?q=" + strings.Repeat("a", 300), "q"},
	} {
		rec := getStaffLogs(h, cookie, tc.query)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("GET %q: status = %d, want 422", tc.query, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
			t.Errorf("GET %q: Content-Type = %q, want a problem", tc.query, ct)
		}
		if !strings.Contains(rec.Body.String(), `"field":"`+tc.field+`"`) {
			t.Errorf("GET %q: body %q, want a violation naming %q", tc.query, rec.Body.String(), tc.field)
		}
	}
}

// TestStaffLogs_NoSinkYetIsEmpty pins the fresh-install case: the directory
// does not exist, so the answer is an empty list — never an error.
func TestStaffLogs_NoSinkYetIsEmpty(t *testing.T) {
	t.Parallel()

	db, h := setupStaffLogsHandler(t, filepath.Join(testDataDir(t), "logs"))
	cookie := mustSuperAdminSession(t, db, "s1")

	rec := getStaffLogs(h, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"items":[]`) {
		t.Errorf("body = %q, want an empty items array (not null)", body)
	}
}

// TestStaffLogs_SkipsMalformedTail pins the degraded-input contract at the
// handler boundary: a partial trailing record is skipped, not served and not
// fatal.
func TestStaffLogs_SkipsMalformedTail(t *testing.T) {
	t.Parallel()

	dir := testDataDir(t)
	seedLogSink(t, dir)
	if err := os.WriteFile(filepath.Join(dir, logging.FileName),
		[]byte(`{"time":"2026-09-18T10:00:00.000Z","level":"ERROR","msg":"trunc`), 0o600); err != nil {
		t.Fatalf("truncate the sink: %v", err)
	}

	db, h := setupStaffLogsHandler(t, dir)
	cookie := mustSuperAdminSession(t, db, "s1")

	rec := getStaffLogs(h, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"items":[]`) {
		t.Errorf("body = %q, want the malformed line skipped", body)
	}
}
