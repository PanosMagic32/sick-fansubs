package handler_test

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store"
)

// setupSuspendHandler creates a fresh migrated database and the suspend +
// reactivate endpoints behind the production users-subtree chain
// (RequestID → TrustedOrigin → Session → ForcePasswordChange → CSRF).
func setupSuspendHandler(t *testing.T, logger *slog.Logger) (*sql.DB, http.Handler) {
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
	mux.HandleFunc("POST /api/v1/users/{id}/suspend", handler.UserSuspend(db, publicBase))
	mux.HandleFunc("POST /api/v1/users/{id}/reactivate", handler.UserReactivate(db, publicBase))

	var h http.Handler = mux
	h = middleware.CSRF()(h)
	h = middleware.ForcePasswordChange()(h)
	h = middleware.Session(db, false)(h)
	h = middleware.TrustedOrigin("http://localhost:5173")(h)
	h = middleware.RequestID()(h)

	return db, logContext(logger, h)
}

// staffStatusChange issues POST /api/v1/users/{id}/{action} as the actor.
func staffStatusChange(h http.Handler, actorCookie, id, action string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+id+"/"+action, nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("X-CSRF-Token", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	if actorCookie != "" {
		req.AddCookie(&http.Cookie{Name: "sf_session", Value: actorCookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestUserSuspend_Unauthenticated(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	_, h := setupSuspendHandler(t, logger)

	rec := staffStatusChange(h, "", "aaaaaaaaaaaaaaaaaaaaaaaa", "suspend")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
	if records := parseAuditEvents(t, &buf); len(records) != 0 {
		t.Errorf("unauthenticated attempt must not audit, got: %+v", records)
	}
}

func TestUserSuspend_InvalidID(t *testing.T) {
	t.Parallel()
	db, h := setupSuspendHandler(t, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	mustCreateStaffUser(t, db, "actor", "AdminOne", "admin", "active", 1000)
	actor := mustCreateStaffSession(t, db, "actor")

	rec := staffStatusChange(h, actor, "not%20a%20valid%20id", "suspend")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400 (id shape rejected before lookup)", rec.Code)
	}
}

func TestUserSuspend_NotFoundMasked(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupSuspendHandler(t, logger)
	mustCreateStaffUser(t, db, "actor", "AdminOne", "admin", "active", 1000)
	actor := mustCreateStaffSession(t, db, "actor")

	rec := staffStatusChange(h, actor, "aaaaaaaaaaaaaaaaaaaaaaaa", "suspend")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", rec.Code)
	}
	if records := parseAuditEvents(t, &buf); len(records) != 0 {
		t.Errorf("masked 404 must not audit, got: %+v", records)
	}
}

func TestUserSuspend_ModeratorPeerForbiddenAudited(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupSuspendHandler(t, logger)
	mustCreateStaffUser(t, db, "mod-a", "ModA", "moderator", "active", 1000)
	mustCreateStaffUser(t, db, "mod-b", "ModB", "moderator", "active", 2000)
	actor := mustCreateStaffSession(t, db, "mod-a")

	rec := staffStatusChange(h, actor, "mod-b", "suspend")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want 403 (moderators manage users only)", rec.Code)
	}
	records := parseAuditEvents(t, &buf)
	if len(records) != 1 {
		t.Fatalf("blocked attempt must audit exactly once, got %d: %+v", len(records), records)
	}
	r := records[0]
	if r.Event != "user_suspended" || r.Result != "failure" {
		t.Errorf("event/result: got %q/%q, want user_suspended/failure", r.Event, r.Result)
	}
	if r.ActorID != "mod-a" || r.TargetID != "mod-b" || r.TargetRole != "moderator" {
		t.Errorf("actor/target/role: got %q/%q/%q, want mod-a/mod-b/moderator", r.ActorID, r.TargetID, r.TargetRole)
	}
}

func TestUserSuspend_AdminPeerForbiddenAudited(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupSuspendHandler(t, logger)
	mustCreateStaffUser(t, db, "admin-a", "AdminA", "admin", "active", 1000)
	mustCreateStaffUser(t, db, "admin-b", "AdminB", "admin", "active", 2000)
	actor := mustCreateStaffSession(t, db, "admin-a")

	rec := staffStatusChange(h, actor, "admin-b", "suspend")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want 403 (admins cannot touch admins)", rec.Code)
	}
	if records := parseAuditEvents(t, &buf); len(records) != 1 || records[0].TargetRole != "admin" {
		t.Fatalf("blocked attempt must audit with the target's role snapshot, got: %+v", records)
	}
}

func TestUserSuspend_ModeratorSuspendsUserSuccess(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupSuspendHandler(t, logger)
	mustCreateStaffUser(t, db, "mod", "ModOne", "moderator", "active", 1000)
	mustCreateStaffUser(t, db, "target", "PlainUser", "user", "active", 2000)
	actor := mustCreateStaffSession(t, db, "mod")
	// The target has a live session that the suspension must delete.
	mustCreateStaffSession(t, db, "target")
	// avatarUrl is NOT role-gated (unlike email): a moderator viewer gets
	// the absolute served URL.
	if _, err := db.Exec(`UPDATE users
		SET avatar_url = 'media/images/ab/abcdef0123456789abcdef0123456789.jpg'
		WHERE id = 'target'`); err != nil {
		t.Fatalf("set avatar: %v", err)
	}

	rec := staffStatusChange(h, actor, "target", "suspend")

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ID        string  `json:"id"`
		Email     *string `json:"email"`
		Role      string  `json:"role"`
		Status    string  `json:"status"`
		AvatarURL *string `json:"avatarUrl"`
	}
	decodeJSON(t, rec.Body, &resp)
	if resp.Status != identity.StatusSuspended || resp.ID != "target" {
		t.Errorf("projection: got status %q id %q, want suspended/target", resp.Status, resp.ID)
	}
	// A moderator viewer never receives the email field at all.
	if resp.Email != nil {
		t.Errorf("moderator viewer must not receive email, got %q", *resp.Email)
	}
	wantAvatar := "https://fans.example/media/images/abcdef0123456789abcdef0123456789.jpg"
	if resp.AvatarURL == nil || *resp.AvatarURL != wantAvatar {
		t.Errorf("avatarUrl: got %v, want %q (every viewer, not just admin+)", resp.AvatarURL, wantAvatar)
	}

	// The suspension persisted: status flipped, version bumped, sessions gone.
	target, err := store.UserByID(t.Context(), db, "target")
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if target.Status != identity.StatusSuspended {
		t.Errorf("status: got %q, want suspended", target.Status)
	}
	if target.AuthVersion != 2 {
		t.Errorf("auth_version: got %d, want 2", target.AuthVersion)
	}
	if _, err := store.SessionByDigest(t.Context(), db, sessionDigestOf("target")); err == nil {
		t.Error("the target's session must be revoked by the suspension")
	}
	// The suspended account cannot bootstrap a new session either.
	if err := store.CreateSession(t.Context(), db, store.CreateSessionParams{
		ID: "sess-new", UserID: "target", TokenDigest: make([]byte, 32),
		CSRF: make([]byte, 32), AuthVersion: 2, CreatedAtMS: 3_000, ExpiresAtMS: 3_100,
	}); err == nil {
		t.Error("a suspended account must not create a new session")
	}

	// Success audit: actor + target + the unchanged role snapshot.
	records := parseAuditEvents(t, &buf)
	if len(records) != 1 {
		t.Fatalf("got %d: %+v, want exactly 1 audit event", len(records), records)
	}
	r := records[0]
	if r.Event != "user_suspended" || r.Result != "success" {
		t.Errorf("event/result: got %q/%q, want user_suspended/success", r.Event, r.Result)
	}
	if r.ActorID != "mod" || r.TargetID != "target" || r.TargetRole != "user" {
		t.Errorf("actor/target/role: got %q/%q/%q, want mod/target/user", r.ActorID, r.TargetID, r.TargetRole)
	}
}

func TestUserSuspend_LastActiveSuperAdminGuard(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupSuspendHandler(t, logger)
	mustCreateStaffUser(t, db, "lone-sa", "LoneSA", "super-admin", "active", 1000)
	actor := mustCreateStaffSession(t, db, "lone-sa")

	// Self-suspension of the only active super-admin → the guarded 409.
	rec := staffStatusChange(h, actor, "lone-sa", "suspend")

	if rec.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want 409 (last active super-admin guard)", rec.Code)
	}
	var problem struct {
		Type string `json:"type"`
	}
	decodeJSON(t, rec.Body, &problem)
	if problem.Type != "/problems/auth/last-super-admin" {
		t.Errorf("problem type: got %q, want /problems/auth/last-super-admin", problem.Type)
	}
	records := parseAuditEvents(t, &buf)
	if len(records) != 1 || records[0].Event != "user_suspended" || records[0].Result != "failure" {
		t.Fatalf("guarded 409 must audit as user_suspended failure, got: %+v", records)
	}
}

