package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"sick-fansubs/internal/mail"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store"
	"sick-fansubs/internal/store/storetest"
)

func TestSignIn_SuccessWithUsername(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	// Register a user first (uses Register, which also tests sign-in).
	sessionTok, csrfTok, user, err := svc.Register(ctx, "TestUser", "test@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if sessionTok == "" {
		t.Error("expected non-empty session token")
	}
	if csrfTok == "" {
		t.Error("expected non-empty CSRF token")
	}
	if user.ID == "" {
		t.Error("expected non-empty user ID")
	}
	if user.Username != "TestUser" {
		t.Errorf("username: got %q, want %q", user.Username, "TestUser")
	}
	if user.Role != "user" {
		t.Errorf("role: got %q, want %q", user.Role, "user")
	}

	// Now sign in with the username.
	sessionTok2, csrfTok2, user2, _, err := svc.SignIn(ctx, "TestUser", "password123", "")
	if err != nil {
		t.Fatalf("SignIn with username: %v", err)
	}
	if sessionTok2 == "" {
		t.Error("expected non-empty session token")
	}
	if csrfTok2 == "" {
		t.Error("expected non-empty CSRF token")
	}
	if user2.Username != "TestUser" {
		t.Errorf("username: got %q, want %q", user2.Username, "TestUser")
	}
}

func TestSignIn_SuccessWithEmail(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	_, _, _, err := svc.Register(ctx, "TestUser", "test@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Sign in with the email (lowercase).
	_, _, user, _, err := svc.SignIn(ctx, "test@example.com", "password123", "")
	if err != nil {
		t.Fatalf("SignIn with email: %v", err)
	}
	if user.Username != "TestUser" {
		t.Errorf("username: got %q, want %q", user.Username, "TestUser")
	}
}

func TestSignIn_CaseInsensitiveUsername(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	_, _, _, err := svc.Register(ctx, "TestUser", "test@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Sign in with mixed case.
	_, _, user, _, err := svc.SignIn(ctx, "testuser", "password123", "")
	if err != nil {
		t.Fatalf("SignIn with mixed case: %v", err)
	}
	if user.Username != "TestUser" {
		t.Errorf("username: got %q, want %q", user.Username, "TestUser")
	}
}

func TestSignIn_WrongPassword(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	_, _, _, err := svc.Register(ctx, "TestUser", "test@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, _, _, _, err = svc.SignIn(ctx, "TestUser", "wrongpassword", "")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("SignIn(%q, wrongpassword) error = %v, want %v", "TestUser", err, ErrInvalidCredentials)
	}
}

func TestSignIn_UnknownUser(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	_, _, _, _, err := svc.SignIn(ctx, "Nobody", "password123", "")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("got %v, want ErrInvalidCredentials", err)
	}
}

func TestSignIn_SuspendedUser(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	// Register a user.
	_, _, user, err := svc.Register(ctx, "TestUser", "test@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Suspend through the store's own path (bumps auth_version, deletes
	// sessions).
	if err := store.SuspendUser(ctx, db, user.ID, time.Now().UnixMilli()); err != nil {
		t.Fatalf("SuspendUser: %v", err)
	}

	_, _, _, _, err = svc.SignIn(ctx, "TestUser", "password123", "")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("got %v, want ErrInvalidCredentials for suspended user", err)
	}
}

func TestSignIn_FallbackAtInUsername(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	// Simulate a legacy user whose username contains '@' by inserting directly.
	// Current registration (isAlphanumeric) rejects '@', but migrated data may have it.
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "legacy01",
		Username:     "m@ria",
		Email:        "maria@example.com",
		PasswordHash: string(mustBcryptHash(t, "password123")),
	})

	// Sign in with the username (contains '@').
	_, _, user, _, err := svc.SignIn(ctx, "m@ria", "password123", "")
	if err != nil {
		t.Fatalf("SignIn with @-containing username: %v", err)
	}
	if user.Username != "m@ria" {
		t.Errorf("username: got %q, want %q", user.Username, "m@ria")
	}
}

func TestSignIn_ImportedWhitespaceUsernames(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	// Imported names keep their legacy spacing; a sign-in form cannot
	// reproduce a leading, trailing, or doubled space.
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "legacy11",
		Username:     "John Doe",
		Email:        "jd@example.com",
		PasswordHash: string(mustBcryptHash(t, "password123")),
	})
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "legacy12",
		Username:     "Jane Doe ",
		Email:        "janed@example.com",
		PasswordHash: string(mustBcryptHash(t, "password123")),
	})
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "legacy13",
		Username:     "Bob  Smith",
		Email:        "bob@example.com",
		PasswordHash: string(mustBcryptHash(t, "password123")),
	})

	tests := []struct {
		name  string
		typed string
		want  string
	}{
		{"inner space matches exactly", "John Doe", "legacy11"},
		{"trailing space falls back", "Jane Doe", "legacy12"},
		{"doubled space falls back", "Bob Smith", "legacy13"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, user, _, err := svc.SignIn(ctx, tt.typed, "password123", "")
			if err != nil {
				t.Fatalf("SignIn(%q): %v", tt.typed, err)
			}
			if user.ID != tt.want {
				t.Errorf("SignIn(%q) user ID = %q, want %q", tt.typed, user.ID, tt.want)
			}
		})
	}
}

func TestSignIn_AmbiguousWhitespaceMatchFailsClosed(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "legacy11",
		Username:     "Dana Fox",
		Email:        "dana@example.com",
		PasswordHash: string(mustBcryptHash(t, "password123")),
	})
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "legacy12",
		Username:     "Dana  Fox",
		Email:        "dana2@example.com",
		PasswordHash: string(mustBcryptHash(t, "password123")),
	})

	// Two rows normalize to the same key; answering either would be a coin
	// flip, so the identifier stays invalid credentials.
	_, _, _, _, err := svc.SignIn(ctx, "Dana   Fox", "password123", "")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("SignIn(an ambiguous spelling) = %v, want ErrInvalidCredentials", err)
	}
}

