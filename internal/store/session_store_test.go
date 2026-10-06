package store

import (
	"database/sql"
	"errors"
	"slices"
	"testing"

	"sick-fansubs/internal/store/storetest"
)

func TestCreateSession_Success(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Katakuri", "katakuri", "k@example.com")

	digest := make([]byte, 32)
	csrf := make([]byte, 32)
	for i := range digest {
		digest[i] = byte(i)
	}
	for i := range csrf {
		csrf[i] = byte(255 - i)
	}

	err := CreateSession(ctx, db, CreateSessionParams{
		ID:          "sess_01",
		UserID:      "user_01",
		TokenDigest: digest,
		CSRF:        csrf,
		AuthVersion: 1,
		CreatedAtMS: 5000,
		ExpiresAtMS: 5000 + 7*24*3600*1000,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	su, err := SessionByDigest(ctx, db, digest)
	if err != nil {
		t.Fatalf("SessionByDigest: %v", err)
	}
	if su.SessionID != "sess_01" {
		t.Errorf("SessionID: got %q, want %q", su.SessionID, "sess_01")
	}
	if su.UserID != "user_01" {
		t.Errorf("UserID: got %q, want %q", su.UserID, "user_01")
	}
	if su.Username != "Katakuri" {
		t.Errorf("Username: got %q, want %q", su.Username, "Katakuri")
	}
	if su.Role != "user" {
		t.Errorf("Role: got %q, want %q", su.Role, "user")
	}
	if len(su.CSRF) != 32 {
		t.Errorf("CSRF length: got %d, want 32", len(su.CSRF))
	}
	for i, b := range su.CSRF {
		if b != byte(255-i) {
			t.Errorf("CSRF[%d]: got %d, want %d", i, b, 255-i)
		}
	}
	if su.IsExpired(5000) {
		t.Error("session should not be expired at created_at_ms")
	}
}

func TestCreateSession_AuthVersionMismatch(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Katakuri", "katakuri", "k@example.com")

	digest := make([]byte, 32)
	csrf := make([]byte, 32)

	err := CreateSession(ctx, db, CreateSessionParams{
		ID:          "sess_01",
		UserID:      "user_01",
		TokenDigest: digest,
		CSRF:        csrf,
		AuthVersion: 99, // mismatched
		CreatedAtMS: 5000,
		ExpiresAtMS: 5000 + 7*24*3600*1000,
	})
	if !errors.Is(err, ErrAuthVersionMismatch) {
		t.Errorf("got %v, want ErrAuthVersionMismatch", err)
	}
}

func TestCreateSession_SuspendedUser(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:       "user_01",
		Username: "Suspended",
		Status:   "suspended",
	})

	digest := make([]byte, 32)
	csrf := make([]byte, 32)

	err := CreateSession(ctx, db, CreateSessionParams{
		ID:          "sess_01",
		UserID:      "user_01",
		TokenDigest: digest,
		CSRF:        csrf,
		AuthVersion: 1,
		CreatedAtMS: 5000,
		ExpiresAtMS: 5000 + 7*24*3600*1000,
	})
	if !errors.Is(err, ErrAuthVersionMismatch) {
		t.Errorf("got %v, want ErrAuthVersionMismatch for suspended user", err)
	}
}

