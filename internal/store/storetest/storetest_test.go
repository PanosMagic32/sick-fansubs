package storetest

import (
	"database/sql"
	"testing"

	"sick-fansubs/internal/database"
)

// openTestDB returns a migrated temporary database.
func openTestDB(t *testing.T) *sql.DB {
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

// seededUser is the row shape read back for comparison.
type seededUser struct {
	username, canon, email, role, status, password string
	avatar                                         sql.NullString
	mustChange                                     int64
	createdAt, updatedAt, authVersion              int64
}

func readUser(t *testing.T, db *sql.DB, id string) seededUser {
	t.Helper()
	var got seededUser
	err := db.QueryRow(`SELECT username, username_canon, email, role, status, password,
		avatar_url, must_change_password, created_at_ms, updated_at_ms, auth_version
		FROM users WHERE id = ?`, id).
		Scan(&got.username, &got.canon, &got.email, &got.role, &got.status, &got.password,
			&got.avatar, &got.mustChange, &got.createdAt, &got.updatedAt, &got.authVersion)
	if err != nil {
		t.Fatalf("read seeded user %s: %v", id, err)
	}
	return got
}

func TestInsertUser_WritesDefaults(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	InsertUser(t, db, UserSpec{ID: "u1", Username: "Katakuri"})

	want := seededUser{
		username: "Katakuri", canon: "katakuri", email: "katakuri@example.com",
		role: "user", status: "active", password: PasswordPlaceholder,
		createdAt: 1000, updatedAt: 1000, authVersion: 1,
	}
	if got := readUser(t, db, "u1"); got != want {
		t.Errorf("InsertUser(defaults) row = %+v, want %+v", got, want)
	}
}

func TestInsertUser_WritesOverrides(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	InsertUser(t, db, UserSpec{
		ID: "u2", Username: "Moddy", UsernameCanon: "moddy", Email: "moddy@example.com",
		PasswordHash: "$2a$12$seeded-verifier", Role: "moderator", Status: "suspended",
		AvatarURL: "media/avatars/ab/x.jpg", MustChangePassword: true,
		AuthVersion: 3, CreatedAtMS: 5000,
	})

	want := seededUser{
		username: "Moddy", canon: "moddy", email: "moddy@example.com",
		role: "moderator", status: "suspended", password: "$2a$12$seeded-verifier",
		avatar:      sql.NullString{String: "media/avatars/ab/x.jpg", Valid: true},
		mustChange:  1,
		createdAt:   5000,
		updatedAt:   5000,
		authVersion: 3,
	}
	if got := readUser(t, db, "u2"); got != want {
		t.Errorf("InsertUser(overrides) row = %+v, want %+v", got, want)
	}
}