func TestUserSuspend_SecondSuperAdminAllowed(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupSuspendHandler(t, logger)
	mustCreateStaffUser(t, db, "sa-a", "SuperA", "super-admin", "active", 1000)
	mustCreateStaffUser(t, db, "sa-b", "SuperB", "super-admin", "active", 2000)
	actor := mustCreateStaffSession(t, db, "sa-a")

	rec := staffStatusChange(h, actor, "sa-b", "suspend")

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (one active super-admin remains)", rec.Code)
	}
	if records := parseAuditEvents(t, &buf); len(records) != 1 || records[0].Result != "success" {
		t.Fatalf("success must audit once, got: %+v", records)
	}
}

func TestUserSuspend_NoOpSameStatus(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupSuspendHandler(t, logger)
	mustCreateStaffUser(t, db, "admin-a", "AdminA", "admin", "active", 1000)
	mustCreateStaffUser(t, db, "target", "AlreadySuspended", "user", "suspended", 2000)
	actor := mustCreateStaffSession(t, db, "admin-a")

	rec := staffStatusChange(h, actor, "target", "suspend")

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (same-status no-op)", rec.Code)
	}
	// No audit — a no-op is not a state change.
	if records := parseAuditEvents(t, &buf); len(records) != 0 {
		t.Errorf("no-op must not audit, got: %+v", records)
	}
	// No version bump, no session revocation.
	target, err := store.UserByID(t.Context(), db, "target")
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if target.AuthVersion != 1 {
		t.Errorf("no-op must not bump auth_version, got %d", target.AuthVersion)
	}
}

