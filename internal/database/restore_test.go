package database

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sick-fansubs/internal/store/storetest"
)

// seedUserAndSession inserts one user and one session into db and returns
// the session's token digest (for proving the restored copy has no such
// row). docs/patterns/go/sqlite.md requires restoring to delete every
// session — the seeded row is the one the restore must kill.
func seedUserAndSession(t *testing.T, db *sql.DB) []byte {
	t.Helper()
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "u1",
		Username:     "kushoyarou",
		Email:        "k@example.com",
		PasswordHash: "$2b$12$fake",
		Role:         "super-admin",
	})
	digest := []byte(strings.Repeat("d", 32))
	csrf := []byte(strings.Repeat("c", 32))
	if _, err := db.Exec(`INSERT INTO sessions
		(id, user_id, token_digest, csrf_token, auth_version, created_at_ms, expires_at_ms)
		VALUES ('s1', 'u1', ?, ?, 1, 1000, 2000)`, digest, csrf); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	// One active self-service reset token: the staged
	// restore must purge it alongside the sessions — a restored older
	// backup must not resurrect a consumed link.
	resetDigest := []byte(strings.Repeat("r", 32))
	if _, err := db.Exec(`INSERT INTO password_reset_tokens
		(token_digest, user_id, created_at_ms, expires_at_ms)
		VALUES (?, 'u1', 1000, 2000)`, resetDigest); err != nil {
		t.Fatalf("seed reset token: %v", err)
	}
	// One email-verification token: the same purge invariant.
	verifyDigest := []byte(strings.Repeat("v", 32))
	if _, err := db.Exec(`INSERT INTO email_verification_tokens
		(token_digest, user_id, created_at_ms, expires_at_ms)
		VALUES (?, 'u1', 1000, 2000)`, verifyDigest); err != nil {
		t.Fatalf("seed verification token: %v", err)
	}
	return digest
}

