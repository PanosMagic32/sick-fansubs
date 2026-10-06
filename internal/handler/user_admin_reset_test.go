package handler_test

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sick-fansubs/internal/auth"
	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/mail"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store"
)

// setupPasswordResetHandler creates a fresh migrated database and the reset
// endpoint behind the production users-subtree chain
// (RequestID → TrustedOrigin → Session → ForcePasswordChange → CSRF) at
// POST /api/v1/users/{id}/reset-password. The logger receives the audit
// events (the durable writer is NOT registered — the slog half is the
// assertion surface, matching the auth audit tests).
func setupPasswordResetHandler(t *testing.T, logger *slog.Logger) (*sql.DB, http.Handler) {
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

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/users/{id}/reset-password",
		handler.UserPasswordReset(db, auth.New(db, mail.LogLink{}, "")))

	var h http.Handler = mux
	h = middleware.CSRF()(h)
	h = middleware.ForcePasswordChange()(h)
	h = middleware.Session(db, false)(h)
	h = middleware.TrustedOrigin("http://localhost:5173")(h)
	h = middleware.RequestID()(h)

	return db, logContext(logger, h)
}

// staffReset issues the reset request as the given actor session. The CSRF
// token matches mustCreateStaffSession's all-zero stored token.
func staffReset(h http.Handler, actorCookie, id string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+id+"/reset-password", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("X-CSRF-Token", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	if actorCookie != "" {
		req.AddCookie(&http.Cookie{Name: "sf_session", Value: actorCookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestUserPasswordReset_Unauthenticated(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	_, h := setupPasswordResetHandler(t, logger)

	rec := staffReset(h, "", "aaaaaaaaaaaaaaaaaaaaaaaa")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
	if records := parseAuditEvents(t, &buf); len(records) != 0 {
		t.Errorf("unauthenticated attempt must not audit, got: %+v", records)
	}
}

func TestUserPasswordReset_InvalidID(t *testing.T) {
	t.Parallel()
	db, h := setupPasswordResetHandler(t, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	mustCreateStaffUser(t, db, "actor", "AdminOne", "admin", "active", 1000)
	actor := mustCreateStaffSession(t, db, "actor")

	rec := staffReset(h, actor, "not%20a%20valid%20id")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400 (id shape rejected before lookup)", rec.Code)
	}
}

func TestUserPasswordReset_NotFound(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupPasswordResetHandler(t, logger)
	mustCreateStaffUser(t, db, "actor", "AdminOne", "admin", "active", 1000)
	actor := mustCreateStaffSession(t, db, "actor")

	rec := staffReset(h, actor, "aaaaaaaaaaaaaaaaaaaaaaaa")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", rec.Code)
	}
	if records := parseAuditEvents(t, &buf); len(records) != 0 {
		t.Errorf("masked 404 must not audit, got: %+v", records)
	}
}

func TestUserPasswordReset_ModeratorForbiddenAudited(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupPasswordResetHandler(t, logger)
	mustCreateStaffUser(t, db, "mod", "ModOne", "moderator", "active", 1000)
	mustCreateStaffUser(t, db, "target", "PlainUser", "user", "active", 2000)
	mod := mustCreateStaffSession(t, db, "mod")

	rec := staffReset(h, mod, "target")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want 403 (below the admin+ floor)", rec.Code)
	}
	records := parseAuditEvents(t, &buf)
	if len(records) != 1 {
		t.Fatalf("blocked attempt must audit exactly once, got %d: %+v", len(records), records)
	}
	r := records[0]
	if r.Event != "password_reset" || r.Result != "failure" {
		t.Errorf("event/result: got %q/%q, want password_reset/failure", r.Event, r.Result)
	}
	if r.ActorID != "mod" || r.TargetID != "target" || r.TargetRole != "user" {
		t.Errorf("actor/target/role: got %q/%q/%q, want mod/target/user", r.ActorID, r.TargetID, r.TargetRole)
	}
}

func TestUserPasswordReset_AdminPeerForbiddenAudited(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupPasswordResetHandler(t, logger)
	mustCreateStaffUser(t, db, "admin-a", "AdminA", "admin", "active", 1000)
	mustCreateStaffUser(t, db, "admin-b", "AdminB", "admin", "active", 2000)
	actor := mustCreateStaffSession(t, db, "admin-a")

	rec := staffReset(h, actor, "admin-b")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want 403 (admin cannot touch admins)", rec.Code)
	}
	records := parseAuditEvents(t, &buf)
	if len(records) != 1 {
		t.Fatalf("blocked attempt must audit exactly once, got %d: %+v", len(records), records)
	}
	if r := records[0]; r.Result != "failure" || r.TargetRole != "admin" {
		t.Errorf("failure audit: got result %q targetRole %q, want failure/admin", r.Result, r.TargetRole)
	}
}

func TestUserPasswordReset_Success(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupPasswordResetHandler(t, logger)
	mustCreateStaffUser(t, db, "admin-a", "AdminA", "admin", "active", 1000)
	mustCreateStaffUser(t, db, "target", "TargetMod", "moderator", "active", 2000)
	actor := mustCreateStaffSession(t, db, "admin-a")
	// The target has a live session that the reset must delete.
	mustCreateStaffSession(t, db, "target")

	rec := staffReset(h, actor, "target")

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("expected Cache-Control: no-store on the one-time plaintext")
	}

	var resp struct {
		Password string `json:"password"`
	}
	decodeJSON(t, rec.Body, &resp)
	if len(resp.Password) != 16 {
		t.Errorf("temp password length: got %d, want 16", len(resp.Password))
	}
	// The one-time plaintext must never reach a log line — the captured
	// buffer is the assertion surface for the whole request.
	if strings.Contains(buf.String(), resp.Password) {
		t.Error("the temp password leaked into a log line")
	}

	// The temp password signs the target in, carrying the forced-change flag.
	authSvc := auth.New(db, mail.LogLink{}, "")
	_, _, _, mustChange, err := authSvc.SignIn(t.Context(), "targetmod", resp.Password, "")
	if err != nil {
		t.Fatalf("sign-in with temp password: %v", err)
	}
	if !mustChange {
		t.Error("sign-in with the temp password must carry mustChangePassword: true")
	}

	// The target row is flagged and version-bumped.
	target, err := store.UserByID(t.Context(), db, "target")
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if !target.MustChangePassword {
		t.Error("target must be flagged for a forced change")
	}
	if target.AuthVersion != 2 {
		t.Errorf("target auth_version: got %d, want 2", target.AuthVersion)
	}

	// The target's pre-reset session is gone: mustCreateStaffSession derives
	// the digest from sha256(sha256("token-"+userID)), so recompute it here.
	rawToken := sha256.Sum256([]byte("token-target"))
	targetDigest := sha256.Sum256(rawToken[:])
	if _, err := store.SessionByDigest(t.Context(), db, targetDigest[:]); err == nil {
		t.Error("the target's session must be deleted by the reset")
	}

	// Success audit: actor + target + role snapshot.
	records := parseAuditEvents(t, &buf)
	if len(records) != 1 {
		t.Fatalf("got %d: %+v, want exactly 1 audit event", len(records), records)
	}
	r := records[0]
	if r.Event != "password_reset" || r.Result != "success" {
		t.Errorf("event/result: got %q/%q, want password_reset/success", r.Event, r.Result)
	}
	if r.ActorID != "admin-a" || r.TargetID != "target" || r.TargetRole != "moderator" {
		t.Errorf("actor/target/role: got %q/%q/%q, want admin-a/target/moderator", r.ActorID, r.TargetID, r.TargetRole)
	}
}

func TestUserPasswordReset_SuperAdminSelfReset(t *testing.T) {
	t.Parallel()
	db, h := setupPasswordResetHandler(t, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	mustCreateStaffUser(t, db, "sa", "SuperOne", "super-admin", "active", 1000)
	sa := mustCreateStaffSession(t, db, "sa")

	rec := staffReset(h, sa, "sa")

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (super-admin self-reset — \"anyone incl. self\")", rec.Code)
	}
	var resp struct {
		Password string `json:"password"`
	}
	decodeJSON(t, rec.Body, &resp)
	if len(resp.Password) != 16 {
		t.Errorf("temp password length: got %d, want 16", len(resp.Password))
	}
}

func TestUserPasswordReset_SuperAdminResetsSuperAdmin(t *testing.T) {
	t.Parallel()
	db, h := setupPasswordResetHandler(t, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	mustCreateStaffUser(t, db, "sa-a", "SuperA", "super-admin", "active", 1000)
	mustCreateStaffUser(t, db, "sa-b", "SuperB", "super-admin", "active", 2000)
	actor := mustCreateStaffSession(t, db, "sa-a")

	rec := staffReset(h, actor, "sa-b")

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (super-admins see peer super-admins)", rec.Code)
	}
}
