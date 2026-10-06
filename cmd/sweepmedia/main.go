// Command sweepmedia removes orphaned media files.
//
// The sweep snapshots the reference set from the content tables
// (store.ReferencedMediaPaths), walks <DATA_DIR>/media/images/, and removes
// files that match the stored media grammar, are unreferenced, and are
// older than the 24-hour grace window (media.SweepGrace) — plus stale
// .processing-* crash leftovers. Non-conforming files are never touched.
// Every removal is logged at Info.
//
// The API process runs the same sweep at startup and hourly; this command
// is the manual trigger (`make sweep-media-local`) — the cmd/rebuildsearch
// maintenance-command pattern. It opens through the strict application
// opener (current migration history required; it never applies migrations
// — running `make migrate-local` first is the explicit migration step) and
// writes nothing else. It is an offline maintenance step: run it while the
// application is stopped.
//
// Usage:
//
//	DATA_DIR=/app/data cmd/sweepmedia
//
// Exit codes: 0 when the sweep completes (per-file removal failures are
// counted in the stat line), 1 on a fatal error. Outcomes go to structured
// logs on stderr.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"sick-fansubs/internal/config"
	"sick-fansubs/internal/database"
	"sick-fansubs/internal/media"
	"sick-fansubs/internal/store"
)

// sweepDeadline bounds the whole sweep — the directory walk and the
// reference snapshot are both bounded; this guards an operator mistake
// (e.g. pointing at a path that is not a real media directory).
const sweepDeadline = 10 * time.Minute

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("media orphan sweep failed", "error", err)
		os.Exit(1)
	}
}

// run is the testable seam — main() only wires the environment.
func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// The strict application opener: it requires current migration history
	// and NEVER applies migrations — running `make migrate-local` first is
	// the operator's explicit step.
	db, err := database.OpenApplication(database.Config{DataDir: cfg.DataDir})
	if err != nil {
		return fmt.Errorf("open application: %w", err)
	}
	defer database.CloseDatabase(db)

	ctx, cancel := context.WithTimeout(context.Background(), sweepDeadline)
	defer cancel()

	refs, err := store.ReferencedMediaPaths(ctx, db)
	if err != nil {
		return fmt.Errorf("reference snapshot: %w", err)
	}

	stats, err := media.Sweep(ctx, logger, cfg.DataDir, refs, time.Now(), media.SweepGrace)
	if err != nil {
		return fmt.Errorf("sweep: %w", err)
	}

	// Close explicitly so a close failure is reported (CloseDatabase is
	// idempotent and the deferred close covers the early-error paths).
	if err := database.CloseDatabase(db); err != nil {
		return fmt.Errorf("close database: %w", err)
	}

	logger.Info("media orphan sweep complete",
		"scanned", stats.Scanned, "deleted", stats.Deleted,
		"tempDeleted", stats.TempDeleted, "failed", stats.Failed,
		"referenced", len(refs))
	return nil
}
