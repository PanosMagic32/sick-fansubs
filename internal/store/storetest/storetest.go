// Package storetest seeds rows for tests in other packages.
//
// It is test support, never production code. The store deliberately exposes no
// test-only creation path: registration goes through
// store.CreateUserAndSession, which expects the caller to already hold a
// password hash and mints a session, so a test that needs a bare user row seeds
// one here instead.
package storetest

import (
	"database/sql"
	"strings"
	"testing"
)

// PasswordPlaceholder is the verifier InsertUser stores when the spec leaves
// PasswordHash empty, so a test that asserts on the stored password compares
// against this constant instead of a literal.
const PasswordPlaceholder = "not-a-real-hash"

// UserSpec describes the row InsertUser writes. ID and Username are required;
// every other zero value takes the fixture's usual shape: the lowercased
// username as the canonical form and as the local part of the address —
// registration lowercases addresses, so the default address is
// <lowercased-username>@example.com — role "user", status "active",
// auth_version 1, PasswordPlaceholder as the verifier, no avatar, no forced
// password change, and created_at_ms and updated_at_ms 1000. A zero
// CreatedAtMS selects that default — the schema requires a positive instant,
// just as it requires a positive auth_version, so neither zero is a legal row
// and both defaults are safe. The verifier and auth_version are overridable
// because a test that signs in or drives the credential CAS needs its own
// values.
type UserSpec struct {
	ID                 string
	Username           string
	UsernameCanon      string
	Email              string
	PasswordHash       string
	Role               string
	Status             string
	AvatarURL          string
	MustChangePassword bool
	AuthVersion        int64
	CreatedAtMS        int64
}

// InsertUser writes one user row and fails the test on error.
func InsertUser(t testing.TB, db *sql.DB, spec UserSpec) {
	t.Helper()
	if spec.ID == "" || spec.Username == "" {
		t.Fatalf("InsertUser: ID and Username are required, got id=%q username=%q", spec.ID, spec.Username)
	}
	canon := spec.UsernameCanon
	if canon == "" {
		canon = strings.ToLower(spec.Username)
	}
	email := spec.Email
	if email == "" {
		email = strings.ToLower(spec.Username) + "@example.com"
	}
	role := spec.Role
	if role == "" {
		role = "user"
	}
	status := spec.Status
	if status == "" {
		status = "active"
	}
	var avatar any
	if spec.AvatarURL != "" {
		avatar = spec.AvatarURL
	}
	verifier := spec.PasswordHash
	if verifier == "" {
		verifier = PasswordPlaceholder
	}
	mustChange := 0
	if spec.MustChangePassword {
		mustChange = 1
	}
	authVersion := spec.AuthVersion
	if authVersion == 0 {
		authVersion = 1
	}
	createdAt := spec.CreatedAtMS
	if createdAt == 0 {
		createdAt = 1000
	}
	const q = `INSERT INTO users (id, username, username_canon, email, password,
		role, status, auth_version, avatar_url, must_change_password, created_at_ms, updated_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := db.ExecContext(t.Context(), q,
		spec.ID, spec.Username, canon, email, verifier,
		role, status, authVersion, avatar, mustChange, createdAt, createdAt); err != nil {
		t.Fatalf("InsertUser(%q): %v", spec.ID, err)
	}
}
