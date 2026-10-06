package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sweepLogger discards log output in sweep tests.
var sweepLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// sweepTestNow is the fixed instant every sweep test derives file ages and
// the sweep call from (docs/patterns/go/testing.md rule 21).
var sweepTestNow = time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)

// idA/idB are well-formed 32-hex identifiers with distinct subdirectories.
const (
	idA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	idB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func writeSweepFile(t *testing.T, now time.Time, dataDir, id, ext string, age time.Duration) string {
	t.Helper()
	rel := RelativePath(id, ext)
	full := filepath.Join(dataDir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte("bytes"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	at := now.Add(-age)
	if err := os.Chtimes(full, at, at); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	return rel
}

func fileExists(t *testing.T, dataDir, rel string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(dataDir, rel))
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("stat %s: %v", rel, err)
	return false
}

// TestSweep_CanceledContextStopsTheWalk pins the cancellation contract: a
// canceled context returns its error instead of walking the whole tree.
func TestSweep_CanceledContextStopsTheWalk(t *testing.T) {
	t.Parallel()

	now := sweepTestNow
	dataDir := t.TempDir()
	writeSweepFile(t, now, dataDir, idA, ExtJPG, 2*SweepGrace)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := Sweep(ctx, sweepLogger, dataDir, nil, now, SweepGrace)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Sweep(canceled context) = %v, want context.Canceled", err)
	}

	// A canceled caller still gets its cancellation when the media root is
	// missing (fresh install).
	_, err = Sweep(ctx, sweepLogger, t.TempDir(), nil, now, SweepGrace)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Sweep(canceled context, missing root) = %v, want context.Canceled", err)
	}
}

func TestSweep_RemovesUnreferencedStaleFile(t *testing.T) {
	t.Parallel()

	now := sweepTestNow
	dataDir := t.TempDir()
	rel := writeSweepFile(t, now, dataDir, idA, ExtJPG, 2*SweepGrace)

	stats, err := Sweep(t.Context(), sweepLogger, dataDir, nil, now, SweepGrace)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if stats.Deleted != 1 {
		t.Errorf("Deleted = %d, want 1", stats.Deleted)
	}
	if fileExists(t, dataDir, rel) {
		t.Error("orphan file still exists after sweep")
	}
}

func TestSweep_KeepsReferencedFileRegardlessOfAge(t *testing.T) {
	t.Parallel()

	now := sweepTestNow
	dataDir := t.TempDir()
	rel := writeSweepFile(t, now, dataDir, idA, ExtJPG, 30*SweepGrace)

	referenced := map[string]struct{}{rel: {}}
	stats, err := Sweep(t.Context(), sweepLogger, dataDir, referenced, now, SweepGrace)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if stats.Deleted != 0 {
		t.Errorf("Deleted = %d, want 0", stats.Deleted)
	}
	if !fileExists(t, dataDir, rel) {
		t.Error("referenced file was deleted")
	}
}

func TestSweep_KeepsUnreferencedFreshFile(t *testing.T) {
	t.Parallel()

	now := sweepTestNow
	dataDir := t.TempDir()
	rel := writeSweepFile(t, now, dataDir, idA, ExtJPG, SweepGrace/2)

	stats, err := Sweep(t.Context(), sweepLogger, dataDir, nil, now, SweepGrace)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if stats.Deleted != 0 {
		t.Errorf("Deleted = %d, want 0", stats.Deleted)
	}
	if !fileExists(t, dataDir, rel) {
		t.Error("fresh orphan was deleted within the grace window")
	}
}

func TestSweep_GraceBoundaryIsStale(t *testing.T) {
	t.Parallel()

	now := sweepTestNow
	dataDir := t.TempDir()
	rel := writeSweepFile(t, now, dataDir, idA, ExtJPG, SweepGrace)

	stats, err := Sweep(t.Context(), sweepLogger, dataDir, nil, now, SweepGrace)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if stats.Deleted != 1 {
		t.Errorf("Deleted = %d, want 1 (exactly-at-grace counts as stale)", stats.Deleted)
	}
	if fileExists(t, dataDir, rel) {
		t.Error("boundary-aged orphan was kept")
	}
}

