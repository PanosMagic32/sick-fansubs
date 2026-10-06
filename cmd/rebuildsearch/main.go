// Command rebuildsearch rebuilds the FTS5 search index from the content tables.
//
// The search index stores Go-normalized text (Greek accents stripped) —
// SQL cannot produce that text, so the
// rebuild reads every publishable blog post and project, computes the
// normalized search text in Go, and refills both FTS tables in one
// transaction (store.ReindexAll). This is the restore-rebuild path
// and the backfill for a database
// whose content predates migration 0005.
//
// The command opens through the strict application opener (current
// migration history required; it never applies migrations — running
// `make migrate-local` first is the explicit migration step) and writes
// nothing else. It is an offline maintenance step: run it while the
// application is stopped.
//
// Usage:
//
//	DATA_DIR=/app/data cmd/rebuildsearch
//
// Exit codes: 0 success, 1 on any failure. Outcomes go to structured logs
// on stderr.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"sick-fansubs/internal/config"
	"sick-fansubs/internal/database"
	"sick-fansubs/internal/store"
)

// reindexDeadline bounds the whole rebuild — the page-sized queries are
// bounded by the database layer; this guards an operator mistake (e.g.
// pointing at a huge file that is not a real database).
const reindexDeadline = 10 * time.Minute

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("search rebuild failed", "error", err)
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

	ctx, cancel := context.WithTimeout(context.Background(), reindexDeadline)
	defer cancel()

	blogCount, projectCount, err := store.ReindexAll(ctx, db)
	if err != nil {
		return err
	}

	// Close explicitly so a close failure is reported (CloseDatabase is
	// idempotent and the deferred close covers the early-error paths).
	if err := database.CloseDatabase(db); err != nil {
		return fmt.Errorf("close database: %w", err)
	}

	logger.Info("search index rebuilt",
		"blogPosts", blogCount, "projects", projectCount)
	return nil
}
