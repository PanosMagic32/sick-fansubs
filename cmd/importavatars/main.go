// Command importavatars imports legacy user avatar objects into the target
// media store and rewrites users.avatar_url.
//
// Input contract: the users import (cmd/importdata -kind users) deferred
// avatars to NULL, so the join key is NOT in the target
// database — the legacy absolute avatar URL is read from the users EXPORT
// FILE (IMPORT_FILE) and joined to the users table by the preserved 24-hex
// ObjectId. The objects themselves come from a local source directory
// named by the key migration.LegacyMediaKey derives, downloaded once from
// the legacy URL by cmd/fetchmedia -kind users. The cutover-time run
// replaces the directory source with the S3 fetch; the processing and
// rewrite steps stay the same.
//
// Each referenced object is processed through media.ProcessAvatar
// (200×200 square crop, PNG — the avatar pipeline, not the thumbnail
// one), stored under a deterministic sha256-derived 32-hex ID in
// <DATA_DIR>/media/images/, and users.avatar_url is rewritten to the
// relative path. Re-running the command reproduces the same state
// (idempotent).
//
// Usage:
//
//	DATA_DIR=/app/data MEDIA_SOURCE_DIR=/path/to/legacy-objects \
//	  IMPORT_FILE=/path/users.json cmd/importavatars
//
// Exit codes: 0 when the run completes with zero failed objects, 1 on any
// fatal error OR when at least one object failed (the report still goes to
// stdout — automation must see failures, not infer them from a missing
// report). The reconciliation report goes to stdout as indented JSON
// (counts and reason codes only — never URLs, keys, user ids, or
// filesystem paths); operational logs go to stderr.
package main

import (
	"context"
	"encoding/json"
	"errors"
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

	importPath := os.Getenv("IMPORT_FILE")
	if importPath == "" {
		slog.Error("avatar import failed", "error", errors.New("IMPORT_FILE is required (legacy users export file, JSON array or JSON-lines)"))
		os.Exit(1)
	}
	sourceDir := os.Getenv("MEDIA_SOURCE_DIR")
	if sourceDir == "" {
		sourceDir = "./media-source"
	}

	if _, err := run(logger, importPath, sourceDir); err != nil {
		slog.Error("avatar import failed", "error", err)
		os.Exit(1)
	}
}

// run executes the avatar import and returns the reconciliation report. It
// is the testable seam — main() only wires the environment.
func run(logger *slog.Logger, importPath, sourceDir string) (*migration.AvatarImportReport, error) {
	// Same env contract as cmd/api and cmd/migrate: DATA_DIR with the
	// ./data dev default.
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	// The export carries real emails and bcrypt verifiers (hygiene:
	// owner-only, never logged). Only the id→avatar URL pairs are read —
	// the FULL record count is the report's exportRecords; records without
	// an avatar keep an empty value (skipped by the import's URL walk).
	recs, err := migration.ReadLegacyUsersFile(importPath)
	if err != nil {
		return nil, fmt.Errorf("read users export: %w", err)
	}
	legacyByUserID := make(map[string]string, len(recs))
	for _, rec := range recs {
		legacyByUserID[rec.ID.OID] = rec.Avatar
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

	report, err := migration.ImportAvatars(context.Background(), db, cfg.DataDir, sourceDir, legacyByUserID)
	if err != nil {
		return report, err
	}

	// Close explicitly so a close failure is reported (the deferred close
	// makes this call idempotent).
	if err := database.CloseDatabase(db); err != nil {
		return report, err
	}

	logger.Info("avatar import complete",
		"exportRecords", report.Source.ExportRecords,
		"usersWithLegacyAvatars", report.Source.UsersWithAvatars,
		"matchedUsers", report.Source.MatchedUsers,
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
		return report, fmt.Errorf("avatar import completed with %d failed object(s)", report.Outcome.Failed)
	}
	return report, nil
}
