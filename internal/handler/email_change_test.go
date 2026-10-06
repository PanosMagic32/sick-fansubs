package handler_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/auth"
	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/mail"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store"
)

// setupEmailChange builds the email-change surface: the register endpoint
// (account creation) + the PUT /me/email handler sharing one db, sender,
// and verification-send limiter.
func setupEmailChange(t *testing.T, sender *resetFakeSender, limiter *middleware.RateLimiter) (*sql.DB, http.Handler, *http.ServeMux) {
	t.Helper()
	return setupEmailChangeWith(t, sender, sender, limiter)
}

// setupEmailChangeWith is the sender-injecting variant: the registration
// handler keeps the plain fake while the email-change service uses svcSender
// (the notice legs' hooks and failures).
func setupEmailChangeWith(t *testing.T, sender *resetFakeSender, svcSender mail.Sender, limiter *middleware.RateLimiter) (*sql.DB, http.Handler, *http.ServeMux) {
	t.Helper()
	_, db, mux := setupResetHandler(t, sender)

	svc := auth.New(db, svcSender, "http://localhost:3000")
	mux.HandleFunc("PUT /api/v1/users/me/email",
		handler.EmailChange(db, svc, "http://localhost:3000", limiter))

	// The users-subtree chain (RequestID → TrustedOrigin → Session → CSRF).
	var h http.Handler = mux
	h = middleware.CSRF()(h)
	h = middleware.Session(db, false)(h)
	h = middleware.TrustedOrigin("http://localhost:5173")(h)
	h = middleware.RequestID()(h)
	return db, h, mux
}

// noticeSubject is the email-change notice subject — the discriminator for
// the shared fake's new mail kind.
var noticeSubject, _ = mail.EmailChangedNotice("new@example.com")

// noticeMails returns only the email-change notices the fake recorded.
func (f *resetFakeSender) noticeMails() []resetSentMail {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []resetSentMail
	for _, m := range f.sent {
		if m.subject == noticeSubject {
			out = append(out, m)
		}
	}
	return out
}

// hookedSender wraps the shared fake for the email-change service: onSend
// observes every send (even a failing one), and failNotice fails only the
// notice leg — the verification send stays intact.
type hookedSender struct {
	inner      *resetFakeSender
	onSend     func(to, subject, body string)
	failNotice bool
}

func (s *hookedSender) Send(ctx context.Context, to, subject, body string) error {
	if s.onSend != nil {
		s.onSend(to, subject, body)
	}
	if s.failNotice && subject == noticeSubject {
		return errors.New("notice smtp down")
	}
	return s.inner.Send(ctx, to, subject, body)
}