// seedSecurityEvent inserts one SUCCESSFUL account-security audit event at
// created_at_ms (the reconciliation v1 compares the newest of these).
func seedSecurityEvent(t *testing.T, db *sql.DB, createdMs int64) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO audit_events
		(id, event, result, actor_id, target_id, request_id, remote_addr, created_at_ms)
		VALUES (?, 'password_reset', 'success', 'a1', 'u1', 'req-1', '127.0.0.1', ?)`,
		"evt-"+time.Now().Format("150405.000000000"), createdMs); err != nil {
		t.Fatalf("seed security event: %v", err)
	}
}

// sessionCount returns the live session row count.
func sessionCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return n
}

// userRole returns the stored role of user u1.
func userRole(t *testing.T, db *sql.DB) string {
	t.Helper()
	var role string
	if err := db.QueryRow("SELECT role FROM users WHERE id = 'u1'").Scan(&role); err != nil {
		t.Fatalf("read user role: %v", err)
	}
	return role
}

// setupRestoreFixture builds a migrated database with a seeded user + session
// and returns its data directory and a validated backup path.
func setupRestoreFixture(t *testing.T) (dir, backupPath string, digest []byte) {
	t.Helper()
	dir = testDataDir(t)
	db, err := OpenMaintenance(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open maintenance: %v", err)
	}
	digest = seedUserAndSession(t, db)
	if err := db.Close(); err != nil {
		t.Fatalf("close fixture db: %v", err)
	}
	backupPath = backupPathFor(t, dir)
	return dir, backupPath, digest
}

// opDir creates an operation directory named with the current attempt's
// millis — the shape the restore code writes, so the marker's attempt scoping
// accepts it.
func opDir(t *testing.T, dir, prefix string) string {
	t.Helper()
	p := filepath.Join(dir, fmt.Sprintf("%s%d", prefix, time.Now().UnixMilli()))
	if err := os.Mkdir(p, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
	return p
}

// TestStagedRestore_ActivatesAndInvalidatesSessions is the happy path: a
// backup taken before a session was revoked and a role changed, restored
// over the newer live state. The restored database has the OLD user state
// and ZERO sessions — the seeded digest must be gone — the backup artifact
// survives, and the quarantined rollback set is retained
// (docs/patterns/go/sqlite.md).
func TestStagedRestore_ActivatesAndInvalidatesSessions(t *testing.T) {
	dir, backupPath, digest := setupRestoreFixture(t)

	// Post-backup changes: delete the session and demote the user.
	db, err := Open(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open live: %v", err)
	}
	if _, err := db.Exec("DELETE FROM sessions"); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	if _, err := db.Exec("UPDATE users SET role = 'user' WHERE id = 'u1'"); err != nil {
		t.Fatalf("demote user: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close live: %v", err)
	}

	res, err := StagedRestore(StagedRestoreOptions{DataDir: dir, BackupPath: backupPath})
	if err != nil {
		t.Fatalf("StagedRestore: %v", err)
	}

	// Marker removed; quarantine set retained.
	present, err := restoreMarkerExists(dir)
	if err != nil || present {
		t.Fatalf("marker present after successful restore (present=%v, err=%v)", present, err)
	}
	if res.QuarantineDir == "" {
		t.Fatal("expected a retained quarantine directory")
	}
	if _, err := os.Stat(filepath.Join(res.QuarantineDir, FileName)); err != nil {
		t.Fatalf("quarantined main file missing: %v", err)
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("backup artifact was not retained: %v", err)
	}

	// The strict application opener accepts the activated file.
	app, err := OpenApplication(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("OpenApplication after restore: %v", err)
	}
	defer app.Close()

	// The pre-backup role is back (the rollback returned the old state)…
	if got := userRole(t, app); got != "super-admin" {
		t.Fatalf("got %q, want restored role super-admin", got)
	}
	// …and the sessions are gone: zero rows AND the seeded digest is absent.
	if n := sessionCount(t, app); n != 0 {
		t.Fatalf("got %d, want 0 sessions after restore", n)
	}
	var found int
	if err := app.QueryRow("SELECT COUNT(*) FROM sessions WHERE token_digest = ?", digest).Scan(&found); err != nil {
		t.Fatalf("query seeded digest: %v", err)
	}
	if found != 0 {
		t.Fatal("the restored database still contains the revoked session digest — a restored session could authenticate")
	}
	// Reset tokens die with the sessions: a restored
	// older backup must not resurrect a consumed link.
	if n := resetTokenCount(t, app); n != 0 {
		t.Fatalf("got %d, want 0 password_reset_tokens after restore", n)
	}
	// Verification tokens die too — one purge invariant across every token
	// table.
	if n := verificationTokenCount(t, app); n != 0 {
		t.Fatalf("got %d, want 0 email_verification_tokens after restore", n)
	}
}

// resetTokenCount returns the password_reset_tokens row count.
func resetTokenCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM password_reset_tokens").Scan(&n); err != nil {
		t.Fatalf("count reset tokens: %v", err)
	}
	return n
}

// verificationTokenCount returns the email_verification_tokens row count.
func verificationTokenCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM email_verification_tokens").Scan(&n); err != nil {
		t.Fatalf("count verification tokens: %v", err)
	}
	return n
}

// TestStagedRestore_OlderSchemaBackupUpgrades proves step 4: a backup with a
// STRICT OLDER migration prefix is upgraded through the maintenance opener
// during staging and activates as a current-history database. The LIVE
// database is current-schema (as in production) — only the backup is older.
func TestStagedRestore_OlderSchemaBackupUpgrades(t *testing.T) {
	dir := testDataDir(t)

	// 1. Build a database one migration behind (a strict older prefix) with
	//    one user row, and take its backup — the older-schema artifact.
	db, err := Open(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	migrations, err := loadedMigrations()
	if err != nil {
		db.Close()
		t.Fatalf("loadedMigrations: %v", err)
	}
	if len(migrations) < 2 {
		db.Close()
		t.Fatalf("got %d, want at least 2 embedded migrations", len(migrations))
	}
	if err := bootstrapMigrations(db); err != nil {
		db.Close()
		t.Fatalf("bootstrapMigrations: %v", err)
	}
	if err := applyOne(db, migrations[0]); err != nil {
		db.Close()
		t.Fatalf("apply migration 1: %v", err)
	}
	// Raw on purpose: only migration 1 is applied here, so the columns the
	// fixture writes (must_change_password, avatar_url) do not exist yet.
	if _, err := db.Exec(`INSERT INTO users
		(id, username, username_canon, email, password, role, status, auth_version, created_at_ms, updated_at_ms)
		VALUES ('u1', 'kushoyarou', 'kushoyarou', 'k@example.com', '$2b$12$fake', 'super-admin', 'active', 1, 1000, 1000)`); err != nil {
		db.Close()
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	backupPath := backupPathFor(t, dir)

	// 2. Upgrade the live database to current history and delete the user —
	//    the state the restore must roll back over.
	db, err = OpenMaintenance(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("upgrade live: %v", err)
	}
	if _, err := db.Exec("DELETE FROM users WHERE id = 'u1'"); err != nil {
		db.Close()
		t.Fatalf("delete user: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close live: %v", err)
	}

	// 3. Restore the older-schema backup over the current-schema live DB.
	_, err = StagedRestore(StagedRestoreOptions{DataDir: dir, BackupPath: backupPath})
	if err != nil {
		t.Fatalf("StagedRestore from older-prefix backup: %v", err)
	}

	app, err := OpenApplication(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("OpenApplication after older-schema restore: %v", err)
	}
	defer app.Close()

	// The staged copy was upgraded, activated, and carries the old user back.
	var n int
	if err := app.QueryRow("SELECT COUNT(*) FROM users WHERE id = 'u1'").Scan(&n); err != nil {
		t.Fatalf("count restored user: %v", err)
	}
	if n != 1 {
		t.Fatalf("got %d rows, want the older backup's user back after upgrade-restore", n)
	}
}

// TestStagedRestore_RejectsCorruptBackup pins the fail-closed validation
// order: an unverifiable backup is refused BEFORE the marker is created and
// the live database is left byte-for-byte intact.
func TestStagedRestore_RejectsCorruptBackup(t *testing.T) {
	dir, backupPath, _ := setupRestoreFixture(t)

	// Corrupt the backup artifact.
	if err := os.WriteFile(backupPath, []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatalf("corrupt backup: %v", err)
	}

	_, err := StagedRestore(StagedRestoreOptions{DataDir: dir, BackupPath: backupPath})
	if err == nil {
		t.Fatal("expected error for a corrupt backup")
	}

	present, err := restoreMarkerExists(dir)
	if err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("marker created for a backup that failed validation")
	}
	app, err := OpenApplication(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("live database changed by a rejected restore: %v", err)
	}
	app.Close()
}

// TestStagedRestore_RefusesNewerSecurityEvents pins reconciliation v1: a
// SUCCESSFUL account-security event newer than the backup refuses activation
// without the override, cleans up (live untouched, marker removed), and
// proceeds with it.
func TestStagedRestore_RefusesNewerSecurityEvents(t *testing.T) {
	dir, backupPath, _ := setupRestoreFixture(t)

	// A password reset happened AFTER the backup.
	db, err := Open(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open live: %v", err)
	}
	seedSecurityEvent(t, db, time.Now().UnixMilli())
	if err := db.Close(); err != nil {
		t.Fatalf("close live: %v", err)
	}

	// Without the override: refused, marker cleaned up, live intact.
	if _, err := StagedRestore(StagedRestoreOptions{DataDir: dir, BackupPath: backupPath}); err == nil {
		t.Fatal("expected refusal without -acknowledge-loss")
	}
	present, err := restoreMarkerExists(dir)
	if err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("marker left behind by a pre-activation refusal")
	}
	app, err := OpenApplication(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("live database changed by a refused restore: %v", err)
	}
	if n := sessionCount(t, app); n != 1 {
		t.Fatalf("got %d sessions, want the live session to survive a refused restore", n)
	}
	app.Close()

	// With the override: activation proceeds and reports the acknowledged loss.
	res, err := StagedRestore(StagedRestoreOptions{DataDir: dir, BackupPath: backupPath, AcknowledgeLoss: true})
	if err != nil {
		t.Fatalf("StagedRestore with acknowledge: %v", err)
	}
	if !res.AcknowledgedLoss || res.LiveNewestMS <= res.BackupNewestMS {
		t.Fatalf("got %+v, want acknowledged loss with liveNewest > backupNewest", res)
	}
}

// TestStagedRestore_NoNewerEventsProceeds pins the happy reconciliation
// case: equal or absent security state needs no override.
func TestStagedRestore_NoNewerEventsProceeds(t *testing.T) {
	dir, backupPath, _ := setupRestoreFixture(t)

	res, err := StagedRestore(StagedRestoreOptions{DataDir: dir, BackupPath: backupPath})
	if err != nil {
		t.Fatalf("StagedRestore: %v", err)
	}
	if res.AcknowledgedLoss {
		t.Fatal("unexpected acknowledged-loss flag with no newer events")
	}
}

// TestStagedRestore_MarkerGatesOpenAndReadiness pins the fail-closed gates:
// while the marker exists, the strict application opener AND the readiness
// checker refuse before touching the database.
func TestStagedRestore_MarkerGatesOpenAndReadiness(t *testing.T) {
	dir := testDataDir(t)
	db, err := OpenMaintenance(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open maintenance: %v", err)
	}
	db.Close()

	if err := acquireRestoreMarker(dir); err != nil {
		t.Fatalf("acquireRestoreMarker: %v", err)
	}

	if _, err := OpenApplication(Config{DataDir: dir}); err == nil {
		t.Fatal("OpenApplication accepted a database under a restore marker")
	}

	// The readiness checker must report not-ready too (docs/patterns/go/sqlite.md).
	live, err := Open(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open live: %v", err)
	}
	defer live.Close()
	checker := DBHealthChecker{DB: live, DataDir: dir}
	if err := checker.Check(t.Context()); err == nil {
		t.Fatal("readiness reported ready while a restore marker is present")
	}

	if err := removeRestoreMarker(dir); err != nil {
		t.Fatalf("removeRestoreMarker: %v", err)
	}
	if _, err := OpenApplication(Config{DataDir: dir}); err != nil {
		t.Fatalf("OpenApplication after marker removal: %v", err)
	}
}

// TestStagedRestore_PreexistingWALSidecarsQuarantined pins step 7: stale
// final-name WAL/SHM files are moved into the quarantine set together with
// the main file, never left behind to pair with the activated file.
func TestStagedRestore_PreexistingWALSidecarsQuarantined(t *testing.T) {
	dir, backupPath, _ := setupRestoreFixture(t)

	for _, name := range []string{FileName + "-wal", FileName + "-shm"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatalf("create stale %s: %v", name, err)
		}
	}

	res, err := StagedRestore(StagedRestoreOptions{DataDir: dir, BackupPath: backupPath})
	if err != nil {
		t.Fatalf("StagedRestore: %v", err)
	}

	for _, name := range []string{FileName + "-wal", FileName + "-shm"} {
		if _, err := os.Stat(filepath.Join(res.QuarantineDir, name)); err != nil {
			t.Fatalf("stale %s not quarantined: %v", name, err)
		}
	}

	app, err := OpenApplication(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("OpenApplication after restore: %v", err)
	}
	app.Close()
}

// TestStagedRestore_UndoActivationRestoresQuarantine pins the failure-after-
// activation mechanism: undoing an activation removes the promoted file and
// puts the quarantined rollback set back.
func TestStagedRestore_UndoActivationRestoresQuarantine(t *testing.T) {
	dir, backupPath, _ := setupRestoreFixture(t)

	// Post-backup state change so activation vs rollback is observable.
	db, err := Open(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open live: %v", err)
	}
	if _, err := db.Exec("UPDATE users SET role = 'user' WHERE id = 'u1'"); err != nil {
		t.Fatalf("demote user: %v", err)
	}
	db.Close()

	// Stage + prepare + finalize by hand (the marker-free mechanism) — the
	// same sequence the real flow runs before reaching the activation window.
	stagedDir, err := stageAndPrepare(dir, backupPath, 0)
	if err != nil {
		t.Fatalf("stageAndPrepare: %v", err)
	}
	if err := finalizeStaged(stagedDir); err != nil {
		t.Fatalf("finalizeStaged: %v", err)
	}
	quarantineDir, err := activate(dir, stagedDir, 0)
	if err != nil {
		t.Fatalf("activate: %v", err)
	}

	// The activated file carries the OLD role…
	app, err := Open(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open activated: %v", err)
	}
	if got := userRole(t, app); got != "super-admin" {
		t.Fatalf("got %q, want activated role super-admin", got)
	}
	app.Close()

	// …and undoing the activation brings the quarantined NEWER state back.
	if err := undoActivation(dir, quarantineDir); err != nil {
		t.Fatalf("undoActivation: %v", err)
	}
	if _, err := os.Stat(quarantineDir); !os.IsNotExist(err) {
		t.Fatal("quarantine directory still exists after undoActivation")
	}
	app, err = OpenApplication(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("OpenApplication after undoActivation: %v", err)
	}
	if got := userRole(t, app); got != "user" {
		t.Fatalf("got %q, want rolled-back role user", got)
	}
	app.Close()
}

// TestStagedRestore_ResumeFailsClosedOnOldLayoutMarker pins the transition
// rule: a marker written without the attempt identifier cannot be scoped, so
// the resume stops for manual recovery instead of guessing at the on-disk
// state.
func TestStagedRestore_ResumeFailsClosedOnOldLayoutMarker(t *testing.T) {
	dir, backupPath, _ := setupRestoreFixture(t)

	if err := os.WriteFile(restoreMarkerPath(dir), []byte("restore started 2026-01-01T00:00:00Z\n"), 0o600); err != nil {
		t.Fatalf("write old-layout marker: %v", err)
	}
	_, err := StagedRestore(StagedRestoreOptions{DataDir: dir, BackupPath: backupPath})
	if err == nil {
		t.Fatal("expected the old-layout marker to fail closed")
	}
	if !strings.Contains(err.Error(), "manual recovery") {
		t.Errorf("error should name manual recovery, got: %v", err)
	}
	if present, err := restoreMarkerExists(dir); err != nil {
		t.Fatal(err)
	} else if !present {
		t.Fatal("marker must stay for manual recovery")
	}
}

// TestOperationMillisFloorsAtAttempt pins the backward-clock guard: an
// operation-directory suffix is never below the marker's attempt millis, so a
// clock step back cannot hide the current attempt's directories from resume.
func TestOperationMillisFloorsAtAttempt(t *testing.T) {
	future := time.Now().Add(time.Hour).UnixMilli()
	if got := operationMillis(future); got != future {
		t.Errorf("operationMillis(future) = %d, want %d", got, future)
	}
	past := time.Now().Add(-time.Hour).UnixMilli()
	if got := operationMillis(past); got <= past {
		t.Errorf("operationMillis(past) = %d, want a value above %d", got, past)
	}
}

// TestStagedRestore_ResumeAfterCrashBeforeActivation pins crash recovery:
// a marker plus live main plus staged leftovers (crash before quarantine)
// aborts cleanly — live untouched, marker removed.
func TestStagedRestore_ResumeAfterCrashBeforeActivation(t *testing.T) {
	dir, backupPath, _ := setupRestoreFixture(t)

	if err := acquireRestoreMarker(dir); err != nil {
		t.Fatalf("acquireRestoreMarker: %v", err)
	}
	stagedDir := opDir(t, dir, restoreStagedPrefix)
	if err := copyFile(backupPath, filepath.Join(stagedDir, FileName)); err != nil {
		t.Fatalf("copy backup: %v", err)
	}

	_, err := StagedRestore(StagedRestoreOptions{DataDir: dir, BackupPath: backupPath})
	if err == nil {
		t.Fatal("expected the resume-abort to report the abandoned attempt")
	}

	present, err := restoreMarkerExists(dir)
	if err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("marker not removed by the resume-abort")
	}
	if _, err := os.Stat(stagedDir); !os.IsNotExist(err) {
		t.Fatal("staged leftovers not discarded by the resume-abort")
	}
	app, err := OpenApplication(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("live database damaged by resume-abort: %v", err)
	}
	if n := sessionCount(t, app); n != 1 {
		t.Fatalf("got %d, want live session to survive", n)
	}
	app.Close()
}

// TestStagedRestore_ResumeActivationAfterCrash pins the other recovery
// branch: crash between quarantine and promotion (live main quarantined,
// staged prepared, marker present) — the command finishes the activation.
func TestStagedRestore_ResumeActivationAfterCrash(t *testing.T) {
	dir, backupPath, _ := setupRestoreFixture(t)

	// Simulate the crash window: marker, staged copy prepared AND finalized
	// (the real flow finalizes before quarantining), live main quarantined
	// (main first, sidecars left behind).
	if err := acquireRestoreMarker(dir); err != nil {
		t.Fatalf("acquireRestoreMarker: %v", err)
	}
	stagedDir, err := stageAndPrepare(dir, backupPath, 0)
	if err != nil {
		t.Fatalf("stageAndPrepare: %v", err)
	}
	if err := finalizeStaged(stagedDir); err != nil {
		t.Fatalf("finalizeStaged: %v", err)
	}
	quarantineDir := opDir(t, dir, restoreQuarantinePrefix)
	if err := os.Rename(filepath.Join(dir, FileName), filepath.Join(quarantineDir, FileName)); err != nil {
		t.Fatalf("quarantine live main: %v", err)
	}

	res, err := StagedRestore(StagedRestoreOptions{DataDir: dir, BackupPath: backupPath})
	if err != nil {
		t.Fatalf("StagedRestore resume-activation: %v", err)
	}

	present, err := restoreMarkerExists(dir)
	if err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("marker still present after resume-activation")
	}
	if res.QuarantineDir != quarantineDir {
		t.Fatalf("got %q, want the resumed quarantine dir %q", res.QuarantineDir, quarantineDir)
	}
	if _, err := os.Stat(stagedDir); !os.IsNotExist(err) {
		t.Fatal("staged directory not consumed by resume-activation")
	}

	app, err := OpenApplication(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("OpenApplication after resume-activation: %v", err)
	}
	defer app.Close()
	if n := sessionCount(t, app); n != 0 {
		t.Fatalf("got %d, want 0 sessions after resume-activation", n)
	}
}

// TestStagedRestore_ResumeAfterCompletedActivation pins the crash point
// between activation and marker removal: the activation record is present, so
// the command re-runs the post-activation step and finishes.
func TestStagedRestore_ResumeAfterCompletedActivation(t *testing.T) {
	dir, backupPath, _ := setupRestoreFixture(t)

	res, err := StagedRestore(StagedRestoreOptions{DataDir: dir, BackupPath: backupPath})
	if err != nil {
		t.Fatalf("first restore: %v", err)
	}

	// Simulate a crash right before marker removal: the marker and the
	// activation record of the in-flight attempt come back, the record naming
	// a quarantine created after the marker (the state activation writes).
	if err := acquireRestoreMarker(dir); err != nil {
		t.Fatalf("re-create marker: %v", err)
	}
	inFlight := opDir(t, dir, restoreQuarantinePrefix)
	if err := writeActivationRecord(dir, inFlight); err != nil {
		t.Fatalf("writeActivationRecord: %v", err)
	}

	res2, err := StagedRestore(StagedRestoreOptions{DataDir: dir, BackupPath: backupPath})
	if err != nil {
		t.Fatalf("resume after completed activation: %v", err)
	}
	if res2.QuarantineDir != inFlight {
		t.Fatalf("got %q, want the in-flight quarantine dir %q", res2.QuarantineDir, inFlight)
	}

	present, err := restoreMarkerExists(dir)
	if err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("marker still present after resume")
	}
	if _, err := os.Stat(activationRecordPath(dir)); !os.IsNotExist(err) {
		t.Error("activation record not removed by the resume")
	}
	// The earlier restore's retained rollback set is untouched.
	if _, err := os.Stat(res.QuarantineDir); err != nil {
		t.Errorf("retained rollback set disturbed: %v", err)
	}
	app, err := OpenApplication(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("OpenApplication after resume: %v", err)
	}
	app.Close()
}

// TestStagedRestore_ResumeAfterFreshVolumeHookFailure pins the fresh-volume
// resume: activation succeeded but the media hook failed, leaving the marker
// and an EMPTY quarantine. The re-run must run the hook and finish — not read
// the empty quarantine as an abandoned attempt and drop the marker with the
// media never extracted.
func TestStagedRestore_ResumeAfterFreshVolumeHookFailure(t *testing.T) {
	_, backupPath, _ := setupRestoreFixture(t)
	fresh := testDataDir(t)

	_, err := StagedRestore(StagedRestoreOptions{
		DataDir:    fresh,
		BackupPath: backupPath,
		PostActivation: func() error {
			return errors.New("media extraction failed")
		},
	})
	if err == nil {
		t.Fatal("expected the hook failure to abort the first run")
	}
	present, err := restoreMarkerExists(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if !present {
		t.Fatal("marker not retained after the hook failure")
	}

	calls := 0
	res, err := StagedRestore(StagedRestoreOptions{
		DataDir:    fresh,
		BackupPath: backupPath,
		PostActivation: func() error {
			calls++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("resume after a fresh-volume hook failure: %v", err)
	}
	if calls != 1 {
		t.Errorf("hook calls on the resumed run = %d, want 1", calls)
	}
	if res.QuarantineDir == "" {
		t.Fatal("resumed result carries no quarantine dir")
	}
	if present, err := restoreMarkerExists(fresh); err != nil {
		t.Fatal(err)
	} else if present {
		t.Fatal("marker still present after the resumed run")
	}
	if _, err := os.Stat(activationRecordPath(fresh)); !os.IsNotExist(err) {
		t.Error("activation record not removed after the resumed run")
	}
	app, err := OpenApplication(Config{DataDir: fresh})
	if err != nil {
		t.Fatalf("OpenApplication after the resumed run: %v", err)
	}
	defer app.Close()
	if n := sessionCount(t, app); n != 0 {
		t.Fatalf("got %d sessions, want 0 on the restored fresh volume", n)
	}
}

// TestStagedRestore_ResumeIgnoresRetainedQuarantine pins the attempt
// scoping: a retained quarantine from an earlier SUCCESSFUL restore must not
// be read as the current attempt's activation. The state simulates a second
// restore that crashed before activating — the resume must abort and leave
// both the live database and the retained rollback set untouched.
func TestStagedRestore_ResumeIgnoresRetainedQuarantine(t *testing.T) {
	dir, backupPath, _ := setupRestoreFixture(t)

	first, err := StagedRestore(StagedRestoreOptions{DataDir: dir, BackupPath: backupPath})
	if err != nil {
		t.Fatalf("first restore: %v", err)
	}

	// Seed a live session again so "live untouched" is observable.
	db, err := Open(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open live: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO sessions
		(id, user_id, token_digest, csrf_token, auth_version, created_at_ms, expires_at_ms)
		VALUES ('s2', 'u1', ?, ?, 1, 1000, 2000)`, []byte(strings.Repeat("e", 32)), []byte(strings.Repeat("f", 32))); err != nil {
		t.Fatalf("seed live session: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close live: %v", err)
	}

	// Simulate the second attempt crashing pre-activation: marker + a staged
	// copy, no activation record.
	if err := acquireRestoreMarker(dir); err != nil {
		t.Fatalf("acquireRestoreMarker: %v", err)
	}
	stagedDir := opDir(t, dir, restoreStagedPrefix)
	if err := copyFile(backupPath, filepath.Join(stagedDir, FileName)); err != nil {
		t.Fatalf("copy staged: %v", err)
	}

	_, err = StagedRestore(StagedRestoreOptions{DataDir: dir, BackupPath: backupPath})
	if err == nil {
		t.Fatal("expected the resume to abort, not complete against the retained quarantine")
	}

	present, err := restoreMarkerExists(dir)
	if err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("marker not removed by the resume-abort")
	}
	if _, err := os.Stat(stagedDir); !os.IsNotExist(err) {
		t.Fatal("staged leftovers not discarded by the resume-abort")
	}
	app, err := OpenApplication(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("live database damaged by resume-abort: %v", err)
	}
	if n := sessionCount(t, app); n != 1 {
		t.Fatalf("got %d, want the live session to survive", n)
	}
	app.Close()
	if _, err := os.Stat(first.QuarantineDir); err != nil {
		t.Errorf("retained rollback set from the first restore was disturbed: %v", err)
	}
}

