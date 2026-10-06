package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"sick-fansubs/internal/middleware"
)

// TestSetSessionCookie_AttributeContract pins the one cookie contract end to
// end: HttpOnly, SameSite=Lax, Path=/, host-only, the literal Max-Age, and
// Secure following its parameter. The literal 604800 (seven days in seconds)
// is deliberate: it pins the value the service's SessionLifetime must keep.
func TestSetSessionCookie_AttributeContract(t *testing.T) {
	t.Parallel()
	const wantMaxAge = 604800

	tests := []struct {
		name   string
		secure bool
	}{
		{"dev cookie is not Secure", false},
		{"production cookie is Secure", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			setSessionCookie(rec, "token-value", tt.secure)

			cookies := rec.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("Set-Cookie count = %d, want 1", len(cookies))
			}
			c := cookies[0]
			if c.Name != middleware.CookieName {
				t.Errorf("Name = %q, want %q", c.Name, middleware.CookieName)
			}
			if c.Value != "token-value" {
				t.Errorf("Value = %q, want the token", c.Value)
			}
			if c.Path != "/" {
				t.Errorf("Path = %q, want %q", c.Path, "/")
			}
			if c.Domain != "" {
				t.Errorf("Domain = %q, want host-only", c.Domain)
			}
			if !c.HttpOnly {
				t.Error("HttpOnly = false, want true")
			}
			if c.Secure != tt.secure {
				t.Errorf("Secure = %t, want %t", c.Secure, tt.secure)
			}
			if c.SameSite != http.SameSiteLaxMode {
				t.Errorf("SameSite = %v, want Lax", c.SameSite)
			}
			if c.MaxAge != wantMaxAge {
				t.Errorf("MaxAge = %d, want the lifetime in seconds %d", c.MaxAge, wantMaxAge)
			}
		})
	}
}

// TestClearSessionCookie_MatchesTheSetContract pins deletion-attribute parity:
// the clear cookie carries the same name, path, scope, and flags as the set
// cookie, with an empty value and a past expiry.
func TestClearSessionCookie_MatchesTheSetContract(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		secure bool
	}{
		{"dev clear", false},
		{"production clear", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRec := httptest.NewRecorder()
			setSessionCookie(setRec, "token-value", tt.secure)
			clearRec := httptest.NewRecorder()
			middleware.ClearSessionCookie(clearRec, tt.secure)

			set := setRec.Result().Cookies()[0]
			cleared := clearRec.Result().Cookies()[0]
			if cleared.Value != "" || cleared.MaxAge != -1 {
				t.Errorf("clear cookie = {value %q, maxAge %d}, want an empty deletion cookie", cleared.Value, cleared.MaxAge)
			}
			if cleared.Name != set.Name || cleared.Path != set.Path || cleared.Domain != set.Domain ||
				cleared.HttpOnly != set.HttpOnly || cleared.Secure != set.Secure || cleared.SameSite != set.SameSite {
				t.Errorf("clear cookie attrs = %+v, want the set contract %+v", cleared, set)
			}
		})
	}
}
