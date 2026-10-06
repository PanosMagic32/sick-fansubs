package handler_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/auth"
	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store"
)

// signInAndGetCookie signs in a pre-created user and returns the session cookie
// and CSRF token from the response. The user must already exist in the database.
func signInAndGetCookie(t *testing.T, mux http.Handler, identifier, password string) (cookie string, csrfToken string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in",
		jsonBody(t, map[string]string{"identifier": identifier, "password": password}))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("sign-in failed: status %d, body: %s", rec.Code, rec.Body.String())
	}

	// Extract cookie.
	for _, c := range rec.Result().Cookies() {
		if c.Name == "sf_session" {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("sign-in response did not set sf_session cookie")
	}

	// Extract CSRF token from body.
	var resp struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeJSON(t, rec.Body, &resp)
	if resp.CSRFToken == "" {
		t.Fatal("sign-in response did not include csrfToken")
	}
	csrfToken = resp.CSRFToken

	return cookie, csrfToken
}

// addCookie adds the sf_session cookie to a request.
func addCookie(r *http.Request, value string) {
	r.AddCookie(&http.Cookie{Name: "sf_session", Value: value})
}

func TestHandleSignIn_Success(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	// First register a user so we have someone to sign in as.
	createUser(t, mux, "TestUser", "test@example.com", "password123")

	// Sign in.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in",
		jsonBody(t, map[string]string{"identifier": "TestUser", "password": "password123"}))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s, want 200", rec.Code, rec.Body.String())
	}

	// Check response body.
	var resp struct {
		CSRFToken          string `json:"csrfToken"`
		MustChangePassword bool   `json:"mustChangePassword"`
		User               struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"user"`
	}
	decodeJSON(t, rec.Body, &resp)

	if resp.CSRFToken == "" {
		t.Error("expected csrfToken in response")
	}
	if resp.MustChangePassword {
		t.Error("mustChangePassword must be false for a normal sign-in")
	}
	if resp.User.Username != "TestUser" {
		t.Errorf("got %q, want username TestUser", resp.User.Username)
	}
	if resp.User.Role != "user" {
		t.Errorf("got %q, want role user", resp.User.Role)
	}
	if resp.User.ID == "" {
		t.Error("expected user id in response")
	}

	// Check cookie.
	var cookieFound bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == "sf_session" {
			cookieFound = true
			if c.HttpOnly != true {
				t.Error("expected HttpOnly cookie")
			}
			if c.Path != "/" {
				t.Errorf("got %q, want Path=/", c.Path)
			}
			if c.MaxAge <= 0 {
				t.Errorf("got %d, want positive MaxAge", c.MaxAge)
			}
		}
	}
	if !cookieFound {
		t.Error("expected sf_session cookie")
	}
}

func TestHandleSignIn_WrongPassword(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)
	createUser(t, mux, "TestUser", "test@example.com", "password123")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in",
		jsonBody(t, map[string]string{"identifier": "TestUser", "password": "wrong"}))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}

	// Verify RFC 9457 problem body.
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("got %q, want application/problem+json", ct)
	}

	var body map[string]any
	decodeJSON(t, rec.Body, &body)
	if body["type"] != "/problems/auth/invalid-credentials" {
		t.Errorf("got %v, want /problems/auth/invalid-credentials", body["type"])
	}
	if body["status"] != float64(401) {
		t.Errorf("got %v, want status 401", body["status"])
	}
}

func TestHandleSignIn_UnknownUser(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in",
		jsonBody(t, map[string]string{"identifier": "Nobody", "password": "anything"}))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401 for unknown user", rec.Code)
	}
}

func TestHandleSignIn_EmailLookup(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)
	createUser(t, mux, "TestUser", "test@example.com", "password123")

	// Sign in with email.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in",
		jsonBody(t, map[string]string{"identifier": "test@example.com", "password": "password123"}))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s, want 200 for email login", rec.Code, rec.Body.String())
	}
}

func TestHandleSignIn_EmptyIdentifier(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in",
		jsonBody(t, map[string]string{"identifier": "", "password": "anything"}))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d, want 422 for empty identifier", rec.Code)
	}
}

func TestHandleSignIn_MalformedJSON(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in",
		strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400 for malformed JSON", rec.Code)
	}
}

func TestHandleSignIn_UnknownFields(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in",
		strings.NewReader(`{"identifier":"x","password":"y","extra":"bad"}`))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400 for unknown fields", rec.Code)
	}
}

func TestHandleSession_Authenticated(t *testing.T) {
	t.Parallel()
	_, db, mux := setupAuth(t)
	cookie, csrfTok := createUser(t, mux, "TestUser", "test@example.com", "password123")

	srv := withMiddleware(mux, db)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	addCookie(req, cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}

	// Check Cache-Control.
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("expected Cache-Control: no-store")
	}

	body := rec.Body.Bytes()
	var resp struct {
		Authenticated bool    `json:"authenticated"`
		CSRFToken     *string `json:"csrfToken"`
		User          *struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"user"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode bootstrap body: %v", err)
	}

	if !resp.Authenticated {
		t.Error("expected authenticated: true")
	}
	if resp.CSRFToken == nil || *resp.CSRFToken != csrfTok {
		t.Errorf("got %v, want csrfToken %q", resp.CSRFToken, csrfTok)
	}
	if resp.User == nil {
		t.Fatal("expected user object")
	}
	if resp.User.Username != "TestUser" {
		t.Errorf("got %q, want username TestUser", resp.User.Username)
	}

	// The bootstrap shape is EXACT: the response carries no
	// session identifier — only the CSRF token, the safe user projection, and
	// the forced-change flag. Decoding into an open struct would silently
	// accept a new field, so the key set is asserted literally.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("decode raw bootstrap body: %v", err)
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	wantKeys := []string{"authenticated", "csrfToken", "mustChangePassword", "user"}
	if !slices.Equal(keys, wantKeys) {
		t.Errorf("bootstrap keys = %v, want %v (no session id on the wire)", keys, wantKeys)
	}
}

func TestHandleSession_Unauthenticated(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 for unauthenticated session", rec.Code)
	}

	var resp struct {
		Authenticated      bool `json:"authenticated"`
		MustChangePassword bool `json:"mustChangePassword"`
		User               any  `json:"user"`
	}
	decodeJSON(t, rec.Body, &resp)

	if resp.Authenticated {
		t.Error("expected authenticated: false")
	}
	if resp.MustChangePassword {
		t.Error("mustChangePassword must be false when unauthenticated")
	}
	if resp.User != nil {
		t.Errorf("got %v, want user: null", resp.User)
	}
}

func TestHandleSession_MustChangePassword(t *testing.T) {
	t.Parallel()
	_, db, mux := setupAuth(t)
	cookie, _ := createUser(t, mux, "TestUser", "test@example.com", "password123")

	// Flag the account — the post-condition of an admin reset.
	if _, err := db.Exec(`UPDATE users SET must_change_password = 1 WHERE username = 'TestUser'`); err != nil {
		t.Fatalf("set flag: %v", err)
	}

	srv := withMiddleware(mux, db)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	addCookie(req, cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}

	var resp struct {
		Authenticated      bool `json:"authenticated"`
		MustChangePassword bool `json:"mustChangePassword"`
	}
	decodeJSON(t, rec.Body, &resp)
	if !resp.Authenticated {
		t.Fatal("expected authenticated: true")
	}
	if !resp.MustChangePassword {
		t.Error("bootstrap must carry the forced-change flag")
	}
}

func TestHandleSignIn_MustChangePassword(t *testing.T) {
	t.Parallel()
	_, db, mux := setupAuth(t)
	createUser(t, mux, "TestUser", "test@example.com", "password123")

	if _, err := db.Exec(`UPDATE users SET must_change_password = 1 WHERE username = 'TestUser'`); err != nil {
		t.Fatalf("set flag: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in",
		jsonBody(t, map[string]string{"identifier": "TestUser", "password": "password123"}))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s, want 200", rec.Code, rec.Body.String())
	}

	var resp struct {
		MustChangePassword bool `json:"mustChangePassword"`
	}
	decodeJSON(t, rec.Body, &resp)
	if !resp.MustChangePassword {
		t.Error("sign-in must carry the forced-change flag")
	}
}

func TestHandleSignOut_Authenticated(t *testing.T) {
	t.Parallel()
	_, db, mux := setupAuth(t)
	cookie, csrfTok := createUser(t, mux, "TestUser", "test@example.com", "password123")

	srv := withMiddleware(mux, db)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-out", nil)
	addCookie(req, cookie)
	setOrigin(req)
	setCSRF(req, csrfTok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s, want 200", rec.Code, rec.Body.String())
	}

	// Check response body.
	var resp struct {
		Authenticated bool `json:"authenticated"`
	}
	decodeJSON(t, rec.Body, &resp)
	if resp.Authenticated {
		t.Error("expected authenticated: false")
	}

	// Check cookie is cleared.
	var cleared bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == "sf_session" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("expected sf_session cookie to be cleared")
	}
}

func TestHandleSignOut_UnauthenticatedIdempotent(t *testing.T) {
	t.Parallel()
	_, db, mux := setupAuth(t)

	srv := withMiddleware(mux, db)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-out", nil)
	setOrigin(req)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s, want 200 for idempotent sign-out", rec.Code, rec.Body.String())
	}

	var resp struct {
		Authenticated bool `json:"authenticated"`
	}
	decodeJSON(t, rec.Body, &resp)
	if resp.Authenticated {
		t.Error("expected authenticated: false")
	}
}

func TestHandleSignOutAll_Success(t *testing.T) {
	t.Parallel()
	_, db, mux := setupAuth(t)
	cookie, csrfTok := createUser(t, mux, "TestUser", "test@example.com", "password123")

	srv := withMiddleware(mux, db)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-out-all", nil)
	addCookie(req, cookie)
	setOrigin(req)
	setCSRF(req, csrfTok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s, want 200", rec.Code, rec.Body.String())
	}

	var resp struct {
		Authenticated bool `json:"authenticated"`
	}
	decodeJSON(t, rec.Body, &resp)
	if resp.Authenticated {
		t.Error("expected authenticated: false")
	}
}

// TestHandleSignOutAll_UnauthenticatedIsIdempotent200 pins the recorded
// contract: the auth chain carries no session requirement, so a call without
// a session has nothing to revoke, clears the cookie, and answers the same
// 200 (the OpenAPI description records the idempotence).
func TestHandleSignOutAll_UnauthenticatedIsIdempotent200(t *testing.T) {
	t.Parallel()
	_, db, mux := setupAuth(t)
	srv := withMiddleware(mux, db)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-out-all", nil)
	setOrigin(req)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s, want 200", rec.Code, rec.Body.String())
	}
	var resp struct {
		Authenticated bool `json:"authenticated"`
	}
	decodeJSON(t, rec.Body, &resp)
	if resp.Authenticated {
		t.Error("expected authenticated: false")
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != middleware.CookieName || cookies[0].Value != "" {
		t.Errorf("cookies = %+v, want one clearing sf_session cookie", cookies)
	}
}

func TestHandleSignOutAll_RevokesAllSessions(t *testing.T) {
	t.Parallel()
	_, db, mux := setupAuth(t)
	cookie, csrfTok := createUser(t, mux, "TestUser", "test@example.com", "password123")

	// Create a second session by signing in again.
	cookie2, _ := signInAndGetCookie(t, mux, "TestUser", "password123")

	srv := withMiddleware(mux, db)

	// Sign out all with the first session.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-out-all", nil)
	addCookie(req, cookie)
	setOrigin(req)
	setCSRF(req, csrfTok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s, want 200", rec.Code, rec.Body.String())
	}

	// Both sessions should now be invalid.
	for _, c := range []string{cookie, cookie2} {
		req2 := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
		addCookie(req2, c)
		rec2 := httptest.NewRecorder()
		srv.ServeHTTP(rec2, req2)

		var resp2 struct {
			Authenticated bool `json:"authenticated"`
		}
		decodeJSON(t, rec2.Body, &resp2)
		if resp2.Authenticated {
			t.Errorf("session %s should be revoked after sign-out-all", c[:10]+"...")
		}
	}
}

func TestHandleRegister_Success(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		jsonBody(t, map[string]string{
			"username": "NewUser",
			"email":    "new@example.com",
			"password": "password123",
		}))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d: %s, want 201", rec.Code, rec.Body.String())
	}

	// Check response body.
	var resp struct {
		CSRFToken string `json:"csrfToken"`
		User      struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"user"`
	}
	decodeJSON(t, rec.Body, &resp)

	if resp.CSRFToken == "" {
		t.Error("expected csrfToken")
	}
	if resp.User.Username != "NewUser" {
		t.Errorf("got %q, want username NewUser", resp.User.Username)
	}
	if resp.User.Role != "user" {
		t.Errorf("got %q, want role user", resp.User.Role)
	}

	// Check cookie is set.
	var cookieFound bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == "sf_session" {
			cookieFound = true
		}
	}
	if !cookieFound {
		t.Error("expected sf_session cookie")
	}
}

