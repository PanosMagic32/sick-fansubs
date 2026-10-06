package routes

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/push"
	"sick-fansubs/internal/store"
	"sick-fansubs/internal/store/storetest"
)

// Push registration tests: the six
// paths on their chains — reads with the session, writes additionally
// CSRF — plus the JSON 404 fallbacks and the self-test endpoint's bucket.

// setupPushMux registers the namespace with an explicit self-test seam
// (nil = delivery disabled, the development shape).
func setupPushMux(t *testing.T, tester push.TestSender) (*http.ServeMux, *sql.DB, string) {
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

	storetest.InsertUser(t, db, storetest.UserSpec{
		ID: "u1", Username: "alice",
		CreatedAtMS: 1_700_000_000_000,
	})
	raw := make([]byte, 32)
	digest := sha256.Sum256(raw)
	if err := store.CreateSession(t.Context(), db, store.CreateSessionParams{
		ID: "s1", UserID: "u1", TokenDigest: digest[:], CSRF: make([]byte, 32),
		AuthVersion: 1, CreatedAtMS: 1_700_000_000_000, ExpiresAtMS: 9_000_000_000_000,
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	mux := http.NewServeMux()
	Push(mux, db, false, "http://localhost:5173", tester)
	APIFallback(mux)
	return mux, db, base64.RawURLEncoding.EncodeToString(raw)
}

// fakeTester is the self-test seam double for the routes suite. Its report is
// INTERNALLY consistent (one outcome per delivered endpoint, like the real
// sender's) so the suite cannot pin a state the sender never produces.
type fakeTester struct {
	delivered int
	calls     int
}

func (f *fakeTester) SendTest(_ context.Context, _ string) (push.TestReport, error) {
	f.calls++
	outcomes := make([]push.DeliveryOutcome, 0, f.delivered)
	for i := range f.delivered {
		outcomes = append(outcomes, push.DeliveryOutcome{
			SubscriptionID: "sub-" + strconv.Itoa(i),
			Service:        "mozilla",
			Accepted:       true,
			Status:         201,
		})
	}
	return push.TestReport{Delivered: f.delivered, Endpoints: outcomes}, nil
}

// TestPush_TestSendRoute pins the self-test endpoint's chain and bucket:
// anonymous 401, authenticated-without-CSRF 403, the 200 {delivered, endpoints}
// shape, and the user-keyed limiter (5/10 min) answering 429 + Retry-After on
// the sixth press without reaching the seam.
func TestPush_TestSendRoute(t *testing.T) {
	t.Parallel()

	tester := &fakeTester{delivered: 2}
	mux, _, cookie := setupPushMux(t, tester)

	rec := httptest.NewRecorder()
	anon := httptest.NewRequest(http.MethodPost, "/api/v1/push-subscriptions/test", nil)
	anon.Header.Set("Origin", "http://localhost:5173")
	mux.ServeHTTP(rec, anon)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: status = %d, want 401", rec.Code)
	}

	rec = httptest.NewRecorder()
	noCSRF := httptest.NewRequest(http.MethodPost, "/api/v1/push-subscriptions/test", nil)
	noCSRF.Header.Set("Origin", "http://localhost:5173")
	noCSRF.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	mux.ServeHTTP(rec, noCSRF)
	if rec.Code != http.StatusForbidden {
		t.Errorf("without CSRF: status = %d, want 403", rec.Code)
	}

	installed := 0
	for i := range 5 {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, pushAuthedReq(http.MethodPost, "/api/v1/push-subscriptions/test", cookie, ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("press %d: status = %d, want 200 (body %q)", i+1, rec.Code, rec.Body.String())
		}
		// Capture the bytes ONCE — decoding consumes the recorder's buffer.
		body := rec.Body.Bytes()
		var got struct {
			Delivered int              `json:"delivered"`
			Endpoints []map[string]any `json:"endpoints"`
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("press %d: decode %q: %v", i+1, string(body), err)
		}
		if got.Delivered != 2 {
			t.Errorf("press %d: delivered = %d, want 2", i+1, got.Delivered)
		}
		// The per-endpoint report reaches the wire through the FULL chain too.
		if len(got.Endpoints) != 2 {
			t.Errorf("press %d: endpoints = %d, want one per delivered device", i+1, len(got.Endpoints))
		}
		installed++
	}
	if tester.calls != installed {
		t.Errorf("seam calls = %d, want %d", tester.calls, installed)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, pushAuthedReq(http.MethodPost, "/api/v1/push-subscriptions/test", cookie, ""))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("sixth press: status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 without a Retry-After header")
	}
	if got := rec.Header().Get("Retry-After"); got != "" {
		// Integer seconds inside the bucket's 10-minute window — the exact
		// value depends on elapsed time, so the bound is the assertion.
		secs, err := strconv.Atoi(got)
		if err != nil || secs < 1 || secs > 600 {
			t.Errorf("Retry-After = %q, want the remaining 10-minute window in seconds", got)
		}
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
		t.Errorf("content type = %q, want a problem+json 429", ct)
	}
	if tester.calls != installed {
		t.Errorf("seam calls = %d after the limited press, want %d — the limiter must run before the send", tester.calls, installed)
	}
}