func TestCreateSession_UserNotFound(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	digest := make([]byte, 32)
	csrf := make([]byte, 32)

	err := CreateSession(ctx, db, CreateSessionParams{
		ID:          "sess_01",
		UserID:      "nonexistent",
		TokenDigest: digest,
		CSRF:        csrf,
		AuthVersion: 1,
		CreatedAtMS: 5000,
		ExpiresAtMS: 5000 + 7*24*3600*1000,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestSessionByDigest_NotFound(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	digest := make([]byte, 32)
	_, err := SessionByDigest(ctx, db, digest)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestSessionByDigest_MustChangePassword(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Katakuri", "katakuri", "k@example.com")

	// Set the flag directly — the realistic reset path would bump
	// auth_version and delete this very session, which is a separate test.
	if _, err := db.Exec(`UPDATE users SET must_change_password = 1 WHERE id = 'user_01'`); err != nil {
		t.Fatalf("set flag: %v", err)
	}

	digest := make([]byte, 32)
	csrf := make([]byte, 32)
	mustCreateSession(t, db, "sess_01", "user_01", digest, csrf, 1, 5000, 5000+7*24*3600*1000)

	su, err := SessionByDigest(ctx, db, digest)
	if err != nil {
		t.Fatalf("SessionByDigest: %v", err)
	}
	if !su.MustChangePassword {
		t.Error("SessionByDigest must project the forced-change flag")
	}

	// And the un-flagged case stays false.
	if _, err := db.Exec(`UPDATE users SET must_change_password = 0 WHERE id = 'user_01'`); err != nil {
		t.Fatalf("clear flag: %v", err)
	}
	su2, err := SessionByDigest(ctx, db, digest)
	if err != nil {
		t.Fatalf("SessionByDigest after clear: %v", err)
	}
	if su2.MustChangePassword {
		t.Error("flag should read false after the clear")
	}
}

// TestSessionByDigest_SuspendedUser pins the u.status = 'active' predicate:
// a session row that exists from BEFORE a suspension must not authenticate
// (CreateSession already refuses to create rows for suspended users — this
// covers the pre-existing-row path).
func TestSessionByDigest_SuspendedUser(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:       "user_01",
		Username: "Suspended",
		Status:   "suspended",
	})

	digest := make([]byte, 32)
	digest[0] = 7
	csrf := make([]byte, 32)
	// Raw insert — CreateSession deliberately refuses suspended users.
	_, err := db.ExecContext(ctx, `INSERT INTO sessions
		(id, user_id, token_digest, csrf_token, auth_version, created_at_ms, expires_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"sess_01", "user_01", digest, csrf, 1, 1000, 1000+7*24*3600*1000)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}

	_, err = SessionByDigest(ctx, db, digest)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("suspended user session: got %v, want ErrNotFound", err)
	}
}

func TestSessionByDigest_Expired(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Katakuri", "katakuri", "k@example.com")

	digest := make([]byte, 32)
	csrf := make([]byte, 32)
	for i := range digest {
		digest[i] = byte(i)
	}

	err := CreateSession(ctx, db, CreateSessionParams{
		ID:          "sess_01",
		UserID:      "user_01",
		TokenDigest: digest,
		CSRF:        csrf,
		AuthVersion: 1,
		CreatedAtMS: 1000,
		ExpiresAtMS: 2000,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	su, err := SessionByDigest(ctx, db, digest)
	if err != nil {
		t.Fatalf("SessionByDigest: %v", err)
	}
	if !su.IsExpired(3000) {
		t.Error("session should be expired at 3000 (expires at 2000)")
	}
	if su.IsExpired(1500) {
		t.Error("session should not be expired at 1500 (expires at 2000)")
	}
}

func TestDeleteSessionByDigest_Existing(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Katakuri", "katakuri", "k@example.com")

	digest := make([]byte, 32)
	csrf := make([]byte, 32)

	mustCreateSession(t, db, "sess_01", "user_01", digest, csrf, 1, 5000, 5000+7*24*3600*1000)

	n, err := DeleteSessionByDigest(ctx, db, digest)
	if err != nil {
		t.Fatalf("DeleteSessionByDigest: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted count: got %d, want 1", n)
	}

	_, err = SessionByDigest(ctx, db, digest)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound after delete", err)
	}
}

func TestDeleteSessionByDigest_NotFound(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	digest := make([]byte, 32)
	n, err := DeleteSessionByDigest(ctx, db, digest)
	if err != nil {
		t.Fatalf("DeleteSessionByDigest: %v", err)
	}
	if n != 0 {
		t.Errorf("deleted count: got %d, want 0", n)
	}
}

func TestDeleteExpiredSessions(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Katakuri", "katakuri", "k@example.com")

	d1, c1 := make([]byte, 32), make([]byte, 32)
	d2, c2 := make([]byte, 32), make([]byte, 32)
	d1[0], d2[0] = 1, 2

	mustCreateSession(t, db, "sess_01", "user_01", d1, c1, 1, 1000, 2000)
	mustCreateSession(t, db, "sess_02", "user_01", d2, c2, 1, 5000, 5000+7*24*3600*1000)

	n, err := DeleteExpiredSessions(ctx, db, 3000, 100)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted count: got %d, want 1", n)
	}

	_, err = SessionByDigest(ctx, db, d1)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expired session: got %v, want ErrNotFound", err)
	}

	_, err = SessionByDigest(ctx, db, d2)
	if err != nil {
		t.Errorf("active session should survive cleanup: %v", err)
	}
}

// TestDeleteExpiredSessions_BoundedBatches pins the bounded-batch
// contract: each call removes at most limit rows, so a caller looping until
// a short batch can never hold the SQLite writer for an unbounded delete.
func TestDeleteExpiredSessions_BoundedBatches(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Katakuri", "katakuri", "k@example.com")

	// Three expired sessions + one active session.
	for i, sessID := range []string{"sess_01", "sess_02", "sess_03"} {
		d, c := make([]byte, 32), make([]byte, 32)
		d[0] = byte(i + 1)
		mustCreateSession(t, db, sessID, "user_01", d, c, 1, 1000, 2000)
	}
	dActive, cActive := make([]byte, 32), make([]byte, 32)
	dActive[0] = 9
	mustCreateSession(t, db, "sess_04", "user_01", dActive, cActive, 1, 5000, 5000+7*24*3600*1000)

	// Batch 1: limit 2 → exactly 2 deleted.
	n, err := DeleteExpiredSessions(ctx, db, 3000, 2)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions batch 1: %v", err)
	}
	if n != 2 {
		t.Errorf("batch 1 deleted: got %d, want 2", n)
	}

	// Batch 2: one expired row remains → 1 deleted.
	n, err = DeleteExpiredSessions(ctx, db, 3000, 2)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions batch 2: %v", err)
	}
	if n != 1 {
		t.Errorf("batch 2 deleted: got %d, want 1", n)
	}

	// Batch 3: nothing expired remains → 0, and the active session survives.
	n, err = DeleteExpiredSessions(ctx, db, 3000, 2)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions batch 3: %v", err)
	}
	if n != 0 {
		t.Errorf("batch 3 deleted: got %d, want 0", n)
	}
	if _, err := SessionByDigest(ctx, db, dActive); err != nil {
		t.Errorf("active session should survive bounded cleanup: %v", err)
	}
}

func TestRehashPasswordAndCreateSession_Success(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Katakuri", "katakuri", "k@example.com")

	digest := make([]byte, 32)
	for i := range digest {
		digest[i] = byte(i)
	}
	csrf := make([]byte, 32)
	csrf[0] = 7

	err := RehashPasswordAndCreateSession(ctx, db, RehashPasswordAndCreateSessionParams{
		UserID:             "user_01",
		CurrentAuthVersion: 1,
		NewHash:            "$2a$12$rehashed-verifier",
		UpdatedAtMS:        6000,
		SessionID:          "sess_01",
		TokenDigest:        digest,
		CSRF:               csrf,
		CreatedAtMS:        6000,
		ExpiresAtMS:        6000 + 7*24*3600*1000,
	})
	if err != nil {
		t.Fatalf("RehashPasswordAndCreateSession: %v", err)
	}

	// The verifier replaced and the version bumped.
	u, err := UserByID(ctx, db, "user_01")
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if u.Password != "$2a$12$rehashed-verifier" {
		t.Errorf("password not rehashed: got %q", u.Password)
	}
	if u.AuthVersion != 2 {
		t.Errorf("auth_version: got %d, want 2", u.AuthVersion)
	}

	// The session exists under the NEW version and is resolvable.
	if _, err := SessionByDigest(ctx, db, digest); err != nil {
		t.Fatalf("SessionByDigest after rehash: %v", err)
	}
}

func TestRehashPasswordAndCreateSession_VersionMismatch(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Katakuri", "katakuri", "k@example.com")

	digest := make([]byte, 32)
	csrf := make([]byte, 32)

	err := RehashPasswordAndCreateSession(ctx, db, RehashPasswordAndCreateSessionParams{
		UserID:             "user_01",
		CurrentAuthVersion: 99, // stale — the version changed concurrently
		NewHash:            "$2a$12$rehashed-verifier",
		UpdatedAtMS:        6000,
		SessionID:          "sess_01",
		TokenDigest:        digest,
		CSRF:               csrf,
		CreatedAtMS:        6000,
		ExpiresAtMS:        6000 + 7*24*3600*1000,
	})
	if !errors.Is(err, ErrAuthVersionMismatch) {
		t.Errorf("got %v, want ErrAuthVersionMismatch", err)
	}

	// The whole transaction must have rolled back: no verifier change, no
	// session row.
	u, lookupErr := UserByID(ctx, db, "user_01")
	if lookupErr != nil {
		t.Fatalf("UserByID: %v", lookupErr)
	}
	if u.AuthVersion != 1 {
		t.Errorf("auth_version changed despite failed tx: got %d, want 1", u.AuthVersion)
	}
	if u.Password != storetest.PasswordPlaceholder {
		t.Errorf("password changed despite failed tx: got %q", u.Password)
	}
	if _, err := SessionByDigest(ctx, db, digest); !errors.Is(err, ErrNotFound) {
		t.Errorf("session created despite failed tx: %v", err)
	}
}

func TestRehashPasswordAndCreateSession_UserNotFound(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	err := RehashPasswordAndCreateSession(ctx, db, RehashPasswordAndCreateSessionParams{
		UserID:             "nonexistent",
		CurrentAuthVersion: 1,
		NewHash:            "$2a$12$rehashed-verifier",
		UpdatedAtMS:        6000,
		SessionID:          "sess_01",
		TokenDigest:        make([]byte, 32),
		CSRF:               make([]byte, 32),
		CreatedAtMS:        6000,
		ExpiresAtMS:        6000 + 7*24*3600*1000,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestRehashPasswordAndCreateSession_SuspendedUser(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	// A suspended user with a matching auth_version: the UPDATE bump would
	// succeed, but the conditional session INSERT must refuse (status guard).
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:       "user_01",
		Username: "Suspended",
		Status:   "suspended",
	})

	digest := make([]byte, 32)
	digest[0] = 3
	err := RehashPasswordAndCreateSession(ctx, db, RehashPasswordAndCreateSessionParams{
		UserID:             "user_01",
		CurrentAuthVersion: 1,
		NewHash:            "$2a$12$rehashed-verifier",
		UpdatedAtMS:        6000,
		SessionID:          "sess_01",
		TokenDigest:        digest,
		CSRF:               make([]byte, 32),
		CreatedAtMS:        6000,
		ExpiresAtMS:        6000 + 7*24*3600*1000,
	})
	if !errors.Is(err, ErrAuthVersionMismatch) {
		t.Errorf("got %v, want ErrAuthVersionMismatch for suspended user", err)
	}
	if _, err := SessionByDigest(ctx, db, digest); !errors.Is(err, ErrNotFound) {
		t.Errorf("session created for suspended user: %v", err)
	}
}

func TestSessionCascadeOnUserDelete(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Katakuri", "katakuri", "k@example.com")

	digest := make([]byte, 32)
	csrf := make([]byte, 32)
	for i := range digest {
		digest[i] = byte(i)
	}

	mustCreateSession(t, db, "sess_01", "user_01", digest, csrf, 1, 5000, 5000+7*24*3600*1000)

	_, err := db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", "user_01")
	if err != nil {
		t.Fatalf("delete user: %v", err)
	}

	_, err = SessionByDigest(ctx, db, digest)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("session should cascade-delete with user, got %v", err)
	}
}

func TestSessionByDigest_AuthVersionMismatch(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Katakuri", "katakuri", "k@example.com")

	digest := make([]byte, 32)
	csrf := make([]byte, 32)
	for i := range digest {
		digest[i] = byte(i)
	}

	// Create session with auth_version = 1.
	mustCreateSession(t, db, "sess_01", "user_01", digest, csrf, 1, 5000, 5000+7*24*3600*1000)

	// Verify findable before bump.
	if _, err := SessionByDigest(ctx, db, digest); err != nil {
		t.Fatalf("SessionByDigest before bump: %v", err)
	}

	// Bump auth_version directly (password change, logout-all, suspension).
	// The statement mirrors the bump inside IncrementAuthVersionAndDeleteSessions
	// — keep the two in lockstep if the users table changes. The session row
	// The session row must SURVIVE so the rejection below is proven to come from the version
	// mismatch, not from the delete.
	if _, err := db.ExecContext(ctx,
		`UPDATE users SET auth_version = auth_version + 1, updated_at_ms = ? WHERE id = ?`,
		6000, "user_01",
	); err != nil {
		t.Fatalf("bump auth version: %v", err)
	}

	// The session row must survive the bump — a bump that deleted the row
	// would pass the rejection check below for the wrong reason.
	var surviving int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions WHERE id = ?`, "sess_01").Scan(&surviving); err != nil {
		t.Fatalf("count sessions after bump: %v", err)
	}
	if surviving != 1 {
		t.Fatalf("session should survive the auth-version bump: got %d rows, want 1", surviving)
	}

	// Session should be rejected — s.auth_version (1) != u.auth_version (2).
	if _, err := SessionByDigest(ctx, db, digest); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound after auth version bump", err)
	}
}

func TestCreateSession_DuplicateDigest(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Katakuri", "katakuri", "k@example.com")

	digest := make([]byte, 32)
	csrf := make([]byte, 32)
	for i := range digest {
		digest[i] = byte(i)
	}

	mustCreateSession(t, db, "sess_01", "user_01", digest, csrf, 1, 5000, 5000+7*24*3600*1000)

	// Second session with same digest.
	err := CreateSession(ctx, db, CreateSessionParams{
		ID:          "sess_02",
		UserID:      "user_01",
		TokenDigest: digest,
		CSRF:        make([]byte, 32),
		AuthVersion: 1,
		CreatedAtMS: 6000,
		ExpiresAtMS: 6000 + 7*24*3600*1000,
	})
	if !errors.Is(err, ErrDuplicate) {
		t.Errorf("got %v, want ErrDuplicate for duplicate digest", err)
	}
}

func TestCreateUserAndSession_Success(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	digest := make([]byte, 32)
	csrf := make([]byte, 32)
	for i := range digest {
		digest[i] = byte(i)
	}
	for i := range csrf {
		csrf[i] = byte(255 - i)
	}

	err := CreateUserAndSession(ctx, db, CreateUserAndSessionParams{
		ID:            "user_01",
		Username:      "Katakuri",
		UsernameCanon: "katakuri",
		Email:         "k@example.com",
		Password:      "$2a$12$...bcrypt-hash...",
		Role:          "user",
		Status:        "active",
		AuthVersion:   1,
		CreatedAtMS:   5000,
		UpdatedAtMS:   5000,
		SessionID:     "sess_01",
		TokenDigest:   digest,
		CSRF:          csrf,
		ExpiresAtMS:   5000 + 7*24*3600*1000,
	})
	if err != nil {
		t.Fatalf("CreateUserAndSession: %v", err)
	}

	// User row exists with the registration defaults.
	u, err := UserByCanonical(ctx, db, "katakuri")
	if err != nil {
		t.Fatalf("UserByCanonical: %v", err)
	}
	if u.Username != "Katakuri" {
		t.Errorf("Username: got %q, want %q", u.Username, "Katakuri")
	}
	if u.Status != "active" {
		t.Errorf("Status: got %q, want %q", u.Status, "active")
	}
	if u.AuthVersion != 1 {
		t.Errorf("AuthVersion: got %d, want 1", u.AuthVersion)
	}

	// The first session resolves under the same version.
	su, err := SessionByDigest(ctx, db, digest)
	if err != nil {
		t.Fatalf("SessionByDigest: %v", err)
	}
	if su.SessionID != "sess_01" {
		t.Errorf("SessionID: got %q, want %q", su.SessionID, "sess_01")
	}
	if su.UserID != "user_01" {
		t.Errorf("UserID: got %q, want %q", su.UserID, "user_01")
	}
}

func TestCreateUserAndSession_SessionConflictRollsBackUser(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	// An existing user owns a session with the ID the new registration
	// will collide on. Its digest is distinct (bytes 0..31) so the
	// rollback assertion below can probe the new registration's own
	// all-zero digest.
	existingDigest := make([]byte, 32)
	for i := range existingDigest {
		existingDigest[i] = byte(i)
	}
	mustCreateUser(t, db, "user_a", "Existing", "existing", "a@example.com")
	mustCreateSession(t, db, "sess_clash", "user_a",
		existingDigest, make([]byte, 32), 1, 5000, 5000+7*24*3600*1000)

	newDigest := make([]byte, 32) // all zeros — distinct from existingDigest
	err := CreateUserAndSession(ctx, db, CreateUserAndSessionParams{
		ID:            "user_b",
		Username:      "NewUser",
		UsernameCanon: "newuser",
		Email:         "b@example.com",
		Password:      "$2a$12$...bcrypt-hash...",
		Role:          "user",
		Status:        "active",
		AuthVersion:   1,
		CreatedAtMS:   6000,
		UpdatedAtMS:   6000,
		SessionID:     "sess_clash", // UNIQUE violation on sessions.id
		TokenDigest:   newDigest,
		CSRF:          make([]byte, 32),
		ExpiresAtMS:   6000 + 7*24*3600*1000,
	})
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("got %v, want ErrDuplicate for the session ID collision", err)
	}

	// The half-created user must not survive the failed session insert.
	_, err = UserByID(ctx, db, "user_b")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("user_b should be rolled back (want ErrNotFound), got %v", err)
	}

	// No session row exists for the new registration's digest.
	_, err = SessionByDigest(ctx, db, newDigest)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound for the rolled-back session digest", err)
	}
}

