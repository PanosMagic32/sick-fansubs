package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"filippo.io/age"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/media"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// permOf returns the permission bits of the file or directory at path.
func permOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}

// seedBackupSource migrates an empty database and drops one media file into
// the media layout — a minimal real source for the unit pipeline.
func seedBackupSource(t *testing.T, dataDir string) {
	t.Helper()
	db, err := database.OpenMaintenance(database.Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("migrate fixture db: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close fixture db: %v", err)
	}
	p := filepath.Join(dataDir, "media", "images", "ab", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir media: %v", err)
	}
	if err := os.WriteFile(p, []byte("fake-image-bytes"), 0o644); err != nil {
		t.Fatalf("write media file: %v", err)
	}
}

func TestResolveConfig_ProductionRequiresRecipient(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	if _, err := resolveConfig("./data", "./backups", ""); err == nil {
		t.Error("production without AGE_RECIPIENT: expected fail-closed error")
	}
	if _, err := resolveConfig("./data", "./backups", "not-a-recipient"); err == nil {
		t.Error("malformed AGE_RECIPIENT: expected validation error")
	}

	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	cfg, err := resolveConfig("./data", "./backups", id.Recipient().String())
	if err != nil {
		t.Fatalf("production with a valid recipient: %v", err)
	}
	if cfg.AgeRecipient == nil {
		t.Error("valid recipient was not retained in the resolved config")
	}
}