func TestHandleRegister_AlreadyAuthenticated(t *testing.T) {
	t.Parallel()
	_, db, mux := setupAuth(t)
	cookie, csrfTok := createUser(t, mux, "Existing", "existing@example.com", "password123")

	srv := withMiddleware(mux, db)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		jsonBody(t, map[string]string{
			"username": "Another",
			"email":    "another@example.com",
			"password": "password123",
		}))
	req.Header.Set("Content-Type", "application/json")
	addCookie(req, cookie)
	setOrigin(req)
	setCSRF(req, csrfTok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403 for register-while-authenticated", rec.Code)
	}
}

func TestHandleSignIn_AlreadyAuthenticated(t *testing.T) {
	t.Parallel()
	_, db, mux := setupAuth(t)
	cookie, csrfTok := createUser(t, mux, "Existing", "existing@example.com", "password123")

	srv := withMiddleware(mux, db)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in",
		jsonBody(t, map[string]string{
			"identifier": "Existing",
			"password":   "password123",
		}))
	req.Header.Set("Content-Type", "application/json")
	addCookie(req, cookie)
	setOrigin(req)
	setCSRF(req, csrfTok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403 for sign-in-while-authenticated", rec.Code)
	}
	var problem struct {
		Type string `json:"type"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Type != "/problems/auth/already-authenticated" {
		t.Errorf("problem type = %q, want /problems/auth/already-authenticated", problem.Type)
	}
}

// TestHandleRegister_UnknownField pins the strict-field contract: confirmPassword is a
// client-side form convenience and must never be sent — DisallowUnknownFields
// rejects the request with 400.
func TestHandleRegister_UnknownField(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		strings.NewReader(`{"username":"NewUser","email":"new@example.com","password":"password123","confirmPassword":"password123"}`))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d: %s, want 400 for unknown field", rec.Code, rec.Body.String())
	}
}

// TestHandleRegister_OversizedBody pins the body bound: a body past the
// 4 KiB endpoint limit is rejected with 413, never truncated.
func TestHandleRegister_OversizedBody(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	big := strings.Repeat("a", 5000)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		strings.NewReader(`{"username":"NewUser","email":"new@example.com","password":"`+big+`"}`))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d: %s, want 413 for oversized body", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "/problems/payload-too-large") {
		t.Errorf("got: %s, want payload-too-large problem", rec.Body.String())
	}
}

// TestHandleRegister_TrailingDataPastLimit pins the trailing-data rule: a
// valid JSON value followed by padding that crosses the 4 KiB limit is
// rejected.
func TestHandleRegister_TrailingDataPastLimit(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	trailing := strings.Repeat(" ", 5000)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		strings.NewReader(`{"username":"NewUser","email":"new@example.com","password":"password123"}`+trailing))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d: %s, want 413 for trailing data past the limit", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "/problems/payload-too-large") {
		t.Errorf("got: %s, want payload-too-large problem", rec.Body.String())
	}
}

// TestHandleRegister_TrailingGarbagePastLimit pins the non-whitespace case:
// garbage that starts before the limit but extends
// the body past it must still be classified as 413, not 400.
func TestHandleRegister_TrailingGarbagePastLimit(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	garbage := `{"x":` + strings.Repeat("a", 5000)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		strings.NewReader(`{"username":"NewUser","email":"new@example.com","password":"password123"}`+garbage))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d: %s, want 413 for trailing garbage past the limit", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "/problems/payload-too-large") {
		t.Errorf("got: %s, want payload-too-large problem", rec.Body.String())
	}
}

// TestHandleSignIn_TrailingDataWithinLimit pins the exactly-one-JSON-value
// contract inside the body bound: a second value, a stray closing brace or
// bracket, or trailing garbage after the decoded object is 400, while
// trailing whitespace is accepted (the request proceeds to credentials).
func TestHandleSignIn_TrailingDataWithinLimit(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	valid := `{"identifier":"x","password":"password123"}`
	cases := []struct {
		name string
		body string
		want int
	}{
		{"second JSON value", valid + `{"a":1}`, http.StatusBadRequest},
		{"stray closing brace", valid + `}`, http.StatusBadRequest},
		{"stray closing bracket", valid + `]`, http.StatusBadRequest},
		{"trailing garbage", valid + `x`, http.StatusBadRequest},
		{"trailing whitespace accepted", valid + "  \n", http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in", strings.NewReader(c.body))
			req.Header.Set("Content-Type", "application/json")
			setOrigin(req)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, c.want, rec.Body.String())
			}
		})
	}
}

// TestHandleRegister_BodyExactlyAtLimit pins the boundary: a body of exactly
// 4 KiB is within the limit and must be accepted.
func TestHandleRegister_BodyExactlyAtLimit(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	base := `{"username":"AtLimit","email":"atlimit@example.com","password":"password123"}`
	pad := strings.Repeat(" ", 4096-len(base))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		strings.NewReader(base+pad))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d: %s, want 201 for exactly-4096-byte body", rec.Code, rec.Body.String())
	}
}

func TestHandleRegister_DuplicateUsername(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)
	createUser(t, mux, "TestUser", "test@example.com", "password123")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		jsonBody(t, map[string]string{
			"username": "TestUser",
			"email":    "different@example.com",
			"password": "password123",
		}))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d: %s, want 422 for duplicate username", rec.Code, rec.Body.String())
	}

	var body struct {
		Violations []struct {
			Field string `json:"field"`
			Code  string `json:"code"`
		} `json:"violations"`
	}
	decodeJSON(t, rec.Body, &body)

	var found bool
	for _, v := range body.Violations {
		if v.Field == "username" && v.Code == "alreadyTaken" {
			found = true
		}
	}
	if !found {
		t.Error("expected violation field=username code=alreadyTaken")
	}
}

func TestHandleRegister_DuplicateEmail(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)
	createUser(t, mux, "TestUser", "test@example.com", "password123")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		jsonBody(t, map[string]string{
			"username": "DifferentUser",
			"email":    "test@example.com",
			"password": "password123",
		}))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d, want 422 for duplicate email", rec.Code)
	}
}

func TestHandleRegister_MissingFields(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		jsonBody(t, map[string]string{
			"username": "",
			"email":    "",
			"password": "",
		}))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d, want 422 for missing fields", rec.Code)
	}
}

func TestHandleRegister_InvalidUsernameFormat(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	tests := []struct {
		name     string
		username string
	}{
		{"too long", strings.Repeat("a", 33)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
				jsonBody(t, map[string]string{
					"username": tt.username,
					"email":    "valid@example.com",
					"password": "password123",
				}))
			req.Header.Set("Content-Type", "application/json")
			setOrigin(req)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("got %d: %s, want 422", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestHandleRegister_RawUsernameFoldsToLegal pins the raw-spelling contract:
// a raw username longer than 32 bytes whose whitespace folds to a legal
// normalized name registers normally.
func TestHandleRegister_RawUsernameFoldsToLegal(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		jsonBody(t, map[string]string{
			"username": "John" + strings.Repeat(" ", 33) + "Doe",
			"email":    "folded@example.com",
			"password": "password123",
		}))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d: %s, want 201 for a raw spelling that folds to a legal name", rec.Code, rec.Body.String())
	}
	var resp struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	decodeJSON(t, rec.Body, &resp)
	if resp.User.Username != "John Doe" {
		t.Errorf("username = %q, want John Doe", resp.User.Username)
	}
}

// TestHandleRegister_RawUsernameOver128 pins the raw cap: a spelling longer
// than the wire schema's maxLength in code points answers 422 before the
// service folds it.
func TestHandleRegister_RawUsernameOver128(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		jsonBody(t, map[string]string{
			"username": "a" + strings.Repeat(" ", 200) + "b",
			"email":    "toolong@example.com",
			"password": "password123",
		}))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d: %s, want 422 for a raw username over 128 code points", rec.Code, rec.Body.String())
	}
	var body struct {
		Violations []struct {
			Field string `json:"field"`
			Code  string `json:"code"`
		} `json:"violations"`
	}
	decodeJSON(t, rec.Body, &body)
	var found bool
	for _, v := range body.Violations {
		if v.Field == "username" && v.Code == "maxLength" {
			found = true
		}
	}
	if !found {
		t.Errorf("violations = %+v, want username/maxLength", body.Violations)
	}
}

// TestHandleRegister_RawUsernameRuneBound pins the raw cap's unit: the wire
// schema counts code points, so NBSP padding past 128 bytes but under 128
// runes folds to a legal name and registers, while 129 runes answer 422 even
// though the padding is whitespace the service would fold away.
func TestHandleRegister_RawUsernameRuneBound(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	t.Run("202 bytes under the rune bound register", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
			jsonBody(t, map[string]string{
				"username": "a" + strings.Repeat("\u00a0", 100) + "b",
				"email":    "nbsp@example.com",
				"password": "password123",
			}))
		req.Header.Set("Content-Type", "application/json")
		setOrigin(req)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("got %d: %s, want 201 for a 102-rune/202-byte raw spelling", rec.Code, rec.Body.String())
		}
		var resp struct {
			User struct {
				Username string `json:"username"`
			} `json:"user"`
		}
		decodeJSON(t, rec.Body, &resp)
		if resp.User.Username != "a b" {
			t.Errorf("username = %q, want %q (the folded name)", resp.User.Username, "a b")
		}
	})

	t.Run("129 runes answer 422", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
			jsonBody(t, map[string]string{
				"username": "a" + strings.Repeat("\u00a0", 128),
				"email":    "nbsp-toolong@example.com",
				"password": "password123",
			}))
		req.Header.Set("Content-Type", "application/json")
		setOrigin(req)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("got %d: %s, want 422 for a 129-rune raw spelling", rec.Code, rec.Body.String())
		}
		var body struct {
			Violations []struct {
				Field string `json:"field"`
				Code  string `json:"code"`
			} `json:"violations"`
		}
		decodeJSON(t, rec.Body, &body)
		var found bool
		for _, v := range body.Violations {
			if v.Field == "username" && v.Code == "maxLength" {
				found = true
			}
		}
		if !found {
			t.Errorf("violations = %+v, want username/maxLength", body.Violations)
		}
	})
}

func TestHandleRegister_NonASCIIEmail(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		jsonBody(t, map[string]string{
			"username": "ValidUser",
			"email":    "ακης@example.com",
			"password": "password123",
		}))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d: %s, want 422", rec.Code, rec.Body.String())
	}
	// The violation must point at the email field with the invalidFormat code.
	if !strings.Contains(rec.Body.String(), `"field":"email"`) {
		t.Errorf("got: %s, want email field violation", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"invalidFormat"`) {
		t.Errorf("got: %s, want invalidFormat code", rec.Body.String())
	}
}

