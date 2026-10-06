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
	"testing"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/media"
	"sick-fansubs/internal/migration"
)

// setupImportMediaCommand prepares a migrated database with two blog rows
// referencing one legacy object and a source directory holding that object.
func setupImportMediaCommand(t *testing.T) (dataDir, sourceDir string) {
	t.Helper()
	dataDir = testDataDir(t)

	db, err := database.Open(database.Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := database.Apply(db); err != nil {
		db.Close()
		t.Fatalf("Apply: %v", err)
	}

	const (
		key = "1778523938726-903c8e7a-71ac-4e3f-a9ed-72a018ec0fbc.jpg"
		url = "https://sickfansubs.com/media/images/" + key
	)
	const q = `INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status,
		 published_at_ms, created_at_ms, updated_at_ms)
		VALUES (?, 'T', '', '', ?, 'published', 1000, 500, 500)`
	for _, id := range []string{"p1", "p2"} {
		if _, err := db.Exec(q, id, url); err != nil {
			db.Close()
			t.Fatalf("insert row %s: %v", id, err)
		}
	}
	db.Close()

	sourceDir = testDataDir(t)
	img := image.NewNRGBA(image.Rect(0, 0, 640, 360))
	img.Set(0, 0, color.NRGBA{R: 200, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, key), buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}

	t.Setenv("DATA_DIR", dataDir)
	return dataDir, sourceDir
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRun_ImportsAndRewrites(t *testing.T) {
	dataDir, sourceDir := setupImportMediaCommand(t)

	report, err := run(discardLogger(), sourceDir)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if report.Source.BlogRowsScanned != 2 || report.Outcome.Processed != 1 || report.Outcome.RowsRewritten != 2 {
		t.Errorf("report = %+v, want 2 rows / 1 processed / 2 rewritten", report)
	}

	db, err := database.OpenMaintenance(database.Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("reopen target: %v", err)
	}
	defer database.CloseDatabase(db)

	key := "1778523938726-903c8e7a-71ac-4e3f-a9ed-72a018ec0fbc.jpg"
	raw, err := os.ReadFile(filepath.Join(sourceDir, key))
	if err != nil {
		t.Fatalf("read source fixture: %v", err)
	}
	wantRel := media.RelativePath(migration.MediaIDForBytes(raw), media.ExtJPG)
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM blog_posts WHERE thumbnail_url = ?`, wantRel).Scan(&count); err != nil {
		t.Fatalf("count rewritten rows: %v", err)
	}
	if count != 2 {
		t.Errorf("rewritten rows = %d, want 2", count)
	}

	if _, err := os.Stat(media.ImagePath(dataDir, migration.MediaIDForBytes(raw), media.ExtJPG)); err != nil {
		t.Errorf("processed file missing: %v", err)
	}
}

func TestRun_ReportIsCountsOnly(t *testing.T) {
	_, sourceDir := setupImportMediaCommand(t)

	// First run rewrites the references; the second run scans nothing and
	// its report must stay counts-and-codes only.
	if _, err := run(discardLogger(), sourceDir); err != nil {
		t.Fatalf("first run: %v", err)
	}
	report, err := run(discardLogger(), sourceDir)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if report.Source.BlogRowsScanned != 0 {
		t.Errorf("second run scanned %d rows, want 0", report.Source.BlogRowsScanned)
	}

	var raw map[string]any
	enc, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if err := json.Unmarshal(enc, &raw); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if bytes.Contains(enc, []byte("sickfansubs.com")) {
		t.Error("report JSON contains legacy URL material — reports are counts and codes only")
	}
	if bytes.Contains(enc, []byte("1778523938726")) {
		t.Error("report JSON contains legacy object keys — reports are counts and codes only")
	}
}

// TestRun_FailedObjectsReturnError pins the command contract: per-object
// failures are counted in the report but must also fail the command (exit
// 1 via main), so cutover automation cannot mistake a partial import for a
// clean one.
func TestRun_FailedObjectsReturnError(t *testing.T) {
	dataDir, sourceDir := setupImportMediaCommand(t)

	// A row whose source object is missing.
	db, err := database.OpenMaintenance(database.Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("reopen target: %v", err)
	}
	const q = `INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status,
		 published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('missing', 'M', '', '', 'https://sickfansubs.com/media/images/9999-missing.jpg', 'published', 1000, 500, 500)`
	if _, err := db.Exec(q); err != nil {
		database.CloseDatabase(db)
		t.Fatalf("insert missing-source row: %v", err)
	}
	if err := database.CloseDatabase(db); err != nil {
		t.Fatalf("close target: %v", err)
	}

	report, err := run(discardLogger(), sourceDir)
	if err == nil {
		t.Fatal("run returned nil error despite failed objects — the command must fail")
	}
	if report == nil || report.Outcome.Failed == 0 {
		t.Fatalf("report = %+v, want failed > 0", report)
	}
}
