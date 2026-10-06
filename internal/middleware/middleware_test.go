package middleware

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/store"
	"sick-fansubs/internal/store/storetest"
)

func openMiddlewareDB(t *testing.T) *sql.DB {
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
	return db
}

type testHandler struct {
	called bool
	fn     func(w http.ResponseWriter, r *http.Request)
}

func (h *testHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.called = true
	if h.fn != nil {
		h.fn(w, r)
	}
}

func TestRequestID_GeneratesID(t *testing.T) {
	t.Parallel()
	var capturedID string
	handler := &testHandler{
		fn: func(w http.ResponseWriter, r *http.Request) {
			capturedID = GetRequestID(r.Context())
		},
	}

	mw := RequestID()(handler)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if !handler.called {
		t.Fatal("handler was not called")
	}
	if len(capturedID) != 32 {
		t.Errorf("request ID length: got %d, want 32", len(capturedID))
	}
	if rec.Header().Get("X-Request-ID") != capturedID {
		t.Errorf("X-Request-ID header mismatch: header %q, context %q",
			rec.Header().Get("X-Request-ID"), capturedID)
	}
}

func TestRequestID_ScopesTheRequestLogger(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	var capturedID string
	inner := &testHandler{
		fn: func(w http.ResponseWriter, r *http.Request) {
			capturedID = GetRequestID(r.Context())
			logging.From(r.Context()).InfoContext(r.Context(), "probe")
		},
	}

	// The logger is attached OUTSIDE RequestID, which scopes whatever it finds
	// — the process default logger in production.
	chain := RequestID()(inner)
	mw := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chain.ServeHTTP(w, r.WithContext(logging.With(r.Context(), logger)))
	})
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &line); err != nil {
		t.Fatalf("decode log line %q: %v", buf.String(), err)
	}
	if line["requestId"] != capturedID {
		t.Errorf("log requestId = %v, want the context ID %q", line["requestId"], capturedID)
	}
	if rec.Header().Get("X-Request-ID") != capturedID {
		t.Errorf("header = %q, want the context ID %q", rec.Header().Get("X-Request-ID"), capturedID)
	}
}

func TestRequestID_UniquePerRequest(t *testing.T) {
	t.Parallel()
	ids := make(map[string]bool)
	mw := RequestID()(&testHandler{fn: func(w http.ResponseWriter, r *http.Request) {}})

	for range 10 {
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, req)
		id := rec.Header().Get("X-Request-ID")
		if ids[id] {
			t.Errorf("duplicate request ID: %q", id)
		}
		ids[id] = true
	}
}

func TestRequestID_IgnoresInboundHeader(t *testing.T) {
	t.Parallel()
	var capturedID string
	handler := &testHandler{
		fn: func(w http.ResponseWriter, r *http.Request) {
			capturedID = GetRequestID(r.Context())
		},
	}

	mw := RequestID()(handler)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", "attacker-controlled-value")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	// The response header must NOT echo the attacker's value.
	if rec.Header().Get("X-Request-ID") == "attacker-controlled-value" {
		t.Error("X-Request-ID response header must not echo inbound value")
	}
	// The context ID must be server-generated (32 hex chars), not the attacker's.
	if capturedID == "attacker-controlled-value" {
		t.Error("request ID in context must not be the inbound value")
	}
	if len(capturedID) != 32 {
		t.Errorf("request ID length: got %d, want 32", len(capturedID))
	}
}

func TestSession_NoCookie(t *testing.T) {
	t.Parallel()
	db := openMiddlewareDB(t)

	handler := &testHandler{
		fn: func(w http.ResponseWriter, r *http.Request) {
			if su := GetSession(r.Context()); su != nil {
				t.Error("expected nil session with no cookie")
			}
		},
	}

	mw := Session(db, false)(handler)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if !handler.called {
		t.Fatal("handler was not called")
	}
}