// TestSignIn_RehashLowCostVerifier pins the atomic rehash+session contract:
// the rehash bumps auth_version, and the session insert must use the bumped
// version — against the stale version INSERT…SELECT matches zero rows and a
// migrated cost<12 account fails its first sign-in with a generic 401.
func TestSignIn_RehashLowCostVerifier(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	// The REAL constructor: this test PINS the production rehash target
	// (cost 12), so the low-cost test seam must not apply.
	svc := New(db, mail.LogLink{}, "")

	// Migrated account: valid verifier at cost 4 (below the target 12).
	lowCostHash, err := bcrypt.GenerateFromPassword([]byte("password123"), 4)
	if err != nil {
		t.Fatalf("bcrypt cost 4: %v", err)
	}
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "migrated01",
		Username:     "Migrated",
		PasswordHash: string(lowCostHash),
	})

	sessionTok, _, user, _, err := svc.SignIn(ctx, "Migrated", "password123", "")
	if err != nil {
		t.Fatalf("first sign-in after migration must succeed: %v", err)
	}
	if user.ID != "migrated01" {
		t.Errorf("user.ID: got %q, want %q", user.ID, "migrated01")
	}

	// The verifier was rehashed to the target cost and the version bumped…
	u, err := store.UserByID(ctx, db, "migrated01")
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	cost, err := bcrypt.Cost([]byte(u.Password))
	if err != nil {
		t.Fatalf("bcrypt.Cost on rehashed verifier: %v", err)
	}
	if cost != 12 {
		t.Errorf("verifier cost after rehash: got %d, want 12", cost)
	}
	if u.AuthVersion != 2 {
		t.Errorf("auth_version after rehash: got %d, want 2", u.AuthVersion)
	}

	// …and the session was created UNDER the new version: the digest resolves
	// (SessionByDigest enforces version equality in its WHERE clause).
	digest, ok := middleware.SessionDigestValue(sessionTok)
	if !ok {
		t.Fatal("returned session token failed digest validation")
	}
	if _, err := store.SessionByDigest(ctx, db, digest); err != nil {
		t.Fatalf("session must resolve after atomic rehash: %v", err)
	}

	// Second sign-in exercises the no-rehash path (verifier already at cost
	// 12): succeeds without another version bump.
	_, _, _, _, err = svc.SignIn(ctx, "Migrated", "password123", "")
	if err != nil {
		t.Fatalf("second sign-in: %v", err)
	}
	u, err = store.UserByID(ctx, db, "migrated01")
	if err != nil {
		t.Fatalf("UserByID after second sign-in: %v", err)
	}
	if u.AuthVersion != 2 {
		t.Errorf("auth_version after no-rehash sign-in: got %d, want 2", u.AuthVersion)
	}
}

func TestRegister_ValidationErrors(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	tests := []struct {
		name     string
		username string
		email    string
		password string
		wantErr  error
		wantCode string
	}{
		{"username too short", "", "a@b.com", "password123", ErrValidation, "minLength"},
		{"username whitespace only", "   ", "a@b.com", "password123", ErrValidation, "minLength"},
		{"username too long", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "a@b.com", "password123", ErrValidation, "maxLength"}, // 33 chars
		{"username invalid chars", "test_user", "a@b.com", "password123", ErrValidation, "invalidFormat"},
		{"username with Greek", "Τεστ", "a@b.com", "password123", ErrValidation, "invalidFormat"},
		{"email missing @", "testuser", "notanemail", "password123", ErrValidation, "invalidFormat"},
		{"email empty local", "testuser", "@example.com", "password123", ErrValidation, "invalidFormat"},
		{"email empty domain", "testuser", "test@", "password123", ErrValidation, "invalidFormat"},
		{"email with Greek local part", "testuser", "ακης@example.com", "password123", ErrValidation, "invalidFormat"},
		{"email with Greek domain", "testuser", "akis@παράδειγμα.ελ", "password123", ErrValidation, "invalidFormat"},
		// Strict charset: only [A-Za-z0-9@._+-].
		{"email with space", "testuser", "a b@example.com", "password123", ErrValidation, "invalidFormat"},
		{"email with control char", "testuser", "a\tb@example.com", "password123", ErrValidation, "invalidFormat"},
		{"email with quote", "testuser", "a\"b@example.com", "password123", ErrValidation, "invalidFormat"},
		{"email with comma", "testuser", "a,b@example.com", "password123", ErrValidation, "invalidFormat"},
		{"email with semicolon", "testuser", "a;b@example.com", "password123", ErrValidation, "invalidFormat"},
		{"valid email with plus and dash", "testuser", "user.name+tag@sub-domain.example.com", "password123", nil, ""},
		{"password too short", "testuser", "a@b.com", "short", ErrValidation, "minLength"},
		{"password 7 bytes", "testuser", "a@b.com", strings.Repeat("a", 7), ErrValidation, "minLength"},
		{"password 8 bytes", "user8", "a@b.com", strings.Repeat("a", 8), nil, ""},
		{"password 72 bytes", "user72", "user72@b.com", strings.Repeat("a", 72), nil, ""},
		{"password 73 bytes", "testuser", "a@b.com", strings.Repeat("a", 73), ErrValidation, "maxLength"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := svc.Register(ctx, tt.username, tt.email, tt.password, "")
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("got %v, want %v", err, tt.wantErr)
			}
			if tt.wantCode != "" {
				fe, ok := errors.AsType[*FieldError](err)
				if !ok || fe.Code != tt.wantCode {
					t.Errorf("field error = %+v, want code %q", fe, tt.wantCode)
				}
			}
		})
	}
}

func TestRegister_DuplicateUsername(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	_, _, _, err := svc.Register(ctx, "TestUser", "a@example.com", "password123", "")
	if err != nil {
		t.Fatalf("first Register: %v", err)
	}

	_, _, _, err = svc.Register(ctx, "testuser", "b@example.com", "password123", "")
	if !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("got %v, want ErrUsernameTaken", err)
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	_, _, _, err := svc.Register(ctx, "UserA", "same@example.com", "password123", "")
	if err != nil {
		t.Fatalf("first Register: %v", err)
	}

	_, _, _, err = svc.Register(ctx, "UserB", "same@example.com", "password123", "")
	if !errors.Is(err, ErrEmailTaken) {
		t.Errorf("got %v, want ErrEmailTaken", err)
	}
}

func TestRegister_UserIDIs32HexChars(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	_, _, user, err := svc.Register(ctx, "TestUser", "test@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if len(user.ID) != 32 {
		t.Errorf("user ID length: got %d, want 32", len(user.ID))
	}
	for _, ch := range user.ID {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			t.Errorf("user ID contains non-hex character: %q", ch)
		}
	}
}

func TestRegister_PasswordIsHashed(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	// The REAL constructor: this test PINS the production hash prefix
	// ($2a$12$) — the low-cost seam must not apply.
	svc := New(db, mail.LogLink{}, "")

	_, _, user, err := svc.Register(ctx, "TestUser", "test@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	u, err := store.UserByID(ctx, db, user.ID)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if u.Password == "password123" {
		t.Error("password stored as plaintext")
	}
	if len(u.Password) < 7 || u.Password[:7] != "$2a$12$" {
		t.Errorf("password not hashed at cost 12: prefix %q", u.Password[:min(len(u.Password), 7)])
	}
}

func TestRegister_NullByteInPassword(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	svc := newTestService(db)

	pwd := string([]byte{'p', 'a', 's', 's', 0x00, 'w', 'o', 'r', 'd', '1', '2', '3', '4'})
	_, _, _, err := svc.Register(t.Context(), "TestUser", "test@example.com", pwd, "")
	if !errors.Is(err, ErrValidation) {
		t.Errorf("got %v, want ErrValidation for null-byte password", err)
	}
	if fe, ok := errors.AsType[*FieldError](err); !ok || fe.Code != "invalidFormat" {
		t.Errorf("field error = %+v, want code invalidFormat", fe)
	}
}

