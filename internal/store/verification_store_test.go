package store

import (
	"errors"
	"sync"
	"testing"
)

func TestCreateVerificationToken_ReplacesExisting(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	u := insertUser(t, db, "verifyuser", "verify@example.com", "active", 1)

	d1 := digestOf([]byte("verify-one-verify-one-verify-one-00"))
	d2 := digestOf([]byte("verify-two-verify-two-verify-two-00"))
	now := nowMS()
	if err := CreateVerificationToken(ctx, db, CreateVerificationTokenParams{TokenDigest: d1, UserID: u, CreatedAtMS: now, ExpiresAtMS: now + 1000}); err != nil {
		t.Fatalf("first CreateVerificationToken: %v", err)
	}
	if err := CreateVerificationToken(ctx, db, CreateVerificationTokenParams{TokenDigest: d2, UserID: u, CreatedAtMS: now, ExpiresAtMS: now + 2000}); err != nil {
		t.Fatalf("second CreateVerificationToken: %v", err)
	}

	// Only the second token exists (one active token per user).
	if _, err := VerificationTokenUserID(ctx, db, d1, now+500); !errors.Is(err, ErrVerificationTokenInvalid) {
		t.Errorf("first token lookup: got %v, want ErrVerificationTokenInvalid", err)
	}
	uid2, err := VerificationTokenUserID(ctx, db, d2, now+500)
	if err != nil {
		t.Fatalf("second token lookup: %v", err)
	}
	if uid2 != u {
		t.Errorf("second token user = %q, want %q", uid2, u)
	}
}

func TestVerifyEmail_SuccessAndSecondUseFails(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	u := insertUser(t, db, "stampuser", "stamp@example.com", "active", 1)
	d := digestOf([]byte("good-verify-good-verify-good-verify0"))
	now := nowMS()
	if err := CreateVerificationToken(ctx, db, CreateVerificationTokenParams{TokenDigest: d, UserID: u, CreatedAtMS: now, ExpiresAtMS: now + 60_000}); err != nil {
		t.Fatalf("CreateVerificationToken: %v", err)
	}

	if err := VerifyEmail(ctx, db, VerifyEmailParams{TokenDigest: d, NowMS: now + 1}); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}

	// Stamp applied, token consumed, no auth_version bump.
	user, err := UserByID(ctx, db, u)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if user.EmailVerifiedAtMS == nil {
		t.Fatal("email_verified_at_ms still NULL after verify")
	}
	if *user.EmailVerifiedAtMS != now+1 {
		t.Errorf("stamp = %d, want %d", *user.EmailVerifiedAtMS, now+1)
	}
	if user.AuthVersion != 1 {
		t.Errorf("auth_version bumped on verify: %d, want 1", user.AuthVersion)
	}

	// Second use of the consumed token → generic invalid.
	if err := VerifyEmail(ctx, db, VerifyEmailParams{TokenDigest: d, NowMS: now + 2}); !errors.Is(err, ErrVerificationTokenInvalid) {
		t.Errorf("second consume: got %v, want ErrVerificationTokenInvalid", err)
	}
}

func TestVerifyEmail_AlreadyVerifiedIdempotent(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	u := insertUser(t, db, "idemuser", "idem@example.com", "active", 1)
	now := nowMS()
	// The reset path stamps verified while the verification token stays
	// valid — the old link must still answer success.
	if _, err := db.ExecContext(ctx,
		`UPDATE users SET email_verified_at_ms = ? WHERE id = ?`, now, u,
	); err != nil {
		t.Fatalf("stamp fixture: %v", err)
	}
	d := digestOf([]byte("idem-verify-idem-verify-idem-verify"))
	if err := CreateVerificationToken(ctx, db, CreateVerificationTokenParams{TokenDigest: d, UserID: u, CreatedAtMS: now, ExpiresAtMS: now + 60_000}); err != nil {
		t.Fatalf("CreateVerificationToken: %v", err)
	}

	if err := VerifyEmail(ctx, db, VerifyEmailParams{TokenDigest: d, NowMS: now + 1}); err != nil {
		t.Fatalf("idempotent VerifyEmail: %v", err)
	}
	user, err := UserByID(ctx, db, u)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if user.EmailVerifiedAtMS == nil || *user.EmailVerifiedAtMS != now {
		t.Errorf("stamp changed on idempotent verify: %v", user.EmailVerifiedAtMS)
	}
	// The stale token was consumed.
	if _, err := VerificationTokenUserID(ctx, db, d, now+1); !errors.Is(err, ErrVerificationTokenInvalid) {
		t.Errorf("token lookup after idempotent verify: got %v, want ErrVerificationTokenInvalid", err)
	}
}