func TestSession_MalformedCookie(t *testing.T) {
	t.Parallel()
	db := openMiddlewareDB(t)

	handler := &testHandler{
		fn: func(w http.ResponseWriter, r *http.Request) {
			if su := GetSession(r.Context()); su != nil {
				t.Error("expected nil session with malformed cookie")
			}
		},
	}

	mw := Session(db, false)(handler)
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "sf_session", Value: "not-valid-base64!!!"})
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if !handler.called {
		t.Fatal("handler was not called")
	}
}

func TestSession_WrongLengthCookie(t *testing.T) {
	t.Parallel()
	db := openMiddlewareDB(t)

	handler := &testHandler{
		fn: func(w http.ResponseWriter, r *http.Request) {
			if su := GetSession(r.Context()); su != nil {
				t.Error("expected nil session with wrong-length cookie")
			}
		},
	}

	mw := Session(db, false)(handler)

	// A valid token is exactly 43 chars. Test with 44 chars.
	longValue := base64.RawURLEncoding.EncodeToString(make([]byte, 33)) // 44 chars
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "sf_session", Value: longValue})
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if !handler.called {
		t.Fatal("handler was not called")
	}
}

func TestSession_ValidSession(t *testing.T) {
	t.Parallel()
	db := openMiddlewareDB(t)

	mustCreateTestUser(t, db, "user01", "testuser", "test@example.com")
	_, encodedToken, digest := mustCreateTestToken(t)
	mustCreateTestSession(t, db, "sess01", "user01", digest)

	handler := &testHandler{
		fn: func(w http.ResponseWriter, r *http.Request) {
			su := GetSession(r.Context())
			if su == nil {
				t.Error("expected non-nil session")
				return
			}
			if su.Username != "testuser" {
				t.Errorf("username: got %q, want %q", su.Username, "testuser")
			}
		},
	}

	mw := Session(db, false)(handler)
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "sf_session", Value: encodedToken})
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if !handler.called {
		t.Fatal("handler was not called")
	}

	for _, c := range rec.Result().Cookies() {
		if c.Name == "sf_session" && c.Value == "" {
			t.Error("valid session cookie was incorrectly cleared")
		}
	}
}

func TestSession_ExpiredSession(t *testing.T) {
	t.Parallel()
	db := openMiddlewareDB(t)

	mustCreateTestUser(t, db, "user01", "testuser", "test@example.com")
	_, encodedToken, digest := mustCreateTestToken(t)
	err := store.CreateSession(t.Context(), db, store.CreateSessionParams{
		ID:          "sess_expired",
		UserID:      "user01",
		TokenDigest: digest,
		CSRF:        make([]byte, 32),
		AuthVersion: 1,
		CreatedAtMS: 1000,
		ExpiresAtMS: 2000,
	})
	if err != nil {
		t.Fatalf("create expired session: %v", err)
	}

	handler := &testHandler{
		fn: func(w http.ResponseWriter, r *http.Request) {
			if su := GetSession(r.Context()); su != nil {
				t.Error("expected nil session for expired token")
			}
		},
	}

	mw := Session(db, false)(handler)
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "sf_session", Value: encodedToken})
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if !handler.called {
		t.Fatal("handler was not called")
	}

	foundClear := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "sf_session" && c.MaxAge == -1 {
			foundClear = true
		}
	}
	if !foundClear {
		t.Error("expired session should set a deletion cookie")
	}
}

func TestSession_NotFound(t *testing.T) {
	t.Parallel()
	db := openMiddlewareDB(t)

	rawToken := make([]byte, 32)
	encodedToken := base64.RawURLEncoding.EncodeToString(rawToken)

	handler := &testHandler{
		fn: func(w http.ResponseWriter, r *http.Request) {
			if su := GetSession(r.Context()); su != nil {
				t.Error("expected nil session for unknown token")
			}
		},
	}

	mw := Session(db, false)(handler)
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "sf_session", Value: encodedToken})
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if !handler.called {
		t.Fatal("handler was not called")
	}
}

func TestCSRF_SafeMethodPasses(t *testing.T) {
	t.Parallel()
	handler := &testHandler{}
	mw := CSRF()(handler)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	if !handler.called {
		t.Fatal("GET should always pass CSRF check")
	}
}

