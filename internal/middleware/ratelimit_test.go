package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"testing/synctest"
	"time"
)

func TestRateLimiter_AllowWithinLimit(t *testing.T) {
	t.Parallel()
	rl := NewRateLimiter(3, time.Minute, time.Minute)

	for i := range 3 {
		allowed, _ := rl.Allow("key1")
		if allowed {
			continue
		}
		t.Errorf("Allow(%q) request %d = false, want true", "key1", i+1)
	}
}

func TestRateLimiter_DenyExceeded(t *testing.T) {
	t.Parallel()
	rl := NewRateLimiter(2, time.Minute, time.Minute)

	rl.Allow("key1") // 1
	rl.Allow("key1") // 2
	allowed, retryAfter := rl.Allow("key1")

	if allowed {
		t.Fatal("third request should be denied")
	}
	if retryAfter <= 0 {
		t.Errorf("Allow(%q) retryAfter = %v, want > 0", "key1", retryAfter)
	}
	if retryAfter > time.Minute {
		t.Errorf("Allow(%q) retryAfter = %v, want <= 1m", "key1", retryAfter)
	}
}

func TestRateLimiter_DifferentKeysIndependent(t *testing.T) {
	t.Parallel()
	rl := NewRateLimiter(1, time.Minute, time.Minute)
	rl.Allow("10.0.0.1") // consume the one allowed hit

	rl.Allow("key1")
	allowed, _ := rl.Allow("key2")

	if !allowed {
		t.Fatal("key2 should be allowed independently of key1")
	}
}

func TestRateLimiter_AllowNilPanics(t *testing.T) {
	t.Parallel()

	var rl *RateLimiter
	defer func() {
		got := recover()
		if got == nil {
			t.Fatal("Allow on a nil limiter did not panic — a skipped limiter silently disables the limit")
		}
		msg, ok := got.(string)
		if !ok || msg != "middleware: nil RateLimiter" {
			t.Errorf("panic = %v, want %q", got, "middleware: nil RateLimiter")
		}
	}()

	rl.Allow("key1")
}

func TestRateLimiter_CleanupRemovesExpired(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rl := NewRateLimiter(1, 10*time.Millisecond, 5*time.Millisecond)
		t.Cleanup(rl.stopCleanup)

		rl.Allow("key1")

		// The window and several cleanup ticks elapse on the bubble clock; the
		// cleanup loop bounds memory without the test waiting in real time.
		synctest.Sleep(20 * time.Millisecond)

		rl.mu.Lock()
		_, exists := rl.entries["key1"]
		rl.mu.Unlock()

		if exists {
			t.Errorf("cleanup() left %q in entries after the window expired", "key1")
		}
	})
}

func TestRateLimitIPByPath_AllowsSafePath(t *testing.T) {
	t.Parallel()
	rl := NewRateLimiter(1, time.Minute, time.Minute)
	rl.Allow("10.0.0.1") // consume the one allowed hit
	called := false
	handler := RateLimitIPByPath(rl, "/sign-in")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodPost, "/session", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("handler should be called for non-rate-limited path")
	}
}

func TestRateLimitIPByPath_BlocksRateLimitedPath(t *testing.T) {
	t.Parallel()
	rl := NewRateLimiter(1, time.Minute, time.Minute)
	rl.Allow("10.0.0.1") // consume the one allowed hit
	called := false
	handler := RateLimitIPByPath(rl, "/sign-in")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodPost, "/sign-in", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called {
		t.Error("handler should NOT be called when rate limited")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("RateLimitIPByPath(/sign-in) status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	retryAfter := rec.Header().Get("Retry-After")
	seconds, err := strconv.Atoi(retryAfter)
	if err != nil {
		t.Errorf("Retry-After = %q, want integer seconds", retryAfter)
	} else if seconds < 1 {
		t.Errorf("Retry-After = %d, want >= 1", seconds)
	}
}

// ClientAddr tests: the shared client-address resolver the
// rate limiter and every audit writer use. The header precedence and the
// fallback are the whole contract — the trust boundary is the deployment
// topology (no published app port, Caddy's strict parse), documented on the
// function itself rather than re-checked here.

func TestClientAddr_XForwardedFor(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "192.168.1.1, 10.0.0.1")
	ip := ClientAddr(req)
	if ip != "192.168.1.1" {
		t.Errorf("got %q, want the left-most entry 192.168.1.1", ip)
	}
}

func TestClientAddr_SingleIP(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", " 192.168.1.1 ")
	ip := ClientAddr(req)
	if ip != "192.168.1.1" {
		t.Errorf("got %q, want the trimmed 192.168.1.1", ip)
	}
}

func TestClientAddr_FallbackToRemoteAddr(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	ip := ClientAddr(req)
	if ip != "10.0.0.1" {
		t.Errorf("got %q, want 10.0.0.1", ip)
	}
}

func TestClientAddr_IPv6Fallback(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		remoteAddr string
		want       string
	}{
		{"bracketed ipv6 strips the port", "[2001:db8::1]:443", "2001:db8::1"},
		{"ipv4 strips the port", "127.0.0.1:8080", "127.0.0.1"},
		{"empty peer stays empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if got := ClientAddr(req); got != tt.want {
				t.Errorf("ClientAddr(RemoteAddr=%q) = %q, want %q", tt.remoteAddr, got, tt.want)
			}
		})
	}
}

// TestClientAddr_NonClientShapedEntriesFallBackToPeer pins the shape check: a
// forwarded entry that is not a bare IP literal — malformed, or carrying an
// IPv6 zone — never becomes a bucket key or an audit row; the resolver falls
// back to the peer address.
func TestClientAddr_NonClientShapedEntriesFallBackToPeer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		fwd  string
	}{
		{"malformed entry", "not-an-address, 192.0.2.1"},
		{"zone-bearing entry", "fe80::1%eth0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = "10.0.0.9:4444"
			req.Header.Set("X-Forwarded-For", tt.fwd)
			if got := ClientAddr(req); got != "10.0.0.9" {
				t.Errorf("ClientAddr(X-Forwarded-For=%q) = %q, want the peer 10.0.0.9", tt.fwd, got)
			}
		})
	}
}

// TestWriteRateLimited_ClampsSubSecondRetryAfter pins the shared clamp: a
// sub-second window still answers integer seconds, minimum one.
func TestWriteRateLimited_ClampsSubSecondRetryAfter(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	writeRateLimited(rec, req, 200*time.Millisecond)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "1" {
		t.Errorf("Retry-After = %q, want %q", got, "1")
	}
}