func TestRegister_SpaceUsernames(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	// Registration normalizes whitespace: trim + collapse every run.
	_, _, user, err := svc.Register(ctx, "  John   Doe ", "jd@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register with a spaced name: %v", err)
	}
	if user.Username != "John Doe" {
		t.Errorf("Register(\"  John   Doe \") username: got %q, want %q", user.Username, "John Doe")
	}

	// The new account signs in under any spelling variant (the sign-in rule
	// and the registration rule are the same whitespace fold).
	if _, _, _, _, err := svc.SignIn(ctx, "john doe", "password123", ""); err != nil {
		t.Errorf("SignIn(\"john doe\") = %v, want success", err)
	}

	// A legacy row differing only by whitespace is the SAME name: registration
	// must refuse the near-duplicate the login could not tell apart.
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "legacy31",
		Username:     "Jane  Roe",
		Email:        "jr@example.com",
		PasswordHash: string(mustBcryptHash(t, "password123")),
	})
	_, _, _, err = svc.Register(ctx, "Jane Roe", "jr2@example.com", "password123", "")
	if !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("Register(a whitespace near-duplicate) = %v, want ErrUsernameTaken", err)
	}

	// Several legacy rows normalizing alike cannot be told apart either — the
	// new name must not become a third near-duplicate the login then refuses.
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "legacy32",
		Username:     "Dana  Fox",
		Email:        "df@example.com",
		PasswordHash: string(mustBcryptHash(t, "password123")),
	})
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "legacy33",
		Username:     "Dana   Fox",
		Email:        "df2@example.com",
		PasswordHash: string(mustBcryptHash(t, "password123")),
	})
	_, _, _, err = svc.Register(ctx, "dana fox", "df3@example.com", "password123", "")
	if !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("Register(a name two legacy rows normalize to) = %v, want ErrUsernameTaken", err)
	}
}

func TestRegister_EmailMaxLength(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	svc := newTestService(db)

	longEmail := strings.Repeat("a", 250) + "@b.com" // 255 chars, exceeds RFC 5321 max
	_, _, _, err := svc.Register(t.Context(), "TestUser", longEmail, "password123", "")
	if !errors.Is(err, ErrValidation) {
		t.Errorf("got %v, want ErrValidation for overlong email", err)
	}
}

func TestRegister_TrimsWhitespace(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	_, _, user, err := svc.Register(ctx, "  TestUser  ", "test@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register with whitespace: %v", err)
	}
	if user.Username != "TestUser" {
		t.Errorf("username not trimmed: got %q, want %q", user.Username, "TestUser")
	}

	_, _, user2, err := svc.Register(ctx, "User2", "  test2@example.com  ", "password123", "")
	if err != nil {
		t.Fatalf("Register with email whitespace: %v", err)
	}
	u, _ := store.UserByID(ctx, db, user2.ID)
	if u.Email != "test2@example.com" {
		t.Errorf("email not trimmed: got %q, want %q", u.Email, "test2@example.com")
	}
}

func TestChangePassword_Success(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	_, _, user, err := svc.Register(ctx, "TestUser", "test@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Create a session for the user.
	_, _, _, _, err = svc.SignIn(ctx, "TestUser", "password123", "")
	if err != nil {
		t.Fatalf("SignIn before change: %v", err)
	}

	// Change password.
	err = svc.ChangePassword(ctx, user.ID, "password123", "newpassword456")
	if err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	// Old password should not work.
	_, _, _, _, err = svc.SignIn(ctx, "TestUser", "password123", "")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("old password should be rejected, got %v", err)
	}

	// New password should work.
	_, _, _, _, err = svc.SignIn(ctx, "TestUser", "newpassword456", "")
	if err != nil {
		t.Fatalf("SignIn with new password: %v", err)
	}
}

func TestChangePassword_InvalidatesSessions(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	_, _, user, err := svc.Register(ctx, "TestUser", "test@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Create two sessions.
	s1, _, _, _, err := svc.SignIn(ctx, "TestUser", "password123", "")
	if err != nil {
		t.Fatalf("SignIn 1: %v", err)
	}
	s2, _, _, _, err := svc.SignIn(ctx, "TestUser", "password123", "")
	if err != nil {
		t.Fatalf("SignIn 2: %v", err)
	}
	if s1 == s2 {
		t.Fatal("expected different session tokens")
	}

	// Change password — should invalidate all sessions.
	err = svc.ChangePassword(ctx, user.ID, "password123", "newpassword456")
	if err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	// Verify sessions were deleted.
	u, _ := store.UserByID(ctx, db, user.ID)
	if u.AuthVersion != 2 {
		t.Errorf("auth_version: got %d, want 2", u.AuthVersion)
	}
}

func TestChangePassword_WrongCurrentPassword(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	_, _, user, err := svc.Register(ctx, "TestUser", "test@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	err = svc.ChangePassword(ctx, user.ID, "wrongcurrent", "newpassword456")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("got %v, want ErrInvalidCredentials", err)
	}
}

func TestChangePassword_NewPasswordValidation(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	_, _, user, err := svc.Register(ctx, "TestUser", "test@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	tests := []struct {
		name        string
		newPassword string
		wantCode    string
	}{
		{"below the minimum", "short", "minLength"},
		{"above the 72-byte maximum", strings.Repeat("a", 73), "maxLength"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.ChangePassword(ctx, user.ID, "password123", tt.newPassword)
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("got %v, want ErrValidation", err)
			}
			if fe, ok := errors.AsType[*FieldError](err); !ok || fe.Code != tt.wantCode {
				t.Errorf("field error = %+v, want code %q", fe, tt.wantCode)
			}
		})
	}
}