func TestCSRF_NoSessionPasses(t *testing.T) {
	t.Parallel()
	handler := &testHandler{}
	mw := CSRF()(handler)
	req := httptest.NewRequest("POST", "/", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	if !handler.called {
		t.Fatal("POST without session should pass")
	}
}

func TestCSRF_MatchingToken(t *testing.T) {
	t.Parallel()
	csrf := makeTestCSRF(t)

	handler := &testHandler{}
	mw := CSRF()(handler)
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("X-CSRF-Token", base64.RawURLEncoding.EncodeToString(csrf))
	ctx := SetSession(req.Context(), &identity.SessionUser{CSRF: csrf})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if !handler.called {
		t.Fatal("matching CSRF token should pass")
	}
}

func TestCSRF_MismatchedToken(t *testing.T) {
	t.Parallel()
	csrf := makeTestCSRF(t)
	wrongCSRF := make([]byte, 32)
	for i := range wrongCSRF {
		wrongCSRF[i] = byte(255 - i)
	}

	handler := &testHandler{}
	mw := CSRF()(handler)
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("X-CSRF-Token", base64.RawURLEncoding.EncodeToString(wrongCSRF))
	ctx := SetSession(req.Context(), &identity.SessionUser{CSRF: csrf})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if handler.called {
		t.Fatal("mismatched CSRF token should be rejected")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusForbidden)
	}
	assertForbiddenBody(t, rec)
}

func TestCSRF_MissingHeader(t *testing.T) {
	t.Parallel()
	csrf := makeTestCSRF(t)

	handler := &testHandler{}
	mw := CSRF()(handler)
	req := httptest.NewRequest("POST", "/", nil)
	ctx := SetSession(req.Context(), &identity.SessionUser{CSRF: csrf})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if handler.called {
		t.Fatal("missing CSRF token should be rejected")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusForbidden)
	}
	assertForbiddenBody(t, rec)
}

