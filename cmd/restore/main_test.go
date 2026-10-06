package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/media"
	"sick-fansubs/internal/store/storetest"
)

// testLogger returns a silent logger for command tests.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// seedMediaFile writes one file into the live media directory.
func seedMediaFile(t *testing.T, dataDir, rel string) {
	t.Helper()
	full := filepath.Join(dataDir, "media", rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir media dir: %v", err)
	}
	if err := os.WriteFile(full, []byte("media-content"), 0o644); err != nil {
		t.Fatalf("write media file: %v", err)
	}
}

// buildUnit wraps a database backup artifact into a complete backup unit: a
// media tarball of <dataDir>/media plus the .media-manifest.json sidecar in
// the artifact's directory. Returns the manifest written.
func buildUnit(t *testing.T, dataDir, backupPath string) media.MediaManifest {
	t.Helper()
	unitDir := filepath.Dir(backupPath)
	sha, err := media.BuildMediaTar(dataDir, unitDir, "sick-fansubs.media.tar")
	if err != nil {
		t.Fatalf("build media tar: %v", err)
	}
	m := media.MediaManifest{
		BackupTimestampMS: time.Now().UnixMilli(),
		DBFileName:        filepath.Base(backupPath),
		MediaTarFileName:  "sick-fansubs.media.tar",
		MediaTarSHA256:    sha,
	}
	if err := media.WriteMediaManifest(media.ManifestPathFor(backupPath), m); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return m
}