func TestResetPassword_Success(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	_, _, user, err := svc.Register(ctx, "TestUser", "test@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	// A live session for the reset to delete.
	if _, _, _, _, err := svc.SignIn(ctx, "TestUser", "password123", ""); err != nil {
		t.Fatalf("SignIn before reset: %v", err)
	}

	temp, err := svc.ResetPassword(ctx, user.ID)
	if err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if len(temp) != 16 {
		t.Errorf("temp password length: got %d, want 16", len(temp))
	}

	// The temp password signs in and carries the forced-change flag.
	_, _, _, mustChange, err := svc.SignIn(ctx, "TestUser", temp, "")
	if err != nil {
		t.Fatalf("SignIn with temp password: %v", err)
	}
	if !mustChange {
		t.Error("sign-in after reset must carry mustChangePassword: true")
	}

	// The flag is set and the pre-reset session is gone (version bumped).
	u, err := store.UserByID(ctx, db, user.ID)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if !u.MustChangePassword {
		t.Error("must_change_password must be set")
	}
	if u.AuthVersion != 2 {
		t.Errorf("auth_version: got %d, want 2 (reset bumps + revokes)", u.AuthVersion)
	}

	// The old password no longer works.
	if _, _, _, _, err := svc.SignIn(ctx, "TestUser", "password123", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("old password must be rejected after reset, got %v", err)
	}

	// The forced change clears the flag (the gate's way out).
	if err := svc.ChangePassword(ctx, user.ID, temp, "newpassword456"); err != nil {
		t.Fatalf("forced ChangePassword: %v", err)
	}
	u2, err := store.UserByID(ctx, db, user.ID)
	if err != nil {
		t.Fatalf("UserByID after change: %v", err)
	}
	if u2.MustChangePassword {
		t.Error("must_change_password must clear after the password change")
	}
}

func TestResetPassword_NotFound(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	if _, err := svc.ResetPassword(ctx, "nonexistent"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("got %v, want store.ErrNotFound", err)
	}
}

func TestGenerateTempPassword_Shape(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for range 32 {
		p, err := generateTempPassword()
		if err != nil {
			t.Fatalf("generateTempPassword: %v", err)
		}
		if len(p) != 16 {
			t.Fatalf("length: got %d, want 16", len(p))
		}
		for _, c := range p {
			if !strings.ContainsRune(tempPasswordAlphabet, c) {
				t.Fatalf("character %q outside the unambiguous alphabet", c)
			}
		}
		if seen[p] {
			t.Fatal("two identical temp passwords in 32 draws")
		}
		seen[p] = true
	}
}

func TestChangePassword_UnknownUser(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	err := svc.ChangePassword(ctx, "nonexistent", "password123", "newpassword456")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("got %v, want ErrInvalidCredentials", err)
	}
}

func TestGenerateSessionToken(t *testing.T) {
	t.Parallel()
	raw, encoded, digest, err := generateSessionToken()
	if err != nil {
		t.Fatalf("generateSessionToken: %v", err)
	}
	if len(raw) != 32 {
		t.Errorf("raw length: got %d, want 32", len(raw))
	}
	if encoded == "" {
		t.Error("encoded is empty")
	}
	if len(digest) != 32 {
		t.Errorf("digest length: got %d, want 32", len(digest))
	}
	// Verify base64url encoding (no padding '=').
	for _, ch := range encoded {
		if ch == '=' {
			t.Error("encoded contains padding '=' — must be unpadded base64url")
		}
	}
}

func TestGenerateCSRFToken(t *testing.T) {
	t.Parallel()
	raw, encoded, err := generateCSRFToken()
	if err != nil {
		t.Fatalf("generateCSRFToken: %v", err)
	}
	if len(raw) != 32 {
		t.Errorf("raw length: got %d, want 32", len(raw))
	}
	if encoded == "" {
		t.Error("encoded is empty")
	}
	for _, ch := range encoded {
		if ch == '=' {
			t.Error("encoded contains padding '=' — must be unpadded base64url")
		}
	}
}

func TestTokensAreUnique(t *testing.T) {
	t.Parallel()
	s1, _, _, _ := generateSessionToken()
	s2, _, _, _ := generateSessionToken()
	if string(s1) == string(s2) {
		t.Error("two session tokens should be different")
	}

	_, c1, _ := generateCSRFToken()
	_, c2, _ := generateCSRFToken()
	if c1 == c2 {
		t.Error("two CSRF tokens should be different")
	}
}

// TestResetTokenLifetime pins the stored 30-minute window — the value the
// emailed link advertises.
func TestResetTokenLifetime(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestServiceWith(db, &fakeSender{}, "http://localhost:3000")

	_, _, _, err := svc.Register(ctx, "TestUser", "test@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := svc.ForgotPassword(ctx, "TestUser"); err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}

	var createdMS, expiresMS int64
	if err := db.QueryRowContext(ctx,
		`SELECT created_at_ms, expires_at_ms FROM password_reset_tokens LIMIT 1`).Scan(&createdMS, &expiresMS); err != nil {
		t.Fatalf("read reset token row: %v", err)
	}
	if got, want := expiresMS-createdMS, int64(30*60*1000); got != want {
		t.Errorf("reset token lifetime = %d ms, want %d", got, want)
	}
}

// TestVerificationTokenLifetime pins the stored seven-day window.
func TestVerificationTokenLifetime(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestServiceWith(db, &fakeSender{}, "http://localhost:3000")

	_, _, user, err := svc.Register(ctx, "TestUser", "test@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := svc.SendVerificationEmail(ctx, user.ID); err != nil {
		t.Fatalf("SendVerificationEmail: %v", err)
	}

	var createdMS, expiresMS int64
	if err := db.QueryRowContext(ctx,
		`SELECT created_at_ms, expires_at_ms FROM email_verification_tokens LIMIT 1`).Scan(&createdMS, &expiresMS); err != nil {
		t.Fatalf("read verification token row: %v", err)
	}
	if got, want := expiresMS-createdMS, int64(7*24*60*60*1000); got != want {
		t.Errorf("verification token lifetime = %d ms, want %d", got, want)
	}
}

// TestBcryptCostConstants pins the tuned cost values; the migration
// classifier mirrors the same literals on its own side
// (internal/migration/user_test.go). Each side pins its own pair — change
// both sides together.
func TestBcryptCostConstants(t *testing.T) {
	t.Parallel()
	if bcryptCost != 12 {
		t.Errorf("bcryptCost = %d, want 12", bcryptCost)
	}
	if maxBcryptCost != 13 {
		t.Errorf("maxBcryptCost = %d, want 13", maxBcryptCost)
	}
}

func TestDummyBcryptHash(t *testing.T) {
	t.Parallel()
	// Verify the dummy hash can be generated without panicking and is a valid
	// bcrypt hash that CompareHashAndPassword accepts.
	hash := dummyBcryptHash()
	if len(hash) == 0 {
		t.Fatal("dummy hash is empty")
	}

	err := bcrypt.CompareHashAndPassword(hash, []byte("dummy-password-for-timing-defense"))
	if err != nil {
		t.Errorf("dummy hash does not verify against its password: %v", err)
	}

	// Verify it doesn't verify against a random password.
	err = bcrypt.CompareHashAndPassword(hash, []byte("wrong-password"))
	if err == nil {
		t.Error("dummy hash should not verify against wrong password")
	}
}

func TestChangePassword_NullByte(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	_, _, user, err := svc.Register(ctx, "TestUser", "test@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	newPwd := string([]byte{'p', 'a', 's', 's', 0x00, 'w', 'o', 'r', 'd', '1', '2', '3', '4'})
	err = svc.ChangePassword(ctx, user.ID, "password123", newPwd)
	if !errors.Is(err, ErrValidation) {
		t.Errorf("got %v, want ErrValidation for null-byte password", err)
	}
	if fe, ok := errors.AsType[*FieldError](err); !ok || fe.Code != "invalidFormat" {
		t.Errorf("field error = %+v, want code invalidFormat", fe)
	}
}

// TestSignIn_MalformedVerifierRunsDummyComparison pins the enumeration-
// resistance contract: a user whose stored
// verifier cannot safely perform the real comparison (malformed hash,
// unsupported version) must still run the dummy comparison before the
// generic failure, matching the unknown-user path's timing profile.
//
// This test is deliberately NOT parallel: it swaps the package-level
// compareDummy seam, and serial tests complete before parallel ones resume.
func TestSignIn_MalformedVerifierRunsDummyComparison(t *testing.T) {
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	now := time.Now().UnixMilli()
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:          "user01",
		Username:    "BrokenHash",
		Email:       "broken@example.com",
		CreatedAtMS: now,
	})

	calls := 0
	original := compareDummy
	compareDummy = func(hash, password []byte) error {
		calls++
		return nil
	}
	t.Cleanup(func() { compareDummy = original })

	_, _, _, _, err := svc.SignIn(ctx, "BrokenHash", "password123", "")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("got %v, want ErrInvalidCredentials", err)
	}
	if calls != 1 {
		t.Errorf("got %d, want exactly one dummy comparison for a malformed verifier", calls)
	}
}

