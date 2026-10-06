package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// openForInspection opens an arbitrary database file by path (used for
// backup artifacts, which do not live at the fixed live path).
func openForInspection(t *testing.T, path string) (*sql.DB, error) {
	t.Helper()
	db, err := openDSN(buildDSNForFile(path))
	if err != nil {
		return nil, err
	}
	return db, nil
}

// setupBackupDB creates a migrated database with one blog post row and
// returns the data directory. It is the shared fixture for backup tests.
func setupBackupDB(t *testing.T) string {
	t.Helper()

	dir := testDataDir(t)
	db, err := OpenMaintenance(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open maintenance: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status,
		 published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('p1', 'T', '', '', 'https://example.com/t.jpg', 'published', 1000, 500, 500)`); err != nil {
		db.Close()
		t.Fatalf("insert fixture: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close fixture db: %v", err)
	}
	return dir
}

func backupPathFor(t *testing.T, dir string) string {
	t.Helper()
	path, err := BackupForMigration(dir, time.Now().Add(30*time.Second))
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	return path
}

// TestBackupForMigration_OlderPrefixHistory proves the scenario the safety
// net exists for: a live database with PENDING migrations (older-prefix
// history) still produces a valid backup. The backup is validated as a
// contiguous prefix of the embedded set, not the exact set
// (docs/patterns/go/sqlite.md). The prefix window is exactly where a pending
// migration lives — a fully migrated fixture cannot exercise it.
func TestBackupForMigration_OlderPrefixHistory(t *testing.T) {
	t.Parallel()

	dir := setupBackupDB(t)

	// Simulate a pre-upgrade database: drop the newest migration row so the
	// applied history is a strict older prefix. The newest version is read
	// dynamically — the scenario is "one migration behind", not a specific
	// version number (a new migration must not break the scenario).
	db, err := Open(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open live: %v", err)
	}
	var newest int
	if err := db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&newest); err != nil {
		db.Close()
		t.Fatalf("read newest version: %v", err)
	}
	if _, err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", newest); err != nil {
		db.Close()
		t.Fatalf("drop newest migration row: %v", err)
	}
	db.Close()

	path := backupPathFor(t, dir)

	// The artifact must open independently and carry the prefix history.
	backupDB, err := openForInspection(t, path)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer backupDB.Close()

	var versions int
	if err := backupDB.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&versions); err != nil {
		t.Fatalf("count backup history: %v", err)
	}
	if versions != newest-1 {
		t.Errorf("backup history = %d versions, want %d (the older prefix)", versions, newest-1)
	}

	// And the rollback path accepts it (full integrity_check included).
	if err := verifyBackupForRestore(path); err != nil {
		t.Errorf("verify backup for restore: %v", err)
	}
}

// TestBackupForMigration_PublishesValidatedArtifact proves the happy path:
// the backup lands under backups/, with 0600 permissions, no temp files
// left behind, and it passes the independent validation (the function's own
// guarantees: exact history, quick_check ok, zero FK violations).
func TestBackupForMigration_PublishesValidatedArtifact(t *testing.T) {
	t.Parallel()

	dir := setupBackupDB(t)
	path := backupPathFor(t, dir)

	if filepath.Dir(path) != filepath.Join(dir, backupsDirName) {
		t.Errorf("backup path %q, want under %s", path, filepath.Join(dir, backupsDirName))
	}
	if !strings.HasPrefix(filepath.Base(path), "pre-migrate-") || !strings.HasSuffix(path, ".db") {
		t.Errorf("unexpected backup filename %q", filepath.Base(path))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat backup: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("backup permissions = %o, want 600", perm)
	}

	// No temp or sidecar files may remain in the backup directory.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("read backups dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

// TestBackupForMigration_SnapshotIsIndependent proves the snapshot does not
// see later live changes — the reason it is a valid rollback artifact.
func TestBackupForMigration_SnapshotIsIndependent(t *testing.T) {
	t.Parallel()

	dir := setupBackupDB(t)
	path := backupPathFor(t, dir)

	// Mutate the live database after the backup was taken.
	db, err := OpenMaintenance(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("reopen live: %v", err)
	}
	if _, err := db.Exec("UPDATE blog_posts SET title = 'Changed' WHERE id = 'p1'"); err != nil {
		db.Close()
		t.Fatalf("update live: %v", err)
	}
	db.Close()

	backupDB, err := openForInspection(t, path)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer backupDB.Close()

	var title string
	if err := backupDB.QueryRow("SELECT title FROM blog_posts WHERE id = 'p1'").Scan(&title); err != nil {
		t.Fatalf("read backup row: %v", err)
	}
	if title != "T" {
		t.Errorf("backup title = %q, want original %q (snapshot must be independent)", title, "T")
	}
}

// TestBackupForMigration_DeadlineFailsCleanly proves a deadline in the past
// fails the copy phase and leaves no published or temp artifacts behind.
func TestBackupForMigration_DeadlineFailsCleanly(t *testing.T) {
	t.Parallel()

	dir := setupBackupDB(t)

	_, err := BackupForMigration(dir, time.Now().Add(-time.Second))
	if err == nil {
		t.Fatal("backup with past deadline: got nil, want error")
	}

	entries, err := os.ReadDir(filepath.Join(dir, backupsDirName))
	if err != nil {
		t.Fatalf("read backups dir: %v", err)
	}
	for _, e := range entries {
		t.Errorf("artifact left behind after failed backup: %s", e.Name())
	}
}

// TestVerifyBackupArtifact_RejectsCorruptFile proves validation fails closed:
// a file without valid migration history is never acceptable.
func TestVerifyBackupArtifact_RejectsCorruptFile(t *testing.T) {
	t.Parallel()

	dir := testDataDir(t)
	path := filepath.Join(dir, "corrupt.db")
	if err := os.WriteFile(path, []byte("this is not a sqlite database"), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	if err := verifyBackupArtifact(path); err == nil {
		t.Fatal("verifyBackupArtifact accepted a corrupt file")
	}
}

// TestRestoreOfflineBackup_ReplacesBrokenLiveFiles proves the migration
// rollback path: a half-migrated live database (plus leftover WAL sidecars)
// is replaced by the validated backup, and the strict application opener
// accepts the restored file. The quarantine/activation mechanics must leave
// no temp or quarantined files behind on success.
func TestRestoreOfflineBackup_ReplacesBrokenLiveFiles(t *testing.T) {
	t.Parallel()

	dir := setupBackupDB(t)
	path := backupPathFor(t, dir)

	// Simulate a failed migration: corrupt the live main file and drop
	// fake sidecar files next to it.
	live := filepath.Join(dir, FileName)
	if err := os.WriteFile(live, []byte("broken"), 0o600); err != nil {
		t.Fatalf("corrupt live db: %v", err)
	}
	for _, sidecar := range []string{FileName + "-wal", FileName + "-shm"} {
		if err := os.WriteFile(filepath.Join(dir, sidecar), []byte("junk"), 0o600); err != nil {
			t.Fatalf("write %s: %v", sidecar, err)
		}
	}

	if err := RestoreOfflineBackup(dir, path); err != nil {
		t.Fatalf("restore: %v", err)
	}

	// Sidecars must be gone and the restored file must be 0600.
	for _, sidecar := range []string{FileName + "-wal", FileName + "-shm"} {
		if _, err := os.Stat(filepath.Join(dir, sidecar)); !os.IsNotExist(err) {
			t.Errorf("%s still exists after restore", sidecar)
		}
	}
	info, err := os.Stat(live)
	if err != nil {
		t.Fatalf("stat restored db: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("restored permissions = %o, want 600", perm)
	}

	// No temp or quarantined leftovers after a successful restore.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read data dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".tmp") || strings.Contains(name, ".broken-") {
			t.Errorf("leftover after restore: %s", name)
		}
	}

	// The strict application opener (exact history check) accepts the
	// restored database and the original data is back.
	db, err := OpenApplication(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("strict open after restore: %v", err)
	}
	defer db.Close()

	var title string
	if err := db.QueryRow("SELECT title FROM blog_posts WHERE id = 'p1'").Scan(&title); err != nil {
		t.Fatalf("read restored row: %v", err)
	}
	if title != "T" {
		t.Errorf("restored title = %q, want %q", title, "T")
	}
}

// TestRestoreOfflineBackup_RejectsInvalidBackup proves the restore refuses
// an unverified artifact before touching the live files: the live database
// must remain exactly as it was.
func TestRestoreOfflineBackup_RejectsInvalidBackup(t *testing.T) {
	t.Parallel()

	dir := setupBackupDB(t)

	badPath := filepath.Join(dir, "bad.db")
	if err := os.WriteFile(badPath, []byte("not a database"), 0o600); err != nil {
		t.Fatalf("write invalid backup: %v", err)
	}

	liveBefore, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("read live before: %v", err)
	}

	if err := RestoreOfflineBackup(dir, badPath); err == nil {
		t.Fatal("restore with invalid backup: got nil, want error")
	}

	// The live database is untouched — no quarantine, no temp, no change.
	liveAfter, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("read live after: %v", err)
	}
	if string(liveBefore) != string(liveAfter) {
		t.Error("live database changed by a rejected restore")
	}
	db, err := OpenApplication(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("strict open after rejected restore: %v", err)
	}
	db.Close()
}

// TestMigrationStatus proves the applied/embedded counts report correctly
// across a fresh directory and a fully migrated database.
func TestMigrationStatus(t *testing.T) {
	t.Parallel()

	dir := testDataDir(t)
	status, err := MigrationStatus(dir)
	if err != nil {
		t.Fatalf("status on fresh dir: %v", err)
	}
	if status.Applied != 0 {
		t.Errorf("fresh dir applied = %d, want 0", status.Applied)
	}
	// The embedded count is read from the embedded set itself: this test
	// pins the STATUS behavior (applied < embedded on a fresh dir), not a
	// snapshot of how many migrations exist today.
	embedded, err := loadedMigrations()
	if err != nil {
		t.Fatalf("loadedMigrations: %v", err)
	}
	if status.Embedded != len(embedded) {
		t.Errorf("embedded = %d, want %d", status.Embedded, len(embedded))
	}

	db, err := OpenMaintenance(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Close()

	status, err = MigrationStatus(dir)
	if err != nil {
		t.Fatalf("status after migration: %v", err)
	}
	if status.Applied != status.Embedded {
		t.Errorf("applied = %d, embedded = %d, want equal", status.Applied, status.Embedded)
	}
}

// TestBackupForDaily_ExactHistory pins the difference between the two backup
// variants: the daily backup is created and served by the same
// artifact, so it must carry the EXACT embedded history — a database one
// migration behind produces a valid pre-migration backup but a REJECTED
// daily backup.
func TestBackupForDaily_ExactHistory(t *testing.T) {
	t.Parallel()

	dir := setupBackupDB(t)

	// Happy path first: a current database backs up cleanly.
	if _, err := BackupForDaily(dir, testDataDir(t), "daily.db", time.Now().Add(30*time.Second)); err != nil {
		t.Fatalf("BackupForDaily on current db: %v", err)
	}

	// Simulate one-migration-behind (the same scenario as
	// TestBackupForMigration_OlderPrefixHistory).
	db, err := Open(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open live: %v", err)
	}
	var newest int
	if err := db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&newest); err != nil {
		db.Close()
		t.Fatalf("read newest version: %v", err)
	}
	if _, err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", newest); err != nil {
		db.Close()
		t.Fatalf("drop newest migration row: %v", err)
	}
	db.Close()

	if _, err := BackupForDaily(dir, testDataDir(t), "daily.db", time.Now().Add(30*time.Second)); err == nil {
		t.Error("BackupForDaily accepted an older-prefix database — exact history must reject it")
	}
	// The pre-migration variant still accepts the same database (the
	// migration safety net exists precisely for this state).
	if _, err := BackupForMigration(dir, time.Now().Add(30*time.Second)); err != nil {
		t.Errorf("BackupForMigration on older-prefix db: %v", err)
	}
}

// TestBackupForPreMigration_OlderPrefixHistory pins the deploy-trigger
// history mode: the deploy backup runs
// BEFORE the deploy's migration, so when the new tag ships a new migration
// the live database is an older prefix of the NEW backup image's embedded
// set. BackupForPreMigration must accept that state (contiguous-prefix
// validation, caller-chosen unit destination) while BackupForDaily rejects
// it (pinned above) — and the published artifact must pass the
// restore-grade verification, because it is the deploy's rollback point.
func TestBackupForPreMigration_OlderPrefixHistory(t *testing.T) {
	t.Parallel()

	dir := setupBackupDB(t)

	// Happy path first: a current database backs up cleanly (applied ==
	// embedded is a valid prefix).
	if _, err := BackupForPreMigration(dir, testDataDir(t), "sick-fansubs.db", time.Now().Add(30*time.Second)); err != nil {
		t.Fatalf("BackupForPreMigration on current db: %v", err)
	}

	// Simulate one-migration-behind.
	db, err := Open(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open live: %v", err)
	}
	var newest int
	if err := db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&newest); err != nil {
		db.Close()
		t.Fatalf("read newest version: %v", err)
	}
	if _, err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", newest); err != nil {
		db.Close()
		t.Fatalf("drop newest migration row: %v", err)
	}
	db.Close()

	dest := testDataDir(t)
	path, err := BackupForPreMigration(dir, dest, "sick-fansubs.db", time.Now().Add(30*time.Second))
	if err != nil {
		t.Fatalf("BackupForPreMigration on older-prefix db: %v", err)
	}
	if path != filepath.Join(dest, "sick-fansubs.db") {
		t.Errorf("artifact path = %q, want %q", path, filepath.Join(dest, "sick-fansubs.db"))
	}

	// The artifact carries the older prefix and passes the restore-grade
	// verification — it is the deploy's rollback point.
	backupDB, err := openForInspection(t, path)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer backupDB.Close()

	var versions int
	if err := backupDB.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&versions); err != nil {
		t.Fatalf("count backup history: %v", err)
	}
	if versions != newest-1 {
		t.Errorf("backup history = %d versions, want %d (the older prefix)", versions, newest-1)
	}
	if err := verifyBackupForRestore(path); err != nil {
		t.Errorf("verify backup for restore: %v", err)
	}
}

// TestBackupForPreMigration_RejectsDrift pins the other half of the
// deploy-trigger contract: the prefix check
// relaxes ONLY "applied == embedded" — history drift still fails closed on
// the deploy path. A tampered newest migration row must reject the backup.
func TestBackupForPreMigration_RejectsDrift(t *testing.T) {
	t.Parallel()

	dir := setupBackupDB(t)

	db, err := Open(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open live: %v", err)
	}
	if _, err := db.Exec("UPDATE schema_migrations SET checksum = ? WHERE version = (SELECT MAX(version) FROM schema_migrations)",
		[]byte("00000000000000000000000000000000")); err != nil {
		db.Close()
		t.Fatalf("tamper newest migration row: %v", err)
	}
	db.Close()

	if _, err := BackupForPreMigration(dir, testDataDir(t), "sick-fansubs.db", time.Now().Add(30*time.Second)); err == nil {
		t.Error("BackupForPreMigration accepted a drifted history — prefix validation must reject it")
	}
}

// TestBackupForDaily_PublishesToDest pins the publication contract: the
// artifact lands in the caller's directory under the caller's name, 0600,
// with no temp leftovers, and passes the restore-grade verification.
func TestBackupForDaily_PublishesToDest(t *testing.T) {
	t.Parallel()

	dir := setupBackupDB(t)
	dest := testDataDir(t)
	path, err := BackupForDaily(dir, dest, "sick-fansubs.db", time.Now().Add(30*time.Second))
	if err != nil {
		t.Fatalf("BackupForDaily: %v", err)
	}
	if path != filepath.Join(dest, "sick-fansubs.db") {
		t.Errorf("path = %q, want under dest with the given name", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat artifact: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("artifact mode = %o, want 0600", perm)
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("dest has %d entries, want exactly the published artifact (no temp leftovers)", len(entries))
	}
	if err := verifyBackupForRestore(path); err != nil {
		t.Errorf("restore-grade verification of the daily artifact: %v", err)
	}
}

// TestCreateBackup_MarkerGate proves both backup variants fail closed while
// a restore marker exists — the backup runs inside the single-owner unready
// window, and a marker means that window is broken (docs/patterns/go/sqlite.md).
func TestCreateBackup_MarkerGate(t *testing.T) {
	t.Parallel()

	dir := setupBackupDB(t)
	if err := acquireRestoreMarker(dir); err != nil {
		t.Fatalf("acquire marker: %v", err)
	}

	if _, err := BackupForDaily(dir, testDataDir(t), "daily.db", time.Now().Add(30*time.Second)); err == nil {
		t.Error("BackupForDaily proceeded with a restore marker present")
	}
	if _, err := BackupForMigration(dir, time.Now().Add(30*time.Second)); err == nil {
		t.Error("BackupForMigration proceeded with a restore marker present")
	}

	if err := removeRestoreMarker(dir); err != nil {
		t.Fatalf("remove marker: %v", err)
	}
	if _, err := BackupForDaily(dir, testDataDir(t), "daily.db", time.Now().Add(30*time.Second)); err != nil {
		t.Errorf("BackupForDaily after marker removal: %v", err)
	}
}