func TestSweep_SharedReferenceKeepsFile(t *testing.T) {
	t.Parallel()

	// Two referenced files and one orphan — the migration deduplicates
	// identical bytes across posts, so one storage path can
	// back several rows; the set membership is what keeps a file alive.
	const idC = "cccccccccccccccccccccccccccccccc"
	now := sweepTestNow
	dataDir := t.TempDir()
	relA := writeSweepFile(t, now, dataDir, idA, ExtPNG, 30*SweepGrace)
	relB := writeSweepFile(t, now, dataDir, idB, ExtJPG, 30*SweepGrace)
	relC := writeSweepFile(t, now, dataDir, idC, ExtJPG, 30*SweepGrace)

	referenced := map[string]struct{}{relA: {}, relB: {}}
	stats, err := Sweep(t.Context(), sweepLogger, dataDir, referenced, now, SweepGrace)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if stats.Deleted != 1 {
		t.Errorf("Deleted = %d, want 1", stats.Deleted)
	}
	if !fileExists(t, dataDir, relA) || !fileExists(t, dataDir, relB) {
		t.Error("referenced file was deleted")
	}
	if fileExists(t, dataDir, relC) {
		t.Error("unreferenced stale file was kept")
	}
}

func TestSweep_NeverTouchesNonConformingFiles(t *testing.T) {
	t.Parallel()

	now := sweepTestNow
	dataDir := t.TempDir()
	root := filepath.Join(dataDir, "media", "images")

	// Wrong extension.
	badExt := filepath.Join(root, idA[:2], idA+".gif")
	// Short identifier.
	badID := filepath.Join(root, idA[:2], "abc.jpg")
	// Non-hex identifier.
	badHex := filepath.Join(root, idA[:2], "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz.jpg")
	// Subdirectory not equal to id[:2].
	badDir := filepath.Join(root, "zz", idB+".jpg")
	// Nested TWO subdirectories deep: the outer dir matches id[:2], but the
	// pipeline/import never writes that deep — must never be touched.
	nested := filepath.Join(root, idA[:2], idA[:2], idA+".jpg")
	// File directly at the images/ root (no 2-hex subdirectory).
	atRoot := filepath.Join(root, idA+".jpg")

	for _, p := range []string{badExt, badID, badHex, badDir, nested, atRoot} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte("bytes"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
		if err := os.Chtimes(p, now.Add(-2*SweepGrace), now.Add(-2*SweepGrace)); err != nil {
			t.Fatalf("chtimes %s: %v", p, err)
		}
	}

	stats, err := Sweep(t.Context(), sweepLogger, dataDir, nil, now, SweepGrace)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if stats.Deleted != 0 || stats.TempDeleted != 0 {
		t.Errorf("sweep touched non-conforming files: %+v", stats)
	}
}

