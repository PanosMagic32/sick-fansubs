package routes

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/mail"
	"sick-fansubs/internal/middleware"
)

// sendAuthRequest drives one request through the auth route set from the given
// client IP, with the trusted origin set so unsafe methods reach the limiter
// chain. The empty JSON object keeps the request cheap: every document below
// fails validation before any store, bcrypt, or mail work.
func sendAuthRequest(t *testing.T, mux *http.ServeMux, method, path, ip string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("X-Forwarded-For", ip)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestAuth_PathIPLimits pins the per-path IP limiter table Auth() wires: each
// limited path allows its documented per-IP boundary and answers 429 on the
// request just past it, while a fresh client IP is still admitted — the
// limiter keys by client address, which the boundary assertion alone cannot
// see. A dropped `pathLimit` fails here; the origin-before-quota slot is
// pinned by TestAuth_UntrustedOriginDoesNotSpendQuota.
func TestAuth_PathIPLimits(t *testing.T) {
	t.Parallel()

	mux, db, _, _ := setupUsersMux(t)
	Auth(mux, db, false, "http://localhost:5173", "http://localhost:3000", mail.LogLink{}, middleware.NewRateLimiter(1000, time.Hour, time.Minute))

	tests := []struct {
		name    string
		method  string
		path    string
		ip      string
		freshIP string
		limit   int
	}{
		{"sign-in", http.MethodPost, "/api/v1/auth/sign-in", "10.30.0.1", "10.30.1.1", 10},
		{"register", http.MethodPost, "/api/v1/auth/register", "10.30.0.2", "10.30.1.2", 3},
		{"forgot-password", http.MethodPost, "/api/v1/auth/forgot-password", "10.30.0.3", "10.30.1.3", 3},
		{"reset-password", http.MethodPost, "/api/v1/auth/reset-password", "10.30.0.4", "10.30.1.4", 5},
		{"verify-email", http.MethodPost, "/api/v1/auth/verify-email", "10.30.0.5", "10.30.1.5", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 1; i <= tt.limit; i++ {
				rec := sendAuthRequest(t, mux, tt.method, tt.path, tt.ip)
				if rec.Code == http.StatusTooManyRequests {
					t.Fatalf("request %d/%d: status = 429, want allowed below the %d/IP boundary", i, tt.limit, tt.limit)
				}
			}

			rec := sendAuthRequest(t, mux, tt.method, tt.path, tt.ip)
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("request %d: status = %d, want 429 (body %q)", tt.limit+1, rec.Code, rec.Body.String())
			}

			fresh := sendAuthRequest(t, mux, tt.method, tt.path, tt.freshIP)
			if fresh.Code == http.StatusTooManyRequests {
				t.Fatalf("fresh IP %s: status = 429 while %s is exhausted, want allowed (the bucket must key by client address)", tt.freshIP, tt.ip)
			}
		})
	}
}

// TestAuth_SessionPathStaysUnlimited proves the path-scoped limits do not leak
// onto an unlimited sibling: one client IP may GET /api/v1/auth/session past
// every limited path's boundary without a 429.
func TestAuth_SessionPathStaysUnlimited(t *testing.T) {
	t.Parallel()

	mux, db, _, _ := setupUsersMux(t)
	Auth(mux, db, false, "http://localhost:5173", "http://localhost:3000", mail.LogLink{}, middleware.NewRateLimiter(1000, time.Hour, time.Minute))

	for i := 1; i <= 12; i++ {
		rec := sendAuthRequest(t, mux, http.MethodGet, "/api/v1/auth/session", "10.30.0.6")
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200 (body %q)", i, rec.Code, rec.Body.String())
		}
	}
}