// emailChangeReq posts one email-change request with the given session.
func emailChangeReq(t *testing.T, h http.Handler, cookie, csrf string, body map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/me/email", jsonBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	setOrigin(req)
	setCSRF(req, csrf)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestEmailChange_HappyPath pins the swap contract: the swap resets the stamp,
// kills both token tables, sends the verification email to the NEW address,
// and answers 200 with the refreshed profile.
func TestEmailChange_HappyPath(t *testing.T) {
	// Deliberately NOT parallel: registers the global audit writer.
	sender := &resetFakeSender{}
	limiter := middleware.NewRateLimiter(1000, time.Hour, time.Minute)
	db, h, mux := setupEmailChange(t, sender, limiter)
	audit.SetWriter(&store.AuditWriter{DB: db})
	t.Cleanup(func() { audit.SetWriter(nil) })

	cookie, csrf := createUser(t, mux, "changer", "old@example.com", "password123")

	rec := emailChangeReq(t, h, cookie, csrf, map[string]string{
		"email":           "new@example.com",
		"currentPassword": "password123",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("email change: status %d, body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"email":"new@example.com"`) {
		t.Errorf("response missing new email: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"emailVerified":false`) {
		t.Errorf("response missing emailVerified:false: %s", rec.Body.String())
	}

	// DB state: new email, NULL stamp, both token tables empty.
	u, err := store.UserByEmail(t.Context(), db, "new@example.com")
	if err != nil {
		t.Fatalf("lookup new email: %v", err)
	}
	if u.EmailVerifiedAtMS != nil {
		t.Error("stamp not reset")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM password_reset_tokens`).Scan(&n); err != nil {
		t.Fatalf("count reset tokens: %v", err)
	}
	if n != 0 {
		t.Errorf("%d reset token rows remain", n)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM email_verification_tokens`).Scan(&n); err != nil {
		t.Fatalf("count verification tokens: %v", err)
	}
	if n != 1 {
		t.Errorf("verification token rows = %d, want 1 (the new-address mint)", n)
	}

	// The verification email went to the NEW address.
	mails := sender.verifyMails()
	if len(mails) != 2 { // register + email change
		t.Fatalf("verification emails = %d, want 2", len(mails))
	}
	if mails[1].to != "new@example.com" {
		t.Errorf("verification email to %q, want new@example.com", mails[1].to)
	}

	// The security notice went to the OLD address and names the NEW one.
	notices := sender.noticeMails()
	if len(notices) != 1 {
		t.Fatalf("email change notices = %d, want 1", len(notices))
	}
	if notices[0].to != "old@example.com" {
		t.Errorf("notice to %q, want old@example.com", notices[0].to)
	}
	if !strings.Contains(notices[0].body, "new@example.com") {
		t.Errorf("notice body missing the new address: %q", notices[0].body)
	}

	// One email_changed success row, actor == target.
	var rows int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM audit_events WHERE event = 'email_changed' AND result = 'success' AND actor_id = target_id`,
	).Scan(&rows); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if rows != 1 {
		t.Errorf("email_changed success rows = %d, want 1", rows)
	}
}

// TestEmailChange_WrongPassword pins the re-authentication gate: generic
// 401 + an email_changed FAILURE audit row.
func TestEmailChange_WrongPassword(t *testing.T) {
	// Deliberately NOT parallel: registers the global audit writer.
	sender := &resetFakeSender{}
	limiter := middleware.NewRateLimiter(1000, time.Hour, time.Minute)
	db, h, mux := setupEmailChange(t, sender, limiter)
	audit.SetWriter(&store.AuditWriter{DB: db})
	t.Cleanup(func() { audit.SetWriter(nil) })

	cookie, csrf := createUser(t, mux, "wrongpw", "wrongpw@example.com", "password123")

	rec := emailChangeReq(t, h, cookie, csrf, map[string]string{
		"email":           "hijack@example.com",
		"currentPassword": "not-the-password",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: status %d, want 401", rec.Code)
	}
	var failures int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM audit_events WHERE event = 'email_changed' AND result = 'failure'`,
	).Scan(&failures); err != nil {
		t.Fatalf("count failures: %v", err)
	}
	if failures != 1 {
		t.Errorf("failure rows = %d, want 1", failures)
	}
	// Nothing changed, nothing sent.
	if _, err := store.UserByEmail(t.Context(), db, "hijack@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Error("email changed despite wrong password")
	}
}

// TestEmailChange_Validation pins the 422 field violations.
func TestEmailChange_Validation(t *testing.T) {
	t.Parallel()
	sender := &resetFakeSender{}
	limiter := middleware.NewRateLimiter(1000, time.Hour, time.Minute)
	_, h, mux := setupEmailChange(t, sender, limiter)

	cookie, csrf := createUser(t, mux, "validmail", "valid@example.com", "password123")
	// Another account claims the target address.
	createUser(t, mux, "takenowner", "taken@example.com", "password123")

	cases := []struct {
		name string
		body map[string]string
		code string
	}{
		{"empty email", map[string]string{"email": "", "currentPassword": "password123"}, "required"},
		{"empty password", map[string]string{"email": "x@example.com", "currentPassword": ""}, "required"},
		{"invalid format", map[string]string{"email": "not-an-email", "currentPassword": "password123"}, "invalidFormat"},
		{"taken", map[string]string{"email": "TAKEN@example.com", "currentPassword": "password123"}, "alreadyTaken"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := emailChangeReq(t, h, cookie, csrf, tc.body)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status %d, want 422 (body %s)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"code":"`+tc.code+`"`) {
				t.Errorf("missing violation code %q: %s", tc.code, rec.Body.String())
			}
		})
	}
}

// TestEmailChange_SameEmailNoop pins the idempotent no-op: 200, no send
// (beyond registration), no audit row.
func TestEmailChange_SameEmailNoop(t *testing.T) {
	// Deliberately NOT parallel: registers the global audit writer.
	sender := &resetFakeSender{}
	limiter := middleware.NewRateLimiter(1000, time.Hour, time.Minute)
	db, h, mux := setupEmailChange(t, sender, limiter)
	audit.SetWriter(&store.AuditWriter{DB: db})
	t.Cleanup(func() { audit.SetWriter(nil) })

	cookie, csrf := createUser(t, mux, "noopmail", "noop@example.com", "password123")

	rec := emailChangeReq(t, h, cookie, csrf, map[string]string{
		"email":           "NOOP@example.com", // case-insensitive same
		"currentPassword": "password123",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("no-op: status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(sender.verifyMails()) != 1 {
		t.Errorf("verification emails = %d, want 1 (registration only)", len(sender.verifyMails()))
	}
	if len(sender.noticeMails()) != 0 {
		t.Errorf("notices for the no-op = %d, want 0", len(sender.noticeMails()))
	}
	var rows int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM audit_events WHERE event = 'email_changed'`,
	).Scan(&rows); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if rows != 0 {
		t.Errorf("audit rows for the no-op = %d, want 0", rows)
	}
}

// TestEmailChange_SendFailureStill200 pins the failure rationale on the change
// surface: an SMTP outage never fails the committed swap.
func TestEmailChange_SendFailureStill200(t *testing.T) {
	t.Parallel()
	sender := &resetFakeSender{}
	limiter := middleware.NewRateLimiter(1000, time.Hour, time.Minute)
	_, h, mux := setupEmailChange(t, sender, limiter)

	cookie, csrf := createUser(t, mux, "outagechange", "outagechange@example.com", "password123")

	// Fail the change's send only.
	sender.mu.Lock()
	sender.err = errors.New("smtp down")
	sender.mu.Unlock()

	rec := emailChangeReq(t, h, cookie, csrf, map[string]string{
		"email":           "fresh@example.com",
		"currentPassword": "password123",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("send-failure change: status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"email":"fresh@example.com"`) {
		t.Errorf("change did not commit: %s", rec.Body.String())
	}
}

// TestEmailChange_SharedBucketWithResend pins the shared-bucket contract: ONE limiter
// instance gates both surfaces — a resend consumes the bucket an email
// change would need, and vice versa.
func TestEmailChange_SharedBucketWithResend(t *testing.T) {
	// Deliberately NOT parallel: builds its own 1-hit limiter and its own
	// mux so BOTH handlers share the SAME limiter instance (the production
	// cmd/api wiring — setupResetHandler would give the resend its own).
	sender := &resetFakeSender{}
	dir := testDataDir(t)
	db, err := database.Open(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := database.Apply(db); err != nil {
		t.Fatalf("apply: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	limiter := middleware.NewRateLimiter(1, 30*time.Minute, time.Minute)
	svc := auth.New(db, sender, "http://localhost:3000")
	generous := middleware.NewRateLimiter(1000, 15*time.Minute, time.Minute)
	authH := handler.NewAuth(svc, false, generous, generous, generous, limiter)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/register", authH.HandleRegister)
	mux.HandleFunc("POST /api/v1/auth/resend-verification", authH.HandleResendVerification)
	mux.HandleFunc("PUT /api/v1/users/me/email",
		handler.EmailChange(db, svc, "http://localhost:3000", limiter))

	var h http.Handler = mux
	h = middleware.CSRF()(h)
	h = middleware.Session(db, false)(h)
	h = middleware.TrustedOrigin("http://localhost:5173")(h)
	h = middleware.RequestID()(h)

	cookie, csrf := createUser(t, mux, "buckets", "buckets@example.com", "password123")

	// The resend consumes the shared bucket.
	resendReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/resend-verification", nil)
	resendReq.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	setOrigin(resendReq)
	setCSRF(resendReq, csrf)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, resendReq)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("resend: status %d, want 204", rec.Code)
	}

	// The email change must now answer 429 — the bucket is shared.
	rec = emailChangeReq(t, h, cookie, csrf, map[string]string{
		"email":           "blocked@example.com",
		"currentPassword": "password123",
	})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("email change after resend: status %d, want 429", rec.Code)
	}
	// Nothing changed.
	if _, err := store.UserByEmail(t.Context(), db, "blocked@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Error("email changed despite the shared bucket being hot")
	}
}

// TestEmailChange_Anonymous401 pins the authentication gate (after the
// trusted-origin gate — an anonymous request must carry the Origin).
func TestEmailChange_Anonymous401(t *testing.T) {
	t.Parallel()
	sender := &resetFakeSender{}
	limiter := middleware.NewRateLimiter(1000, time.Hour, time.Minute)
	_, h, _ := setupEmailChange(t, sender, limiter)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/me/email",
		jsonBody(t, map[string]string{"email": "x@example.com", "currentPassword": "pw"}))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: status %d, want 401", rec.Code)
	}
}

// TestVerifyEmail_AlreadyVerifiedNoAudit pins the idempotent path: a
// still-valid token consumed by an already-verified account answers 204
// but emits NO second email_verified row (a no-op is not a state change —
// the role-change precedent).
func TestVerifyEmail_AlreadyVerifiedNoAudit(t *testing.T) {
	// Deliberately NOT parallel: registers the global audit writer.
	sender := &resetFakeSender{}
	limiter := middleware.NewRateLimiter(1000, time.Hour, time.Minute)
	db, _, mux := setupEmailChange(t, sender, limiter)
	audit.SetWriter(&store.AuditWriter{DB: db})
	t.Cleanup(func() { audit.SetWriter(nil) })

	createUser(t, mux, "idemaudit", "idemaudit@example.com", "password123")
	token := resetLinkToken(sender.verifyMails()[0].body)

	// First verify: 204 + the one audit row.
	if rec := verifyReq(t, mux, map[string]string{"token": token}); rec.Code != http.StatusNoContent {
		t.Fatalf("first verify: status %d", rec.Code)
	}

	// The account is verified; mint a FRESH token directly (the resend
	// surface no-ops for verified users, so the store is the only mint).
	u, err := store.UserByCanonical(t.Context(), db, "idemaudit")
	if err != nil {
		t.Fatalf("lookup user: %v", err)
	}
	raw := []byte(strings.Repeat("x", 32)) // exactly 32 bytes → 43-char base64url
	sum := sha256.Sum256(raw)
	now := time.Now().UnixMilli()
	if err := store.CreateVerificationToken(t.Context(), db, store.CreateVerificationTokenParams{
		TokenDigest: sum[:], UserID: u.ID, CreatedAtMS: now, ExpiresAtMS: now + 60_000,
	}); err != nil {
		t.Fatalf("mint fresh token: %v", err)
	}
	rec := verifyReq(t, mux, map[string]string{
		"token": base64.RawURLEncoding.EncodeToString(raw),
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("idempotent verify: status %d, body %s", rec.Code, rec.Body.String())
	}

	var rows int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM audit_events WHERE event = 'email_verified'`,
	).Scan(&rows); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if rows != 1 {
		t.Errorf("email_verified rows = %d, want 1 (the idempotent path emits none)", rows)
	}
}

// TestVerifiedResend_NoBucketConsumption pins the shared bucket's no-op clause:
// a verified user's resend answers 204 BEFORE the shared bucket — it
// consumes no quota, and a later email change is not throttled.
func TestVerifiedResend_NoBucketConsumption(t *testing.T) {
	// Deliberately NOT parallel: its own 1-hit limiter + mux (the shared
	// wiring shape from TestEmailChange_SharedBucketWithResend).
	sender := &resetFakeSender{}
	dir := testDataDir(t)
	db, err := database.Open(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := database.Apply(db); err != nil {
		t.Fatalf("apply: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	limiter := middleware.NewRateLimiter(1, 30*time.Minute, time.Minute)
	svc := auth.New(db, sender, "http://localhost:3000")
	generous := middleware.NewRateLimiter(1000, 15*time.Minute, time.Minute)
	authH := handler.NewAuth(svc, false, generous, generous, generous, limiter)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/register", authH.HandleRegister)
	mux.HandleFunc("POST /api/v1/auth/verify-email", authH.HandleVerifyEmail)
	mux.HandleFunc("POST /api/v1/auth/resend-verification", authH.HandleResendVerification)
	mux.HandleFunc("PUT /api/v1/users/me/email",
		handler.EmailChange(db, svc, "http://localhost:3000", limiter))

	var h http.Handler = mux
	h = middleware.CSRF()(h)
	h = middleware.Session(db, false)(h)
	h = middleware.TrustedOrigin("http://localhost:5173")(h)
	h = middleware.RequestID()(h)

	cookie, csrf := createUser(t, mux, "vresend", "vresend@example.com", "password123")
	token := resetLinkToken(sender.verifyMails()[0].body)

	// Verify the account.
	if rec := verifyReq(t, mux, map[string]string{"token": token}); rec.Code != http.StatusNoContent {
		t.Fatalf("verify: status %d", rec.Code)
	}

	// The verified resend: 204, no send, no bucket touch.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/resend-verification", nil)
	req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	setOrigin(req)
	setCSRF(req, csrf)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("verified resend: status %d, want 204", rec.Code)
	}
	if len(sender.verifyMails()) != 1 {
		t.Errorf("verified resend sent mail: %d verification emails", len(sender.verifyMails()))
	}

	// The untouched bucket lets the email change through.
	rec = emailChangeReq(t, h, cookie, csrf, map[string]string{
		"email":           "fresh@example.com",
		"currentPassword": "password123",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("email change after verified resend: status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestEmailChange_NoticeFailureKeeps200 pins the notice leg: a failing
// previous-address notice is logged, never surfaced — the committed swap
// stands and the response stays 200.
func TestEmailChange_NoticeFailureKeeps200(t *testing.T) {
	t.Parallel()
	sender := &resetFakeSender{}
	var attempts []resetSentMail
	hooked := &hookedSender{
		inner:      sender,
		failNotice: true,
		onSend: func(to, subject, body string) {
			if subject == noticeSubject {
				attempts = append(attempts, resetSentMail{to: to, subject: subject, body: body})
			}
		},
	}
	limiter := middleware.NewRateLimiter(1000, time.Hour, time.Minute)
	db, h, mux := setupEmailChangeWith(t, sender, hooked, limiter)

	cookie, csrf := createUser(t, mux, "noticefail", "noticefail@example.com", "password123")

	rec := emailChangeReq(t, h, cookie, csrf, map[string]string{
		"email":           "fresh@example.com",
		"currentPassword": "password123",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("notice-failure change: status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if _, err := store.UserByEmail(t.Context(), db, "fresh@example.com"); err != nil {
		t.Errorf("committed swap missing after the notice failure: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("notice attempts = %d, want 1", len(attempts))
	}
	if attempts[0].to != "noticefail@example.com" {
		t.Errorf("notice attempt to %q, want noticefail@example.com", attempts[0].to)
	}
	if !strings.Contains(attempts[0].body, "fresh@example.com") {
		t.Errorf("notice attempt missing the new address: %q", attempts[0].body)
	}
}

// TestEmailChange_NoticeSeesCommittedSwap pins the commit-before-send order:
// when the notice send runs, the new address is already committed.
func TestEmailChange_NoticeSeesCommittedSwap(t *testing.T) {
	t.Parallel()
	sender := &resetFakeSender{}
	var db *sql.DB
	observed := false
	hooked := &hookedSender{
		inner: sender,
		onSend: func(_, subject, _ string) {
			if subject != noticeSubject {
				return
			}
			observed = true
			if _, err := store.UserByEmail(t.Context(), db, "fresh@example.com"); err != nil {
				t.Errorf("committed swap not visible at notice-send time: %v", err)
			}
		},
	}
	limiter := middleware.NewRateLimiter(1000, time.Hour, time.Minute)
	var h http.Handler
	var mux *http.ServeMux
	db, h, mux = setupEmailChangeWith(t, sender, hooked, limiter)

	cookie, csrf := createUser(t, mux, "commitseen", "commitseen@example.com", "password123")

	rec := emailChangeReq(t, h, cookie, csrf, map[string]string{
		"email":           "fresh@example.com",
		"currentPassword": "password123",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("change: status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !observed {
		t.Error("notice send never observed — the send-time proof did not run")
	}
}