func TestCSRF_WrongLengthClientToken(t *testing.T) {
	t.Parallel()
	csrf := makeTestCSRF(t)

	handler := &testHandler{}
	mw := CSRF()(handler)
	req := httptest.NewRequest("POST", "/", nil)
	// Encode a 31-byte token (wrong length) — decodes to 31 bytes, not 32.
	req.Header.Set("X-CSRF-Token", base64.RawURLEncoding.EncodeToString(make([]byte, 31)))
	ctx := SetSession(req.Context(), &identity.SessionUser{CSRF: csrf})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if handler.called {
		t.Fatal("wrong-length CSRF token should be rejected")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestCSRF_NilStoredToken(t *testing.T) {
	t.Parallel()
	handler := &testHandler{}
	mw := CSRF()(handler)
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("X-CSRF-Token", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	// Session with nil CSRF (defensive — shouldn't happen, but handle gracefully).
	ctx := SetSession(req.Context(), &identity.SessionUser{CSRF: nil})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if handler.called {
		t.Fatal("nil stored CSRF should be rejected")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestCSRF_WrongLengthStoredToken(t *testing.T) {
	t.Parallel()
	handler := &testHandler{}
	mw := CSRF()(handler)
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("X-CSRF-Token", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	// Stored CSRF with wrong length (defensive — shouldn't happen).
	ctx := SetSession(req.Context(), &identity.SessionUser{CSRF: make([]byte, 31)})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if handler.called {
		t.Fatal("wrong-length stored CSRF should be rejected")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestTrustedOrigin_SafeMethodPasses(t *testing.T) {
	t.Parallel()
	handler := &testHandler{}
	mw := TrustedOrigin("https://example.com")(handler)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	if !handler.called {
		t.Fatal("GET should always pass origin check")
	}
}

func TestTrustedOrigin_MatchingOrigin(t *testing.T) {
	t.Parallel()
	handler := &testHandler{}
	mw := TrustedOrigin("https://example.com")(handler)
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	if !handler.called {
		t.Fatal("matching origin should pass")
	}
}

func TestTrustedOrigin_CaseInsensitiveOrigin(t *testing.T) {
	t.Parallel()
	handler := &testHandler{}
	mw := TrustedOrigin("https://example.com")(handler)
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Origin", "HTTPS://EXAMPLE.COM")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	if !handler.called {
		t.Fatal("case-different origin should pass")
	}
}

func TestTrustedOrigin_RefererWithPath(t *testing.T) {
	t.Parallel()
	handler := &testHandler{}
	mw := TrustedOrigin("https://example.com")(handler)
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Referer", "https://example.com/some/page")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	if !handler.called {
		t.Fatal("Referer with path should be stripped and pass")
	}
}

func TestTrustedOrigin_RefererWithoutPath(t *testing.T) {
	t.Parallel()
	handler := &testHandler{}
	mw := TrustedOrigin("https://example.com")(handler)
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Referer", "https://example.com") // no trailing slash or path
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	if !handler.called {
		t.Fatal("Referer without path should pass")
	}
}

func TestTrustedOrigin_WrongOrigin(t *testing.T) {
	t.Parallel()
	handler := &testHandler{}
	mw := TrustedOrigin("https://example.com")(handler)
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Origin", "https://evil.com")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	if handler.called {
		t.Fatal("wrong origin should be rejected")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusForbidden)
	}
	assertForbiddenBody(t, rec)
}

func TestTrustedOrigin_MissingOrigin(t *testing.T) {
	t.Parallel()
	handler := &testHandler{}
	mw := TrustedOrigin("https://example.com")(handler)
	req := httptest.NewRequest("POST", "/", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	if handler.called {
		t.Fatal("missing origin should be rejected")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusForbidden)
	}
	assertForbiddenBody(t, rec)
}

func TestWriteForbidden_ResponseContract(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest("POST", "/", nil)
	ctx := SetRequestID(req.Context(), "abc123def456")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	writeForbidden(rec, req, "test reason")

	if rec.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusForbidden)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("Content-Type: got %q, want application/problem+json", ct)
	}

	cc := rec.Header().Get("Cache-Control")
	if cc != "no-store" {
		t.Errorf("Cache-Control: got %q, want no-store", cc)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}

	// Must include required problem fields.
	if body["type"] != "/problems/forbidden" {
		t.Errorf("type: got %q, want /problems/forbidden", body["type"])
	}
	if body["title"] != "Forbidden" {
		t.Errorf("title: got %q, want Forbidden", body["title"])
	}
	if body["status"] != float64(http.StatusForbidden) {
		t.Errorf("status: got %v, want %d", body["status"], http.StatusForbidden)
	}
	if body["requestId"] != "abc123def456" {
		t.Errorf("requestId: got %q, want abc123def456", body["requestId"])
	}

	// The opaque body must NOT leak the internal reason.
	if body["detail"] != nil {
		t.Errorf("body must not contain detail field (leaks internal state): got %v", body["detail"])
	}
}

func assertForbiddenBody(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("Content-Type: got %q, want application/problem+json", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["type"] != "/problems/forbidden" {
		t.Errorf("type: got %q, want /problems/forbidden", body["type"])
	}
}

func makeTestCSRF(t *testing.T) []byte {
	t.Helper()
	csrf := make([]byte, 32)
	for i := range csrf {
		csrf[i] = byte(i)
	}
	return csrf
}

func mustCreateTestUser(t *testing.T, db *sql.DB, id, usernameCanon, email string) {
	t.Helper()
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:       id,
		Username: usernameCanon,
		Email:    email,
	})
}

func mustCreateTestToken(t *testing.T) (raw []byte, encoded string, digest []byte) {
	t.Helper()
	raw = make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i)
	}
	encoded = base64.RawURLEncoding.EncodeToString(raw)
	h := sha256.Sum256(raw)
	digest = h[:]
	return
}

func mustCreateTestSession(t *testing.T, db *sql.DB, sessionID, userID string, digest []byte) {
	t.Helper()
	err := store.CreateSession(t.Context(), db, store.CreateSessionParams{
		ID:          sessionID,
		UserID:      userID,
		TokenDigest: digest,
		CSRF:        make([]byte, 32),
		AuthVersion: 1,
		CreatedAtMS: 1_000_000_000_000,
		ExpiresAtMS: 9_000_000_000_000,
	})
	if err != nil {
		t.Fatalf("mustCreateTestSession: %v", err)
	}
}

