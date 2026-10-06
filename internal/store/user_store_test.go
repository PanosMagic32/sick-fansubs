package store

import (
	"database/sql"
	"errors"
	"testing"

	"sick-fansubs/internal/store/storetest"
)

func TestCreateUserAndSession_DuplicateEmail(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Katakuri", "katakuri", "same@example.com")

	// Same email, different username: the UNIQUE backstop rejects the insert,
	// and no session row survives the rollback.
	digest := make([]byte, 32)
	err := CreateUserAndSession(ctx, db, CreateUserAndSessionParams{
		ID:            "user_02",
		Username:      "UserB",
		UsernameCanon: "userb",
		Email:         "same@example.com",
		Password:      "$2a$12$...bcrypt-hash...",
		Role:          "user",
		Status:        "active",
		AuthVersion:   1,
		CreatedAtMS:   2000,
		UpdatedAtMS:   2000,
		SessionID:     "sess_02",
		TokenDigest:   digest,
		CSRF:          make([]byte, 32),
		ExpiresAtMS:   2000 + 24*3600*1000,
	})
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("CreateUserAndSession with a claimed email = %v, want ErrDuplicate", err)
	}
	if _, err := SessionByDigest(ctx, db, digest); !errors.Is(err, ErrNotFound) {
		t.Errorf("SessionByDigest for the rejected registration = %v, want ErrNotFound", err)
	}
}

func TestUserByCanonical_Found(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Katakuri", "katakuri", "k@example.com")

	u, err := UserByCanonical(ctx, db, "katakuri")
	if err != nil {
		t.Fatalf("UserByCanonical: %v", err)
	}
	if u.ID != "user_01" {
		t.Errorf("ID: got %q, want %q", u.ID, "user_01")
	}
}

func TestUserByCanonical_NotFound(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	_, err := UserByCanonical(ctx, db, "nobody")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// TestUserRefByCanonical pins the public byline projection: the canonical
// lookup answers id, username, and the avatar reference (nil for an
// avatarless row), and an unknown name is ErrNotFound.
func TestUserRefByCanonical(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:          "user_01",
		Username:    "Kushoyarou",
		AvatarURL:   "media/images/ab/abcdef0123456789abcdef0123456789.png",
		CreatedAtMS: 1000,
	})
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:          "user_02",
		Username:    "NoPicture",
		CreatedAtMS: 1000,
	})

	ref, err := UserRefByCanonical(ctx, db, "kushoyarou")
	if err != nil {
		t.Fatalf("UserRefByCanonical: %v", err)
	}
	if ref.ID != "user_01" || ref.Username != "Kushoyarou" {
		t.Errorf("UserRefByCanonical(%q) = %q/%q, want user_01/Kushoyarou", "kushoyarou", ref.ID, ref.Username)
	}
	gotAvatar := ""
	if ref.AvatarURL != nil {
		gotAvatar = *ref.AvatarURL
	}
	if gotAvatar != "media/images/ab/abcdef0123456789abcdef0123456789.png" {
		t.Errorf("UserRefByCanonical(%q) avatar = %q, want the stored reference", "kushoyarou", gotAvatar)
	}

	avatarless, err := UserRefByCanonical(ctx, db, "nopicture")
	if err != nil {
		t.Fatalf("UserRefByCanonical avatarless: %v", err)
	}
	if avatarless.AvatarURL != nil {
		t.Errorf("UserRefByCanonical(%q) avatar = %q, want nil", "nopicture", *avatarless.AvatarURL)
	}

	if _, err := UserRefByCanonical(ctx, db, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UserRefByCanonical(nobody) = %v, want ErrNotFound", err)
	}
}

