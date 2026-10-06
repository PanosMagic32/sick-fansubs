package auth

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/mail"
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

// openServiceDB creates a temporary SQLite database with all migrations
// applied and returns the open connection pool.
func openServiceDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := database.Open(database.Config{DataDir: testDataDir(t)})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}

	if err := database.Apply(db); err != nil {
		db.Close()
		t.Fatalf("apply migrations: %v", err)
	}

	t.Cleanup(func() { db.Close() })
	return db
}

// newTestService builds the service with a LOW bcrypt cost so the
// success-path hash GENERATION stays fast in tests (~30 ms instead of
// ~1 s per hash). Verification cost is read from the stored hash, so the
// seam changes nothing about comparison. The production default lives in
// New; TestSignIn_RehashLowCostVerifier deliberately uses the
// real constructor because it pins the production rehash target.
func newTestService(db *sql.DB) *Service {
	svc := New(db, mail.LogLink{}, "")
	svc.bcryptCost = 4
	return svc
}

// newTestServiceWith is the sender/base variant (the reset-email
// tests) — same low-cost seam.
func newTestServiceWith(db *sql.DB, sender mail.Sender, base string) *Service {
	svc := New(db, sender, base)
	svc.bcryptCost = 4
	return svc
}

func mustBcryptHash(t *testing.T, password string) []byte {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		t.Fatalf("bcrypt hash: %v", err)
	}
	return hash
}

// fakeSender records Send calls and returns a scripted error when set.
type fakeSender struct {
	mu   sync.Mutex
	sent []sentMail
	err  error
}

type sentMail struct {
	to      string
	subject string
	body    string
}

func (f *fakeSender) Send(_ context.Context, to, subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, sentMail{to: to, subject: subject, body: body})
	return nil
}

func (f *fakeSender) last() sentMail {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return sentMail{}
	}
	return f.sent[len(f.sent)-1]
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

// linkToken extracts the ?token= value from a reset link (the link line
// ends at the next newline).
func linkToken(link string) string {
	_, after, ok := strings.Cut(link, "?token=")
	if !ok {
		return ""
	}
	token, _, _ := strings.Cut(after, "\n")
	return token
}

func mustUserID(t *testing.T, db *sql.DB, username string) string {
	t.Helper()
	u, err := store.UserByCanonical(context.Background(), db, username)
	if err != nil {
		t.Fatalf("UserByCanonical(%q): %v", username, err)
	}
	return u.ID
}