// TestSession_DuplicateCookiesRejected pins the rule that duplicate
// sf_session cookies are rejected rather than resolved by picking the first.
// The request proceeds as unauthenticated (no session oracle).
//
// The cookie is a real valid session token sent twice: a first-match resolver
// (r.Cookie) would authenticate this request, so the case is a live control
// for the ambiguity rule.
func TestSession_DuplicateCookiesRejected(t *testing.T) {
	t.Parallel()
	db := openMiddlewareDB(t)

	mustCreateTestUser(t, db, "user01", "dupuser", "dup@example.com")
	_, encodedToken, digest := mustCreateTestToken(t)
	mustCreateTestSession(t, db, "sess01", "user01", digest)

	handler := &testHandler{
		fn: func(w http.ResponseWriter, r *http.Request) {
			if su := GetSession(r.Context()); su != nil {
				t.Error("expected nil session for duplicate cookies")
			}
		},
	}

	mw := Session(db, false)(handler)
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: encodedToken})
	req.AddCookie(&http.Cookie{Name: CookieName, Value: encodedToken})
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if !handler.called {
		t.Fatal("request should reach the handler as unauthenticated")
	}
}

// TestCSRF_DuplicateHeaderRejected pins rejection of ambiguous X-CSRF-Token
// values: two header lines, and a comma-joined pair in one line, must 403 —
// never be resolved by taking the first.
func TestCSRF_DuplicateHeaderRejected(t *testing.T) {
	t.Parallel()
	csrf := makeTestCSRF(t)
	tok := base64.RawURLEncoding.EncodeToString(csrf)

	tests := []struct {
		name   string
		values []string
	}{
		{"two header lines", []string{tok, tok}},
		{"comma-joined pair", []string{tok + "," + tok}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &testHandler{}
			mw := CSRF()(handler)
			req := httptest.NewRequest("POST", "/", nil)
			for _, v := range tt.values {
				req.Header.Add("X-CSRF-Token", v)
			}
			ctx := SetSession(req.Context(), &identity.SessionUser{CSRF: csrf})
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()
			mw.ServeHTTP(rec, req)

			if handler.called {
				t.Fatal("ambiguous CSRF header must be rejected before the handler")
			}
			if rec.Code != http.StatusForbidden {
				t.Errorf("got %d, want 403 for duplicate CSRF tokens", rec.Code)
			}
		})
	}
}

// TestTrustedOrigin_DuplicateOriginRejected pins rejection of ambiguous
// Origin values on unsafe requests.
func TestTrustedOrigin_DuplicateOriginRejected(t *testing.T) {
	t.Parallel()
	handler := &testHandler{}
	mw := TrustedOrigin("https://example.com")(handler)
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Add("Origin", "https://example.com")
	req.Header.Add("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if handler.called {
		t.Fatal("ambiguous origin must be rejected before the handler")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403 for duplicate Origin values", rec.Code)
	}
}

// TestTrustedOrigin_Edges pins the fallback shape: Origin is primary and a
// duplicate is rejected, Referer is consulted only without Origin and reduced
// to its origin, and null or trailing-dot hosts never match.
func TestTrustedOrigin_Edges(t *testing.T) {
	t.Parallel()
	const trusted = "http://localhost:5173"
	tests := []struct {
		name     string
		origins  []string
		referers []string
		wantCall bool
	}{
		{"duplicate referer rejected", nil, []string{trusted + "/a", trusted + "/b"}, false},
		{"null origin rejected", []string{"null"}, nil, false},
		{"trailing-dot host rejected", []string{"http://localhost.:5173"}, nil, false},
		{"empty origin falls back to referer", []string{""}, []string{trusted + "/some/path?q=1#frag"}, true},
		{"origin wins over an untrusted referer", []string{trusted}, []string{"http://evil.example/"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &testHandler{}
			mw := TrustedOrigin(trusted)(handler)
			req := httptest.NewRequest("POST", "/", nil)
			for _, origin := range tt.origins {
				req.Header.Add("Origin", origin)
			}
			for _, referer := range tt.referers {
				req.Header.Add("Referer", referer)
			}
			rec := httptest.NewRecorder()
			mw.ServeHTTP(rec, req)

			if handler.called != tt.wantCall {
				t.Fatalf("called = %t, want %t (status %d, body %q)", handler.called, tt.wantCall, rec.Code, rec.Body.String())
			}
			if !tt.wantCall && rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403", rec.Code)
			}
		})
	}
}

