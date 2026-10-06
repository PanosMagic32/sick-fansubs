package handler_test

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"sick-fansubs/internal/auth"
	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/mail"
	"sick-fansubs/internal/middleware"
)

// logContext wraps a test handler so every request carries logger — the value
// the RequestID middleware installs in production. The logger keeps the
// request ID the context already holds, so a record logged inside the request
// carries the same requestId field production stamps. A nil logger becomes a
// discard logger, so a test that asserts no log output still sees none.
func logContext(logger *slog.Logger, next http.Handler) http.Handler {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scoped := logger
		if id := middleware.GetRequestID(r.Context()); id != "" {
			scoped = logger.With("requestId", id)
		}
		next.ServeHTTP(w, r.WithContext(logging.With(r.Context(), scoped)))
	})
}

// testDataDir returns an owner-only temporary directory for tests that open a
// database: database.Open refuses a data directory with group or other access,
// and t.TempDir can inherit wider bits from the environment.
func testDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod test data dir: %v", err)
	}
	return dir
}

// setupAuth creates a fresh SQLite database with migrations, a Service, and an
// Auth. Returns the handler, the database handle (for direct store access in
// tests), and the ServeMux with middleware.
func setupAuth(t *testing.T) (*handler.Auth, *sql.DB, http.Handler) {
	return setupAuthWithLogger(t, nil)
}

// setupAuthWithLogger is setupAuth with an injectable logger for audit-event
// tests (nil discards the output; the helper re-stamps the request ID).
func setupAuthWithLogger(t *testing.T, logger *slog.Logger) (*handler.Auth, *sql.DB, http.Handler) {
	t.Helper()

	dir := testDataDir(t)
	db, err := database.Open(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := database.Apply(db); err != nil {
		db.Close()
		t.Fatalf("apply migrations: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	auth := auth.New(db, mail.LogLink{}, "")
	// Real limiters with generous bounds. Production wiring always passes
	// non-nil limiters — a nil limiter would silently disable abuse
	// protection, so tests model the real wiring instead of a degenerate one.
	generous := middleware.NewRateLimiter(1000, 15*time.Minute, time.Minute)
	h := handler.NewAuth(auth, false, generous, generous, generous, generous) // secure=false for test (HTTP)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/sign-in", h.HandleSignIn)
	mux.HandleFunc("GET /api/v1/auth/session", h.HandleSession)
	mux.HandleFunc("POST /api/v1/auth/sign-out", h.HandleSignOut)
	mux.HandleFunc("POST /api/v1/auth/sign-out-all", h.HandleSignOutAll)
	mux.HandleFunc("POST /api/v1/auth/register", h.HandleRegister)
	mux.HandleFunc("PUT /api/v1/auth/password", h.HandlePasswordChange)
	mux.HandleFunc("POST /api/v1/auth/forgot-password", h.HandleForgotPassword)
	mux.HandleFunc("POST /api/v1/auth/reset-password", h.HandleResetPassword)

	return h, db, logContext(logger, mux)
}

// withMiddleware wraps a mux handler in the standard middleware chain:
// RequestID → TrustedOrigin → Session → CSRF.
func withMiddleware(mux http.Handler, db *sql.DB) http.Handler {
	var h http.Handler = mux
	h = middleware.CSRF()(h)
	h = middleware.Session(db, false)(h)
	h = middleware.TrustedOrigin("http://localhost:5173")(h)
	h = middleware.RequestID()(h)
	return h
}

// setOrigin sets the Origin header on a request to the test trusted origin.
func setOrigin(r *http.Request) {
	r.Header.Set("Origin", "http://localhost:5173")
}

// setCSRF sets the X-CSRF-Token header on a request.
func setCSRF(r *http.Request, token string) {
	r.Header.Set("X-CSRF-Token", token)
}

// jsonBody creates a request body reader from a Go value and sets Content-Type.
func jsonBody(t *testing.T, v any) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	return bytes.NewReader(b)
}

// decodeJSON decodes a JSON response body into dst.
func decodeJSON(t *testing.T, body *bytes.Buffer, dst any) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(dst); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
}

// createUser signs up a new user via the register endpoint and returns
// the session cookie and CSRF token.
func createUser(t *testing.T, mux http.Handler, username, email, password string) (cookie string, csrfToken string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		jsonBody(t, map[string]string{
			"username": username,
			"email":    email,
			"password": password,
		}))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("register failed: status %d, body: %s", rec.Code, rec.Body.String())
	}

	for _, c := range rec.Result().Cookies() {
		if c.Name == "sf_session" {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("register response did not set sf_session cookie")
	}

	var resp struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeJSON(t, rec.Body, &resp)
	csrfToken = resp.CSRFToken

	return cookie, csrfToken
}