func TestHandleRegister_ShortPassword(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		jsonBody(t, map[string]string{
			"username": "ValidUser",
			"email":    "valid@example.com",
			"password": "short",
		}))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d: %s, want 422 for short password", rec.Code, rec.Body.String())
	}
}

func TestHandlePasswordChange_Success(t *testing.T) {
	t.Parallel()
	_, db, mux := setupAuth(t)
	cookie, csrfTok := createUser(t, mux, "TestUser", "test@example.com", "password123")

	srv := withMiddleware(mux, db)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/password",
		jsonBody(t, map[string]string{
			"currentPassword": "password123",
			"newPassword":     "newpassword456",
		}))
	req.Header.Set("Content-Type", "application/json")
	addCookie(req, cookie)
	setOrigin(req)
	setCSRF(req, csrfTok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s, want 200", rec.Code, rec.Body.String())
	}

	// Cookie should be cleared.
	var cleared bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == "sf_session" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("expected sf_session cookie to be cleared after password change")
	}

	// Old password should no longer work.
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in",
		jsonBody(t, map[string]string{"identifier": "TestUser", "password": "password123"}))
	req2.Header.Set("Content-Type", "application/json")
	setOrigin(req2)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want old password to fail (401)", rec2.Code)
	}

	// New password should work.
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in",
		jsonBody(t, map[string]string{"identifier": "TestUser", "password": "newpassword456"}))
	req3.Header.Set("Content-Type", "application/json")
	setOrigin(req3)
	rec3 := httptest.NewRecorder()
	mux.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusOK {
		t.Errorf("got %d: %s, want new password to succeed (200)", rec3.Code, rec3.Body.String())
	}
}

func TestHandlePasswordChange_WrongCurrentPassword(t *testing.T) {
	t.Parallel()
	_, db, mux := setupAuth(t)
	cookie, csrfTok := createUser(t, mux, "TestUser", "test@example.com", "password123")

	srv := withMiddleware(mux, db)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/password",
		jsonBody(t, map[string]string{
			"currentPassword": "wrongpassword",
			"newPassword":     "newpassword456",
		}))
	req.Header.Set("Content-Type", "application/json")
	addCookie(req, cookie)
	setOrigin(req)
	setCSRF(req, csrfTok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401 for wrong current password", rec.Code)
	}
}

func TestHandlePasswordChange_NewPasswordTooShort(t *testing.T) {
	t.Parallel()
	_, db, mux := setupAuth(t)
	cookie, csrfTok := createUser(t, mux, "TestUser", "test@example.com", "password123")

	srv := withMiddleware(mux, db)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/password",
		jsonBody(t, map[string]string{
			"currentPassword": "password123",
			"newPassword":     "short",
		}))
	req.Header.Set("Content-Type", "application/json")
	addCookie(req, cookie)
	setOrigin(req)
	setCSRF(req, csrfTok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d, want 422 for short new password", rec.Code)
	}
}

func TestHandlePasswordChange_RevokesSessions(t *testing.T) {
	t.Parallel()
	_, db, mux := setupAuth(t)
	cookie, csrfTok := createUser(t, mux, "TestUser", "test@example.com", "password123")

	// Create a second session.
	cookie2, _ := signInAndGetCookie(t, mux, "TestUser", "password123")

	srv := withMiddleware(mux, db)

	// Change password using first session.
	req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/password",
		jsonBody(t, map[string]string{
			"currentPassword": "password123",
			"newPassword":     "newpassword456",
		}))
	req.Header.Set("Content-Type", "application/json")
	addCookie(req, cookie)
	setOrigin(req)
	setCSRF(req, csrfTok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s, want 200", rec.Code, rec.Body.String())
	}

	// Both sessions should now be invalid.
	for _, c := range []string{cookie, cookie2} {
		req2 := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
		addCookie(req2, c)
		rec2 := httptest.NewRecorder()
		srv.ServeHTTP(rec2, req2)

		var resp2 struct {
			Authenticated bool `json:"authenticated"`
		}
		decodeJSON(t, rec2.Body, &resp2)
		if resp2.Authenticated {
			t.Errorf("session should be revoked after password change")
		}
	}
}

func TestHandlePasswordChange_MissingFields(t *testing.T) {
	t.Parallel()
	_, db, mux := setupAuth(t)
	cookie, csrfTok := createUser(t, mux, "TestUser", "test@example.com", "password123")

	srv := withMiddleware(mux, db)

	tests := []struct {
		name    string
		current string
		newPw   string
	}{
		{"missing current", "", "newpassword"},
		{"missing new", "password123", ""},
		{"both missing", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/password",
				jsonBody(t, map[string]string{
					"currentPassword": tt.current,
					"newPassword":     tt.newPw,
				}))
			req.Header.Set("Content-Type", "application/json")
			addCookie(req, cookie)
			setOrigin(req)
			setCSRF(req, csrfTok)
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("got %d, want 422", rec.Code)
			}
		})
	}
}

