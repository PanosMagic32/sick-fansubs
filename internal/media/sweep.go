package media

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SweepGrace is the orphan grace window: an unreferenced
// file younger than this is never deleted. The window protects the
// upload→create flow — between the upload response and the content create
// that references it, a file is unreferenced by definition, and the create
// validates thumbnailPath by file existence. Fixed
// constant, not config (the 10 MB upload cap precedent).
const SweepGrace = 24 * time.Hour

// SweepStats reports one sweep run's outcomes. Deleted counts orphans
// removed, TempDeleted counts stale .processing-* crash leftovers, Failed
// counts per-file removal failures (logged and skipped, never fatal), and
// Scanned counts every walked non-directory entry — temp leftovers and
// non-conforming files included.
type SweepStats struct {
	Scanned     int
	Deleted     int
	TempDeleted int
	Failed      int
}

// Sweep removes orphaned processed media files.
//
// referenced is the set of storage-relative paths every active content row
// points at (store.ReferencedMediaPaths) — membership means KEEP, regardless
// of age. Candidates are files under <dataDir>/media/images/ matching the
// stored grammar exactly (<2hex>/<32hex>.(jpg|png) with the subdirectory
// equal to id[:2]); anything non-conforming is NEVER touched. A candidate
// is removed when unreferenced AND older than grace, measured from now.
// Stale .processing-* temp files (the writeAtomic crash leftovers) in a
// layout directory and older than grace are removed too.
//
// # Logging
//
// Every orphan removal is logged at Info with the asset id
// (docs/patterns/go/logging.md rule 8 — never a media storage path); a
// stale temp file logs its .processing-* basename. A per-file removal
// failure is logged (Warn), counted, and does not abort the run; a
// directory-walk failure aborts. Callers log the run summary.
//
// A missing media directory (fresh install) is not an error: the sweep
// reports zero stats. now and grace are parameters for the tests; callers
// pass time.Now and SweepGrace.
func Sweep(ctx context.Context, logger *slog.Logger, dataDir string, referenced map[string]struct{}, now time.Time, grace time.Duration) (SweepStats, error) {
	if logger == nil {
		logger = slog.Default()
	}

	root := filepath.Join(dataDir, "media", "images")
	stats := SweepStats{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		stats.Scanned++

		// The layout-depth contract: both the temp leftovers and the image
		// candidates live EXACTLY one subdirectory below the images root. Anything
		// deeper, or at the root, is not ours and is never touched — not even a
		// file that carries the temp prefix.
		if filepath.Dir(filepath.Dir(path)) != root {
			return nil
		}

		name := d.Name()
		if strings.HasPrefix(name, tempPrefix) {
			if staleFile(path, now, grace) {
				if removeLogged(logger, path, filepath.Base(path), "media temp removed", "media temp removal failed") {
					stats.TempDeleted++
				} else {
					stats.Failed++
				}
			}
			return nil
		}

		dir := filepath.Base(filepath.Dir(path))
		ref := "media/images/" + dir + "/" + name
		id, ext, ok := ParseStorageReference(ref)
		if !ok || dir != id[:2] {
			return nil
		}

		stored := RelativePath(id, ext)
		if _, keep := referenced[stored]; keep {
			return nil
		}
		if !staleFile(path, now, grace) {
			return nil
		}
		if removeLogged(logger, path, id, "media orphan removed", "media orphan removal failed") {
			stats.Deleted++
		} else {
			stats.Failed++
		}
		return nil
	})
	if err != nil {
		if !os.IsNotExist(err) {
			return stats, err
		}
		// A missing root is a fresh install — not an error — but a canceled
		// caller still gets its cancellation.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return stats, ctxErr
		}
	}
	return stats, nil
}

// staleFile reports whether the file's mtime is older than grace relative
// to now — exactly at the boundary counts as stale.
func staleFile(path string, now time.Time, grace time.Duration) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !fi.ModTime().Add(grace).After(now)
}

// removeLogged removes one file, logging the outcome at Info on success and
// Warn on failure. logRef is the reference that goes into the log — the
// asset id for orphans (never the storage path), the temp file's basename
// for crash leftovers. The failure record carries only the unwrapped cause:
// the *os.PathError text embeds the storage path (logging.md rule 8). It
// returns true on success.
func removeLogged(logger *slog.Logger, path, logRef, successMsg, failureMsg string) bool {
	if err := os.Remove(path); err != nil {
		attrs := []any{"file", logRef}
		if cause := errors.Unwrap(err); cause != nil {
			attrs = append(attrs, "error", cause)
		}
		logger.Warn(failureMsg, attrs...)
		return false
	}
	logger.Info(successMsg, "file", logRef)
	return true
}