// TestSignIn_WrongPasswordSkipsDummyComparison pins the complementary path:
// a real comparison DID run (the verifier is well-formed), so no dummy
// comparison is needed — the timing profile already matches.
//
// Deliberately serial, like the malformed-verifier test above: both swap
// the compareDummy seam.
func TestSignIn_WrongPasswordSkipsDummyComparison(t *testing.T) {
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	_, _, _, err := svc.Register(ctx, "TestUser", "test@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	calls := 0
	original := compareDummy
	compareDummy = func(hash, password []byte) error {
		calls++
		return nil
	}
	t.Cleanup(func() { compareDummy = original })

	_, _, _, _, err = svc.SignIn(ctx, "TestUser", "wrong-password", "")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("got %v, want ErrInvalidCredentials", err)
	}
	if calls != 0 {
		t.Errorf("wrong-password path must not run the dummy comparison, got %d calls", calls)
	}
}

// TestSignIn_Over72ByteLegacyPassword pins the verification-only
// truncation: a legacy library hashed only the first 72 bytes of an
// over-72-byte password, so the target must compare the 72-byte prefix. The
// 72-byte boundary is BYTES, not characters — 40 Greek letters are 80 bytes
// in UTF-8 without looking long at all.
func TestSignIn_Over72ByteLegacyPassword(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	longPassword := strings.Repeat("α", 40)
	prefixHash, err := bcrypt.GenerateFromPassword([]byte(longPassword)[:72], bcryptCost)
	if err != nil {
		t.Fatalf("bcrypt prefix hash: %v", err)
	}
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "legacy01",
		Username:     "Legacy",
		PasswordHash: string(prefixHash),
	})

	// The FULL password verifies against the truncated legacy hash.
	_, _, user, _, err := svc.SignIn(ctx, "Legacy", longPassword, "")
	if err != nil {
		t.Fatalf("over-72-byte legacy sign-in must succeed: %v", err)
	}
	if user.ID != "legacy01" {
		t.Errorf("user.ID: got %q, want %q", user.ID, "legacy01")
	}

	// The verifier is unchanged: cost 12 already, and truncation is
	// verification-only — nothing is written anywhere.
	u, err := store.UserByID(ctx, db, "legacy01")
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if u.Password != string(prefixHash) {
		t.Error("verifier changed after over-72-byte sign-in")
	}
	if u.AuthVersion != 1 {
		t.Errorf("auth_version: got %d, want 1 (no rehash for an over-72-byte credential)", u.AuthVersion)
	}

	// A wrong over-72-byte password fails with the generic outcome.
	_, _, _, _, err = svc.SignIn(ctx, "Legacy", strings.Repeat("β", 40), "")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong over-72-byte password: got %v, want ErrInvalidCredentials", err)
	}
}

// TestSignIn_Over72ByteLowCostVerifierSkipsRehash pins the rehash scope: an
// over-72-byte credential cannot be rehashed (Go refuses to hash it), so a
// verified sign-in proceeds under the unchanged low-cost verifier instead of
// the normal cost<12 rehash path.
func TestSignIn_Over72ByteLowCostVerifierSkipsRehash(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	longPassword := strings.Repeat("α", 40)
	lowCostHash, err := bcrypt.GenerateFromPassword([]byte(longPassword)[:72], 4)
	if err != nil {
		t.Fatalf("bcrypt cost 4: %v", err)
	}
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "legacy02",
		Username:     "Legacy2",
		PasswordHash: string(lowCostHash),
	})

	if _, _, _, _, err := svc.SignIn(ctx, "Legacy2", longPassword, ""); err != nil {
		t.Fatalf("over-72-byte sign-in must succeed: %v", err)
	}

	u, err := store.UserByID(ctx, db, "legacy02")
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if u.Password != string(lowCostHash) {
		t.Error("verifier changed after over-72-byte sign-in")
	}
	if u.AuthVersion != 1 {
		t.Errorf("auth_version: got %d, want 1 (rehash must be skipped)", u.AuthVersion)
	}
}

// TestSignIn_VerifierAboveMaxCostRunsDummyComparison pins the fail-fast
// rule: a verifier above the operational maximum must NOT run the
// expensive comparison; the dummy comparison runs instead and the outcome is
// the generic failure.
//
// Deliberately serial: swaps the compareDummy seam.
func TestSignIn_VerifierAboveMaxCostRunsDummyComparison(t *testing.T) {
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	// Cost 14 is one step above the operational maximum.
	highCostHash, err := bcrypt.GenerateFromPassword([]byte("password123"), 14)
	if err != nil {
		t.Fatalf("bcrypt cost 14: %v", err)
	}
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "legacy03",
		Username:     "HighCost",
		PasswordHash: string(highCostHash),
	})

	calls := 0
	original := compareDummy
	compareDummy = func(hash, password []byte) error {
		calls++
		return nil
	}
	t.Cleanup(func() { compareDummy = original })

	_, _, _, _, err = svc.SignIn(ctx, "HighCost", "password123", "")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("above-max verifier: got %v, want ErrInvalidCredentials", err)
	}
	if calls != 1 {
		t.Errorf("got %d, want exactly one dummy comparison for an above-max verifier", calls)
	}
}

// TestSignIn_UnsupportedVerifierPrefixRunsDummyComparison pins the explicit
// family gate: Go's parser accepts ANY minor character, so a
// $2x$ verifier (the PHP-bug variant with different high-bit semantics)
// would otherwise pass the cost gate and run a real comparison. The runtime
// must deny non-$2a$/$2b$/$2y$ prefixes — imported verifiers are preserved
// byte-for-byte, so this gate is the only thing standing between a
// non-supported verifier and the expensive compare.
//
// Deliberately serial: swaps the compareDummy seam.
func TestSignIn_UnsupportedVerifierPrefixRunsDummyComparison(t *testing.T) {
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	// Build a well-formed hash, then swap the prefix to $2x$ — the hash body
	// is untouched, so a prefix-oblivious implementation would compare and
	// SUCCEED for the correct password.
	regular := mustBcryptHash(t, "password123")
	x2xHash := "$2x$" + string(regular[4:])
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "legacy06",
		Username:     "X2x",
		PasswordHash: x2xHash,
	})

	calls := 0
	original := compareDummy
	compareDummy = func(hash, password []byte) error {
		calls++
		return nil
	}
	t.Cleanup(func() { compareDummy = original })

	// The CORRECT password must still fail: the family gate rejects the
	// verifier before any comparison runs.
	_, _, _, _, err := svc.SignIn(ctx, "X2x", "password123", "")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("unsupported prefix: got %v, want ErrInvalidCredentials", err)
	}
	if calls != 1 {
		t.Errorf("got %d, want exactly one dummy comparison for an unsupported prefix", calls)
	}
}