// TestHandlePasswordChange_CurrentPasswordTooLong pins the
// request bound: currentPassword carries the same 128-byte bound as sign-in's
// password field (the separately bounded legacy-investigation field).
func TestHandlePasswordChange_CurrentPasswordTooLong(t *testing.T) {
	t.Parallel()
	_, db, mux := setupAuth(t)
	cookie, csrfTok := createUser(t, mux, "TestUser", "test@example.com", "password123")

	srv := withMiddleware(mux, db)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/password",
		jsonBody(t, map[string]string{
			"currentPassword": strings.Repeat("a", 129),
			"newPassword":     "newpassword456",
		}))
	req.Header.Set("Content-Type", "application/json")
	addCookie(req, cookie)
	setOrigin(req)
	setCSRF(req, csrfTok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status: got %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"field":"currentPassword"`) ||
		!strings.Contains(rec.Body.String(), `"code":"maxLength"`) {
		t.Errorf("got: %s, want currentPassword maxLength violation", rec.Body.String())
	}
}

func TestAuthEndpointsSetXRequestID(t *testing.T) {
	t.Parallel()
	_, db, mux := setupAuth(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	rec := httptest.NewRecorder()
	withMiddleware(mux, db).ServeHTTP(rec, req)

	reqID := rec.Header().Get("X-Request-ID")
	if reqID == "" {
		t.Error("expected X-Request-ID header on auth response")
	}
	if len(reqID) != 32 {
		t.Errorf("got %d chars: %q, want 32-char hex X-Request-ID", len(reqID), reqID)
	}
}

func TestAuthEndpointsNoStore(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("expected Cache-Control: no-store on auth endpoints")
	}
}

func TestCSRFRequiredForSignOut(t *testing.T) {
	t.Parallel()
	_, db, mux := setupAuth(t)
	cookie, _ := createUser(t, mux, "TestUser", "test@example.com", "password123")

	srv := withMiddleware(mux, db)

	// Sign-out without CSRF token.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-out", nil)
	addCookie(req, cookie)
	setOrigin(req)
	// No X-CSRF-Token header.
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403 for missing CSRF token", rec.Code)
	}
}

func TestOriginRequiredForSignIn(t *testing.T) {
	t.Parallel()
	_, db, mux := setupAuth(t)

	srv := withMiddleware(mux, db)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in",
		jsonBody(t, map[string]string{"identifier": "anyone", "password": "anything"}))
	req.Header.Set("Content-Type", "application/json")
	// No Origin header.
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403 for missing origin", rec.Code)
	}
}

// TestSignIn_ContentTypeVariants pins the exact media-type boundary:
// only application/json — with an optional charset parameter — is accepted.
// Look-alike types must not be parsed as JSON. Accepted types proceed to
// field validation (422 for the empty payload); rejected types stop at 400.
func TestSignIn_ContentTypeVariants(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	cases := []struct {
		name        string
		contentType string
		wantStatus  int
	}{
		{"exact json", "application/json", http.StatusUnprocessableEntity},
		{"json with charset", "application/json; charset=utf-8", http.StatusUnprocessableEntity},
		{"json suffix lookalike", "application/jsonx", http.StatusUnsupportedMediaType},
		{"json-patch subtype", "application/json-patch+json", http.StatusUnsupportedMediaType},
		{"missing content type", "", http.StatusUnsupportedMediaType},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in",
				strings.NewReader(`{"identifier":"","password":""}`))
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			setOrigin(req)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("got %d: %s, want %d", rec.Code, rec.Body.String(), tc.wantStatus)
			}
		})
	}
}

// Test422Violations_ExactWireShape pins the violation wire contract:
// each violation is exactly { field, code } — internal validator text must
// never reach the wire.
func Test422Violations_ExactWireShape(t *testing.T) {
	t.Parallel()
	_, _, mux := setupAuth(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		strings.NewReader(`{"username":"NewUser","email":"not-an-email","password":"password123"}`))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d: %s, want 422", rec.Code, rec.Body.String())
	}

	var body map[string]any
	decodeJSON(t, rec.Body, &body)
	violations, ok := body["violations"].([]any)
	if !ok || len(violations) == 0 {
		t.Fatalf("got: %s, want violations array", rec.Body.String())
	}
	for _, v := range violations {
		obj, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("violation is not an object: %v", v)
		}
		// Exactly two keys — the OpenAPI schema declares additionalProperties:
		// false, so a len check catches any future stray member (e.g. an
		// omitempty'd field).
		if len(obj) != 2 {
			t.Errorf("violation has unexpected members: %v", obj)
		}
		if _, has := obj["message"]; has {
			t.Errorf("violation leaked internal message on the wire: %v", obj)
		}
		if obj["field"] == "" || obj["code"] == "" {
			t.Errorf("violation missing field/code: %v", obj)
		}
	}
}

// TestSignOut_StoreFailureKeepsCookie pins the commit contract: the cookie
// the cookie is cleared only after the session deletion commits. On store
// failure the response is 500 and the cookie is untouched, so the client
// keeps the credential and can retry instead of silently losing it while
// the session row survives server-side.
func TestSignOut_StoreFailureKeepsCookie(t *testing.T) {
	t.Parallel()
	_, db, mux := setupAuth(t)
	cookie, csrfTok := createUser(t, mux, "TestUser", "test@example.com", "password123")

	// Break the database after sign-in so the store layer fails.
	db.Close()

	srv := withMiddleware(mux, db)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-out", nil)
	addCookie(req, cookie)
	setOrigin(req)
	setCSRF(req, csrfTok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got %d: %s, want 500 when session deletion fails", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "sf_session" {
			t.Errorf("cookie must not be cleared when deletion fails, got: %v", c)
		}
	}
}

// sessionUserID bootstraps the current session and returns the user ID.
func sessionUserID(t *testing.T, srv http.Handler, cookie string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	addCookie(req, cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var resp struct {
		Authenticated bool `json:"authenticated"`
		User          *struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	decodeJSON(t, rec.Body, &resp)
	if !resp.Authenticated || resp.User == nil || resp.User.ID == "" {
		t.Fatalf("session bootstrap did not return a user: %s", rec.Body.String())
	}
	return resp.User.ID
}

func TestAudit_SignInSuccess(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	_, _, mux := setupAuthWithLogger(t, logger)

	createUser(t, mux, "TestUser", "test@example.com", "password123")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in",
		jsonBody(t, map[string]string{"identifier": "TestUser", "password": "password123"}))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	req = req.WithContext(middleware.SetRequestID(req.Context(), "test-request-id"))
	req.RemoteAddr = "192.0.2.1:1234" // the peer, with its port
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sign-in: got %d", rec.Code)
	}

	var body struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	decodeJSON(t, rec.Body, &body)

	records := parseAuditEvents(t, &buf)
	if len(records) != 1 {
		t.Fatalf("got %d: %+v, want exactly 1 audit event", len(records), records)
	}
	ev := records[0]
	if ev.Event != "sign_in_success" || ev.Result != "success" {
		t.Errorf("event: got %q/%q, want sign_in_success/success", ev.Event, ev.Result)
	}
	if ev.ActorID != body.User.ID {
		t.Errorf("actorID: got %q, want %q", ev.ActorID, body.User.ID)
	}
	if ev.RequestID != "test-request-id" {
		t.Errorf("requestID: got %q, want test-request-id", ev.RequestID)
	}
	if ev.RemoteAddr != "192.0.2.1" {
		// The shared resolver strips the peer port (the audit-browser
		// precedent): the audit row
		// carries a bare client address.
		t.Errorf("remoteAddr: got %q, want 192.0.2.1", ev.RemoteAddr)
	}
}

// TestAudit_SignInSuccessUsesForwardedClient pins the OTHER half of the
// trusted-proxy extraction at the HANDLER boundary (the audit-browser
// precedent): when Caddy
// supplied an X-Forwarded-For header, the ledger row carries the left-most
// entry rather than the peer. The helper's own unit test could pass while an
// emitter still passed r.RemoteAddr, which is exactly what this covers.
func TestAudit_SignInSuccessUsesForwardedClient(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	_, _, mux := setupAuthWithLogger(t, logger)

	createUser(t, mux, "FwdUser", "fwd@example.com", "password123")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in",
		jsonBody(t, map[string]string{"identifier": "FwdUser", "password": "password123"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 198.51.100.7")
	setOrigin(req)
	req.RemoteAddr = "10.0.0.5:4432"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sign-in: got %d (body %q)", rec.Code, rec.Body.String())
	}

	records := parseAuditEvents(t, &buf)
	if len(records) != 1 {
		t.Fatalf("got %d: %+v, want exactly 1 audit event", len(records), records)
	}
	if got := records[0].RemoteAddr; got != "203.0.113.9" {
		t.Errorf("remoteAddr: got %q, want the left-most forwarded client 203.0.113.9", got)
	}
}

func TestAudit_SignInFailureOmitsActorID(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	_, _, mux := setupAuthWithLogger(t, logger)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in",
		jsonBody(t, map[string]string{"identifier": "Nobody", "password": "anything"}))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	req = req.WithContext(middleware.SetRequestID(req.Context(), "test-request-id"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("sign-in: got %d", rec.Code)
	}

	records := parseAuditEvents(t, &buf)
	if len(records) != 1 {
		t.Fatalf("got %d: %+v, want exactly 1 audit event", len(records), records)
	}
	ev := records[0]
	if ev.Event != "sign_in_failure" || ev.Result != "failure" {
		t.Errorf("event: got %q/%q, want sign_in_failure/failure", ev.Event, ev.Result)
	}
	if ev.ActorID != "" {
		t.Errorf("failure event must not carry an actorID (enumeration resistance), got %q", ev.ActorID)
	}
	if ev.RequestID != "test-request-id" {
		t.Errorf("requestID: got %q, want test-request-id", ev.RequestID)
	}
}

func TestAudit_SignOut(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	_, db, mux := setupAuthWithLogger(t, logger)
	cookie, csrfTok := createUser(t, mux, "TestUser", "test@example.com", "password123")

	srv := withMiddleware(mux, db)
	userID := sessionUserID(t, srv, cookie)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-out", nil)
	addCookie(req, cookie)
	setOrigin(req)
	setCSRF(req, csrfTok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sign-out: got %d", rec.Code)
	}

	records := parseAuditEvents(t, &buf)
	if len(records) != 1 {
		t.Fatalf("got %d: %+v, want exactly 1 audit event", len(records), records)
	}
	ev := records[0]
	if ev.Event != "sign_out" || ev.Result != "success" {
		t.Errorf("event: got %q/%q, want sign_out/success", ev.Event, ev.Result)
	}
	if ev.ActorID != userID {
		t.Errorf("actorID: got %q, want %q", ev.ActorID, userID)
	}
	if ev.RequestID == "" {
		t.Error("requestID must be present (middleware-generated)")
	}
}

func TestAudit_SignOutAll(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	_, db, mux := setupAuthWithLogger(t, logger)
	cookie, csrfTok := createUser(t, mux, "TestUser", "test@example.com", "password123")

	srv := withMiddleware(mux, db)
	userID := sessionUserID(t, srv, cookie)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-out-all", nil)
	addCookie(req, cookie)
	setOrigin(req)
	setCSRF(req, csrfTok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sign-out-all: got %d", rec.Code)
	}

	records := parseAuditEvents(t, &buf)
	if len(records) != 1 {
		t.Fatalf("got %d: %+v, want exactly 1 audit event", len(records), records)
	}
	ev := records[0]
	if ev.Event != "sign_out_all" || ev.Result != "success" {
		t.Errorf("event: got %q/%q, want sign_out_all/success", ev.Event, ev.Result)
	}
	if ev.ActorID != userID {
		t.Errorf("actorID: got %q, want %q", ev.ActorID, userID)
	}
}

func TestAudit_PasswordChanged(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	_, db, mux := setupAuthWithLogger(t, logger)
	cookie, csrfTok := createUser(t, mux, "TestUser", "test@example.com", "password123")

	srv := withMiddleware(mux, db)
	userID := sessionUserID(t, srv, cookie)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/password",
		jsonBody(t, map[string]string{
			"currentPassword": "password123",
			"newPassword":     "newpassword456",
		}))
	req.Header.Set("Content-Type", "application/json")
	addCookie(req, cookie)
	setOrigin(req)
	setCSRF(req, csrfTok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("password change: got %d: %s", rec.Code, rec.Body.String())
	}

	records := parseAuditEvents(t, &buf)
	if len(records) != 1 {
		t.Fatalf("got %d: %+v, want exactly 1 audit event", len(records), records)
	}
	ev := records[0]
	if ev.Event != "password_changed" || ev.Result != "success" {
		t.Errorf("event: got %q/%q, want password_changed/success", ev.Event, ev.Result)
	}
	if ev.ActorID != userID {
		t.Errorf("actorID: got %q, want %q", ev.ActorID, userID)
	}
}

// TestAudit_NoSessionSignOutEmitsNoEvent pins that an idempotent no-session
// sign-out (clear the stale cookie, nothing deleted) produces no
// audit event — nothing happened server-side.
func TestAudit_NoSessionSignOutEmitsNoEvent(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	_, db, mux := setupAuthWithLogger(t, logger)

	srv := withMiddleware(mux, db)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-out", nil)
	setOrigin(req)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sign-out: got %d", rec.Code)
	}

	if records := parseAuditEvents(t, &buf); len(records) != 0 {
		t.Errorf("no-session sign-out must not emit an audit event, got: %+v", records)
	}
}

// TestAudit_StaleCookieSignOutEmitsNoEvent pins the stale-cookie rule:
// a WELL-FORMED cookie whose session row no longer exists (already revoked
// or expired) deletes zero rows and must not emit sign_out — otherwise a
// replayed stale cookie could generate unlimited spurious audit records
// (sign-out is deliberately rate-limit-free).
func TestAudit_StaleCookieSignOutEmitsNoEvent(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	_, db, mux := setupAuthWithLogger(t, logger)

	// Mint a cryptographically valid token for a session that does not exist.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)

	srv := withMiddleware(mux, db)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-out", nil)
	addCookie(req, encoded)
	setOrigin(req)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sign-out: got %d", rec.Code)
	}

	if records := parseAuditEvents(t, &buf); len(records) != 0 {
		t.Errorf("stale-cookie sign-out must not emit an audit event, got: %+v", records)
	}
}

// TestAudit_SignOut_Failure pins the failure-result event: an attempted
// sign-out whose deletion fails (store unavailable) is audit-worthy — the
// user's trail shows the attempt did NOT succeed. The actor ID is absent
// because the session middleware could not resolve the credential against
// the broken store.
func TestAudit_SignOut_Failure(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	_, db, mux := setupAuthWithLogger(t, logger)
	cookie, csrfTok := createUser(t, mux, "TestUser", "test@example.com", "password123")

	// Break the database so the deletion fails while the cookie remains
	// well-formed.
	db.Close()

	srv := withMiddleware(mux, db)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-out", nil)
	addCookie(req, cookie)
	setOrigin(req)
	setCSRF(req, csrfTok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("sign-out: got %d, want 500", rec.Code)
	}

	records := parseAuditEvents(t, &buf)
	if len(records) != 1 {
		t.Fatalf("got %d: %+v, want exactly 1 audit event", len(records), records)
	}
	ev := records[0]
	if ev.Event != "sign_out" || ev.Result != "failure" {
		t.Errorf("event: got %q/%q, want sign_out/failure", ev.Event, ev.Result)
	}
	if ev.ActorID != "" {
		t.Errorf("unresolved session must not carry an actorID, got %q", ev.ActorID)
	}
}

// TestAudit_PasswordChanged_WrongPassword pins the failure-result event for
// the security-relevant failure: a wrong CURRENT password (possible takeover
// attempt or typo) after the session already authenticated the caller.
func TestAudit_PasswordChanged_WrongPassword(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	_, db, mux := setupAuthWithLogger(t, logger)
	cookie, csrfTok := createUser(t, mux, "TestUser", "test@example.com", "password123")

	srv := withMiddleware(mux, db)
	userID := sessionUserID(t, srv, cookie)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/password",
		jsonBody(t, map[string]string{
			"currentPassword": "wrongpassword",
			"newPassword":     "newpassword456",
		}))
	req.Header.Set("Content-Type", "application/json")
	addCookie(req, cookie)
	setOrigin(req)
	setCSRF(req, csrfTok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("password change: got %d, want 401", rec.Code)
	}

	records := parseAuditEvents(t, &buf)
	if len(records) != 1 {
		t.Fatalf("got %d: %+v, want exactly 1 audit event", len(records), records)
	}
	ev := records[0]
	if ev.Event != "password_changed" || ev.Result != "failure" {
		t.Errorf("event: got %q/%q, want password_changed/failure", ev.Event, ev.Result)
	}
	if ev.ActorID != userID {
		t.Errorf("actorID: got %q, want %q", ev.ActorID, userID)
	}
}

func forgotReq(t *testing.T, mux http.Handler, body map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/forgot-password", jsonBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func resetReq(t *testing.T, mux http.Handler, body map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/reset-password", jsonBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	setOrigin(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestForgotPassword_Always200EmptyObject(t *testing.T) {
	t.Parallel()
	sender := &resetFakeSender{}
	_, _, mux := setupResetHandler(t, sender)

	// Unknown identifier: 200 {}, no email.
	rec := forgotReq(t, mux, map[string]string{"identifier": "ghost"})
	if rec.Code != http.StatusOK {
		t.Fatalf("unknown: status %d, body %s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "{}" {
		t.Errorf("unknown body = %q, want {}", rec.Body.String())
	}
	if len(sender.sent) != 0 {
		t.Errorf("unknown: %d emails sent, want 0", len(sender.sent))
	}

	// Known account: same 200 {} body, one email.
	createUser(t, mux, "knownacct", "known@example.com", "password123")
	rec = forgotReq(t, mux, map[string]string{"identifier": "knownacct"})
	if rec.Code != http.StatusOK {
		t.Fatalf("known: status %d, body %s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "{}" {
		t.Errorf("known body = %q, want {}", rec.Body.String())
	}
	if len(sender.resetMails()) != 1 {
		t.Fatalf("known: %d emails sent, want 1", len(sender.resetMails()))
	}

	// The link inside the email points at the dev origin + /auth/reset.
	body := sender.resetMails()[0].body
	if !strings.Contains(body, "http://localhost:3000/auth/reset?token=") {
		t.Errorf("email body missing reset link: %q", body)
	}
}

func TestForgotPassword_AuthenticatedRejected(t *testing.T) {
	t.Parallel()
	sender := &resetFakeSender{}
	_, db, mux := setupResetHandler(t, sender)
	cookie, csrf := createUser(t, mux, "authed", "authed@example.com", "password123")

	h := withMiddleware(mux, db)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/forgot-password",
		jsonBody(t, map[string]string{"identifier": "authed"}))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	setOrigin(req)
	setCSRF(req, csrf)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", rec.Code)
	}
	var prob struct {
		Type string `json:"type"`
	}
	decodeJSON(t, rec.Body, &prob)
	if prob.Type != "/problems/auth/already-authenticated" {
		t.Errorf("problem type = %q", prob.Type)
	}
	if len(sender.resetMails()) != 0 {
		t.Errorf("%d reset emails sent, want 0", len(sender.resetMails()))
	}
}

func TestForgotPassword_Validation(t *testing.T) {
	t.Parallel()
	sender := &resetFakeSender{}
	_, _, mux := setupResetHandler(t, sender)

	rec := forgotReq(t, mux, map[string]string{})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing identifier: status %d, want 422", rec.Code)
	}
	rec = forgotReq(t, mux, map[string]string{"identifier": strings.Repeat("x", 257)})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("overlong identifier: status %d, want 422", rec.Code)
	}
}

func TestForgotPassword_SendFailure500(t *testing.T) {
	t.Parallel()
	sender := &resetFakeSender{err: errors.New("smtp down")}
	_, _, mux := setupResetHandler(t, sender)
	createUser(t, mux, "outage", "outage@example.com", "password123")

	rec := forgotReq(t, mux, map[string]string{"identifier": "outage"})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("send failure: status %d, want 500", rec.Code)
	}
	// The unknown path still answers 200 during the outage.
	rec = forgotReq(t, mux, map[string]string{"identifier": "ghost"})
	if rec.Code != http.StatusOK {
		t.Fatalf("unknown during outage: status %d, want 200", rec.Code)
	}
}

func TestResetPassword_SuccessAndSecondUseFails(t *testing.T) {
	t.Parallel()
	sender := &resetFakeSender{}
	_, _, mux := setupResetHandler(t, sender)
	createUser(t, mux, "resetme", "resetme@example.com", "password123")
	forgotReq(t, mux, map[string]string{"identifier": "resetme"})

	token := resetLinkToken(sender.resetMails()[0].body)
	if len(token) != 43 {
		t.Fatalf("token length = %d", len(token))
	}

	rec := resetReq(t, mux, map[string]string{"token": token, "password": "fresh-password-1"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("reset: status %d, body %s", rec.Code, rec.Body.String())
	}

	// Second use: generic 422 token-invalid.
	rec = resetReq(t, mux, map[string]string{"token": token, "password": "another-password-1"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("second use: status %d, want 422", rec.Code)
	}
	var prob struct {
		Type string `json:"type"`
	}
	decodeJSON(t, rec.Body, &prob)
	if prob.Type != "/problems/auth/reset-token-invalid" {
		t.Errorf("problem type = %q, want reset-token-invalid", prob.Type)
	}
}

func TestResetPassword_TokenInvalidGeneric(t *testing.T) {
	t.Parallel()
	sender := &resetFakeSender{}
	_, _, mux := setupResetHandler(t, sender)

	for _, bad := range []string{"", "short", strings.Repeat("A", 43), strings.Repeat("!", 43)} {
		rec := resetReq(t, mux, map[string]string{"token": bad, "password": "valid-password-1"})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("token %q: status %d, want 422", bad, rec.Code)
		}
		var prob struct {
			Type string `json:"type"`
		}
		decodeJSON(t, rec.Body, &prob)
		if prob.Type != "/problems/auth/reset-token-invalid" {
			t.Errorf("token %q: problem type = %q", bad, prob.Type)
		}
	}
}

func TestResetPassword_TokenCheckedBeforePassword(t *testing.T) {
	t.Parallel()
	sender := &resetFakeSender{}
	_, _, mux := setupResetHandler(t, sender)

	rec := resetReq(t, mux, map[string]string{"token": strings.Repeat("!", 43), "password": "short"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", rec.Code)
	}
	var prob struct {
		Type string `json:"type"`
	}
	decodeJSON(t, rec.Body, &prob)
	if prob.Type != "/problems/auth/reset-token-invalid" {
		t.Errorf("problem type = %q, want reset-token-invalid (token first)", prob.Type)
	}
}

func TestResetPassword_WeakPasswordFieldViolation(t *testing.T) {
	t.Parallel()
	sender := &resetFakeSender{}
	_, _, mux := setupResetHandler(t, sender)
	createUser(t, mux, "weakacct", "weak@example.com", "password123")
	forgotReq(t, mux, map[string]string{"identifier": "weakacct"})
	token := resetLinkToken(sender.resetMails()[0].body)

	rec := resetReq(t, mux, map[string]string{"token": token, "password": "short"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", rec.Code)
	}
	var prob struct {
		Type       string `json:"type"`
		Violations []struct {
			Field string `json:"field"`
			Code  string `json:"code"`
		} `json:"violations"`
	}
	decodeJSON(t, rec.Body, &prob)
	if prob.Type != "/problems/validation" {
		t.Errorf("problem type = %q, want validation", prob.Type)
	}
	if len(prob.Violations) != 1 || prob.Violations[0].Field != "password" || prob.Violations[0].Code != "minLength" {
		t.Errorf("violations = %+v, want password/minLength", prob.Violations)
	}
}

func TestForgotPassword_IdentifierBucket429(t *testing.T) {
	// Deliberately NOT parallel: builds its own handler with a 1-hit limiter.
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

	svc := auth.New(db, sender, "http://localhost:3000")
	generous := middleware.NewRateLimiter(1000, 15*time.Minute, time.Minute)
	tiny := middleware.NewRateLimiter(1, time.Hour, time.Minute)
	h := handler.NewAuth(svc, false, generous, generous, tiny, generous)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/forgot-password", h.HandleForgotPassword)

	// The two spellings fold to one bucket key, so the second is throttled.
	first := forgotReq(t, mux, map[string]string{"identifier": " victim"})
	if first.Code != http.StatusOK {
		t.Fatalf("first request: status %d", first.Code)
	}
	second := forgotReq(t, mux, map[string]string{"identifier": "victim"})
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: status %d, want 429", second.Code)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Error("429 missing Retry-After")
	}
}

// TestSignIn_IdentifierBucketFoldsWhitespace pins the identifier-key fold:
// the lenient lookup resolves " victim" and "victim" to one account, so the
// identifier bucket must collapse them too — else each spelling buys a window.
func TestSignIn_IdentifierBucketFoldsWhitespace(t *testing.T) {
	// Deliberately NOT parallel: builds its own handler with a 1-hit limiter.
	dir := testDataDir(t)
	db, err := database.Open(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := database.Apply(db); err != nil {
		t.Fatalf("apply: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	svc := auth.New(db, nil, "http://localhost:3000")
	generous := middleware.NewRateLimiter(1000, 15*time.Minute, time.Minute)
	tiny := middleware.NewRateLimiter(1, 15*time.Minute, time.Minute)
	h := handler.NewAuth(svc, false, tiny, generous, generous, generous)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/sign-in", h.HandleSignIn)

	signIn := func(identifier string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in",
			jsonBody(t, map[string]string{"identifier": identifier, "password": "password123"}))
		req.Header.Set("Content-Type", "application/json")
		setOrigin(req)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	first := signIn(" victim")
	if first.Code != http.StatusUnauthorized {
		t.Fatalf("first request: status %d, want 401", first.Code)
	}
	second := signIn("victim")
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("spelling-variant request: status %d, want 429", second.Code)
	}
}

// TestSignIn_IdentifierBucketRecordOmitsTheIdentifier pins the log-hygiene
// rule: the 429 record names the bucket and never the identifier, which may be
// an email address.
func TestSignIn_IdentifierBucketRecordOmitsTheIdentifier(t *testing.T) {
	// Deliberately NOT parallel: builds its own handler with a 1-hit limiter
	// and a capture logger.
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	dir := testDataDir(t)
	db, err := database.Open(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := database.Apply(db); err != nil {
		t.Fatalf("apply: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	svc := auth.New(db, nil, "http://localhost:3000")
	generous := middleware.NewRateLimiter(1000, 15*time.Minute, time.Minute)
	tiny := middleware.NewRateLimiter(1, 15*time.Minute, time.Minute)
	h := handler.NewAuth(svc, false, tiny, generous, generous, generous)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/sign-in", h.HandleSignIn)
	captured := logContext(logger, mux)

	const identifier = "probe@example.com"
	signIn := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in",
			jsonBody(t, map[string]string{"identifier": identifier, "password": "password123"}))
		req.Header.Set("Content-Type", "application/json")
		setOrigin(req)
		rec := httptest.NewRecorder()
		captured.ServeHTTP(rec, req)
		return rec
	}

	if first := signIn(); first.Code != http.StatusUnauthorized {
		t.Fatalf("first request: status = %d, want 401 (body %q)", first.Code, first.Body.String())
	}
	if second := signIn(); second.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: status = %d, want 429 (body %q)", second.Code, second.Body.String())
	}

	logs := buf.String()
	if !strings.Contains(logs, "sign-in-id") {
		t.Errorf("log = %q, want the sign-in-id bucket record", logs)
	}
	if strings.Contains(logs, identifier) {
		t.Errorf("log = %q, must not carry the identifier", logs)
	}
}

// TestForgotPassword_IdentifierBucketRecordOmitsTheIdentifier is the forgot
// twin of the sign-in hygiene pin: the 429 record names the bucket, never the
// identifier.
func TestForgotPassword_IdentifierBucketRecordOmitsTheIdentifier(t *testing.T) {
	// Deliberately NOT parallel: builds its own handler with a 1-hit limiter
	// and a capture logger.
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	dir := testDataDir(t)
	db, err := database.Open(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := database.Apply(db); err != nil {
		t.Fatalf("apply: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	svc := auth.New(db, &resetFakeSender{}, "http://localhost:3000")
	generous := middleware.NewRateLimiter(1000, 15*time.Minute, time.Minute)
	tiny := middleware.NewRateLimiter(1, time.Hour, time.Minute)
	h := handler.NewAuth(svc, false, generous, generous, tiny, generous)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/forgot-password", h.HandleForgotPassword)
	captured := logContext(logger, mux)

	const identifier = "probe@example.com"
	forgot := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/forgot-password",
			jsonBody(t, map[string]string{"identifier": identifier}))
		req.Header.Set("Content-Type", "application/json")
		setOrigin(req)
		rec := httptest.NewRecorder()
		captured.ServeHTTP(rec, req)
		return rec
	}

	if first := forgot(); first.Code != http.StatusOK {
		t.Fatalf("first request: status = %d, want 200 (body %q)", first.Code, first.Body.String())
	}
	if second := forgot(); second.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: status = %d, want 429 (body %q)", second.Code, second.Body.String())
	}

	logs := buf.String()
	if !strings.Contains(logs, "forgot-password-id") {
		t.Errorf("log = %q, want the forgot-password-id bucket record", logs)
	}
	if strings.Contains(logs, identifier) {
		t.Errorf("log = %q, must not carry the identifier", logs)
	}
}

// TestResetPassword_AuthenticatedClearsCookieAndRevokes pins the reset's
// authenticated path: a reset from the emailed link while a live session
// exists succeeds with CSRF attached, clears the cookie, and the old session
// is unauthenticated afterward (semantic, not cookie bytes).
func TestResetPassword_AuthenticatedClearsCookieAndRevokes(t *testing.T) {
	// Deliberately NOT parallel: registers the global audit writer.
	sender := &resetFakeSender{}
	_, db, mux := setupResetHandler(t, sender)
	mux.HandleFunc("GET /api/v1/auth/session", func(w http.ResponseWriter, r *http.Request) {
		// Minimal bootstrap mirror for the semantic check.
		if middleware.GetSession(r.Context()) != nil {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"authenticated":true}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"authenticated":false}`))
	})

	h := withMiddleware(mux, db)
	cookie, csrf := createUser(t, mux, "cookieacct", "cookie@example.com", "password123")
	forgotReq(t, mux, map[string]string{"identifier": "cookieacct"})
	token := resetLinkToken(sender.resetMails()[0].body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/reset-password",
		jsonBody(t, map[string]string{"token": token, "password": "fresh-password-1"}))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	setOrigin(req)
	setCSRF(req, csrf)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("reset: status %d, body %s", rec.Code, rec.Body.String())
	}
	// The cookie is cleared (Max-Age 0) in the response…
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "sf_session" && c.MaxAge <= 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("session cookie not cleared in the reset response")
	}
	// …and the old session is gone server-side (semantic revocation).
	probe := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	probe.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	probeRec := httptest.NewRecorder()
	h.ServeHTTP(probeRec, probe)
	if !strings.Contains(probeRec.Body.String(), `"authenticated":false`) {
		t.Errorf("old session still authenticates: %s", probeRec.Body.String())
	}
}