func TestCreateUserAndSession_DuplicateUser(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Katakuri", "katakuri", "k@example.com")

	digest := make([]byte, 32)
	err := CreateUserAndSession(ctx, db, CreateUserAndSessionParams{
		ID:            "user_02",
		Username:      "Katakuri",
		UsernameCanon: "katakuri", // taken
		Email:         "different@example.com",
		Password:      "$2a$12$...bcrypt-hash...",
		Role:          "user",
		Status:        "active",
		AuthVersion:   1,
		CreatedAtMS:   6000,
		UpdatedAtMS:   6000,
		SessionID:     "sess_02",
		TokenDigest:   digest,
		CSRF:          make([]byte, 32),
		ExpiresAtMS:   6000 + 7*24*3600*1000,
	})
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("got %v, want ErrDuplicate for duplicate username_canon", err)
	}

	// No session row was created for the rejected registration.
	_, err = SessionByDigest(ctx, db, digest)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound for the rejected registration's session", err)
	}
}

func TestCreateUserAndSession_NonActiveStatusRollsBack(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	err := CreateUserAndSession(ctx, db, CreateUserAndSessionParams{
		ID:            "user_01",
		Username:      "Katakuri",
		UsernameCanon: "katakuri",
		Email:         "k@example.com",
		Password:      "$2a$12$...bcrypt-hash...",
		Role:          "user",
		Status:        "suspended", // inconsistent birth status
		AuthVersion:   1,
		CreatedAtMS:   5000,
		UpdatedAtMS:   5000,
		SessionID:     "sess_01",
		TokenDigest:   make([]byte, 32),
		CSRF:          make([]byte, 32),
		ExpiresAtMS:   5000 + 7*24*3600*1000,
	})
	if !errors.Is(err, ErrAuthVersionMismatch) {
		t.Fatalf("got %v, want ErrAuthVersionMismatch for non-active birth status", err)
	}

	// An account that cannot hold its first session must not be created.
	_, err = UserByID(ctx, db, "user_01")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("user should be rolled back (want ErrNotFound), got %v", err)
	}
}