func TestUserSuspend_NoOpUnauthorizedStill403(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupSuspendHandler(t, logger)
	mustCreateStaffUser(t, db, "mod", "ModOne", "moderator", "active", 1000)
	mustCreateStaffUser(t, db, "target", "SuspendedMod", "moderator", "suspended", 2000)
	actor := mustCreateStaffSession(t, db, "mod")

	// A moderator addressing a peer moderator is forbidden even when the
	// target's status already matches — authorization precedes no-ops.
	rec := staffStatusChange(h, actor, "target", "suspend")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want 403 (authorization precedes even no-ops)", rec.Code)
	}
	if records := parseAuditEvents(t, &buf); len(records) != 1 || records[0].Result != "failure" {
		t.Fatalf("unauthorized attempt must audit as failure, got: %+v", records)
	}
}

// TestUserSuspend_WriteFailureAudited pins the failure-result class for a
// write that fails AFTER authorization: the attempted suspension is audited
// as user_suspended/failure. The trigger forces only the UPDATE to abort —
// closing the database would fail the session lookup first and answer 401
// before the handler runs.
func TestUserSuspend_WriteFailureAudited(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupSuspendHandler(t, logger)
	mustCreateStaffUser(t, db, "admin-a", "AdminA", "admin", "active", 1000)
	mustCreateStaffUser(t, db, "target", "TargetUser", "user", "active", 2000)
	actor := mustCreateStaffSession(t, db, "admin-a")

	if _, err := db.Exec(`CREATE TRIGGER fail_suspend BEFORE UPDATE ON users
		WHEN NEW.status = 'suspended'
		BEGIN SELECT RAISE(ABORT, 'injected write failure'); END`); err != nil {
		t.Fatalf("install failure trigger: %v", err)
	}

	rec := staffStatusChange(h, actor, "target", "suspend")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500 (body %q)", rec.Code, rec.Body.String())
	}
	records := parseAuditEvents(t, &buf)
	if len(records) != 1 {
		t.Fatalf("got %d audit events: %+v, want exactly 1", len(records), records)
	}
	r := records[0]
	if r.Event != "user_suspended" || r.Result != "failure" {
		t.Errorf("event/result: got %q/%q, want user_suspended/failure", r.Event, r.Result)
	}
	if r.ActorID != "admin-a" || r.TargetID != "target" || r.TargetRole != "user" {
		t.Errorf("actor/target/role: got %q/%q/%q, want admin-a/target/user", r.ActorID, r.TargetID, r.TargetRole)
	}
}

