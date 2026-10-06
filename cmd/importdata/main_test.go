package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"sick-fansubs/internal/database"
)

// TestRun_ImportsFixture pins the pilot command end-to-end against the
// synthetic blog fixture: fresh data dir, records imported, report correct.
func TestRun_ImportsFixture(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process-global state.
	dir := testDataDir(t)
	t.Setenv("DATA_DIR", dir)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	report, err := run(logger, "../../internal/migration/testdata/legacy-blog.jsonl", "blog-posts")
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if report.Source.BlogPosts != 3 || report.Source.Imported != 3 || report.Source.Rejected != 0 {
		t.Errorf("source counts: got %+v", report.Source)
	}

	db, err := database.OpenApplication(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("OpenApplication after import: %v", err)
	}
	defer db.Close()

	var posts int
	if err := db.QueryRow("SELECT COUNT(*) FROM blog_posts").Scan(&posts); err != nil {
		t.Fatalf("count posts: %v", err)
	}
	if posts != 3 {
		t.Errorf("blog_posts rows: got %d, want 3", posts)
	}
}

// TestRun_ImportsProjectsFixture pins the -kind projects command end-to-end:
// the -kind projects selector routes the synthetic project fixture through
// ReadLegacyProjectsFile + ImportProjects into a fresh data dir, and the
// strict application opener accepts the result.
func TestRun_ImportsProjectsFixture(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process-global state.
	dir := testDataDir(t)
	t.Setenv("DATA_DIR", dir)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	report, err := run(logger, "../../internal/migration/testdata/legacy-projects.jsonl", "projects")
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if report.Source.Projects != 3 || report.Source.Imported != 3 || report.Source.Rejected != 0 {
		t.Errorf("source counts: got %+v", report.Source)
	}

	db, err := database.OpenApplication(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("OpenApplication after import: %v", err)
	}
	defer db.Close()

	var projects int
	if err := db.QueryRow("SELECT COUNT(*) FROM projects").Scan(&projects); err != nil {
		t.Fatalf("count projects: %v", err)
	}
	if projects != 3 {
		t.Errorf("projects rows: got %d, want 3", projects)
	}
}

// TestRun_UnknownKindFails pins the selector boundary: an unknown -kind
// value is a command error before any file is read or database touched.
func TestRun_UnknownKindFails(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process-global state.
	t.Setenv("DATA_DIR", testDataDir(t))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if _, err := run(logger, "nonexistent-file", "nope"); err == nil {
		t.Fatal("expected an error for an unknown record kind")
	}
}

// TestRun_ImportsFavorites pins the -kind favorites selector end to end:
// users → content → favorites in one data dir (the VPS order), the report
// counts the resolved arrays, and the strict application opener accepts the
// result with the join rows present.
func TestRun_ImportsFavorites(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process-global state.
	dir := testDataDir(t)
	t.Setenv("DATA_DIR", dir)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Synthetic users export (rule 8 — synthetic values only; the verifier
	// is a fake 60+ char $2a$12$ shape, never a real hash). The favorite
	// arrays reference the content fixtures' legacy ObjectIds.
	usersFile := filepath.Join(testDataDir(t), "users.jsonl")
	usersJSON := `{"_id":{"$oid":"65a0b1c2d3e4f5a6b7c8d9e1"},"username":"SyntheticFav","email":"fav@example.com","password":"$2a$12$abcdefghijklmnopqrstuvwx0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ01","role":"user","status":"active","updatedAt":{"$date":{"$numberLong":"1706781600000"}},"favoriteBlogPostIds":[{"$oid":"65b0a1c2d3e4f5a6b7c8d9e0"},{"$oid":"65c0a1c2d3e4f5a6b7c8d9e2"}],"favoriteProjectIds":[{"$oid":"65b0d1a2b3c4d5e6f7a8b9e0"}]}` + "\n"
	if err := os.WriteFile(usersFile, []byte(usersJSON), 0o600); err != nil {
		t.Fatalf("write users fixture: %v", err)
	}

	// Order matters: users → content → favorites.
	for _, step := range []struct{ file, kind string }{
		{usersFile, "users"},
		{"../../internal/migration/testdata/legacy-blog.jsonl", "blog-posts"},
		{"../../internal/migration/testdata/legacy-projects.jsonl", "projects"},
	} {
		if _, err := run(logger, step.file, step.kind); err != nil {
			t.Fatalf("run -kind %s: %v", step.kind, err)
		}
	}

	report, err := run(logger, usersFile, "favorites")
	if err != nil {
		t.Fatalf("run -kind favorites: %v", err)
	}
	blog, proj := report.Favorites.Blog, report.Favorites.Projects
	if blog.Users != 1 || blog.SourceRows != 2 || blog.Imported != 2 || blog.UserMissing != 0 || blog.ContentMissing != 0 {
		t.Errorf("blog favorites counts: got %+v", blog)
	}
	if proj.Users != 1 || proj.SourceRows != 1 || proj.Imported != 1 || proj.UserMissing != 0 || proj.ContentMissing != 0 {
		t.Errorf("project favorites counts: got %+v", proj)
	}

	db, err := database.OpenApplication(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("OpenApplication after import: %v", err)
	}
	defer db.Close()

	var blogRows, projRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM blog_post_favorites`).Scan(&blogRows); err != nil {
		t.Fatalf("count blog favorites: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM project_favorites`).Scan(&projRows); err != nil {
		t.Fatalf("count project favorites: %v", err)
	}
	if blogRows != 2 || projRows != 1 {
		t.Errorf("join rows: blog=%d projects=%d, want 2/1", blogRows, projRows)
	}
}