// TestStagedRestore_ResumeNoRecordQuarantineWithMainFailsClosed pins the
// ambiguous state: a marker plus a post-marker quarantine holding the main
// file, with no activation record (a marker from an older layout, or an
// interrupted rollback). The resume must fail closed rather than overwrite
// live with the quarantine.
func TestStagedRestore_ResumeNoRecordQuarantineWithMainFailsClosed(t *testing.T) {
	dir, backupPath, _ := setupRestoreFixture(t)

	if err := acquireRestoreMarker(dir); err != nil {
		t.Fatalf("acquireRestoreMarker: %v", err)
	}
	quarantineDir := opDir(t, dir, restoreQuarantinePrefix)
	if err := copyFile(filepath.Join(dir, FileName), filepath.Join(quarantineDir, FileName)); err != nil {
		t.Fatalf("copy live main into quarantine: %v", err)
	}

	_, err := StagedRestore(StagedRestoreOptions{DataDir: dir, BackupPath: backupPath})
	if err == nil {
		t.Fatal("expected fail-closed manual recovery, not a silent activation")
	}
	present, err := restoreMarkerExists(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !present {
		t.Fatal("marker must stay for manual recovery")
	}
	// A failed resume must leave the decision to the operator: the marker
	// stays so the application keeps refusing to open, and the live database
	// is untouched. The ad-hoc opener reads it while the marker is present.
	app, err := Open(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer app.Close()
	if n := sessionCount(t, app); n != 1 {
		t.Fatalf("got %d, want the live session to survive", n)
	}
}

// TestStagedRestore_ResumeWithEmptyQuarantineAborts pins the abandoned
// attempt: a crash between creating the quarantine directory and the first
// rename leaves an EMPTY quarantine next to an untouched live main and no
// activation record. Resume must NOT read that as a completed activation — it
// aborts cleanly, restores nothing, and the live database stays intact.
func TestStagedRestore_ResumeWithEmptyQuarantineAborts(t *testing.T) {
	dir, backupPath, _ := setupRestoreFixture(t)

	if err := acquireRestoreMarker(dir); err != nil {
		t.Fatalf("acquireRestoreMarker: %v", err)
	}
	emptyQuarantine := opDir(t, dir, restoreQuarantinePrefix)

	_, err := StagedRestore(StagedRestoreOptions{DataDir: dir, BackupPath: backupPath})
	if err == nil {
		t.Fatal("expected the resume-abort to report the abandoned attempt")
	}

	// No false success: marker gone, empty quarantine gone, live untouched.
	present, err := restoreMarkerExists(dir)
	if err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("marker not removed by the resume-abort")
	}
	if _, err := os.Stat(emptyQuarantine); !os.IsNotExist(err) {
		t.Fatal("empty quarantine directory not discarded by the resume-abort")
	}
	app, err := OpenApplication(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("live database damaged by resume-abort: %v", err)
	}
	if n := sessionCount(t, app); n != 1 {
		t.Fatalf("got %d, want live session to survive", n)
	}
	app.Close()
}

// TestStagedRestore_PostActivationRuns pins the hook contract on the normal
// path: invoked exactly once, after activation (the live main file is
// already the restored one) and before marker removal (the run returns with
// no marker present). The sentinel file makes the ordering observable.
func TestStagedRestore_PostActivationRuns(t *testing.T) {
	dir, backupPath, _ := setupRestoreFixture(t)

	sentinel := filepath.Join(dir, "hook-ran")
	calls := 0
	res, err := StagedRestore(StagedRestoreOptions{
		DataDir:    dir,
		BackupPath: backupPath,
		PostActivation: func() error {
			calls++
			if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
				return fmt.Errorf("live main not present when the hook ran: %w", err)
			}
			return os.WriteFile(sentinel, nil, 0o600)
		},
	})
	if err != nil {
		t.Fatalf("StagedRestore: %v", err)
	}
	if calls != 1 {
		t.Fatalf("hook called %d times, want 1", calls)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("hook sentinel missing: %v", err)
	}
	present, err := restoreMarkerExists(dir)
	if err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("marker still present after a successful run with the hook")
	}
	if res.BackupPath != backupPath {
		t.Errorf("StagedRestore result BackupPath = %q, want %q", res.BackupPath, backupPath)
	}
}

