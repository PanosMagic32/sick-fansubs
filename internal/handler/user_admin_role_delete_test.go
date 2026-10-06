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

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store"
)

// setupRoleDeleteHandler creates a fresh migrated database and the role
// change + deletion endpoints behind the production users-subtree chain
// (RequestID → TrustedOrigin → Session → ForcePasswordChange → CSRF).
func setupRoleDeleteHandler(t *testing.T, logger *slog.Logger) (*sql.DB, http.Handler) {
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
	const publicBase = "https://fans.example"
	mux.HandleFunc("PATCH /api/v1/users/{id}/role", handler.UserRoleChange(db, publicBase))
	mux.HandleFunc("DELETE /api/v1/users/{id}", handler.UserDelete(db))

	var h http.Handler = mux
	h = middleware.CSRF()(h)
	h = middleware.ForcePasswordChange()(h)
	h = middleware.Session(db, false)(h)
	h = middleware.TrustedOrigin("http://localhost:5173")(h)
	h = middleware.RequestID()(h)

	return db, logContext(logger, h)
}

// staffRoleChange issues PATCH /api/v1/users/{id}/role as the given actor.
func staffRoleChange(h http.Handler, actorCookie, id, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/"+id+"/role", strings.NewReader(body))
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("X-CSRF-Token", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	req.Header.Set("Content-Type", "application/json")
	if actorCookie != "" {
		req.AddCookie(&http.Cookie{Name: "sf_session", Value: actorCookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// staffDelete issues DELETE /api/v1/users/{id} as the given actor.
func staffDelete(h http.Handler, actorCookie, id string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/"+id, nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("X-CSRF-Token", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	if actorCookie != "" {
		req.AddCookie(&http.Cookie{Name: "sf_session", Value: actorCookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// sessionDigestOf recomputes mustCreateStaffSession's digest for "token-<id>"
// so tests can prove the target's session was revoked.
func sessionDigestOf(userID string) []byte {
	raw := sha256.Sum256([]byte("token-" + userID))
	digest := sha256.Sum256(raw[:])
	return digest[:]
}

func TestUserRoleChange_Unauthenticated(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	_, h := setupRoleDeleteHandler(t, logger)

	rec := staffRoleChange(h, "", "aaaaaaaaaaaaaaaaaaaaaaaa", `{"role":"moderator"}`)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
	if records := parseAuditEvents(t, &buf); len(records) != 0 {
		t.Errorf("unauthenticated attempt must not audit, got: %+v", records)
	}
}

func TestUserRoleChange_InvalidID(t *testing.T) {
	t.Parallel()
	db, h := setupRoleDeleteHandler(t, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	mustCreateStaffUser(t, db, "actor", "AdminOne", "admin", "active", 1000)
	actor := mustCreateStaffSession(t, db, "actor")

	rec := staffRoleChange(h, actor, "not%20a%20valid%20id", `{"role":"moderator"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400 (id shape rejected before lookup)", rec.Code)
	}
}

func TestUserRoleChange_BadBody(t *testing.T) {
	t.Parallel()
	db, h := setupRoleDeleteHandler(t, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	mustCreateStaffUser(t, db, "actor", "AdminOne", "admin", "active", 1000)
	actor := mustCreateStaffSession(t, db, "actor")

	rec := staffRoleChange(h, actor, "target", `{not json`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400 (malformed body)", rec.Code)
	}
}

func TestUserRoleChange_UnknownField(t *testing.T) {
	t.Parallel()
	db, h := setupRoleDeleteHandler(t, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	mustCreateStaffUser(t, db, "actor", "AdminOne", "admin", "active", 1000)
	actor := mustCreateStaffSession(t, db, "actor")

	// readJSON rejects unknown fields (strict decode).
	rec := staffRoleChange(h, actor, "target", `{"role":"moderator","bogus":true}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400 (unknown field)", rec.Code)
	}
}

func TestUserRoleChange_InvalidRoleValue(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupRoleDeleteHandler(t, logger)
	mustCreateStaffUser(t, db, "actor", "AdminOne", "admin", "active", 1000)
	actor := mustCreateStaffSession(t, db, "actor")

	rec := staffRoleChange(h, actor, "target", `{"role":"root"}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status: got %d, want 422", rec.Code)
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
		t.Errorf("problem type: got %q, want /problems/validation", prob.Type)
	}
	if len(prob.Violations) != 1 || prob.Violations[0].Field != "role" || prob.Violations[0].Code != "invalidValue" {
		t.Errorf("violations: got %+v, want [{role invalidValue}]", prob.Violations)
	}
	// A body-validation rejection is not an attempted state change: no audit row.
	if records := parseAuditEvents(t, &buf); len(records) != 0 {
		t.Errorf("422 must not audit, got: %+v", records)
	}
}

func TestUserRoleChange_NotFoundMasked(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupRoleDeleteHandler(t, logger)
	mustCreateStaffUser(t, db, "actor", "AdminOne", "admin", "active", 1000)
	actor := mustCreateStaffSession(t, db, "actor")

	rec := staffRoleChange(h, actor, "aaaaaaaaaaaaaaaaaaaaaaaa", `{"role":"moderator"}`)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", rec.Code)
	}
	if records := parseAuditEvents(t, &buf); len(records) != 0 {
		t.Errorf("masked 404 must not audit, got: %+v", records)
	}
}

func TestUserRoleChange_ModeratorForbiddenAudited(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupRoleDeleteHandler(t, logger)
	mustCreateStaffUser(t, db, "mod", "ModOne", "moderator", "active", 1000)
	mustCreateStaffUser(t, db, "target", "PlainUser", "user", "active", 2000)
	mod := mustCreateStaffSession(t, db, "mod")

	rec := staffRoleChange(h, mod, "target", `{"role":"moderator"}`)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want 403 (moderators never change roles)", rec.Code)
	}
	records := parseAuditEvents(t, &buf)
	if len(records) != 1 {
		t.Fatalf("blocked attempt must audit exactly once, got %d: %+v", len(records), records)
	}
	r := records[0]
	if r.Event != "role_changed" || r.Result != "failure" {
		t.Errorf("event/result: got %q/%q, want role_changed/failure", r.Event, r.Result)
	}
	if r.ActorID != "mod" || r.TargetID != "target" || r.TargetRole != "user" {
		t.Errorf("actor/target/role: got %q/%q/%q, want mod/target/user", r.ActorID, r.TargetID, r.TargetRole)
	}
}

func TestUserRoleChange_AdminPromotionDeniedAudited(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupRoleDeleteHandler(t, logger)
	mustCreateStaffUser(t, db, "admin-a", "AdminA", "admin", "active", 1000)
	mustCreateStaffUser(t, db, "target", "PlainUser", "user", "active", 2000)
	actor := mustCreateStaffSession(t, db, "admin-a")

	// Admins never grant admin or super-admin.
	rec := staffRoleChange(h, actor, "target", `{"role":"admin"}`)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want 403 (admin promotion denied)", rec.Code)
	}
	records := parseAuditEvents(t, &buf)
	if len(records) != 1 || records[0].Result != "failure" {
		t.Fatalf("blocked attempt must audit as failure, got: %+v", records)
	}
}

func TestUserRoleChange_AdminPeerForbiddenAudited(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupRoleDeleteHandler(t, logger)
	mustCreateStaffUser(t, db, "admin-a", "AdminA", "admin", "active", 1000)
	mustCreateStaffUser(t, db, "admin-b", "AdminB", "admin", "active", 2000)
	actor := mustCreateStaffSession(t, db, "admin-a")

	rec := staffRoleChange(h, actor, "admin-b", `{"role":"user"}`)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want 403 (admins cannot touch admins)", rec.Code)
	}
	if records := parseAuditEvents(t, &buf); len(records) != 1 || records[0].TargetRole != "admin" {
		t.Fatalf("blocked attempt must audit with the target's role snapshot, got: %+v", records)
	}
}

func TestUserRoleChange_Success(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupRoleDeleteHandler(t, logger)
	mustCreateStaffUser(t, db, "admin-a", "AdminA", "admin", "active", 1000)
	mustCreateStaffUser(t, db, "target", "TargetMod", "moderator", "active", 2000)
	actor := mustCreateStaffSession(t, db, "admin-a")
	mustCreateStaffSession(t, db, "target")
	// The write response shares staffUserProjection with the list, so it
	// carries avatarUrl too: the stored storage-form reference reaches the
	// wire as the absolute served URL.
	if _, err := db.Exec(`UPDATE users
		SET avatar_url = 'media/images/ab/abcdef0123456789abcdef0123456789.jpg'
		WHERE id = 'target'`); err != nil {
		t.Fatalf("set avatar: %v", err)
	}

	rec := staffRoleChange(h, actor, "target", `{"role":"user"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ID        string  `json:"id"`
		Username  string  `json:"username"`
		Email     *string `json:"email"`
		Role      string  `json:"role"`
		Status    string  `json:"status"`
		AvatarURL *string `json:"avatarUrl"`
	}
	decodeJSON(t, rec.Body, &resp)
	if resp.Role != identity.RoleUser || resp.ID != "target" {
		t.Errorf("projection: got role %q id %q, want user/target", resp.Role, resp.ID)
	}
	if resp.Email == nil || *resp.Email != "TargetMod@example.com" {
		t.Errorf("admin viewer must see the email, got %v", resp.Email)
	}
	wantAvatar := "https://fans.example/media/images/abcdef0123456789abcdef0123456789.jpg"
	if resp.AvatarURL == nil || *resp.AvatarURL != wantAvatar {
		t.Errorf("avatarUrl: got %v, want %q (absolute served URL)", resp.AvatarURL, wantAvatar)
	}

	// The change persisted: role updated, version bumped, sessions revoked.
	target, err := store.UserByID(t.Context(), db, "target")
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if target.Role != identity.RoleUser {
		t.Errorf("role: got %q, want user", target.Role)
	}
	if target.AuthVersion != 2 {
		t.Errorf("auth_version: got %d, want 2", target.AuthVersion)
	}
	if _, err := store.SessionByDigest(t.Context(), db, sessionDigestOf("target")); err == nil {
		t.Error("the target's session must be revoked by the role change")
	}

	// Success audit: actor + target + the NEW role snapshot.
	records := parseAuditEvents(t, &buf)
	if len(records) != 1 {
		t.Fatalf("got %d: %+v, want exactly 1 audit event", len(records), records)
	}
	r := records[0]
	if r.Event != "role_changed" || r.Result != "success" {
		t.Errorf("event/result: got %q/%q, want role_changed/success", r.Event, r.Result)
	}
	if r.ActorID != "admin-a" || r.TargetID != "target" || r.TargetRole != "user" {
		t.Errorf("actor/target/role: got %q/%q/%q, want admin-a/target/user (role AFTER)", r.ActorID, r.TargetID, r.TargetRole)
	}
}

// TestUserRoleChange_SuperAdminPromotion pins the promotion path:
// only a super-admin grants admin/super-admin — here a user becomes admin,
// the sessions revoke, and the success audit carries the NEW role.
func TestUserRoleChange_SuperAdminPromotion(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupRoleDeleteHandler(t, logger)
	mustCreateStaffUser(t, db, "sa", "SuperOne", "super-admin", "active", 1000)
	mustCreateStaffUser(t, db, "target", "PlainUser", "user", "active", 2000)
	actor := mustCreateStaffSession(t, db, "sa")
	mustCreateStaffSession(t, db, "target")

	rec := staffRoleChange(h, actor, "target", `{"role":"admin"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Role string `json:"role"`
	}
	decodeJSON(t, rec.Body, &resp)
	if resp.Role != identity.RoleAdmin {
		t.Errorf("projection role: got %q, want admin", resp.Role)
	}

	// The promotion persisted and revoked the target's sessions.
	target, err := store.UserByID(t.Context(), db, "target")
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if target.Role != identity.RoleAdmin {
		t.Errorf("role: got %q, want admin", target.Role)
	}
	if _, err := store.SessionByDigest(t.Context(), db, sessionDigestOf("target")); err == nil {
		t.Error("promotion must revoke the target's sessions (auth_version bump)")
	}

	records := parseAuditEvents(t, &buf)
	if len(records) != 1 {
		t.Fatalf("got %d: %+v, want exactly 1 audit event", len(records), records)
	}
	r := records[0]
	if r.Event != "role_changed" || r.Result != "success" || r.ActorID != "sa" || r.TargetID != "target" || r.TargetRole != "admin" {
		t.Errorf("audit: got %+v, want role_changed/success/sa/target/admin (role AFTER)", r)
	}
}

func TestUserRoleChange_NoOpSameRole(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupRoleDeleteHandler(t, logger)
	mustCreateStaffUser(t, db, "admin-a", "AdminA", "admin", "active", 1000)
	mustCreateStaffUser(t, db, "target", "TargetMod", "moderator", "active", 2000)
	actor := mustCreateStaffSession(t, db, "admin-a")

	rec := staffRoleChange(h, actor, "target", `{"role":"moderator"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (same-role no-op)", rec.Code)
	}
	// No state change: no version bump, no audit.
	target, err := store.UserByID(t.Context(), db, "target")
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if target.AuthVersion != 1 {
		t.Errorf("no-op must not bump auth_version, got %d", target.AuthVersion)
	}
	if records := parseAuditEvents(t, &buf); len(records) != 0 {
		t.Errorf("no-op must not audit, got: %+v", records)
	}
}

func TestUserRoleChange_NoOpUnauthorizedStill403(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupRoleDeleteHandler(t, logger)
	mustCreateStaffUser(t, db, "mod", "ModOne", "moderator", "active", 1000)
	mustCreateStaffUser(t, db, "target", "TargetMod", "moderator", "active", 2000)
	actor := mustCreateStaffSession(t, db, "mod")

	// A moderator addressing a peer moderator is forbidden even when the
	// requested role already matches — authorization precedes no-ops.
	rec := staffRoleChange(h, actor, "target", `{"role":"moderator"}`)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want 403 (authorization precedes even no-ops)", rec.Code)
	}
	records := parseAuditEvents(t, &buf)
	if len(records) != 1 || records[0].Result != "failure" {
		t.Fatalf("unauthorized attempt must audit as failure, got: %+v", records)
	}
}

func TestUserRoleChange_LastSuperAdminGuard(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupRoleDeleteHandler(t, logger)
	mustCreateStaffUser(t, db, "lone-sa", "LoneSA", "super-admin", "active", 1000)
	sa := mustCreateStaffSession(t, db, "lone-sa")

	rec := staffRoleChange(h, sa, "lone-sa", `{"role":"admin"}`)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want 409 (self-demotion of the last active super-admin)", rec.Code)
	}
	var prob struct {
		Type string `json:"type"`
	}
	decodeJSON(t, rec.Body, &prob)
	if prob.Type != "/problems/auth/last-super-admin" {
		t.Errorf("problem type: got %q, want /problems/auth/last-super-admin", prob.Type)
	}
	records := parseAuditEvents(t, &buf)
	if len(records) != 1 || records[0].Result != "failure" || records[0].TargetRole != "super-admin" {
		t.Fatalf("blocked demotion must audit as failure with the role snapshot, got: %+v", records)
	}
}

func TestUserDelete_Unauthenticated(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	_, h := setupRoleDeleteHandler(t, logger)

	rec := staffDelete(h, "", "aaaaaaaaaaaaaaaaaaaaaaaa")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
	if records := parseAuditEvents(t, &buf); len(records) != 0 {
		t.Errorf("unauthenticated attempt must not audit, got: %+v", records)
	}
}

func TestUserDelete_NotFoundMasked(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupRoleDeleteHandler(t, logger)
	mustCreateStaffUser(t, db, "actor", "AdminOne", "admin", "active", 1000)
	actor := mustCreateStaffSession(t, db, "actor")

	rec := staffDelete(h, actor, "aaaaaaaaaaaaaaaaaaaaaaaa")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", rec.Code)
	}
	if records := parseAuditEvents(t, &buf); len(records) != 0 {
		t.Errorf("masked 404 must not audit, got: %+v", records)
	}
}

func TestUserDelete_ModeratorForbiddenAudited(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupRoleDeleteHandler(t, logger)
	mustCreateStaffUser(t, db, "mod", "ModOne", "moderator", "active", 1000)
	mustCreateStaffUser(t, db, "target", "PlainUser", "user", "active", 2000)
	mod := mustCreateStaffSession(t, db, "mod")

	rec := staffDelete(h, mod, "target")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want 403 (below the admin+ floor)", rec.Code)
	}
	records := parseAuditEvents(t, &buf)
	if len(records) != 1 {
		t.Fatalf("blocked attempt must audit exactly once, got %d: %+v", len(records), records)
	}
	r := records[0]
	if r.Event != "user_deleted" || r.Result != "failure" {
		t.Errorf("event/result: got %q/%q, want user_deleted/failure", r.Event, r.Result)
	}
	if r.ActorID != "mod" || r.TargetID != "target" || r.TargetRole != "user" {
		t.Errorf("actor/target/role: got %q/%q/%q, want mod/target/user", r.ActorID, r.TargetID, r.TargetRole)
	}
}

func TestUserDelete_Success(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupRoleDeleteHandler(t, logger)
	mustCreateStaffUser(t, db, "admin-a", "AdminA", "admin", "active", 1000)
	mustCreateStaffUser(t, db, "target", "PlainUser", "user", "active", 2000)
	actor := mustCreateStaffSession(t, db, "admin-a")
	mustCreateStaffSession(t, db, "target")

	rec := staffDelete(h, actor, "target")

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204: %s", rec.Code, rec.Body.String())
	}

	// The row is gone and the target's session cascaded.
	if _, err := store.UserByID(t.Context(), db, "target"); err == nil {
		t.Error("the deleted user must be gone")
	}
	if _, err := store.SessionByDigest(t.Context(), db, sessionDigestOf("target")); err == nil {
		t.Error("the target's session must cascade with the deletion")
	}

	// Success audit: actor + target + the role snapshot AT deletion time.
	records := parseAuditEvents(t, &buf)
	if len(records) != 1 {
		t.Fatalf("got %d: %+v, want exactly 1 audit event", len(records), records)
	}
	r := records[0]
	if r.Event != "user_deleted" || r.Result != "success" {
		t.Errorf("event/result: got %q/%q, want user_deleted/success", r.Event, r.Result)
	}
	if r.ActorID != "admin-a" || r.TargetID != "target" || r.TargetRole != "user" {
		t.Errorf("actor/target/role: got %q/%q/%q, want admin-a/target/user", r.ActorID, r.TargetID, r.TargetRole)
	}
}

func TestUserDelete_LastSuperAdminGuard(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupRoleDeleteHandler(t, logger)
	mustCreateStaffUser(t, db, "lone-sa", "LoneSA", "super-admin", "active", 1000)
	sa := mustCreateStaffSession(t, db, "lone-sa")

	rec := staffDelete(h, sa, "lone-sa")

	if rec.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want 409 (self-deletion of the last active super-admin)", rec.Code)
	}
	if _, err := store.UserByID(t.Context(), db, "lone-sa"); err != nil {
		t.Error("the guarded super-admin must survive")
	}
	records := parseAuditEvents(t, &buf)
	if len(records) != 1 || records[0].Result != "failure" {
		t.Fatalf("blocked deletion must audit as failure, got: %+v", records)
	}
}

// TestUserDelete_SuspendedUserDeletable pins the deletion rule: deletion of
// a suspended account is a normal delete — suspension does not add a
// restriction (has none).
func TestUserDelete_SuspendedUserDeletable(t *testing.T) {
	t.Parallel()
	db, h := setupRoleDeleteHandler(t, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	mustCreateStaffUser(t, db, "admin-a", "AdminA", "admin", "active", 1000)
	mustCreateStaffUser(t, db, "target", "Suspended", "user", "suspended", 2000)
	actor := mustCreateStaffSession(t, db, "admin-a")

	rec := staffDelete(h, actor, "target")

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204 (suspended accounts are deletable)", rec.Code)
	}
}