// TestResetPassword_OtherAccountSessionSurvives pins the guarded cookie
// clear: a reset of account A carrying account B's session answers 204,
// leaves B's cookie untouched, and B's session still authenticates.
func TestResetPassword_OtherAccountSessionSurvives(t *testing.T) {
	t.Parallel()
	sender := &resetFakeSender{}
	_, db, mux := setupResetHandler(t, sender)
	mux.HandleFunc("GET /api/v1/auth/session", func(w http.ResponseWriter, r *http.Request) {
		// Minimal bootstrap mirror for the semantic check.
		if middleware.GetSession(r.Context()) != nil {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"authenticated":true}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"authenticated":false}`))
	})

	h := withMiddleware(mux, db)
	createUser(t, mux, "resetacct", "resetacct@example.com", "password123")
	otherCookie, otherCSRF := createUser(t, mux, "otheracct", "other@example.com", "password123")

	forgotReq(t, mux, map[string]string{"identifier": "resetacct"})
	token := resetLinkToken(sender.resetMails()[0].body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/reset-password",
		jsonBody(t, map[string]string{"token": token, "password": "fresh-password-1"}))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "sf_session", Value: otherCookie})
	setOrigin(req)
	setCSRF(req, otherCSRF)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("reset: status %d, body %s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "sf_session" && c.MaxAge <= 0 {
			t.Errorf("other account's session cookie cleared: %v", c)
		}
	}
	probe := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	probe.AddCookie(&http.Cookie{Name: "sf_session", Value: otherCookie})
	probeRec := httptest.NewRecorder()
	h.ServeHTTP(probeRec, probe)
	if !strings.Contains(probeRec.Body.String(), `"authenticated":true`) {
		t.Errorf("other account's session no longer authenticates: %s", probeRec.Body.String())
	}
}

// TestForgotAndReset_AuditRows pins the reset-flow audit contract: success +
// failure requested rows (actor-scoped) and the completed row with the
// target snapshot.
func TestForgotAndReset_AuditRows(t *testing.T) {
	// Deliberately NOT parallel: registers the global audit writer.
	sender := &resetFakeSender{}
	_, db, mux := setupResetHandler(t, sender)
	audit.SetWriter(&store.AuditWriter{DB: db})
	t.Cleanup(func() { audit.SetWriter(nil) })

	createUser(t, mux, "auditacct", "audit@example.com", "password123")
	forgotReq(t, mux, map[string]string{"identifier": "auditacct"})
	token := resetLinkToken(sender.resetMails()[0].body)
	resetReq(t, mux, map[string]string{"token": token, "password": "fresh-password-1"})

	var requestedSuccess, completed int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM audit_events WHERE event = 'password_reset_requested' AND result = 'success'`,
	).Scan(&requestedSuccess); err != nil {
		t.Fatalf("count requested: %v", err)
	}
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM audit_events WHERE event = 'password_reset_completed' AND result = 'success' AND target_role = 'user'`,
	).Scan(&completed); err != nil {
		t.Fatalf("count completed: %v", err)
	}
	if requestedSuccess != 1 || completed != 1 {
		t.Errorf("audit rows: requested_success=%d completed=%d, want 1 and 1", requestedSuccess, completed)
	}
}

// TestForgotPassword_SendFailureAuditRow pins the 500-path failure row.
func TestForgotPassword_SendFailureAuditRow(t *testing.T) {
	// Deliberately NOT parallel: registers the global audit writer.
	sender := &resetFakeSender{err: errors.New("smtp down")}
	_, db, mux := setupResetHandler(t, sender)
	audit.SetWriter(&store.AuditWriter{DB: db})
	t.Cleanup(func() { audit.SetWriter(nil) })

	createUser(t, mux, "outageacct", "outage@example.com", "password123")
	forgotReq(t, mux, map[string]string{"identifier": "outageacct"})

	var failures int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM audit_events WHERE event = 'password_reset_requested' AND result = 'failure'`,
	).Scan(&failures); err != nil {
		t.Fatalf("count failures: %v", err)
	}
	if failures != 1 {
		t.Errorf("failure rows = %d, want 1", failures)
	}
}