// writeTar writes the given tar entries as a tarball at path and returns its
// SHA-256 (for manifest rewrites in the tampered-tar test).
func writeTar(t *testing.T, path string, entries map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, content := range entries {
		hdr := &tar.Header{
			Name:    name,
			Mode:    0o644,
			Size:    int64(len(content)),
			ModTime: time.Unix(0, 0).UTC(),
			Format:  tar.FormatUSTAR,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("tar content: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	data := buf.Bytes()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write tar: %v", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// markerPresent reports whether the restore marker exists in dataDir (the
// fail-closed gate — a rejected restore must never create it).
func markerPresent(t *testing.T, dataDir string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(dataDir, "restore.marker"))
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("stat restore marker: %v", err)
	return false
}

// TestRun_RestoresBackupUnitAndMedia drives the full command path against a
// real temp database: seed user/session + media, build a complete unit,
// post-backup changes (session revoked, media file deleted, a NEW media file
// added), restore, and the observable outcomes — sessions gone, the
// pre-backup media file restored from the tar, the post-backup file left
// alone (extraction is additive), quarantine retained, summary printed.
func TestRun_RestoresBackupUnitAndMedia(t *testing.T) {
	dir := testDataDir(t)
	t.Setenv("DATA_DIR", dir)

	// 1. Migrate + seed.
	db, err := database.OpenMaintenance(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open maintenance: %v", err)
	}
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "u1",
		Username:     "kushoyarou",
		Email:        "k@example.com",
		PasswordHash: "$2b$12$fake",
		Role:         "super-admin",
	})
	digest := []byte(strings.Repeat("d", 32))
	if _, err := db.Exec(`INSERT INTO sessions
		(id, user_id, token_digest, csrf_token, auth_version, created_at_ms, expires_at_ms)
		VALUES ('s1', 'u1', ?, ?, 1, 1000, 2000)`, digest, digest); err != nil {
		db.Close()
		t.Fatalf("seed session: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}
	seedMediaFile(t, dir, filepath.Join("aa", "pre-backup.png"))

	backupPath, err := database.BackupForMigration(dir, time.Now().Add(30*time.Second))
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	buildUnit(t, dir, backupPath)

	// 2. Post-backup changes: revoke the session, lose the media file, add a
	//    new one.
	db, err = database.Open(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open live: %v", err)
	}
	if _, err := db.Exec("DELETE FROM sessions"); err != nil {
		db.Close()
		t.Fatalf("revoke session: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close live: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "media", "aa", "pre-backup.png")); err != nil {
		t.Fatalf("remove media file: %v", err)
	}
	seedMediaFile(t, dir, filepath.Join("cc", "post-backup.png"))

	// 3. Restore through the command.
	var out bytes.Buffer
	if err := run(testLogger(), &out, backupPath, false); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "restore complete") {
		t.Fatalf("summary missing from output: %q", out.String())
	}

	// 4. Outcomes.
	app, err := database.OpenApplication(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("OpenApplication after restore: %v", err)
	}
	defer app.Close()
	var n int
	if err := app.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if n != 0 {
		t.Fatalf("got %d, want 0 sessions after restore", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "media", "aa", "pre-backup.png")); err != nil {
		t.Fatalf("pre-backup media file not restored by the extraction: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "media", "cc", "post-backup.png")); err != nil {
		t.Fatalf("post-backup media file removed — extraction must be additive: %v", err)
	}
}

// TestRun_AcceptsRelativeBackupPath pins the absolutization seam: a relative
// -backup path (the natural `make restore-local` invocation) must resolve,
// not fail with the file-URI "invalid uri authority" error.
func TestRun_AcceptsRelativeBackupPath(t *testing.T) {
	dir := testDataDir(t)
	t.Setenv("DATA_DIR", dir)

	db, err := database.OpenMaintenance(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open maintenance: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	backupPath, err := database.BackupForMigration(dir, time.Now().Add(30*time.Second))
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	buildUnit(t, dir, backupPath)

	// Run from the unit's parent directory with a bare relative path.
	t.Chdir(filepath.Dir(backupPath))
	if err := run(testLogger(), io.Discard, filepath.Base(backupPath), false); err != nil {
		t.Fatalf("run with a relative backup path: %v", err)
	}
	if markerPresent(t, dir) {
		t.Fatal("marker still present after the relative-path restore")
	}
}

// TestRun_RequiresBackupFlag pins the flag validation.
func TestRun_RequiresBackupFlag(t *testing.T) {
	t.Setenv("DATA_DIR", testDataDir(t))
	if err := run(testLogger(), io.Discard, "", false); err == nil {
		t.Fatal("expected error without a backup path")
	}
}

// TestRun_RefusesNewerSecurityEventsWithoutOverride pins the reconciliation
// surfacing through the command: a newer SUCCESSFUL account-security event
// fails the run without -acknowledge-loss and succeeds with it.
func TestRun_RefusesNewerSecurityEventsWithoutOverride(t *testing.T) {
	dir := testDataDir(t)
	t.Setenv("DATA_DIR", dir)

	db, err := database.OpenMaintenance(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open maintenance: %v", err)
	}
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "u1",
		Username:     "kushoyarou",
		Email:        "k@example.com",
		PasswordHash: "$2b$12$fake",
		Role:         "super-admin",
	})
	if err := db.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	backupPath, err := database.BackupForMigration(dir, time.Now().Add(30*time.Second))
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	buildUnit(t, dir, backupPath)

	db, err = database.Open(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open live: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO audit_events
		(id, event, result, actor_id, target_id, request_id, remote_addr, created_at_ms)
		VALUES ('e1', 'role_changed', 'success', 'a1', 'u1', 'req-1', '127.0.0.1', ?)`, time.Now().UnixMilli()); err != nil {
		db.Close()
		t.Fatalf("seed event: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close live: %v", err)
	}

	if err := run(testLogger(), io.Discard, backupPath, false); err == nil {
		t.Fatal("expected refusal without -acknowledge-loss")
	}
	// The acknowledged run logs the decision with both timestamps — the
	// durable operator evidence.
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	if err := run(logger, io.Discard, backupPath, true); err != nil {
		t.Fatalf("run with acknowledge: %v", err)
	}
	records := decodeLogRecords(t, logBuf.String())
	warned := false
	for _, rec := range records {
		if rec["msg"] != "restore proceeded over post-backup account-security changes" {
			continue
		}
		warned = true
		if _, ok := rec["liveNewestMs"]; !ok {
			t.Errorf("warning record lacks liveNewestMs: %v", rec)
		}
		if _, ok := rec["backupNewestMs"]; !ok {
			t.Errorf("warning record lacks backupNewestMs: %v", rec)
		}
	}
	if !warned {
		t.Errorf("records = %v, want the acknowledged-loss warning with both timestamps", records)
	}
}

// TestRun_RejectsDBOnlyArtifact pins the unit contract: a pre-migrate
// DB-only artifact (no manifest sidecar) is rejected before anything happens
// — no marker, live database untouched. That artifact is cmd/migrate's own
// rollback path, not this command's.
func TestRun_RejectsDBOnlyArtifact(t *testing.T) {
	dir := testDataDir(t)
	t.Setenv("DATA_DIR", dir)

	db, err := database.OpenMaintenance(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open maintenance: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	backupPath, err := database.BackupForMigration(dir, time.Now().Add(30*time.Second))
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	if err := run(testLogger(), io.Discard, backupPath, false); err == nil {
		t.Fatal("expected rejection of a manifest-less backup")
	}
	if markerPresent(t, dir) {
		t.Fatal("marker created by a rejected restore")
	}
}

// TestRun_RejectsBrokenUnits pins the pre-activation verification: a missing
// tarball, a checksum mismatch, and a manifest bound to a different database
// artifact are all rejected before the marker exists or the database is
// touched.
func TestRun_RejectsBrokenUnits(t *testing.T) {
	cases := []struct {
		name   string
		damage func(t *testing.T, dir, backupPath string, m media.MediaManifest)
	}{
		{"missing tar", func(t *testing.T, dir, backupPath string, m media.MediaManifest) {
			if err := os.Remove(filepath.Join(filepath.Dir(backupPath), m.MediaTarFileName)); err != nil {
				t.Fatalf("remove tar: %v", err)
			}
		}},
		{"renamed tar", func(t *testing.T, dir, backupPath string, m media.MediaManifest) {
			// The manifest still names the original file; the tar exists
			// under a different name. The unit's artifacts no longer match
			// — rejected like a missing tar.
			if err := os.Rename(
				filepath.Join(filepath.Dir(backupPath), m.MediaTarFileName),
				filepath.Join(filepath.Dir(backupPath), "renamed.tar")); err != nil {
				t.Fatalf("rename tar: %v", err)
			}
		}},
		{"checksum mismatch", func(t *testing.T, dir, backupPath string, m media.MediaManifest) {
			m.MediaTarSHA256 = strings.Repeat("0", 64)
			if err := media.WriteMediaManifest(media.ManifestPathFor(backupPath), m); err != nil {
				t.Fatalf("rewrite manifest: %v", err)
			}
		}},
		{"mismatched db filename", func(t *testing.T, dir, backupPath string, m media.MediaManifest) {
			m.DBFileName = "other.db"
			if err := media.WriteMediaManifest(media.ManifestPathFor(backupPath), m); err != nil {
				t.Fatalf("rewrite manifest: %v", err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := testDataDir(t)
			t.Setenv("DATA_DIR", dir)

			db, err := database.OpenMaintenance(database.Config{DataDir: dir})
			if err != nil {
				t.Fatalf("open maintenance: %v", err)
			}
			if err := db.Close(); err != nil {
				t.Fatalf("close db: %v", err)
			}
			backupPath, err := database.BackupForMigration(dir, time.Now().Add(30*time.Second))
			if err != nil {
				t.Fatalf("backup: %v", err)
			}
			m := buildUnit(t, dir, backupPath)
			tc.damage(t, dir, backupPath, m)

			if err := run(testLogger(), io.Discard, backupPath, false); err == nil {
				t.Fatal("expected rejection of the broken unit")
			}
			if markerPresent(t, dir) {
				t.Fatal("marker created by a rejected restore")
			}
		})
	}
}

// TestRun_ExtractionFailureKeepsMarkerThenRerunCompletes pins the
// post-activation failure contract end to end: a tar that passes the
// pre-activation checksum but fails extraction (an unsafe entry name) leaves
// the activated database and the marker in place; after the unit is repaired
// (clean tar + matching manifest hash), a re-run resumes and completes.
func TestRun_ExtractionFailureKeepsMarkerThenRerunCompletes(t *testing.T) {
	dir := testDataDir(t)
	t.Setenv("DATA_DIR", dir)

	db, err := database.OpenMaintenance(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open maintenance: %v", err)
	}
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "u1",
		Username:     "kushoyarou",
		Email:        "k@example.com",
		PasswordHash: "$2b$12$fake",
		Role:         "super-admin",
	})
	if err := db.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	backupPath, err := database.BackupForMigration(dir, time.Now().Add(30*time.Second))
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	unitDir := filepath.Dir(backupPath)

	// A tar with a traversal entry: its checksum is valid (the manifest
	// matches), so verification passes — extraction is what rejects it.
	evilSHA := writeTar(t, filepath.Join(unitDir, "sick-fansubs.media.tar"), map[string]string{"../evil.png": ""})
	evil := media.MediaManifest{
		BackupTimestampMS: time.Now().UnixMilli(),
		DBFileName:        filepath.Base(backupPath),
		MediaTarFileName:  "sick-fansubs.media.tar",
		MediaTarSHA256:    evilSHA,
	}
	if err := media.WriteMediaManifest(media.ManifestPathFor(backupPath), evil); err != nil {
		t.Fatalf("write evil manifest: %v", err)
	}

	if err := run(testLogger(), io.Discard, backupPath, false); err == nil {
		t.Fatal("expected the extraction failure to abort the run")
	}
	if !markerPresent(t, dir) {
		t.Fatal("marker not retained after the extraction failure")
	}

	// The activated database is in place (fail closed — the app refuses to
	// open while the marker exists).
	app, err := database.OpenApplication(database.Config{DataDir: dir})
	if err == nil {
		app.Close()
		t.Fatal("OpenApplication succeeded while the restore marker was retained")
	}

	// Repair the unit: a clean tar + a manifest whose hash matches it.
	cleanSHA := writeTar(t, filepath.Join(unitDir, "sick-fansubs.media.tar"), map[string]string{"aa/ok.png": "restored"})
	clean := evil
	clean.MediaTarSHA256 = cleanSHA
	if err := media.WriteMediaManifest(media.ManifestPathFor(backupPath), clean); err != nil {
		t.Fatalf("write repaired manifest: %v", err)
	}

	if err := run(testLogger(), io.Discard, backupPath, false); err != nil {
		t.Fatalf("re-run after repair: %v", err)
	}
	if markerPresent(t, dir) {
		t.Fatal("marker still present after the completed re-run")
	}
	if _, err := os.Stat(filepath.Join(dir, "media", "aa", "ok.png")); err != nil {
		t.Fatalf("repaired tar not extracted on the re-run: %v", err)
	}
}
