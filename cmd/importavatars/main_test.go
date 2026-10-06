package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/store/storetest"
)

const (
	avatarUserID  = "65a0b1c2d3e4f5a6b7c8d9e1"
	avatarKey     = "1778523938726-903c8e7a-71ac-4e3f-a9ed-72a018ec0fbc.jpg"
	avatarURLBase = "https://sickfansubs.com/media/images/"
)

// setupImportAvatarsCommand prepares a migrated database with one user row
// whose preserved ObjectId matches the users export, plus a source directory
// holding the referenced object under its legacy key.
func setupImportAvatarsCommand(t *testing.T) (dataDir, exportPath, sourceDir string) {
	t.Helper()
	dataDir = testDataDir(t)

	db, err := database.OpenMaintenance(database.Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := database.Apply(db); err != nil {
		db.Close()
		t.Fatalf("Apply: %v", err)
	}
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:       avatarUserID,
		Username: "SyntheticAvatar",
		Email:    "avatar@example.com",
	})
	if err := database.CloseDatabase(db); err != nil {
		t.Fatalf("close target: %v", err)
	}

	// Synthetic values only (rule 8): the verifier is a fake $2a$12$ shape,
	// never a real hash.
	exportPath = filepath.Join(testDataDir(t), "users.jsonl")
	export := `{"_id":{"$oid":"` + avatarUserID + `"},"username":"SyntheticAvatar","email":"avatar@example.com","password":"$2a$12$abcdefghijklmnopqrstuvwx0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ01","role":"user","status":"active","avatar":"` + avatarURLBase + avatarKey + `"}` + "\n"
	if err := os.WriteFile(exportPath, []byte(export), 0o600); err != nil {
		t.Fatalf("write export fixture: %v", err)
	}

	sourceDir = testDataDir(t)
	img := image.NewNRGBA(image.Rect(0, 0, 300, 300))
	img.Set(0, 0, color.NRGBA{R: 200, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, avatarKey), buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}

	t.Setenv("DATA_DIR", dataDir)
	return dataDir, exportPath, sourceDir
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestRun_ImportsAvatars pins the command wrapper end to end: env-configured
// data dir, export-derived join, the rewrite, and the returned report.
func TestRun_ImportsAvatars(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process-global state.
	dataDir, exportPath, sourceDir := setupImportAvatarsCommand(t)

	report, err := run(discardLogger(), exportPath, sourceDir)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Source.ExportRecords != 1 || report.Source.UsersWithAvatars != 1 || report.Source.MatchedUsers != 1 {
		t.Errorf("source = %+v, want 1 export record / 1 avatar-bearing user / 1 matched", report.Source)
	}
	if report.Outcome.Processed != 1 || report.Outcome.RowsRewritten != 1 || report.Outcome.Failed != 0 {
		t.Errorf("outcome = %+v, want 1 processed / 1 rewritten / 0 failed", report.Outcome)
	}

	db, err := database.OpenMaintenance(database.Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("reopen target: %v", err)
	}
	defer database.CloseDatabase(db)

	var stored string
	if err := db.QueryRow(`SELECT avatar_url FROM users WHERE id = ?`, avatarUserID).Scan(&stored); err != nil {
		t.Fatalf("read avatar_url: %v", err)
	}
	if !strings.HasPrefix(stored, "media/images/") || !strings.HasSuffix(stored, ".png") {
		t.Errorf("stored avatar = %q, want a media/images/<2hex>/<32hex>.png reference", stored)
	}
}

// TestRun_ReportIsCountsOnly pins the report contract: a second run (nothing
// left to match) still reports counts and codes only — never URLs or keys.
func TestRun_ReportIsCountsOnly(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process-global state.
	_, exportPath, sourceDir := setupImportAvatarsCommand(t)

	if _, err := run(discardLogger(), exportPath, sourceDir); err != nil {
		t.Fatalf("first run: %v", err)
	}
	report, err := run(discardLogger(), exportPath, sourceDir)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if report.Source.MatchedUsers != 0 || report.Outcome.Failed != 0 {
		t.Errorf("second run report = %+v, want 0 matched / 0 failed", report)
	}

	enc, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if bytes.Contains(enc, []byte("sickfansubs.com")) {
		t.Error("report JSON contains legacy URL material — reports are counts and codes only")
	}
	if bytes.Contains(enc, []byte(avatarKey)) {
		t.Error("report JSON contains legacy object keys — reports are counts and codes only")
	}
}

// TestRun_FailedObjectsReturnError pins the command contract: per-object
// failures are counted in the report but must also fail the command (exit 1
// via main), so cutover automation cannot mistake a partial import for a
// clean one.
func TestRun_FailedObjectsReturnError(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process-global state.
	_, exportPath, sourceDir := setupImportAvatarsCommand(t)

	if err := os.Remove(filepath.Join(sourceDir, avatarKey)); err != nil {
		t.Fatalf("remove source fixture: %v", err)
	}

	report, err := run(discardLogger(), exportPath, sourceDir)
	if err == nil {
		t.Fatal("run returned nil error despite failed objects — the command must fail")
	}
	if report == nil || report.Outcome.Failed == 0 {
		t.Fatalf("report = %+v, want failed > 0", report)
	}
}