func TestResolveConfig_RejectsUnknownAppEnv(t *testing.T) {
	t.Setenv("APP_ENV", "staging")
	if _, err := resolveConfig("./data", "./backups", ""); err == nil {
		t.Error("unknown APP_ENV: expected validation error")
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	t.Parallel()

	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	dir := testDataDir(t)
	plain := "sensitive backup bytes"
	if err := os.WriteFile(filepath.Join(dir, "artifact.db"), []byte(plain), 0o600); err != nil {
		t.Fatalf("write plaintext: %v", err)
	}
	if err := encryptFile(dir, "artifact.db", id.Recipient()); err != nil {
		t.Fatalf("encryptFile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "artifact.db")); !os.IsNotExist(err) {
		t.Error("plaintext artifact still present after encryption")
	}

	cipher, err := os.ReadFile(filepath.Join(dir, "artifact.db.age"))
	if err != nil {
		t.Fatalf("read ciphertext: %v", err)
	}
	r, err := age.Decrypt(bytes.NewReader(cipher), id)
	if err != nil {
		t.Fatalf("age.Decrypt: %v", err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read decrypted: %v", err)
	}
	if string(got) != plain {
		t.Errorf("round-trip = %q, want %q", got, plain)
	}
}

func TestBackupFolders_PruneKeepsNewestTen(t *testing.T) {
	t.Parallel()

	backupDir := testDataDir(t)
	for d := 20; d <= 30; d++ {
		folder := filepath.Join(backupDir, fmt.Sprintf("2026-08-%02d", d))
		if err := os.Mkdir(folder, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", folder, err)
		}
	}
	// Foreign entries: never listed, never pruned.
	if err := os.Mkdir(filepath.Join(backupDir, "not-a-date"), 0o700); err != nil {
		t.Fatalf("mkdir foreign dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "loose-file"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write loose file: %v", err)
	}

	folders, err := backupFolders(backupDir)
	if err != nil {
		t.Fatalf("backupFolders: %v", err)
	}
	if len(folders) != 11 {
		t.Fatalf("backupFolders = %d, want 11", len(folders))
	}
	if filepath.Base(folders[0]) != "2026-08-30" {
		t.Errorf("newest = %s, want 2026-08-30", folders[0])
	}

	pruned, err := pruneOld(testLogger(), backupDir, 10)
	if err != nil {
		t.Fatalf("pruneOld: %v", err)
	}
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}
	if _, err := os.Stat(filepath.Join(backupDir, "2026-08-20")); !os.IsNotExist(err) {
		t.Error("oldest folder still present after prune")
	}
	if _, err := os.Stat(filepath.Join(backupDir, "not-a-date")); err != nil {
		t.Error("foreign dir was touched by pruning")
	}
	if _, err := os.Stat(filepath.Join(backupDir, "loose-file")); err != nil {
		t.Error("loose file was touched by pruning")
	}
}

func TestRunStatus(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	missing := filepath.Join(testDataDir(t), "missing")
	if err := runStatus(&buf, missing); err != nil {
		t.Fatalf("runStatus on missing dir: %v", err)
	}
	if !strings.Contains(buf.String(), "does not exist") {
		t.Errorf("status = %q, want the missing-directory message", buf.String())
	}

	backupDir := testDataDir(t)
	if err := os.Mkdir(filepath.Join(backupDir, "2026-08-31"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	buf.Reset()
	if err := runStatus(&buf, backupDir); err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	if !strings.Contains(buf.String(), "2026-08-31") || !strings.Contains(buf.String(), "ago") {
		t.Errorf("status = %q, want the newest folder and its age", buf.String())
	}

	// An existing directory with no units is the fresh-install shape.
	empty := testDataDir(t)
	buf.Reset()
	if err := runStatus(&buf, empty); err != nil {
		t.Fatalf("runStatus on empty dir: %v", err)
	}
	if !strings.Contains(buf.String(), "no published backups") {
		t.Errorf("status = %q, want the empty message", buf.String())
	}
}

// athensMorning is a fixed "now" whose Athens-local date is 2026-08-31.
var athensMorning = time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)

func TestRunBackup_EndToEndPlaintext(t *testing.T) {
	t.Parallel()

	dataDir := testDataDir(t)
	seedBackupSource(t, dataDir)
	backupDir := testDataDir(t)
	cfg := config{DataDir: dataDir, BackupDir: backupDir}

	var buf bytes.Buffer
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	if err := runBackup(logger, &buf, cfg, false, false, athensMorning); err != nil {
		t.Fatalf("runBackup: %v", err)
	}
	if !strings.Contains(buf.String(), "plaintext") {
		t.Errorf("summary = %q, want the plaintext marker", buf.String())
	}
	records := decodeLogRecords(t, logBuf.String())
	warned := false
	for _, rec := range records {
		if rec["msg"] == "publishing PLAINTEXT unit — AGE_RECIPIENT unset (development only; production fails closed)" {
			warned = true
		}
	}
	if !warned {
		t.Errorf("records = %v, want the plaintext warning", records)
	}

	folder := filepath.Join(backupDir, "2026-08-31")
	entries, err := os.ReadDir(folder)
	if err != nil {
		t.Fatalf("read unit folder: %v", err)
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	for _, want := range []string{"sick-fansubs.db", "sick-fansubs.media.tar", "sick-fansubs.db.media-manifest.json"} {
		if !names[want] {
			t.Errorf("unit folder missing %s (has %v)", want, names)
		}
	}

	// Access control: a PLAINTEXT unit stays
	// owner-only — folder 0700, artifacts 0600 — because its halves hold
	// user data.
	if mode := permOf(t, folder); mode != 0o700 {
		t.Errorf("plaintext folder mode = %#o, want 0700", mode)
	}
	for _, name := range []string{"sick-fansubs.db", "sick-fansubs.media.tar", "sick-fansubs.db.media-manifest.json"} {
		if mode := permOf(t, filepath.Join(folder, name)); mode != 0o600 {
			t.Errorf("plaintext %s mode = %#o, want 0600", name, mode)
		}
	}

	// The database artifact is a real SQLite file.
	dbBytes, err := os.ReadFile(filepath.Join(folder, "sick-fansubs.db"))
	if err != nil {
		t.Fatalf("read db artifact: %v", err)
	}
	if !strings.HasPrefix(string(dbBytes), "SQLite format 3\x00") {
		t.Error("database artifact lacks the SQLite magic header")
	}

	// The manifest binds the tar by checksum and passes verification.
	m, err := media.ReadMediaManifest(media.ManifestPathFor(filepath.Join(folder, "sick-fansubs.db")))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if err := media.VerifyMediaTar(folder, m); err != nil {
		t.Errorf("VerifyMediaTar on the published unit: %v", err)
	}

	// A second run the same day refuses to clobber the published unit.
	if err := runBackup(testLogger(), &buf, cfg, false, false, athensMorning); err == nil {
		t.Error("second same-day run: expected refusal")
	}
}

func TestRunBackup_EndToEndEncrypted(t *testing.T) {
	t.Parallel()

	dataDir := testDataDir(t)
	seedBackupSource(t, dataDir)
	backupDir := testDataDir(t)

	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	cfg := config{DataDir: dataDir, BackupDir: backupDir, AgeRecipient: id.Recipient()}

	if err := runBackup(testLogger(), io.Discard, cfg, false, false, athensMorning); err != nil {
		t.Fatalf("runBackup: %v", err)
	}

	// A complete unit was published: the database artifact, the media tar,
	// and the manifest — the deploy's rollback point.
	folder := filepath.Join(backupDir, "2026-08-31")
	entries, err := os.ReadDir(folder)
	if err != nil {
		t.Fatalf("read unit folder: %v", err)
	}
	decrypt := func(t *testing.T, path string) []byte {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		r, err := age.Decrypt(bytes.NewReader(raw), id)
		if err != nil {
			t.Fatalf("decrypt %s: %v", path, err)
		}
		out, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("read decrypted %s: %v", path, err)
		}
		return out
	}

	// Ciphertext only: every file ends in .age, nothing else exists.
	var dbOut, tarOut, manifestOut []byte
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".age") {
			t.Errorf("folder contains a non-ciphertext file: %s", e.Name())
		}
		// Access control: the ciphertext is
		// world-readable — age encryption is the boundary, and the host-side
		// push script reads it as the deploy account.
		if mode := permOf(t, filepath.Join(folder, e.Name())); mode != 0o644 {
			t.Errorf("ciphertext %s mode = %#o, want 0644", e.Name(), mode)
		}
		switch e.Name() {
		case "sick-fansubs.db.age":
			dbOut = decrypt(t, filepath.Join(folder, e.Name()))
		case "sick-fansubs.media.tar.age":
			tarOut = decrypt(t, filepath.Join(folder, e.Name()))
		case "sick-fansubs.db.media-manifest.json.age":
			manifestOut = decrypt(t, filepath.Join(folder, e.Name()))
		}
	}
	if mode := permOf(t, folder); mode != 0o755 {
		t.Errorf("encrypted folder mode = %#o, want 0755", mode)
	}
	if !strings.HasPrefix(string(dbOut), "SQLite format 3\x00") {
		t.Error("decrypted database artifact lacks the SQLite magic header")
	}
	var m media.MediaManifest
	if err := json.Unmarshal(manifestOut, &m); err != nil {
		t.Fatalf("decrypted manifest is not JSON: %v", err)
	}
	if m.MediaTarFileName != "sick-fansubs.media.tar" {
		t.Errorf("manifest tar name = %q", m.MediaTarFileName)
	}
	if len(tarOut) == 0 {
		t.Error("decrypted media tarball is empty")
	}
}

// TestRunBackup_StaleStagingNeverLeaksPlaintext pins the crash-leftover
// contract: a staging folder left by a killed run (plaintext temp artifacts,
// no ciphertext yet) must be cleared at entry — reusing it would publish the
// crashed run's plaintext into the world-traversable unit.
func TestRunBackup_StaleStagingNeverLeaksPlaintext(t *testing.T) {
	t.Parallel()

	dataDir := testDataDir(t)
	seedBackupSource(t, dataDir)
	backupDir := testDataDir(t)

	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	cfg := config{DataDir: dataDir, BackupDir: backupDir, AgeRecipient: id.Recipient()}

	// The leftover shapes: today's stage with a plaintext database temp and no
	// .age anywhere (a kill during the page copy), plus a cross-date stage a
	// previous day's crash left behind.
	stage := filepath.Join(backupDir, "2026-08-31"+stagingSuffix)
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatalf("mkdir stale stage: %v", err)
	}
	debris := filepath.Join(stage, ".tmp-deadbeef.db")
	if err := os.WriteFile(debris, []byte("plaintext user data"), 0o600); err != nil {
		t.Fatalf("write debris: %v", err)
	}
	oldStage := filepath.Join(backupDir, "2026-08-30"+stagingSuffix)
	if err := os.Mkdir(oldStage, 0o700); err != nil {
		t.Fatalf("mkdir old stale stage: %v", err)
	}

	if err := runBackup(testLogger(), io.Discard, cfg, false, false, athensMorning); err != nil {
		t.Fatalf("runBackup over a stale stage: %v", err)
	}

	folder := filepath.Join(backupDir, "2026-08-31")
	entries, err := os.ReadDir(folder)
	if err != nil {
		t.Fatalf("read unit folder: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("published unit holds %d entries, want exactly 3", len(entries))
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".age") {
			t.Errorf("published unit holds a non-ciphertext entry %q", e.Name())
		}
	}
	if _, err := os.Stat(debris); !os.IsNotExist(err) {
		t.Error("crash-leftover plaintext debris survived into the publication")
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Error("stale staging folder survived the run")
	}
	if _, err := os.Stat(oldStage); !os.IsNotExist(err) {
		t.Error("cross-date stale staging folder survived the run")
	}
}