func TestIsUniqueViolation_Canary(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	// The canary drives the registration path, the only user-insert path
	// production keeps, so the driver error it pins is the one callers see.
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:       "user_01",
		Username: "Katakuri",
		Email:    "a@example.com",
	})

	// Trigger a real UNIQUE violation (same username_canon).
	err := CreateUserAndSession(ctx, db, CreateUserAndSessionParams{
		ID:            "user_02",
		Username:      "Katakuri",
		UsernameCanon: "katakuri",
		Email:         "b@example.com",
		Password:      "$2a$12$...",
		Role:          "user",
		Status:        "active",
		AuthVersion:   1,
		CreatedAtMS:   1000,
		UpdatedAtMS:   1000,
		SessionID:     "sess_01",
		TokenDigest:   make([]byte, 32),
		CSRF:          make([]byte, 32),
		ExpiresAtMS:   9_000_000_000_000,
	})
	if err == nil {
		t.Fatal("got nil, want an error from duplicate username")
	}

	// Canary: verify IsUniqueViolation detects the real driver error.
	if !IsUniqueViolation(err) {
		t.Errorf("IsUniqueViolation returned false for a real UNIQUE violation: %v", err)
		t.Error("the SQLite driver may have changed its error message format — ErrDuplicate detection is at risk")
	}

	// Verify the sentinel is preserved through error wrapping.
	if !errors.Is(err, ErrDuplicate) {
		t.Errorf("got %v, want ErrDuplicate in chain", err)
	}
}