func TestUserByCanonicalLenient_WhitespaceShapes(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	// Imported names keep their legacy spacing — surrounding, trailing, and
	// repeated whitespace — which a sign-in form cannot reproduce. The
	// long-run row also pins the coarse SQL prefilter (a run longer than any
	// collapse pass would catch), and the Unicode rows pin it against the
	// whitespace set strings.Fields splits on.
	mustCreateUser(t, db, "user_01", "Jane Doe ", "jane doe ", "jane@example.com")
	mustCreateUser(t, db, "user_02", "Bob  Smith", "bob  smith", "bob@example.com")
	mustCreateUser(t, db, "user_03", "Kim     Lee", "kim     lee", "kim@example.com")
	mustCreateUser(t, db, "user_04", "Ana\u00a0Maria", "ana\u00a0maria", "ana@example.com")
	mustCreateUser(t, db, "user_05", "Rex\vWu", "rex\vwu", "rex@example.com")

	tests := []struct {
		name  string
		typed string
		want  string
	}{
		{"trailing space", "jane doe", "user_01"},
		{"doubled space", "bob smith", "user_02"},
		{"long whitespace run", "kim lee", "user_03"},
		{"non-breaking space", "ana maria", "user_04"},
		{"vertical tab", "rex wu", "user_05"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := UserByCanonicalLenient(ctx, db, tt.typed)
			if err != nil {
				t.Fatalf("UserByCanonicalLenient(%q): %v", tt.typed, err)
			}
			if u.ID != tt.want {
				t.Errorf("UserByCanonicalLenient(%q) ID = %q, want %q", tt.typed, u.ID, tt.want)
			}
		})
	}
}

func TestUserByCanonicalLenient_ExactMatchWins(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Carl", "carl", "carl@example.com")
	mustCreateUser(t, db, "user_02", "Carl ", "carl ", "carl2@example.com")

	u, err := UserByCanonicalLenient(ctx, db, "carl")
	if err != nil {
		t.Fatalf("UserByCanonicalLenient: %v", err)
	}
	if u.ID != "user_01" {
		t.Errorf("UserByCanonicalLenient(\"carl\") ID = %q, want the exact match %q", u.ID, "user_01")
	}
}

func TestUserByCanonicalLenient_AmbiguousMatchFailsClosed(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Dana Fox", "dana fox", "dana@example.com")
	mustCreateUser(t, db, "user_02", "Dana  Fox", "dana  fox", "dana2@example.com")

	// Neither stored spelling is typed exactly, and two rows normalize to the
	// same key — answering either would be a coin flip.
	_, err := UserByCanonicalLenient(ctx, db, "dana   fox")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("UserByCanonicalLenient(\"dana   fox\") = %v, want ErrNotFound", err)
	}
	if !IsAmbiguousUser(err) {
		t.Errorf("UserByCanonicalLenient(\"dana   fox\") = %v, want IsAmbiguousUser", err)
	}
}

func TestUserByCanonicalLenient_NotFound(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Katakuri", "katakuri", "k@example.com")

	_, err := UserByCanonicalLenient(ctx, db, "nobody")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("UserByCanonicalLenient(\"nobody\") = %v, want ErrNotFound", err)
	}
	if IsAmbiguousUser(err) {
		t.Errorf("UserByCanonicalLenient(\"nobody\") = %v, want a plain not-found", err)
	}
}

func TestUserByEmail_Found(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Katakuri", "katakuri", "k@example.com")

	u, err := UserByEmail(ctx, db, "k@example.com")
	if err != nil {
		t.Fatalf("UserByEmail: %v", err)
	}
	if u.ID != "user_01" {
		t.Errorf("ID: got %q, want %q", u.ID, "user_01")
	}
}