// TestAuth_UntrustedOriginDoesNotSpendQuota pins the rule-4 slot shared by
// every chain: an untrusted-origin burst answers 403 before the IP limiter
// sees it, so the first trusted-origin request is still inside the boundary.
func TestAuth_UntrustedOriginDoesNotSpendQuota(t *testing.T) {
	t.Parallel()

	mux, db, _, _ := setupUsersMux(t)
	Auth(mux, db, false, "http://localhost:5173", "http://localhost:3000", mail.LogLink{}, middleware.NewRateLimiter(1000, time.Hour, time.Minute))

	const ip = "10.31.0.1"
	for i := 1; i <= 11; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://evil.example")
		req.Header.Set("X-Forwarded-For", ip)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("untrusted-origin request %d: status = %d, want 403 (body %q)", i, rec.Code, rec.Body.String())
		}
	}

	rec := sendAuthRequest(t, mux, http.MethodPost, "/api/v1/auth/sign-in", ip)
	if rec.Code == http.StatusTooManyRequests {
		t.Fatal("trusted-origin request: status = 429; an untrusted-origin burst must not spend the IP bucket")
	}
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("trusted-origin request: status = %d, want 422 (body %q)", rec.Code, rec.Body.String())
	}
}

// TestAuth_UnregisteredPathAnswersJSONNotFound pins the auth subtree's JSON 404
// fallback: an unknown path under /api/v1/auth/ answers the problem document,
// never the ServeMux text/plain default.
func TestAuth_UnregisteredPathAnswersJSONNotFound(t *testing.T) {
	t.Parallel()

	mux, db, _, _ := setupUsersMux(t)
	Auth(mux, db, false, "http://localhost:5173", "http://localhost:3000", mail.LogLink{}, middleware.NewRateLimiter(1000, time.Hour, time.Minute))

	rec := sendAuthRequest(t, mux, http.MethodGet, "/api/v1/auth/no-such-endpoint", "10.30.0.7")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["type"] != "/problems/not-found" {
		t.Errorf("problem type = %v, want /problems/not-found", body["type"])
	}
}

// TestAuth_NestedFallbackKeepsOneRequestIdentity pins the nested-chain
// contract end to end: an unknown auth path runs the auth chain and then the
// fallback chain, and a record written earlier in the request, the response
// header, and the problem body all share one request ID.
func TestAuth_NestedFallbackKeepsOneRequestIdentity(t *testing.T) {
	t.Parallel()

	mux, db, _, _ := setupUsersMux(t)
	Auth(mux, db, false, "http://localhost:5173", "http://localhost:3000", mail.LogLink{}, middleware.NewRateLimiter(1000, time.Hour, time.Minute))

	// A well-formed cookie (32 zero bytes, base64url) makes Session attempt a
	// lookup; the closed store forces the error branch, which logs the ID the
	// outer chain minted before the fallback runs.
	const token = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/no-such-endpoint", nil)
	req.AddCookie(&http.Cookie{Name: middleware.CookieName, Value: token})
	req = req.WithContext(logging.With(req.Context(), logger))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %q)", rec.Code, rec.Body.String())
	}
	headerID := rec.Header().Get("X-Request-ID")
	if len(headerID) != 32 {
		t.Fatalf("X-Request-ID = %q, want a 32-character ID", headerID)
	}
	var body struct {
		RequestID string `json:"requestId"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if body.RequestID != headerID {
		t.Errorf("problem requestId = %q, want the header ID %q", body.RequestID, headerID)
	}

	if strings.TrimSpace(buf.String()) == "" {
		t.Fatal("no log output; the session lookup failure was not recorded")
	}
	var recordID string
	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		var record struct {
			Msg       string `json:"msg"`
			RequestID string `json:"requestId"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		if record.Msg == "session lookup failed" {
			recordID = record.RequestID
		}
	}
	if recordID == "" {
		t.Fatalf("no session lookup failure record (log %q)", buf.String())
	}
	if recordID != headerID {
		t.Errorf("record requestId = %q, want the response ID %q (one request, one identity)", recordID, headerID)
	}
}