// TestListSessions_WindowOrderAndKeyset pins the list
// contract: the expiry window (an expired row is absent BEFORE cleanup runs),
// newest-first order with the id tie-break, the keyset continuation, and the
// owner filter.
func TestListSessions_WindowOrderAndKeyset(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "u1", "Katakuri", "katakuri", "k@example.com")
	mustCreateUser(t, db, "u2", "Other", "other", "o@example.com")

	const nowMS = 100_000
	const active = nowMS + 1000

	createSessionRow(t, db, "s1", "u1", 1, "Chrome · Android", 1000, active)
	createSessionRow(t, db, "s2", "u1", 2, "", 3000, active)
	createSessionRow(t, db, "s3", "u1", 3, "Safari · iOS", 3000, active)
	// Expired rows: strictly before now, and exactly at now (expired when
	// now >= expires_at).
	createSessionRow(t, db, "s4", "u1", 4, "Firefox · Windows", 2000, 5_000)
	createSessionRow(t, db, "s6", "u1", 6, "Opera · Linux", 1500, nowMS)
	// Another user's live session must never appear.
	createSessionRow(t, db, "s5", "u2", 5, "Edge · Windows", 9000, active)

	page1, hasNext, err := ListSessions(ctx, db, "u1", nowMS, 2, nil)
	if err != nil {
		t.Fatalf("ListSessions page 1: %v", err)
	}
	if !hasNext {
		t.Error("page 1: hasNextPage = false, want true")
	}
	if len(page1) != 2 || page1[0].ID != "s2" || page1[1].ID != "s3" {
		t.Fatalf("page 1 items = %+v, want [s2 s3] (same created_at_ms, id ascending)", page1)
	}
	if page1[0].ClientLabel != "" {
		t.Errorf("s2 label = %q, want empty for a NULL column", page1[0].ClientLabel)
	}
	if page1[1].ClientLabel != "Safari · iOS" {
		t.Errorf("s3 label = %q, want %q", page1[1].ClientLabel, "Safari · iOS")
	}

	page2, hasNext, err := ListSessions(ctx, db, "u1", nowMS, 2, &SessionPageKey{CreatedAtMS: page1[len(page1)-1].CreatedAtMS, ID: page1[len(page1)-1].ID})
	if err != nil {
		t.Fatalf("ListSessions page 2: %v", err)
	}
	if hasNext {
		t.Error("page 2: hasNextPage = true, want false")
	}
	if len(page2) != 1 || page2[0].ID != "s1" {
		t.Fatalf("page 2 items = %+v, want [s1]", page2)
	}
	if page2[0].ClientLabel != "Chrome · Android" {
		t.Errorf("s1 label = %q, want %q", page2[0].ClientLabel, "Chrome · Android")
	}

	// One wide page shows exactly the live, owned rows — the expired pair and
	// the other user's session stay out.
	all, hasNext, err := ListSessions(ctx, db, "u1", nowMS, 100, nil)
	if err != nil {
		t.Fatalf("ListSessions wide: %v", err)
	}
	if hasNext {
		t.Error("wide page: hasNextPage = true, want false")
	}
	ids := make([]string, 0, len(all))
	for _, it := range all {
		ids = append(ids, it.ID)
	}
	if !slices.Equal(ids, []string{"s2", "s3", "s1"}) {
		t.Errorf("wide page ids = %v, want [s2 s3 s1]", ids)
	}
}