func TestRunStatus_IgnoresBackupEnvironment(t *testing.T) {
	t.Setenv("APP_ENV", "production")

	backupDir := testDataDir(t)
	if err := os.Mkdir(filepath.Join(backupDir, "2026-08-31"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var buf bytes.Buffer
	// No AGE_RECIPIENT and no DATA_DIR: the diagnostic resolves BACKUP_DIR
	// only, so it works while a missing age key is what is being diagnosed.
	if err := run(testLogger(), &buf, true, "", backupDir, "", false, false, athensMorning); err != nil {
		t.Fatalf("status without AGE_RECIPIENT: %v", err)
	}
	if !strings.Contains(buf.String(), "2026-08-31") {
		t.Errorf("status = %q, want the newest folder", buf.String())
	}
}

// envOr is the flag-default bridge: an explicit environment value wins, and
// the fallback applies only when the variable is unset or empty. The flag
// parsing itself is main()'s thin shell, outside this pin.
func TestEnvOr(t *testing.T) {
	t.Setenv("BACKUP_TEST_ENVOR", "from-env")
	if got := envOr("BACKUP_TEST_ENVOR", "fallback"); got != "from-env" {
		t.Errorf("envOr with env set = %q, want %q", got, "from-env")
	}
	t.Setenv("BACKUP_TEST_ENVOR", "")
	if got := envOr("BACKUP_TEST_ENVOR", "fallback"); got != "fallback" {
		t.Errorf("envOr with env empty = %q, want %q", got, "fallback")
	}
}

// TestRunBackup_ReplaceToday_ReplacesExistingUnit pins the deploy-trigger
// policy: with replaceToday, a second same-day run
// removes the existing date-named folder (sentinel-pinned) and publishes a
// complete new unit; without it the fail-closed refusal stands (pinned above
// in the plaintext test).
func TestRunBackup_ReplaceToday_ReplacesExistingUnit(t *testing.T) {
	t.Parallel()

	dataDir := testDataDir(t)
	seedBackupSource(t, dataDir)
	backupDir := testDataDir(t)
	cfg := config{DataDir: dataDir, BackupDir: backupDir}

	if err := runBackup(testLogger(), io.Discard, cfg, false, false, athensMorning); err != nil {
		t.Fatalf("first runBackup: %v", err)
	}

	folder := filepath.Join(backupDir, "2026-08-31")

	// A sentinel proves the replacement DELETED the old folder (including
	// foreign files), not merely layered new artifacts over it.
	sentinel := filepath.Join(folder, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("old unit"), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	// The deploy trigger replaces the unit: the run succeeds and the folder
	// holds the three fresh artifacts of a complete unit.
	if err := runBackup(testLogger(), io.Discard, cfg, true, false, athensMorning); err != nil {
		t.Fatalf("runBackup with replaceToday: %v", err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Error("sentinel survived the replace — the old unit folder was not removed")
	}
	if _, err := os.Stat(folder + previousSuffix); !os.IsNotExist(err) {
		t.Error("the moved-aside previous unit was not cleaned up after the swap")
	}

	entries, err := os.ReadDir(folder)
	if err != nil {
		t.Fatalf("read replaced unit folder: %v", err)
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	for _, want := range []string{"sick-fansubs.db", "sick-fansubs.media.tar", "sick-fansubs.db.media-manifest.json"} {
		if !names[want] {
			t.Errorf("replaced unit folder missing %s (has %v)", want, names)
		}
	}
}

// TestRunBackup_ReplaceToday_FailureKeepsPreviousUnit pins the staged-swap
// guarantee: when the NEW unit fails to build, the existing unit survives
// untouched — the replace must never destroy the rollback point it exists
// to protect.
func TestRunBackup_ReplaceToday_FailureKeepsPreviousUnit(t *testing.T) {
	t.Parallel()

	dataDir := testDataDir(t)
	seedBackupSource(t, dataDir)
	backupDir := testDataDir(t)
	cfg := config{DataDir: dataDir, BackupDir: backupDir}

	if err := runBackup(testLogger(), io.Discard, cfg, false, false, athensMorning); err != nil {
		t.Fatalf("first runBackup: %v", err)
	}

	folder := filepath.Join(backupDir, "2026-08-31")
	marker := filepath.Join(folder, "marker.txt")
	if err := os.WriteFile(marker, []byte("the existing unit"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	// Simulate schema drift so the fresh backup's validation rejects the
	// source (the same technique as ValidationFailurePublishesNothing).
	db, err := database.Open(database.Config{DataDir: dataDir})
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

	if err := runBackup(testLogger(), io.Discard, cfg, true, false, athensMorning); err == nil {
		t.Fatal("replace with a drifted database: expected non-zero error")
	}

	// The previous unit survived the failed replace, marker and all.
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("previous unit's marker lost after a failed replace: %v", err)
	}
	if _, err := os.Stat(folder + stagingSuffix); !os.IsNotExist(err) {
		t.Error("staging folder left behind after a failed replace")
	}
}

// TestRunBackup_ModesAreUmaskIndependent pins the contract: the publish modes
// are set with EXPLICIT chmods, so a restrictive umask cannot silently narrow
// them back (which would break the host-side push the 0755/0644 contract
// exists for). Not parallel — umask is process-global.
func TestRunBackup_ModesAreUmaskIndependent(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)

	t.Run("encrypted", func(t *testing.T) {
		dataDir := testDataDir(t)
		seedBackupSource(t, dataDir)
		backupDir := testDataDir(t)
		id, err := age.GenerateX25519Identity()
		if err != nil {
			t.Fatalf("generate identity: %v", err)
		}
		cfg := config{DataDir: dataDir, BackupDir: backupDir, AgeRecipient: id.Recipient()}
		if err := runBackup(testLogger(), io.Discard, cfg, false, false, athensMorning); err != nil {
			t.Fatalf("runBackup: %v", err)
		}
		folder := filepath.Join(backupDir, "2026-08-31")
		if mode := permOf(t, folder); mode != 0o755 {
			t.Errorf("encrypted folder mode under umask 077 = %#o, want 0755", mode)
		}
		for _, name := range []string{"sick-fansubs.db.age", "sick-fansubs.media.tar.age", "sick-fansubs.db.media-manifest.json.age"} {
			if mode := permOf(t, filepath.Join(folder, name)); mode != 0o644 {
				t.Errorf("%s mode under umask 077 = %#o, want 0644", name, mode)
			}
		}
	})

	t.Run("plaintext", func(t *testing.T) {
		dataDir := testDataDir(t)
		seedBackupSource(t, dataDir)
		backupDir := testDataDir(t)
		cfg := config{DataDir: dataDir, BackupDir: backupDir}
		if err := runBackup(testLogger(), io.Discard, cfg, false, false, athensMorning); err != nil {
			t.Fatalf("runBackup: %v", err)
		}
		folder := filepath.Join(backupDir, "2026-08-31")
		if mode := permOf(t, folder); mode != 0o700 {
			t.Errorf("plaintext folder mode under umask 077 = %#o, want 0700", mode)
		}
		if mode := permOf(t, filepath.Join(folder, "sick-fansubs.db")); mode != 0o600 {
			t.Errorf("plaintext artifact mode under umask 077 = %#o, want 0600", mode)
		}
	})
}

// TestRunBackup_ValidationFailurePublishesNothing pins the contract: a
// database that fails validation exits non-zero, publishes nothing, and
// removes the empty unit folder (the tar/manifest/encryption steps never run).
func TestRunBackup_ValidationFailurePublishesNothing(t *testing.T) {
	t.Parallel()

	dataDir := testDataDir(t)
	seedBackupSource(t, dataDir)

	// Simulate schema drift: drop the newest migration row so the daily
	// backup's exact-history validation rejects the source.
	db, err := database.Open(database.Config{DataDir: dataDir})
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

	backupDir := testDataDir(t)
	cfg := config{DataDir: dataDir, BackupDir: backupDir}

	if err := runBackup(testLogger(), io.Discard, cfg, false, false, athensMorning); err == nil {
		t.Fatal("runBackup with a drifted database: expected non-zero error")
	}

	// Nothing published: no date-stamped folders at all, and the unit
	// folder created for staging was cleaned up (no partial artifacts).
	folders, err := backupFolders(backupDir)
	if err != nil {
		t.Fatalf("backupFolders: %v", err)
	}
	if len(folders) != 0 {
		t.Errorf("published folders = %v, want none", folders)
	}
	if _, err := os.Stat(filepath.Join(backupDir, "2026-08-31")); !os.IsNotExist(err) {
		t.Error("unit folder left behind after a failed validation")
	}
}

// TestRunBackup_PreMigrate_AllowsOlderPrefix pins the deploy-trigger
// history mode: with preMigrate set, a live
// database one migration behind this artifact's embedded set still produces
// a complete published unit — the deploy backup runs BEFORE the deploy's
// migration, so the older-prefix state is expected when the new tag ships a
// new migration. Without the flag the same database fails closed (pinned by
// TestRunBackup_ValidationFailurePublishesNothing).
func TestRunBackup_PreMigrate_AllowsOlderPrefix(t *testing.T) {
	t.Parallel()

	dataDir := testDataDir(t)
	seedBackupSource(t, dataDir)

	// Simulate a live database one migration behind
	// the embedded set (the new tag ships the migration; it is not applied
	// yet — the deploy's migrate step runs AFTER the backup).
	db, err := database.Open(database.Config{DataDir: dataDir})
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

	backupDir := testDataDir(t)
	cfg := config{DataDir: dataDir, BackupDir: backupDir}

	if err := runBackup(testLogger(), io.Discard, cfg, false, true, athensMorning); err != nil {
		t.Fatalf("runBackup with preMigrate on an older-prefix db: %v", err)
	}

	// A complete unit was published: the database artifact, the media tar,
	// and the manifest — the deploy's rollback point.
	folder := filepath.Join(backupDir, "2026-08-31")
	entries, err := os.ReadDir(folder)
	if err != nil {
		t.Fatalf("read published unit: %v", err)
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	for _, want := range []string{"sick-fansubs.db", "sick-fansubs.media.tar", "sick-fansubs.db.media-manifest.json"} {
		if !names[want] {
			t.Errorf("published unit missing %s (has %v)", want, names)
		}
	}
}

// TestNumericContractsPinned pins the retention count and the database
// deadline, so a silent change is caught here (the retention count is also
// named by docs/ops/backup.md and the feature doc).
func TestNumericContractsPinned(t *testing.T) {
	t.Parallel()

	if keepBackups != 10 {
		t.Errorf("keepBackups = %d, want the documented 10", keepBackups)
	}
	if backupDeadline != 10*time.Minute {
		t.Errorf("backupDeadline = %s, want 10m", backupDeadline)
	}
}