// TestChangePassword_Over72ByteCurrentPassword pins the second verification
// site: a migrated over-72-byte credential verifies the same way
// in the change-password current-password check, so the account can
// self-repair to a ≤72-byte password.
func TestChangePassword_Over72ByteCurrentPassword(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	longPassword := strings.Repeat("α", 40)
	prefixHash := mustBcryptHash(t, string([]byte(longPassword)[:72]))
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "legacy04",
		Username:     "Legacy4",
		PasswordHash: string(prefixHash),
	})

	if err := svc.ChangePassword(ctx, "legacy04", longPassword, "newpassword456"); err != nil {
		t.Fatalf("ChangePassword with over-72-byte current password must succeed: %v", err)
	}

	// The new ≤72-byte password now signs in.
	if _, _, _, _, err := svc.SignIn(ctx, "Legacy4", "newpassword456", ""); err != nil {
		t.Fatalf("sign-in with the new password: %v", err)
	}
}

// TestChangePassword_VerifierAboveMaxCost: the stored verifier is above the
// operational maximum, so the current-password check fails with the generic
// outcome — self-service repair is impossible for these accounts; they need
// the recovery path.
func TestChangePassword_VerifierAboveMaxCost(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := t.Context()
	svc := newTestService(db)

	highCostHash, err := bcrypt.GenerateFromPassword([]byte("password123"), 14)
	if err != nil {
		t.Fatalf("bcrypt cost 14: %v", err)
	}
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "legacy05",
		Username:     "HighCost5",
		PasswordHash: string(highCostHash),
	})

	err = svc.ChangePassword(ctx, "legacy05", "password123", "newpassword456")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("above-max verifier: got %v, want ErrInvalidCredentials", err)
	}
}

func TestForgotPassword_UnknownIdentifier(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	sender := &fakeSender{}
	svc := newTestServiceWith(db, sender, "http://localhost:3000")

	userID, err := svc.ForgotPassword(t.Context(), "nobody")
	if err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	if userID != "" {
		t.Errorf("userID = %q, want empty for unknown", userID)
	}
	if sender.count() != 0 {
		t.Errorf("sent %d emails, want 0", sender.count())
	}
}

func TestForgotPassword_KnownUserSendsEmail(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	sender := &fakeSender{}
	svc := newTestServiceWith(db, sender, "http://localhost:3000")
	ctx := t.Context()

	_, _, user, err := svc.Register(ctx, "resetme", "resetme@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	userID, err := svc.ForgotPassword(ctx, "resetme")
	if err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	if userID != user.ID {
		t.Errorf("userID = %q, want %q", userID, user.ID)
	}
	if sender.count() != 1 {
		t.Fatalf("sent %d emails, want 1", sender.count())
	}
	last := sender.last()
	if last.to != "resetme@example.com" {
		t.Errorf("email to = %q", last.to)
	}
	token := linkToken(last.body)
	if len(token) != 43 {
		t.Errorf("link token length = %d, want 43", len(token))
	}
	if !strings.Contains(last.body, "http://localhost:3000/auth/reset?token=") {
		t.Errorf("link origin wrong: %q", last.body)
	}

	// A second request replaces the token (one active per user).
	if _, err := svc.ForgotPassword(ctx, "resetme"); err != nil {
		t.Fatalf("second ForgotPassword: %v", err)
	}
	second := linkToken(sender.last().body)
	if second == token {
		t.Error("second request did not mint a fresh token")
	}
}

// TestForgotPassword_ImportedWhitespaceUsername pins that the lenient lookup
// serves self-service recovery too, so the cohort that can sign in with a
// spaced legacy name can also reset its password.
func TestForgotPassword_ImportedWhitespaceUsername(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	sender := &fakeSender{}
	svc := newTestServiceWith(db, sender, "http://localhost:3000")

	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "legacy21",
		Username:     "Jane Doe ",
		Email:        "janed@example.com",
		PasswordHash: string(mustBcryptHash(t, "password123")),
	})

	userID, err := svc.ForgotPassword(t.Context(), "Jane Doe")
	if err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	if userID != "legacy21" {
		t.Errorf("ForgotPassword(\"Jane Doe\") userID: got %q, want %q", userID, "legacy21")
	}
	if sender.count() != 1 {
		t.Errorf("ForgotPassword(\"Jane Doe\") sent %d emails, want 1", sender.count())
	}
}

func TestForgotPassword_SuspendedTreatedAsUnknown(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	sender := &fakeSender{}
	svc := newTestServiceWith(db, sender, "http://localhost:3000")
	ctx := t.Context()

	_, _, _, err := svc.Register(ctx, "suspended", "suspended@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := store.SuspendUser(ctx, db, mustUserID(t, db, "suspended"), time.Now().UnixMilli()); err != nil {
		t.Fatalf("SuspendUser: %v", err)
	}

	userID, err := svc.ForgotPassword(ctx, "suspended")
	if err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	if userID != "" {
		t.Errorf("userID = %q, want empty for suspended", userID)
	}
	if sender.count() != 0 {
		t.Errorf("sent %d emails, want 0", sender.count())
	}
}

func TestForgotPassword_SendFailureReturnsUserIDAndError(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	sender := &fakeSender{err: errors.New("smtp: connection refused")}
	var registeredID string
	duringSend := -1
	hooked := &sendHookSender{fakeSender: sender, onSend: func() {
		if err := db.QueryRow(`SELECT COUNT(*) FROM password_reset_tokens WHERE user_id = ?`, registeredID).Scan(&duringSend); err != nil {
			t.Errorf("count reset tokens during send: %v", err)
		}
	}}
	svc := newTestServiceWith(db, hooked, "http://localhost:3000")
	ctx := t.Context()

	_, _, user, err := svc.Register(ctx, "sendfail", "sendfail@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	registeredID = user.ID

	userID, err := svc.ForgotPassword(ctx, "sendfail")
	if err == nil {
		t.Fatal("got nil, want send error")
	}
	if userID != user.ID {
		t.Errorf("userID = %q, want %q (the account was identified)", userID, user.ID)
	}
	if duringSend != 1 {
		t.Errorf("reset token rows during the send = %d, want 1 (the commit precedes the send)", duringSend)
	}

	// The token-row commit precedes the send: the row survives the failure.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM password_reset_tokens WHERE user_id = ?`, user.ID).Scan(&n); err != nil {
		t.Fatalf("count reset tokens: %v", err)
	}
	if n != 1 {
		t.Errorf("reset token rows after the send failure = %d, want 1", n)
	}
}