// TestPush_TestSendDeliveryDisabled: without the seam (development) the
// endpoint still answers 200 with a zero count — the contract is about the
// caller's devices, not about the server's configuration.
func TestPush_TestSendDeliveryDisabled(t *testing.T) {
	t.Parallel()

	mux, _, cookie := setupPushMux(t, nil)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, pushAuthedReq(http.MethodPost, "/api/v1/push-subscriptions/test", cookie, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	body := rec.Body.Bytes()
	var got struct {
		Delivered int `json:"delivered"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %q: %v", string(body), err)
	}
	if got.Delivered != 0 {
		t.Errorf("delivered = %d, want 0", got.Delivered)
	}
}

// pushAuthedReq mirrors commentsAuthedReq: the session cookie plus (for
// unsafe methods) the trusted origin + CSRF token.
func pushAuthedReq(method, path, cookie, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	if method != http.MethodGet {
		req.Header.Set("Origin", "http://localhost:5173")
		req.Header.Set("X-CSRF-Token", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	}
	return req
}

func TestPush_RoutesRegistered(t *testing.T) {
	t.Parallel()
	// nil tester: the self-test endpoint reports zero (delivery disabled);
	// its own chain and bucket are pinned in TestPush_TestSendRoute.
	mux, _, cookie := setupPushMux(t, nil)

	p256dh := base64.RawURLEncoding.EncodeToString(make([]byte, 65))
	auth := base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	body := `{"endpoint":"https://push.example.com/e","keys":{"p256dh":"` + p256dh + `","auth":"` + auth + `"}}`

	// Anonymous → 401 on every registered path.
	for _, path := range []string{
		"/api/v1/push-subscriptions",
		"/api/v1/notification-preferences",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("anonymous GET %s: status = %d, want 401", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	anonPost := httptest.NewRequest(http.MethodPost, "/api/v1/push-subscriptions", strings.NewReader(body))
	anonPost.Header.Set("Content-Type", "application/json")
	anonPost.Header.Set("Origin", "http://localhost:5173") // the origin gate runs before the session gate
	mux.ServeHTTP(rec, anonPost)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous POST create: status = %d, want 401", rec.Code)
	}

	// Authenticated without a CSRF token → 403 on the writes.
	rec = httptest.NewRecorder()
	noCSRF := httptest.NewRequest(http.MethodPost, "/api/v1/push-subscriptions", strings.NewReader(body))
	noCSRF.Header.Set("Content-Type", "application/json")
	noCSRF.Header.Set("Origin", "http://localhost:5173")
	noCSRF.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	mux.ServeHTTP(rec, noCSRF)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST without CSRF: status = %d, want 403", rec.Code)
	}

	// Full chain → 201, then the GETs answer 200.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, pushAuthedReq(http.MethodPost, "/api/v1/push-subscriptions", cookie, body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST create: status = %d, want 201 (body %q)", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, pushAuthedReq(http.MethodGet, "/api/v1/push-subscriptions", cookie, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET list: status = %d, want 200", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, pushAuthedReq(http.MethodGet, "/api/v1/notification-preferences", cookie, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET preferences: status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("missing X-Request-ID on the preferences read")
	}

	// JSON 404 fallbacks inside the namespaces.
	for _, path := range []string{
		"/api/v1/push-subscriptions/unknown",
		"/api/v1/notification-preferences/heart/extra",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, pushAuthedReq(http.MethodGet, path, cookie, ""))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want the JSON 404", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
			t.Errorf("GET %s: Content-Type = %q, want a problem", path, ct)
		}
	}
}