func TestUserReactivate_Unauthenticated(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	_, h := setupSuspendHandler(t, logger)

	rec := staffStatusChange(h, "", "aaaaaaaaaaaaaaaaaaaaaaaa", "reactivate")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
	if records := parseAuditEvents(t, &buf); len(records) != 0 {
		t.Errorf("unauthenticated attempt must not audit, got: %+v", records)
	}
}

func TestUserReactivate_NotFoundMasked(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupSuspendHandler(t, logger)
	mustCreateStaffUser(t, db, "actor", "AdminOne", "admin", "active", 1000)
	actor := mustCreateStaffSession(t, db, "actor")

	rec := staffStatusChange(h, actor, "aaaaaaaaaaaaaaaaaaaaaaaa", "reactivate")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", rec.Code)
	}
	if records := parseAuditEvents(t, &buf); len(records) != 0 {
		t.Errorf("masked 404 must not audit, got: %+v", records)
	}
}

func TestUserReactivate_ModeratorPeerForbiddenAudited(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupSuspendHandler(t, logger)
	mustCreateStaffUser(t, db, "mod", "ModOne", "moderator", "active", 1000)
	mustCreateStaffUser(t, db, "target", "SuspendedMod", "moderator", "suspended", 2000)
	actor := mustCreateStaffSession(t, db, "mod")

	rec := staffStatusChange(h, actor, "target", "reactivate")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want 403 (moderators manage users only)", rec.Code)
	}
	records := parseAuditEvents(t, &buf)
	if len(records) != 1 || records[0].Event != "user_reactivated" || records[0].Result != "failure" {
		t.Fatalf("blocked attempt must audit as user_reactivated failure, got: %+v", records)
	}
}

