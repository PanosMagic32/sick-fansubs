package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"sick-fansubs/internal/auth"
	"sick-fansubs/internal/database"
	"sick-fansubs/internal/mail"
	"sick-fansubs/internal/store"
	"sick-fansubs/internal/store/storetest"
)

// TestRun_ResetsPassword proves the command end to end: it resets the named
// account, prints the temp password exactly once, forces the change, deletes
// the account's sessions, and persists a password_reset audit event with the
// maintenance actor.
func TestRun_ResetsPassword(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process-global state.
	dir := testDataDir(t)
	t.Setenv("DATA_DIR", dir)

	db, err := database.OpenMaintenance(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open maintenance: %v", err)
	}
	if err := database.Apply(db); err != nil {
		t.Fatalf("apply: %v", err)
	}

	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:          "u1",
		Username:    "Katakuri",
		CreatedAtMS: 1_700_000_000_000,
	})
	raw := make([]byte, 32)
	raw[0] = 7
	digest := sha256.Sum256(raw)
	if err := store.CreateSession(t.Context(), db, store.CreateSessionParams{
		ID:          "s1",
		UserID:      "u1",
		TokenDigest: digest[:],
		CSRF:        make([]byte, 32),
		AuthVersion: 1,
		CreatedAtMS: 1_700_000_000_000,
		ExpiresAtMS: 9_000_000_000_000,
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := database.CloseDatabase(db); err != nil {
		t.Fatalf("close: %v", err)
	}

	var out bytes.Buffer
	if err := run(slog.New(slog.NewTextHandler(io.Discard, nil)), "KATAKURI", &out); err != nil {
		t.Fatalf("run: %v", err)
	}

	line := out.String()
	const prefix = "Temporary password for Katakuri: "
	if !strings.HasPrefix(line, prefix) {
		t.Fatalf("stdout = %q, want the one-time temp password line", line)
	}
	temp := strings.TrimSuffix(strings.TrimPrefix(line, prefix), "\n")
	if len(temp) != 16 {
		t.Fatalf("temp password length: got %d, want 16", len(temp))
	}
	if strings.Count(line, temp) != 1 {
		t.Fatal("the temp password must print exactly once")
	}

	// Reopen and verify the post-conditions through the real stores.
	db2, err := database.Open(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer database.CloseDatabase(db2)

	// The temp password signs the user in, carrying the forced-change flag.
	svc := auth.New(db2, mail.LogLink{}, "")
	_, _, _, mustChange, err := svc.SignIn(t.Context(), "katakuri", temp, "")
	if err != nil {
		t.Fatalf("sign-in with temp password: %v", err)
	}
	if !mustChange {
		t.Error("sign-in after the command must carry mustChangePassword: true")
	}

	u, err := store.UserByID(t.Context(), db2, "u1")
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if !u.MustChangePassword {
		t.Error("must_change_password must be set")
	}
	if u.AuthVersion != 2 {
		t.Errorf("auth_version: got %d, want 2", u.AuthVersion)
	}
	if _, err := store.SessionByDigest(t.Context(), db2, digest[:]); err == nil {
		t.Error("the pre-reset session must be deleted")
	}

	// The durable audit event: maintenance actor, target + role snapshot.
	var event, result, actorID, targetID, targetRole string
	err = db2.QueryRow(`SELECT event, result, actor_id, target_id, target_role FROM audit_events`).
		Scan(&event, &result, &actorID, &targetID, &targetRole)
	if err != nil {
		t.Fatalf("read audit event: %v", err)
	}
	if event != "password_reset" || result != "success" {
		t.Errorf("audit event/result: got %q/%q, want password_reset/success", event, result)
	}
	if actorID != "maintenance" || targetID != "u1" || targetRole != "user" {
		t.Errorf("audit actor/target/role: got %q/%q/%q, want maintenance/u1/user", actorID, targetID, targetRole)
	}
}

func TestRun_UnknownUsername(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process-global state.
	dir := testDataDir(t)
	t.Setenv("DATA_DIR", dir)

	db, err := database.OpenMaintenance(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open maintenance: %v", err)
	}
	if err := database.Apply(db); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := database.CloseDatabase(db); err != nil {
		t.Fatalf("close: %v", err)
	}

	if err := run(slog.New(slog.NewTextHandler(io.Discard, nil)), "Nobody", io.Discard); err == nil {
		t.Fatal("expected an error for an unknown username")
	}
}

// errWriter always fails so the temp-password stdout failure arm is reachable.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("stdout write failed") }

// TestRun_TempPasswordWriteFailureIsReported pins the handover contract: the
// temp password is the only copy of the new secret, so a failed stdout write
// must fail the command with the reset's outcome stated.
func TestRun_TempPasswordWriteFailureIsReported(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process-global state.
	dir := testDataDir(t)
	t.Setenv("DATA_DIR", dir)

	db, err := database.OpenMaintenance(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open maintenance: %v", err)
	}
	if err := database.Apply(db); err != nil {
		t.Fatalf("apply: %v", err)
	}
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:          "u1",
		Username:    "Katakuri",
		CreatedAtMS: 1_700_000_000_000,
	})
	if err := database.CloseDatabase(db); err != nil {
		t.Fatalf("close: %v", err)
	}

	err = run(slog.New(slog.NewTextHandler(io.Discard, nil)), "Katakuri", errWriter{})
	if err == nil {
		t.Fatal("run() err = nil, want the write failure reported")
	}
	if !strings.Contains(err.Error(), "temporary password") {
		t.Errorf("err = %q, want the temp-password handover failure named", err)
	}
}

// TestResetDeadlinePinned pins the operation budget, so a silent change is
// caught here.
func TestResetDeadlinePinned(t *testing.T) {
	t.Parallel()

	if resetDeadline != time.Minute {
		t.Errorf("resetDeadline = %s, want 1m", resetDeadline)
	}
}