func TestResetPasswordWithToken_FullFlow(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	sender := &fakeSender{}
	svc := newTestServiceWith(db, sender, "http://localhost:3000")
	ctx := t.Context()

	_, _, user, err := svc.Register(ctx, "resetflow", "resetflow@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := svc.ForgotPassword(ctx, "resetflow"); err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	token := linkToken(sender.last().body)

	userID, role, err := svc.ResetPasswordWithToken(ctx, token, "new-password-123")
	if err != nil {
		t.Fatalf("ResetPasswordWithToken: %v", err)
	}
	if userID != user.ID || role != "user" {
		t.Errorf("reset returned (%q, %q), want (%q, user)", userID, role, user.ID)
	}

	// The new password signs in (the old session died with the bump).
	if _, _, _, _, err := svc.SignIn(ctx, "resetflow", "new-password-123", ""); err != nil {
		t.Errorf("sign-in with new password: %v", err)
	}
	// The token is consumed.
	if _, _, err := svc.ResetPasswordWithToken(ctx, token, "another-password-123"); !errors.Is(err, ErrResetTokenInvalid) {
		t.Errorf("second reset: got %v, want ErrResetTokenInvalid", err)
	}
}

func TestResetPasswordWithToken_InvalidToken(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	svc := newTestServiceWith(db, &fakeSender{}, "http://localhost:3000")

	for _, bad := range []string{"", "short", strings.Repeat("A", 43), strings.Repeat("!", 43)} {
		if _, _, err := svc.ResetPasswordWithToken(t.Context(), bad, "valid-password-123"); !errors.Is(err, ErrResetTokenInvalid) {
			t.Errorf("token %q: got %v, want ErrResetTokenInvalid", bad, err)
		}
	}
}

func TestResetPasswordWithToken_WeakPassword(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	sender := &fakeSender{}
	svc := newTestServiceWith(db, sender, "http://localhost:3000")
	ctx := t.Context()

	_, _, _, err := svc.Register(ctx, "weakpw", "weakpw@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := svc.ForgotPassword(ctx, "weakpw"); err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	token := linkToken(sender.last().body)

	_, _, err = svc.ResetPasswordWithToken(ctx, token, "short")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("weak password: got %v, want ErrValidation", err)
	}
	fe, ok := errors.AsType[*FieldError](err)
	if !ok || fe.Field != "password" || fe.Code != "minLength" {
		t.Errorf("field error = %+v, want password/minLength", fe)
	}
	// The token survives a policy rejection — retry with a valid password.
	if _, _, err := svc.ResetPasswordWithToken(ctx, token, "now-a-good-password"); err != nil {
		t.Errorf("retry after policy rejection: %v", err)
	}
}

func TestResetPasswordWithToken_TokenCheckedFirst(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	svc := newTestServiceWith(db, &fakeSender{}, "http://localhost:3000")

	// Bad token + weak password: the token outcome wins (no field leak, no
	// bcrypt run). "!" is not base64url — a decode failure, not a weak
	// password.
	if _, _, err := svc.ResetPasswordWithToken(t.Context(), strings.Repeat("!", 43), "short"); !errors.Is(err, ErrResetTokenInvalid) {
		t.Errorf("got %v, want ErrResetTokenInvalid", err)
	}
}

// TestResetPasswordWithToken_NeverIssuedTokenCheckedBeforePassword pins the
// token-first rule: token EXISTENCE (not just shape) precedes password
// policy, so a well-formed but never-issued token plus a weak password
// answers the generic token-invalid — no field leak.
func TestResetPasswordWithToken_NeverIssuedTokenCheckedBeforePassword(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	svc := newTestServiceWith(db, &fakeSender{}, "http://localhost:3000")

	raw := make([]byte, 32)
	raw[0] = 42
	wellFormed := base64.RawURLEncoding.EncodeToString(raw)
	if _, _, err := svc.ResetPasswordWithToken(t.Context(), wellFormed, "short"); !errors.Is(err, ErrResetTokenInvalid) {
		t.Errorf("got %v, want ErrResetTokenInvalid", err)
	}
}

// TestForgotPassword_UnknownRunsDummyComparison pins the enumeration
// defense: the no-send path runs the fixed dummy bcrypt comparison — the DB
// segment equalizes; the SMTP RTT stays the accepted residual. Deliberately
// serial: swaps the compareDummy seam.
func TestForgotPassword_UnknownRunsDummyComparison(t *testing.T) {
	db := openServiceDB(t)
	svc := newTestServiceWith(db, &fakeSender{}, "http://localhost:3000")

	calls := 0
	original := compareDummy
	compareDummy = func(hash, password []byte) error {
		calls++
		return nil
	}
	t.Cleanup(func() { compareDummy = original })

	if _, err := svc.ForgotPassword(t.Context(), "nobody"); err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	if calls != 1 {
		t.Errorf("dummy comparisons = %d, want 1", calls)
	}
}

// TestForgotPassword_NilSenderFailsClosed pins the admin-surface guard:
// surfaces without a mailer answer an honest error, never a nil panic.
func TestForgotPassword_NilSenderFailsClosed(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	svc := New(db, nil, "")

	_, _, _, err := svc.Register(t.Context(), "nilmail", "nilmail@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := svc.ForgotPassword(t.Context(), "nilmail"); err == nil {
		t.Fatal("got nil, want mail-not-configured error")
	}
}

// TestSendVerificationEmail_SendFailureKeepsTokenRow pins the
// commit-before-send order on the verification mint: the token row survives a
// failed send.
func TestSendVerificationEmail_SendFailureKeepsTokenRow(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	sender := &fakeSender{err: errors.New("smtp: connection refused")}
	var registeredID string
	duringSend := -1
	hooked := &sendHookSender{fakeSender: sender, onSend: func() {
		if err := db.QueryRow(`SELECT COUNT(*) FROM email_verification_tokens WHERE user_id = ?`, registeredID).Scan(&duringSend); err != nil {
			t.Errorf("count verification tokens during send: %v", err)
		}
	}}
	svc := newTestServiceWith(db, hooked, "http://localhost:3000")
	ctx := t.Context()

	_, _, user, err := svc.Register(ctx, "verifyfail", "verifyfail@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	registeredID = user.ID
	if err := svc.SendVerificationEmail(ctx, user.ID); err == nil {
		t.Fatal("got nil, want send error")
	}
	if duringSend != 1 {
		t.Errorf("verification token rows during the send = %d, want 1 (the commit precedes the send)", duringSend)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM email_verification_tokens WHERE user_id = ?`, user.ID).Scan(&n); err != nil {
		t.Fatalf("count verification tokens: %v", err)
	}
	if n != 1 {
		t.Errorf("verification token rows after the send failure = %d, want 1 (the commit precedes the send)", n)
	}
}

// TestRegister_EmailShadowingLegacyUsername pins the shadow rule: a new
// registration cannot claim an email equal to a legacy account's username.
func TestRegister_EmailShadowingLegacyUsername(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	svc := newTestService(db)
	ctx := t.Context()

	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:       "legacyshadow",
		Username: "m@ria",
	})

	_, _, _, err := svc.Register(ctx, "NewUser", "m@ria", "password123", "")
	if !errors.Is(err, ErrEmailTaken) {
		t.Errorf("Register(email shadowing a username) = %v, want ErrEmailTaken", err)
	}
	if _, err := store.UserByCanonical(ctx, db, "newuser"); !errors.Is(err, store.ErrNotFound) {
		t.Error("registration created a user despite the shadow")
	}
}

// TestRegister_EmailShadowingWhitespaceVariantUsername pins the lenient shadow
// rule: a migrated canon differing from the address only in whitespace still
// shadows, because sign-in and recovery resolve through the same lenient
// lookup.
func TestRegister_EmailShadowingWhitespaceVariantUsername(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		username string
	}{
		{"leading space", " m@ria"},
		{"nbsp edge", "m@ria\u00a0"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := openServiceDB(t)
			svc := newTestService(db)
			ctx := t.Context()

			storetest.InsertUser(t, db, storetest.UserSpec{ID: "legacyvar", Username: tt.username})

			_, _, _, err := svc.Register(ctx, "NewVar", "m@ria", "password123", "")
			if !errors.Is(err, ErrEmailTaken) {
				t.Errorf("Register(email shadowing a whitespace-variant username) = %v, want ErrEmailTaken", err)
			}
		})
	}
}

// TestChangeEmail_EmailShadowingLegacyUsername pins the same rule on the
// change surface: another account cannot adopt an email equal to a legacy
// username.
func TestChangeEmail_EmailShadowingLegacyUsername(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	svc := newTestServiceWith(db, &fakeSender{}, "http://localhost:3000")
	ctx := t.Context()

	storetest.InsertUser(t, db, storetest.UserSpec{ID: "legacyshadow2", Username: "m@ria"})
	_, _, user, err := svc.Register(ctx, "Other", "other@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if _, _, _, err := svc.ChangeEmail(ctx, user.ID, "m@ria", "password123"); !errors.Is(err, ErrEmailTaken) {
		t.Errorf("ChangeEmail(to a shadowed address) = %v, want ErrEmailTaken", err)
	}
}

// TestChangeEmail_EmailShadowingWhitespaceVariantUsername pins the lenient
// rule on the change surface.
func TestChangeEmail_EmailShadowingWhitespaceVariantUsername(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		username string
	}{
		{"leading space", " m@ria"},
		{"nbsp edge", "m@ria\u00a0"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := openServiceDB(t)
			svc := newTestServiceWith(db, &fakeSender{}, "http://localhost:3000")
			ctx := t.Context()

			storetest.InsertUser(t, db, storetest.UserSpec{ID: "legacyvar2", Username: tt.username})
			_, _, user, err := svc.Register(ctx, "Other", "other@example.com", "password123", "")
			if err != nil {
				t.Fatalf("Register: %v", err)
			}

			if _, _, _, err := svc.ChangeEmail(ctx, user.ID, "m@ria", "password123"); !errors.Is(err, ErrEmailTaken) {
				t.Errorf("ChangeEmail(to a whitespace-variant shadowed address) = %v, want ErrEmailTaken", err)
			}
		})
	}
}

// TestChangeEmail_OwnLegacyUsernameNotShadowed pins the own-row exemption: a
// legacy account whose username equals the email it adopts is not shadowed by
// itself.
func TestChangeEmail_OwnLegacyUsernameNotShadowed(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	svc := newTestServiceWith(db, &fakeSender{}, "http://localhost:3000")
	ctx := t.Context()

	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "legacyown",
		Username:     "m@ria",
		Email:        "old@example.com",
		PasswordHash: string(mustBcryptHash(t, "password123")),
	})

	u, previousEmail, changed, err := svc.ChangeEmail(ctx, "legacyown", "m@ria", "password123")
	if err != nil {
		t.Fatalf("ChangeEmail(own legacy username) = %v, want success", err)
	}
	if !changed || previousEmail != "old@example.com" || u.Email != "m@ria" {
		t.Errorf("ChangeEmail(own legacy username) = (%q, %q, %v), want (m@ria, old@example.com, true)", u.Email, previousEmail, changed)
	}
}

// TestChangeEmail_ReturnsPreviousEmail pins the previous-address return that
// drives the security notice.
func TestChangeEmail_ReturnsPreviousEmail(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	svc := newTestServiceWith(db, &fakeSender{}, "http://localhost:3000")
	ctx := t.Context()

	_, _, user, err := svc.Register(ctx, "mover", "old@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	u, previousEmail, changed, err := svc.ChangeEmail(ctx, user.ID, "new@example.com", "password123")
	if err != nil {
		t.Fatalf("ChangeEmail: %v", err)
	}
	if !changed {
		t.Error("changed = false, want true")
	}
	if previousEmail != "old@example.com" {
		t.Errorf("previousEmail = %q, want old@example.com", previousEmail)
	}
	if u.Email != "new@example.com" {
		t.Errorf("u.Email = %q, want new@example.com", u.Email)
	}
}

// TestChangeEmail_SameEmailNoopPreviousEmailEmpty pins the no-op's empty
// previous address — the handler must not send a notice for it.
func TestChangeEmail_SameEmailNoopPreviousEmailEmpty(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	svc := newTestServiceWith(db, &fakeSender{}, "http://localhost:3000")
	ctx := t.Context()

	_, _, user, err := svc.Register(ctx, "stayer", "stay@example.com", "password123", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	u, previousEmail, changed, err := svc.ChangeEmail(ctx, user.ID, "STAY@example.com", "password123")
	if err != nil {
		t.Fatalf("ChangeEmail: %v", err)
	}
	if changed || previousEmail != "" {
		t.Errorf("no-op returned (previousEmail=%q, changed=%v), want (\"\", false)", previousEmail, changed)
	}
	if u.Email != "stay@example.com" {
		t.Errorf("u.Email = %q, want stay@example.com", u.Email)
	}
}

// TestSendEmailChangeNotice_NilSenderFailsClosed pins the admin-surface
// guard: a service without a mailer answers an honest error, never a nil
// panic.
func TestSendEmailChangeNotice_NilSenderFailsClosed(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	svc := New(db, nil, "")

	if err := svc.SendEmailChangeNotice(t.Context(), "old@example.com", "new@example.com"); err == nil {
		t.Fatal("got nil, want mail-not-configured error")
	}
}

// sendHookSender wraps fakeSender with a hook that runs inside Send, letting a
// test observe the store at the moment of the send — not only after it.
// fakeSender's own Send runs afterwards, so scripted errors still surface.
type sendHookSender struct {
	*fakeSender
	onSend func()
}

func (s *sendHookSender) Send(ctx context.Context, to, subject, body string) error {
	s.onSend()
	return s.fakeSender.Send(ctx, to, subject, body)
}