// TestCountSessions_WindowAndOwner proves the count applies the same expiry
// window and owner scope as the list read: an expired row and another user's
// live row are both invisible to the count.
func TestCountSessions_WindowAndOwner(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "u1", "Katakuri", "katakuri", "k@example.com")
	mustCreateUser(t, db, "u2", "Other", "other", "o@example.com")

	const nowMS = 100_000
	const active = nowMS + 1000

	createSessionRow(t, db, "s1", "u1", 1, "", 1000, active)
	createSessionRow(t, db, "s2", "u1", 2, "", 2000, 5_000) // expired before nowMS
	createSessionRow(t, db, "s3", "u2", 3, "", 3000, active)

	n, err := CountSessions(ctx, db, "u1", nowMS)
	if err != nil {
		t.Fatalf("CountSessions: %v", err)
	}
	if n != 1 {
		t.Errorf("count = %d, want 1 (the one live owned row)", n)
	}

	all, _, err := ListSessions(ctx, db, "u1", nowMS, 100, nil)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(all) != n {
		t.Errorf("list returned %d rows, count says %d — the mirror drifted", len(all), n)
	}
}

// TestRevokeSession_ScopesToOwner pins the owner scope: only the caller's own
// row can be deleted, a foreign id deletes nothing, and a repeat revoke is an
// idempotent no-op. The owner's other sessions and auth_version survive.
func TestRevokeSession_ScopesToOwner(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "u1", "Katakuri", "katakuri", "k@example.com")
	mustCreateUser(t, db, "u2", "Other", "other", "o@example.com")

	const nowMS = 100_000
	const active = nowMS + 1000
	revokedDigest := createSessionRow(t, db, "s1", "u1", 1, "", 1000, active)
	keptDigest := createSessionRow(t, db, "s2", "u1", 2, "", 2000, active)
	createSessionRow(t, db, "s3", "u2", 3, "", 3000, active)

	// A foreign id removes nothing, and says so with the same count a
	// repeat revoke returns.
	n, err := RevokeSession(ctx, db, "u1", "s3")
	if err != nil {
		t.Fatalf("RevokeSession foreign: %v", err)
	}
	if n != 0 {
		t.Errorf("foreign revoke rows = %d, want 0", n)
	}
	if got := sessionRowCount(t, db, "u2"); got != 1 {
		t.Errorf("u2 sessions = %d, want 1 (a foreign id must not revoke)", got)
	}

	n, err = RevokeSession(ctx, db, "u1", "s1")
	if err != nil {
		t.Fatalf("RevokeSession own: %v", err)
	}
	if n != 1 {
		t.Errorf("own revoke rows = %d, want 1", n)
	}

	n, err = RevokeSession(ctx, db, "u1", "s1")
	if err != nil {
		t.Fatalf("RevokeSession repeat: %v", err)
	}
	if n != 0 {
		t.Errorf("repeat revoke rows = %d, want 0 (idempotent)", n)
	}

	// The revoked digest is dead; the surviving session still resolves, and
	// no auth_version bump happened (a bump would kill every session — the
	// per-session revocation contract).
	if _, err := SessionByDigest(ctx, db, revokedDigest); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoked digest lookup: err = %v, want ErrNotFound", err)
	}
	if _, err := SessionByDigest(ctx, db, keptDigest); err != nil {
		t.Errorf("surviving session lookup: %v", err)
	}
	var authVersion int64
	if err := db.QueryRow(`SELECT auth_version FROM users WHERE id = 'u1'`).Scan(&authVersion); err != nil {
		t.Fatalf("read auth_version: %v", err)
	}
	if authVersion != 1 {
		t.Errorf("auth_version = %d, want 1 (per-session revoke must not bump it)", authVersion)
	}
}

// sessionRowCount counts a user's session rows directly — the list window
// would hide expired rows and this helper must not.
func sessionRowCount(t *testing.T, db *sql.DB, userID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, userID).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return n
}
