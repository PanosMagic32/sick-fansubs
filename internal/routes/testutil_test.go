package routes

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"mime/multipart"
	"os"
	"testing"

	"sick-fansubs/internal/store"
)

// testDataDir returns an owner-only temporary directory for tests that open a
// database: database.Open refuses a data directory with group or other access,
// and t.TempDir can inherit wider bits from the environment.
func testDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod test data dir: %v", err)
	}
	return dir
}

// setupUploadSession creates a user with the given role and a live session
// row, returning the sf_session cookie value and the CSRF header value.
// Identity fields derive from the role so multiple roles can coexist in one
// database (email/username_canon are UNIQUE).
func setupUploadSession(t *testing.T, db *sql.DB, role string) (cookie, csrf string) {
	t.Helper()

	rawToken := make([]byte, 32)
	rawCSRF := make([]byte, 32)
	// Role-derived bytes: token_digest and the session id are UNIQUE, so
	// every role must mint distinct material.
	seed := byte(len(role) * 7)
	for i := range rawToken {
		rawToken[i] = byte(i+1) + seed
		rawCSRF[i] = byte(255-i) - seed
	}

	// Digest via the same sha256 the middleware computes (no import cycle:
	// crypto/sha256 here, the middleware's SessionDigestValue is tested in
	// its own suite).
	h := sha256.Sum256(rawToken)
	if err := store.CreateUserAndSession(t.Context(), db, store.CreateUserAndSessionParams{
		ID:            "user-" + role,
		Username:      "User" + role,
		UsernameCanon: "user" + role,
		Email:         role + "@example.com",
		Password:      "not-a-real-hash",
		Role:          role,
		Status:        "active",
		AuthVersion:   1,
		CreatedAtMS:   1_000,
		UpdatedAtMS:   1_000,
		SessionID:     "sess-" + role,
		TokenDigest:   h[:],
		CSRF:          rawCSRF,
		ExpiresAtMS:   9_000_000_000_000,
	}); err != nil {
		t.Fatalf("create user+session: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(rawToken), base64.RawURLEncoding.EncodeToString(rawCSRF)
}

// multipartUploadBody builds a multipart/form-data body with one "file"
// part (plus any extra fields) and returns the body bytes and content type.
func multipartUploadBody(t *testing.T, filename string, content []byte, extra map[string]string) ([]byte, string) {
	t.Helper()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	for k, v := range extra {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("write field %s: %v", k, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return buf.Bytes(), w.FormDataContentType()
}