// TestStagedRestore_PostActivationFailureKeepsMarkerThenRerunCompletes pins
// the failure semantics: a hook failure after
// activation keeps the activated database AND the marker (no rollback),
// ordinary startup fails closed, and a re-run resumes — the hook is
// idempotent, so the second run re-invokes it and completes.
func TestStagedRestore_PostActivationFailureKeepsMarkerThenRerunCompletes(t *testing.T) {
	dir, backupPath, _ := setupRestoreFixture(t)

	failOnce := true
	hook := func() error {
		if failOnce {
			failOnce = false
			return errors.New("simulated extraction failure")
		}
		return nil
	}

	_, err := StagedRestore(StagedRestoreOptions{DataDir: dir, BackupPath: backupPath, PostActivation: hook})
	if err == nil {
		t.Fatal("expected the hook failure to abort the run")
	}
	present, err := restoreMarkerExists(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !present {
		t.Fatal("marker was removed despite the hook failure")
	}

	// The activated database stays in place (the restored copy has no
	// sessions) — no rollback to the quarantine.
	db, err := Open(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("activated database not in place after the hook failure: %v", err)
	}
	if n := sessionCount(t, db); n != 0 {
		t.Fatalf("got %d sessions, want the restored session-less database", n)
	}
	db.Close()

	// Re-run: the resume path re-invokes the hook and completes.
	if _, err := StagedRestore(StagedRestoreOptions{DataDir: dir, BackupPath: backupPath, PostActivation: hook}); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	present, err = restoreMarkerExists(dir)
	if err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("marker still present after the completed re-run")
	}
}

