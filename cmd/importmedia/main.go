// Command importmedia imports legacy blog and project thumbnail objects into
// the target media store and rewrites the database references.
//
// Input contract: cmd/importdata has already imported the legacy blog and
// project exports, so blog_posts.thumbnail_url / projects.thumbnail_url hold
// the verbatim legacy absolute URL — that URL is the join key to the legacy
// object via migration.LegacyMediaKey: MinIO-shaped URLs keep the legacy
// object key, external-host URLs (Discord CDN, postimg) derive a URL-hash
// key. The objects themselves come from a local source directory named by
// that key, downloaded once from the legacy URL. The cutover-time run
// replaces this directory source with the S3 fetch; the processing and
// rewrite steps stay the same.
//
// Each referenced object is processed through the media pipeline and stored
// under a deterministic sha256-derived 32-hex ID in
// <DATA_DIR>/media/images/, and thumbnail_url is rewritten to the relative
// path. Re-running the command reproduces the same state (idempotent).
//
// Usage:
//
//	DATA_DIR=/app/data MEDIA_SOURCE_DIR=/path/to/legacy-objects cmd/importmedia
//
// Exit codes: 0 when the run completes with zero failed objects, 1 on any
// fatal error OR when at least one object failed (the report still goes to
// stdout — automation must see failures, not infer them from a missing
// report). The reconciliation report goes to stdout as indented JSON
// (counts and reason codes only — never URLs, keys, or filesystem paths);
// operational logs go to stderr.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"sick-fansubs/internal/config"
	"sick-fansubs/internal/database"
	"sick-fansubs/internal/migration"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	sourceDir := os.Getenv("MEDIA_SOURCE_DIR")
	if sourceDir == "" {
		sourceDir = "./media-source"
	}

	if _, err := run(logger, sourceDir); err != nil {
		slog.Error("media import failed", "error", err)
		os.Exit(1)
	}
}

// run executes the media import and returns the reconciliation report. It
// is the testable seam — main() only wires the environment.
func run(logger *slog.Logger, sourceDir string) (*migration.MediaImportReport, error) {
	// Same env contract as cmd/api and cmd/migrate: DATA_DIR with the
	// ./data dev default.
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	// Maintenance opener: applies pending migrations, then the import
	// rewrites references — migrations run through the maintenance opener
	// while the application is offline.
	db, err := database.OpenMaintenance(database.Config{DataDir: cfg.DataDir})
	if err != nil {
		return nil, fmt.Errorf("open target: %w", err)
	}
	// CloseDatabase is idempotent — the deferred close covers the
	// early-error paths.
	defer database.CloseDatabase(db)

	report, err := migration.ImportMedia(context.Background(), db, cfg.DataDir, sourceDir)
	if err != nil {
		return report, err
	}

	// Close explicitly so a close failure is reported (the deferred close
	// makes this call idempotent).
	if err := database.CloseDatabase(db); err != nil {
		return report, err
	}

	logger.Info("media import complete",
		"rows", report.Source.BlogRowsScanned,
		"processed", report.Outcome.Processed,
		"failed", report.Outcome.Failed,
		"rewritten", report.Outcome.RowsRewritten,
	)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return report, fmt.Errorf("write report: %w", err)
	}

	// Per-object failures are counted in the report but must also fail the
	// command, so cutover automation cannot mistake a partial import for a
	// clean one.
	if report.Outcome.Failed > 0 {
		return report, fmt.Errorf("media import completed with %d failed object(s)", report.Outcome.Failed)
	}
	return report, nil
}