// TestRegister_SendsVerificationEmail pins the verification-send happy path: the 201
// account also mints one verification token and emails the link.
func TestRegister_SendsVerificationEmail(t *testing.T) {
	t.Parallel()
	sender := &resetFakeSender{}
	_, db, mux := setupResetHandler(t, sender)

	createUser(t, mux, "verifyreg", "verifyreg@example.com", "password123")

	mails := sender.verifyMails()
	if len(mails) != 1 {
		t.Fatalf("verification emails = %d, want 1", len(mails))
	}
	token := resetLinkToken(mails[0].body)
	if len(token) != 43 {
		t.Fatalf("verification token length = %d, want 43", len(token))
	}
	if !strings.Contains(mails[0].body, "http://localhost:3000/auth/verify?token=") {
		t.Errorf("email body missing verify link: %q", mails[0].body)
	}

	// One token row exists and the account is unverified.
	var tokenRows, verifiedNull int
	if err := db.QueryRow(`SELECT COUNT(*) FROM email_verification_tokens`).Scan(&tokenRows); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM users WHERE username = 'verifyreg' AND email_verified_at_ms IS NULL`,
	).Scan(&verifiedNull); err != nil {
		t.Fatalf("count unverified: %v", err)
	}
	if tokenRows != 1 || verifiedNull != 1 {
		t.Errorf("token rows = %d, unverified rows = %d, want 1 and 1", tokenRows, verifiedNull)
	}
}

// TestRegister_SendFailureStill201 pins the failure leg: an SMTP outage
// never fails the registration — the account exists and the response stays
// 201 (the resend surface is the recovery path).
func TestRegister_SendFailureStill201(t *testing.T) {
	t.Parallel()
	sender := &resetFakeSender{err: errors.New("smtp down")}
	_, _, mux := setupResetHandler(t, sender)

	// createUser asserts the 201 itself — with the failing sender, the
	// verification send must not change the outcome.
	_, _ = createUser(t, mux, "outagereg", "outagereg@example.com", "password123")
	if len(sender.sent) != 0 {
		t.Errorf("emails recorded = %d, want 0 (sender failed)", len(sender.sent))
	}
}

// TestVerifyEmail_SuccessAndIdempotent pins the consume flow: 204, stamp,
// token gone, audit row; second use is the generic 422.
func TestVerifyEmail_SuccessAndIdempotent(t *testing.T) {
	// Deliberately NOT parallel: registers the global audit writer.
	sender := &resetFakeSender{}
	_, db, mux := setupResetHandler(t, sender)
	audit.SetWriter(&store.AuditWriter{DB: db})
	t.Cleanup(func() { audit.SetWriter(nil) })

	createUser(t, mux, "verifyacct", "verify@example.com", "password123")
	token := resetLinkToken(sender.verifyMails()[0].body)

	rec := verifyReq(t, mux, map[string]string{"token": token})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("verify: status %d, body %s", rec.Code, rec.Body.String())
	}

	var verifiedNull int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM users WHERE username = 'verifyacct' AND email_verified_at_ms IS NOT NULL`,
	).Scan(&verifiedNull); err != nil {
		t.Fatalf("count verified: %v", err)
	}
	if verifiedNull != 1 {
		t.Error("account not stamped verified")
	}
	var tokenRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM email_verification_tokens`).Scan(&tokenRows); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if tokenRows != 0 {
		t.Errorf("token rows = %d, want 0", tokenRows)
	}

	// Audit row: email_verified, success, actor == target.
	var rows int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM audit_events WHERE event = 'email_verified' AND result = 'success' AND actor_id = target_id`,
	).Scan(&rows); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if rows != 1 {
		t.Errorf("email_verified audit rows = %d, want 1", rows)
	}

	// Second use → generic 422.
	rec = verifyReq(t, mux, map[string]string{"token": token})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("second verify: status %d, want 422", rec.Code)
	}
	var prob struct {
		Type string `json:"type"`
	}
	decodeJSON(t, rec.Body, &prob)
	if prob.Type != "/problems/auth/verification-token-invalid" {
		t.Errorf("problem type = %q, want verification-token-invalid", prob.Type)
	}
}