func TestVerifyEmail_SuspendedAccountRejected(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	u := insertUser(t, db, "suspendverify", "suspverify@example.com", "suspended", 5)
	d := digestOf([]byte("susp-verify-susp-verify-susp-verify0"))
	now := nowMS()
	if err := CreateVerificationToken(ctx, db, CreateVerificationTokenParams{TokenDigest: d, UserID: u, CreatedAtMS: now, ExpiresAtMS: now + 60_000}); err != nil {
		t.Fatalf("CreateVerificationToken: %v", err)
	}

	if err := VerifyEmail(ctx, db, VerifyEmailParams{TokenDigest: d, NowMS: now + 1}); !errors.Is(err, ErrVerificationTokenInvalid) {
		t.Errorf("suspended verify: got %v, want ErrVerificationTokenInvalid", err)
	}
	// The token died with the attempt — the link is unreplayable.
	if _, err := VerificationTokenUserID(ctx, db, d, now+1); !errors.Is(err, ErrVerificationTokenInvalid) {
		t.Errorf("token lookup after suspended attempt: got %v, want ErrVerificationTokenInvalid", err)
	}
	// Stamp untouched.
	user, err := UserByID(ctx, db, u)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if user.EmailVerifiedAtMS != nil {
		t.Error("suspended account stamped verified")
	}
}

func TestVerifyEmail_ExpiredToken(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	u := insertUser(t, db, "expverify", "expverify@example.com", "active", 1)
	d := digestOf([]byte("exp-verify-exp-verify-exp-verify-00"))
	now := nowMS()
	if err := CreateVerificationToken(ctx, db, CreateVerificationTokenParams{TokenDigest: d, UserID: u, CreatedAtMS: now, ExpiresAtMS: now + 100}); err != nil {
		t.Fatalf("CreateVerificationToken: %v", err)
	}

	if err := VerifyEmail(ctx, db, VerifyEmailParams{TokenDigest: d, NowMS: now + 101}); !errors.Is(err, ErrVerificationTokenInvalid) {
		t.Errorf("expired verify: got %v, want ErrVerificationTokenInvalid", err)
	}
}

func TestVerifyEmail_ConcurrentSingleWinner(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	u := insertUser(t, db, "vrace", "vrace@example.com", "active", 1)
	d := digestOf([]byte("race-verify-race-verify-race-verify0"))
	now := nowMS()
	if err := CreateVerificationToken(ctx, db, CreateVerificationTokenParams{TokenDigest: d, UserID: u, CreatedAtMS: now, ExpiresAtMS: now + 60_000}); err != nil {
		t.Fatalf("CreateVerificationToken: %v", err)
	}

	var wg sync.WaitGroup
	results := make([]error, 2)
	for i := range 2 {
		wg.Go(func() {
			results[i] = VerifyEmail(ctx, db, VerifyEmailParams{TokenDigest: d, NowMS: now + 1})
		})
	}
	wg.Wait()

	okCount := 0
	for _, err := range results {
		if err == nil {
			okCount++
			continue
		}
		if !errors.Is(err, ErrVerificationTokenInvalid) {
			t.Errorf("unexpected error: %v", err)
		}
	}
	if okCount != 1 {
		t.Errorf("winners = %d, want exactly 1", okCount)
	}
	// The stamp landed once and the token is gone.
	user, err := UserByID(ctx, db, u)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if user.EmailVerifiedAtMS == nil {
		t.Error("stamp missing after the concurrent verify")
	}
	if _, err := VerificationTokenUserID(ctx, db, d, now+1); !errors.Is(err, ErrVerificationTokenInvalid) {
		t.Errorf("token survived the race: %v", err)
	}
}

func TestDeleteExpiredVerificationTokens_BoundedBatch(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	u1 := insertUser(t, db, "vsweep1", "vsweep1@example.com", "active", 1)
	u2 := insertUser(t, db, "vsweep2", "vsweep2@example.com", "active", 1)
	u3 := insertUser(t, db, "vsweep3", "vsweep3@example.com", "active", 1)
	now := nowMS()
	for i, u := range []string{u1, u2} {
		d := digestOf([]byte{byte(i), 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1})
		if _, err := db.ExecContext(ctx,
			`INSERT INTO email_verification_tokens (token_digest, user_id, created_at_ms, expires_at_ms) VALUES (?, ?, ?, ?)`,
			d, u, now-2000, now-1000,
		); err != nil {
			t.Fatalf("insert expired token: %v", err)
		}
	}
	live := digestOf([]byte("live-verify-live-verify-live-verif0"))
	if err := CreateVerificationToken(ctx, db, CreateVerificationTokenParams{TokenDigest: live, UserID: u3, CreatedAtMS: now, ExpiresAtMS: now + 60_000}); err != nil {
		t.Fatalf("CreateVerificationToken live: %v", err)
	}

	n, err := DeleteExpiredVerificationTokens(ctx, db, now, 10)
	if err != nil {
		t.Fatalf("DeleteExpiredVerificationTokens: %v", err)
	}
	if n != 2 {
		t.Errorf("swept = %d, want 2", n)
	}
	if _, err := VerificationTokenUserID(ctx, db, live, now); err != nil {
		t.Errorf("live token was swept: %v", err)
	}
}

func TestVerificationTokensCascadeOnUserDelete(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	u := insertUser(t, db, "vdeluser", "vdel@example.com", "active", 1)
	d := digestOf([]byte("vdel-verify-vdel-verify-vdel-verify0"))
	now := nowMS()
	if err := CreateVerificationToken(ctx, db, CreateVerificationTokenParams{TokenDigest: d, UserID: u, CreatedAtMS: now, ExpiresAtMS: now + 60_000}); err != nil {
		t.Fatalf("CreateVerificationToken: %v", err)
	}
	if err := DeleteUser(ctx, db, u); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM email_verification_tokens`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("%d token rows remain after user delete", n)
	}
}