// TestStagedRestore_PostActivationResumeAfterCompletedActivation pins the
// third hook site: a crash between a SUCCESSFUL hook and marker removal
// (marker and activation record re-created, activation already complete)
// re-invokes the hook on the resume path — extraction is idempotent, so
// running it again is safe.
func TestStagedRestore_PostActivationResumeAfterCompletedActivation(t *testing.T) {
	dir, backupPath, _ := setupRestoreFixture(t)

	calls := 0
	hook := func() error {
		calls++
		return nil
	}
	if _, err := StagedRestore(StagedRestoreOptions{DataDir: dir, BackupPath: backupPath, PostActivation: hook}); err != nil {
		t.Fatalf("first restore: %v", err)
	}

	// Simulate the crash window between hook success and marker removal: the
	// marker and the activation record of the in-flight attempt come back.
	if err := acquireRestoreMarker(dir); err != nil {
		t.Fatalf("re-create marker: %v", err)
	}
	inFlight := opDir(t, dir, restoreQuarantinePrefix)
	if err := writeActivationRecord(dir, inFlight); err != nil {
		t.Fatalf("writeActivationRecord: %v", err)
	}
	if _, err := StagedRestore(StagedRestoreOptions{DataDir: dir, BackupPath: backupPath, PostActivation: hook}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if calls != 2 {
		t.Fatalf("hook called %d times across both runs, want 2", calls)
	}
	present, err := restoreMarkerExists(dir)
	if err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("marker still present after resume")
	}
}

