// Command importdata imports legacy export records into the target
// SQLite database and prints the reconciliation report.
//
// This is the data command for the legacy migration slices: it transforms
// source records and reports
// counts, source→target identifier mappings, download classifications,
// warnings, and relationship orphans — never full records or sensitive
// values.
//
// The record kind selects the legacy shape and the target tables:
// -kind blog-posts (default) imports blog_posts/blog_post_downloads,
// -kind projects imports projects/project_downloads,
// -kind users imports users (run BEFORE content — creator/updater
// references resolve against imported users),
// -kind favorites imports the favorite arrays from the USERS export into
// the join tables (run AFTER users + content).
//
// The target database is opened through the maintenance opener, which
// applies pending embedded migrations first (a fresh target through the
// maintenance opener). After the import, the foreign-key
// check runs (docs/patterns/go/sqlite.md — data-dependent checks belong to
// the explicit migration operation, not to readiness).
//
// Usage:
//
//	DATA_DIR=/app/data IMPORT_FILE=/path/export.json cmd/importdata
//	DATA_DIR=/app/data IMPORT_FILE=/path/export.json cmd/importdata -kind projects
//	DATA_DIR=/app/data IMPORT_FILE=/path/export.json cmd/importdata -kind users
//	DATA_DIR=/app/data IMPORT_FILE=/path/export.json cmd/importdata -kind favorites
//
// The source file is the legacy export as a JSON array or JSON-lines.
// Exit codes: 0 success, 1 on any failure. The report goes to stdout as
// indented JSON; operational logs go to stderr.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
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

	kind := flag.String("kind", "blog-posts", "record kind to import: blog-posts (default), projects, users, or favorites")
	flag.Parse()

	importPath := os.Getenv("IMPORT_FILE")
	if importPath == "" {
		slog.Error("import failed", "error", errors.New("IMPORT_FILE is required (legacy export file, JSON array or JSON-lines)"))
		os.Exit(1)
	}

	if _, err := run(logger, importPath, *kind); err != nil {
		slog.Error("import failed", "error", err)
		os.Exit(1)
	}
}

// run executes the import and returns the reconciliation report. It is the
// testable seam — main() only wires the environment. kind selects the
// legacy record shape: "blog-posts", "projects", "users", or "favorites".
func run(logger *slog.Logger, importPath, kind string) (*migration.ReconciliationReport, error) {
	// Same env contract as cmd/api and cmd/migrate: DATA_DIR with the
	// ./data dev default.
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	// Read the source FIRST: a malformed source file fails before the target
	// database is touched. Each kind has its
	// own record shape and import function.
	var (
		importBlog      = kind == "blog-posts"
		importProjects  = kind == "projects"
		importUsers     = kind == "users"
		importFavorites = kind == "favorites"
		blogRecords     []migration.LegacyBlogPost
		projectRecords  []migration.LegacyProject
		userRecords     []migration.LegacyUser
	)
	switch {
	case importBlog:
		blogRecords, err = migration.ReadLegacyBlogPostsFile(importPath)
	case importProjects:
		projectRecords, err = migration.ReadLegacyProjectsFile(importPath)
	case importUsers, importFavorites:
		// Favorites read the USERS export — the arrays live on the legacy
		// user documents.
		userRecords, err = migration.ReadLegacyUsersFile(importPath)
	default:
		return nil, fmt.Errorf("unknown record kind %q (want blog-posts, projects, users, or favorites)", kind)
	}
	if err != nil {
		return nil, err
	}
	if (importBlog && len(blogRecords) == 0) || (importProjects && len(projectRecords) == 0) || ((importUsers || importFavorites) && len(userRecords) == 0) {
		return nil, errors.New("source file contains no records")
	}

	// Maintenance opener: applies pending migrations (including the content
	// schema) before data lands.
	db, err := database.OpenMaintenance(database.Config{DataDir: cfg.DataDir})
	if err != nil {
		return nil, fmt.Errorf("open target: %w", err)
	}
	defer database.CloseDatabase(db)

	ctx := context.Background()

	var report *migration.ReconciliationReport
	switch {
	case importBlog:
		report, err = migration.ImportBlogPosts(ctx, db, blogRecords)
	case importProjects:
		report, err = migration.ImportProjects(ctx, db, projectRecords)
	case importUsers:
		report, err = migration.ImportUsers(ctx, db, userRecords)
	default:
		report, err = migration.ImportFavorites(ctx, db, userRecords)
	}
	if err != nil {
		return report, err
	}
	count := report.Source.BlogPosts
	switch {
	case importProjects:
		count = report.Source.Projects
	case importUsers:
		count = report.Source.Users
	case importFavorites:
		count = report.Favorites.Blog.SourceRows + report.Favorites.Projects.SourceRows
	}

	// Data-dependent relation check after the import (docs/patterns/go/sqlite.md).
	if err := database.VerifyForeignKeys(db); err != nil {
		return report, err
	}

	// Close explicitly (CloseDatabase is idempotent — the deferred close
	// still covers the early-error paths) so a close failure is reported.
	if err := database.CloseDatabase(db); err != nil {
		return report, err
	}

	// The favorites kind counts join rows, not source records — log the
	// favorites section so the operator line matches the printed report
	// (the zeroed Source section would say "imported 0").
	imported, rejected := report.Source.Imported, report.Source.Rejected
	if importFavorites {
		imported = report.Favorites.Blog.Imported + report.Favorites.Projects.Imported
		rejected = report.Favorites.Blog.UserMissing + report.Favorites.Blog.ContentMissing +
			report.Favorites.Projects.UserMissing + report.Favorites.Projects.ContentMissing
	}

	logger.Info("import complete",
		"kind", kind,
		"records", count,
		"imported", imported,
		"rejected", rejected,
	)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return report, fmt.Errorf("write report: %w", err)
	}
	return report, nil
}
