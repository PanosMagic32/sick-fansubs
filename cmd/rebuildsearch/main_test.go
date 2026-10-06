package main

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/search"
	"sick-fansubs/internal/store"
)

// TestRun_RebuildsIndex proves the command end to end: a migrated database
// with content but an empty/cleared index becomes searchable again, with
// normalized (accent-insensitive) text and unpublished rows excluded.
func TestRun_RebuildsIndex(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process-global state.
	dir := testDataDir(t)
	t.Setenv("DATA_DIR", dir)

	// Migrate via the maintenance opener, then insert content WITHOUT
	// indexing (simulating a pre-0005 database).
	db, err := database.OpenMaintenance(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open maintenance: %v", err)
	}
	if err := database.Apply(db); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status, published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('b1', 'Καλημέρα κόσμε', '', '', 'https://example.com/t.jpg', 'published', 1000, 500, 500)`); err != nil {
		t.Fatalf("insert fixture: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status, published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('b-draft', 'Κρυφό', '', '', 'https://example.com/t.jpg', 'draft', NULL, 500, 500)`); err != nil {
		t.Fatalf("insert draft fixture: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO projects
		(id, title, description, slug, thumbnail_url, status, published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('p1', 'Έργο Ταξίδι', '', 'slug-p1', 'https://example.com/t.jpg', 'published', 2000, 500, 500)`); err != nil {
		t.Fatalf("insert project fixture: %v", err)
	}
	if err := database.CloseDatabase(db); err != nil {
		t.Fatalf("close: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := run(logger); err != nil {
		t.Fatalf("run: %v", err)
	}

	// Reopen and search through the real store.
	db2, err := database.Open(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer database.CloseDatabase(db2)

	match, err := search.BuildQuery("καλημερα") // unaccented — normalized index
	if err != nil {
		t.Fatalf("BuildQuery: %v", err)
	}
	results, _, err := store.SearchContent(t.Context(), db2, store.SearchQuery{
		Match: match, Scope: store.SearchAll, Sort: store.SearchByDate, Limit: 20,
	})
	if err != nil {
		t.Fatalf("SearchContent: %v", err)
	}
	if len(results) != 1 || results[0].ID != "b1" {
		t.Errorf("results = %+v, want [b1]", results)
	}

	match2, err := search.BuildQuery("ταξιδι")
	if err != nil {
		t.Fatalf("BuildQuery: %v", err)
	}
	proj, _, err := store.SearchContent(t.Context(), db2, store.SearchQuery{
		Match: match2, Scope: store.SearchProjects, Sort: store.SearchByDate, Limit: 20,
	})
	if err != nil {
		t.Fatalf("SearchContent projects: %v", err)
	}
	if len(proj) != 1 || proj[0].ID != "p1" {
		t.Errorf("project results = %+v, want [p1]", proj)
	}

	// The draft must not be indexed by the rebuild.
	var count int
	if err := db2.QueryRow(`SELECT COUNT(*) FROM blog_post_search`).Scan(&count); err != nil {
		t.Fatalf("count index: %v", err)
	}
	if count != 1 {
		t.Errorf("blog index rows = %d, want 1 (drafts excluded)", count)
	}
}

// TestReindexDeadlinePinned pins the rebuild budget, so a silent change is
// caught here.
func TestReindexDeadlinePinned(t *testing.T) {
	t.Parallel()

	if reindexDeadline != 10*time.Minute {
		t.Errorf("reindexDeadline = %s, want 10m", reindexDeadline)
	}
}
