package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/media"
)

// TestRun_SweepsOrphans proves the command end to end: a referenced stale
// file survives, a stale orphan is removed, a fresh orphan is kept (grace
// window), and a non-conforming file is untouched.
func TestRun_SweepsOrphans(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process-global state.
	dir := testDataDir(t)
	t.Setenv("DATA_DIR", dir)

	// Migrate via the maintenance opener, then reference one file.
	db, err := database.OpenMaintenance(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open maintenance: %v", err)
	}
	if err := database.Apply(db); err != nil {
		t.Fatalf("apply: %v", err)
	}
	const refID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	writeCmdMedia(t, dir, refID, 2*media.SweepGrace)
	if _, err := db.Exec(`INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status, published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('b1', 'T', '', '', ?, 'published', 1000, 500, 500)`,
		media.RelativePath(refID, media.ExtJPG)); err != nil {
		t.Fatalf("insert fixture: %v", err)
	}
	if err := database.CloseDatabase(db); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Stale orphan, fresh orphan, and a non-conforming stray.
	const orphanStale = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const orphanFresh = "cccccccccccccccccccccccccccccccc"
	writeCmdMedia(t, dir, orphanStale, 2*media.SweepGrace)
	writeCmdMedia(t, dir, orphanFresh, media.SweepGrace/2)

	imagesRoot := filepath.Join(dir, "media", "images")
	stray := filepath.Join(imagesRoot, "not-media.txt")
	if err := os.WriteFile(stray, []byte("bytes"), 0o644); err != nil {
		t.Fatalf("write stray: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := run(logger); err != nil {
		t.Fatalf("run: %v", err)
	}

	exists := func(rel string) bool {
		_, err := os.Stat(filepath.Join(dir, rel))
		return err == nil
	}
	if !exists(media.RelativePath(refID, media.ExtJPG)) {
		t.Error("referenced file was removed")
	}
	if exists(media.RelativePath(orphanStale, media.ExtJPG)) {
		t.Error("stale orphan was not removed")
	}
	if !exists(media.RelativePath(orphanFresh, media.ExtJPG)) {
		t.Error("fresh orphan was removed within the grace window")
	}
	if !exists("media/images/not-media.txt") {
		t.Error("non-conforming file was touched")
	}
}

// writeCmdMedia writes a tiny media file for one 32-hex id with the given
// age (mtime in the past).
func writeCmdMedia(t *testing.T, dataDir, id string, age time.Duration) {
	t.Helper()
	full := filepath.Join(dataDir, media.RelativePath(id, media.ExtJPG))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir media fixture: %v", err)
	}
	if err := os.WriteFile(full, []byte("bytes"), 0o644); err != nil {
		t.Fatalf("write media fixture: %v", err)
	}
	past := time.Now().Add(-age)
	if err := os.Chtimes(full, past, past); err != nil {
		t.Fatalf("chtimes media fixture: %v", err)
	}
}

// TestSweepDeadlinePinned pins the sweep budget, so a silent change is caught
// here.
func TestSweepDeadlinePinned(t *testing.T) {
	t.Parallel()

	if sweepDeadline != 10*time.Minute {
		t.Errorf("sweepDeadline = %s, want 10m", sweepDeadline)
	}
}