// TestStagedRestore_PostActivationRunsOnResumedActivation pins the fourth
// hook site: a crash between quarantine and promotion (live main missing)
// resumes the activation and still runs the hook before marker removal.
func TestStagedRestore_PostActivationRunsOnResumedActivation(t *testing.T) {
	dir, backupPath, _ := setupRestoreFixture(t)

	if err := acquireRestoreMarker(dir); err != nil {
		t.Fatalf("acquireRestoreMarker: %v", err)
	}
	stagedDir, err := stageAndPrepare(dir, backupPath, 0)
	if err != nil {
		t.Fatalf("stageAndPrepare: %v", err)
	}
	if err := finalizeStaged(stagedDir); err != nil {
		t.Fatalf("finalizeStaged: %v", err)
	}
	quarantineDir := opDir(t, dir, restoreQuarantinePrefix)
	if err := os.Rename(filepath.Join(dir, FileName), filepath.Join(quarantineDir, FileName)); err != nil {
		t.Fatalf("quarantine live main: %v", err)
	}

	calls := 0
	if _, err := StagedRestore(StagedRestoreOptions{
		DataDir:    dir,
		BackupPath: backupPath,
		PostActivation: func() error {
			calls++
			return nil
		},
	}); err != nil {
		t.Fatalf("resumed activation: %v", err)
	}
	if calls != 1 {
		t.Fatalf("hook called %d times on the resumed activation, want 1", calls)
	}
	present, err := restoreMarkerExists(dir)
	if err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("marker still present after the resumed activation")
	}
}