// TestVerifyEmail_InvalidTokenGeneric pins the shape/unknown/expired cases
// behind ONE generic problem type.
func TestVerifyEmail_InvalidTokenGeneric(t *testing.T) {
	t.Parallel()
	sender := &resetFakeSender{}
	_, _, mux := setupResetHandler(t, sender)

	for _, bad := range []string{"", "short", strings.Repeat("A", 43), strings.Repeat("!", 43)} {
		rec := verifyReq(t, mux, map[string]string{"token": bad})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("token %q: status %d, want 422", bad, rec.Code)
			continue
		}
		var prob struct {
			Type string `json:"type"`
		}
		decodeJSON(t, rec.Body, &prob)
		if prob.Type != "/problems/auth/verification-token-invalid" {
			t.Errorf("token %q: problem type = %q", bad, prob.Type)
		}
	}
}

// TestVerifyEmail_SuspendedGeneric pins the suspension kill: a suspended
// account's live token answers the same generic 422 and the token dies.
func TestVerifyEmail_SuspendedGeneric(t *testing.T) {
	t.Parallel()
	sender := &resetFakeSender{}
	_, db, mux := setupResetHandler(t, sender)

	createUser(t, mux, "suspendverify2", "suspendverify2@example.com", "password123")
	token := resetLinkToken(sender.verifyMails()[0].body)

	// Suspend the account directly.
	ctx := t.Context()
	uid, err := store.UserByCanonical(ctx, db, "suspendverify2")
	if err != nil {
		t.Fatalf("lookup user: %v", err)
	}
	if err := store.SuspendUser(ctx, db, uid.ID, time.Now().UnixMilli()); err != nil {
		t.Fatalf("suspend: %v", err)
	}

	rec := verifyReq(t, mux, map[string]string{"token": token})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("suspended verify: status %d, want 422", rec.Code)
	}
	// The token died — unreplayable after reactivation.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM email_verification_tokens`).Scan(&n); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if n != 0 {
		t.Errorf("token rows = %d, want 0", n)
	}
}

