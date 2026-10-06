package store

import (
	"errors"
	"sync"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestCreateResetToken_ReplacesExisting(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	u := insertUser(t, db, "resetuser", "reset@example.com", "active", 1)

	d1 := digestOf([]byte("token-one-token-one-token-one-00"))
	d2 := digestOf([]byte("token-two-token-two-token-two-00"))
	now := nowMS()
	if err := CreateResetToken(ctx, db, CreateResetTokenParams{TokenDigest: d1, UserID: u, CreatedAtMS: now, ExpiresAtMS: now + 1000}); err != nil {
		t.Fatalf("first CreateResetToken: %v", err)
	}
	if err := CreateResetToken(ctx, db, CreateResetTokenParams{TokenDigest: d2, UserID: u, CreatedAtMS: now, ExpiresAtMS: now + 2000}); err != nil {
		t.Fatalf("second CreateResetToken: %v", err)
	}

	// Only the second token exists (one active token per user).
	if _, err := ResetTokenUserID(ctx, db, d1, now+500); !errors.Is(err, ErrResetTokenInvalid) {
		t.Errorf("first token lookup: got %v, want ErrResetTokenInvalid", err)
	}
	uid2, err := ResetTokenUserID(ctx, db, d2, now+500)
	if err != nil {
		t.Fatalf("second token lookup: %v", err)
	}
	if uid2 != u {
		t.Errorf("second token user = %q, want %q", uid2, u)
	}
}

func TestResetTokenUserID_Expired(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	u := insertUser(t, db, "expuser", "exp@example.com", "active", 1)
	d := digestOf([]byte("expired-token-expired-token-expired0"))
	now := nowMS()
	if err := CreateResetToken(ctx, db, CreateResetTokenParams{TokenDigest: d, UserID: u, CreatedAtMS: now, ExpiresAtMS: now + 100}); err != nil {
		t.Fatalf("CreateResetToken: %v", err)
	}
	if _, err := ResetTokenUserID(ctx, db, d, now+101); !errors.Is(err, ErrResetTokenInvalid) {
		t.Errorf("expired lookup: got %v, want ErrResetTokenInvalid", err)
	}
}

func TestResetPasswordWithToken_Success(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	u := insertUser(t, db, "pwuser", "pw@example.com", "active", 3)
	if err := seedSession(t, db, u, 3); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	d := digestOf([]byte("good-token-good-token-good-token-00"))
	now := nowMS()
	if err := CreateResetToken(ctx, db, CreateResetTokenParams{TokenDigest: d, UserID: u, CreatedAtMS: now, ExpiresAtMS: now + 1000}); err != nil {
		t.Fatalf("CreateResetToken: %v", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("new-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	if err := ResetPasswordWithToken(ctx, db, ResetPasswordWithTokenParams{
		TokenDigest: d, NowMS: now + 1, CurrentAuthVersion: 3, NewHash: string(hash), UpdatedAtMS: now + 1,
	}); err != nil {
		t.Fatalf("ResetPasswordWithToken: %v", err)
	}

	// Token consumed — a second consume fails.
	if err := ResetPasswordWithToken(ctx, db, ResetPasswordWithTokenParams{
		TokenDigest: d, NowMS: now + 1, CurrentAuthVersion: 4, NewHash: string(hash), UpdatedAtMS: now + 2,
	}); !errors.Is(err, ErrResetTokenInvalid) {
		t.Errorf("second consume: got %v, want ErrResetTokenInvalid", err)
	}

	// Password swapped, version bumped, sessions gone, forced flag cleared.
	user, err := UserByID(ctx, db, u)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if user.AuthVersion != 4 {
		t.Errorf("auth_version = %d, want 4", user.AuthVersion)
	}
	if user.MustChangePassword {
		t.Error("must_change_password still set after self-reset")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte("new-password")); err != nil {
		t.Errorf("new verifier does not match new password: %v", err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE user_id = ?`, u).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if n != 0 {
		t.Errorf("%d sessions remain, want 0", n)
	}
}

func TestResetPasswordWithToken_StampsEmailVerified(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	u := insertUser(t, db, "stampreset", "stampreset@example.com", "active", 3)
	d := digestOf([]byte("stamp-reset-stamp-reset-stamp-rese0"))
	now := nowMS()
	if err := CreateResetToken(ctx, db, CreateResetTokenParams{TokenDigest: d, UserID: u, CreatedAtMS: now, ExpiresAtMS: now + 60_000}); err != nil {
		t.Fatalf("CreateResetToken: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("fresh-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	if err := ResetPasswordWithToken(ctx, db, ResetPasswordWithTokenParams{
		TokenDigest: d, NowMS: now + 1, CurrentAuthVersion: 3, NewHash: string(hash), UpdatedAtMS: now + 1,
	}); err != nil {
		t.Fatalf("ResetPasswordWithToken: %v", err)
	}

	// The self-service reset proved inbox control — the stamp is
	// set in the same transaction.
	user, err := UserByID(ctx, db, u)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if user.EmailVerifiedAtMS == nil || *user.EmailVerifiedAtMS != now+1 {
		t.Errorf("email_verified_at_ms = %v, want %d", user.EmailVerifiedAtMS, now+1)
	}
}

func TestResetPasswordWithToken_KeepsExistingStamp(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	u := insertUser(t, db, "keepstamp", "keepstamp@example.com", "active", 3)
	now := nowMS()
	// Already verified BEFORE the reset (the grandfather/verify path).
	if _, err := db.ExecContext(ctx,
		`UPDATE users SET email_verified_at_ms = ? WHERE id = ?`, now-1000, u,
	); err != nil {
		t.Fatalf("stamp fixture: %v", err)
	}
	d := digestOf([]byte("keep-stamp-keep-stamp-keep-stamp-00"))
	if err := CreateResetToken(ctx, db, CreateResetTokenParams{TokenDigest: d, UserID: u, CreatedAtMS: now, ExpiresAtMS: now + 60_000}); err != nil {
		t.Fatalf("CreateResetToken: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("fresh-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	if err := ResetPasswordWithToken(ctx, db, ResetPasswordWithTokenParams{
		TokenDigest: d, NowMS: now + 1, CurrentAuthVersion: 3, NewHash: string(hash), UpdatedAtMS: now + 1,
	}); err != nil {
		t.Fatalf("ResetPasswordWithToken: %v", err)
	}

	// The COALESCE keeps the original stamp — the reset never rewrites it.
	user, err := UserByID(ctx, db, u)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if user.EmailVerifiedAtMS == nil || *user.EmailVerifiedAtMS != now-1000 {
		t.Errorf("stamp rewritten: %v, want %d", user.EmailVerifiedAtMS, now-1000)
	}
}

func TestResetPasswordWithToken_ConcurrentSingleWinner(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	u := insertUser(t, db, "raceuser", "race@example.com", "active", 1)
	d := digestOf([]byte("race-token-race-token-race-token-000"))
	now := nowMS()
	if err := CreateResetToken(ctx, db, CreateResetTokenParams{TokenDigest: d, UserID: u, CreatedAtMS: now, ExpiresAtMS: now + 60_000}); err != nil {
		t.Fatalf("CreateResetToken: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("racing-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}

	var wg sync.WaitGroup
	results := make([]error, 2)
	for i := range 2 {
		wg.Go(func() {
			results[i] = ResetPasswordWithToken(ctx, db, ResetPasswordWithTokenParams{
				TokenDigest: d, NowMS: now + 1, CurrentAuthVersion: 1, NewHash: string(hash), UpdatedAtMS: now + 1,
			})
		})
	}
	wg.Wait()

	okCount := 0
	for _, err := range results {
		if err == nil {
			okCount++
			continue
		}
		if !errors.Is(err, ErrResetTokenInvalid) {
			t.Errorf("unexpected error: %v", err)
		}
	}
	if okCount != 1 {
		t.Errorf("winners = %d, want exactly 1", okCount)
	}
}

func TestResetPasswordWithToken_SuspendedAccountRejected(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	u := insertUser(t, db, "suspuser", "susp@example.com", "suspended", 5)
	d := digestOf([]byte("susp-token-susp-token-susp-token-000"))
	now := nowMS()
	if err := CreateResetToken(ctx, db, CreateResetTokenParams{TokenDigest: d, UserID: u, CreatedAtMS: now, ExpiresAtMS: now + 1000}); err != nil {
		t.Fatalf("CreateResetToken: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("whatever-123"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	err = ResetPasswordWithToken(ctx, db, ResetPasswordWithTokenParams{
		TokenDigest: d, NowMS: now + 1, CurrentAuthVersion: 5, NewHash: string(hash), UpdatedAtMS: now + 1,
	})
	if !errors.Is(err, ErrResetTokenInvalid) {
		t.Errorf("suspended reset: got %v, want ErrResetTokenInvalid", err)
	}
	// The token died with the attempt — the link is unreplayable.
	if _, err := ResetTokenUserID(ctx, db, d, now+1); !errors.Is(err, ErrResetTokenInvalid) {
		t.Errorf("token lookup after suspended attempt: got %v, want ErrResetTokenInvalid", err)
	}
	// Password untouched.
	user, err := UserByID(ctx, db, u)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if user.AuthVersion != 5 {
		t.Errorf("auth_version changed on rejected reset: %d", user.AuthVersion)
	}
}

func TestResetPasswordWithToken_VersionMismatchRejected(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	u := insertUser(t, db, "casuser", "cas@example.com", "active", 2)
	d := digestOf([]byte("cas-token-cas-token-cas-token-00000"))
	now := nowMS()
	if err := CreateResetToken(ctx, db, CreateResetTokenParams{TokenDigest: d, UserID: u, CreatedAtMS: now, ExpiresAtMS: now + 1000}); err != nil {
		t.Fatalf("CreateResetToken: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("whatever-123"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	// Wrong CAS input (auth_version changed since the load).
	err = ResetPasswordWithToken(ctx, db, ResetPasswordWithTokenParams{
		TokenDigest: d, NowMS: now + 1, CurrentAuthVersion: 9, NewHash: string(hash), UpdatedAtMS: now + 1,
	})
	if !errors.Is(err, ErrResetTokenInvalid) {
		t.Errorf("version mismatch: got %v, want ErrResetTokenInvalid", err)
	}
}

func TestDeleteExpiredResetTokens_BoundedBatch(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	u1 := insertUser(t, db, "sweepuser1", "sweep1@example.com", "active", 1)
	u2 := insertUser(t, db, "sweepuser2", "sweep2@example.com", "active", 1)
	u3 := insertUser(t, db, "sweepuser3", "sweep3@example.com", "active", 1)
	now := nowMS()
	// One expired token per user (the one-active UNIQUE(user_id) rule).
	for i, u := range []string{u1, u2} {
		d := digestOf([]byte{byte(i), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
		if _, err := db.ExecContext(ctx,
			`INSERT INTO password_reset_tokens (token_digest, user_id, created_at_ms, expires_at_ms) VALUES (?, ?, ?, ?)`,
			d, u, now-2000, now-1000,
		); err != nil {
			t.Fatalf("insert expired token: %v", err)
		}
	}
	// A live token survives.
	live := digestOf([]byte("live-token-live-token-live-token-000"))
	if err := CreateResetToken(ctx, db, CreateResetTokenParams{TokenDigest: live, UserID: u3, CreatedAtMS: now, ExpiresAtMS: now + 1000}); err != nil {
		t.Fatalf("CreateResetToken live: %v", err)
	}

	n, err := DeleteExpiredResetTokens(ctx, db, now, 10)
	if err != nil {
		t.Fatalf("DeleteExpiredResetTokens: %v", err)
	}
	if n != 2 {
		t.Errorf("swept = %d, want 2", n)
	}
	if _, err := ResetTokenUserID(ctx, db, live, now); err != nil {
		t.Errorf("live token was swept: %v", err)
	}
}

func TestPasswordChangeAndAdminReset_KillResetTokens(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	u := insertUser(t, db, "killuser", "kill@example.com", "active", 1)
	d := digestOf([]byte("kill-token-kill-token-kill-token-000"))
	now := nowMS()
	if err := CreateResetToken(ctx, db, CreateResetTokenParams{TokenDigest: d, UserID: u, CreatedAtMS: now, ExpiresAtMS: now + 1000}); err != nil {
		t.Fatalf("CreateResetToken: %v", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("changed-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	if err := UpdatePasswordAndRevokeSessions(ctx, db, u, string(hash), 1, now); err != nil {
		t.Fatalf("UpdatePasswordAndRevokeSessions: %v", err)
	}
	if _, err := ResetTokenUserID(ctx, db, d, now); !errors.Is(err, ErrResetTokenInvalid) {
		t.Errorf("token survived password change: got %v", err)
	}

	// Admin reset path: mint again, then ResetUserPassword.
	if err := CreateResetToken(ctx, db, CreateResetTokenParams{TokenDigest: d, UserID: u, CreatedAtMS: now, ExpiresAtMS: now + 1000}); err != nil {
		t.Fatalf("re-CreateResetToken: %v", err)
	}
	if err := ResetUserPassword(ctx, db, u, string(hash), now); err != nil {
		t.Fatalf("ResetUserPassword: %v", err)
	}
	if _, err := ResetTokenUserID(ctx, db, d, now); !errors.Is(err, ErrResetTokenInvalid) {
		t.Errorf("token survived admin reset: got %v", err)
	}
}

func TestResetTokensCascadeOnUserDelete(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	u := insertUser(t, db, "deluser", "del@example.com", "active", 1)
	d := digestOf([]byte("del-token-del-token-del-token-00000"))
	now := nowMS()
	if err := CreateResetToken(ctx, db, CreateResetTokenParams{TokenDigest: d, UserID: u, CreatedAtMS: now, ExpiresAtMS: now + 1000}); err != nil {
		t.Fatalf("CreateResetToken: %v", err)
	}
	if err := DeleteUser(ctx, db, u); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM password_reset_tokens`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("%d token rows remain after user delete", n)
	}
}