// auditRecord is the structured subset of one JSON audit log line.
type auditRecord struct {
	Event      string `json:"event"`
	Result     string `json:"result"`
	ActorID    string `json:"actorId"`
	TargetID   string `json:"targetId"`
	TargetRole string `json:"targetRole"`
	RequestID  string `json:"requestId"`
	RemoteAddr string `json:"remoteAddr"`
}

// parseAuditEvents decodes every JSON log line captured by the injected test
// logger and returns only the audit records (msg == "audit").
func parseAuditEvents(t *testing.T, buf *bytes.Buffer) []auditRecord {
	t.Helper()
	var records []auditRecord
	scanner := bufio.NewScanner(buf)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec struct {
			Msg string `json:"msg"`
			auditRecord
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		if rec.Msg == "audit" {
			records = append(records, rec.auditRecord)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan log buffer: %v", err)
	}
	return records
}

// resetFakeSender records emails and scripts failures.
type resetFakeSender struct {
	mu   sync.Mutex
	sent []resetSentMail
	err  error
}

type resetSentMail struct {
	to, subject, body string
}

// resetLinkToken extracts the ?token= value from a reset-link line.
func resetLinkToken(link string) string {
	_, after, ok := strings.Cut(link, "?token=")
	if !ok {
		return ""
	}
	token, _, _ := strings.Cut(after, "\n")
	return token
}

func (f *resetFakeSender) Send(_ context.Context, to, subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, resetSentMail{to: to, subject: subject, body: body})
	return nil
}

// resetSubject is the reset-email subject — the discriminator between the
// reset and verification mails the same sender carries.
var resetSubject, _ = mail.ResetEmail("http://localhost:3000")
var verifySubject, _ = mail.VerifyEmail("http://localhost:3000")

// resetMails returns only the RESET emails the fake recorded. Registration
// now also sends a verification email through the same sender,
// so the reset tests must count/index reset mails, not every mail.
func (f *resetFakeSender) resetMails() []resetSentMail {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []resetSentMail
	for _, m := range f.sent {
		if m.subject == resetSubject {
			out = append(out, m)
		}
	}
	return out
}

// verifyMails returns only the VERIFICATION emails the fake recorded —
// the mirror discriminator for the verification tests.
func (f *resetFakeSender) verifyMails() []resetSentMail {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []resetSentMail
	for _, m := range f.sent {
		if m.subject == verifySubject {
			out = append(out, m)
		}
	}
	return out
}

// setupResetHandler builds a handler wired to a fake sender, so tests can
// read the emailed link without SMTP.
func setupResetHandler(t *testing.T, sender *resetFakeSender) (*handler.Auth, *sql.DB, *http.ServeMux) {
	t.Helper()
	dir := testDataDir(t)
	db, err := database.Open(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := database.Apply(db); err != nil {
		db.Close()
		t.Fatalf("apply migrations: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	svc := auth.New(db, sender, "http://localhost:3000")
	generous := middleware.NewRateLimiter(1000, 15*time.Minute, time.Minute)
	h := handler.NewAuth(svc, false, generous, generous, generous, generous)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/sign-in", h.HandleSignIn)
	mux.HandleFunc("POST /api/v1/auth/register", h.HandleRegister)
	mux.HandleFunc("POST /api/v1/auth/forgot-password", h.HandleForgotPassword)
	mux.HandleFunc("POST /api/v1/auth/reset-password", h.HandleResetPassword)
	mux.HandleFunc("POST /api/v1/auth/verify-email", h.HandleVerifyEmail)
	mux.HandleFunc("POST /api/v1/auth/resend-verification", h.HandleResendVerification)
	return h, db, mux
}

// verifyReq posts one verify-email request (no session).
func verifyReq(t *testing.T, mux http.Handler, body map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify-email", jsonBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// resendReq posts one resend-verification request with the given session
// state (cookie + csrf empty = anonymous).
func resendReq(t *testing.T, mux http.Handler, cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/resend-verification", nil)
	setOrigin(req)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	}
	if csrf != "" {
		setCSRF(req, csrf)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}