// TestCSRF_RejectionsLogTheDistinguishingDetail pins each guard branch by the
// warn detail it writes: the client shape check, the stored shape check, and
// the comparison stay distinguishable in the log while the wire body stays
// opaque.
func TestCSRF_RejectionsLogTheDistinguishingDetail(t *testing.T) {
	t.Parallel()
	zeroHeader := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	nonzero := make([]byte, 32)
	nonzero[0] = 1

	tests := []struct {
		name    string
		header  string
		stored  []byte
		wantLog string
	}{
		{"wrong-length header", base64.RawURLEncoding.EncodeToString(make([]byte, 31)), nonzero, "invalid CSRF token format"},
		{"wrong-length stored token", zeroHeader, nonzero[:31], "invalid stored CSRF token"},
		{"right-length mismatch", zeroHeader, nonzero, "CSRF token mismatch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))

			mw := CSRF()(&testHandler{})
			req := httptest.NewRequest("POST", "/", nil)
			req.Header.Set("X-CSRF-Token", tt.header)
			req = req.WithContext(logging.With(req.Context(), logger))
			req = req.WithContext(SetSession(req.Context(), &identity.SessionUser{CSRF: tt.stored}))
			rec := httptest.NewRecorder()
			mw.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", rec.Code)
			}
			var line map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &line); err != nil {
				t.Fatalf("decode log line %q: %v", buf.String(), err)
			}
			if line["detail"] != tt.wantLog {
				t.Errorf("log detail = %v, want %q", line["detail"], tt.wantLog)
			}
		})
	}
}

// TestSession_StoreFailureIsUnauthenticatedAndLogged pins the fail-safe branch:
// a session lookup error serves the request unauthenticated and records one
// error line naming the failure.
func TestSession_StoreFailureIsUnauthenticatedAndLogged(t *testing.T) {
	t.Parallel()
	db := openMiddlewareDB(t)

	mustCreateTestUser(t, db, "user01", "testuser", "test@example.com")
	_, encodedToken, digest := mustCreateTestToken(t)
	mustCreateTestSession(t, db, "sess01", "user01", digest)
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	handler := &testHandler{
		fn: func(w http.ResponseWriter, r *http.Request) {
			if su := GetSession(r.Context()); su != nil {
				t.Error("expected nil session after a store failure")
			}
		},
	}

	mw := Session(db, false)(handler)
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: encodedToken})
	req = req.WithContext(logging.With(req.Context(), logger))
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if !handler.called {
		t.Fatal("request must reach the handler unauthenticated after a store failure")
	}
	if got := strings.Count(buf.String(), "session lookup failed"); got != 1 {
		t.Errorf("session lookup failed records = %d, want 1 (log %q)", got, buf.String())
	}
}

// TestRequestID_ReusesAnExistingContextID pins nested-chain idempotence: a
// chain wrapped inside another reuses the ID already in the context instead of
// minting a second identity for one request.
func TestRequestID_ReusesAnExistingContextID(t *testing.T) {
	t.Parallel()
	const pinned = "0123456789abcdef0123456789abcdef"

	handler := &testHandler{
		fn: func(w http.ResponseWriter, r *http.Request) {
			if got := GetRequestID(r.Context()); got != pinned {
				t.Errorf("context ID = %q, want the reused %q", got, pinned)
			}
		},
	}

	mw := RequestID()(handler)
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(SetRequestID(req.Context(), pinned))
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") != pinned {
		t.Errorf("X-Request-ID = %q, want the reused %q", rec.Header().Get("X-Request-ID"), pinned)
	}
}