func TestSweep_RemovesStaleTempKeepsFreshTemp(t *testing.T) {
	t.Parallel()

	now := sweepTestNow
	dataDir := t.TempDir()
	dir := imageDir(dataDir, idA)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	stale := filepath.Join(dir, ".processing-stale")
	fresh := filepath.Join(dir, ".processing-fresh")
	for path, age := range map[string]time.Duration{
		stale: 2 * SweepGrace,
		fresh: SweepGrace / 2,
	} {
		if err := os.WriteFile(path, []byte("bytes"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		if err := os.Chtimes(path, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatalf("chtimes %s: %v", path, err)
		}
	}

	stats, err := Sweep(t.Context(), sweepLogger, dataDir, nil, now, SweepGrace)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if stats.TempDeleted != 1 {
		t.Errorf("TempDeleted = %d, want 1", stats.TempDeleted)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale temp file was not removed")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("fresh temp file was removed")
	}
}

// TestSweep_NestedTempLeftoverUntouched pins the layout-depth rule: a temp
// leftover below the one-level layout is not ours and is never touched, even
// though it carries the temp prefix (writeAtomic only ever creates them in a
// layout directory).
func TestSweep_NestedTempLeftoverUntouched(t *testing.T) {
	t.Parallel()

	now := sweepTestNow
	dataDir := t.TempDir()
	nestedDir := filepath.Join(dataDir, "media", "images", "aa", "nested")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	nested := filepath.Join(nestedDir, ".processing-stale")
	if err := os.WriteFile(nested, []byte("bytes"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chtimes(nested, now.Add(-2*SweepGrace), now.Add(-2*SweepGrace)); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	stats, err := Sweep(t.Context(), sweepLogger, dataDir, nil, now, SweepGrace)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if stats.TempDeleted != 0 {
		t.Errorf("TempDeleted = %d, want 0 for a nested temp file", stats.TempDeleted)
	}
	if _, err := os.Stat(nested); err != nil {
		t.Errorf("nested temp file was touched: %v", err)
	}
}

// TestSweep_LogsAssetIdNotStoragePath pins the logging contract: an orphan
// removal logs the asset id, never the storage path or the data directory
// (docs/patterns/go/logging.md rule 8); a temp leftover logs its basename.
func TestSweep_LogsAssetIdNotStoragePath(t *testing.T) {
	t.Parallel()

	now := sweepTestNow
	dataDir := t.TempDir()
	writeSweepFile(t, now, dataDir, idA, ExtJPG, 2*SweepGrace)

	dir := imageDir(dataDir, idB)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stale := filepath.Join(dir, ".processing-stale")
	if err := os.WriteFile(stale, []byte("bytes"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chtimes(stale, now.Add(-2*SweepGrace), now.Add(-2*SweepGrace)); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	if _, err := Sweep(t.Context(), logger, dataDir, nil, now, SweepGrace); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "file="+idA) {
		t.Errorf("Sweep log does not carry the removed asset id %q: %s", idA, out)
	}
	if !strings.Contains(out, "file="+filepath.Base(stale)) {
		t.Errorf("Sweep log does not carry the temp basename %q: %s", filepath.Base(stale), out)
	}
	if strings.Contains(out, "media/images/") {
		t.Errorf("Sweep log carries the asset's storage path: %s", out)
	}
	if strings.Contains(out, dataDir) {
		t.Errorf("Sweep log carries the data directory: %s", out)
	}
}

// TestSweep_LogsRemovalFailureShape pins the failure logging contract: a
// failed removal names the operation in the `failed` message and carries
// only the unwrapped cause — the path-bearing *os.PathError text never
// reaches a log record (docs/patterns/go/logging.md rules 3 and 8).
func TestSweep_LogsRemovalFailureShape(t *testing.T) {
	t.Parallel()

	now := sweepTestNow
	dataDir := t.TempDir()
	writeSweepFile(t, now, dataDir, idA, ExtJPG, 2*SweepGrace)

	tempDir := imageDir(dataDir, idB)
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stale := filepath.Join(tempDir, ".processing-stale")
	if err := os.WriteFile(stale, []byte("bytes"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chtimes(stale, now.Add(-2*SweepGrace), now.Add(-2*SweepGrace)); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// Read-only image directories: the walk and os.Stat still work, every
	// os.Remove fails with EACCES.
	for _, dir := range []string{imageDir(dataDir, idA), tempDir} {
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatalf("chmod %s: %v", dir, err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	stats, err := Sweep(t.Context(), logger, dataDir, nil, now, SweepGrace)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if stats.Failed != 2 {
		t.Errorf("Sweep(blocked removals) failed = %d, want 2", stats.Failed)
	}

	out := buf.String()
	for _, want := range []string{"media orphan removal failed", "media temp removal failed"} {
		if !strings.Contains(out, want) {
			t.Errorf("Sweep log misses %q: %s", want, out)
		}
	}
	if strings.Contains(out, dataDir) || strings.Contains(out, "media/images/") {
		t.Errorf("Sweep failure log carries a filesystem path: %s", out)
	}
}

func TestSweep_MissingMediaDirIsNotAnError(t *testing.T) {
	t.Parallel()

	now := sweepTestNow
	stats, err := Sweep(t.Context(), sweepLogger, t.TempDir(), nil, now, SweepGrace)
	if err != nil {
		t.Fatalf("Sweep on a fresh data dir: %v", err)
	}
	if stats.Scanned != 0 || stats.Deleted != 0 {
		t.Errorf("stats = %+v, want zero", stats)
	}
}