func TestUserReactivate_Success(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupSuspendHandler(t, logger)
	mustCreateStaffUser(t, db, "admin-a", "AdminA", "admin", "active", 1000)
	mustCreateStaffUser(t, db, "target", "SuspendedUser", "user", "suspended", 2000)
	actor := mustCreateStaffSession(t, db, "admin-a")

	rec := staffStatusChange(h, actor, "target", "reactivate")

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	decodeJSON(t, rec.Body, &resp)
	if resp.Status != identity.StatusActive || resp.ID != "target" {
		t.Errorf("projection: got status %q id %q, want active/target", resp.Status, resp.ID)
	}

	// Reactivation sets active and bumps auth_version (a new
	// sign-in is required even though suspension already removed the rows).
	target, err := store.UserByID(t.Context(), db, "target")
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if target.Status != identity.StatusActive {
		t.Errorf("status: got %q, want active", target.Status)
	}
	if target.AuthVersion != 2 {
		t.Errorf("auth_version: got %d, want 2", target.AuthVersion)
	}

	records := parseAuditEvents(t, &buf)
	if len(records) != 1 {
		t.Fatalf("got %d: %+v, want exactly 1 audit event", len(records), records)
	}
	r := records[0]
	if r.Event != "user_reactivated" || r.Result != "success" {
		t.Errorf("event/result: got %q/%q, want user_reactivated/success", r.Event, r.Result)
	}
	if r.ActorID != "admin-a" || r.TargetID != "target" || r.TargetRole != "user" {
		t.Errorf("actor/target/role: got %q/%q/%q, want admin-a/target/user", r.ActorID, r.TargetID, r.TargetRole)
	}
}

func TestUserReactivate_NoOpSameStatus(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupSuspendHandler(t, logger)
	mustCreateStaffUser(t, db, "admin-a", "AdminA", "admin", "active", 1000)
	mustCreateStaffUser(t, db, "target", "ActiveUser", "user", "active", 2000)
	actor := mustCreateStaffSession(t, db, "admin-a")
	mustCreateStaffSession(t, db, "target")

	rec := staffStatusChange(h, actor, "target", "reactivate")

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (same-status no-op)", rec.Code)
	}
	if records := parseAuditEvents(t, &buf); len(records) != 0 {
		t.Errorf("no-op must not audit, got: %+v", records)
	}
	// The no-op must NOT bump auth_version: a bump on an already-active
	// account would revoke its live sessions for nothing.
	target, err := store.UserByID(t.Context(), db, "target")
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if target.AuthVersion != 1 {
		t.Errorf("no-op must not bump auth_version, got %d", target.AuthVersion)
	}
	if _, err := store.SessionByDigest(t.Context(), db, sessionDigestOf("target")); err != nil {
		t.Error("the no-op must leave the target's live session intact")
	}
}

func TestUserReactivate_NoOpUnauthorizedStill403(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupSuspendHandler(t, logger)
	mustCreateStaffUser(t, db, "mod", "ModOne", "moderator", "active", 1000)
	mustCreateStaffUser(t, db, "target", "ActiveMod", "moderator", "active", 2000)
	actor := mustCreateStaffSession(t, db, "mod")

	// A moderator addressing a peer moderator is forbidden even when the
	// target's status already matches — authorization precedes no-ops.
	rec := staffStatusChange(h, actor, "target", "reactivate")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want 403 (authorization precedes even no-ops)", rec.Code)
	}
	if records := parseAuditEvents(t, &buf); len(records) != 1 || records[0].Result != "failure" {
		t.Fatalf("unauthorized attempt must audit as failure, got: %+v", records)
	}
}

// TestUserReactivate_NoGuard pins that reactivation never trips the
// last-active-super-admin guard: with a single ACTIVE super-admin, restoring
// a suspended peer super-admin only ADDS to the active set.
func TestUserReactivate_NoGuard(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, h := setupSuspendHandler(t, logger)
	mustCreateStaffUser(t, db, "sa-a", "SuperA", "super-admin", "active", 1000)
	mustCreateStaffUser(t, db, "sa-b", "SuperB", "super-admin", "suspended", 2000)
	actor := mustCreateStaffSession(t, db, "sa-a")

	rec := staffStatusChange(h, actor, "sa-b", "reactivate")

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (reactivation cannot reduce the active set)", rec.Code)
	}
	if records := parseAuditEvents(t, &buf); len(records) != 1 || records[0].Result != "success" {
		t.Fatalf("success must audit once, got: %+v", records)
	}
}