// TestVerifyEmail_AuthenticatedCSRF pins the reset-page precedent: the
// emailed link works from a live session with CSRF attached.
func TestVerifyEmail_AuthenticatedCSRF(t *testing.T) {
	t.Parallel()
	sender := &resetFakeSender{}
	_, db, mux := setupResetHandler(t, sender)

	cookie, csrf := createUser(t, mux, "authedverify", "authedverify@example.com", "password123")
	token := resetLinkToken(sender.verifyMails()[0].body)

	h := withMiddleware(mux, db)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify-email",
		jsonBody(t, map[string]string{"token": token}))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	setOrigin(req)
	setCSRF(req, csrf)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("authenticated verify: status %d, body %s", rec.Code, rec.Body.String())
	}
}

// TestResendVerification pins the resend contract: anonymous 401, verified no-op, send
// failure 500.
func TestResendVerification(t *testing.T) {
	t.Parallel()
	sender := &resetFakeSender{}
	_, db, mux := setupResetHandler(t, sender)

	// Anonymous → 401.
	if rec := resendReq(t, mux, "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous resend: status %d, want 401", rec.Code)
	}

	cookie, csrf := createUser(t, mux, "resender", "resender@example.com", "password123")
	firstToken := resetLinkToken(sender.verifyMails()[0].body)

	// Unverified resend → 204 + ONE more verification mail (the mint
	// replaced the registration token).
	h := withMiddleware(mux, db)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/resend-verification", nil)
	req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	setOrigin(req)
	setCSRF(req, csrf)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unverified resend: status %d, body %s", rec.Code, rec.Body.String())
	}
	mails := sender.verifyMails()
	if len(mails) != 2 {
		t.Fatalf("verification emails = %d, want 2", len(mails))
	}
	secondToken := resetLinkToken(mails[1].body)
	if secondToken == firstToken {
		t.Error("resend re-sent the same token")
	}

	// Verify with the fresh token → account verified.
	rec = verifyReq(t, mux, map[string]string{"token": secondToken})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("verify after resend: status %d", rec.Code)
	}

	// Verified resend → 204 no-op, no new mail, no new token row.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("verified resend: status %d, want 204", rec.Code)
	}
	if len(sender.verifyMails()) != 2 {
		t.Errorf("verified resend sent mail: %d verification emails", len(sender.verifyMails()))
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM email_verification_tokens`).Scan(&n); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if n != 0 {
		t.Errorf("token rows after verified resend = %d, want 0", n)
	}
}

// TestResendVerification_SendFailure500 pins the honest-500 leg.
func TestResendVerification_SendFailure500(t *testing.T) {
	t.Parallel()
	sender := &resetFakeSender{}
	_, db, mux := setupResetHandler(t, sender)

	cookie, csrf := createUser(t, mux, "resendfail", "resendfail@example.com", "password123")

	// The registration minted the current token; the resend must replace it
	// before the send so a failed send never leaves the old link active.
	var before []byte
	if err := db.QueryRow(`SELECT token_digest FROM email_verification_tokens`).Scan(&before); err != nil {
		t.Fatalf("read token before resend: %v", err)
	}

	// Fail the NEXT send only.
	sender.mu.Lock()
	sender.err = errors.New("smtp down")
	sender.mu.Unlock()

	h := withMiddleware(mux, db)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/resend-verification", nil)
	req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	setOrigin(req)
	setCSRF(req, csrf)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("send-failure resend: status %d, want 500", rec.Code)
	}
	// The committed replacement token is the pin: the digest changed even
	// though no email left the server.
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM email_verification_tokens`).Scan(&rows); err != nil {
		t.Fatalf("count tokens after resend: %v", err)
	}
	if rows != 1 {
		t.Errorf("token rows after failed send = %d, want 1", rows)
	}
	var after []byte
	if err := db.QueryRow(`SELECT token_digest FROM email_verification_tokens`).Scan(&after); err != nil {
		t.Fatalf("read token after resend: %v", err)
	}
	if bytes.Equal(before, after) {
		t.Error("verification token digest unchanged after a failed send, want the committed replacement")
	}
}

// TestResendVerification_RateLimit429 pins the SHARED verification-send
// bucket (1/user/30min; a 1-hit limiter stands in).
func TestResendVerification_RateLimit429(t *testing.T) {
	// Deliberately NOT parallel: builds its own handler with a 1-hit limiter.
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

	svc := auth.New(db, sender, "http://localhost:3000")
	generous := middleware.NewRateLimiter(1000, 15*time.Minute, time.Minute)
	tiny := middleware.NewRateLimiter(1, time.Hour, time.Minute)
	h := handler.NewAuth(svc, false, generous, generous, generous, tiny)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/register", h.HandleRegister)
	mux.HandleFunc("POST /api/v1/auth/resend-verification", h.HandleResendVerification)

	cookie, csrf := createUser(t, mux, "limitresend", "limitresend@example.com", "password123")

	h2 := withMiddleware(mux, db)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/resend-verification", nil)
	req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	setOrigin(req)
	setCSRF(req, csrf)
	rec := httptest.NewRecorder()
	h2.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("first resend: status %d, want 204", rec.Code)
	}
	rec = httptest.NewRecorder()
	h2.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second resend: status %d, want 429", rec.Code)
	}
}