// TestStagedRestore_PostActivationNotRunOnPreActivationFailure pins that a
// failure BEFORE activation never invokes the hook — the media half runs
// only once the database is activated.
func TestStagedRestore_PostActivationNotRunOnPreActivationFailure(t *testing.T) {
	dir, backupPath, _ := setupRestoreFixture(t)

	// Corrupt the backup so staging fails before any mutation.
	if err := os.WriteFile(backupPath, []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatalf("corrupt backup: %v", err)
	}

	calls := 0
	_, err := StagedRestore(StagedRestoreOptions{
		DataDir:    dir,
		BackupPath: backupPath,
		PostActivation: func() error {
			calls++
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected an error for a corrupt backup")
	}
	if calls != 0 {
		t.Fatalf("hook called %d times on a pre-activation failure, want 0", calls)
	}
}

// TestStagedRestore_ReadOnlyUnitDirectory pins the read-only unit-mount
// contract: a restore must work when its unit directory is mounted read-only.
// The backup is WAL-mode, so the validation open needs sidecar files — the
// workflow therefore validates a private copy in the data directory and only
// ever READS the artifact itself (verifyBackupCopy).
func TestStagedRestore_ReadOnlyUnitDirectory(t *testing.T) {
	dir, backupPath, _ := setupRestoreFixture(t)

	unitDir := filepath.Dir(backupPath)
	if err := os.Chmod(unitDir, 0o555); err != nil {
		t.Fatalf("chmod unit dir read-only: %v", err)
	}
	defer func() {
		if err := os.Chmod(unitDir, 0o755); err != nil {
			t.Fatalf("restore unit dir perms (cleanup): %v", err)
		}
	}()

	if _, err := StagedRestore(StagedRestoreOptions{DataDir: dir, BackupPath: backupPath}); err != nil {
		t.Fatalf("StagedRestore from a read-only unit directory: %v", err)
	}
	present, err := restoreMarkerExists(dir)
	if err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("marker still present after the read-only-unit restore")
	}

	// The artifact was only ever READ, never SQL-opened: its directory must
	// hold exactly the original file — no -wal/-shm sidecars materialized.
	entries, err := os.ReadDir(unitDir)
	if err != nil {
		t.Fatalf("read unit dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(backupPath) {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("unit dir mutated by the restore: %v", names)
	}
}

// TestStagedRestore_FreshDataDir pins the disaster-recovery provisioning
// path: restoring a unit into an EMPTY data directory (a fresh volume — the
// wipe-VPS rebuild) succeeds because a missing live database has no security
// state to reconcile against. The activated database opens, sessions are
// zero, and the (empty) quarantine rollback set is retained.
func TestStagedRestore_FreshDataDir(t *testing.T) {
	_, backupPath, _ := setupRestoreFixture(t)

	// A brand-new target directory with no live database at all.
	fresh := testDataDir(t)

	res, err := StagedRestore(StagedRestoreOptions{DataDir: fresh, BackupPath: backupPath})
	if err != nil {
		t.Fatalf("StagedRestore into a fresh data dir: %v", err)
	}
	if res.QuarantineDir == "" {
		t.Fatal("expected the retained quarantine dir even on a fresh volume")
	}
	if _, err := os.Stat(res.QuarantineDir); err != nil {
		t.Fatalf("quarantine dir missing: %v", err)
	}
	present, err := restoreMarkerExists(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("marker still present after a fresh-volume restore")
	}

	app, err := OpenApplication(Config{DataDir: fresh})
	if err != nil {
		t.Fatalf("OpenApplication on the restored fresh volume: %v", err)
	}
	defer app.Close()
	if n := sessionCount(t, app); n != 0 {
		t.Fatalf("got %d, want 0 sessions on the restored fresh volume", n)
	}
	if role := userRole(t, app); role != "super-admin" {
		t.Fatalf("got role %q, want the restored user state", role)
	}
}