func TestUserByEmail_NotFound(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	_, err := UserByEmail(ctx, db, "nobody@example.com")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestUserByID_Found(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Katakuri", "katakuri", "k@example.com")

	u, err := UserByID(ctx, db, "user_01")
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if u.Username != "Katakuri" {
		t.Errorf("username: got %q, want %q", u.Username, "Katakuri")
	}
}

func TestUserByID_NotFound(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	_, err := UserByID(ctx, db, "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestIncrementAuthVersionAndDeleteSessions_Success(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Katakuri", "katakuri", "k@example.com")

	// Two sessions before the bump.
	d1, c1 := make([]byte, 32), make([]byte, 32)
	d2, c2 := make([]byte, 32), make([]byte, 32)
	d1[0], d2[0] = 1, 2
	mustCreateSession(t, db, "sess_01", "user_01", d1, c1, 1, 5000, 5000+7*24*3600*1000)
	mustCreateSession(t, db, "sess_02", "user_01", d2, c2, 1, 5000, 5000+7*24*3600*1000)

	err := IncrementAuthVersionAndDeleteSessions(ctx, db, "user_01", 2000)
	if err != nil {
		t.Fatalf("IncrementAuthVersionAndDeleteSessions: %v", err)
	}

	// Version bumped AND both rows deleted (one transaction).
	u, err := UserByID(ctx, db, "user_01")
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if u.AuthVersion != 2 {
		t.Errorf("auth_version: got %d, want 2", u.AuthVersion)
	}
	if u.UpdatedAtMS != 2000 {
		t.Errorf("updated_at_ms: got %d, want 2000", u.UpdatedAtMS)
	}
	for _, d := range [][]byte{d1, d2} {
		if _, err := SessionByDigest(ctx, db, d); !errors.Is(err, ErrNotFound) {
			t.Errorf("session should be deleted by the bump: %v", err)
		}
	}
}

func TestIncrementAuthVersionAndDeleteSessions_UserNotFound(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	err := IncrementAuthVersionAndDeleteSessions(ctx, db, "nonexistent", 2000)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestResetUserPassword_Success(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Katakuri", "katakuri", "k@example.com")

	// Two live sessions before the reset.
	d1, c1 := make([]byte, 32), make([]byte, 32)
	d2, c2 := make([]byte, 32), make([]byte, 32)
	d1[0], d2[0] = 1, 2
	mustCreateSession(t, db, "sess_01", "user_01", d1, c1, 1, 5000, 5000+7*24*3600*1000)
	mustCreateSession(t, db, "sess_02", "user_01", d2, c2, 1, 5000, 5000+7*24*3600*1000)

	err := ResetUserPassword(ctx, db, "user_01", "$2a$12$...temp-hash...", 2000)
	if err != nil {
		t.Fatalf("ResetUserPassword: %v", err)
	}

	u, err := UserByID(ctx, db, "user_01")
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if u.Password != "$2a$12$...temp-hash..." {
		t.Error("password not replaced")
	}
	if !u.MustChangePassword {
		t.Error("must_change_password not set by the reset")
	}
	if u.AuthVersion != 2 {
		t.Errorf("auth_version: got %d, want 2", u.AuthVersion)
	}
	if u.UpdatedAtMS != 2000 {
		t.Errorf("updated_at_ms: got %d, want 2000", u.UpdatedAtMS)
	}

	// Both sessions deleted in the same transaction.
	for _, d := range [][]byte{d1, d2} {
		if _, err := SessionByDigest(ctx, db, d); !errors.Is(err, ErrNotFound) {
			t.Errorf("session should be deleted by the reset: %v", err)
		}
	}
}

func TestResetUserPassword_NotFound(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	err := ResetUserPassword(ctx, db, "nonexistent", "$2a$12$...", 2000)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestUpdatePasswordAndRevokeSessions_ClearsForcedFlag(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	mustCreateUser(t, db, "user_01", "Katakuri", "katakuri", "k@example.com")

	// Set the flag through the reset path (the realistic sequence), then
	// clear it through the password-change path — the forced change is the
	// gate's way out, and it must clear the flag in the SAME
	// transaction as the verifier swap.
	if err := ResetUserPassword(ctx, db, "user_01", "$2a$12$...temp...", 2000); err != nil {
		t.Fatalf("ResetUserPassword: %v", err)
	}
	if err := UpdatePasswordAndRevokeSessions(ctx, db, "user_01", "$2a$12$...final...", 2, 3000); err != nil {
		t.Fatalf("UpdatePasswordAndRevokeSessions: %v", err)
	}

	u, err := UserByID(ctx, db, "user_01")
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if u.MustChangePassword {
		t.Error("must_change_password should be cleared by the password change")
	}
	if u.Password != "$2a$12$...final..." {
		t.Error("password not replaced")
	}
	if u.AuthVersion != 3 {
		t.Errorf("auth_version: got %d, want 3", u.AuthVersion)
	}
}

// TestUpdateUserAvatar pins the avatar write: the reference
// and updated_at_ms change in one statement, auth_version is untouched
// (not a revocation event), and a missing user maps to ErrNotFound.
func TestUpdateUserAvatar(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := t.Context()
	mustCreateUser(t, db, "u1", "Katakuri", "katakuri", "k@example.com")

	const rel = "media/images/ab/abcdef0123456789abcdef0123456789.png"
	if err := UpdateUserAvatar(ctx, db, "u1", rel, 5000); err != nil {
		t.Fatalf("UpdateUserAvatar: %v", err)
	}

	var (
		stored      string
		updatedAtMS int64
		authVersion int64
	)
	err := db.QueryRow(`SELECT avatar_url, updated_at_ms, auth_version FROM users WHERE id = 'u1'`).
		Scan(&stored, &updatedAtMS, &authVersion)
	if err != nil {
		t.Fatalf("read user row: %v", err)
	}
	if stored != rel {
		t.Errorf("avatar_url = %q, want %q", stored, rel)
	}
	if updatedAtMS != 5000 {
		t.Errorf("updated_at_ms = %d, want 5000", updatedAtMS)
	}
	if authVersion != 1 {
		t.Errorf("auth_version = %d, want unchanged 1", authVersion)
	}

	// An empty reference means absence: it stores SQL NULL, never "".
	if err := UpdateUserAvatar(ctx, db, "u1", "", 6000); err != nil {
		t.Fatalf("UpdateUserAvatar(clear): %v", err)
	}
	var cleared sql.NullString
	if err := db.QueryRow(`SELECT avatar_url FROM users WHERE id = 'u1'`).Scan(&cleared); err != nil {
		t.Fatalf("read cleared avatar: %v", err)
	}
	if cleared.Valid {
		t.Errorf("UpdateUserAvatar(%q, %q) stored %q, want NULL", "u1", "", cleared.String)
	}

	if err := UpdateUserAvatar(ctx, db, "missing", rel, 6000); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateUserAvatar(missing) err = %v, want ErrNotFound", err)
	}
}

func TestChangeUserEmail_SwapResetsStampAndKillsTokens(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	u := insertUser(t, db, "changer", "old@example.com", "active", 2)
	// Verified account with one live reset token and one verification
	// token (both must die with the old address).
	if _, err := db.ExecContext(ctx,
		`UPDATE users SET email_verified_at_ms = 1000 WHERE id = ?`, u,
	); err != nil {
		t.Fatalf("stamp fixture: %v", err)
	}
	now := nowMS()
	if err := CreateResetToken(ctx, db, CreateResetTokenParams{
		TokenDigest: digestOf([]byte("email-swap-reset-token-0000000000")), UserID: u, CreatedAtMS: now, ExpiresAtMS: now + 1000,
	}); err != nil {
		t.Fatalf("seed reset token: %v", err)
	}
	if err := CreateVerificationToken(ctx, db, CreateVerificationTokenParams{
		TokenDigest: digestOf([]byte("email-swap-verify-token-000000000")), UserID: u, CreatedAtMS: now, ExpiresAtMS: now + 1000,
	}); err != nil {
		t.Fatalf("seed verification token: %v", err)
	}

	if err := ChangeUserEmail(ctx, db, u, "new@example.com", 2, now+1); err != nil {
		t.Fatalf("ChangeUserEmail: %v", err)
	}

	user, err := UserByID(ctx, db, u)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if user.Email != "new@example.com" {
		t.Errorf("email = %q, want new@example.com", user.Email)
	}
	if user.EmailVerifiedAtMS != nil {
		t.Error("stamp not reset on email change")
	}
	if user.AuthVersion != 2 {
		t.Errorf("auth_version changed on email change: %d", user.AuthVersion)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM password_reset_tokens`).Scan(&n); err != nil {
		t.Fatalf("count reset tokens: %v", err)
	}
	if n != 0 {
		t.Errorf("%d reset token rows remain after email change", n)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM email_verification_tokens`).Scan(&n); err != nil {
		t.Fatalf("count verification tokens: %v", err)
	}
	if n != 0 {
		t.Errorf("%d verification token rows remain after email change", n)
	}
}

func TestChangeUserEmail_CASMismatchRejected(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	u := insertUser(t, db, "casemail", "casemail@example.com", "active", 7)
	err := ChangeUserEmail(ctx, db, u, "other@example.com", 99, nowMS())
	if !errors.Is(err, ErrAuthVersionMismatch) {
		t.Errorf("version mismatch: got %v, want ErrAuthVersionMismatch", err)
	}
	user, err := UserByID(ctx, db, u)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if user.Email != "casemail@example.com" {
		t.Errorf("email changed despite version mismatch: %q", user.Email)
	}
}

func TestChangeUserEmail_DuplicateAddress(t *testing.T) {
	db := openStoreDB(t)
	ctx := t.Context()

	insertUser(t, db, "dupowner", "dupowner@example.com", "active", 1)
	u2 := insertUser(t, db, "dupchanger", "dupchanger@example.com", "active", 1)

	// The advisory pre-check is bypassed here on purpose: the store must
	// map the UNIQUE backstop itself (the concurrent-change/register race).
	err := ChangeUserEmail(ctx, db, u2, "dupowner@example.com", 1, nowMS())
	if !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate address: got %v, want ErrDuplicate", err)
	}
	user, err := UserByID(ctx, db, u2)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if user.Email != "dupchanger@example.com" {
		t.Errorf("email changed despite the UNIQUE violation: %q", user.Email)
	}
}
